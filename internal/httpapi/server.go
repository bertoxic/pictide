package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"pictide.local/studio/internal/config"
	"pictide.local/studio/internal/imgbb"
	"pictide.local/studio/internal/paystack"
	"pictide.local/studio/internal/security"
)

type Server struct {
	DB             *sql.DB
	Config         config.Config
	Paystack       *paystack.Client
	SigningKey     string
	attempts       map[string]attempt
	uploads        map[string]attempt
	mu             sync.Mutex
	mediaMu        sync.Mutex
	previewStore   previewStore
	previewMu      sync.Mutex
	previewPending map[string]bool
	previewWorkers chan struct{}
}
type attempt struct {
	count int
	until time.Time
}

func New(db *sql.DB, c config.Config) (*Server, error) {
	key := c.SigningKey
	if key == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		key = hex.EncodeToString(b)
	}
	if err := os.MkdirAll(c.MediaDir, 0700); err != nil {
		return nil, err
	}
	p := paystack.New(c.PaystackSecret)
	server := &Server{DB: db, Config: c, Paystack: p, SigningKey: key, attempts: map[string]attempt{}, uploads: map[string]attempt{}, previewPending: map[string]bool{}, previewWorkers: make(chan struct{}, 2)}
	if c.ImgBBPreviews && c.ImgBBKey != "" {
		server.previewStore = imgbb.New(c.ImgBBKey)
	}
	return server, nil
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if value != nil {
		_ = json.NewEncoder(w).Encode(value)
	}
}
func fail(w http.ResponseWriter, status int, msg string) {
	respond(w, status, map[string]string{"error": msg})
}
func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	d := json.NewDecoder(io.LimitReader(r.Body, 2<<20))
	return d.Decode(v)
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func rowsMap(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(cols))
		p := make([]any, len(cols))
		for i := range values {
			p[i] = &values[i]
		}
		if err = rows.Scan(p...); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for i, c := range cols {
			if b, ok := values[i].([]byte); ok {
				m[c] = string(b)
			} else {
				m[c] = values[i]
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Server) query(query string, args ...any) ([]map[string]any, error) {
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	return rowsMap(rows)
}
func (s *Server) one(query string, args ...any) (map[string]any, error) {
	rows, err := s.query(query, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, sql.ErrNoRows
	}
	return rows[0], nil
}
func (s *Server) setting(key, fallback string) string {
	var v string
	if s.DB.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&v) != nil {
		return fallback
	}
	return v
}
func (s *Server) putSetting(key, value string) error {
	_, err := s.DB.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value)
	return err
}
func (s *Server) HasPassword() bool { return s.passwordRequired() }
func (s *Server) passwordRequired() bool {
	return s.setting("password_hash", "") != "" || s.Config.AdminPassword != ""
}
func (s *Server) checkPassword(p string) bool {
	if hash := s.setting("password_hash", ""); hash != "" {
		return security.CheckPassword(hash, p)
	}
	return s.Config.AdminPassword != "" && subtle.ConstantTimeCompare([]byte(p), []byte(s.Config.AdminPassword)) == 1
}
func (s *Server) owner(r *http.Request) bool {
	if !s.passwordRequired() {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return false
		}
		return net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
	}
	cookie, err := r.Cookie("lumen_session")
	if err != nil {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 3 {
		return false
	}
	expires, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || expires <= time.Now().UnixMilli() || parts[1] != s.setting("security_version", "0") {
		return false
	}
	expected := security.SessionSignature(s.SigningKey, expires, parts[1])
	return hmac.Equal([]byte(expected), []byte(parts[2]))
}
func (s *Server) base(r *http.Request) string {
	if s.Config.PublicURL != "" {
		return s.Config.PublicURL
	}
	return "http://" + r.Host
}
func (s *Server) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		public := r.Method == "GET" && (p == "/api/config" || p == "/api/v1/health" || p == "/api/session" || regexp.MustCompile(`^/api/galleries/[^/]+(?:/qr|/free-download)?$`).MatchString(p) || regexp.MustCompile(`^/api/photos/[^/]+/(?:clean|free)$`).MatchString(p) || strings.HasPrefix(p, "/api/download/")) || r.Method == "POST" && (p == "/api/login" || p == "/api/logout" || p == "/api/checkout" || p == "/api/v1/webhooks/paystack" || p == "/api/guest-uploads/status" || regexp.MustCompile(`^/api/galleries/[^/]+/guest-uploads$`).MatchString(p) || regexp.MustCompile(`^/api/checkout/[^/]+/verify$`).MatchString(p) || regexp.MustCompile(`^/api/photos/[^/]+/download-link$`).MatchString(p))
		if r.Method != "GET" && r.Method != "HEAD" && p != "/api/v1/webhooks/paystack" && r.Header.Get("Origin") != "" {
			origin, e := url.Parse(r.Header.Get("Origin"))
			allowed, e2 := url.Parse(s.base(r))
			if e != nil || e2 != nil || origin.Scheme != allowed.Scheme || origin.Host != allowed.Host {
				fail(w, 403, "Origin not allowed.")
				return
			}
		}
		if !public && !s.owner(r) {
			fail(w, 401, "Please sign in to your studio.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, securityHeaders)
	r.Get("/l/{id}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/?gallery="+url.QueryEscape(chi.URLParam(r, "id")), http.StatusFound)
	})
	r.Get("/media/{id}", s.proof)
	r.Route("/api", func(a chi.Router) {
		a.Use(s.protect)
		a.Get("/v1/health", func(w http.ResponseWriter, r *http.Request) {
			respond(w, 200, map[string]string{"app": "pictide", "status": "ok"})
		})
		a.Get("/config", func(w http.ResponseWriter, r *http.Request) {
			mode := "unavailable"
			if s.Config.PaystackSecret != "" {
				mode = "paystack"
			}
			respond(w, 200, map[string]any{"payments": mode, "public_url": s.Config.PublicURL, "imgbb_previews": s.Config.ImgBBPreviews && s.Config.ImgBBKey != ""})
		})
		a.Get("/session", func(w http.ResponseWriter, r *http.Request) {
			respond(w, 200, map[string]bool{"owner": s.owner(r), "passwordRequired": s.passwordRequired()})
		})
		a.Post("/login", s.login)
		a.Post("/logout", s.logout)
		a.Post("/security/password", s.changePassword)
		a.Post("/galleries/{id}/guest-uploads", s.guestUpload)
		a.Post("/guest-uploads/status", s.guestStatus)
		a.Get("/galleries/{id}/guest-uploads", s.guestUploads)
		a.Get("/guest-uploads/{id}/preview", s.guestPreview)
		a.Post("/guest-uploads/{id}/review", s.reviewGuest)
		a.Delete("/galleries/{id}", s.deleteGallery)
		a.Get("/galleries", s.listGalleries)
		a.Post("/galleries", s.createGallery)
		a.Get("/galleries/{id}", s.getGallery)
		a.Patch("/galleries/{id}", s.updateGallery)
		a.Post("/galleries/{id}/photos", s.uploadPhotos)
		a.Patch("/photos/{id}", s.photoPrice)
		a.Delete("/photos/{id}", s.deletePhoto)
		a.Put("/galleries/{id}/cover", s.cover)
		a.Patch("/galleries/{id}/customization", s.customize)
		a.Put("/galleries/{id}/card-design", s.cardDesign)
		a.Post("/galleries/{id}/regenerate", s.regenerate)
		a.Post("/galleries/{id}/logo", s.saveLogo)
		a.Delete("/galleries/{id}/logo", s.deleteLogo)
		a.Post("/settings/logo", s.saveLogo)
		a.Delete("/settings/logo", s.deleteLogo)
		a.Post("/settings/apply-brand", s.applyBrand)
		a.Get("/galleries/{id}/qr", s.qrImage)
		a.Get("/galleries/{id}/print", s.printCard)
		a.Get("/wallet", s.wallet)
		a.Get("/banks", s.banks)
		a.Get("/banks/resolve", s.resolve)
		a.Post("/withdrawals", s.withdraw)
		a.Post("/withdrawals/{reference}/sync", s.syncWithdrawal)
		a.Get("/orders", s.orders)
		a.Post("/checkout", s.checkout)
		a.Post("/checkout/{id}/verify", s.verifyPayment)
		a.Post("/orders/{id}/sync", s.syncOrder)
		a.Post("/v1/webhooks/paystack", s.webhook)
		a.Post("/photos/{id}/download-link", s.downloadLink)
		a.Get("/download/{id}", s.download)
		a.Get("/photos/{id}/clean", s.clean)
		a.Get("/photos/{id}/free", s.freePhoto)
		a.Get("/galleries/{id}/free-download", s.freeArchive)
		a.Get("/settings", s.getSettings)
		a.Put("/settings", s.putSettings)
	})
	r.Handle("/*", http.FileServer(http.Dir("public")))
	return r
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	s.mu.Lock()
	a := s.attempts[host]
	if time.Now().After(a.until) {
		a = attempt{until: time.Now().Add(10 * time.Minute)}
	}
	if a.count >= 10 {
		s.mu.Unlock()
		fail(w, 429, "Too many attempts. Try again later.")
		return
	}
	a.count++
	s.attempts[host] = a
	s.mu.Unlock()
	var body struct {
		Password string `json:"password"`
	}
	if decode(r, &body) != nil || len(body.Password) > 200 || !s.checkPassword(body.Password) {
		fail(w, 401, "Incorrect password.")
		return
	}
	s.mu.Lock()
	delete(s.attempts, host)
	s.mu.Unlock()
	expiry := time.Now().Add(12 * time.Hour).UnixMilli()
	version := s.setting("security_version", "0")
	http.SetCookie(w, &http.Cookie{Name: "lumen_session", Value: fmt.Sprintf("%d.%s.%s", expiry, version, security.SessionSignature(s.SigningKey, expiry, version)), Path: "/", MaxAge: 43200, HttpOnly: true, Secure: strings.HasPrefix(s.Config.PublicURL, "https://"), SameSite: http.SameSiteStrictMode})
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "lumen_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var b struct{ Current, Password string }
	if decode(r, &b) != nil || len(b.Password) < 12 || len(b.Password) > 200 {
		fail(w, 400, "Use a password between 12 and 200 characters.")
		return
	}
	if s.passwordRequired() && !s.checkPassword(b.Current) {
		fail(w, 403, "Current password is incorrect.")
		return
	}
	hash, err := security.PasswordHash(b.Password)
	if err != nil {
		fail(w, 500, "Unable to update password.")
		return
	}
	version := uuid.NewString()
	tx, err := s.DB.Begin()
	if err == nil {
		_, err = tx.Exec("INSERT INTO settings(key,value) VALUES('password_hash',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", hash)
		if err == nil {
			_, err = tx.Exec("INSERT INTO settings(key,value) VALUES('security_version',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", version)
		}
		if err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
	}
	if err != nil {
		fail(w, 500, "Unable to update password.")
		return
	}
	s.logout(w, r)
}
func (s *Server) gallery(id string) (map[string]any, error) {
	return s.one("SELECT g.*, (SELECT count(*) FROM photos p WHERE p.gallery_id=g.id) photo_count FROM galleries g WHERE id=?", id)
}
func (s *Server) proof(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		http.NotFound(w, r)
		return
	}
	allow := 0
	if s.owner(r) {
		allow = 1
	}
	var publicPreview int
	err := s.DB.QueryRow("SELECT CASE WHEN g.status='Published' AND g.archived=0 AND p.archived=0 THEN 1 ELSE 0 END FROM photos p JOIN galleries g ON g.id=p.gallery_id WHERE p.id=? AND (g.status='Published' OR ?=1)", id, allow).Scan(&publicPreview)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	suffix := ".jpg"
	if r.URL.Query().Get("size") == "thumb" {
		suffix = "_thumb.jpg"
	}
	w.Header().Set("Cache-Control", "no-store")
	file := filepath.Join(s.Config.MediaDir, id+suffix)
	if publicPreview == 1 && s.previewStore != nil && s.serveImgBBPreview(w, r, id, suffix, file) {
		return
	}
	http.ServeFile(w, r, file)
}

var _ = errors.New
