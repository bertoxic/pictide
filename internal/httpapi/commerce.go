package httpapi

import (
	"archive/zip"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"io"
	"pictide.local/studio/internal/commerce"
	"pictide.local/studio/internal/security"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (s *Server) orders(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Has("limit") {
		s.pagedOrders(w, r)
		return
	}
	v, e := s.query("SELECT o.*,g.name gallery_name FROM orders o JOIN galleries g ON g.id=o.gallery_id ORDER BY o.created DESC")
	if e != nil {
		fail(w, 500, "Unable to load orders.")
		return
	}
	respond(w, 200, v)
}
func (s *Server) checkout(w http.ResponseWriter, r *http.Request) {
	var b struct {
		GalleryID string   `json:"galleryId"`
		Email     string   `json:"email"`
		Photos    []string `json:"photos"`
		Expected  *int64   `json:"expectedAmount"`
	}
	if decode(r, &b) != nil || len(b.Photos) == 0 || len(b.Photos) > 500 {
		fail(w, 400, "Select photographs and enter an email.")
		return
	}
	addr, err := mail.ParseAddress(b.Email)
	if err != nil || addr.Address != b.Email {
		fail(w, 400, "Enter a valid email.")
		return
	}
	s.mediaMu.Lock()
	locked := true
	defer func() {
		if locked {
			s.mediaMu.Unlock()
		}
	}()
	g, err := s.gallery(b.GalleryID)
	if err != nil || g["status"] != "Published" || g["archived"] == int64(1) {
		fail(w, 400, "Gallery unavailable.")
		return
	}
	seen := map[string]bool{}
	ids := []string{}
	prices := []int64{}
	var amount int64
	for _, id := range b.Photos {
		if seen[id] {
			continue
		}
		seen[id] = true
		var p int64
		err = s.DB.QueryRow("SELECT COALESCE(price,?) FROM photos WHERE id=? AND gallery_id=? AND archived=0", g["price"], id, b.GalleryID).Scan(&p)
		if err != nil || p < 0 {
			fail(w, 400, "Selection contains unavailable photos.")
			return
		}
		if amount > 1000000000000-p {
			fail(w, 400, "Total exceeds the permitted amount.")
			return
		}
		ids = append(ids, id)
		prices = append(prices, p)
		amount += p
	}
	if b.Expected != nil && *b.Expected != amount {
		fail(w, 409, "Prices have changed. Refresh and review your selection.")
		return
	}
	if amount == 0 {
		respond(w, 200, map[string]any{"mode": "free", "photos": ids})
		return
	}
	if s.Config.PaystackSecret == "" {
		fail(w, 503, "Paystack is not configured.")
		return
	}
	order := uuid.NewString()
	token, err := security.Token()
	if err != nil {
		fail(w, 500, "Could not create order.")
		return
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "Could not create order.")
		return
	}
	defer tx.Rollback()
	_, err = tx.Exec("INSERT INTO orders(id,gallery_id,email,amount,status,created,platform_fee) VALUES(?,?,?,?,?,?,?)", order, b.GalleryID, b.Email, amount, "Pending", now(), amount*int64(s.Config.FeeBPS)/10000)
	for i, id := range ids {
		if err != nil {
			break
		}
		_, err = tx.Exec("INSERT INTO order_photos(order_id,photo_id,unit_price) VALUES(?,?,?)", order, id, prices[i])
	}
	if err == nil {
		_, err = tx.Exec("INSERT INTO entitlements(token_hash,order_id,downloads_used,max_downloads) VALUES(?,?,0,?)", security.HashToken(token), order, len(ids)*3)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(w, 500, "Could not create order.")
		return
	}
	// The order and entitlements are durable; deletion will archive ordered photos.
	// Do not hold the studio-wide media lock during the external provider request.
	s.mediaMu.Unlock()
	locked = false
	callback := s.base(r) + "/?gallery=" + b.GalleryID + "&reference=" + order
	link, err := s.Paystack.Initialize(r.Context(), b.Email, amount, order, callback)
	if err != nil {
		// Initialization errors can be ambiguous; leave the order pending for reconciliation.
		fail(w, 502, "Unable to start Paystack checkout. Please try again.")
		return
	}
	respond(w, 200, map[string]any{"order": order, "token": token, "amount": amount, "authorization_url": link, "mode": "paystack"})
}

func (s *Server) syncOrder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	o, err := s.one("SELECT * FROM orders WHERE id=?", id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if o["status"] != "Pending" && o["status"] != "Failed" {
		respond(w, 200, map[string]any{"status": o["status"]})
		return
	}
	t, err := s.Paystack.Verify(r.Context(), id)
	if err != nil {
		fail(w, 502, "Could not verify this order with Paystack.")
		return
	}
	if t.Reference != id || t.Amount != o["amount"].(int64) || t.Currency != "NGN" {
		fail(w, 400, "Payment details do not match this order.")
		return
	}
	status := str(o["status"])
	if t.Status == "success" {
		if err = commerce.CompletePayment(r.Context(), s.DB, id, id, "paystack"); err != nil {
			fail(w, 500, "Could not complete order.")
			return
		}
		status = "Paid"
	} else if t.Status == "failed" || t.Status == "abandoned" {
		status = "Failed"
		_, err = s.DB.Exec("UPDATE orders SET status='Failed' WHERE id=? AND status='Pending'", id)
		if err != nil {
			fail(w, 500, "Could not update order.")
			return
		}
	}
	if err = s.DB.QueryRow("SELECT status FROM orders WHERE id=?", id).Scan(&status); err != nil {
		fail(w, 500, "Could not load order status.")
		return
	}
	respond(w, 200, map[string]any{"status": status})
}

func (s *Server) orderForToken(id, token string) (map[string]any, error) {
	return s.one("SELECT o.* FROM orders o JOIN entitlements e ON e.order_id=o.id WHERE o.id=? AND e.token_hash=?", id, security.HashToken(token))
}
func (s *Server) verifyPayment(w http.ResponseWriter, r *http.Request) {
	var b struct{ Token string }
	if decode(r, &b) != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	id := chi.URLParam(r, "id")
	o, err := s.orderForToken(id, b.Token)
	if err != nil {
		fail(w, 403, "Order not found.")
		return
	}
	if o["status"] != "Paid" {
		t, e := s.Paystack.Verify(r.Context(), id)
		if e != nil {
			fail(w, 502, "Could not verify payment.")
			return
		}
		if t.Status != "success" || t.Reference != id || t.Amount != o["amount"].(int64) || t.Currency != "NGN" {
			fail(w, 400, "Payment has not been verified.")
			return
		}
		if e = commerce.CompletePayment(r.Context(), s.DB, id, id, "paystack"); e != nil {
			fail(w, 500, "Could not complete order.")
			return
		}
	}
	rows, err := s.query("SELECT photo_id FROM order_photos WHERE order_id=?", id)
	if err != nil {
		fail(w, 500, "Could not load order.")
		return
	}
	photos := []string{}
	for _, x := range rows {
		photos = append(photos, str(x["photo_id"]))
	}
	respond(w, 200, map[string]any{"status": "Paid", "photos": photos})
}
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil || !s.Paystack.VerifySignature(body, r.Header.Get("X-Paystack-Signature")) {
		w.WriteHeader(401)
		return
	}
	var event struct {
		Event string `json:"event"`
		Data  struct {
			Reference, Status, Currency string
			Amount                      int64
		} `json:"data"`
	}
	if json.Unmarshal(body, &event) != nil {
		w.WriteHeader(400)
		return
	}
	if event.Event == "charge.success" {
		var amount int64
		var status string
		err = s.DB.QueryRow("SELECT amount,status FROM orders WHERE id=?", event.Data.Reference).Scan(&amount, &status)
		if err == nil && event.Data.Status == "success" && event.Data.Amount == amount && event.Data.Currency == "NGN" {
			if err = commerce.CompletePayment(r.Context(), s.DB, event.Data.Reference, event.Data.Reference, "paystack"); err != nil {
				w.WriteHeader(500)
				return
			}
		}
	}
	if event.Event == "transfer.success" || event.Event == "transfer.failed" || event.Event == "transfer.reversed" {
		target := strings.TrimPrefix(event.Event, "transfer.")
		if event.Data.Status == target && event.Data.Currency == "NGN" {
			_, err = s.DB.Exec("UPDATE withdrawals SET status=? WHERE reference=? AND amount=? AND ((status IN ('pending','otp') AND ? IN ('success','failed','reversed')) OR (status='success' AND ?='reversed'))", target, event.Data.Reference, event.Data.Amount, target, target)
			if err != nil {
				w.WriteHeader(500)
				return
			}
		}
	}
	w.WriteHeader(200)
}
func (s *Server) downloadLink(w http.ResponseWriter, r *http.Request) {
	var b struct{ Token string }
	if decode(r, &b) != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := os.Stat(filepath.Join(s.Config.MediaDir, id+".png")); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := commerce.ConsumeDownload(r.Context(), s.DB, security.HashToken(b.Token), id); err != nil {
		fail(w, 403, "Photo unavailable or download limit reached.")
		return
	}
	expires := time.Now().Unix() + 90
	signature := security.SignDownload(s.SigningKey, id, expires)
	respond(w, 200, map[string]string{"url": fmt.Sprintf("/api/download/%s?expires=%d&signature=%s", id, expires, signature)})
}
func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	expires, e := strconv.ParseInt(r.URL.Query().Get("expires"), 10, 64)
	if e != nil || !security.CheckDownload(s.SigningKey, id, r.URL.Query().Get("signature"), expires, time.Now()) {
		w.WriteHeader(403)
		return
	}
	var name string
	if s.DB.QueryRow("SELECT name FROM photos WHERE id=?", id).Scan(&name) != nil {
		http.NotFound(w, r)
		return
	}
	s.sendDownload(w, r, id, name)
}
func (s *Server) sendDownload(w http.ResponseWriter, r *http.Request, id, name string) {
	if _, err := uuid.Parse(id); err != nil {
		http.NotFound(w, r)
		return
	}
	filename := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)) + ".png"
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	http.ServeFile(w, r, filepath.Join(s.Config.MediaDir, id+".png"))
}
func (s *Server) freeName(id string) (string, error) {
	var name string
	err := s.DB.QueryRow("SELECT p.name FROM photos p JOIN galleries g ON g.id=p.gallery_id WHERE p.id=? AND g.status='Published' AND g.archived=0 AND p.archived=0 AND COALESCE(p.price,g.price)=0", id).Scan(&name)
	return name, err
}
func (s *Server) freePhoto(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	name, err := s.freeName(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.sendDownload(w, r, id, name)
}
func (s *Server) clean(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		http.NotFound(w, r)
		return
	}
	_, err := s.freeName(id)
	if err != nil {
		var n int
		err = s.DB.QueryRow("SELECT 1 FROM entitlements e JOIN orders o ON o.id=e.order_id JOIN order_photos p ON p.order_id=o.id WHERE e.token_hash=? AND p.photo_id=? AND o.status='Paid'", security.HashToken(r.URL.Query().Get("token")), id).Scan(&n)
	}
	if err != nil {
		w.WriteHeader(403)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	file := filepath.Join(s.Config.MediaDir, id+"_clean.jpg")
	if r.URL.Query().Get("size") == "thumb" {
		thumb := filepath.Join(s.Config.MediaDir, id+"_thumb_clean.jpg")
		if _, err := os.Stat(thumb); err == nil {
			file = thumb
		}
	}
	http.ServeFile(w, r, file)
}
func (s *Server) freeArchive(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rows, err := s.query("SELECT p.id,p.name FROM photos p JOIN galleries g ON g.id=p.gallery_id WHERE g.id=? AND g.status='Published' AND g.archived=0 AND p.archived=0 AND COALESCE(p.price,g.price)=0", id)
	if err != nil || len(rows) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="pictide-free-photos.zip"`)
	z := zip.NewWriter(w)
	for i, p := range rows {
		photo := str(p["id"])
		if _, e := uuid.Parse(photo); e != nil {
			continue
		}
		f, e := os.Open(filepath.Join(s.Config.MediaDir, photo+".png"))
		if e != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(str(p["name"])), filepath.Ext(str(p["name"]))) + ".png"
		dst, e := z.Create(fmt.Sprintf("%d-%s", i+1, name))
		if e == nil {
			_, _ = io.Copy(dst, f)
		}
		f.Close()
	}
	_ = z.Close()
}

var _ = sql.ErrNoRows
