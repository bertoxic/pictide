# Handoff Report: Router Fix Strategy for Public Guest Uploads (M1 Retry)

## 1. Observation

1. **Reproduction of Failure**:
   Ran:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestE2E_Tier1$/Feature01' ./internal/httpapi
   ```
   Direct output:
   ```
   === RUN   TestE2E_Tier1
   === RUN   TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher
       e2e_tier1_test.go:48: public endpoint POST /api/galleries/779dd780d9df05e5321282c090237f3022112f3de2061d7c81c19b2fb188c968/guest-uploads incorrectly required authentication (401)
   --- FAIL: TestE2E_Tier1 (4.46s)
       --- FAIL: TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher (0.37s)
   FAIL
   FAIL	pictide.local/studio/internal/httpapi	4.569s
   FAIL
   ```

2. **Source Code Inspection in `internal/httpapi/server.go`**:
   - `server.go:490-505`:
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
   - `server.go:521-533`:
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

3. **Call Site Analysis**:
   - `grep_search` confirmed `isAuthRoute` is an unexported helper only referenced once in the entire repository at `server.go:492`.
   - `parts[1] == "guest-uploads"` is grouped alongside studio-authenticated actions (`cover`, `customization`, `card-design`, `photos`) without discriminating by HTTP method `r.Method`.

4. **Interface Contract & Feature Scope**:
   - `PROJECT.md` line 57:
     `Public endpoints (/api/register, /api/account/verify, /api/account/forgot, /api/account/reset, /api/galleries/{id}/guest-uploads) MUST NOT require authentication session.`
   - `PROJECT.md` line 50:
     `Feature 17: Guest Uploads Listing: Studio owner endpoint GET /api/galleries/{id}/guest-uploads lists submissions pending review.`
   - In M1, public endpoints not yet implemented (`/api/register`, `/api/account/verify`, etc.) fall through to HTTP 404 (`server.go:285`), which correctly satisfies `TestE2E_Tier1/Feature01` without returning 401.

---

## 2. Logic Chain

1. In `server.go:274-286`, requests flow through `publicAPI` -> `authenticatedAPI` -> 404 catch-all.
2. In Milestone M1, `publicAPI` does not handle `POST /api/galleries/:id/guest-uploads` because guest upload submission logic is scheduled for Milestone M4 (`PROJECT.md` line 50).
3. The request reaches `authenticatedAPI`, which calls `isAuthRoute(p)`.
4. Because `isAuthRoute(p)` matches `parts[1] == "guest-uploads"` solely by URL path, it returns `true` for all HTTP methods (including `POST`).
5. When an unauthenticated visitor posts a guest upload (`user == nil`), `authenticatedAPI` terminates the request with HTTP 401 `"Please sign in to your studio."`.
6. This directly violates the interface contract defined in `PROJECT.md` line 57 and causes `TestE2E_Tier1/Feature01` line 48 to fail.
7. Furthermore, `GET /api/galleries/:id/guest-uploads` is a studio-owner protected route for listing pending guest uploads. Therefore, `GET` MUST remain authenticated.
8. Updating `isAuthRoute` to accept `*http.Request` and checking `case "guest-uploads": return r.Method != http.MethodPost` cleanly exempts `POST` from authentication (letting it return 404 in M1 and route to `publicAPI` in M4) while retaining mandatory 401 authentication protection for `GET`.

---

## 3. Caveats

1. **Milestone M4 Forward Scope**: In M1, exempting `POST /api/galleries/:id/guest-uploads` from `isAuthRoute` causes unauthenticated `POST` requests to return HTTP 404 `"Endpoint not found."` rather than 200. This is the authentic, correct behavior in M1 because the GridFS multipart upload handler belongs to Milestone M4. Implementing a fake mock 200 response in M1 would be an integrity violation.
2. **Alternative Approaches**: Option 2 (inserting a method check directly in `authenticatedAPI:492`) produces identical HTTP behavior with a 1-line diff, but Option 1 is architecturally cleaner as it consolidates all route matching logic in `isAuthRoute`. Both options are documented in `fix_strategy.md`.

---

## 4. Conclusion

The routing failure in `TestE2E_Tier1/Feature01` is caused by `isAuthRoute` treating `POST /api/galleries/:id/guest-uploads` as an authenticated studio route. 

The exact fix is:
1. Update `isAuthRoute(r *http.Request) bool` in `internal/httpapi/server.go`.
2. In `authenticatedAPI`, invoke `if !isAuthRoute(r) { return false }`.
3. In `isAuthRoute`, separate `"guest-uploads"` from the unconditional list and evaluate `case "guest-uploads": return r.Method != http.MethodPost`.
4. In `internal/httpapi/m1_test.go`, add unit test `TestM1_GuestUploads_PublicVsAuthenticatedRouting` to verify this contract Hermetically.

Complete code diffs and implementation details are provided in `fix_strategy.md`.

---

## 5. Verification Method

Independent verification can be executed with the following commands:

1. **Verify E2E Feature 01 Pass**:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestE2E_Tier1$/Feature01' ./internal/httpapi
   ```
   *Pass condition*: `TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher` passes with status `PASS`.

2. **Verify Full M1 Test Suite**:
   ```powershell
   go test -v -p 1 ./internal/mongostore/...
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestM1_' ./internal/httpapi
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestChallengerM1_' ./internal/httpapi
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestMongoWorkspace$' ./internal/httpapi
   node --test test/*.test.js
   go vet ./...
   ```
   *Pass condition*: Zero failures across all unit, challenger, and frontend tests.
