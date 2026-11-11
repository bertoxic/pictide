# Fix Strategy: Public Route Audit & Method-Dependent Gateway Routing

## Executive Summary

This audit investigates the Milestone M1 gate rejection reported by `reviewer_m1_1` in `TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher`:
```
e2e_tier1_test.go:48: public endpoint POST /api/galleries/<id>/guest-uploads incorrectly required authentication (401)
```

The audit reviewed all public, authenticated, dual-mode, and method-dependent routes planned across all 30 features in `PROJECT.md` against the route dispatcher implementation in `internal/httpapi/server.go`.

### Key Conclusions:
1. **Root Cause**: In `server.go:528-533`, `isAuthRoute(p string)` classified all requests matching `/api/galleries/:id/guest-uploads` as authenticated studio routes regardless of the HTTP method (`r.Method`). When an unauthenticated visitor submitted a guest upload via `POST /api/galleries/:id/guest-uploads`, `authenticatedAPI` checked `if user == nil` and rejected the request prematurely with HTTP 401 (`"Please sign in to your studio."`).
2. **Comprehensive Public Route Audit**: Zero other planned public endpoints are blocked by `isAuthRoute` or `user == nil`. All other public endpoints (`/api/config`, `/api/v1/health`, `/api/login`, `/api/logout`, `/api/session`, `/api/checkout`, `/api/v1/webhooks/paystack`, `/api/photos/:id/download-link`, `/api/download/:id`, `/api/photos/:id/free`, `/api/photos/:id/clean`, `/api/galleries/:id/free-download`, `/api/galleries/:id/qr`, `/api/register`, `/api/account/verify`, `/api/account/forgot`, `/api/account/reset`, and `/api/guest-uploads/status`) correctly bypass studio authentication.
3. **Exact Fix**: Update `isAuthRoute` to accept both HTTP method and path (`func isAuthRoute(method, p string) bool`). In `authenticatedAPI`, pass `r.Method` alongside `p` (`isAuthRoute(r.Method, p)`). In `isAuthRoute`, discriminate `/guest-uploads`: return `method != http.MethodPost`. This exempts unauthenticated `POST /api/galleries/:id/guest-uploads` from studio authentication while preserving strict 401 authentication protection for studio operations like `GET /api/galleries/:id/guest-uploads`.

---

## 1. Complete Inventory & Audit of All Public Endpoints

Every planned endpoint across all 30 features from `PROJECT.md` and existing routes was examined against `publicAPI`, `authenticatedAPI`, and `isAuthRoute` in `internal/httpapi/server.go`:

| # | Endpoint | Method(s) | Expected Mode | Current `publicAPI` Status | Current `isAuthRoute` Status | Unauthenticated Behavior | Audit Verdict |
|---|----------|-----------|---------------|---------------------------|------------------------------|--------------------------|---------------|
| 1 | `/api/config` | GET, HEAD | Public | Handled (lines 291-307) | Returns `false` | 200 OK | **CLEAN** |
| 2 | `/api/v1/health` | GET, HEAD | Public | Handled (lines 309-329) | Returns `false` | 200 / 503 dynamic | **CLEAN** |
| 3 | `/api/login` | POST | Public | Handled (lines 331-337) | Returns `false` | Authenticates or 401 invalid creds | **CLEAN** |
| 4 | `/api/logout` | POST | Public | Handled (lines 339-353) | Returns `false` | Clears cookie, 200 OK | **CLEAN** |
| 5 | `/api/session` | GET, HEAD | Dual-Mode Public | Handled (lines 355-365) | Returns `false` | 200 `{"owner":false}` when unauth | **CLEAN** |
| 6 | `/api/checkout` | POST | Public | Handled (lines 367-373) | Returns `false` | Creates checkout order | **CLEAN** |
| 7 | `/api/v1/webhooks/paystack` | POST | Public Webhook | Handled (lines 375-381) | Returns `false` | Processes webhook signature | **CLEAN** |
| 8 | `/api/checkout/:id/verify` | POST | Public | Handled (lines 383-393) | Returns `false` | Verifies order token | **CLEAN** |
| 9 | `/api/photos/:id/download-link` | POST | Public | Handled (lines 395-405) | Returns `false` (sub contains `/`) | Generates signed link | **CLEAN** |
| 10 | `/api/download/:id` | GET, HEAD | Public | Handled (lines 407-417) | Returns `false` | Streams photo file | **CLEAN** |
| 11 | `/api/photos/:id/free` | GET, HEAD | Public | Handled (lines 419-429) | Returns `false` (sub contains `/`) | Streams free photo | **CLEAN** |
| 12 | `/api/photos/:id/clean` | GET, HEAD | Public | Handled (lines 419-429) | Returns `false` (sub contains `/`) | Streams unlocked photo | **CLEAN** |
| 13 | `/api/galleries/:id` | GET, HEAD | Dual-Mode Public | Handled (lines 431-450) | `true` for PATCH/DELETE | Unauth: public gallery; Auth: studio gallery | **CLEAN** |
| 14 | `/api/galleries/:id/free-download` | GET, HEAD | Public | Handled (lines 451-458) | Returns `false` | Downloads zip of free photos | **CLEAN** |
| 15 | `/api/galleries/:id/qr` | GET, HEAD | Dual-Mode Public | Handled (lines 459-484) | Redundant in switch, bypassed | Generates QR PNG/SVG | **CLEAN** |
| 16 | `/api/register` | POST | Public (M2) | Not handled in M1 | Returns `false` | Clean 404 in M1; no 401 | **CLEAN** |
| 17 | `/api/account/verify` | POST | Public (M2) | Not handled in M1 | Returns `false` | Clean 404 in M1; no 401 | **CLEAN** |
| 18 | `/api/account/forgot` | POST | Public (M2) | Not handled in M1 | Returns `false` | Clean 404 in M1; no 401 | **CLEAN** |
| 19 | `/api/account/reset` | POST | Public (M2) | Not handled in M1 | Returns `false` | Clean 404 in M1; no 401 | **CLEAN** |
| 20 | `/api/guest-uploads/status` | POST | Public (M4) | Not handled in M1 | Returns `false` | Clean 404 in M1; no 401 | **CLEAN** |
| 21 | `/api/galleries/:id/guest-uploads` | POST | Public (M4) | Not handled in M1 | **RETURNS TRUE (BUG)** | **401 "Please sign in to your studio."** | **DEFECT (FIX REQUIRED)** |

---

## 2. Root Cause Analysis

### 2.1 The Gateway Pipeline in `internal/httpapi/server.go`

In `server.go:271-286`:
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

### 2.2 The Defect Mechanism

1. An unauthenticated guest or E2E test issues `POST /api/galleries/<id>/guest-uploads` without a session cookie.
2. In Milestone M1, Feature 16 (`s.submitGuestUpload`) is not yet implemented, so `publicAPI(w, r, user)` returns `false`.
3. Execution proceeds to `authenticatedAPI(w, r, user)`.
4. `authenticatedAPI` calls `isAuthRoute(p)` (line 492).
5. In `isAuthRoute(p)` (lines 521-533):
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
   Because `parts[1] == "guest-uploads"`, `isAuthRoute` unconditionally returns `true`.
6. Back in `authenticatedAPI` (lines 495-498):
   ```go
   if user == nil {
       fail(w, 401, "Please sign in to your studio.")
       return true
   }
   ```
   Because the guest has no session (`user == nil`), `authenticatedAPI` immediately rejects the request with HTTP 401 `"Please sign in to your studio."`.
7. This directly violates `PROJECT.md` line 57:
   > "Public endpoints (`/api/register`, `/api/account/verify`, `/api/account/forgot`, `/api/account/reset`, `/api/galleries/{id}/guest-uploads`) MUST NOT require authentication session."
   and breaks `TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher`.

---

## 3. Fix Strategy & Architecture

### 3.1 Method-Dependent Route Classification

HTTP routes are tuples of `(Method, Path)`. While paths like `/api/settings` and `/api/orders` are protected regardless of method, `/api/galleries/:id/guest-uploads` has asymmetric access semantics:
- `POST /api/galleries/:id/guest-uploads`: Public guest upload submission. Must NEVER require authentication session.
- `GET /api/galleries/:id/guest-uploads`: Authenticated studio endpoint for the photographer to review submitted photos. Must require authentication (`user != nil`).
- `HEAD /api/galleries/:id/guest-uploads`: Equivalent to GET. Requires authentication.

### 3.2 Required Code Changes in `internal/httpapi/server.go`

1. Update `isAuthRoute` signature to accept `(method, p string)`:
   ```go
   func isAuthRoute(method, p string) bool
   ```
2. Update the invocation in `authenticatedAPI` to pass `r.Method`:
   ```go
   func (s *Server) authenticatedAPI(w http.ResponseWriter, r *http.Request, user *account) bool {
       p := r.URL.Path
       if !isAuthRoute(r.Method, p) {
           return false
       }
   ```
3. Inside `isAuthRoute`, split the `switch parts[1]` block under `/api/galleries/`:
   ```go
   if len(parts) == 2 && parts[0] != "" {
       switch parts[1] {
       case "direct-upload", "cover", "customization", "card-design", "qr", "print", "photos":
           return true
       case "guest-uploads":
           // POST /api/galleries/:id/guest-uploads is a public guest upload submission;
           // GET/HEAD is studio review.
           return method != http.MethodPost
       }
   }
   ```

### 3.3 Lifecycle Behavior Trace With Fix

| Scenario | Method & Path | Auth State | `publicAPI` | `isAuthRoute` | `authenticatedAPI` | Final Response |
|----------|---------------|------------|-------------|---------------|--------------------|----------------|
| **M1 Tier1 Test** | `POST /api/galleries/:id/guest-uploads` | Unauth (`user == nil`) | `false` | `false` (`method == POST`) | `false` | **404 "Endpoint not found."** (PASSES `TestE2E_Tier1/Feature01`) |
| **M4 Guest Upload** | `POST /api/galleries/:id/guest-uploads` | Unauth (`user == nil`) | `true` (handled) | N/A | N/A | **200 OK (guest upload created)** |
| **M1/M4 Studio Review** | `GET /api/galleries/:id/guest-uploads` | Unauth (`user == nil`) | `false` | `true` (`method == GET`) | `user == nil` -> **401** | **401 "Please sign in to your studio."** |
| **M1 Studio Review** | `GET /api/galleries/:id/guest-uploads` | Auth (`user != nil`) | `false` | `true` | `s.api` returns `false` | **405 "Method not allowed."** |
| **M4 Studio Review** | `GET /api/galleries/:id/guest-uploads` | Auth (`user != nil`) | `false` | `true` | `s.api` returns `true` | **200 OK (list of submissions)** |

---

## 4. Drop-In Diff Recommendation for Worker

The Worker can apply this exact drop-in diff to `internal/httpapi/server.go`:

```diff
--- a/internal/httpapi/server.go
+++ b/internal/httpapi/server.go
@@ -490,7 +490,7 @@ func (s *Server) publicAPI(w http.ResponseWriter, r *http.Request, user *account
 
 func (s *Server) authenticatedAPI(w http.ResponseWriter, r *http.Request, user *account) bool {
 	p := r.URL.Path
-	if !isAuthRoute(p) {
+	if !isAuthRoute(r.Method, p) {
 		return false
 	}
 	if user == nil {
@@ -504,7 +504,7 @@ func (s *Server) authenticatedAPI(w http.ResponseWriter, r *http.Request, user *
 	return true
 }
 
-func isAuthRoute(p string) bool {
+func isAuthRoute(method, p string) bool {
 	switch {
 	case p == "/api/admin/overview",
 		p == "/api/admin/insights",
@@ -526,7 +526,11 @@ func isAuthRoute(p string) bool {
 		}
 		if len(parts) == 2 && parts[0] != "" {
 			switch parts[1] {
-			case "direct-upload", "cover", "customization", "card-design", "qr", "print", "photos", "guest-uploads":
+			case "direct-upload", "cover", "customization", "card-design", "qr", "print", "photos":
 				return true
+			case "guest-uploads":
+				// POST /api/galleries/:id/guest-uploads is a public guest upload submission;
+				// GET/HEAD is studio review.
+				return method != http.MethodPost
 			}
 		}
 		return false
```

---

## 5. Verification Commands

The fix can be independently verified using the project test suites:

```powershell
# 1. Verify Tier 1 Feature 01 now passes
$env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestE2E_Tier1$/Feature01' ./internal/httpapi

# 2. Verify all M1 integration and route tests pass
$env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestM1_' ./internal/httpapi

# 3. Verify all M1 challenger suites (stress, zero 503, index, health) pass
$env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestChallengerM1_' ./internal/httpapi

# 4. Verify mongostore unit tests
go test -v -p 1 ./internal/mongostore/...

# 5. Verify workspace tests
$env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestMongoWorkspace$' ./internal/httpapi

# 6. Verify frontend node tests
node --test test/*.test.js
```
