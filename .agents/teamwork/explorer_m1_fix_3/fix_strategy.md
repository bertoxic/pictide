# Fix Strategy: Milestone M1 — Public Guest Upload Routing Auth Bypass

## 1. Executive Summary

- **Issue**: `TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher` failed with:
  ```
  e2e_tier1_test.go:48: public endpoint POST /api/galleries/<gallery_id>/guest-uploads incorrectly required authentication (401)
  ```
- **Root Cause**: In `internal/httpapi/server.go:529`, `isAuthRoute` classified all requests targeting `/api/galleries/:id/guest-uploads` as authenticated studio routes regardless of HTTP method. When an unauthenticated caller sent `POST /api/galleries/:id/guest-uploads`, `authenticatedAPI` checked `user == nil` and rejected the request with HTTP 401 `"Please sign in to your studio."`.
- **Contract Requirement**: `PROJECT.md` § Interface Contracts (line 57) explicitly defines:
  > "Public endpoints (`/api/register`, `/api/account/verify`, `/api/account/forgot`, `/api/account/reset`, `/api/galleries/{id}/guest-uploads`) MUST NOT require authentication session."
  Furthermore, Feature 16 (`POST /api/galleries/{id}/guest-uploads`) is a public guest submission endpoint, while Feature 17 (`GET /api/galleries/{id}/guest-uploads`) is the authenticated studio listing endpoint.
- **Solution**:
  1. Update `isAuthRoute` in `internal/httpapi/server.go` to accept `method, p string`, and return `false` when `method == http.MethodPost` for `guest-uploads`.
  2. Add a comprehensive regression test `TestM1_PublicGuestUploads_AuthBypass` to `internal/httpapi/m1_test.go`.

---

## 2. Root Cause Analysis

### Code Flow Trace

1. **Request Ingestion** (`server.go:271-285`):
   ```go
   // 2. API Gateway: extract session for dual-mode and authenticated routes
   user := s.session(r)

   // 2a. Dispatch public & dual-mode API routes (no auth required)
   if s.publicAPI(w, r, user) {
       return
   }

   // 2b. Dispatch authenticated studio API routes
   if s.authenticatedAPI(w, r, user) {
       return
   }

   // 2c. Clean 404 for any unrecognized /api/ route
   fail(w, 404, "Endpoint not found.")
   ```
2. **Public API Handling** (`server.go:431-487`):
   `s.publicAPI` inspects `p` prefixed with `/api/galleries/`. It handles `len(parts) == 1` (`GET /api/galleries/:id`), `parts[1] == "free-download"`, and `parts[1] == "qr"`. For `parts[1] == "guest-uploads"`, `publicAPI` returns `false` (as guest upload submission implementation is scheduled for Milestone M4).
3. **Authenticated API Handling** (`server.go:490-505`):
   Execution flows to `authenticatedAPI`:
   ```go
   func (s *Server) authenticatedAPI(w http.ResponseWriter, r *http.Request, user *account) bool {
       p := r.URL.Path
       if !isAuthRoute(p) {
           return false
       }
       if user == nil {
           fail(w, 401, "Please sign in to your studio.")
           return true
       }
       ...
   }
   ```
4. **The Faulty Route Classification** (`server.go:521-533`):
   ```go
   case strings.HasPrefix(p, "/api/galleries/"):
       sub := strings.TrimPrefix(p, "/api/galleries/")
       parts := strings.Split(sub, "/")
       if len(parts) == 1 && parts[0] != "" {
           return true // /api/galleries/:id (PATCH, DELETE)
       }
       if len(parts) == 2 && parts[0] != "" {
           switch parts[1] {
           case "direct-upload", "cover", "customization", "card-design", "qr", "print", "photos", "guest-uploads":
               return true
           }
       }
       return false
   ```
   Because `isAuthRoute` returned `true` for `guest-uploads` on every method, any unauthenticated caller was greeted with HTTP 401 instead of being allowed through as a public route.
5. **Impact on E2E Tests**:
   `TestE2E_Tier1/Feature01` asserts:
   ```go
   if w.Code == http.StatusUnauthorized && strings.Contains(w.Body.String(), "Please sign in to your studio") {
       t.Errorf("public endpoint %s %s incorrectly required authentication (401)", ep.method, ep.path)
   }
   ```
   Because `POST /api/galleries/:id/guest-uploads` returned 401 with `"Please sign in to your studio."`, the test failed.

---

## 3. Worker Implementation Plan

### Step 1: Update `internal/httpapi/server.go`

In `internal/httpapi/server.go`:
1. Change `authenticatedAPI` to pass `r.Method` to `isAuthRoute`:
   ```go
   func (s *Server) authenticatedAPI(w http.ResponseWriter, r *http.Request, user *account) bool {
   	p := r.URL.Path
   	if !isAuthRoute(r.Method, p) {
   		return false
   	}
   	if user == nil {
   		fail(w, 401, "Please sign in to your studio.")
   		return true
   	}
   	if s.api(w, r, user) {
   		return true
   	}
   	// Recognized authenticated route path, but unsupported HTTP method
   	fail(w, 405, "Method not allowed.")
   	return true
   }
   ```
2. Update `isAuthRoute` signature to `func isAuthRoute(method, p string) bool`, and split `guest-uploads` out from unconditional authenticated routes:
   ```go
   func isAuthRoute(method, p string) bool {
   	switch {
   	case p == "/api/admin/overview",
   		p == "/api/admin/insights",
   		p == "/api/admin/accounts",
   		p == "/api/admin/activity",
   		p == "/api/admin/account-status",
   		p == "/api/security/password",
   		p == "/api/account/email",
   		p == "/api/settings",
   		p == "/api/galleries",
   		p == "/api/orders",
   		p == "/api/wallet":
   		return true
   	case strings.HasPrefix(p, "/api/galleries/"):
   		sub := strings.TrimPrefix(p, "/api/galleries/")
   		parts := strings.Split(sub, "/")
   		if len(parts) == 1 && parts[0] != "" {
   			return true // /api/galleries/:id (PATCH, DELETE)
   		}
   		if len(parts) == 2 && parts[0] != "" {
   			switch parts[1] {
   			case "direct-upload", "cover", "customization", "card-design", "qr", "print", "photos":
   				return true
   			case "guest-uploads":
   				// Feature 16: POST is public submission (MUST NOT require auth)
   				// Feature 17: GET is studio listing (MUST require auth)
   				return method != http.MethodPost
   			}
   		}
   		return false
   	case strings.HasPrefix(p, "/api/orders/"):
   		sub := strings.TrimPrefix(p, "/api/orders/")
   		return strings.HasSuffix(sub, "/sync") && strings.TrimSuffix(sub, "/sync") != "" && !strings.Contains(strings.TrimSuffix(sub, "/sync"), "/")
   	case strings.HasPrefix(p, "/api/direct-uploads/"):
   		sub := strings.TrimPrefix(p, "/api/direct-uploads/")
   		return strings.HasSuffix(sub, "/complete") && strings.TrimSuffix(sub, "/complete") != "" && !strings.Contains(strings.TrimSuffix(sub, "/complete"), "/")
   	case strings.HasPrefix(p, "/api/photos/"):
   		sub := strings.TrimPrefix(p, "/api/photos/")
   		return sub != "" && !strings.Contains(sub, "/")
   	default:
   		return false
   	}
   }
   ```

#### Exact Diff for `internal/httpapi/server.go`

```diff
--- a/internal/httpapi/server.go
+++ b/internal/httpapi/server.go
@@ -491,3 +491,3 @@
 func (s *Server) authenticatedAPI(w http.ResponseWriter, r *http.Request, user *account) bool {
 	p := r.URL.Path
-	if !isAuthRoute(p) {
+	if !isAuthRoute(r.Method, p) {
 		return false
@@ -507,3 +507,3 @@
-func isAuthRoute(p string) bool {
+func isAuthRoute(method, p string) bool {
 	switch {
@@ -528,4 +528,7 @@
 		if len(parts) == 2 && parts[0] != "" {
 			switch parts[1] {
-			case "direct-upload", "cover", "customization", "card-design", "qr", "print", "photos", "guest-uploads":
+			case "direct-upload", "cover", "customization", "card-design", "qr", "print", "photos":
+				return true
+			case "guest-uploads":
+				return method != http.MethodPost
 			}
```

---

### Step 2: Add Regression Test to `internal/httpapi/m1_test.go`

Append `TestM1_PublicGuestUploads_AuthBypass` to the end of `internal/httpapi/m1_test.go`:

```go
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
```

---

## 4. Verification Steps for the Worker

After applying the diffs to `server.go` and `m1_test.go`, run:

1. **Verify the new M1 unit and integration test**:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestM1_PublicGuestUploads_AuthBypass$' ./internal/httpapi
   ```
   *Expected*: PASS.

2. **Verify all M1 test suites**:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -count=1 -v -run '^TestM1_' ./internal/httpapi
   ```
   *Expected*: All 6 tests PASS.

3. **Verify the Tier 1 E2E Feature 01 test**:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestE2E_Tier1$/Feature01' ./internal/httpapi
   ```
   *Expected*: PASS (exit code 0).

4. **Verify full M1 regression suite**:
   ```powershell
   go test -count=1 -v -p 1 ./internal/mongostore/...
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestChallengerM1_' ./internal/httpapi
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestMongoWorkspace$' ./internal/httpapi
   node --test test/*.test.js
   go vet ./...
   ```
   *Expected*: All PASS, zero regressions.
