# Handoff Report: Database Architecture & MongoDB Transition Survey

**Agent**: `explorer_survey_2`  
**Working Directory**: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_survey_2\`  
**Target Recipient**: Orchestrator (`963791b5-32ce-4fe6-ad97-67f5764269a0`)  
**Type**: Hard Handoff (Investigation Complete)

---

## 1. Observation

1. **MongoDB Connection and Configuration**:
   - MongoDB driver version is `go.mongodb.org/mongo-driver/v2 v2.3.1` in `go.mod:13`.
   - Connected via `mongostore.Connect(ctx, c.MongoDBURI)` at `internal/app/app.go:30` using configuration loaded from `.env` via `internal/config/config.go:39-40` (`MONGODB_URI` and `MONGODB_DATABASE`).
   - The connection timeout is 15 seconds; graceful client disconnection is deferred (`app.go:38`).
   - Local MongoDB test was executed and verified working:
     Command `$env:PICTIDE_TEST_MONGODB='1'; go test -v -run '^TestMongoWorkspace$' ./internal/httpapi` passed (`PASS ok pictide.local/studio/internal/httpapi 12.635s`).

2. **Conversion Catch-All Stub**:
   - `internal/httpapi/server.go:327-331`:
     ```go
     if s.api(w, r, user) {
         return
     }
     fail(w, 503, "This feature is still being converted to MongoDB.")
     return
     ```
     Any authenticated API request matching `/api/...` that is not explicitly handled in `s.api` returns HTTP `503` with body `{"error":"This feature is still being converted to MongoDB."}`.

3. **Explicit 503 and Incomplete Feature Locations**:
   - `internal/httpapi/controls.go:150-153`:
     ```go
     if k == "allow_guest_upload" && n == 1 {
         fail(w, 503, "Guest uploads are not available yet.")
         return
     }
     ```
   - `internal/httpapi/commerce.go:236-239`:
     ```go
     if strings.HasPrefix(event.Event, "transfer.") {
         w.WriteHeader(503)
         return
     }
     ```
   - `internal/httpapi/wallet.go:68`:
     ```go
     reply(w, 200, bson.M{"earned": earned, "available": available, "feeBps": s.cfg.FeeBPS, "withdrawals": list, "configured": false})
     ```
   - `internal/httpapi/workspace.go:597`:
     `listOrders` returns hardcoded empty arrays: `"galleries": []any{}, "days": []any{}, "galleries": []any{}` inside the insights response and lacks filtering by `status`, `gallery`, `from`, or `to`.
   - `internal/httpapi/server.go:206`:
     Hardcodes `"email_configured": false, "imgbb_previews": false` in `/api/config`.
   - `internal/httpapi/server.go:210`:
     Returns `{"app": "pictide", "status": "partial"}` in `/api/v1/health`.

4. **Legacy Architecture and Prior Storage**:
   - Commit `ebf6b3c` (`internal/database/database.go:12-42`) reveals the legacy storage was SQLite (`modernc.org/sqlite`) using a multi-database architecture:
     - Platform database `platform.db` with tables `accounts`, `account_links`, `admin_audit`.
     - Per-studio SQLite databases at `studios/<studio_id>/studio.db` with tables `galleries`, `photos`, `orders`, `order_photos`, `entitlements`, `payment_events`, `withdrawals`, `settings`, `imgbb_previews`, `guest_uploads`.
     - Local filesystem media files at `data/studios/<studio_id>/media/`.
   - Commit `ebf6b3c:internal/httpapi/guests.go` and `internal/platform/accounts.go` contain the complete original implementations of guest submissions, SMTP account verification/reset, bank resolution, and Paystack transfers.

5. **Existing MongoDB Indexes in Codebase**:
   - `accounts`: `{username: 1}` (unique) in `server.go:55`.
   - `galleries`: `{id: 1}` (unique) in `server.go:59`, and `{studio_id: 1, id: 1}` in `mongostore/galleries.go:85`.
   - `photos`: `{id: 1}` (unique) and `{studio_id: 1, gallery_id: 1, hash: 1}` (unique) in `server.go:63-66`.
   - `orders`: `{id: 1}` (unique) in `server.go:70`.
   - `direct_uploads`: `{expires_at: 1}` (TTL expireAfterSeconds: 0) in `server.go:75`.
   - Missing collection indexes: `withdrawals`, `guest_uploads`, `account_links`, `admin_audit`, and `accounts.email`.

---

## 2. Logic Chain

1. **Premise 1**: The client frontend (`public/app.js`, `public/enhancements.js`, `public/admin-console.js`, `public/auth-admin.js`) issues requests to specific REST endpoints (`/api/banks`, `/api/banks/resolve`, `/api/withdrawals`, `/api/galleries/:id/guest-uploads`, `/api/guest-uploads/status`, `/api/guest-uploads/:id/review`, `/api/register`, `/api/account/verify`, `/api/account/forgot`, `/api/account/reset`, `/api/account/email`, `/api/settings/logo`, `/api/settings/apply-brand`, `/api/galleries/:id/regenerate`, `/api/admin/sync`).
2. **Premise 2**: In `internal/httpapi/server.go`, any `/api/...` route that is not routed before line 322 and is not handled in `s.api` (`internal/httpapi/workspace.go:17`) falls through to line 330: `fail(w, 503, "This feature is still being converted to MongoDB.")`. Public routes not intercepted before line 322 return `401 "Please sign in to your studio."`.
3. **Premise 3**: In commit `ebf6b3c`, all these features were fully functional in SQLite and per-studio files. During the initial MongoDB cutover in commit `d5a824f`, these files were removed, and only partial functionality was incrementally restored in subsequent commits (`f1de55b`, `25420dc`, `2acfdb6`).
4. **Premise 4**: MongoDB and GridFS are already configured and functional for core features (`accounts`, `galleries`, `photos`, `orders`, `direct_uploads`).
5. **Deduction**: Achieving production readiness and meeting acceptance criteria (zero 503 conversion stubs, all features operational with MongoDB) requires:
   - Defining and implementing the missing collections (`withdrawals`, `guest_uploads`, `account_links`, `admin_audit`, GridFS logo/pending assets).
   - Adding required indexes on startup in `httpapi.New`.
   - Routing and implementing all missing endpoints in `server.go` and `workspace.go:api`.
   - Supporting Paystack payout workflows, SMTP STARTTLS emails, and GridFS asset re-processing.

---

## 3. Caveats

1. **Live Paystack & SMTP Credentials**:
   - Production operations against live Paystack API and live SMTP mail servers require valid secret keys (`PAYSTACK_SECRET_KEY`, `LUMEN_SMTP_*`).
   - For automated verification and testing, mock Paystack HTTP handlers and test SMTP servers/mocks must be used to ensure tests pass without external dependencies.
2. **Cloudflare R2 Availability**:
   - Cloudflare R2 is an optional configuration (`R2_ACCOUNT_ID` etc.). When R2 is not configured, image uploads and downloads fall back to MongoDB GridFS. Implementation of guest submissions and brand regeneration must support both GridFS-only and hybrid R2 storage.
3. **Write Permission Constraint**:
   - As an explorer agent with read-only constraints, no application source code modifications have been made directly. Detailed specifications, schema definitions, queries, and roadmaps are provided in `survey_report.md`.

---

## 4. Conclusion

The database architecture is well-positioned for full production completion:
- MongoDB connection, client lifecycle, and schema conventions are cleanly established in `internal/mongostore/` and `internal/httpapi/`.
- The exact specifications for all 10 collections, GridFS namespaces, 18 indexes, and query aggregations are fully detailed in `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_survey_2\survey_report.md`.
- Implementation teams can directly resolve all occurrences of `503 "This feature is still being converted to MongoDB."` and placeholder responses by implementing the routed methods identified in Section 4 of `survey_report.md`.

---

## 5. Verification Method

To independently verify the observations and conclusions in this report:

1. **Verify MongoDB Connectivity**:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"
   go test -v -run "^TestMongoWorkspace$" ./internal/httpapi
   ```
   *Expected result*: Test connects to local MongoDB, runs all workspace operations, and exits with code 0 (`PASS`).

2. **Verify Conversion Stubs in Codebase**:
   Inspect line 330 of `internal/httpapi/server.go`:
   ```powershell
   Select-String -Path internal/httpapi/server.go -Pattern "converted to MongoDB"
   ```
   *Expected output*: Line 330 matching `fail(w, 503, "This feature is still being converted to MongoDB.")`.

3. **Verify Guest Uploads Stub**:
   ```powershell
   Select-String -Path internal/httpapi/controls.go -Pattern "Guest uploads are not available yet"
   ```
   *Expected output*: Line 151.

4. **Verify Paystack Webhook Transfer Stub**:
   ```powershell
   Select-String -Path internal/httpapi/commerce.go -Pattern "transfer\."
   ```
   *Expected output*: Line 236 returning 503.

5. **Inspect Survey Report**:
   Read `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_survey_2\survey_report.md` for full schema specifications and query patterns.
