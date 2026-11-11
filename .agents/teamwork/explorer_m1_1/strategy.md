# Milestone M1: Routing Dispatcher Strategy & Code Diff Plan

**Author**: Explorer M1.1 (`teamwork_explorer`)  
**Target File**: `internal/httpapi/server.go`  
**Milestone**: M1: Core DB Schemas, Indexes & API Gateway Routing  
**Status**: Ready for Implementation  

---

## 1. Executive Summary

This document specifies the routing architecture refactor for `internal/httpapi/server.go`. Currently, `ServeHTTP` mixes static asset serving, public endpoint routing, dual-mode gallery viewing, and authenticated session enforcement in an ad-hoc linear chain. At line 330 of `server.go`, any unmatched route or method mismatch returns `503 {"error":"This feature is still being converted to MongoDB."}`, directly violating project acceptance criteria. Unauthenticated requests to non-existent API paths inappropriately return `401 {"error":"Please sign in to your studio."}`, while unauthenticated requests to planned public endpoints (such as `POST /api/register` or `POST /api/galleries/:id/guest-uploads`) are incorrectly blocked.

This strategy establishes a two-tier API Gateway dispatcher:
1. **Public API Dispatcher (`publicAPI`)**: Evaluates all public and dual-mode endpoints (health, config, auth, checkout, downloads, public gallery views) *before* authentication is required.
2. **Authenticated API Dispatcher (`authenticatedAPI`)**: Verifies the session cookie for recognized protected studio endpoints; returns `401` if unauthenticated, routes to `s.api(w, r, user)` if authenticated, and returns `405 Method Not Allowed` if the HTTP method is unsupported.
3. **Clean 404 Fallthrough**: Any unrecognized `/api/` path immediately returns `404 {"error":"Endpoint not found."}`, completely eliminating the catch-all `503` stub.

---

## 2. Current Architecture & Identified Flaws

### 2.1 File Analysis: `internal/httpapi/server.go` (Lines 182–338)

```go
// Line 182: ServeHTTP entrypoint
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // Lines 183-199: Security headers and CORS Origin check
    ...
    // Lines 201-221: Inline public checks: /api/config, /api/v1/health, /api/login, /api/logout
    ...
    // Line 222: Session lookup
    user := s.session(r)
    
    // Lines 223-321: Scattered public/dual routes: /api/checkout, /api/v1/webhooks/paystack,
    // /api/checkout/:id/verify, /api/photos/:id/download-link, /api/download/:id,
    // /api/photos/:id/free, /media/:id, /l/:id, /api/galleries/:id, /api/session
    ...
    // Lines 322-332: Authenticated gate and 503 Catch-all
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
    // Lines 333-338: Static files and 404
    ...
}
```

### 2.2 Deficiencies Identified

1. **Premature Authentication Blocker for Public Routes**:
   New public endpoints planned for Milestone M2 (`/api/register`, `/api/account/verify`, `/api/account/forgot`, `/api/account/reset`) and Milestone M4 (`POST /api/galleries/:id/guest-uploads`) fall into the catch-all `if strings.HasPrefix(p, "/api/")` at line 322. Because `user == nil`, they return `401 Unauthorized` instead of reaching their respective handlers.
2. **Violation of Acceptance Criterion 1 (The 503 Catch-all)**:
   Line 330 emits `fail(w, 503, "This feature is still being converted to MongoDB.")`. This catch-all masks 404s (non-existent routes) and 405s (method mismatches on valid routes), and must be eliminated.
3. **Misleading 401 on Unrecognized Routes**:
   When an unauthenticated client requests a nonexistent route like `GET /api/nonexistent`, line 324 returns `401 {"error":"Please sign in to your studio."}`. This falsely implies that the resource exists behind authentication, leaking architectural details and confusing clients.
4. **Lack of HTTP 405 Method Not Allowed Handling**:
   If a client requests a known route with an unsupported method (e.g. `POST /api/config`, `GET /api/login`, or `POST /api/orders`), the router currently falls through: unauthenticated requests receive 401, while authenticated requests receive 503. A compliant REST gateway must return `405 Method Not Allowed`.
5. **Non-API and Static Route Coupling**:
   Non-API routes like `/media/` and `/l/` are currently checked in the middle of `/api/` route checks. Separating `/api/` from non-API assets simplifies both branches.

---

## 3. Route Inventory & Classification

### 3.1 Non-API Routes (Handled outside `/api/`)
| Pattern | Methods | Handler / Destination | Purpose |
|---|---|---|---|
| `/media/:id` | `GET`, `HEAD` | `s.servePreview(w, r, user, id)` | Photo thumbnails/previews |
| `/l/:id` | `GET`, `HEAD` | `http.Redirect("/?gallery=" + id)` | Shortlink gallery redirect |
| `/`, `*.js`, `*.css`, etc. | `GET`, `HEAD` | `http.FileServer(http.FS(public.Files))` | Static frontend assets |
| Non-matching paths | ANY | `http.NotFound(w, r)` | Fallback |

### 3.2 Public & Dual-Mode API Routes (Handled by `publicAPI`)
*MUST NOT require `user != nil`.*

| Path / Pattern | Allowed Methods | Handler / Response | Description |
|---|---|---|---|
| `/api/config` | `GET`, `HEAD` | Inline reply | App & payment config |
| `/api/v1/health` | `GET`, `HEAD` | Inline reply | Service health status |
| `/api/login` | `POST` | `s.login(w, r)` | Studio authentication |
| `/api/logout` | `POST` | Clear cookie, reply `{ok: true}` | End studio session |
| `/api/session` | `GET`, `HEAD` | Inline reply (uses `user` if non-nil) | Session state check |
| `/api/checkout` | `POST` | `s.checkout(w, r)` | Initiate order payment |
| `/api/v1/webhooks/paystack` | `POST` | `s.webhook(w, r)` | External Paystack webhook |
| `/api/checkout/:id/verify` | `POST` | `s.verifyPayment(w, r, id)` | Verify payment transaction |
| `/api/photos/:id/download-link` | `POST` | `s.downloadLink(w, r, id)` | Generate photo download link |
| `/api/download/:id` | `GET`, `HEAD` | `s.download(w, r, id)` | Asset download binary |
| `/api/photos/:id/free` | `GET`, `HEAD` | `s.photoDownload(w, r, id, "free")` | Download free photo |
| `/api/photos/:id/clean` | `GET`, `HEAD` | `s.photoDownload(w, r, id, "clean")` | Download clean photo |
| `/api/galleries/:id` | `GET`, `HEAD` | `s.getGallery` (if owner) / `s.publicGallery` | Dual-mode gallery view |
| `/api/galleries/:id/free-download` | `GET`, `HEAD` | `s.freeArchive(w, r, id)` | Download full free archive |
| `/api/galleries/:id/qr` | `GET`, `HEAD` | `s.galleryQR` (owner or public) | Gallery QR code generation |
| *M2: `/api/register`* | `POST` | Hook point for `s.register` | Photographer registration |
| *M2: `/api/account/verify`* | `POST` | Hook point for `s.verifyAccount` | Token email verification |
| *M2: `/api/account/forgot`* | `POST` | Hook point for `s.forgotPassword`| Password reset request |
| *M2: `/api/account/reset`* | `POST` | Hook point for `s.resetPassword` | Password reset submission |
| *M4: `/api/galleries/:id/guest-uploads`* | `POST` | Hook point for `s.guestUpload` | Guest photo submission |
| *M4: `/api/guest-uploads/status`* | `POST` | Hook point for `s.guestUploadStatus` | Guest token status check |

### 3.3 Authenticated Studio API Routes (Handled by `authenticatedAPI` & `s.api`)
*Requires `user != nil`. If `user == nil`, returns `401 {"error":"Please sign in to your studio."}`.*

| Path / Pattern | Allowed Methods | Handler in `workspace.go` / other |
|---|---|---|
| `/api/admin/overview` | `GET`, `HEAD` | `s.adminOverview` |
| `/api/admin/insights` | `GET`, `HEAD` | `s.adminInsights` |
| `/api/admin/accounts` | `GET`, `HEAD`, `POST` | `s.listAccounts` (GET) / `s.createAccount` (POST) |
| `/api/admin/activity` | `GET`, `HEAD` | `s.accountActivity` |
| `/api/admin/account-status` | `POST` | `s.accountStatus` |
| `/api/security/password` | `POST` | `s.changePassword` |
| `/api/settings` | `GET`, `HEAD`, `PUT` | `s.settings` (GET) / `s.saveSettings` (PUT) |
| `/api/galleries` | `GET`, `HEAD`, `POST` | `s.listGalleries` (GET) / `s.createGallery` (POST) |
| `/api/galleries/:id` | `PATCH`, `DELETE` | `s.updateGallery` (PATCH) / `s.archiveGallery` (DELETE) |
| `/api/galleries/:id/photos` | `POST` | `s.uploadPhotos` |
| `/api/galleries/:id/direct-upload` | `POST` | `s.startDirectUpload` |
| `/api/galleries/:id/cover` | `PUT` | `s.cover` |
| `/api/galleries/:id/customization` | `PATCH` | `s.customize` |
| `/api/galleries/:id/card-design` | `PUT` | `s.cardDesign` |
| `/api/galleries/:id/print` | `GET`, `HEAD` | `s.printGallery` |
| `/api/orders` | `GET`, `HEAD` | `s.listOrders` |
| `/api/orders/:id/sync` | `POST` | `s.syncOrder` |
| `/api/wallet` | `GET`, `HEAD` | `s.wallet` |
| `/api/direct-uploads/:id/complete` | `POST` | `s.completeDirectUpload` |
| `/api/photos/:id` | `PATCH`, `DELETE` | `s.photoPrice` (PATCH) / `s.archivePhoto` (DELETE) |
| *M4: `/api/galleries/:id/guest-uploads`* | `GET`, `HEAD` | Hook point for studio guest uploads listing |
| *M4: `/api/guest-uploads/:id/preview`* | `GET`, `HEAD` | Hook point for guest upload preview |
| *M4: `/api/guest-uploads/:id/review`* | `POST` | Hook point for review approval/rejection |

---

## 4. Architectural Design

### 4.1 Flow Diagram

```
Request arrives at ServeHTTP(w, r)
 │
 ├── 1. Apply Security Headers (nosniff, DENY, no-referrer)
 ├── 2. Verify CORS Origin (skip for webhooks and GET/HEAD)
 │
 ├── 3. Path does NOT start with "/api/"?
 │     ├── /media/:id  ──> GET/HEAD: servePreview | Other: 405
 │     ├── /l/:id      ──> GET/HEAD: redirect     | Other: 405
 │     ├── Static file ──> http.FileServer
 │     └── Fallback    ──> http.NotFound
 │
 └── 4. Path starts with "/api/"
       │
       ├── user := s.session(r)  [extract session cookie once]
       │
       ├── Call s.publicAPI(w, r, user)
       │    └── Match public/dual route?
       │         ├── Method allowed?  ──> Execute handler, return true
       │         └── Method invalid?  ──> fail(w, 405, "Method not allowed."), return true
       │
       ├── Call s.authenticatedAPI(w, r, user)
       │    └── Match isAuthRoute(p)?
       │         ├── user == nil?     ──> fail(w, 401, "Please sign in to your studio."), return true
       │         ├── s.api(w,r,user)? ──> Handler executed, return true
       │         └── Method invalid?  ──> fail(w, 405, "Method not allowed."), return true
       │
       └── Fallthrough: Unrecognized API Route
            └── fail(w, 404, "Endpoint not found.")  [CLEAN 404, ZERO 503]
```

### 4.2 Handling of HTTP 405 Method Not Allowed
When an incoming request matches a recognized endpoint path, the router immediately checks the HTTP method:
- If valid: forwards to handler.
- If invalid: sends `fail(w, 405, "Method not allowed.")` and returns `true`, halting execution.

### 4.3 Handling of HTTP 404 Endpoint Not Found
When an incoming request path under `/api/` matches neither any public route nor any recognized authenticated route, execution reaches the bottom of the API block:
- Sends `fail(w, 404, "Endpoint not found.")` with `Content-Type: application/json`.
- This ensures 100% adherence to the JSON contract: `{"error": "<message>"}`.

---

## 5. Concrete Code Diff Plan for the Worker

The Worker should apply the following edits to `internal/httpapi/server.go`.

### 5.1 Replacement of `ServeHTTP` (Lines 182–338)

```go
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != "GET" && r.Method != "HEAD" && r.URL.Path != "/api/v1/webhooks/paystack" && r.Header.Get("Origin") != "" {
		expected := s.cfg.PublicURL
		if expected == "" {
			scheme := "http://"
			if s.secure() {
				scheme = "https://"
			}
			expected = scheme + r.Host
		}
		if r.Header.Get("Origin") != expected {
			fail(w, 403, "Origin not allowed.")
			return
		}
	}

	p := r.URL.Path

	// 1. Non-API Routes (static assets, previews, short links)
	if !strings.HasPrefix(p, "/api/") {
		if strings.HasPrefix(p, "/media/") {
			if r.Method != "GET" && r.Method != "HEAD" {
				fail(w, 405, "Method not allowed.")
				return
			}
			user := s.session(r)
			s.servePreview(w, r, user, strings.TrimPrefix(p, "/media/"))
			return
		}
		if strings.HasPrefix(p, "/l/") {
			if r.Method != "GET" && r.Method != "HEAD" {
				fail(w, 405, "Method not allowed.")
				return
			}
			id := strings.TrimPrefix(p, "/l/")
			if id != "" && !strings.Contains(id, "/") {
				http.Redirect(w, r, "/?gallery="+id, http.StatusFound)
				return
			}
			http.NotFound(w, r)
			return
		}
		if p == "/" || (!strings.Contains(p, "..") && !strings.HasPrefix(p, "/media/")) {
			http.FileServer(http.FS(public.Files)).ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}

	// 2. API Gateway: extract session for dual-mode and authenticated routes
	user := s.session(r)

	// 2a. Dispatch public & dual-mode API routes (no auth required)
	if s.publicAPI(w, r, user) {
		return
	}

	// 2b. Dispatch authenticated studio API routes
	if s.authenticatedAPI(w, r, user) {
		return
	}

	// 2c. Clean 404 for any unrecognized /api/ route (replaces line 330 503 catch-all)
	fail(w, 404, "Endpoint not found.")
}
```

### 5.2 Implementation of `publicAPI` in `server.go`

```go
func (s *Server) publicAPI(w http.ResponseWriter, r *http.Request, user *account) bool {
	p := r.URL.Path
	switch {
	case p == "/api/config":
		if r.Method != "GET" && r.Method != "HEAD" {
			fail(w, 405, "Method not allowed.")
			return true
		}
		mode := "unavailable"
		if s.cfg.PaystackSecret != "" {
			mode = "paystack"
		}
		reply(w, 200, map[string]any{
			"payments":         mode,
			"public_url":       s.cfg.PublicURL,
			"email_configured": false,
			"imgbb_previews":   false,
			"r2_uploads":       s.r2 != nil,
		})
		return true

	case p == "/api/v1/health":
		if r.Method != "GET" && r.Method != "HEAD" {
			fail(w, 405, "Method not allowed.")
			return true
		}
		reply(w, 200, map[string]string{"app": "pictide", "status": "partial"})
		return true

	case p == "/api/login":
		if r.Method != "POST" {
			fail(w, 405, "Method not allowed.")
			return true
		}
		s.login(w, r)
		return true

	case p == "/api/logout":
		if r.Method != "POST" {
			fail(w, 405, "Method not allowed.")
			return true
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "lumen_platform",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   s.secure(),
			SameSite: http.SameSiteStrictMode,
		})
		reply(w, 200, map[string]bool{"ok": true})
		return true

	case p == "/api/session":
		if r.Method != "GET" && r.Method != "HEAD" {
			fail(w, 405, "Method not allowed.")
			return true
		}
		if user == nil {
			reply(w, 200, map[string]any{"owner": false, "passwordRequired": true})
		} else {
			reply(w, 200, map[string]any{"owner": true, "passwordRequired": true, "username": user.Username, "role": user.Role})
		}
		return true

	case p == "/api/checkout":
		if r.Method != "POST" {
			fail(w, 405, "Method not allowed.")
			return true
		}
		s.checkout(w, r)
		return true

	case p == "/api/v1/webhooks/paystack":
		if r.Method != "POST" {
			fail(w, 405, "Method not allowed.")
			return true
		}
		s.webhook(w, r)
		return true

	case strings.HasPrefix(p, "/api/checkout/") && strings.HasSuffix(p, "/verify"):
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/api/checkout/"), "/verify")
		if id != "" && !strings.Contains(id, "/") {
			if r.Method != "POST" {
				fail(w, 405, "Method not allowed.")
				return true
			}
			s.verifyPayment(w, r, id)
			return true
		}
		return false

	case strings.HasPrefix(p, "/api/photos/") && strings.HasSuffix(p, "/download-link"):
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/api/photos/"), "/download-link")
		if id != "" && !strings.Contains(id, "/") {
			if r.Method != "POST" {
				fail(w, 405, "Method not allowed.")
				return true
			}
			s.downloadLink(w, r, id)
			return true
		}
		return false

	case strings.HasPrefix(p, "/api/download/"):
		id := strings.TrimPrefix(p, "/api/download/")
		if id != "" && !strings.Contains(id, "/") {
			if r.Method != "GET" && r.Method != "HEAD" {
				fail(w, 405, "Method not allowed.")
				return true
			}
			s.download(w, r, id)
			return true
		}
		return false

	case strings.HasPrefix(p, "/api/photos/"):
		parts := strings.Split(strings.TrimPrefix(p, "/api/photos/"), "/")
		if len(parts) == 2 && (parts[1] == "free" || parts[1] == "clean") {
			if r.Method != "GET" && r.Method != "HEAD" {
				fail(w, 405, "Method not allowed.")
				return true
			}
			s.photoDownload(w, r, parts[0], parts[1])
			return true
		}
		return false

	case strings.HasPrefix(p, "/api/galleries/"):
		parts := strings.Split(strings.TrimPrefix(p, "/api/galleries/"), "/")
		if len(parts) == 1 && parts[0] != "" {
			// Single-segment gallery ID: /api/galleries/:id
			// GET/HEAD is dual-mode (owner view if authenticated and owner, public view otherwise)
			// PATCH and DELETE are authenticated studio routes handled in authenticatedAPI
			if r.Method == "GET" || r.Method == "HEAD" {
				if user != nil {
					n, err := s.db.Collection("galleries").CountDocuments(r.Context(), bson.M{"studio_id": user.ID, "id": parts[0]})
					if err != nil {
						fail(w, 500, "Unable to load gallery.")
						return true
					}
					if n > 0 {
						s.getGallery(w, r, user, parts[0])
						return true
					}
				}
				s.publicGallery(w, r, parts[0])
				return true
			}
			return false
		}
		if len(parts) == 2 && parts[0] != "" && parts[1] == "free-download" {
			if r.Method != "GET" && r.Method != "HEAD" {
				fail(w, 405, "Method not allowed.")
				return true
			}
			s.freeArchive(w, r, parts[0])
			return true
		}
		if len(parts) == 2 && parts[0] != "" && parts[1] == "qr" {
			if r.Method != "GET" && r.Method != "HEAD" {
				fail(w, 405, "Method not allowed.")
				return true
			}
			if user != nil {
				n, err := s.db.Collection("galleries").CountDocuments(r.Context(), bson.M{"studio_id": user.ID, "id": parts[0]})
				if err != nil {
					fail(w, 500, "Unable to load gallery.")
					return true
				}
				if n > 0 {
					s.galleryQR(w, r, user, parts[0])
					return true
				}
			}
			var gallery struct {
				StudioID string `bson:"studio_id"`
			}
			if s.db.Collection("galleries").FindOne(r.Context(), bson.M{"id": parts[0], "status": "Published", "archived": bson.M{"$ne": 1}}).Decode(&gallery) != nil {
				fail(w, 404, "Gallery not found.")
				return true
			}
			s.galleryQR(w, r, &account{ID: gallery.StudioID}, parts[0])
			return true
		}
		return false
	}
	return false
}
```

### 5.3 Implementation of `authenticatedAPI` & `isAuthRoute` in `server.go`

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

func isAuthRoute(p string) bool {
	switch {
	case p == "/api/admin/overview",
		p == "/api/admin/insights",
		p == "/api/admin/accounts",
		p == "/api/admin/activity",
		p == "/api/admin/account-status",
		p == "/api/security/password",
		p == "/api/settings",
		p == "/api/galleries",
		p == "/api/orders",
		p == "/api/wallet":
		return true
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
	case strings.HasPrefix(p, "/api/orders/"):
		sub := strings.TrimPrefix(p, "/api/orders/")
		return strings.HasSuffix(sub, "/sync") && strings.TrimSuffix(sub, "/sync") != "" && !strings.Contains(strings.TrimSuffix(sub, "/sync"), "/")
	case strings.HasPrefix(p, "/api/direct-uploads/"):
		sub := strings.TrimPrefix(p, "/api/direct-uploads/")
		return strings.HasSuffix(sub, "/complete") && strings.TrimSuffix(sub, "/complete") != "" && !strings.Contains(strings.TrimSuffix(sub, "/complete"), "/")
	case strings.HasPrefix(p, "/api/photos/"):
		sub := strings.TrimPrefix(p, "/api/photos/")
		return sub != "" && !strings.Contains(sub, "/")
	default:
		return false
	}
}
```

---

## 6. Milestone Extension Hooks

When subsequent milestones implement their features, they plug into this structure effortlessly:

1. **Milestone M2: Auth Lifecycle (`/api/register`, `/api/account/verify`, etc.)**:
   - Add cases to `publicAPI`:
     ```go
     case p == "/api/register":
         if r.Method != "POST" { fail(w, 405, "Method not allowed."); return true }
         s.register(w, r)
         return true
     case p == "/api/account/verify":
         if r.Method != "POST" { fail(w, 405, "Method not allowed."); return true }
         s.verifyAccount(w, r)
         return true
     ```
   - For `/api/account/email` (authenticated): Add `p == "/api/account/email"` to `isAuthRoute` and dispatch in `workspace.go`.

2. **Milestone M4: Guest Uploads (`POST /api/galleries/:id/guest-uploads`)**:
   - Add to `publicAPI`:
     ```go
     if len(parts) == 2 && parts[0] != "" && parts[1] == "guest-uploads" && r.Method == "POST" {
         s.guestUpload(w, r, parts[0])
         return true
     }
     ```
   - `GET /api/galleries/:id/guest-uploads` (studio listing) will automatically fall through to `authenticatedAPI` because `isAuthRoute` already includes `"guest-uploads"`.

---

## 7. Verification Plan

1. **Build and Unit Tests**:
   Execute `go test ./...` to verify clean compilation and ensure all unit tests in `internal/httpapi/` and `internal/config/` pass without regression.
2. **Frontend Test Suite**:
   Run `node --test test/*.test.js` to ensure client interaction suites remain 100% green.
3. **Specific Route Verification**:
   - `GET /api/config` -> 200 JSON
   - `POST /api/config` -> 405 JSON
   - `GET /api/nonexistent` (unauthenticated) -> 404 JSON
   - `GET /api/nonexistent` (authenticated) -> 404 JSON (zero 503 errors!)
   - `GET /api/admin/overview` (unauthenticated) -> 401 JSON
   - `POST /api/orders` (authenticated) -> 405 JSON
