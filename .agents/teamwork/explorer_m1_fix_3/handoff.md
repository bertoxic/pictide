# Handoff Report: Milestone M1 Fix 3 — Public Guest Upload Routing Auth Bypass

## 1. Observation

1. **Reproduction of Test Failure**:
   Executed command:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestE2E_Tier1$/Feature01' ./internal/httpapi
   ```
   Verbatim output:
   ```
   === RUN   TestE2E_Tier1
   === RUN   TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher
       e2e_tier1_test.go:48: public endpoint POST /api/galleries/a38cfc27b7b5a8af0eddca7a3c9bb6571dbc1a9e5361728b2228c33d5dde600c/guest-uploads incorrectly required authentication (401)
   --- FAIL: TestE2E_Tier1 (4.90s)
       --- FAIL: TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher (0.37s)
   FAIL
   FAIL	pictide.local/studio/internal/httpapi	5.008s
   FAIL
   ```

2. **Relevant Code Locations**:
   - `internal/httpapi/e2e_tier1_test.go:37, 46-50`:
     ```go
     {"POST", "/api/galleries/" + galleryID + "/guest-uploads", ``},
     ...
     for _, ep := range publicEndpoints {
         w := env.request(ep.method, ep.path, []byte(ep.body), "application/json", nil)
         if w.Code == http.StatusUnauthorized && strings.Contains(w.Body.String(), "Please sign in to your studio") {
             t.Errorf("public endpoint %s %s incorrectly required authentication (401)", ep.method, ep.path)
         }
     }
     ```
   - `internal/httpapi/server.go:490-505`:
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
   - `internal/httpapi/server.go:521-533`:
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
   - `PROJECT.md:57`:
     ```markdown
     - Public endpoints (/api/register, /api/account/verify, /api/account/forgot, /api/account/reset, /api/galleries/{id}/guest-uploads) MUST NOT require authentication session.
     ```
   - `PROJECT.md:28-29`:
     ```markdown
     | 16 | Guest Upload Submission | POST /api/galleries/{id}/guest-uploads public endpoint to submit photos to GridFS and guest_uploads collection | M4 | survey |
     | 17 | Guest Uploads Listing | GET /api/galleries/{id}/guest-uploads studio endpoint to list submissions | M4 | survey |
     ```

---

## 2. Logic Chain

1. **Contract Specification** (Observation 2: `PROJECT.md:28-29, 57`):
   - `POST /api/galleries/:id/guest-uploads` is a public endpoint used by guests to upload photos without logging into a studio account.
   - `GET /api/galleries/:id/guest-uploads` is an authenticated studio endpoint used by the photographer to inspect submissions.
   - The contract explicitly prohibits requiring studio authentication for `POST /api/galleries/{id}/guest-uploads`.
2. **Execution Path to Failure** (Observation 2: `server.go:490-533`):
   - When an unauthenticated visitor sends `POST /api/galleries/:id/guest-uploads`, `publicAPI(w, r, user)` does not intercept it in M1 (handler deferred to M4).
   - Execution passes to `authenticatedAPI(w, r, user)`.
   - `authenticatedAPI` evaluates `isAuthRoute(p)`.
   - `isAuthRoute` matches `/api/galleries/:id/guest-uploads` and returns `true` unconditionally, without inspecting `r.Method`.
   - Because the guest caller has no studio cookie, `user == nil`.
   - `authenticatedAPI` executes `fail(w, 401, "Please sign in to your studio.")`.
3. **Failure in Test** (Observation 1: `e2e_tier1_test.go:48`):
   - `TestE2E_Tier1/Feature01` verifies that public endpoints do not return 401 `"Please sign in to your studio"`.
   - The premature 401 rejection fails the test.
4. **Resolution Strategy**:
   - `isAuthRoute` must receive the HTTP method: `isAuthRoute(method, p string) bool`.
   - For `parts[1] == "guest-uploads"`, `isAuthRoute` must return `method != http.MethodPost`.
   - For `POST /api/galleries/:id/guest-uploads`, `isAuthRoute` returns `false`, causing `authenticatedAPI` to return `false`.
   - The request falls through to `fail(w, 404, "Endpoint not found.")` in M1 (or into `publicAPI` when M4 is implemented).
   - Because HTTP 404 != HTTP 401, `TestE2E_Tier1/Feature01` passes.
   - For `GET /api/galleries/:id/guest-uploads`, `isAuthRoute` continues to return `true`, correctly requiring studio authentication (401 when unauthenticated).

---

## 3. Caveats

1. In Milestone M1, `POST /api/galleries/:id/guest-uploads` is not yet functionally implemented (it is Milestone M4 Feature 16). Returning 404 (or any non-401, non-503 status) is the intended M1 behavior.
2. In Milestone M4, `publicAPI` will be extended to parse multipart form payloads and persist records into GridFS and `guest_uploads`. The method discrimination in `isAuthRoute` designed here is forward-compatible and requires no subsequent re-work in M4.
3. No other endpoints in `isAuthRoute` are affected.

---

## 4. Conclusion

- Root cause is conclusively identified: `isAuthRoute` in `internal/httpapi/server.go:529` unconditionally classifies `guest-uploads` as an authenticated route without checking for `http.MethodPost`.
- The fix is minimal and localized (2 lines changed in `server.go`).
- A dedicated test `TestM1_PublicGuestUploads_AuthBypass` has been formulated for `internal/httpapi/m1_test.go` to permanently guard against regression across all public endpoints and ensure differential behavior between `POST` (public) and `GET` (authenticated).
- Full implementation plan and diffs are documented in `fix_strategy.md`.

---

## 5. Verification Method

1. **Verify E2E Feature 01 Pass**:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestE2E_Tier1$/Feature01' ./internal/httpapi
   ```
   *Pass Condition*: Output contains `--- PASS: TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher` and exits with code 0.

2. **Verify New M1 Regression Test**:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestM1_PublicGuestUploads_AuthBypass$' ./internal/httpapi
   ```
   *Pass Condition*: All subtests (`Unauthenticated POST`, `Unauthenticated GET`, `Authenticated GET`, `ProjectContract_PublicEndpoints_NoAuthRequired`) PASS.

3. **Verify All M1 and Challenger Test Suites**:
   ```powershell
   go test -count=1 -v -p 1 ./internal/mongostore/...
   $env:PICTIDE_TEST_MONGODB="1"; go test -count=1 -v -run '^TestM1_' ./internal/httpapi
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestChallengerM1_' ./internal/httpapi
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestMongoWorkspace$' ./internal/httpapi
   node --test test/*.test.js
   go vet ./...
   ```
   *Pass Condition*: All exit with code 0, 0 failures.
