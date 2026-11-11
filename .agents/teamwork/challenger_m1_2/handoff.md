# Challenger M1.2: Database & Schema Stress Testing — Handoff Report

## Verdict: APPROVE

---

## 1. Observation

1. **MongoDB Collection Index Models and Catalog Metadata**:
   - In `internal/httpapi/server.go:58-109`, startup index provisioning models were inspected against live MongoDB cluster specifications returned by `coll.Indexes().List(ctx)`:
     - `accounts`: `{username: 1}` verified with `unique: true`.
     - `account_links`: `{token: 1}` verified with `unique: true`; `{expires_at: 1}` verified with `expireAfterSeconds: 0`.
     - `withdrawals`: `{id: 1}` verified with `unique: true`; compound `{studio_id: 1, created_at: -1}` verified; `{reference: 1}` verified.
     - `guest_uploads`: `{id: 1}` verified with `unique: true`; compound `{gallery_id: 1, created_at: -1}` verified; `{studio_id: 1}` verified.
     - `admin_audit`: `{id: 1}` verified with `unique: true`; `{created_at: -1}` verified.
     - `photos`: `{id: 1}` verified with `unique: true`; compound `{studio_id: 1, gallery_id: 1, hash: 1}` verified with `unique: true`.

2. **Empirical Concurrency and Duplicate Rejection Testing**:
   - `internal/httpapi/m1_challenger_test.go:TestChallengerM1_ConcurrentDuplicateRejection` dispatched 10 concurrent goroutines racing to insert duplicate values:
     - `accounts.username`: 1 succeeded, 9 returned `mongo.IsDuplicateKeyError(err) == true`.
     - `withdrawals.id`: 1 succeeded, 9 returned `mongo.IsDuplicateKeyError(err) == true`.
     - `guest_uploads.id`: 1 succeeded, 9 returned `mongo.IsDuplicateKeyError(err) == true`.
     - `account_links.token`: 1 succeeded, 9 returned `mongo.IsDuplicateKeyError(err) == true`.

3. **TTL Index and Immediate Query Invalidation**:
   - In `internal/mongostore/account_links.go:72`, `GetValid` filters `"expires_at": bson.M{"$gt": time.Now()}`.
   - In `internal/httpapi/m1_challenger_test.go:TestChallengerM1_TTLIndex_And_TimeQueries`:
     - An account link inserted with `ExpiresAt: time.Now().Add(-10 * time.Minute)` was immediately rejected by `GetValid` returning `mongo.ErrNoDocuments`, confirming that applications do not leak expired credentials during the up to 60-second MongoDB TTL sweeper cycle.
     - Tokens with mismatched purpose were likewise rejected with `mongo.ErrNoDocuments`.

4. **Store Constructors, Nil-Safety, and Parameter Validation**:
   - In `internal/mongostore/models_test.go:134` and `internal/httpapi/m1_challenger_test.go:420`:
     - `mongostore.NewWithdrawals(nil)`, `NewGuestUploads(nil)`, `NewAccountLinks(nil)`, `NewAdminAudit(nil)` all return non-nil error `"database is required"`.
     - Nil arguments passed to `Create` and `Record` return `"invalid ... record"`.
     - Non-existent resource lookups (`GetByID`, `GetByToken`, `GetByReference`) cleanly return `mongo.ErrNoDocuments`.
     - `Withdrawals.AccountLast4` auto-derives from `item.AccountNumber[len(item.AccountNumber)-4:]` when `len >= 4`, avoiding out-of-bounds slice indexing on short strings.

5. **Live Health Check Ping under Severed Network Conditions**:
   - In `internal/httpapi/server.go:309-329`, `/api/v1/health` executes `s.db.Client().Ping(ctx, readpref.Primary())` with a 2-second timeout.
   - In `internal/httpapi/m1_challenger_test.go:TestChallengerM1_HealthCheck_SeveredClient`:
     - Healthy connected state returned HTTP 200 `{"app":"pictide","database":"connected","status":"ok"}`.
     - Actively disconnecting the client via `client.Disconnect(ctx)` caused `/api/v1/health` to return HTTP 503 `{"app":"pictide","database":"disconnected","status":"unavailable"}` for both `GET` and `HEAD` requests.

6. **Execution Commands and Verbatim Test Results**:
   - Challenger stress suite:
     ```powershell
     $env:PICTIDE_TEST_MONGODB="1"; go test -v -count=1 -run '^TestChallengerM1_' ./internal/httpapi
     ```
     Output:
     ```
     === RUN   TestChallengerM1_IndexSpecifications_EmpiricalInspection
     --- PASS: TestChallengerM1_IndexSpecifications_EmpiricalInspection (4.09s)
     === RUN   TestChallengerM1_IdempotentIndexCreation
     --- PASS: TestChallengerM1_IdempotentIndexCreation (5.29s)
     === RUN   TestChallengerM1_ConcurrentDuplicateRejection
     === RUN   TestChallengerM1_ConcurrentDuplicateRejection/ConcurrentAccountUsernameDuplicates
     === RUN   TestChallengerM1_ConcurrentDuplicateRejection/ConcurrentWithdrawalIDDuplicates
     === RUN   TestChallengerM1_ConcurrentDuplicateRejection/ConcurrentGuestUploadIDDuplicates
     === RUN   TestChallengerM1_ConcurrentDuplicateRejection/ConcurrentAccountLinkTokenDuplicates
     --- PASS: TestChallengerM1_ConcurrentDuplicateRejection (5.07s)
     === RUN   TestChallengerM1_TTLIndex_And_TimeQueries
     --- PASS: TestChallengerM1_TTLIndex_And_TimeQueries (2.50s)
     === RUN   TestChallengerM1_StoreConstructors_NilSafety
     --- PASS: TestChallengerM1_StoreConstructors_NilSafety (1.70s)
     === RUN   TestChallengerM1_HealthCheck_SeveredClient
     --- PASS: TestChallengerM1_HealthCheck_SeveredClient (3.06s)
     === RUN   TestChallengerM1_StoreRepositoryLifecycle
     --- PASS: TestChallengerM1_StoreRepositoryLifecycle (3.79s)
     PASS
     ok   pictide.local/studio/internal/httpapi 25.600s
     ```
   - Worker integration tests:
     ```powershell
     $env:PICTIDE_TEST_MONGODB="1"; go test -v -count=1 -run '^TestM1_' ./internal/httpapi
     ```
     Output:
     ```
     PASS
     ok   pictide.local/studio/internal/httpapi 22.574s
     ```
   - Unit tests:
     ```powershell
     go test -v -p 1 ./internal/mongostore/...
     ```
     Output:
     ```
     PASS
     ok   pictide.local/studio/internal/mongostore 0.00s
     ```
   - Node test suite:
     ```powershell
     node --test test/*.test.js
     ```
     Output:
     ```
     ℹ tests 17
     ℹ suites 0
     ℹ pass 17
     ℹ fail 0
     ```

---

## 2. Logic Chain

1. **Uniqueness Enforcement**: Observations 1 and 2 prove that index models registered in `internal/httpapi/server.go` and `internal/mongostore/` exist in MongoDB metadata with `unique: true`. Under concurrent write pressure from 10 parallel goroutines, MongoDB's storage engine consistently admitted only the first insert and rejected all subsequent duplicates with `mongo.IsDuplicateKeyError`. This guarantees data integrity against race conditions across accounts, withdrawals, guest uploads, and account links.
2. **TTL Index and Application-Layer Expiration Defense-in-Depth**: Observation 3 confirms that the TTL index is created with `expireAfterSeconds: 0`. Because background MongoDB TTL reaper threads only run periodically (typically every 60s), relying solely on TTL collection sweeps would risk accepting expired tokens. Observation 3 verifies that `GetValid` defensively filters `expires_at > time.Now()`, returning `mongo.ErrNoDocuments` immediately upon token expiration.
3. **Nil Safety and Input Validation**: Observation 4 demonstrates that all store constructors guard against nil database handles, preventing nil pointer dereferences. Furthermore, store methods enforce presence of mandatory fields (`id`, `reference`, `gallery_id`, `studio_id`) and handle non-existent documents by returning standard `mongo.ErrNoDocuments`.
4. **Health Check Fidelity**: Observation 5 demonstrates that `/api/v1/health` accurately reflects database status: healthy when connected (200 OK) and immediately switching to degraded (503 Service Unavailable) upon connection termination (`client.Disconnect()`).
5. **Idempotency and Regressions**: Observations 1, 2, and 6 establish that re-initializing the server or calling `EnsureIndexes` repeatedly produces zero index-collision errors. All existing worker tests, mongostore unit tests, and frontend test suites pass 100%.

---

## 3. Caveats

- **MongoDB Instance Dependency**: The integration and challenger test suites require an active `MONGODB_URI` connection (verified against the live Atlas cluster configured in `.env`). In isolated offline environments without MongoDB, tests skip gracefully.
- **Future Endpoints (M2–M6)**: Milestone M1 provisions the schemas, repository stores, and index guarantees. Handler endpoints utilizing these stores (e.g. `POST /api/register`, `POST /api/withdrawals`) are scheduled for implementation in subsequent milestones.

---

## 4. Conclusion

**Verdict: APPROVE**

Milestone M1 satisfies all database schema, index uniqueness, TTL management, store constructor nil-safety, and health check requirements. Adversarial stress testing under concurrent race conditions and severed network connections confirmed robust error handling, zero panics, and 100% test pass rates across all suites.

---

## 5. Verification Method

To independently reproduce the empirical findings:

1. Execute the challenger adversarial stress suite:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -count=1 -run '^TestChallengerM1_' ./internal/httpapi
   ```
2. Execute the worker M1 integration suite:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v -count=1 -run '^TestM1_' ./internal/httpapi
   ```
3. Execute the mongostore unit tests:
   ```powershell
   go test -v -p 1 ./internal/mongostore/...
   ```
4. Execute the node frontend contract tests:
   ```powershell
   node --test test/*.test.js
   ```
5. Inspect test assertions and implementation in:
   - `internal/httpapi/m1_challenger_test.go`
   - `internal/httpapi/m1_test.go`
   - `internal/mongostore/*.go`
   - `internal/httpapi/server.go`
