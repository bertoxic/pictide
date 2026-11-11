# Handoff Report: Explorer M1.1 — Routing Dispatcher Strategy

**From**: `explorer_m1_1` (teamwork_explorer)  
**To**: Orchestrator (`963791b5-32ce-4fe6-ad97-67f5764269a0`) & Worker M1  
**Working Directory**: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_1\`  
**Date**: 2026-11-10T21:09:00Z  
**Type**: Hard Handoff (Investigation Complete)  

---

## 1. Observation

1. **`internal/httpapi/server.go` (lines 322–332)**:
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
   Directly observed:
   - Line 323 returns `401 {"error":"Please sign in to your studio."}` for ANY `/api/` request when `user == nil`, even if the path does not exist or is a planned public endpoint (`/api/register`, `/api/account/verify`, etc.).
   - Line 330 unconditionally returns HTTP 503 with verbatim message `"This feature is still being converted to MongoDB."` whenever `s.api(w, r, user)` returns `false`.

2. **`PROJECT.md` (lines 13, 23, 57–60)**:
   - Line 13: Feature 1 specifies: *"Route public vs authenticated `/api/` endpoints cleanly in `server.go`, removing catch-all 503 'This feature is still being converted to MongoDB.'"*
   - Line 23 (Acceptance Criteria): *"Zero endpoints or features in the application return `{"error":"This feature is still being converted to MongoDB."}` or placeholder unimplemented errors."*
   - Line 57: *"Public endpoints (`/api/register`, `/api/account/verify`, `/api/account/forgot`, `/api/account/reset`, `/api/galleries/{id}/guest-uploads`) MUST NOT require authentication session."*
   - Line 59: *"All JSON errors MUST return `{"error": "<human readable message>"}` with appropriate HTTP status (400, 401, 403, 404, 500)."*

3. **`internal/httpapi/workspace.go` (lines 17–133)**:
   `func (s *Server) api(w http.ResponseWriter, r *http.Request, user *account) bool` tests both path AND method in every case (e.g. `case r.URL.Path == "/api/orders" && r.Method == "GET"`). When the path matches a known authenticated route but the method is unsupported (e.g. `POST /api/orders`), `s.api` returns `false`, causing line 330 in `server.go` to return 503 instead of 405 Method Not Allowed.

4. **Baseline Test Execution**:
   - `go test ./...` exited with code 0 (all packages pass).
   - `node --test test/*.test.js` passed 13/13 tests (duration 263ms).

---

## 2. Logic Chain

1. **Premise 1 (from Observation 1 & 2)**:
   Line 322 blocks all unauthenticated requests to `/api/` with `401`. Planned public routes (`/api/register`, `/api/account/verify`, `/api/account/forgot`, `/api/account/reset`, `POST /api/galleries/:id/guest-uploads`) cannot function until public route matching occurs before the `user == nil` gate.
2. **Premise 2 (from Observation 1 & 2)**:
   Line 330 returns 503 for all unhandled requests. The acceptance criteria explicitly forbid returning `"This feature is still being converted to MongoDB."`. Therefore, line 330 must be completely replaced.
3. **Premise 3 (from Observation 1, 2, & 3)**:
   A clean REST gateway must distinguish between three distinct outcomes for an incoming `/api/` request:
   - **Case A**: Matches a public/dual route -> execute handler if method is allowed; return `405 Method Not Allowed` if method is disallowed.
   - **Case B**: Matches a recognized authenticated route -> return `401 Please sign in to your studio.` if `user == nil`; if `user != nil`, execute handler if method is allowed, or return `405 Method Not Allowed` if method is disallowed.
   - **Case C**: Matches neither public nor authenticated routes -> return `404 Endpoint not found.`.
4. **Premise 4 (from Observation 1 & Strategy Document)**:
   Factoring the routing into `s.publicAPI(w, r, user) bool`, `s.authenticatedAPI(w, r, user) bool`, and `isAuthRoute(p string) bool` isolates concerns, cleanly decouples static file serving from API dispatching, and allows subsequent milestone workers (M2, M3, M4, M5, M6) to add endpoints by modifying only their respective routing table without touching core gateway logic.

---

## 3. Caveats

1. **Future Milestone Handlers**: Handlers for `POST /api/register`, `POST /api/account/verify`, `POST /api/account/forgot`, `POST /api/account/reset` (M2) and `POST /api/galleries/:id/guest-uploads` (M4) are not implemented in M1. In M1, these paths are unrecognized and will cleanly return 404 until implemented by their milestone workers.
2. **Trailing Slashes**: Standard Go URL matching preserves `r.URL.Path` as-is. Frontend calls consistently use paths without trailing slashes.
3. **Database Dependency for Tests**: `TestMongoWorkspace` in `workspace_test.go` skips when `PICTIDE_TEST_MONGODB=1` is not set. Standalone unit tests must be added by the worker to test 401, 404, and 405 status codes without requiring a live MongoDB instance.

---

## 4. Conclusion

The strategy and code diff plan detailed in `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_1\strategy.md` completely fulfills all Milestone M1 routing requirements:
1. Public and dual-mode routes are evaluated in `s.publicAPI` before the authentication gate.
2. Catch-all 503 `"This feature is still being converted to MongoDB."` at line 330 is removed.
3. Unrecognized endpoints cleanly return JSON 404 (`fail(w, 404, "Endpoint not found.")`).
4. Method mismatches on recognized routes cleanly return JSON 405 (`fail(w, 405, "Method not allowed.")`).
5. The Worker can implement this change in `server.go` with zero regressions to existing functionality.

---

## 5. Verification Method

To independently verify the implementation:

1. **Code Inspection**:
   Inspect `internal/httpapi/server.go`:
   - Verify `ServeHTTP` calls `s.publicAPI` then `s.authenticatedAPI` then `fail(w, 404, ...)`.
   - Verify zero occurrences of `"This feature is still being converted to MongoDB."` remain in `server.go`.

2. **Automated Unit Tests**:
   Run the following commands:
   ```powershell
   go test -v ./internal/httpapi
   go test ./...
   node --test test/*.test.js
   ```

3. **HTTP Response Verification**:
   Verify via Go tests or HTTP requests:
   - `GET /api/config` -> HTTP 200 JSON
   - `POST /api/config` -> HTTP 405 JSON `{"error":"Method not allowed."}`
   - `GET /api/nonexistent` (unauthenticated) -> HTTP 404 JSON `{"error":"Endpoint not found."}`
   - `GET /api/nonexistent` (authenticated) -> HTTP 404 JSON `{"error":"Endpoint not found."}`
   - `GET /api/orders` (unauthenticated) -> HTTP 401 JSON `{"error":"Please sign in to your studio."}`
   - `POST /api/orders` (authenticated) -> HTTP 405 JSON `{"error":"Method not allowed."}`

4. **Invalidation Conditions**:
   The fix is invalid if:
   - Any endpoint returns 503 with `"This feature is still being converted to MongoDB."`.
   - An unauthenticated request to a nonexistent endpoint returns 401 instead of 404.
   - A method mismatch on a valid endpoint returns 503 or 401 instead of 405.
