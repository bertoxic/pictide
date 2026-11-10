package platform

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"pictide.local/studio/internal/config"
	"pictide.local/studio/internal/database"
	"pictide.local/studio/internal/httpapi"
	"pictide.local/studio/internal/security"
)

func TestRegistrationRecoveryAndAdmin(t *testing.T) {
	dir := t.TempDir()
	c := config.Config{DatabasePath: filepath.Join(dir, "studio.db"), MediaDir: filepath.Join(dir, "media"), SigningKey: "01234567890123456789012345678901", AdminPassword: "admin-password-123", PaystackSecret: "test", PublicURL: "https://example.test"}
	db, e := database.Open(c.DatabasePath)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	legacy, e := httpapi.New(db, c)
	if e != nil {
		t.Fatal(e)
	}
	p, e := New(c, db, legacy)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	var lastMail string
	p.sendMail = func(to, subject, body string) error { lastMail = body; return nil }
	server := httptest.NewServer(p)
	defer server.Close()
	req := func(method, path string, v any, cookie *http.Cookie) *http.Response {
		var body io.Reader
		if v != nil {
			b, _ := json.Marshal(v)
			body = bytes.NewReader(b)
		}
		r, _ := http.NewRequest(method, server.URL+path, body)
		if v != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		res, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		return res
	}
	check := func(r *http.Response, want int) {
		t.Helper()
		defer r.Body.Close()
		if r.StatusCode != want {
			body, _ := io.ReadAll(r.Body)
			t.Fatalf("status %d, expected %d: %s", r.StatusCode, want, body)
		}
	}
	login := func(user, pass string, want int) *http.Cookie {
		r := req("POST", "/api/login", map[string]string{"username": user, "password": pass}, nil)
		defer r.Body.Close()
		if r.StatusCode != want {
			t.Fatalf("login: %s", r.Status)
		}
		if want == 200 {
			return r.Cookies()[0]
		}
		return nil
	}
	token := func(kind string) string {
		t.Helper()
		mark := "#" + kind + "="
		i := strings.Index(lastMail, mark)
		if i < 0 {
			t.Fatalf("missing %s link", kind)
		}
		return lastMail[i+len(mark) : i+len(mark)+64]
	}
	mailer := p.sendMail
	p.sendMail = nil
	check(req("POST", "/api/register", map[string]string{"username": "newperson", "email": "new@example.test", "password": "long-password-123"}, nil), 503)
	p.sendMail = mailer
	check(req("POST", "/api/register", map[string]string{"username": "alice", "email": "alice@example.test", "password": "long-password-123"}, nil), 201)
	verify := token("verify")
	login("alice", "long-password-123", 401)
	check(req("POST", "/api/account/verify", map[string]string{"token": verify}, nil), 200)
	check(req("POST", "/api/account/verify", map[string]string{"token": verify}, nil), 400)
	alice := login("alice", "long-password-123", 200)
	check(req("GET", "/api/admin/overview", nil, alice), 403)
	check(req("POST", "/api/account/forgot", map[string]string{"email": "alice@example.test"}, nil), 200)
	reset := token("reset")
	check(req("POST", "/api/account/reset", map[string]string{"token": reset, "password": "new-password-123"}, nil), 200)
	check(req("GET", "/api/orders", nil, alice), 401)
	check(req("POST", "/api/account/reset", map[string]string{"token": reset, "password": "other-password-123"}, nil), 400)
	login("alice", "long-password-123", 401)
	alice = login("alice", "new-password-123", 200)
	check(req("POST", "/api/account/forgot", map[string]string{"email": "alice@example.test"}, nil), 200)
	expired := token("reset")
	if _, e := p.db.Exec("UPDATE account_links SET expires=0 WHERE token_hash=?", security.HashToken(expired)); e != nil {
		t.Fatal(e)
	}
	check(req("POST", "/api/account/reset", map[string]string{"token": expired, "password": "other-password-123"}, nil), 400)
	admin := login("admin", "admin-password-123", 200)
	check(req("POST", "/api/account/email", map[string]string{"email": "admin@example.test", "password": "admin-password-123"}, admin), 200)
	check(req("POST", "/api/account/verify", map[string]string{"token": token("verify")}, nil), 200)
	check(req("GET", "/api/admin/activity?account="+p.studiosByName("alice").id, nil, alice), 403)
	check(req("GET", "/api/admin/activity?account="+p.studiosByName("alice").id, nil, admin), 200)

	check(req("GET", "/api/admin/overview", nil, admin), 200)
	a := p.studiosByName("alice")
	check(req("POST", "/api/admin/account-status", map[string]any{"id": a.id, "disabled": true}, alice), 403)
	check(req("POST", "/api/admin/account-status", map[string]any{"id": a.id, "disabled": true}, admin), 200)
	check(req("GET", "/api/orders", nil, alice), 401)
	login("alice", "new-password-123", 401)
	check(req("POST", "/api/admin/account-status", map[string]any{"id": a.id, "disabled": false}, admin), 200)
	login("alice", "new-password-123", 200)
}
