# M1 Strategy: Dynamic Config, Health & Automated Tests

**Author**: `explorer_m1_3`  
**Milestone**: M1: Core DB Schemas, Indexes & API Gateway Routing  
**Target Files**:
- `internal/httpapi/server.go` (Endpoint updates)
- `internal/httpapi/m1_test.go` (New integration test suite)

---

## 1. Executive Summary & Objective

In Milestone M1, Pictide Studio transitions from static placeholder responses and stubbed error fallthroughs to production-ready routing and live database verification. This strategy provides the exact architectural design, code diffs, and automated test implementation for:

1. **Dynamic Runtime Configuration (`/api/config`)**:
   Eliminate hardcoded `email_configured: false` and `imgbb_previews: false` in `server.go:206`. Reflect real configuration values based on `s.cfg.SMTPHost != ""` and `s.cfg.ImgBBKey != ""`.
2. **Dynamic Live Health Check (`/api/v1/health`)**:
   Replace static `{"app": "pictide", "status": "partial"}` with active MongoDB cluster pinging (`s.db.Client().Ping(...)`) with timeout bounding, returning `{"app": "pictide", "status": "ok", "database": "connected"}` when healthy and HTTP 503 when degraded.
3. **Route Fallthrough Resolution**:
   Eliminate the catch-all 503 `"This feature is still being converted to MongoDB."` at `server.go:330`, returning clean HTTP 404 for unrecognized routes.
4. **Automated Integration Test Suite (`internal/httpapi/m1_test.go`)**:
   Implement automated end-to-end integration tests running against MongoDB in isolated temporary databases, thoroughly verifying schema indexes, dynamic config permutations, dynamic health status, and 404 error routing.

---

## 2. Analysis & Technical Specifications

### 2.1 Dynamic App Config (`GET /api/config`)

#### Current Behavior (`internal/httpapi/server.go:201-208`):
```go
if p == "/api/config" && r.Method == "GET" {
    mode := "unavailable"
    if s.cfg.PaystackSecret != "" {
        mode = "paystack"
    }
    reply(w, 200, map[string]any{
        "payments": mode,
        "public_url": s.cfg.PublicURL,
        "email_configured": false,
        "imgbb_previews": false,
        "r2_uploads": s.r2 != nil,
    })
    return
}
```

#### Rationale & Requirements:
- The frontend client (`public/app.js:66`, `public/auth-admin.js:19`, `public/enhancements.js:32`) relies on `/api/config` during initialization (`boot()`).
- When `email_configured` is false, self-registration and password recovery UI links are disabled. In Milestone M2, self-registration requires checking if email is configured.
- When `imgbb_previews` is false, preview images default to local storage. In `public/enhancements.js:32`:
  `${config.imgbb_previews ? 'ImgBB is enabled for published previews.' : 'Published previews are stored locally.'}`.

#### Field Naming Caution in `config.Config`:
- In `internal/config/config.go:19`, the struct definition is:
  ```go
  type Config struct {
      ...
      SMTPHost, SMTPPort, SMTPUser, SMTPPassword, SMTPFrom string
      ...
      ImgBBKey      string
      ImgBBPreviews bool
      ...
  }
  ```
- **Crucial finding**: The environment variable is `IMGBB_API_KEY`, but the struct field on `config.Config` is `ImgBBKey` (NOT `ImgBBApiKey`). Writing `s.cfg.ImgBBApiKey` will fail Go compilation with:
  `s.cfg.ImgBBApiKey undefined (type config.Config has no field or method ImgBBApiKey)`.
- Worker must use `s.cfg.ImgBBKey != ""` (or `s.cfg.ImgBBKey != "" && s.cfg.ImgBBPreviews`).

#### Target Implementation:
```go
if p == "/api/config" && r.Method == "GET" {
    mode := "unavailable"
    if s.cfg.PaystackSecret != "" {
        mode = "paystack"
    }
    reply(w, 200, map[string]any{
        "payments":         mode,
        "public_url":       s.cfg.PublicURL,
        "email_configured": s.cfg.SMTPHost != "",
        "imgbb_previews":   s.cfg.ImgBBKey != "",
        "r2_uploads":       s.r2 != nil,
    })
    return
}
```

---

### 2.2 Dynamic Health Check (`GET /api/v1/health`)

#### Current Behavior (`internal/httpapi/server.go:209-212`):
```go
if p == "/api/v1/health" && r.Method == "GET" {
    reply(w, 200, map[string]string{"app": "pictide", "status": "partial"})
    return
}
```

#### Rationale & Requirements:
- Container orchestrators (Docker, Kubernetes, AWS ECS) and uptime monitors hit `/api/v1/health` to determine container readiness and liveness.
- Returning `"status": "partial"` signals an unfinished migration to external monitors.
- A live ping to the MongoDB primary node ensures the server can serve requests against the database.
- The ping MUST be bounded by a timeout context (2 seconds) so that a transient database freeze does not hang incoming HTTP health probes.
- If `s.db == nil` or `Ping` returns an error:
  - Respond with HTTP `503 Service Unavailable`.
  - Body: `{"app": "pictide", "status": "unavailable", "database": "disconnected"}`.
- If healthy:
  - Respond with HTTP `200 OK`.
  - Body: `{"app": "pictide", "status": "ok", "database": "connected"}`.

#### Import Requirement:
- `server.go` needs `"go.mongodb.org/mongo-driver/v2/mongo/readpref"`.
- Call `s.db.Client().Ping(ctx, readpref.Primary())` (matching the pattern in `internal/mongostore/connect.go:24`).

#### Target Implementation:
```go
if p == "/api/v1/health" && r.Method == "GET" {
    ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
    defer cancel()
    if s.db == nil || s.db.Client().Ping(ctx, readpref.Primary()) != nil {
        reply(w, 503, map[string]string{
            "app":      "pictide",
            "status":   "unavailable",
            "database": "disconnected",
        })
        return
    }
    reply(w, 200, map[string]string{
        "app":      "pictide",
        "status":   "ok",
        "database": "connected",
    })
    return
}
```

---

### 2.3 Unrecognized Route Fallthrough (404 instead of 503)

#### Current Behavior (`internal/httpapi/server.go:322-332`):
```go
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
```

#### Rationale & Requirements:
- Line 330 is the primary source of `"This feature is still being converted to MongoDB."` across the application.
- When an authenticated client calls an endpoint that does not exist or has not been wired in `s.api`, returning HTTP 503 violates REST semantics and causes client retry loops.
- Replacing line 330 with `fail(w, 404, "Endpoint not found.")` ensures any unhandled route cleanly reports HTTP 404.

#### Target Implementation:
```go
if strings.HasPrefix(p, "/api/") {
    if user == nil {
        fail(w, 401, "Please sign in to your studio.")
        return
    }
    if s.api(w, r, user) {
        return
    }
    fail(w, 404, "Endpoint not found.")
    return
}
```

---

## 3. Worker Code Diff Plan

### Diff 1: `internal/httpapi/server.go`

```diff
--- a/internal/httpapi/server.go
+++ b/internal/httpapi/server.go
@@ -18,6 +18,7 @@ import (
 	"go.mongodb.org/mongo-driver/v2/bson"
 	"go.mongodb.org/mongo-driver/v2/mongo"
 	"go.mongodb.org/mongo-driver/v2/mongo/options"
+	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
 	"pictide.local/studio/internal/config"
 	"pictide.local/studio/internal/security"
 	"pictide.local/studio/public"
@@ -203,11 +204,26 @@ func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
 		if s.cfg.PaystackSecret != "" {
 			mode = "paystack"
 		}
-		reply(w, 200, map[string]any{"payments": mode, "public_url": s.cfg.PublicURL, "email_configured": false, "imgbb_previews": false, "r2_uploads": s.r2 != nil})
+		reply(w, 200, map[string]any{
+			"payments":         mode,
+			"public_url":       s.cfg.PublicURL,
+			"email_configured": s.cfg.SMTPHost != "",
+			"imgbb_previews":   s.cfg.ImgBBKey != "",
+			"r2_uploads":       s.r2 != nil,
+		})
 		return
 	}
 	if p == "/api/v1/health" && r.Method == "GET" {
-		reply(w, 200, map[string]string{"app": "pictide", "status": "partial"})
+		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
+		defer cancel()
+		if s.db == nil || s.db.Client().Ping(ctx, readpref.Primary()) != nil {
+			reply(w, 503, map[string]string{
+				"app":      "pictide",
+				"status":   "unavailable",
+				"database": "disconnected",
+			})
+			return
+		}
+		reply(w, 200, map[string]string{
+			"app":      "pictide",
+			"status":   "ok",
+			"database": "connected",
+		})
 		return
 	}
 	if p == "/api/login" && r.Method == "POST" {
@@ -327,7 +343,7 @@ func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
 		if s.api(w, r, user) {
 			return
 		}
-		fail(w, 503, "This feature is still being converted to MongoDB.")
+		fail(w, 404, "Endpoint not found.")
 		return
 	}
```

---

## 4. Automated Integration Test Plan (`internal/httpapi/m1_test.go`)

### 4.1 Architecture & Isolation Model
1. **Dynamic Database Isolation**:
   - The test loads `../../.env` using `godotenv.Load`.
   - If `MONGODB_URI` is empty, it skips via `t.Skip`.
   - Each test run generates a random 16-character token (`security.Token()`) and attaches to a dedicated temporary database: `pictide_m1_test_<token>`.
   - A `defer db.Drop(context.Background())` ensures zero residue in the MongoDB cluster.
2. **Four Distinct Verification Suites**:
   - `TestM1_DatabaseAndIndexes`: Verifies all collections (`accounts`, `account_links`, `withdrawals`, `guest_uploads`, `admin_audit`, `galleries`, `photos`, `orders`) and index specifications (uniqueness, compound keys, TTL).
   - `TestM1_DynamicConfig`: Verifies all permutations of `/api/config` (minimal config, full config, email-only, imgbb-only).
   - `TestM1_DynamicHealth`: Verifies `/api/v1/health` reports status `ok` and `database: connected` when Mongo is live, and status `unavailable` (HTTP 503) when disconnected or nil.
   - `TestM1_UnrecognizedRoutes_Return404`: Verifies that authenticated requests to unknown routes return HTTP 404 instead of HTTP 503 `"This feature is still being converted to MongoDB."`.

### 4.2 Exact Implementation Code for `internal/httpapi/m1_test.go`

```go
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
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			r := httptest.NewRequest(route.method, route.path, nil)
			r.AddCookie(sessionCookie)
			w := httptest.NewRecorder()

			server.ServeHTTP(w, r)

			// Critical check: Must be HTTP 404, NEVER HTTP 503
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
	}
}
```

---

## 5. Verification Commands

The Worker should run the following commands to verify Milestone M1 changes:

```powershell
# 1. Run unit tests across internal packages
go test -v ./internal/config/...

# 2. Run M1 integration tests against MongoDB
$env:PICTIDE_TEST_MONGODB="1"; go test -v -run "^TestM1_" ./internal/httpapi

# 3. Run all httpapi tests including workspace test
$env:PICTIDE_TEST_MONGODB="1"; go test -v ./internal/httpapi
```
