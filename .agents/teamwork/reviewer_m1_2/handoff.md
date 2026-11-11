# Milestone M1 Adversarial Code Review & Quality Report — Reviewer M1.2

## Review Summary
- **Verdict**: **APPROVE**
- **Risk Assessment**: **LOW**
- **Integrity Violation Check**: **PASS** (zero evidence of hardcoded test results, facade implementations, shortcut bypasses, or fabricated outputs)

---

## 1. Observation

1. **Gateway Routing & Catch-All Elimination**:
   - In `internal/httpapi/server.go:284-286`:
     ```go
     // 2c. Clean 404 for any unrecognized /api/ route (replaces line 330 503 catch-all)
     fail(w, 404, "Endpoint not found.")
     ```
   - Unrecognized routes falling through return HTTP 404 with JSON `{"error":"Endpoint not found."}`. The legacy HTTP 503 `"This feature is still being converted to MongoDB."` has been completely eliminated.
   - Non-API routes (`server.go:240-269`) enforce GET/HEAD on `/media/` and `/l/` (returning 405 on unsupported methods), protect against directory traversal (`!strings.Contains(p, "..")`), and fall back to 404.

2. **Public vs. Authenticated Dispatching**:
   - In `server.go:271-283`:
     ```go
     user := s.session(r)
     if s.publicAPI(w, r, user) {
         return
     }
     if s.authenticatedAPI(w, r, user) {
         return
     }
     fail(w, 404, "Endpoint not found.")
     ```
   - `publicAPI` (`server.go:288-488`) handles `/api/config`, `/api/v1/health`, `/api/login`, `/api/logout`, `/api/session`, `/api/checkout`, `/api/v1/webhooks/paystack`, `/api/checkout/:id/verify`, `/api/photos/:id/download-link`, `/api/download/:id`, `/api/photos/:id/(free|clean)`, `/api/galleries/:id`, `/api/galleries/:id/free-download`, and `/api/galleries/:id/qr` without requiring an active session cookie.
   - `authenticatedAPI` (`server.go:490-505`) checks `isAuthRoute(p)`. If the route matches `isAuthRoute` and `user == nil`, it returns HTTP 401 `"Please sign in to your studio."`. If `user != nil` and `s.api` does not handle the HTTP method, it returns HTTP 405 `"Method not allowed."`.
   - Unauthenticated requests to unknown endpoints return HTTP 404 rather than prematurely failing with 401.

3. **Dynamic Configuration & Health Checks**:
   - In `server.go:291-307`:
     `/api/config` dynamically returns `email_configured: s.cfg.SMTPHost != ""` and `imgbb_previews: s.cfg.ImgBBKey != ""` alongside `payments: mode` ("paystack" or "unavailable") and `r2_uploads: s.r2 != nil`.
   - In `server.go:309-330`:
     `/api/v1/health` creates a context bounded by 2 seconds (`context.WithTimeout(r.Context(), 2*time.Second)`) and performs `s.db.Client().Ping(ctx, readpref.Primary())`. It returns 200 `status: "ok", database: "connected"` when healthy, and 503 `status: "unavailable", database: "disconnected"` when disconnected or timed out. Nil check `s.db == nil` prevents nil dereference.

4. **MongoDB Collections & TTL Index Provisioning**:
   - In `server.go:58-109`:
     Indexes are provisioned via `CreateMany` during `httpapi.New`:
     - `accounts`: `{username: 1}` (unique), `{email: 1}` (sparse)
     - `account_links`: `{token: 1}` (unique), `{expires_at: 1}` (`SetExpireAfterSeconds(0)`)
     - `withdrawals`: `{id: 1}` (unique), `{studio_id: 1, created_at: -1}` (compound), `{reference: 1}`
     - `guest_uploads`: `{id: 1}` (unique), `{gallery_id: 1, created_at: -1}` (compound), `{studio_id: 1}`
     - `admin_audit`: `{id: 1}` (unique), `{created_at: -1}`
   - `internal/mongostore/account_links.go:18` models `ExpiresAt` as `time.Time` (mapping to BSON Date), enabling MongoDB's TTL background monitor. In addition, `GetValid` filters `"expires_at": bson.M{"$gt": time.Now()}`, providing immediate application-level expiration protection even if the background TTL cleaner (which runs every 60s) has not executed yet.

5. **Nil Safety in Mongo Stores**:
   - `internal/mongostore/withdrawals.go`, `guest_uploads.go`, `account_links.go`, and `admin_audit.go` validate `db == nil` in their constructors and reject nil / empty records in `Create` / `Record`.
   - Iterators and cursors (`cur, err := ...`) use `defer cur.Close(ctx)` to prevent resource leaks. Limits are clamped between 1 and 200-500.

6. **Empirical Independent Build & Test Execution**:
   - Command: `go test -v -p 1 ./internal/mongostore/...`
     Result: All 5 tests passed (PASS).
   - Command: `$env:PICTIDE_TEST_MONGODB="1"; go test -v -count=1 -run '^TestM1_' ./internal/httpapi`
     Result: All 5 tests passed in 22.025s (PASS).
   - Command: `node --test test/*.test.js`
     Result: 17/17 tests passed in 348ms (PASS).
   - Command: `go test ./...`
     Result: All repository packages compile and pass tests (PASS).
   - Command: `go build -o tmp_pictide.exe ./cmd/pictide`
     Result: Successful compilation with zero warnings or errors (PASS).

---

## 2. Logic Chain

1. From Observation 1 and 2, partitioning requests into `publicAPI` and `authenticatedAPI` eliminates the legacy catch-all 503 stub in `server.go` and guarantees that unauthenticated callers can access public assets, dynamic configurations, health pings, public galleries, and payment flows without getting blocked with 401 or 503 errors.
2. From Observation 3, dynamically computing `email_configured` and `imgbb_previews` from `s.cfg` eliminates hardcoded boolean placeholders. Implementing `Ping(ctx, readpref.Primary())` with `context.WithTimeout(..., 2*time.Second)` ensures that `/api/v1/health` accurately reports cluster availability without risking hung HTTP connections during database degradation or network partitions.
3. From Observation 4, defining models and provisioning indexes in `httpapi.New` and `EnsureIndexes` ensures schema integrity across domain collections (`withdrawals`, `guest_uploads`, `account_links`, `admin_audit`). In particular, TTL index `{expires_at: 1}` combined with programmatic `$gt: time.Now()` filtering guarantees secure token lifecycles for password recovery and email verification.
4. From Observation 5, all store constructors and mutating methods defensively check for nil pointers and empty identifiers, returning clean Go errors instead of panicking.
5. From Observation 6, running the complete suite of tests and builds independently confirms that the changes are regression-free and production-ready.

---

## 3. Adversarial Challenges & Findings

### Challenge 1: Dual-Mode Routing for Upcoming Milestone M4 Guest Uploads (Low Risk — Informational)
- **Assumption**: `/api/galleries/:id/guest-uploads` is listed under `isAuthRoute` in `server.go:529`.
- **Scenario**: In M4, guest upload submission (`POST /api/galleries/:id/guest-uploads`) is intended to be a public endpoint, while guest upload listing (`GET /api/galleries/:id/guest-uploads`) is studio-authenticated.
- **Blast Radius**: None in M1 (guest uploads are scheduled for M4). However, in M4, if `POST /api/galleries/:id/guest-uploads` is not added to `publicAPI`, unauthenticated guests would receive a 401.
- **Mitigation / Note for M4**: Because `publicAPI` is evaluated before `authenticatedAPI`, the M4 worker should add `case strings.HasPrefix(p, "/api/galleries/") && strings.HasSuffix(p, "/guest-uploads") && r.Method == "POST":` into `publicAPI`.

### Challenge 2: MongoDB Clock Precision in TTL Comparisons (Low Risk — Verified Robust)
- **Assumption**: `AccountLink.ExpiresAt` comparison across Go and MongoDB matches.
- **Analysis**: MongoDB BSON dates are stored with millisecond precision, while Go's `time.Now()` has nanosecond precision.
- **Verification**: `account_links.go` queries with `$gt: time.Now()`, which converts to BSON Date with millisecond precision; this correctly handles token expiration boundaries without false invalidations.

---

## 4. Integrity Watchdog Check

- **Hardcoded test results or expected outputs embedded in source code**: None. Dynamic checks are performed against live MongoDB connections and server configuration structs.
- **Dummy or facade implementations**: None. Complete models, indexes, queries, and error handling are present.
- **Shortcuts bypassing the task**: None. Required models, collections, indexes, dynamic endpoints, and gateway routing were built from scratch.
- **Fabricated verification outputs**: None. All commands were independently executed via shell in this turn, confirming matching output and zero failures.
- **Self-certifying work**: All claims were independently confirmed via live test runs and line-by-line inspection.

---

## 5. Verified Claims Matrix

| Claim | Upstream Source | Verification Method | Status |
|---|---|---|---|
| Domain models & repositories created | `worker_m1/handoff.md:5-6` | `view_file` on `internal/mongostore/*.go` | PASS |
| Legacy 503 catch-all eliminated | `worker_m1/handoff.md:24-28` | Inspected `server.go:284-286` (returns 404) | PASS |
| Unauthenticated requests to public routes not blocked | `DISPATCH.md:12` | Inspected `server.go:288-488` and tested via `m1_test.go` | PASS |
| Dynamic config reports runtime flags | `worker_m1/handoff.md:8-9` | Inspected `server.go:291-307` and ran `TestM1_DynamicConfig` | PASS |
| Dynamic health ping with 2s timeout | `worker_m1/handoff.md:10-12` | Inspected `server.go:309-330` and ran `TestM1_DynamicHealth` | PASS |
| Indexes provisioned at startup in `httpapi.New` | `worker_m1/handoff.md:78` | Inspected `server.go:58-109` and ran `TestM1_DatabaseAndIndexes` | PASS |
| Unit and integration tests pass | `worker_m1/handoff.md:42-73` | Ran `go test` and `node --test` directly | PASS |

---

## 6. Caveats
No caveats. All M1 criteria have been empirically verified.

---

## 7. Conclusion
Milestone M1 satisfies all requirements set forth in `ORIGINAL_REQUEST.md`, `PROJECT.md`, and `DISPATCH.md`. The code is safe, clean, tested, and ready for production.

**Verdict**: **APPROVE**

---

## 8. Verification Method for Independent Reproduction
To reproduce these findings independently, run the following commands in the workspace root:

1. Run mongostore unit tests:
   ```powershell
   go test -v -p 1 ./internal/mongostore/...
   ```
2. Run M1 integration tests against MongoDB:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -count=1 -run '^TestM1_' ./internal/httpapi
   ```
3. Run frontend contract tests:
   ```powershell
   node --test test/*.test.js
   ```
4. Verify overall package build and compilation:
   ```powershell
   go build ./cmd/pictide
   go test ./...
   ```
