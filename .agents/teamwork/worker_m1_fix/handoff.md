# Handoff Report: Milestone M1 Public Guest-Uploads Routing Fix

## 1. Observation

1. **Initial Defect Reproduction**:
   - Running `$env:PICTIDE_TEST_MONGODB="1"; go test -v -run "^TestE2E_Tier1$/Feature01" ./internal/httpapi` resulted in:
     ```
     === RUN   TestE2E_Tier1
     === RUN   TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher
         e2e_tier1_test.go:48: public endpoint POST /api/galleries/901626004568cf2eeff59cb4a4c362f5c2c0d173965df17c99a5f6b36d97d900/guest-uploads incorrectly required authentication (401)
     --- FAIL: TestE2E_Tier1 (4.63s)
         --- FAIL: TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher (0.39s)
     FAIL
     FAIL	pictide.local/studio/internal/httpapi	4.710s
     ```
2. **Defect Site in Source**:
   - In `internal/httpapi/server.go:490-505`: `authenticatedAPI` checked `if !isAuthRoute(p) { return false }` and if `user == nil`, returned `fail(w, 401, "Please sign in to your studio.")`.
   - In `internal/httpapi/server.go:528-531`: `isAuthRoute` matched `guest-uploads` unconditionally as an authenticated route:
     ```go
     case "direct-upload", "cover", "customization", "card-design", "qr", "print", "photos", "guest-uploads":
         return true
     ```
3. **Applied Modifications**:
   - `internal/httpapi/server.go`:
     - Line 492: Updated `authenticatedAPI` to check `if !isAuthRoute(r.Method, p)`.
     - Line 507: Updated `isAuthRoute` signature to `func isAuthRoute(method, p string) bool`.
     - Line 528-534: Separated `guest-uploads` from the unconditional true list and set:
       ```go
       case "direct-upload", "cover", "customization", "card-design", "qr", "print", "photos":
           return true
       case "guest-uploads":
           return method != http.MethodPost
       ```
   - `internal/httpapi/m1_test.go`:
     - Appended `TestM1_PublicGuestUploads_AuthBypass` covering unauthenticated `POST` (does not return 401 or 503), unauthenticated `GET` (returns 401), authenticated `GET` (passes auth without 401), and a comprehensive check of all public endpoints from `PROJECT.md` line 57.

---

## 2. Logic Chain

1. `PROJECT.md` line 57 states: "Public endpoints (`/api/register`, `/api/account/verify`, `/api/account/forgot`, `/api/account/reset`, `/api/galleries/{id}/guest-uploads`) MUST NOT require authentication session."
2. Feature 16 (`POST /api/galleries/{id}/guest-uploads`) is the public guest upload submission endpoint, whereas Feature 17 (`GET /api/galleries/{id}/guest-uploads`) is the authenticated studio review endpoint.
3. Prior to the fix, `isAuthRoute(p)` only looked at the URL path `/api/galleries/:id/guest-uploads` without inspecting `r.Method`.
4. As a result, when an unauthenticated guest performed a `POST` request to `/api/galleries/:id/guest-uploads`, `isAuthRoute` returned `true`, causing `authenticatedAPI` to reject the request with HTTP 401 `"Please sign in to your studio."`.
5. By updating `isAuthRoute` to accept `method` and evaluate `return method != http.MethodPost` for `guest-uploads`:
   - `POST` requests are NOT classified as auth routes, allowing them through without requiring session credentials (returning HTTP 404 in M1 before Feature 16 is implemented in M4).
   - `GET` and other methods remain classified as authenticated routes, properly returning HTTP 401 if unauthenticated.
6. The regression test `TestM1_PublicGuestUploads_AuthBypass` confirms both directions (unauthenticated POST bypasses auth challenge; unauthenticated GET requires auth challenge).

---

## 3. Caveats

1. Feature 16 (`POST /api/galleries/:id/guest-uploads`) itself is scheduled for full implementation in Milestone M4. In Milestone M1, unauthenticated `POST /api/galleries/:id/guest-uploads` returns HTTP 404 `"Endpoint not found."` because `publicAPI` does not handle it yet. This correctly satisfies `TestE2E_Tier1/Feature01`.
2. No other endpoints required changes.

---

## 4. Conclusion

The fix successfully resolves the premature 401 rejection for unauthenticated `POST /api/galleries/:id/guest-uploads` while maintaining strict authentication requirements for `GET /api/galleries/:id/guest-uploads` and other studio endpoints. All targeted and dependent test suites pass with zero regressions.

---

## 5. Verification Method

Independent verification commands executed with outputs:

1. **Verify New M1 Regression Test**:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run "^TestM1_PublicGuestUploads_AuthBypass$" ./internal/httpapi
   ```
   *Result*: PASS (`ok pictide.local/studio/internal/httpapi 3.528s`).

2. **Verify All M1 Test Suites**:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run "^TestM1_" ./internal/httpapi
   ```
   *Result*: All 6 tests pass (`ok pictide.local/studio/internal/httpapi 25.698s`).

3. **Verify E2E Tier 1 Feature 01**:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run "TestE2E_Tier1/Feature01" ./internal/httpapi
   ```
   *Result*: PASS (`ok pictide.local/studio/internal/httpapi 4.325s`).

4. **Verify Frontend Test Suite**:
   ```powershell
   node --test test/*.test.js
   ```
   *Result*: 17/17 tests pass.

5. **Verify Challenger M1 Stress & Boundary Suite**:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run "^TestChallengerM1_" ./internal/httpapi
   ```
   *Result*: All challenger tests pass (`ok pictide.local/studio/internal/httpapi 119.464s`).

6. **Verify MongoStore and Workspace Suites**:
   ```powershell
   go test -v -p 1 ./internal/mongostore/...
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run "^TestMongoWorkspace$" ./internal/httpapi
   ```
   *Result*: All pass.

7. **Code Linter**:
   ```powershell
   go vet ./internal/httpapi
   ```
   *Result*: Exit code 0, clean.
