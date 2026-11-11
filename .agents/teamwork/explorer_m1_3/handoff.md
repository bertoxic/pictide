# Handoff Report: Dynamic Config, Health & Automated Tests Strategy (M1.3)

**Agent**: `explorer_m1_3` (teamwork_explorer)  
**Recipient**: `parent` (ID: `963791b5-32ce-4fe6-ad97-67f5764269a0`) / Worker  
**Working Directory**: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_3\`  
**Milestone**: M1 (Core DB Schemas, Indexes & API Gateway Routing)  

---

## 1. Observation

1. **Hardcoded `/api/config` Response**:
   In `internal/httpapi/server.go:201-208`:
   ```go
   p := r.URL.Path
   if p == "/api/config" && r.Method == "GET" {
       mode := "unavailable"
       if s.cfg.PaystackSecret != "" {
           mode = "paystack"
       }
       reply(w, 200, map[string]any{"payments": mode, "public_url": s.cfg.PublicURL, "email_configured": false, "imgbb_previews": false, "r2_uploads": s.r2 != nil})
       return
   }
   ```
   `email_configured` and `imgbb_previews` are hardcoded to `false` regardless of environment settings.

2. **Hardcoded `/api/v1/health` Response**:
   In `internal/httpapi/server.go:209-212`:
   ```go
   if p == "/api/v1/health" && r.Method == "GET" {
       reply(w, 200, map[string]string{"app": "pictide", "status": "partial"})
       return
   }
   ```
   The health check returns a static `"status": "partial"` without verifying connectivity to the configured MongoDB cluster.

3. **Field Naming in `config.Config`**:
   In `internal/config/config.go:19-20`:
   ```go
   ImgBBKey      string
   ImgBBPreviews bool
   ```
   The configuration field is named `ImgBBKey` (populated from `IMGBB_API_KEY` at line 38: `ImgBBKey: os.Getenv("IMGBB_API_KEY")`). Any code referencing `s.cfg.ImgBBApiKey` will fail Go compilation with an undefined field error.

4. **Catch-All 503 Fallthrough in `server.go`**:
   In `internal/httpapi/server.go:322-332`:
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
   Any authenticated request reaching an unrecognized route under `/api/` triggers HTTP 503 with the placeholder message `"This feature is still being converted to MongoDB."`.

5. **Existing Live MongoDB Integration Test Passing**:
   Executing `$env:PICTIDE_TEST_MONGODB="1"; go test -v -run TestMongoWorkspace ./internal/httpapi` passed in 13.31s using credentials from `.env` (`MONGODB_URI="mongodb+srv://..."`).

---

## 2. Logic Chain

1. **Dynamic Config Resolution**:
   - From Observation 1, the frontend relies on `/api/config` to detect capabilities (e.g. registration, recovery, image previews).
   - From Observation 3, the runtime config holds `s.cfg.SMTPHost` for email and `s.cfg.ImgBBKey` for image previews.
   - Therefore, updating `server.go:206` to evaluate `email_configured: s.cfg.SMTPHost != ""` and `imgbb_previews: s.cfg.ImgBBKey != ""` dynamically reflects active services without breaking Go compilation.

2. **Health Check MongoDB Ping**:
   - From Observation 2, health monitors currently receive a static `"partial"` status.
   - From Observation 5 and `internal/mongostore/connect.go:24`, MongoDB driver v2 supports `client.Ping(ctx, readpref.Primary())`.
   - Wrapping `s.db.Client().Ping` in a 2-second timeout context ensures `/api/v1/health` reports `{"app": "pictide", "status": "ok", "database": "connected"}` (HTTP 200) when healthy, or `{"app": "pictide", "status": "unavailable", "database": "disconnected"}` (HTTP 503) when degraded.

3. **Routing Error Correction**:
   - From Observation 4, unrecognized routes under `/api/` currently return HTTP 503.
   - Changing line 330 from `fail(w, 503, "This feature is still being converted to MongoDB.")` to `fail(w, 404, "Endpoint not found.")` correctly communicates 404 Not Found to clients while fulfilling acceptance criteria R1/R2.

4. **Automated M1 Verification Test Suite**:
   - To guarantee regression prevention and verify R3, a new test suite `internal/httpapi/m1_test.go` is designed with 4 sub-suites:
     1. `TestM1_DatabaseAndIndexes`: Provisions a random test database (`pictide_m1_test_<token>`), initializes `httpapi.New`, and tests index constraints (unique username, unique token, unique withdrawal id, unique guest upload id, admin audit).
     2. `TestM1_DynamicConfig`: Verifies all permutations (unconfigured, fully configured, email-only, imgbb-only).
     3. `TestM1_DynamicHealth`: Verifies HTTP 200 and JSON contract against live MongoDB, and HTTP 503 when database is disconnected.
     4. `TestM1_UnrecognizedRoutes_Return404`: Authenticates as admin and verifies unrecognized routes return HTTP 404 and zero occurrences of the 503 string.

---

## 3. Caveats

1. **Compilation Nuance on ImgBB Field Name**:
   Worker must not use `s.cfg.ImgBBApiKey`, which does not exist in `config.Config`. The field is `s.cfg.ImgBBKey`.
2. **Database Permissions**:
   The automated tests create and drop a temporary database per test run (`pictide_m1_test_<token>`). The MongoDB Atlas user configured in `.env` has permissions to create and drop collections and databases.
3. **Route Order Coordination with M1.1**:
   Explorer M1.1 is refactoring the public vs authenticated route order. The 404 fallthrough replacement at line 330 applies to all remaining routes under `/api/`.

---

## 4. Conclusion

The strategy for M1.3 is fully designed, documented, and ready for Worker execution:
- Update `internal/httpapi/server.go` to dynamically compute `email_configured` and `imgbb_previews`.
- Update `internal/httpapi/server.go` to perform a 2-second timeout bounded `Ping` on MongoDB and return `{"app": "pictide", "status": "ok", "database": "connected"}`.
- Replace catch-all 503 in `server.go:330` with 404.
- Create `internal/httpapi/m1_test.go` with complete automated test coverage for schemas, dynamic config, dynamic health, and route fallthrough.

The complete strategy and copy-pasteable test code are available at:
`c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_3\strategy.md`.

---

## 5. Verification Method

Once the Worker implements the strategy, execute:

```powershell
# Run M1 automated tests against MongoDB
$env:PICTIDE_TEST_MONGODB="1"
go test -v -run "^TestM1_" ./internal/httpapi
```

Expected Output:
```
=== RUN   TestM1_DatabaseAndIndexes
--- PASS: TestM1_DatabaseAndIndexes
=== RUN   TestM1_DynamicConfig
=== RUN   TestM1_DynamicConfig/MinimalConfig_Unconfigured
=== RUN   TestM1_DynamicConfig/FullConfig_Configured
=== RUN   TestM1_DynamicConfig/EmailOnlyConfigured
=== RUN   TestM1_DynamicConfig/ImgBBOnlyConfigured
--- PASS: TestM1_DynamicConfig
=== RUN   TestM1_DynamicHealth
=== RUN   TestM1_DynamicHealth/LiveConnectedCluster
=== RUN   TestM1_DynamicHealth/DisconnectedDatabase
--- PASS: TestM1_DynamicHealth
=== RUN   TestM1_UnrecognizedRoutes_Return404
--- PASS: TestM1_UnrecognizedRoutes_Return404
PASS
ok  	pictide.local/studio/internal/httpapi
```

Invalidation Conditions:
- If `s.cfg.ImgBBApiKey` is used instead of `s.cfg.ImgBBKey`, `go test` will fail to compile.
- If `/api/v1/health` does not check `s.db == nil`, a nil pointer dereference panic occurs on missing DB.
- If unrecognized routes return 503, `TestM1_UnrecognizedRoutes_Return404` fails immediately.
