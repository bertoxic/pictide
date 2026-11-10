package platform

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"pictide.local/studio/internal/security"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"pictide.local/studio/internal/config"
	"pictide.local/studio/internal/database"
	"pictide.local/studio/internal/httpapi"
)

func TestAccountIsolationAndMigration(t *testing.T) {
	dir := t.TempDir()
	c := config.Config{DatabasePath: filepath.Join(dir, "lumen.db"), MediaDir: filepath.Join(dir, "media"), SigningKey: "01234567890123456789012345678901", AdminPassword: "initial-password", PaystackSecret: "test", PublicURL: "https://example.test"}
	legacy, e := database.Open(c.DatabasePath)
	if e != nil {
		t.Fatal(e)
	}
	defer legacy.Close()
	api, e := httpapi.New(legacy, c)
	if e != nil {
		t.Fatal(e)
	}
	_, e = legacy.Exec("INSERT INTO galleries(id,name,created,status) VALUES('original','Original','2026-01-01T00:00:00Z','Draft')")
	if e != nil {
		t.Fatal(e)
	}
	p, e := New(c, legacy, api)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	srv := httptest.NewServer(p)
	defer srv.Close()
	req := func(method, path string, body any, cookie *http.Cookie) *http.Response {
		t.Helper()
		var in io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			in = bytes.NewReader(b)
		}
		r, _ := http.NewRequest(method, srv.URL+path, in)
		if body != nil {
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
	check := func(r *http.Response, status int) {
		t.Helper()
		defer r.Body.Close()
		if r.StatusCode != status {
			b, _ := io.ReadAll(r.Body)
			t.Fatalf("got %d want %d %s", r.StatusCode, status, b)
		}
	}
	login := func(name, password string) *http.Cookie {
		t.Helper()
		r := req("POST", "/api/login", map[string]string{"username": name, "password": password}, nil)
		if r.StatusCode != 200 {
			t.Fatal("login failed", r.Status)
		}
		defer r.Body.Close()
		return r.Cookies()[0]
	}
	admin := login("admin", "initial-password")
	r := req("GET", "/api/galleries", nil, admin)
	var old []map[string]any
	json.NewDecoder(r.Body).Decode(&old)
	r.Body.Close()
	if len(old) != 1 || old[0]["name"] != "Original" {
		t.Fatal("existing studio not migrated", old)
	}
	for _, name := range []string{"alice", "bob"} {
		check(req("POST", "/api/admin/accounts", map[string]string{"username": name, "password": "long-password-123"}, admin), 201)
	}
	alice := login("alice", "long-password-123")
	bob := login("bob", "long-password-123")
	check(req("GET", "/api/admin/accounts", nil, alice), 403)
	r = req("POST", "/api/galleries", map[string]any{"name": "Alice private", "price": 100}, alice)
	var gallery map[string]any
	json.NewDecoder(r.Body).Decode(&gallery)
	r.Body.Close()
	id := gallery["id"].(string)
	check(req("GET", "/api/galleries/"+id, nil, bob), 404)
	check(req("PATCH", "/api/galleries/"+id, map[string]any{"name": "stolen"}, bob), 404)
	r = req("GET", "/api/galleries", nil, bob)
	var list []any
	json.NewDecoder(r.Body).Decode(&list)
	r.Body.Close()
	if len(list) != 0 {
		t.Fatal("gallery leaked", list)
	}
	// A forged legacy studio cookie is never forwarded to a different tenant.
	expiry := time.Now().Add(time.Hour).UnixMilli()
	fake := &http.Cookie{Name: "lumen_session", Value: fmt.Sprintf("%d.0.%s", expiry, security.SessionSignature(c.SigningKey, expiry, "0"))}
	check(req("GET", "/api/orders", nil, fake), 401)
	a := p.studiosByName("alice")
	_, e = a.db.Exec("INSERT INTO orders(id,gallery_id,email,amount,status,created,platform_fee) VALUES('paid',?,'guest@example.test',10000,'Paid','2026-01-01T00:00:00Z',0);INSERT INTO payment_events VALUES('paid','paystack','2026-01-01T00:00:00Z')", id)
	if e != nil {
		t.Fatal(e)
	}
	readWallet := func(c *http.Cookie) float64 {
		r := req("GET", "/api/wallet", nil, c)
		defer r.Body.Close()
		var w map[string]any
		json.NewDecoder(r.Body).Decode(&w)
		return w["earned"].(float64)
	}
	if readWallet(alice) != 10000 || readWallet(bob) != 0 {
		t.Fatal("wallet balances are not isolated")
	}
	r = req("GET", "/api/orders?limit=1", nil, alice)
	var alicePage map[string]any
	if e = json.NewDecoder(r.Body).Decode(&alicePage); e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	r = req("GET", "/api/orders?limit=1", nil, bob)
	var bobPage map[string]any
	if e = json.NewDecoder(r.Body).Decode(&bobPage); e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if alicePage["total"] != float64(1) || bobPage["total"] != float64(0) {
		t.Fatal("paged orders crossed studio boundary", alicePage, bobPage)
	}

	check(req("POST", "/api/orders/paid/sync", nil, bob), 404)
	_, e = a.db.Exec("INSERT INTO orders(id,gallery_id,email,amount,status,created,platform_fee) VALUES('webhook-order',?,'guest@example.test',10000,'Pending','2026-01-01T00:00:00Z',0)", id)
	if e != nil {
		t.Fatal(e)
	}
	event, _ := json.Marshal(map[string]any{"event": "charge.success", "data": map[string]any{"reference": "webhook-order", "status": "success", "currency": "NGN", "amount": 10000}})
	mac := hmac.New(sha512.New, []byte("test"))
	mac.Write(event)
	webhook, _ := http.NewRequest("POST", srv.URL+"/api/v1/webhooks/paystack", bytes.NewReader(event))
	webhook.Header.Set("X-Paystack-Signature", hex.EncodeToString(mac.Sum(nil)))
	wr, e := http.DefaultClient.Do(webhook)
	if e != nil {
		t.Fatal(e)
	}
	check(wr, 200)
	if readWallet(alice) != 20000 || readWallet(bob) != 0 {
		t.Fatal("webhook was credited to the wrong studio")
	}
	check(req("GET", "/api/admin/insights", nil, alice), 403)
	r = req("GET", "/api/admin/insights", nil, admin)
	if r.StatusCode != 200 {
		t.Fatal("admin insights unavailable", r.Status)
	}
	var insight map[string]any
	if e = json.NewDecoder(r.Body).Decode(&insight); e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	totals := insight["totals"].(map[string]any)
	if totals["gross"] != float64(20000) || totals["paid_orders"] != float64(2) || totals["photographer_earnings"] != float64(20000) {
		t.Fatal("incorrect platform financial totals", totals)
	}
	if len(insight["timeline"].([]any)) != 90 || len(insight["orders"].([]any)) != 2 {
		t.Fatal("invalid insight timeline or recent orders", insight)
	}

	_, e = a.db.Exec("INSERT INTO orders(id,gallery_id,email,amount,status,created,platform_fee) VALUES('sync-pending',?,'guest@example.test',12000,'Pending','2026-01-01T00:00:00Z',0)", id)
	if e != nil {
		t.Fatal(e)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"status": true, "data": map[string]any{"status": "success", "reference": "sync-pending", "amount": 12000, "currency": "NGN"}})
	}))
	defer provider.Close()
	a.api.Paystack.BaseURL = provider.URL
	syncBody := map[string]string{"accountId": a.id, "reference": "sync-pending", "kind": "order"}
	check(req("POST", "/api/admin/sync", syncBody, bob), 403)
	check(req("POST", "/api/admin/sync", map[string]string{"accountId": p.studiosByName("bob").id, "reference": "sync-pending", "kind": "order"}, admin), 404)
	r = req("POST", "/api/admin/sync", syncBody, admin)
	var syncResult map[string]any
	json.NewDecoder(r.Body).Decode(&syncResult)
	r.Body.Close()
	if syncResult["status"] != "Paid" {
		t.Fatal("admin sync failed", syncResult)
	}
	if readWallet(alice) != 32000 || readWallet(bob) != 0 {
		t.Fatal("admin sync credited wrong studio")
	}
	check(req("PATCH", "/api/galleries/"+id, map[string]any{"status": "Published"}, alice), 200)
	check(req("GET", "/api/galleries/"+id, nil, bob), 200)

	// A cached routing result is checked against its studio before use, so deleted records do not route incorrectly.
	_, e = a.db.Exec("INSERT INTO galleries(id,name,created,status) VALUES('cache-route','Cache','2026-01-01T00:00:00Z','Published')")
	if e != nil {
		t.Fatal(e)
	}
	if p.findGallery("cache-route") != a || p.findGallery("cache-route") != a {
		t.Fatal("gallery cache lookup failed")
	}
	_, e = a.db.Exec("DELETE FROM galleries WHERE id='cache-route'")
	if e != nil {
		t.Fatal(e)
	}
	bStudio := p.studiosByName("bob")
	_, e = bStudio.db.Exec("INSERT INTO galleries(id,name,created,status) VALUES('cache-route','Cache','2026-01-01T00:00:00Z','Published')")
	if e != nil {
		t.Fatal(e)
	}
	if p.findGallery("cache-route") != bStudio {
		t.Fatal("stale cache routed to wrong studio")
	}
	restarted, e := New(c, legacy, api)
	if e != nil {
		t.Fatal(e)
	}
	defer restarted.Close()
	if restarted.session(&http.Request{Header: http.Header{"Cookie": []string{alice.String()}}}) == nil || restarted.studiosByName("alice") == nil {
		t.Fatal("account or session missing after restart")
	}

}
func (p *Platform) studiosByName(name string) *studio {
	for _, s := range p.studios {
		if s.username == name {
			return s
		}
	}
	return nil
}
func BenchmarkStudioRouting(b *testing.B) {
	p := &Platform{studios: map[string]*studio{}, routes: map[string]*studio{}}
	for i := 0; i < 30; i++ {
		db, e := sql.Open("sqlite", ":memory:")
		if e != nil {
			b.Fatal(e)
		}
		db.SetMaxOpenConns(1)
		if _, e = db.Exec("CREATE TABLE galleries(id TEXT PRIMARY KEY)"); e != nil {
			b.Fatal(e)
		}
		if i == 29 {
			if _, e = db.Exec("INSERT INTO galleries VALUES('asset')"); e != nil {
				b.Fatal(e)
			}
		}
		p.studios[fmt.Sprintf("s%d", i)] = &studio{db: db}
		defer db.Close()
	}
	if p.findGallery("asset") == nil {
		b.Fatal("fixture missing")
	}
	b.Run("cached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if p.findGallery("asset") == nil {
				b.Fatal("missing")
			}
		}
	})
	b.Run("cold-miss", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if p.findGallery("absent") != nil {
				b.Fatal("unexpected")
			}
		}
	})
}
