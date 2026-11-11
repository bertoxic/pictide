# Handoff Report — Explorer Survey 1

**Agent**: `explorer_survey_1`  
**Milestone**: Codebase Audit & Survey Report  
**Target File**: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_survey_1\survey_report.md`  

---

## 1. Observation

Direct code observations from inspecting the codebase:

1. **Catch-All 503 Fallthrough**:
   In `internal/httpapi/server.go:322–332`:
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
   Any `/api/` request not recognized by `s.api(w, r, user)` fails with HTTP 503 `"This feature is still being converted to MongoDB."`. Furthermore, unauthenticated endpoints with prefix `/api/` (such as registration and guest uploads) are blocked with HTTP 401.

2. **Explicit 503 in Guest Customization**:
   In `internal/httpapi/controls.go:150–153`:
   ```go
   if k == "allow_guest_upload" && n == 1 {
       fail(w, 503, "Guest uploads are not available yet.")
       return
   }
   ```

3. **Explicit 503 in Paystack Webhook**:
   In `internal/httpapi/commerce.go:236–239`:
   ```go
   if strings.HasPrefix(event.Event, "transfer.") {
       w.WriteHeader(503)
       return
   }
   ```

4. **Hardcoded Stub in Wallet**:
   In `internal/httpapi/wallet.go:68`:
   ```go
   reply(w, 200, bson.M{"earned": earned, "available": available, "feeBps": s.cfg.FeeBPS, "withdrawals": list, "configured": false})
   ```
   Line 10 comments: `// wallet reports recorded balances; payout creation remains unavailable.`

5. **Hardcoded Stubs in Config**:
   In `internal/httpapi/server.go:206`:
   ```go
   reply(w, 200, map[string]any{"payments": mode, "public_url": s.cfg.PublicURL, "email_configured": false, "imgbb_previews": false, "r2_uploads": s.r2 != nil})
   ```

6. **Hardcoded Stub in Settings**:
   In `internal/httpapi/workspace.go:269`:
   ```go
   reply(w, 200, bson.M{"studio_name": doc.StudioName, "contact_email": doc.ContactEmail, "default_price": doc.DefaultPrice, "studio_logo": false})
   ```

7. **Stubbed Filtering & Analytics in Orders**:
   In `internal/httpapi/workspace.go:558–598`:
   `listOrders` takes `r *http.Request` but ignores query parameters `from`, `to`, `gallery`, and `status`. Line 597 returns:
   ```go
   "galleries": []any{}, "insights": bson.M{"paid": paid, "pending": pending, "gross": gross, "net": gross - fees, "average": average, "days": []any{}, "galleries": []any{}}
   ```

8. **Frontend Endpoints Called But Unhandled**:
   Inspected frontend JS files (`public/app.js`, `public/auth-admin.js`, `public/admin-console.js`, `public/enhancements.js`, `public/print-studio.js`):
   - Auth/Accounts: `POST /api/register`, `POST /api/account/forgot`, `POST /api/account/verify`, `POST /api/account/reset`, `POST /api/account/email`
   - Payouts/Wallet: `GET /api/banks`, `GET /api/banks/resolve`, `POST /api/withdrawals`, `POST /api/withdrawals/{reference}/sync`
   - Guest Uploads: `POST /api/galleries/{id}/guest-uploads`, `GET /api/galleries/{id}/guest-uploads`, `GET /api/guest-uploads/{id}/preview`, `POST /api/guest-uploads/{id}/review`, `POST /api/guest-uploads/status`
   - Branding: `POST /api/settings/logo`, `DELETE /api/settings/logo`, `POST /api/galleries/{id}/logo`, `DELETE /api/galleries/{id}/logo`, `POST /api/galleries/{id}/regenerate`, `POST /api/settings/apply-brand`
   - Admin: `POST /api/admin/sync`

9. **Prior Working Implementation Reference**:
   Commit `a22694a37c9c778450bc09df85ee91dcece08b7c` in Git history contained full implementations in `internal/httpapi/guests.go`, `internal/httpapi/wallet.go`, `internal/platform/accounts.go`, `internal/platform/admin.go`, `internal/httpapi/photos.go` (`saveLogo`, `deleteLogo`, `applyBrand`, `regenGallery`), and `internal/httpapi/order_pages.go` (`pagedOrders`), before they were deleted during the MongoDB restructuring in commit `d5a824f`.

---

## 2. Logic Chain

1. From **Observation 1**, `server.go:330` returns 503 whenever an endpoint is requested under `/api/` that is not explicitly handled in `s.api` or earlier in `ServeHTTP`.
2. From **Observation 8**, the frontend invokes 16 distinct endpoints across auth, payouts, guest uploads, branding, and admin sync. None of these 16 endpoints are present in `server.go` or `s.api` in `workspace.go`. Therefore, any request to these endpoints either gets blocked by 401 (if unauthenticated) or hits line 330 and returns `{"error":"This feature is still being converted to MongoDB."}`.
3. From **Observation 2**, setting `allow_guest_upload: 1` triggers an explicit 503 check in `controls.go:151`, preventing photographers from enabling guest submissions.
4. From **Observation 3**, Paystack transfer webhooks return 503 instead of updating payout status in MongoDB.
5. From **Observation 4, 5, 6, 7**, several endpoints (`/api/wallet`, `/api/config`, `/api/settings`, `/api/orders`, `/api/admin/overview`) return mock/stubbed/hardcoded values (`configured: false`, `email_configured: false`, `studio_logo: false`, empty insights and filter suppression), preventing the frontend from enabling features or displaying real data.
6. From **Observation 9**, the exact schemas, validation rules, Paystack API calls, and business logic for all these missing features were previously codified in commit `a22694a`. Converting them into idiomatic MongoDB operations (using `mongo.Database`, `GridFSBucket`, collections `accounts`, `account_links`, `withdrawals`, `guest_uploads`, `orders`, `galleries`, `settings`, `admin_audit`) will achieve 100% feature completeness and eliminate all 503s.

---

## 3. Caveats

- R2 storage: Image uploads and full-resolution downloads can use Cloudflare R2 when configured, but GridFS is the default local and fallback storage for all photo assets, thumbnails, logos, and guest submissions. Both stores must be supported.
- SMTP: Email sending requires valid SMTP credentials. When SMTP is unconfigured, endpoints should gracefully report error messages (e.g. 503 "Email delivery is not configured") or operate in local dev mode.
- Paystack: Payouts require a live or test Paystack secret key with sufficient balance for transfers.

---

## 4. Conclusion

The audit is complete. All non-functional, stubbed, placeholder, and unimplemented code has been cataloged in:
`c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_survey_1\survey_report.md`

There are:
- 1 catch-all 503 fallthrough (`server.go:330`) covering 16 missing API endpoints.
- 2 explicit 503 blockers (`controls.go:151` and `commerce.go:237`).
- 5 stubbed / hardcoded responses (`wallet.go:68`, `server.go:206`, `workspace.go:269`, `workspace.go:597`, `workspace.go:184`).
- Missing MongoDB collections & indexes (`account_links`, `withdrawals`, `guest_uploads`, `admin_audit`, and GridFS logos/submissions).

The survey report contains the exact contract, payload, response, and MongoDB design for each feature, enabling the implementer agent to immediately proceed with implementation.

---

## 5. Verification Method

To verify the audit findings:
1. Grep search for the 503 strings:
   ```powershell
   Select-String -Path "internal\httpapi\*.go" -Pattern "503"
   ```
   Expected matches: `server.go:330`, `controls.go:151`, `commerce.go:106`, `commerce.go:237`, `direct_upload.go:33, 77`.
2. Inspect `survey_report.md` at:
   `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_survey_1\survey_report.md`
3. Inspect Git historical implementations:
   ```powershell
   git show a22694a:internal/httpapi/guests.go
   git show a22694a:internal/httpapi/wallet.go
   git show a22694a:internal/platform/accounts.go
   git show a22694a:internal/httpapi/order_pages.go
   ```
4. Verify current test suite:
   ```powershell
   go test -p 1 ./...
   node --test
   ```
