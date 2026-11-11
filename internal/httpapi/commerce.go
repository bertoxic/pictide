package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"path"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"pictide.local/studio/internal/paystack"
	"pictide.local/studio/internal/security"
)

type orderItem struct {
	PhotoID   string `bson:"photo_id"`
	UnitPrice int64  `bson:"unit_price"`
}
type purchase struct {
	ID              string      `bson:"id"`
	StudioID        string      `bson:"studio_id"`
	GalleryID       string      `bson:"gallery_id"`
	Email           string      `bson:"email"`
	Amount          int64       `bson:"amount"`
	Status          string      `bson:"status"`
	Created         string      `bson:"created"`
	PlatformFee     int64       `bson:"platform_fee"`
	ProcessingFee   *int64      `bson:"processing_fee,omitempty"`
	Items           []orderItem `bson:"items"`
	TokenHash       string      `bson:"token_hash"`
	DownloadsUsed   int         `bson:"downloads_used"`
	MaxDownloads    int         `bson:"max_downloads"`
	PaymentProvider string      `bson:"payment_provider"`
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
	var gallery struct {
		StudioID string `bson:"studio_id"`
		Price    int64  `bson:"price"`
	}
	if s.db.Collection("galleries").FindOne(r.Context(), bson.M{"id": b.GalleryID, "status": "Published", "archived": bson.M{"$ne": 1}}).Decode(&gallery) != nil {
		fail(w, 400, "Gallery unavailable.")
		return
	}
	items := []orderItem{}
	ids := []string{}
	seen := map[string]bool{}
	var amount int64
	for _, id := range b.Photos {
		if seen[id] {
			continue
		}
		seen[id] = true
		var photo struct {
			Price *int64 `bson:"price"`
		}
		if s.db.Collection("photos").FindOne(r.Context(), bson.M{"id": id, "studio_id": gallery.StudioID, "gallery_id": b.GalleryID, "ready": true, "archived": bson.M{"$ne": 1}}).Decode(&photo) != nil {
			fail(w, 400, "Selection contains unavailable photos.")
			return
		}
		price := gallery.Price
		if photo.Price != nil {
			price = *photo.Price
		}
		if price < 0 || amount > 1_000_000_000_000-price {
			fail(w, 400, "Invalid selection total.")
			return
		}
		amount += price
		ids = append(ids, id)
		items = append(items, orderItem{PhotoID: id, UnitPrice: price})
	}
	if b.Expected != nil && *b.Expected != amount {
		fail(w, 409, "Prices have changed. Refresh and review your selection.")
		return
	}
	if amount == 0 {
		reply(w, 200, bson.M{"mode": "free", "photos": ids})
		return
	}
	if amount < 250000 {
		fail(w, 400, "Paid orders must total at least ₦2,500. Please select more photos.")
		return
	}
	if s.cfg.PaystackSecret == "" {
		fail(w, 503, "Paystack is not configured.")
		return
	}
	if s.cfg.FeeBPS <= 0 || s.cfg.FeeBPS >= 10000 {
		fail(w, 503, "Paid checkout is not configured.")
		return
	}
	id, err := security.Token()
	if err != nil {
		fail(w, 500, "Unable to create order.")
		return
	}
	token, err := security.Token()
	if err != nil {
		fail(w, 500, "Unable to create order.")
		return
	}
	purchase := purchase{ID: id, StudioID: gallery.StudioID, GalleryID: b.GalleryID, Email: b.Email, Amount: amount, Status: "Pending", Created: timestamp(), PlatformFee: amount * int64(s.cfg.FeeBPS) / 10000, Items: items, TokenHash: security.HashToken(token), MaxDownloads: len(items) * 3}
	if _, err = s.db.Collection("orders").InsertOne(r.Context(), purchase); err != nil {
		fail(w, 500, "Unable to create order.")
		return
	}
	base := s.cfg.PublicURL
	if base == "" {
		scheme := "http://"
		if s.secure() {
			scheme = "https://"
		}
		base = scheme + r.Host
	}
	link, err := paystack.New(s.cfg.PaystackSecret).Initialize(r.Context(), b.Email, amount, id, base+"/?gallery="+b.GalleryID+"&reference="+id)
	if err != nil {
		fail(w, 502, "Unable to start Paystack checkout. Please try again.")
		return
	}
	reply(w, 200, bson.M{"order": id, "token": token, "amount": amount, "authorization_url": link, "mode": "paystack"})
}

func (s *Server) verifyPurchase(r *http.Request, o purchase) (string, error) {
	if o.Status == "Paid" {
		return "Paid", nil
	}
	transaction, err := paystack.New(s.cfg.PaystackSecret).Verify(r.Context(), o.ID)
	if err != nil {
		return "", err
	}
	if transaction.Reference != o.ID || transaction.Amount != o.Amount || transaction.Currency != "NGN" {
		return "", fmt.Errorf("payment details do not match")
	}
	switch transaction.Status {
	case "success":
		if transaction.Fees == nil || *transaction.Fees < 0 || *transaction.Fees > o.Amount-o.PlatformFee {
			return "", fmt.Errorf("payment processing fee unavailable or invalid")
		}
		_, err = s.db.Collection("orders").UpdateOne(r.Context(), bson.M{"id": o.ID, "amount": o.Amount, "status": bson.M{"$in": bson.A{"Pending", "Failed"}}}, bson.M{"$set": bson.M{"status": "Paid", "payment_provider": "paystack", "processing_fee": *transaction.Fees}})
		if err != nil {
			return "", err
		}
	case "failed", "abandoned":
		_, err = s.db.Collection("orders").UpdateOne(r.Context(), bson.M{"id": o.ID, "status": "Pending"}, bson.M{"$set": bson.M{"status": "Failed"}})
		if err != nil {
			return "", err
		}
	}
	var current purchase
	if err = s.db.Collection("orders").FindOne(r.Context(), bson.M{"id": o.ID}).Decode(&current); err != nil {
		return "", err
	}
	return current.Status, nil
}

func (s *Server) verifyPayment(w http.ResponseWriter, r *http.Request, id string) {
	var b struct{ Token string }
	if decode(r, &b) != nil || b.Token == "" {
		fail(w, 400, "Invalid request.")
		return
	}
	var o purchase
	if s.db.Collection("orders").FindOne(r.Context(), bson.M{"id": id, "token_hash": security.HashToken(b.Token)}).Decode(&o) != nil {
		fail(w, 403, "Order not found.")
		return
	}
	status, err := s.verifyPurchase(r, o)
	if err != nil {
		fail(w, 502, "Could not verify payment.")
		return
	}
	if status != "Paid" {
		fail(w, 400, "Payment has not been verified.")
		return
	}
	photos := []string{}
	for _, item := range o.Items {
		photos = append(photos, item.PhotoID)
	}
	reply(w, 200, bson.M{"status": "Paid", "photos": photos})
}
func (s *Server) syncOrder(w http.ResponseWriter, r *http.Request, user *account, id string) {
	var o purchase
	if s.db.Collection("orders").FindOne(r.Context(), bson.M{"id": id, "studio_id": user.ID}).Decode(&o) != nil {
		http.NotFound(w, r)
		return
	}
	status, err := s.verifyPurchase(r, o)
	if err != nil {
		fail(w, 502, "Could not verify this order with Paystack.")
		return
	}
	reply(w, 200, bson.M{"status": status})
}

func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 || !paystack.New(s.cfg.PaystackSecret).VerifySignature(body, r.Header.Get("X-Paystack-Signature")) {
		w.WriteHeader(401)
		return
	}
	var event struct {
		Event string `json:"event"`
		Data  struct {
			Reference string `json:"reference"`
			Status    string `json:"status"`
			Currency  string `json:"currency"`
			Amount    int64  `json:"amount"`
			Fees      *int64 `json:"fees"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &event) != nil {
		w.WriteHeader(400)
		return
	}
	if event.Event == "charge.success" && event.Data.Status == "success" && event.Data.Currency == "NGN" {
		var order purchase
		err = s.db.Collection("orders").FindOne(r.Context(), bson.M{"id": event.Data.Reference, "amount": event.Data.Amount}).Decode(&order)
		if errors.Is(err, mongo.ErrNoDocuments) {
			w.WriteHeader(200)
			return
		}
		if err != nil {
			w.WriteHeader(500)
			return
		}
		if order.Status == "Paid" {
			w.WriteHeader(200)
			return
		}
		if event.Data.Fees == nil || *event.Data.Fees < 0 || *event.Data.Fees > order.Amount-order.PlatformFee {
			if _, err = s.verifyPurchase(r, order); err != nil {
				w.WriteHeader(503)
				return
			}
			w.WriteHeader(200)
			return
		}
		_, err = s.db.Collection("orders").UpdateOne(r.Context(), bson.M{"id": event.Data.Reference, "amount": event.Data.Amount, "status": bson.M{"$in": bson.A{"Pending", "Failed"}}}, bson.M{"$set": bson.M{"status": "Paid", "payment_provider": "paystack", "processing_fee": *event.Data.Fees}})
		if err != nil {
			w.WriteHeader(500)
			return
		}
	}
	if strings.HasPrefix(event.Event, "transfer.") {
		w.WriteHeader(503)
		return
	}
	w.WriteHeader(200)
}

func (s *Server) downloadLink(w http.ResponseWriter, r *http.Request, id string) {
	var b struct{ Token string }
	if decode(r, &b) != nil || b.Token == "" {
		fail(w, 400, "Invalid request.")
		return
	}
	var order purchase
	filter := bson.M{"token_hash": security.HashToken(b.Token), "status": "Paid", "items.photo_id": id, "$expr": bson.M{"$lt": bson.A{"$downloads_used", "$max_downloads"}}}
	err := s.db.Collection("orders").FindOneAndUpdate(r.Context(), filter, bson.M{"$inc": bson.M{"downloads_used": 1}}).Decode(&order)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			fail(w, 403, "Photo unavailable or download limit reached.")
		} else {
			fail(w, 500, "Unable to prepare download.")
		}
		return
	}
	expires := time.Now().Unix() + 90
	signature := security.SignDownload(s.cfg.SigningKey, id, expires)
	reply(w, 200, bson.M{"url": fmt.Sprintf("/api/download/%s?expires=%d&signature=%s", id, expires, signature)})
}
func (s *Server) download(w http.ResponseWriter, r *http.Request, id string) {
	expires, err := strconv.ParseInt(r.URL.Query().Get("expires"), 10, 64)
	if err != nil || !security.CheckDownload(s.cfg.SigningKey, id, r.URL.Query().Get("signature"), expires, time.Now()) {
		fail(w, 403, "Download link expired or invalid.")
		return
	}
	s.sendAsset(w, r, id, "master", "image/png")
}
func (s *Server) sendAsset(w http.ResponseWriter, r *http.Request, id, variant, contentType string) {
	if variant == "master" && s.r2 != nil {
		var photo struct {
			StudioID string `bson:"studio_id"`
			R2Master bool   `bson:"r2_master"`
		}
		if err := s.db.Collection("photos").FindOne(r.Context(), bson.M{"id": id, "ready": true}).Decode(&photo); err != nil {
			http.NotFound(w, r)
			return
		}
		key := r2PhotoKey(photo.StudioID, id, "master")
		if !photo.R2Master {
			var legacy bytes.Buffer
			if _, err := s.db.GridFSBucket().DownloadToStream(r.Context(), id+":master", &legacy); err != nil {
				http.NotFound(w, r)
				return
			}
			if err := s.r2.put(r.Context(), key, "image/png", legacy.Bytes()); err != nil {
				fail(w, 502, "Unable to prepare download storage.")
				return
			}
			if _, err := s.db.Collection("photos").UpdateOne(r.Context(), bson.M{"id": id, "studio_id": photo.StudioID, "ready": true}, bson.M{"$set": bson.M{"r2_master": true}}); err != nil {
				fail(w, 500, "Unable to prepare download.")
				return
			}
		}
		url, err := s.r2.getURL(r.Context(), key)
		if err != nil {
			fail(w, 502, "Unable to prepare download.")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, url, http.StatusFound)
		return
	}
	var data bytes.Buffer
	if _, err := s.db.GridFSBucket().DownloadToStream(r.Context(), id+":"+variant, &data); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data.Bytes())
}

func (s *Server) photoDownload(w http.ResponseWriter, r *http.Request, id, kind string) {
	var photo struct {
		GalleryID string `bson:"gallery_id"`
		StudioID  string `bson:"studio_id"`
		Price     *int64 `bson:"price"`
	}
	if s.db.Collection("photos").FindOne(r.Context(), bson.M{"id": id, "ready": true}).Decode(&photo) != nil {
		http.NotFound(w, r)
		return
	}
	if kind == "clean" {
		token := r.URL.Query().Get("token")
		if token != "" {
			count, err := s.db.Collection("orders").CountDocuments(r.Context(), bson.M{"token_hash": security.HashToken(token), "status": "Paid", "items.photo_id": id})
			if err != nil {
				fail(w, 500, "Unable to load photo.")
				return
			}
			if count > 0 {
				s.sendAsset(w, r, id, "clean", "image/jpeg")
				return
			}
		}
	}
	var g struct {
		Price int64 `bson:"price"`
	}
	if s.db.Collection("galleries").FindOne(r.Context(), bson.M{"id": photo.GalleryID, "studio_id": photo.StudioID, "status": "Published", "archived": bson.M{"$ne": 1}}).Decode(&g) != nil {
		http.NotFound(w, r)
		return
	}
	price := g.Price
	if photo.Price != nil {
		price = *photo.Price
	}
	if price != 0 {
		fail(w, 403, "Photo is not free.")
		return
	}
	count, err := s.db.Collection("photos").CountDocuments(r.Context(), bson.M{"id": id, "archived": bson.M{"$ne": 1}})
	if err != nil || count == 0 {
		http.NotFound(w, r)
		return
	}
	if kind == "clean" {
		variant := "clean"
		if r.URL.Query().Get("size") == "thumb" {
			variant = "clean_thumb"
		}
		s.sendAsset(w, r, id, variant, "image/jpeg")
		return
	}
	s.sendAsset(w, r, id, "master", "image/png")
}

func (s *Server) freeArchive(w http.ResponseWriter, r *http.Request, id string) {
	var gallery struct {
		StudioID string `bson:"studio_id"`
		Price    int64  `bson:"price"`
	}
	if s.db.Collection("galleries").FindOne(r.Context(), bson.M{"id": id, "status": "Published", "archived": bson.M{"$ne": 1}}).Decode(&gallery) != nil {
		http.NotFound(w, r)
		return
	}
	cur, err := s.db.Collection("photos").Find(r.Context(), bson.M{"studio_id": gallery.StudioID, "gallery_id": id, "ready": true, "archived": bson.M{"$ne": 1}}, options.Find().SetProjection(bson.M{"id": 1, "name": 1, "price": 1, "r2_master": 1}))
	if err != nil {
		fail(w, 500, "Unable to load photos.")
		return
	}
	defer cur.Close(r.Context())
	var photos []struct {
		ID       string `bson:"id"`
		Name     string `bson:"name"`
		Price    *int64 `bson:"price"`
		R2Master bool   `bson:"r2_master"`
	}
	if cur.All(r.Context(), &photos) != nil {
		fail(w, 500, "Unable to load photos.")
		return
	}
	writeArchive := func(dst io.Writer) error {
		archive := zip.NewWriter(dst)
		for i, photo := range photos {
			price := gallery.Price
			if photo.Price != nil {
				price = *photo.Price
			}
			if price != 0 {
				continue
			}
			var source io.ReadCloser
			var e error
			if photo.R2Master {
				if s.r2 == nil {
					return errors.New("R2 storage is unavailable")
				}
				source, e = s.r2.open(r.Context(), r2PhotoKey(gallery.StudioID, photo.ID, "master"))
			} else {
				source, e = s.db.GridFSBucket().OpenDownloadStream(r.Context(), photo.ID+":master")
			}
			if e != nil {
				return e
			}
			name := path.Base(strings.ReplaceAll(photo.Name, "\\", "/"))
			if name == "." || name == "/" || name == "" {
				name = "photo"
			}
			name = strings.TrimSuffix(name, path.Ext(name))
			entry, e := archive.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("%03d-%s.png", i+1, name), Method: zip.Store})
			if e == nil {
				_, e = io.Copy(entry, source)
			}
			source.Close()
			if e != nil {
				return e
			}
		}
		return archive.Close()
	}
	if s.r2 != nil {
		token, e := security.Token()
		if e != nil {
			fail(w, 500, "Unable to prepare archive.")
			return
		}
		key := "pictide/temporary/archives/" + token + ".zip"
		if e = s.r2.putArchive(r.Context(), key, writeArchive); e != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = s.r2.delete(cleanup, key)
			cancel()
			fail(w, 502, "Unable to prepare archive.")
			return
		}
		url, e := s.r2.getURL(r.Context(), key)
		if e != nil {
			fail(w, 502, "Unable to prepare archive.")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, url, http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="photos.zip"`)
	_ = writeArchive(w)
}
