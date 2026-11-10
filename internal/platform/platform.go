package platform

import (
	"bytes"
	"crypto/hmac"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"pictide.local/studio/internal/config"
	"pictide.local/studio/internal/database"
	"pictide.local/studio/internal/httpapi"
	"pictide.local/studio/internal/security"
)

type studio struct {
	id, username, role, version string
	db                          *sql.DB
	api                         *httpapi.Server
	handler                     http.Handler
}
type Platform struct {
	cfg      config.Config
	db       *sql.DB
	mu       sync.RWMutex
	studios  map[string]*studio
	attempts map[string]attempt
	routeMu  sync.RWMutex
	routes   map[string]*studio
	sendMail func(string, string, string) error
}
type attempt struct {
	count int
	until time.Time
}

func New(c config.Config, legacy *sql.DB, legacyAPI *httpapi.Server) (*Platform, error) {
	path := filepath.Join(filepath.Dir(c.DatabasePath), "platform.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS accounts(id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE COLLATE NOCASE, password_hash TEXT NOT NULL, role TEXT NOT NULL, session_version TEXT NOT NULL, created TEXT NOT NULL)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	for _, col := range []struct{ name, typ string }{{"email", "TEXT"}, {"email_verified", "INTEGER NOT NULL DEFAULT 0"}, {"disabled", "INTEGER NOT NULL DEFAULT 0"}} {
		rows, e := db.Query("PRAGMA table_info(accounts)")
		if e != nil {
			return nil, e
		}
		found := false
		for rows.Next() {
			var cid, notnull, pk int
			var name, typ string
			var def any
			if e = rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); e != nil {
				rows.Close()
				return nil, e
			}
			if name == col.name {
				found = true
			}
		}
		rows.Close()
		if !found {
			if _, e = db.Exec("ALTER TABLE accounts ADD COLUMN " + col.name + " " + col.typ); e != nil {
				return nil, e
			}
		}
	}
	if _, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS accounts_email ON accounts(email COLLATE NOCASE) WHERE email IS NOT NULL; CREATE TABLE IF NOT EXISTS account_links(token_hash TEXT PRIMARY KEY, account_id TEXT NOT NULL, email TEXT NOT NULL, kind TEXT NOT NULL, expires INTEGER NOT NULL); CREATE INDEX IF NOT EXISTS account_links_account ON account_links(account_id,kind); CREATE TABLE IF NOT EXISTS admin_audit(id TEXT PRIMARY KEY, actor TEXT NOT NULL, action TEXT NOT NULL, target TEXT NOT NULL, created TEXT NOT NULL)`); err != nil {
		return nil, err
	}
	var n int
	if err = db.QueryRow("SELECT count(*) FROM accounts").Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		var hash string
		_ = legacy.QueryRow("SELECT value FROM settings WHERE key='password_hash'").Scan(&hash)
		if hash == "" {
			hash, err = security.PasswordHash(c.AdminPassword)
			if err != nil {
				return nil, err
			}
		}
		_, err = db.Exec("INSERT INTO accounts(id,username,password_hash,role,session_version,created) VALUES(?,?,?,?,?,?)", "legacy", "admin", hash, "admin", uuid.NewString(), time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return nil, err
		}
	}
	p := &Platform{cfg: c, db: db, studios: map[string]*studio{}, attempts: map[string]attempt{}, routes: map[string]*studio{}}
	if c.SMTPHost != "" && c.SMTPFrom != "" && c.PublicURL != "" {
		p.sendMail = p.smtpMail
	}
	rows, err := db.Query("SELECT id,username,role,session_version FROM accounts")
	if err != nil {
		return nil, err
	}
	var accounts []studio
	for rows.Next() {
		var a studio
		if err = rows.Scan(&a.id, &a.username, &a.role, &a.version); err != nil {
			rows.Close()
			return nil, err
		}
		accounts = append(accounts, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		if a.id == "legacy" {
			a.db = legacy
			a.api = legacyAPI
		} else {
			a.db, err = database.Open(filepath.Join(filepath.Dir(c.DatabasePath), "studios", a.id, "studio.db"))
			if err != nil {
				return nil, err
			}
			sc := c
			sc.MediaDir = filepath.Join(filepath.Dir(c.DatabasePath), "studios", a.id, "media")
			sc.R2Prefix = "studios/" + a.id
			a.api, err = httpapi.New(a.db, sc)
			if err != nil {
				return nil, err
			}
		}
		a.handler = a.api.Router()
		p.studios[a.id] = &a
	}
	return p, nil
}
func (p *Platform) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, s := range p.studios {
		if id != "legacy" {
			s.db.Close()
		}
	}
	p.db.Close()
}
func nowUTC() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	reply(w, status, map[string]string{"error": msg})
}
func (p *Platform) session(r *http.Request) *studio {
	c, e := r.Cookie("lumen_platform")
	if e != nil {
		return nil
	}
	v := strings.Split(c.Value, ".")
	if len(v) != 4 {
		return nil
	}
	expires, e := strconv.ParseInt(v[1], 10, 64)
	if e != nil || expires <= time.Now().UnixMilli() {
		return nil
	}
	p.mu.RLock()
	s := p.studios[v[0]]
	if s == nil || s.version != v[2] {
		p.mu.RUnlock()
		return nil
	}
	sig := security.SessionSignature(p.cfg.SigningKey, expires, s.id+"."+s.version)
	ok := hmac.Equal([]byte(sig), []byte(v[3]))
	p.mu.RUnlock()
	if !ok {
		return nil
	}
	return s
}
func (p *Platform) setSession(w http.ResponseWriter, s *studio, version string) {
	expires := time.Now().Add(12 * time.Hour).UnixMilli()
	sig := security.SessionSignature(p.cfg.SigningKey, expires, s.id+"."+version)
	http.SetCookie(w, &http.Cookie{Name: "lumen_platform", Value: fmt.Sprintf("%s.%d.%s.%s", s.id, expires, version, sig), Path: "/", MaxAge: 43200, HttpOnly: true, Secure: strings.HasPrefix(p.cfg.PublicURL, "https://"), SameSite: http.SameSiteStrictMode})
}
func (p *Platform) login(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	p.mu.Lock()
	a := p.attempts[host]
	if time.Now().After(a.until) {
		a = attempt{until: time.Now().Add(10 * time.Minute)}
	}
	if a.count >= 10 {
		p.mu.Unlock()
		fail(w, 429, "Too many attempts. Try again later.")
		return
	}
	a.count++
	p.attempts[host] = a
	p.mu.Unlock()
	var b struct{ Username, Password string }
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b) != nil || len(b.Password) > 200 {
		fail(w, 401, "Incorrect username or password.")
		return
	}
	if strings.TrimSpace(b.Username) == "" {
		b.Username = "admin"
	}
	var id, hash, loginVersion string
	var verified, disabled int
	err := p.db.QueryRow("SELECT id,password_hash,CASE WHEN email IS NULL THEN 1 ELSE email_verified END,disabled,session_version FROM accounts WHERE username=?", strings.TrimSpace(b.Username)).Scan(&id, &hash, &verified, &disabled, &loginVersion)
	if err != nil || !security.CheckPassword(hash, b.Password) || verified != 1 || disabled != 0 {
		fail(w, 401, "Incorrect username or password.")
		return
	}
	p.mu.Lock()
	delete(p.attempts, host)
	s := p.studios[id]
	p.mu.Unlock()
	if s == nil {
		fail(w, 500, "Account unavailable.")
		return
	}
	p.setSession(w, s, loginVersion)
	reply(w, 200, map[string]bool{"ok": true})
}
func (p *Platform) password(w http.ResponseWriter, r *http.Request, s *studio) {
	var b struct{ Current, Password string }
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b) != nil || len(b.Password) < 12 || len(b.Password) > 200 {
		fail(w, 400, "Use a password between 12 and 200 characters.")
		return
	}
	var hash string
	if p.db.QueryRow("SELECT password_hash FROM accounts WHERE id=?", s.id).Scan(&hash) != nil || !security.CheckPassword(hash, b.Current) {
		fail(w, 403, "Current password is incorrect.")
		return
	}
	oldHash := hash
	hash, e := security.PasswordHash(b.Password)
	if e != nil {
		fail(w, 500, "Unable to update password.")
		return
	}
	version := uuid.NewString()
	p.mu.Lock()
	defer p.mu.Unlock()
	tx, e := p.db.Begin()
	if e != nil {
		fail(w, 500, "Unable to update password.")
		return
	}
	defer tx.Rollback()
	result, e := tx.Exec("UPDATE accounts SET password_hash=?,session_version=? WHERE id=? AND password_hash=? AND disabled=0", hash, version, s.id, oldHash)
	if e == nil {
		n, _ := result.RowsAffected()
		if n != 1 {
			e = fmt.Errorf("credentials changed")
		}
	}
	if e == nil {
		_, e = tx.Exec("DELETE FROM account_links WHERE account_id=?", s.id)
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		fail(w, 500, "Unable to update password.")
		return
	}
	s.version = version
	http.SetCookie(w, &http.Cookie{Name: "lumen_platform", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: strings.HasPrefix(p.cfg.PublicURL, "https://"), SameSite: http.SameSiteStrictMode})
	reply(w, 200, map[string]bool{"ok": true})
}
func (p *Platform) accounts(w http.ResponseWriter, r *http.Request, s *studio) {
	if s == nil || s.role != "admin" {
		fail(w, 403, "Administrator access required.")
		return
	}
	if r.Method == "GET" {
		rows, e := p.db.Query("SELECT id,username,role,created FROM accounts ORDER BY created")
		if e != nil {
			fail(w, 500, "Unable to load accounts.")
			return
		}
		defer rows.Close()
		list := []map[string]string{}
		for rows.Next() {
			var id, u, role, created string
			if rows.Scan(&id, &u, &role, &created) != nil {
				fail(w, 500, "Unable to load accounts.")
				return
			}
			list = append(list, map[string]string{"id": id, "username": u, "role": role, "created": created})
		}
		reply(w, 200, list)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var b struct{ Username, Password string }
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b) != nil {
		fail(w, 400, "Invalid account.")
		return
	}
	b.Username = strings.ToLower(strings.TrimSpace(b.Username))
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`).MatchString(b.Username) || len(b.Password) < 12 || len(b.Password) > 200 {
		fail(w, 400, "Use a username of 3–64 letters, numbers, dots, dashes or underscores, and a password of at least 12 characters.")
		return
	}
	account, e := p.createStudio(b.Username, b.Password, "")
	if e != nil {
		fail(w, 409, "Could not create account; username may already be in use.")
		return
	}
	_, _ = p.db.Exec("INSERT INTO admin_audit VALUES(?,?,?,?,?)", uuid.NewString(), s.username, "account created", account.username, nowUTC())
	reply(w, 201, map[string]string{"id": account.id, "username": account.username, "role": "photographer"})
}
func (p *Platform) createStudio(username, password, email string) (*studio, error) {
	var existing int
	if e := p.db.QueryRow("SELECT count(*) FROM accounts WHERE username=? COLLATE NOCASE OR (? <> '' AND email=? COLLATE NOCASE)", username, email, email).Scan(&existing); e != nil {
		return nil, e
	}
	if existing != 0 {
		return nil, fmt.Errorf("account already exists")
	}
	hash, e := security.PasswordHash(password)
	if e != nil {
		return nil, e
	}
	id := uuid.NewString()
	dir := filepath.Join(filepath.Dir(p.cfg.DatabasePath), "studios", id)
	db, e := database.Open(filepath.Join(dir, "studio.db"))
	if e != nil {
		return nil, e
	}
	sc := p.cfg
	sc.MediaDir = filepath.Join(dir, "media")
	sc.R2Prefix = "studios/" + id
	api, e := httpapi.New(db, sc)
	if e != nil {
		db.Close()
		return nil, e
	}
	version := uuid.NewString()
	var emailValue any
	if email != "" {
		emailValue = email
	}
	_, e = p.db.Exec("INSERT INTO accounts(id,username,password_hash,role,session_version,created,email,email_verified) VALUES(?,?,?,?,?,?,?,0)", id, username, hash, "photographer", version, time.Now().UTC().Format(time.RFC3339Nano), emailValue)
	if e != nil {
		db.Close()
		return nil, e
	}
	account := &studio{id: id, username: username, role: "photographer", version: version, db: db, api: api, handler: api.Router()}
	p.mu.Lock()
	p.studios[id] = account
	p.mu.Unlock()
	return account, nil
}

func (p *Platform) lookup(query string, arg string) *studio {
	key := query + "\x00" + arg
	p.routeMu.RLock()
	cached := p.routes[key]
	p.routeMu.RUnlock()
	if cached != nil {
		var n int
		if cached.db.QueryRow(query, arg).Scan(&n) == nil {
			return cached
		}
		p.routeMu.Lock()
		delete(p.routes, key)
		p.routeMu.Unlock()
	}
	p.mu.RLock()
	studios := make([]*studio, 0, len(p.studios))
	for _, s := range p.studios {
		studios = append(studios, s)
	}
	p.mu.RUnlock()
	for _, s := range studios {
		var n int
		if s.db.QueryRow(query, arg).Scan(&n) == nil {
			p.routeMu.Lock()
			if len(p.routes) >= 8192 {
				for k := range p.routes {
					delete(p.routes, k)
					break
				}
			}
			p.routes[key] = s
			p.routeMu.Unlock()
			return s
		}
	}
	return nil
}

func (p *Platform) forward(w http.ResponseWriter, r *http.Request, target, viewer *studio) {
	req := r.Clone(r.Context())
	req.Header = r.Header.Clone()
	req.Header.Del("Cookie")
	for _, c := range r.Cookies() {
		if c.Name != "lumen_session" && c.Name != "lumen_platform" {
			req.AddCookie(c)
		}
	}
	if viewer != nil && viewer == target {
		var version string
		if target.db.QueryRow("SELECT value FROM settings WHERE key='security_version'").Scan(&version) != nil {
			version = "0"
		}
		expires := time.Now().Add(time.Minute).UnixMilli()
		sig := security.SessionSignature(p.cfg.SigningKey, expires, version)
		req.AddCookie(&http.Cookie{Name: "lumen_session", Value: fmt.Sprintf("%d.%s.%s", expires, version, sig)})
	}
	target.handler.ServeHTTP(w, req)
}
func (p *Platform) findGallery(id string) *studio {
	return p.lookup("SELECT 1 FROM galleries WHERE id=?", id)
}
func (p *Platform) findPhoto(id string) *studio {
	return p.lookup("SELECT 1 FROM photos WHERE id=?", id)
}
func (p *Platform) findOrder(id string) *studio {
	return p.lookup("SELECT 1 FROM orders WHERE id=?", id)
}
func (p *Platform) findTransfer(id string) *studio {
	return p.lookup("SELECT 1 FROM withdrawals WHERE reference=?", id)
}
func capture(r *http.Request, max int64) ([]byte, error) {
	data, e := io.ReadAll(io.LimitReader(r.Body, max+1))
	if e != nil || int64(len(data)) > max {
		return nil, errors.New("request too large")
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	return data, nil
}

var galleryRoute = regexp.MustCompile(`^/api/galleries/([^/]+)(?:/(qr|free-download|guest-uploads))?$`)
var photoRoute = regexp.MustCompile(`^/api/photos/([^/]+)/(clean|free|download-link)$`)
var verifyRoute = regexp.MustCompile(`^/api/checkout/([^/]+)/verify$`)
var mediaRoute = regexp.MustCompile(`^/(?:media|api/download)/([^/]+)$`)

func (p *Platform) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	path := r.URL.Path
	viewer := p.session(r)
	if strings.HasPrefix(path, "/api/") && r.Method != "GET" && r.Method != "HEAD" && path != "/api/v1/webhooks/paystack" && r.Header.Get("Origin") != "" {
		expected := p.cfg.PublicURL
		if expected == "" {
			expected = "http://" + r.Host
		}
		if r.Header.Get("Origin") != expected {
			fail(w, 403, "Origin not allowed.")
			return
		}
	}
	switch path {
	case "/api/config":
		mode := "unavailable"
		if p.cfg.PaystackSecret != "" {
			mode = "paystack"
		}
		reply(w, 200, map[string]any{"payments": mode, "public_url": p.cfg.PublicURL, "email_configured": p.sendMail != nil, "imgbb_previews": p.cfg.ImgBBPreviews && p.cfg.ImgBBKey != ""})
		return
	case "/api/register":
		p.register(w, r)
		return
	case "/api/account/verify":
		p.consumeLink(w, r, "verify")
		return
	case "/api/account/forgot":
		p.forgot(w, r)
		return
	case "/api/account/reset":
		p.consumeLink(w, r, "reset")
		return
	case "/api/account/email":
		p.bindEmail(w, r, viewer)
		return
	case "/api/admin/sync":
		p.adminSync(w, r, viewer)
		return
	case "/api/admin/insights":
		p.adminInsights(w, r, viewer)
		return
	case "/api/admin/activity":
		p.adminActivity(w, r, viewer)
		return
	case "/api/admin/overview":
		p.adminOverview(w, r, viewer)
		return
	case "/api/admin/account-status":
		p.adminStatus(w, r, viewer)
		return
	case "/api/login":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		p.login(w, r)
		return
	case "/api/logout":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "lumen_platform", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: strings.HasPrefix(p.cfg.PublicURL, "https://"), SameSite: http.SameSiteStrictMode})
		reply(w, 200, map[string]bool{"ok": true})
		return
	case "/api/session":
		if viewer == nil {
			reply(w, 200, map[string]any{"owner": false, "passwordRequired": true})
			return
		}
		reply(w, 200, map[string]any{"owner": true, "passwordRequired": true, "username": viewer.username, "role": viewer.role})
		return
	case "/api/security/password":
		if viewer == nil {
			fail(w, 401, "Please sign in.")
			return
		}
		p.password(w, r, viewer)
		return
	case "/api/admin/accounts":
		p.accounts(w, r, viewer)
		return
	case "/api/v1/webhooks/paystack":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		body, e := capture(r, 2<<20)
		if e != nil {
			w.WriteHeader(400)
			return
		}
		var event struct {
			Data struct {
				Reference string `json:"reference"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &event) != nil {
			w.WriteHeader(400)
			return
		}
		p.mu.RLock()
		legacy := p.studios["legacy"]
		p.mu.RUnlock()
		if !legacy.api.Paystack.VerifySignature(body, r.Header.Get("X-Paystack-Signature")) {
			w.WriteHeader(401)
			return
		}
		target := p.findOrder(event.Data.Reference)
		if target == nil {
			target = p.findTransfer(event.Data.Reference)
		}
		if target == nil {
			w.WriteHeader(200)
			return
		}
		p.forward(w, r, target, nil)
		return
	}
	var target *studio
	public := false
	if m := galleryRoute.FindStringSubmatch(path); m != nil && ((r.Method == "GET" && (m[2] == "" || m[2] == "qr" || m[2] == "free-download")) || (r.Method == "POST" && m[2] == "guest-uploads")) {
		public = true
		target = p.findGallery(m[1])
	}
	if m := photoRoute.FindStringSubmatch(path); m != nil && ((r.Method == "GET" && (m[2] == "clean" || m[2] == "free")) || (r.Method == "POST" && m[2] == "download-link")) {
		public = true
		target = p.findPhoto(m[1])
	}
	if m := verifyRoute.FindStringSubmatch(path); m != nil && r.Method == "POST" {
		public = true
		target = p.findOrder(m[1])
	}
	if m := mediaRoute.FindStringSubmatch(path); m != nil && r.Method == "GET" {
		public = true
		target = p.findPhoto(m[1])
	}
	if path == "/api/checkout" && r.Method == "POST" {
		public = true
		body, e := capture(r, 2<<20)
		if e != nil {
			fail(w, 400, "Invalid request.")
			return
		}
		var b struct {
			GalleryID string `json:"galleryId"`
		}
		if json.Unmarshal(body, &b) == nil {
			target = p.findGallery(b.GalleryID)
		}
	}
	if path == "/api/guest-uploads/status" && r.Method == "POST" {
		public = true
		body, e := capture(r, 2<<20)
		if e != nil {
			fail(w, 400, "Invalid request.")
			return
		}
		var b struct{ Tokens []string }
		if json.Unmarshal(body, &b) == nil && len(b.Tokens) > 0 {
			target = p.lookup("SELECT 1 FROM guest_uploads WHERE token_hash=?", security.HashToken(b.Tokens[0]))
		}
		if target == nil {
			reply(w, 200, []any{})
			return
		}
	}
	if public {
		if target == nil {
			http.NotFound(w, r)
			return
		}
		p.forward(w, r, target, viewer)
		return
	}
	if strings.HasPrefix(path, "/api/") && path != "/api/config" && path != "/api/v1/health" {
		if viewer == nil {
			fail(w, 401, "Please sign in to your studio.")
			return
		}
		p.forward(w, r, viewer, viewer)
		return
	}
	p.mu.RLock()
	legacy := p.studios["legacy"]
	p.mu.RUnlock()
	p.forward(w, r, legacy, nil)
}
