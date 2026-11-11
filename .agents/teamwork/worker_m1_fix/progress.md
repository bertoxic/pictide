# Progress

Last visited: 2026-11-10T22:11:30Z
Status: Completed M1 fix for public guest uploads routing auth bypass and test suite validation.

## Completed Tasks
- [x] Analyzed defect: `isAuthRoute` intercepted `POST /api/galleries/:id/guest-uploads` and forced 401 unauthenticated check.
- [x] Updated `isAuthRoute(method, p string) bool` in `internal/httpapi/server.go` to exempt `POST` on `guest-uploads`.
- [x] Updated `authenticatedAPI` in `internal/httpapi/server.go` to call `isAuthRoute(r.Method, p)`.
- [x] Added `TestM1_PublicGuestUploads_AuthBypass` regression test in `internal/httpapi/m1_test.go`.
- [x] Verified `TestM1_PublicGuestUploads_AuthBypass` passes.
- [x] Verified all `TestM1_` tests pass.
- [x] Verified `TestE2E_Tier1/Feature01_PublicAndAuthenticatedRoutingDispatcher` passes.
- [x] Verified `node --test test/*.test.js` passes.
- [x] Verified `TestChallengerM1_` suite passes.
- [x] Verified `TestMongoWorkspace` passes.
- [x] Verified `go vet ./internal/httpapi` passes.
