# Milestone M1: Core DB Schemas, Indexes & API Gateway Routing — Review & Challenge Report

## Review Summary

**Verdict**: **REQUEST_CHANGES**

Integrity Assessment: **PASS** (Zero integrity violations. No hardcoded facades, genuine MongoDB driver v2 integration, and authentic test executions).

Quality Assessment: **95% compliant**, with a single blocking route dispatching bug where `POST /api/galleries/:id/guest-uploads` is incorrectly flagged as an authenticated studio route, causing public guest submissions to be rejected with HTTP 401.

---

## 1. Observation

1. **Test Execution Observations**:
   - `go test -count=1 -v -p 1 ./internal/mongostore/...`:
     ```
     === RUN   TestWithdrawalBSONRoundtrip
     --- PASS: TestWithdrawalBSONRoundtrip (0.00s)
     === RUN   TestGuestUploadBSONRoundtrip
     --- PASS: TestGuestUploadBSONRoundtrip (0.00s)
     === RUN   TestAccountLinkBSONRoundtrip
     --- PASS: TestAccountLinkBSONRoundtrip (0.00s)
     === RUN   TestAdminAuditBSONRoundtrip
     --- PASS: TestAdminAuditBSONRoundtrip (0.00s)
     === RUN   TestStoreConstructorsRequireDB
     --- PASS: TestStoreConstructorsRequireDB (0.00s)
     PASS
     ok  	pictide.local/studio/internal/mongostore	0.904s
     ```
   - `$env:PICTIDE_TEST_MONGODB="1"; go test -count=1 -v -run '^TestM1_' ./internal/httpapi`:
     ```
     === RUN   TestM1_DatabaseAndIndexes
     --- PASS: TestM1_DatabaseAndIndexes (4.14s)
     === RUN   TestM1_DynamicConfig
     --- PASS: TestM1_DynamicConfig (7.26s)
     === RUN   TestM1_DynamicHealth
     --- PASS: TestM1_DynamicHealth (3.28s)
     === RUN   TestM1_UnrecognizedRoutes_Return404
     --- PASS: TestM1_UnrecognizedRoutes_Return404 (3.63s)
     === RUN   TestM1_MethodNotAllowed_Return405
     --- PASS: TestM1_MethodNotAllowed_Return405 (3.56s)
     PASS
     ok  	pictide.local/studio/internal/httpapi	22.014s
     ```
   - `$env:PICTIDE_TEST_MONGODB="1"; go test -count=1 -v -run '^TestMongoWorkspace$' ./internal/httpapi`:
     ```
     === RUN   TestMongoWorkspace
     --- PASS: TestMongoWorkspace (13.03s)
     PASS
     ok  	pictide.local/studio/internal/httpapi	13.132s
     ```
   - `node --test test/*.test.js`:
     ```
     ℹ tests 17
     ℹ suites 0
     ℹ pass 17
     ℹ fail 0
     ℹ duration_ms 293.0506
     ```
   - `go vet ./...`:
     ```
     PASS (exited with code 0)
     ```
   - `$env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestChallengerM1_' ./internal/httpapi`:
     ```
     === RUN   TestChallengerM1_IndexSpecifications_EmpiricalInspection
     --- PASS: TestChallengerM1_IndexSpecifications_EmpiricalInspection (4.85s)
     === RUN   TestChallengerM1_IdempotentIndexCreation
     --- PASS: TestChallengerM1_IdempotentIndexCreation (6.18s)
     === RUN   TestChallengerM1_ConcurrentDuplicateRejection
     --- PASS: TestChallengerM1_ConcurrentDuplicateRejection (5.03s)
     === RUN   TestChallengerM1_TTLIndex_And_TimeQueries
     --- PASS: TestChallengerM1_TTLIndex_And_TimeQueries (2.56s)
     === RUN   TestChallengerM1_StoreConstructors_NilSafety
     --- PASS: TestChallengerM1_StoreConstructors_NilSafety (1.67s)
     === RUN   TestChallengerM1_HealthCheck_SeveredClient
     --- PASS: TestChallengerM1_HealthCheck_SeveredClient (3.25s)
     === RUN   TestChallengerM1_StoreRepositoryLifecycle
     --- PASS: TestChallengerM1_StoreRepositoryLifecycle (3.76s)
     PASS
     ok  	pictide.local/studio/internal/httpapi	27.420s
     ```

2. **Observed Failure in Gateway Routing Verification**:
   - Running `$env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestE2E_Tier1$/Feature01' ./internal/httpapi`:
     ```
     === RUN   TestE2E_Tier1
     === RUN   TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher
         e2e_tier1_test.go:48: public endpoint POST /api/galleries/afee7cdd3a84443dd082b9d24dcee3a4ac19f9af2bfe0e5bab0b83c1cf2dfc22/guest-uploads incorrectly required authentication (401)
     --- FAIL: TestE2E_Tier1 (4.92s)
         --- FAIL: TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher (0.37s)
     FAIL
     ```
3. **Exact Code Location of Defect**:
   - In `internal/httpapi/server.go:521-533`:
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
   - In `internal/httpapi/server.go:490-505`:
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

---

## 2. Logic Chain

1. **Contract Violation**:
   - `PROJECT.md` § Interface Contracts (line 57) explicitly defines:
     > "Public endpoints (`/api/register`, `/api/account/verify`, `/api/account/forgot`, `/api/account/reset`, `/api/galleries/{id}/guest-uploads`) MUST NOT require authentication session."
   - Feature 01 of Milestone M1 specifies:
     > "Public & Authenticated Routing Dispatcher: Route public vs authenticated `/api/` endpoints cleanly in `server.go`, removing catch-all 503 'This feature is still being converted to MongoDB.'"
2. **Failure Analysis**:
   - In `server.go`, `isAuthRoute` checks the string path `p` and includes `"guest-uploads"` under `/api/galleries/:id/` without discriminating by HTTP method.
   - When an unauthenticated visitor issues `POST /api/galleries/:id/guest-uploads`, `publicAPI(w, r, user)` does not handle this path in M1, so execution passes to `authenticatedAPI(w, r, user)`.
   - `authenticatedAPI` observes `isAuthRoute(p) == true`.
   - Because the guest user is unauthenticated (`user == nil`), `authenticatedAPI` immediately aborts with `fail(w, 401, "Please sign in to your studio.")`.
   - As a result, public guest uploads are completely blocked by premature authentication rejection, failing `TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher`.
3. **Required Fix**:
   - While `GET /api/galleries/:id/guest-uploads` is indeed an authenticated studio route (for the studio owner to inspect submissions in `workspace.go:52`), `POST /api/galleries/:id/guest-uploads` is a public guest upload submission route.
   - `isAuthRoute` or `authenticatedAPI` must exempt `POST` requests targeting `/api/galleries/:id/guest-uploads` from the studio authentication requirement so that unauthenticated guest requests are not prematurely rejected with 401.

---

## 3. Findings

### [Major] Finding 1: Public Guest Upload Endpoint Blocked by Studio Authentication (401)

- **What**: `POST /api/galleries/:id/guest-uploads` returns HTTP 401 `"Please sign in to your studio."` when accessed without an authenticated studio session.
- **Where**: `internal/httpapi/server.go:492-498` and line `529`.
- **Why**: Violates `PROJECT.md` line 57 interface contract and causes `TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher` to fail.
- **Suggestion**:
  In `internal/httpapi/server.go`:
  Ensure `isAuthRoute` or `authenticatedAPI` distinguishes `POST` from `GET` for `guest-uploads`. For example, in `authenticatedAPI`:
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
  ...
  ```
  Or adjust `isAuthRoute(r *http.Request)` so that `/guest-uploads` is only an auth route for `GET` / `HEAD`.

---

## 4. Verified Claims

- **Zero Integrity Violations**: Verified. No fake/dummy stubs; models and repositories use actual `go.mongodb.org/mongo-driver/v2` collections and transactions; no hardcoded test responses.
- **Database Index Provisioning**: Verified. Startup `New` creates unique indexes on `accounts.username`, sparse on `accounts.email`, unique and TTL on `account_links.token` and `expires_at`, unique on `withdrawals.id`, compound on `withdrawals.studio_id` + `created_at`, unique on `guest_uploads.id`, and unique on `admin_audit.id`. Verified empirically against live MongoDB via `TestChallengerM1_IndexSpecifications_EmpiricalInspection`.
- **Dynamic Configuration Endpoint**: Verified. `/api/config` dynamically computes `payments`, `public_url`, `email_configured` (`SMTPHost != ""`), `imgbb_previews` (`ImgBBKey != ""`), and `r2_uploads` (`r2 != nil`).
- **Dynamic Live Health Check**: Verified. `/api/v1/health` sends a bounded ping to MongoDB; returns 200 `connected` when live and 503 `disconnected` when the client connection is severed. Verified via `TestChallengerM1_HealthCheck_SeveredClient`.
- **Catch-All 503 Stub Elimination**: Verified. The legacy 503 stub `"This feature is still being converted to MongoDB."` has been removed from `server.go`. Unrecognized routes cleanly return 404 `"Endpoint not found."` and unsupported methods return 405 `"Method not allowed."`.
- **Hermetic Unit Tests**: Verified. `internal/mongostore/models_test.go` verifies BSON marshal/unmarshal roundtrips and nil-safety across all 4 domain models.
- **Frontend Contract Conformance**: Verified. 17/17 tests in `test/*.test.js` pass.

---

## 5. Adversarial Challenge & Stress-Testing

### Challenge Summary
- **Overall risk assessment**: **LOW** (except for the routing bug identified in Finding 1). The database persistence layer, nil safety, and concurrency controls are robust.

### Challenges Evaluated:
1. **Concurrent Duplicate Inserts (Race Condition Stress-Testing)**:
   - *Attack scenario*: 10 concurrent goroutines racing to insert identical records into `accounts`, `withdrawals`, `guest_uploads`, and `account_links`.
   - *Result*: Exactly 1 insert succeeded and 9 failed with duplicate key errors. MongoDB unique constraints held perfectly (`TestChallengerM1_ConcurrentDuplicateRejection`).
2. **Severed Database Connection During Health Probe**:
   - *Attack scenario*: Mongo client connection severed via `client.Disconnect` while server is running.
   - *Result*: `/api/v1/health` immediately detected the severed connection and returned 503 `status: unavailable, database: disconnected` (`TestChallengerM1_HealthCheck_SeveredClient`).
3. **TTL Background Monitor Window Leakage**:
   - *Attack scenario*: A token is expired by timestamp, but MongoDB's background TTL reaper thread (which runs periodically every 60s) has not yet purged the document.
   - *Result*: `AccountLinks.GetValid` includes `"expires_at": bson.M{"$gt": time.Now()}` at query time, completely preventing expired token reuse even before background deletion occurs (`TestChallengerM1_TTLIndex_And_TimeQueries`).
4. **Store Nil-Safety and Bounds Stress-Testing**:
   - *Attack scenario*: Constructing repositories with `nil` database or invoking CRUD methods with empty IDs, references, or nil items.
   - *Result*: All constructors safely return descriptive errors, and all mutation/query methods reject nil or malformed inputs without panics (`TestChallengerM1_StoreConstructors_NilSafety`).

---

## 6. Caveats

- `TestE2E_Tier1` contains assertions for milestones M2 through M6 (e.g. registration, payout webhook, branding); failures in those downstream features are expected until their respective milestones are delivered. Only the Feature 01 gateway routing failure on `POST /api/galleries/:id/guest-uploads` is in scope for M1.

---

## 7. Conclusion

Milestone M1 architecture, schema provisioning, models, dynamic health/config endpoints, and unit test suites are well-engineered, verified, and free of integrity shortcuts. However, because `server.go` prematurely requires studio authentication for `POST /api/galleries/:id/guest-uploads`, breaking the interface contract and failing `TestE2E_Tier1/Feature01`, the verdict is **REQUEST_CHANGES**. Once this routing filter is adjusted to exempt unauthenticated `POST` guest uploads, M1 will be ready for immediate approval.

---

## 8. Verification Method

1. Run Tier 1 Feature 01 test to verify the failure:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestE2E_Tier1$/Feature01' ./internal/httpapi
   ```
2. Verify all M1 integration and challenger test suites:
   ```powershell
   go test -v -p 1 ./internal/mongostore/...
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestM1_' ./internal/httpapi
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestChallengerM1_' ./internal/httpapi
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestMongoWorkspace$' ./internal/httpapi
   node --test test/*.test.js
   ```
