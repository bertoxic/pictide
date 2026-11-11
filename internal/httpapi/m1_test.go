package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"pictide.local/studio/internal/config"
	"pictide.local/studio/internal/mongostore"
	"pictide.local/studio/internal/security"
)

// setupM1TestDB establishes a connection to MongoDB and creates an isolated, temporary database.
func setupM1TestDB(t *testing.T) (*mongo.Client, *mongo.Database, func()) {
	t.Helper()
	_ = godotenv.Load("../../.env")
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		t.Skip("skipping M1 MongoDB integration test: MONGODB_URI not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := mongostore.Connect(ctx, uri)
	if err != nil {
		t.Fatalf("failed to connect to MongoDB: %v", err)
	}

	token, err := security.Token()
	if err != nil {
		_ = client.Disconnect(context.Background())
		t.Fatalf("failed to generate unique test database ID: %v", err)
	}

	dbName := "pictide_m1_test_" + token[:16]
	db := client.Database(dbName)

	cleanup := func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		_ = db.Drop(dropCtx)
		_ = client.Disconnect(dropCtx)
	}

	return client, db, cleanup
}

// TestM1_DatabaseAndIndexes verifies that httpapi.New successfully provisions all required
// collections and indexes without errors, and that constraints (e.g. uniqueness) function as expected.
func TestM1_DatabaseAndIndexes(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cfg := config.Config{
		SigningKey:    strings.Repeat("s", 32),
		AdminPassword: "admin-secure-password-123",
	}

	server, err := New(ctx, db, cfg)
	if err != nil {
		t.Fatalf("httpapi.New failed to provision indexes and initialize server: %v", err)
	}
	if server == nil {
		t.Fatal("expected non-nil server instance")
	}

	// 1. Verify accounts collection and uniqueness constraint on username
	accountsColl := db.Collection("accounts")
	_, err = accountsColl.InsertOne(ctx, bson.M{
		"_id":      "test-account-1",
		"username": "duplicate_user",
		"role":     "photographer",
	})
	if err != nil {
		t.Fatalf("failed to insert initial account: %v", err)
	}
	_, err = accountsColl.InsertOne(ctx, bson.M{
		"_id":      "test-account-2",
		"username": "duplicate_user",
		"role":     "photographer",
	})
	if err == nil {
		t.Fatal("expected duplicate username insertion to fail unique index constraint, but succeeded")
	}

	// 2. Verify account_links collection and unique token index
	linksColl := db.Collection("account_links")
	_, err = linksColl.InsertOne(ctx, bson.M{
		"token":      "unique-token-abc",
		"account_id": "acc-1",
		"purpose":    "verify",
		"expires_at": time.Now().Add(1 * time.Hour),
	})
	if err != nil {
		t.Fatalf("failed to insert account link: %v", err)
	}
	_, err = linksColl.InsertOne(ctx, bson.M{
		"token":      "unique-token-abc",
		"account_id": "acc-2",
		"purpose":    "reset",
		"expires_at": time.Now().Add(1 * time.Hour),
	})
	if err == nil {
		t.Fatal("expected duplicate token in account_links to fail unique index constraint, but succeeded")
	}

	// 3. Verify withdrawals collection and unique id index
	withdrawalsColl := db.Collection("withdrawals")
	_, err = withdrawalsColl.InsertOne(ctx, bson.M{
		"id":         "wd-100",
		"studio_id":  "legacy",
		"amount":     int64(500000),
		"reference":  "ref-wd-100",
		"created_at": time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("failed to insert withdrawal record: %v", err)
	}
	_, err = withdrawalsColl.InsertOne(ctx, bson.M{
		"id":         "wd-100",
		"studio_id":  "legacy",
		"amount":     int64(250000),
		"reference":  "ref-wd-101",
		"created_at": time.Now().Unix(),
	})
	if err == nil {
		t.Fatal("expected duplicate id in withdrawals to fail unique index constraint, but succeeded")
	}

	// 4. Verify guest_uploads collection and unique id index
	guestUploadsColl := db.Collection("guest_uploads")
	_, err = guestUploadsColl.InsertOne(ctx, bson.M{
		"id":         "gu-200",
		"studio_id":  "legacy",
		"gallery_id": "gal-1",
		"filename":   "guest_photo.jpg",
		"created_at": time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("failed to insert guest upload record: %v", err)
	}
	_, err = guestUploadsColl.InsertOne(ctx, bson.M{
		"id":         "gu-200",
		"studio_id":  "legacy",
		"gallery_id": "gal-2",
		"filename":   "another_guest_photo.jpg",
		"created_at": time.Now().Unix(),
	})
	if err == nil {
		t.Fatal("expected duplicate id in guest_uploads to fail unique index constraint, but succeeded")
	}

	// 5. Verify admin_audit collection
	auditColl := db.Collection("admin_audit")
	_, err = auditColl.InsertOne(ctx, bson.M{
		"id":         "audit-1",
		"action":     "system_init",
		"created_at": time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("failed to insert admin audit record: %v", err)
	}
}

// TestM1_DynamicConfig verifies that /api/config dynamically reports email_configured,
// imgbb_previews, payment mode, and public URL based on runtime configuration.
func TestM1_DynamicConfig(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	type configResponse struct {
		Payments        string `json:"payments"`
		PublicURL       string `json:"public_url"`
		EmailConfigured bool   `json:"email_configured"`
		ImgBBPreviews   bool   `json:"imgbb_previews"`
		R2Uploads       bool   `json:"r2_uploads"`
	}

	t.Run("MinimalConfig_Unconfigured", func(t *testing.T) {
		cfg := config.Config{
			SigningKey:    strings.Repeat("k", 32),
			AdminPassword: "demo-admin-password-123",
		}
		server, err := New(ctx, db, cfg)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/config", nil)
		server.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200, got %d", w.Code)
		}

		var res configResponse
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("invalid json response: %v", err)
		}

		if res.EmailConfigured {
			t.Errorf("expected EmailConfigured to be false, got true")
		}
		if res.ImgBBPreviews {
			t.Errorf("expected ImgBBPreviews to be false, got true")
		}
		if res.Payments != "unavailable" {
			t.Errorf("expected Payments 'unavailable', got %s", res.Payments)
		}
	})

	t.Run("FullConfig_Configured", func(t *testing.T) {
		cfg := config.Config{
			SigningKey:     strings.Repeat("k", 32),
			AdminPassword:  "demo-admin-password-123",
			SMTPHost:       "smtp.mailgun.org",
			ImgBBKey:       "synthetic_imgbb_api_key_123",
			PaystackSecret: "sk_test_fake_secret_key",
			PublicURL:      "https://studio.example.com",
		}
		server, err := New(ctx, db, cfg)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/config", nil)
		server.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200, got %d", w.Code)
		}

		var res configResponse
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("invalid json response: %v", err)
		}

		if !res.EmailConfigured {
			t.Errorf("expected EmailConfigured to be true, got false")
		}
		if !res.ImgBBPreviews {
			t.Errorf("expected ImgBBPreviews to be true, got false")
		}
		if res.Payments != "paystack" {
			t.Errorf("expected Payments 'paystack', got %s", res.Payments)
		}
		if res.PublicURL != "https://studio.example.com" {
			t.Errorf("expected PublicURL 'https://studio.example.com', got %s", res.PublicURL)
		}
	})

	t.Run("EmailOnlyConfigured", func(t *testing.T) {
		cfg := config.Config{
			SigningKey:    strings.Repeat("k", 32),
			AdminPassword: "demo-admin-password-123",
			SMTPHost:      "smtp.postmarkapp.com",
		}
		server, err := New(ctx, db, cfg)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/config", nil)
		server.ServeHTTP(w, r)

		var res configResponse
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if !res.EmailConfigured {
			t.Errorf("expected EmailConfigured true")
		}
		if res.ImgBBPreviews {
			t.Errorf("expected ImgBBPreviews false")
		}
	})

	t.Run("ImgBBOnlyConfigured", func(t *testing.T) {
		cfg := config.Config{
			SigningKey:    strings.Repeat("k", 32),
			AdminPassword: "demo-admin-password-123",
			ImgBBKey:      "sample-key",
		}
		server, err := New(ctx, db, cfg)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/config", nil)
		server.ServeHTTP(w, r)

		var res configResponse
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.EmailConfigured {
			t.Errorf("expected EmailConfigured false")
		}
		if !res.ImgBBPreviews {
			t.Errorf("expected ImgBBPreviews true")
		}
	})
}

// TestM1_DynamicHealth verifies /api/v1/health live ping to MongoDB.
func TestM1_DynamicHealth(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cfg := config.Config{
		SigningKey:    strings.Repeat("k", 32),
		AdminPassword: "demo-admin-password-123",
	}

	t.Run("LiveConnectedCluster", func(t *testing.T) {
		server, err := New(ctx, db, cfg)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v1/health", nil)
		server.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var res map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to unmarshal health response: %v", err)
		}

		if res["app"] != "pictide" {
			t.Errorf("expected app 'pictide', got %s", res["app"])
		}
		if res["status"] != "ok" {
			t.Errorf("expected status 'ok', got %s", res["status"])
		}
		if res["database"] != "connected" {
			t.Errorf("expected database 'connected', got %s", res["database"])
		}
	})

	t.Run("DisconnectedDatabase", func(t *testing.T) {
		unhealthyServer := &Server{
			db:  nil, // Simulated missing database connection
			cfg: cfg,
		}

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v1/health", nil)
		unhealthyServer.ServeHTTP(w, r)

		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected HTTP 503 for disconnected database, got %d", w.Code)
		}

		var res map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to unmarshal health response: %v", err)
		}

		if res["database"] != "disconnected" {
			t.Errorf("expected database 'disconnected', got %s", res["database"])
		}
		if res["status"] != "unavailable" {
			t.Errorf("expected status 'unavailable', got %s", res["status"])
		}
	})
}

// TestM1_UnrecognizedRoutes_Return404 verifies that unrecognized routes under /api/
// return HTTP 404 instead of HTTP 503 "This feature is still being converted to MongoDB."
func TestM1_UnrecognizedRoutes_Return404(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	adminPass := "super-secret-admin-pass-123"
	cfg := config.Config{
		SigningKey:    strings.Repeat("k", 32),
		AdminPassword: adminPass,
	}

	server, err := New(ctx, db, cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// Sign in to obtain session cookie
	loginBody := []byte(`{"username":"admin","password":"` + adminPass + `"}`)
	loginReq := httptest.NewRequest("POST", "/api/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	server.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("admin login failed: %d %s", loginRec.Code, loginRec.Body.String())
	}

	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie in login response")
	}
	sessionCookie := cookies[0]

	unknownRoutes := []struct {
		method string
		path   string
	}{
		{"GET", "/api/unknown-feature-endpoint"},
		{"POST", "/api/nonexistent-action"},
		{"DELETE", "/api/unimplemented/items"},
		{"GET", "/api/legacy-sqlite-stub"},
	}

	for _, route := range unknownRoutes {
		// Test 1: Authenticated unknown route returns 404 (NEVER 503)
		t.Run("Authenticated "+route.method+" "+route.path, func(t *testing.T) {
			r := httptest.NewRequest(route.method, route.path, nil)
			r.AddCookie(sessionCookie)
			w := httptest.NewRecorder()

			server.ServeHTTP(w, r)

			if w.Code == http.StatusServiceUnavailable {
				t.Fatalf("FAILED: route %s returned HTTP 503 ('%s') instead of HTTP 404", route.path, w.Body.String())
			}

			if w.Code != http.StatusNotFound {
				t.Fatalf("expected HTTP 404 for route %s, got %d: %s", route.path, w.Code, w.Body.String())
			}

			if strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
				t.Fatalf("response body still contains legacy 503 message: %s", w.Body.String())
			}
		})

		// Test 2: Unauthenticated unknown route returns 404 (NEVER 401 or 503)
		t.Run("Unauthenticated "+route.method+" "+route.path, func(t *testing.T) {
			r := httptest.NewRequest(route.method, route.path, nil)
			w := httptest.NewRecorder()

			server.ServeHTTP(w, r)

			if w.Code == http.StatusServiceUnavailable {
				t.Fatalf("FAILED: route %s returned HTTP 503 ('%s') instead of HTTP 404", route.path, w.Body.String())
			}

			if w.Code != http.StatusNotFound {
				t.Fatalf("expected HTTP 404 for route %s, got %d: %s", route.path, w.Code, w.Body.String())
			}
		})
	}
}

// TestM1_MethodNotAllowed_Return405 verifies that requests with unsupported methods
// on recognized paths return HTTP 405 Method Not Allowed.
func TestM1_MethodNotAllowed_Return405(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	adminPass := "super-secret-admin-pass-123"
	cfg := config.Config{
		SigningKey:    strings.Repeat("k", 32),
		AdminPassword: adminPass,
	}

	server, err := New(ctx, db, cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// Login
	loginBody := []byte(`{"username":"admin","password":"` + adminPass + `"}`)
	loginReq := httptest.NewRequest("POST", "/api/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	server.ServeHTTP(loginRec, loginReq)
	sessionCookie := loginRec.Result().Cookies()[0]

	cases := []struct {
		method string
		path   string
		auth   bool
	}{
		{"POST", "/api/config", false},
		{"DELETE", "/api/v1/health", false},
		{"GET", "/api/login", false},
		{"GET", "/api/logout", false},
		{"GET", "/api/checkout", false},
		{"POST", "/api/orders", true},
		{"POST", "/api/wallet", true},
		{"POST", "/api/admin/overview", true},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.auth {
				r.AddCookie(sessionCookie)
			}
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)

			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("expected HTTP 405 for %s %s, got %d: %s", tc.method, tc.path, w.Code, w.Body.String())
			}
		})
	}
}

// TestM1_PublicGuestUploads_AuthBypass verifies that POST /api/galleries/:id/guest-uploads
// does NOT require studio authentication (never returns HTTP 401 "Please sign in to your studio."),
// conforming to PROJECT.md § Interface Contracts. It also verifies that GET /api/galleries/:id/guest-uploads
// correctly DOES require studio authentication.
func TestM1_PublicGuestUploads_AuthBypass(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	adminPass := "test-admin-guest-upload-routing"
	cfg := config.Config{
		SigningKey:    strings.Repeat("g", 32),
		AdminPassword: adminPass,
	}

	server, err := New(ctx, db, cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// 1. Obtain authenticated session cookie for differential verification
	loginBody := []byte(`{"username":"admin","password":"` + adminPass + `"}`)
	loginReq := httptest.NewRequest("POST", "/api/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	server.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("admin login failed: %d %s", loginRec.Code, loginRec.Body.String())
	}
	sessionCookie := loginRec.Result().Cookies()[0]

	galleryIDs := []string{
		"gal-test-123",
		"afee7cdd3a84443dd082b9d24dcee3a4ac19f9af2bfe0e5bab0b83c1cf2dfc22",
	}

	for _, galID := range galleryIDs {
		uploadPath := "/api/galleries/" + galID + "/guest-uploads"

		// Test A: Unauthenticated POST MUST NOT return 401
		t.Run("Unauthenticated POST "+galID, func(t *testing.T) {
			r := httptest.NewRequest("POST", uploadPath, bytes.NewReader([]byte(`{}`)))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)

			if w.Code == http.StatusUnauthorized {
				t.Fatalf("VIOLATION: unauthenticated POST %s returned 401: %s", uploadPath, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "Please sign in to your studio") {
				t.Fatalf("VIOLATION: unauthenticated POST %s returned auth challenge message: %s", uploadPath, w.Body.String())
			}
			if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
				t.Fatalf("VIOLATION: POST %s returned legacy 503 stub", uploadPath)
			}
		})

		// Test B: Unauthenticated GET MUST return 401 (guest uploads listing is studio-only)
		t.Run("Unauthenticated GET "+galID, func(t *testing.T) {
			r := httptest.NewRequest("GET", uploadPath, nil)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected HTTP 401 for unauthenticated GET %s, got %d: %s", uploadPath, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "Please sign in to your studio") {
				t.Fatalf("expected 'Please sign in to your studio' error message, got: %s", w.Body.String())
			}
		})

		// Test C: Authenticated GET MUST NOT return 401
		t.Run("Authenticated GET "+galID, func(t *testing.T) {
			r := httptest.NewRequest("GET", uploadPath, nil)
			r.AddCookie(sessionCookie)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)

			if w.Code == http.StatusUnauthorized {
				t.Fatalf("authenticated GET %s unexpectedly returned 401: %s", uploadPath, w.Body.String())
			}
		})
	}

	// 2. Comprehensive check of all public endpoints from PROJECT.md line 57
	t.Run("ProjectContract_PublicEndpoints_NoAuthRequired", func(t *testing.T) {
		publicEndpoints := []struct {
			method string
			path   string
		}{
			{"POST", "/api/register"},
			{"POST", "/api/account/verify"},
			{"POST", "/api/account/forgot"},
			{"POST", "/api/account/reset"},
			{"POST", "/api/galleries/gal-public-check/guest-uploads"},
		}

		for _, ep := range publicEndpoints {
			r := httptest.NewRequest(ep.method, ep.path, bytes.NewReader([]byte(`{}`)))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)

			if w.Code == http.StatusUnauthorized && strings.Contains(w.Body.String(), "Please sign in to your studio") {
				t.Errorf("public endpoint %s %s incorrectly required studio authentication (401)", ep.method, ep.path)
			}
		}
	})
}

