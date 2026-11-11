# Handoff: Explorer M1 Fix 2 — Public Route Audit & Gateway Routing Strategy

## 1. Observation

1. **Reproduction Command and Output**:
   Ran `$env:PICTIDE_TEST_MONGODB="1"; go test -v -run "^TestE2E_Tier1$/Feature01" ./internal/httpapi`:
   ```
   === RUN   TestE2E_Tier1
   === RUN   TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher
       e2e_tier1_test.go:48: public endpoint POST /api/galleries/b5122bbe0e1ac4d71ebf8eed1761edfa99431169cd0c696be3a1d9245cb60c21/guest-uploads incorrectly required authentication (401)
   --- FAIL: TestE2E_Tier1 (4.53s)
       --- FAIL: TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher (0.37s)
   FAIL
   FAIL	pictide.local/studio/internal/httpapi	4.622s
   FAIL
   ```
2. **Defect Location**:
   In `internal/httpapi/server.go:490-505`:
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
   	// Recognized authenticated route path, but unsupported HTTP method
   	fail(w, 405, "Method not allowed.")
   	return true
   }
   ```
   In `internal/httpapi/server.go:521-533`:
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
3. **Public Endpoint Audit Observations**:
   Audited all public endpoints in `PROJECT.md § Feature Inventory` and `internal/httpapi/server.go`:
   - `/api/config` (GET) — handled in `publicAPI:291-307`, not caught by `isAuthRoute`.
   - `/api/v1/health` (GET) — handled in `publicAPI:309-329`, not caught by `isAuthRoute`.
   - `/api/login` (POST) — handled in `publicAPI:331-337`, not caught by `isAuthRoute`.
   - `/api/logout` (POST) — handled in `publicAPI:339-353`, not caught by `isAuthRoute`.
   - `/api/session` (GET) — handled in `publicAPI:355-365`, not caught by `isAuthRoute`.
   - `/api/checkout` (POST) — handled in `publicAPI:367-373`, not caught by `isAuthRoute`.
   - `/api/v1/webhooks/paystack` (POST) — handled in `publicAPI:375-381`, not caught by `isAuthRoute`.
   - `/api/checkout/:id/verify` (POST) — handled in `publicAPI:383-393`, not caught by `isAuthRoute`.
   - `/api/photos/:id/download-link` (POST) — handled in `publicAPI:395-405`, not caught by `isAuthRoute`.
   - `/api/download/:id` (GET) — handled in `publicAPI:407-417`, not caught by `isAuthRoute`.
   - `/api/photos/:id/free` & `/clean` (GET) — handled in `publicAPI:419-429`, not caught by `isAuthRoute`.
   - `/api/galleries/:id` (GET) — handled in `publicAPI:431-450`, not blocked by `user == nil`.
   - `/api/galleries/:id/free-download` (GET) — handled in `publicAPI:451-458`, not caught by `isAuthRoute`.
   - `/api/galleries/:id/qr` (GET) — handled in `publicAPI:459-484`, not blocked by `user == nil`.
   - `/api/register` (POST) — not in `isAuthRoute`, returns clean 404 in M1.
   - `/api/account/verify` (POST) — not in `isAuthRoute`, returns clean 404 in M1.
   - `/api/account/forgot` (POST) — not in `isAuthRoute`, returns clean 404 in M1.
   - `/api/account/reset` (POST) — not in `isAuthRoute`, returns clean 404 in M1.
   - `/api/guest-uploads/status` (POST) — not in `isAuthRoute`, returns clean 404 in M1.
   - `/api/galleries/:id/guest-uploads` (POST) — caught by `isAuthRoute:529`, prematurely rejected with 401 when `user == nil`.

---

## 2. Logic Chain

1. From Observation 1, `TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher` sends unauthenticated requests to public endpoints, expecting no 401 `"Please sign in to your studio."` rejection.
2. From Observation 2, `isAuthRoute` checks path `p` and returns `true` for all requests targeting `/api/galleries/:id/guest-uploads` regardless of `r.Method`.
3. Because `publicAPI` does not handle `POST /api/galleries/:id/guest-uploads` in M1 (planned for M4), execution falls through to `authenticatedAPI`.
4. In `authenticatedAPI`, `isAuthRoute(p)` evaluates to `true`.
5. When `user == nil` (unauthenticated guest), `authenticatedAPI` immediately executes `fail(w, 401, "Please sign in to your studio.")`.
6. According to `PROJECT.md` line 57 ("Public endpoints (`/api/register`, `/api/account/verify`, `/api/account/forgot`, `/api/account/reset`, `/api/galleries/{id}/guest-uploads`) MUST NOT require authentication session"), `POST /api/galleries/:id/guest-uploads` is an unauthenticated public submission endpoint.
7. In contrast, `GET /api/galleries/:id/guest-uploads` is an authenticated studio endpoint for the photographer to review submitted photos (`public/enhancements.js:60`).
8. Therefore, route classification requires discriminating by HTTP method: `POST /api/galleries/:id/guest-uploads` is NOT an authenticated route, while `GET` / `HEAD` IS an authenticated route.
9. From Observation 3, all other public endpoints are already completely free from unintentional 401 interception.
10. Updating `isAuthRoute(method, p string) bool` to return `method != http.MethodPost` for `guest-uploads` and updating `authenticatedAPI` to call `isAuthRoute(r.Method, p)` eliminates the premature 401 rejection for `POST /api/galleries/:id/guest-uploads` while retaining full studio authentication on `GET /api/galleries/:id/guest-uploads`.

---

## 3. Caveats

1. In Milestone M1, Feature 16 (`POST /api/galleries/:id/guest-uploads`) is not yet implemented (scheduled for M4). With this fix, unauthenticated `POST /api/galleries/:id/guest-uploads` returns clean HTTP 404 `"Endpoint not found."` in M1, which satisfies `TestE2E_Tier1/Feature01`. When Milestone M4 implements Feature 16 in `publicAPI`, it will return HTTP 200/201.
2. No other endpoints require modification.

---

## 4. Conclusion

The sole cause of the Milestone M1 gate rejection is the path-only matching in `isAuthRoute` for `guest-uploads`. Passing `r.Method` to `isAuthRoute(method, p string)` and returning `method != http.MethodPost` for `guest-uploads` cleanly resolves the defect, satisfies all contracts in `PROJECT.md`, and allows `TestE2E_Tier1/Feature01` to pass.

### Recommended Drop-In Diff for Worker:

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

## 5. Verification Method

1. **Verify Tier 1 Feature 01 test**:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestE2E_Tier1$/Feature01' ./internal/httpapi
   ```
   *Expected outcome*: Passes without any 401 error on `POST /api/galleries/:id/guest-uploads`.
2. **Verify all M1 integration and route tests**:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestM1_' ./internal/httpapi
   ```
3. **Verify all M1 challenger suites (stress, zero 503, index specifications, health)**:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestChallengerM1_' ./internal/httpapi
   ```
4. **Verify mongostore unit tests**:
   ```powershell
   go test -v -p 1 ./internal/mongostore/...
   ```
5. **Verify workspace tests**:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestMongoWorkspace$' ./internal/httpapi
   ```
6. **Verify frontend tests**:
   ```powershell
   node --test test/*.test.js
   ```
7. **Invalidation Condition**:
   If any public endpoint returns HTTP 401 when accessed without session, or if `GET /api/galleries/:id/guest-uploads` stops requiring authentication, this fix is invalidated.
