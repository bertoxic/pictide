# Milestone M1 Router Fix Strategy: Public Guest Upload Exemption

## 1. Executive Summary & Problem Root Cause

### 1.1 The Failure
During the review and adversarial challenge phase of Milestone M1, `TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher` failed with:
```
e2e_tier1_test.go:48: public endpoint POST /api/galleries/779dd780.../guest-uploads incorrectly required authentication (401)
```

### 1.2 Root Cause Analysis
In `internal/httpapi/server.go`:
1. **The Request Dispatch Pipeline (`server.go:270-286`)**:
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

   // 2c. Clean 404 for any unrecognized /api/ route (replaces line 330 503 catch-all)
   fail(w, 404, "Endpoint not found.")
   ```

2. **The Route Authenticator (`server.go:490-505`)**:
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
       if s.api(w, r, user) {
           return true
       }
       fail(w, 405, "Method not allowed.")
       return true
   }
   ```

3. **The Route Classifier (`server.go:507-533`)**:
   ```go
   func isAuthRoute(p string) bool {
       switch {
       ...
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

Because `isAuthRoute` matched `parts[1] == "guest-uploads"` solely based on URL path without checking the HTTP method:
- An unauthenticated caller issuing `POST /api/galleries/:id/guest-uploads` passes through `publicAPI` (which does not handle it in M1).
- `authenticatedAPI` evaluates `isAuthRoute(p)`, which returns `true`.
- Since `user == nil`, `authenticatedAPI` immediately rejects the request with HTTP 401 `"Please sign in to your studio."`.
- This violates `PROJECT.md` § Interface Contracts line 57:
  > *"Public endpoints (`/api/register`, `/api/account/verify`, `/api/account/forgot`, `/api/account/reset`, `/api/galleries/{id}/guest-uploads`) MUST NOT require authentication session."*

---

## 2. Milestone Scope & Lifecycle Architecture (M1 vs M4)

### 2.1 Scope Separation
- **Milestone M1**: DB schemas, indexes, dynamic `/api/config` and `/api/v1/health`, clean 404/405 gateway dispatching, and elimination of legacy 503 catch-all stubs.
- **Milestone M4**: Guest uploads feature implementation (`POST /api/galleries/:id/guest-uploads` multipart upload, GridFS persistence, token generation, preview, review, and status check).

### 2.2 Behavior of Planned Public Routes in M1
In `TestE2E_Tier1/Feature01`:
```go
publicEndpoints := []struct {
    method string
    path   string
    body   string
}{
    {"POST", "/api/register", ...},
    {"POST", "/api/account/forgot", ...},
    {"POST", "/api/account/verify", ...},
    {"POST", "/api/account/reset", ...},
    {"POST", "/api/galleries/" + galleryID + "/guest-uploads", ...},
    {"GET", "/api/config", ...},
    ...
}
```
For unimplemented public endpoints (`/api/register`, `/api/account/forgot`, etc.):
- They are not in `publicAPI` in M1.
- They are NOT in `isAuthRoute`.
- Therefore, `authenticatedAPI` returns `false`.
- They fall through to `fail(w, 404, "Endpoint not found.")`.
- In `Feature01`: `w.Code == 404 != 401`, so they pass without requiring authentication.

Exempting `POST /api/galleries/:id/guest-uploads` from `isAuthRoute` allows unauthenticated callers to receive HTTP 404 instead of HTTP 401 in M1. This satisfies the interface contract and test assertion completely while avoiding fake stubs or mock facades.

### 2.3 Preserving Protection for Studio Owners
Studio owners review guest submissions via `GET /api/galleries/:id/guest-uploads` (`PROJECT.md` line 50: *"Feature 17: Guest Uploads Listing: Studio owner endpoint GET /api/galleries/{id}/guest-uploads lists submissions pending review"*).
- For `GET /api/galleries/:id/guest-uploads`, `isAuthRoute` MUST return `true`.
- If an unauthenticated caller attempts `GET /api/galleries/:id/guest-uploads`, they MUST receive HTTP 401 `"Please sign in to your studio."`.
- If an authenticated studio owner attempts `GET /api/galleries/:id/guest-uploads`, in M1 it reaches `s.api(w, r, user)` (returning 405 until M4 implements the listing handler). In M4, `workspace.go` will handle `GET` and return the submissions list.

### 2.4 Future M4 Integration Blueprint
When M4 is implemented:
- In `internal/httpapi/server.go` (`publicAPI`):
  ```go
  if len(parts) == 2 && parts[0] != "" && parts[1] == "guest-uploads" && r.Method == http.MethodPost {
      s.guestUpload(w, r, parts[0])
      return true
  }
  ```
- In `internal/httpapi/workspace.go` (`s.api`):
  ```go
  case parts[1] == "guest-uploads" && r.Method == http.MethodGet:
      s.listGuestUploads(w, r, user, parts[0])
      return true
  ```
Because `isAuthRoute` already exempts `POST` while protecting `GET`, this setup is 100% forward-compatible with M4.

---

## 3. Drop-in Implementation Strategy

### 3.1 Option 1 (Recommended): Single Source of Truth in `isAuthRoute(r *http.Request)`
Refactor `isAuthRoute` to accept `*http.Request` so that method-dependent path rules are evaluated directly where route definitions reside.

#### Target File: `internal/httpapi/server.go`
```go
// Line 490: Update authenticatedAPI
func (s *Server) authenticatedAPI(w http.ResponseWriter, r *http.Request, user *account) bool {
	if !isAuthRoute(r) {
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

// Line 507: Update isAuthRoute signature and case
func isAuthRoute(r *http.Request) bool {
	p := r.URL.Path
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
				return r.Method != http.MethodPost
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

#### Diff Patch for Option 1:
```diff
--- a/internal/httpapi/server.go
+++ b/internal/httpapi/server.go
@@ -490,5 +490,4 @@
 func (s *Server) authenticatedAPI(w http.ResponseWriter, r *http.Request, user *account) bool {
-	p := r.URL.Path
-	if !isAuthRoute(p) {
+	if !isAuthRoute(r) {
 		return false
 	}
@@ -507,3 +506,4 @@
-func isAuthRoute(p string) bool {
+func isAuthRoute(r *http.Request) bool {
+	p := r.URL.Path
 	switch {
@@ -528,3 +528,5 @@
 			switch parts[1] {
-			case "direct-upload", "cover", "customization", "card-design", "qr", "print", "photos", "guest-uploads":
+			case "direct-upload", "cover", "customization", "card-design", "qr", "print", "photos":
 				return true
+			case "guest-uploads":
+				return r.Method != http.MethodPost
 			}
```

---

### 3.2 Option 2 (Alternative): Minimal Diff Guard in `authenticatedAPI`
If keeping `isAuthRoute(p string)` unchanged is preferred, the exemption can be added directly inside `authenticatedAPI`:

```go
func (s *Server) authenticatedAPI(w http.ResponseWriter, r *http.Request, user *account) bool {
	p := r.URL.Path
	if !isAuthRoute(p) || (r.Method == http.MethodPost && strings.HasSuffix(p, "/guest-uploads")) {
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

#### Diff Patch for Option 2:
```diff
--- a/internal/httpapi/server.go
+++ b/internal/httpapi/server.go
@@ -491,3 +491,3 @@
 	p := r.URL.Path
-	if !isAuthRoute(p) {
+	if !isAuthRoute(p) || (r.Method == http.MethodPost && strings.HasSuffix(p, "/guest-uploads")) {
 		return false
 	}
```

*Note: Option 1 is recommended over Option 2 because it avoids path suffix heuristics and keeps route pattern matching consolidated within `isAuthRoute`.*

---

## 4. Test Suite Augmentation

To ensure regressions are prevented at the unit test level (without relying solely on the extensive `TestE2E_Tier1`), the Worker should add the following targeted test to `internal/httpapi/m1_test.go`:

```go
// TestM1_GuestUploads_PublicVsAuthenticatedRouting verifies that POST /api/galleries/:id/guest-uploads
// does not require studio authentication, whereas GET /api/galleries/:id/guest-uploads is protected.
func TestM1_GuestUploads_PublicVsAuthenticatedRouting(t *testing.T) {
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

	// 1. Unauthenticated POST /api/galleries/:id/guest-uploads MUST NOT return 401
	rPost := httptest.NewRequest("POST", "/api/galleries/gal-test-123/guest-uploads", nil)
	wPost := httptest.NewRecorder()
	server.ServeHTTP(wPost, rPost)
	if wPost.Code == http.StatusUnauthorized {
		t.Errorf("unauthenticated POST /api/galleries/:id/guest-uploads returned HTTP 401; expected non-401 (404 in M1)")
	}

	// 2. Unauthenticated GET /api/galleries/:id/guest-uploads MUST return 401
	rGet := httptest.NewRequest("GET", "/api/galleries/gal-test-123/guest-uploads", nil)
	wGet := httptest.NewRecorder()
	server.ServeHTTP(wGet, rGet)
	if wGet.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated GET /api/galleries/:id/guest-uploads returned HTTP %d; expected 401", wGet.Code)
	}
	if !strings.Contains(wGet.Body.String(), "Please sign in to your studio.") {
		t.Errorf("expected 401 body 'Please sign in to your studio.', got: %s", wGet.Body.String())
	}
}
```

---

## 5. Verification Commands for Worker & Reviewer

1. Verify fix against `TestE2E_Tier1/Feature01`:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestE2E_Tier1$/Feature01' ./internal/httpapi
   ```
   *Expected output*: `--- PASS: TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher (0.35s)`

2. Verify all M1 integration and unit suites:
   ```powershell
   go test -v -p 1 ./internal/mongostore/...
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestM1_' ./internal/httpapi
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestChallengerM1_' ./internal/httpapi
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestMongoWorkspace$' ./internal/httpapi
   node --test test/*.test.js
   go vet ./...
   ```
   *Expected output*: All tests pass with zero failures.
