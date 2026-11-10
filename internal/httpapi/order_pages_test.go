package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pictide.local/studio/internal/config"
	"pictide.local/studio/internal/database"
)

func makeOrderFixture(tb testing.TB, n int) *Server {
	tb.Helper()
	dir := tb.TempDir()
	db, e := database.Open(filepath.Join(dir, "test.db"))
	if e != nil {
		tb.Fatal(e)
	}
	tb.Cleanup(func() { db.Close() })
	s, e := New(db, config.Config{MediaDir: filepath.Join(dir, "media"), SigningKey: "test"})
	if e != nil {
		tb.Fatal(e)
	}
	_, e = db.Exec("INSERT INTO galleries(id,name,created,status) VALUES('g1','One','2026-01-01T00:00:00Z','Published'),('g2','Two','2026-01-01T00:00:00Z','Published')")
	if e != nil {
		tb.Fatal(e)
	}
	tx, e := db.Begin()
	if e != nil {
		tb.Fatal(e)
	}
	stmt, e := tx.Prepare("INSERT INTO orders(id,gallery_id,email,amount,status,created,platform_fee) VALUES(?,?,?,?,?,?,?)")
	if e != nil {
		tb.Fatal(e)
	}
	for i := 0; i < n; i++ {
		gallery, status := "g1", "Paid"
		if i%2 == 1 {
			gallery, status = "g2", "Pending"
		}
		_, e = stmt.Exec(fmt.Sprintf("o%08d", i), gallery, "guest@example.test", 10000, status, "2026-10-02T12:00:00Z", 1000)
		if e != nil {
			tb.Fatal(e)
		}
	}
	stmt.Close()
	if e = tx.Commit(); e != nil {
		tb.Fatal(e)
	}
	return s
}
func TestPagedOrdersFiltersAndTotals(t *testing.T) {
	s := makeOrderFixture(t, 120)
	call := func(url string) (int, map[string]any) {
		r := httptest.NewRequest("GET", url, nil)
		w := httptest.NewRecorder()
		s.orders(w, r)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	code, first := call("/api/orders?limit=50&offset=0")
	if code != 200 || first["total"] != float64(120) || len(first["items"].([]any)) != 50 || first["has_more"] != true {
		t.Fatal("first page", code, first)
	}
	_, second := call("/api/orders?limit=50&offset=50")
	seen := map[string]bool{}
	for _, x := range first["items"].([]any) {
		seen[x.(map[string]any)["id"].(string)] = true
	}
	for _, x := range second["items"].([]any) {
		if seen[x.(map[string]any)["id"].(string)] {
			t.Fatal("duplicate across pages")
		}
	}
	stats := second["insights"].(map[string]any)
	if stats["gross"] != float64(600000) || stats["net"] != float64(540000) || stats["pending"] != float64(60) {
		t.Fatal("summary should include all pages", stats)
	}
	_, filtered := call("/api/orders?limit=20&status=Pending&gallery=g2&from=2026-10-02T00%3A00%3A00Z&to=2026-10-03T00%3A00%3A00Z")
	if filtered["total"] != float64(60) || filtered["insights"].(map[string]any)["pending"] != float64(60) {
		t.Fatal("filtered totals", filtered)
	}
	_, outside := call("/api/orders?limit=20&to=2026-10-02T00%3A00%3A00Z")
	if outside["total"] != float64(0) {
		t.Fatal("exclusive end boundary", outside)
	}
	if code, _ = call("/api/orders?limit=101"); code != 400 {
		t.Fatal("invalid limit accepted")
	}
}
func BenchmarkPagedOrders(b *testing.B) {
	s := makeOrderFixture(b, 10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		s.orders(w, httptest.NewRequest("GET", "/api/orders?limit=50&offset=0", nil))
		if w.Code != 200 {
			b.Fatal(w.Body.String())
		}
	}
}
func TestCheckoutDoesNotHoldMediaLockDuringProviderRequest(t *testing.T) {
	s := makeOrderFixture(t, 0)
	s.Config.PaystackSecret = "secret"
	s.Paystack.Secret = "secret"
	_, e := s.DB.Exec("INSERT INTO photos(id,gallery_id,name,hash,created,price) VALUES('photo','g1','photo','hash','2026-10-02T12:00:00Z',10000)")
	if e != nil {
		t.Fatal(e)
	}
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		entered <- struct{}{}
		<-release
		json.NewEncoder(w).Encode(map[string]any{"status": true, "data": map[string]any{"reference": body["reference"], "authorization_url": "https://checkout.paystack.com/test"}})
	}))
	defer provider.Close()
	s.Paystack.BaseURL = provider.URL
	done := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/api/checkout", strings.NewReader(`{"galleryId":"g1","email":"guest@example.test","photos":["photo"],"expectedAmount":10000}`))
			s.checkout(w, r)
			done <- w.Code
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			close(release)
			t.Fatal("a checkout was blocked behind another provider request")
		}
	}
	close(release)
	for i := 0; i < 2; i++ {
		if code := <-done; code != 200 {
			t.Fatalf("checkout status %d", code)
		}
	}
}
func TestPagedGalleriesFiltersAndStablePages(t *testing.T) {
	s := makeOrderFixture(t, 0)
	tx, e := s.DB.Begin()
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 65; i++ {
		_, e = tx.Exec("INSERT INTO galleries(id,name,created,status) VALUES(?,?,?,?)", fmt.Sprintf("gallery%03d", i), fmt.Sprintf("Gallery %03d", i), "2026-10-02T12:00:00Z", "Draft")
		if e != nil {
			t.Fatal(e)
		}
	}
	_, e = tx.Exec("INSERT INTO galleries(id,name,created,status) VALUES('percent','100% real','2026-10-02T12:00:00Z','Draft'),('other','100x real','2026-10-02T12:00:00Z','Draft')")
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	call := func(query string) (int, map[string]any) {
		w := httptest.NewRecorder()
		s.listGalleries(w, httptest.NewRequest("GET", "/api/galleries?"+query, nil))
		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		return w.Code, result
	}
	code, first := call("limit=30&offset=0&status=Draft&sort=newest")
	if code != 200 || first["total"] != float64(67) || len(first["items"].([]any)) != 30 {
		t.Fatal("first page", code, first)
	}
	_, second := call("limit=30&offset=30&status=Draft&sort=newest")
	seen := map[string]bool{}
	for _, x := range first["items"].([]any) {
		seen[x.(map[string]any)["id"].(string)] = true
	}
	for _, x := range second["items"].([]any) {
		if seen[x.(map[string]any)["id"].(string)] {
			t.Fatal("duplicate gallery across pages")
		}
	}
	_, matches := call("limit=30&q=100%25")
	if matches["total"] != float64(1) {
		t.Fatal("search wildcard must be literal", matches)
	}
	if code, _ = call("limit=1000"); code != 400 {
		t.Fatal("oversized page accepted")
	}
}
func TestCheckoutProviderFailureKeepsOrderPending(t *testing.T) {
	s := makeOrderFixture(t, 0)
	s.Config.PaystackSecret = "secret"
	s.Paystack.Secret = "secret"
	_, e := s.DB.Exec("INSERT INTO photos(id,gallery_id,name,hash,created,price) VALUES('photo','g1','photo','hash','2026-10-02T12:00:00Z',10000)")
	if e != nil {
		t.Fatal(e)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		w.Write([]byte(`{"status":false,"message":"unavailable"}`))
	}))
	defer provider.Close()
	s.Paystack.BaseURL = provider.URL
	w := httptest.NewRecorder()
	s.checkout(w, httptest.NewRequest("POST", "/api/checkout", strings.NewReader(`{"galleryId":"g1","email":"guest@example.test","photos":["photo"]}`)))
	if w.Code != 502 {
		t.Fatal("unexpected status", w.Code)
	}
	var status string
	if e = s.DB.QueryRow("SELECT status FROM orders").Scan(&status); e != nil || status != "Pending" {
		t.Fatal("uncertain provider outcome must remain pending", status, e)
	}
}
func BenchmarkPagedOrdersParallel(b *testing.B) {
	s := makeOrderFixture(b, 10000)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			w := httptest.NewRecorder()
			s.orders(w, httptest.NewRequest("GET", "/api/orders?limit=50&offset=0", nil))
			if w.Code != 200 {
				b.Errorf("unexpected status: %d", w.Code)
				return
			}
		}
	})
}
