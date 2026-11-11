# Dispatch: Worker M1

You are worker_m1 (teamwork_worker).
Working Directory: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\worker_m1\`
Project Scope: `c:\Users\Hp\Desktop\lumen v2\PROJECT.md`
Original Request: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\ORIGINAL_REQUEST.md`

## Milestone M1: Core DB Schemas, Indexes & API Gateway Routing

## MANDATORY INTEGRITY WARNING
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## Input Artifacts
Read the strategies prepared by the M1 explorers:
1. Routing Dispatcher Strategy: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_1\strategy.md`
2. Schemas & Indexes Strategy: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_2\strategy.md`
3. Dynamic Config, Health & Tests: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_3\strategy.md`

## Write Ownership
You exclusively own:
- `internal/mongostore/withdrawals.go`
- `internal/mongostore/guest_uploads.go`
- `internal/mongostore/account_links.go`
- `internal/mongostore/admin_audit.go`
- `internal/mongostore/models_test.go`
- `internal/httpapi/server.go`
- `internal/httpapi/m1_test.go`

## Implementation Tasks
1. **New Mongo Store Models (`internal/mongostore/`)**:
   Implement `withdrawals.go`, `guest_uploads.go`, `account_links.go`, `admin_audit.go` per `explorer_m1_2\strategy.md`.
   Implement hermetic unit tests in `models_test.go`.
2. **Index Provisioning & Struct Updates (`internal/httpapi/server.go`)**:
   Add `Email` and `Verified` to `account` struct.
   Provision all missing indexes in `httpapi.New`:
   - `accounts`: `{username: 1}` (unique), `{email: 1}` (sparse)
   - `account_links`: `{token: 1}` (unique), `{expires_at: 1}` (TTL index, expireAfterSeconds: 0)
   - `withdrawals`: `{id: 1}` (unique), `{studio_id: 1, created_at: -1}`, `{reference: 1}`
   - `guest_uploads`: `{id: 1}` (unique), `{gallery_id: 1, created_at: -1}`, `{studio_id: 1}`
   - `admin_audit`: `{id: 1}` (unique), `{created_at: -1}`
3. **Dynamic Config & Health (`internal/httpapi/server.go`)**:
   In `/api/config`, dynamically set `email_configured: s.cfg.SMTPHost != ""` and `imgbb_previews: s.cfg.ImgBBKey != ""`.
   In `/api/v1/health`, ping MongoDB (`s.db.Client().Ping(r.Context(), nil)`) and reply `{"app": "pictide", "status": "ok", "database": "connected"}`.
4. **Two-Tier API Gateway Router (`internal/httpapi/server.go`)**:
   Refactor `ServeHTTP` per `explorer_m1_1\strategy.md`:
   - Route public and dual-mode endpoints (`publicAPI`) without requiring `user != nil`.
   - Route protected studio endpoints (`authenticatedAPI`) requiring `user != nil`, returning 401 if unauthenticated and 405 if method unsupported.
   - Remove the catch-all 503 `"This feature is still being converted to MongoDB."` at line 330.
   - Return clean 404 `{"error": "Endpoint not found."}` for any unrecognized `/api/` endpoints.
5. **Automated Integration Tests (`internal/httpapi/m1_test.go`)**:
   Implement the test suite from `explorer_m1_3\strategy.md` verifying router dispatch, 404 behavior, dynamic config, health ping, and index creation against MongoDB (`$env:PICTIDE_TEST_MONGODB="1"`).
6. **Build & Test Verification**:
   Run `go test -v -p 1 ./internal/mongostore` and `$env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestM1_' ./internal/httpapi`.
   Verify all tests pass with exit code 0.

Write your report to `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\worker_m1\handoff.md` and send a message when done.

## 2026-11-10T21:12:52Z
Read the original request at: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\ORIGINAL_REQUEST.md
Read the project scope at: c:\Users\Hp\Desktop\lumen v2\PROJECT.md
Read your dispatch instructions at: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\worker_m1\DISPATCH.md

Your working directory is: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\worker_m1\

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Implement Milestone M1:
1. Mongo store models and stores in internal/mongostore/
2. Indexes and struct updates in internal/httpapi/server.go
3. Dynamic /api/config and /api/v1/health in internal/httpapi/server.go
4. Two-tier API gateway in internal/httpapi/server.go removing catch-all 503
5. Integration tests in internal/httpapi/m1_test.go
6. Run build and tests to verify.
Write handoff.md and send a message when done.
