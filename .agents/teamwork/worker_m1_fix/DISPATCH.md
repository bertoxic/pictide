# Dispatch: Worker M1 Fix

You are worker_m1_fix (teamwork_worker).
Working Directory: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\worker_m1_fix\`
Project Scope: `c:\Users\Hp\Desktop\lumen v2\PROJECT.md`
Original Request: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\ORIGINAL_REQUEST.md`

## MANDATORY INTEGRITY WARNING
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## Input Artifacts
Read the fix strategies and drop-in diffs:
1. `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_fix_2\handoff.md`
2. `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_fix_3\fix_strategy.md`

## Write Ownership
You exclusively own:
- `internal/httpapi/server.go`
- `internal/httpapi/m1_test.go`

## Implementation Tasks
1. In `internal/httpapi/server.go`:
   - Update `isAuthRoute(method, p string) bool` so that for `case "guest-uploads": return method != http.MethodPost`.
   - Update `authenticatedAPI` to call `if isAuthRoute(r.Method, p)`.
2. In `internal/httpapi/m1_test.go`:
   - Add `TestM1_PublicGuestUploads_AuthBypass` verifying that `POST /api/galleries/test_gallery/guest-uploads` returns 404 (endpoint not implemented yet in M1) rather than 401 Unauthorized when unauthenticated, while `GET /api/galleries/test_gallery/guest-uploads` continues to return 401 when unauthenticated.
3. Run tests and verify:
   - `$env:PICTIDE_TEST_MONGODB="1"; go test -v -run '^TestM1_' ./internal/httpapi`
   - `$env:PICTIDE_TEST_MONGODB="1"; go test -v -run 'TestE2E_Tier1/Feature01' ./internal/httpapi`
   - `node --test test/*.test.js`
   All must pass with exit code 0.

Write your report to `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\worker_m1_fix\handoff.md` and send a message when done.

## 2026-11-10T22:04:04Z
Read the original request at: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\ORIGINAL_REQUEST.md
Read the project scope at: c:\Users\Hp\Desktop\lumen v2\PROJECT.md
Read your dispatch instructions at: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\worker_m1_fix\DISPATCH.md

Your working directory is: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\worker_m1_fix\

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Apply the fix in internal/httpapi/server.go and m1_test.go:
1. Update isAuthRoute to check (method, p string) and exempt POST on guest-uploads.
2. Add TestM1_PublicGuestUploads_AuthBypass in m1_test.go.
3. Run tests to verify TestE2E_Tier1/Feature01 and TestM1_ pass.
Write handoff.md and send a message when done.
