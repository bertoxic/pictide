package httpapi

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"pictide.local/studio/internal/config"
	"pictide.local/studio/internal/security"
	"pictide.local/studio/public"
)

type Server struct {
	db       *mongo.Database
	cfg      config.Config
	r2       *r2Storage
	mu       sync.Mutex
	attempts map[string]attempt
}

type attempt struct {
	count int
	until time.Time
}

type account struct {
	ID           string `bson:"_id"`
	Username     string `bson:"username"`
	PasswordHash string `bson:"password_hash"`
	Role         string `bson:"role"`
	Version      string `bson:"session_version"`
	Disabled     bool   `bson:"disabled"`
	Created      string `bson:"created"`
}

func New(ctx context.Context, db *mongo.Database, cfg config.Config) (*Server, error) {
	if db == nil || len(cfg.SigningKey) < 32 || len(cfg.AdminPassword) < 12 {
		return nil, errors.New("MongoDB, a signing key of at least 32 characters, and an admin password of at least 12 characters are required")
	}
	r2, err := newR2Storage(cfg)
	if err != nil {
		return nil, err
	}
	_, err = db.Collection("accounts").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetUnique(true)})
	if err != nil {
		return nil, err
	}
	_, err = db.Collection("galleries").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)})
	if err != nil {
		return nil, err
	}
	_, err = db.Collection("photos").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "studio_id", Value: 1}, {Key: "gallery_id", Value: 1}, {Key: "hash", Value: 1}}, Options: options.Index().SetUnique(true)},
	})
	if err != nil {
		return nil, err
	}
	_, err = db.Collection("orders").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)})
	if err != nil {
		return nil, err
	}
	if r2 != nil {
		_, err = db.Collection("direct_uploads").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)})
		if err != nil {
			return nil, err
		}
	}
	hash, err := security.PasswordHash(cfg.AdminPassword)
	if err != nil {
		return nil, err
	}
	version, err := security.Token()
	if err != nil {
		return nil, err
	}
	_, err = db.Collection("accounts").UpdateOne(ctx, bson.M{"_id": "legacy"}, bson.M{"$setOnInsert": bson.M{"username": "admin", "password_hash": hash, "role": "admin", "session_version": version, "disabled": false, "created": timestamp()}}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return &Server{db: db, cfg: cfg, r2: r2, attempts: map[string]attempt{}}, nil
}

func timestamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int, msg string) {
	reply(w, code, map[string]string{"error": msg})
}
func decode(r *http.Request, value any) error {
	defer r.Body.Close()
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 2<<20)).Decode(value)
}

func (s *Server) session(r *http.Request) *account {
	cookie, err := r.Cookie("lumen_platform")
	if err != nil {
		return nil
	}
	p := strings.Split(cookie.Value, ".")
	if len(p) != 4 {
		return nil
	}
	exp, err := strconv.ParseInt(p[1], 10, 64)
	if err != nil || exp <= time.Now().UnixMilli() {
		return nil
	}
	expected := security.SessionSignature(s.cfg.SigningKey, exp, p[0]+"."+p[2])
	if !hmac.Equal([]byte(expected), []byte(p[3])) {
		return nil
	}
	var a account
	if s.db.Collection("accounts").FindOne(r.Context(), bson.M{"_id": p[0], "session_version": p[2], "disabled": false}).Decode(&a) != nil {
		return nil
	}
	return &a
}

func (s *Server) cookie(w http.ResponseWriter, a *account) {
	expires := time.Now().Add(12 * time.Hour).UnixMilli()
	sig := security.SessionSignature(s.cfg.SigningKey, expires, a.ID+"."+a.Version)
	http.SetCookie(w, &http.Cookie{Name: "lumen_platform", Value: a.ID + "." + strconv.FormatInt(expires, 10) + "." + a.Version + "." + sig, Path: "/", MaxAge: 43200, HttpOnly: true, Secure: s.secure(), SameSite: http.SameSiteStrictMode})
}

func (s *Server) secure() bool {
	return strings.HasPrefix(s.cfg.PublicURL, "https://") || os.Getenv("VERCEL") == "1"
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
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
	var input struct{ Username, Password string }
	if decode(r, &input) != nil || len(input.Password) > 200 {
		fail(w, 401, "Incorrect username or password.")
		return
	}
	username := strings.ToLower(strings.TrimSpace(input.Username))
	if username == "" {
		username = "admin"
	}
	var user account
	if s.db.Collection("accounts").FindOne(r.Context(), bson.M{"username": username, "disabled": false}).Decode(&user) != nil || !security.CheckPassword(user.PasswordHash, input.Password) {
		fail(w, 401, "Incorrect username or password.")
		return
	}
	s.mu.Lock()
	delete(s.attempts, host)
	s.mu.Unlock()
	s.cookie(w, &user)
	reply(w, 200, map[string]bool{"ok": true})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != "GET" && r.Method != "HEAD" && r.URL.Path != "/api/v1/webhooks/paystack" && r.Header.Get("Origin") != "" {
		expected := s.cfg.PublicURL
		if expected == "" {
			scheme := "http://"
			if s.secure() {
				scheme = "https://"
			}
			expected = scheme + r.Host
		}
		if r.Header.Get("Origin") != expected {
			fail(w, 403, "Origin not allowed.")
			return
		}
	}
	p := r.URL.Path
	if p == "/api/config" && r.Method == "GET" {
		mode := "unavailable"
		if s.cfg.PaystackSecret != "" {
			mode = "paystack"
		}
		reply(w, 200, map[string]any{"payments": mode, "public_url": s.cfg.PublicURL, "email_configured": false, "imgbb_previews": false, "r2_uploads": s.r2 != nil})
		return
	}
	if p == "/api/v1/health" && r.Method == "GET" {
		reply(w, 200, map[string]string{"app": "pictide", "status": "partial"})
		return
	}
	if p == "/api/login" && r.Method == "POST" {
		s.login(w, r)
		return
	}
	if p == "/api/logout" && r.Method == "POST" {
		http.SetCookie(w, &http.Cookie{Name: "lumen_platform", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secure(), SameSite: http.SameSiteStrictMode})
		reply(w, 200, map[string]bool{"ok": true})
		return
	}
	user := s.session(r)
	if p == "/api/checkout" && r.Method == "POST" {
		s.checkout(w, r)
		return
	}
	if p == "/api/v1/webhooks/paystack" && r.Method == "POST" {
		s.webhook(w, r)
		return
	}
	if strings.HasPrefix(p, "/api/checkout/") && strings.HasSuffix(p, "/verify") && r.Method == "POST" {
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/api/checkout/"), "/verify")
		if id != "" && !strings.Contains(id, "/") {
			s.verifyPayment(w, r, id)
			return
		}
	}
	if strings.HasPrefix(p, "/api/photos/") && r.Method == "POST" && strings.HasSuffix(p, "/download-link") {
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/api/photos/"), "/download-link")
		if id != "" && !strings.Contains(id, "/") {
			s.downloadLink(w, r, id)
			return
		}
	}
	if strings.HasPrefix(p, "/api/download/") && r.Method == "GET" {
		id := strings.TrimPrefix(p, "/api/download/")
		if id != "" && !strings.Contains(id, "/") {
			s.download(w, r, id)
			return
		}
	}
	if strings.HasPrefix(p, "/api/photos/") && r.Method == "GET" {
		parts := strings.Split(strings.TrimPrefix(p, "/api/photos/"), "/")
		if len(parts) == 2 && (parts[1] == "free" || parts[1] == "clean") {
			s.photoDownload(w, r, parts[0], parts[1])
			return
		}
	}
	if strings.HasPrefix(p, "/media/") && r.Method == "GET" {
		s.servePreview(w, r, user, strings.TrimPrefix(p, "/media/"))
		return
	}
	if strings.HasPrefix(p, "/l/") && r.Method == "GET" {
		id := strings.TrimPrefix(p, "/l/")
		if id != "" && !strings.Contains(id, "/") {
			http.Redirect(w, r, "/?gallery="+id, http.StatusFound)
			return
		}
	}
	if strings.HasPrefix(p, "/api/galleries/") && r.Method == "GET" {
		parts := strings.Split(strings.TrimPrefix(p, "/api/galleries/"), "/")
		if len(parts) == 1 && parts[0] != "" {
			if user != nil {
				n, err := s.db.Collection("galleries").CountDocuments(r.Context(), bson.M{"studio_id": user.ID, "id": parts[0]})
				if err != nil {
					fail(w, 500, "Unable to load gallery.")
					return
				}
				if n > 0 {
					s.getGallery(w, r, user, parts[0])
					return
				}
			}
			s.publicGallery(w, r, parts[0])
			return
		}
		if len(parts) == 2 && parts[0] != "" && parts[1] == "free-download" {
			s.freeArchive(w, r, parts[0])
			return
		}
		if len(parts) == 2 && parts[0] != "" && parts[1] == "qr" {
			if user != nil {
				n, err := s.db.Collection("galleries").CountDocuments(r.Context(), bson.M{"studio_id": user.ID, "id": parts[0]})
				if err != nil {
					fail(w, 500, "Unable to load gallery.")
					return
				}
				if n > 0 {
					s.galleryQR(w, r, user, parts[0])
					return
				}
			}
			var gallery struct {
				StudioID string `bson:"studio_id"`
			}
			if s.db.Collection("galleries").FindOne(r.Context(), bson.M{"id": parts[0], "status": "Published", "archived": bson.M{"$ne": 1}}).Decode(&gallery) != nil {
				http.NotFound(w, r)
				return
			}
			s.galleryQR(w, r, &account{ID: gallery.StudioID}, parts[0])
			return
		}
	}
	if p == "/api/session" && r.Method == "GET" {
		if user == nil {
			reply(w, 200, map[string]any{"owner": false, "passwordRequired": true})
		} else {
			reply(w, 200, map[string]any{"owner": true, "passwordRequired": true, "username": user.Username, "role": user.Role})
		}
		return
	}
	if strings.HasPrefix(p, "/api/") {
		if user == nil {
			fail(w, 401, "Please sign in to your studio.")
			return
		}
		if s.api(w, r, user) {
			return
		}
		fail(w, 503, "This feature is still being converted to MongoDB.")
		return
	}
	if p == "/" || (!strings.Contains(p, "..") && !strings.HasPrefix(p, "/media/")) {
		http.FileServer(http.FS(public.Files)).ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}
