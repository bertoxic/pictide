# Pictide Codebase Audit & Feature Survey Report

**Date**: 2026-11-10  
**Auditor**: Explorer Survey Agent (`explorer_survey_1`)  
**Workspace**: `c:\Users\Hp\Desktop\lumen v2`  
**Target Architecture**: Go 1.23.3 + MongoDB v2 Driver (`go.mongodb.org/mongo-driver/v2`) + GridFS + Paystack + Cloudflare R2  

---

## 1. Executive Summary

A comprehensive audit was performed across all routes, controllers, middleware, frontend JavaScript client scripts, and data store layers in the Pictide Studio codebase. The audit identified the exact reasons why features fail, are stubbed, or return `"This feature is still being converted to MongoDB."`.

### Core Architectural Findings

1. **Catch-All 503 Route Fallthrough**:
   In `internal/httpapi/server.go` lines 322–332:
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
   Any `/api/` endpoint that is not matched by `s.api(w, r, user)` in `internal/httpapi/workspace.go` or previous routes in `ServeHTTP` triggers this 503 error.
   Furthermore, any unauthenticated public endpoint (such as self-registration, password recovery, or guest photo submission) that is prefixed with `/api/` is blocked by `user == nil` (HTTP 401) before reaching the router.

2. **Explicit 503 Feature Blockers**:
   - `internal/httpapi/controls.go:151`: Explicitly returns HTTP 503 `"Guest uploads are not available yet."` when a photographer attempts to enable guest uploads (`allow_guest_upload: 1`).
   - `internal/httpapi/commerce.go:237`: Explicitly returns HTTP 503 when Paystack sends transfer webhook notifications (`transfer.success`, `transfer.failed`, `transfer.reversed`).

3. **Hardcoded Placeholder Flags & Stubbed Responses**:
   - `internal/httpapi/wallet.go:68`: Hardcodes `"configured": false`, disabling the "Withdraw funds" button in the frontend wallet view and displaying "Payout requests are not available yet."
   - `internal/httpapi/server.go:206`: Hardcodes `"email_configured": false` and `"imgbb_previews": false`, which disables frontend registration and password reset links.
   - `internal/httpapi/workspace.go:269`: Hardcodes `"studio_logo": false`, preventing the UI from recognizing an uploaded studio brand mark.
   - `internal/httpapi/workspace.go:597`: `listOrders` completely ignores query parameters (`status`, `gallery`, `from`, `to`) and returns empty stubs: `"galleries": []any{}`, `"insights": { ..., "days": []any{}, "galleries": []any{} }`, breaking the order insights charts and filtering.
   - `internal/httpapi/workspace.go:184`: `adminOverview` omits `email` and `verified` fields from photographer account records.

4. **Missing Endpoints**:
   A total of **16 API endpoints** called by the frontend or external webhooks are completely missing from the routing tables.

---

## 2. Comprehensive Inventory of Unimplemented / Stubbed Features

### Domain 1: Photographer Onboarding, Registration & Password Recovery

The application includes an onboarding workflow (`public/onboarding.js`) and account management screen (`public/auth-admin.js`), but the backend handlers for self-registration and recovery links are not wired to MongoDB.

#### 1.1 `POST /api/register`
- **Location in Code**: Called in `public/auth-admin.js:6` (`registerAccount`).
- **Access**: Public / Unauthenticated (Must be placed before `user == nil` check in `server.go`).
- **Request Payload**:
  ```json
  {
    "username": "photographer1",
    "email": "photographer@example.com",
    "password": "securepassword123"
  }
  ```
  - Validation: Username 3–64 chars matching `^[a-z0-9][a-z0-9._-]{2,63}$`, valid email, password 12–200 chars.
- **Expected Response**:
  - Success: HTTP 201 Created:
    ```json
    { "message": "Check your email for a verification link. If it does not arrive, request another link." }
    ```
  - Errors: HTTP 400 (validation), HTTP 409 (username or email already in use), HTTP 429 (rate limited: max 5 attempts/hour/IP), HTTP 503 (if SMTP is not configured).
- **MongoDB Operations**:
  - Check `accounts` collection for duplicate `username` or `email`.
  - Hash password with scrypt (`security.PasswordHash`).
  - Insert new document in `accounts` collection:
    ```bson
    {
      "_id": "<token>",
      "username": "<lowercase>",
      "email": "<lowercase>",
      "email_verified": false,
      "password_hash": "<salt:hash>",
      "role": "photographer",
      "session_version": "<token>",
      "disabled": false,
      "created": "<ISO timestamp>"
    }
    ```
  - Insert token link in `account_links` collection:
    ```bson
    {
      "token_hash": "<sha256(token)>",
      "account_id": "<account_id>",
      "email": "<email>",
      "kind": "verify",
      "expires": <unix timestamp + 30m>
    }
    ```
  - Send email via SMTP STARTTLS containing verification link: `<PUBLIC_URL>/#verify=<token>`.

#### 1.2 `POST /api/account/forgot`
- **Location in Code**: Called in `public/auth-admin.js:8` (`forgotAccount`).
- **Access**: Public / Unauthenticated.
- **Request Payload**:
  ```json
  { "email": "photographer@example.com" }
  ```
- **Expected Response**:
  - HTTP 200 OK:
    ```json
    { "message": "If an eligible account exists, a link has been sent." }
    ```
  - Error: HTTP 429 (rate limited: max 5 attempts/hour/IP), HTTP 503 (SMTP not configured).
- **MongoDB Operations**:
  - Query `accounts` collection by `email` (case-insensitive) where `disabled: false`.
  - If account exists: determine link kind (`"reset"` if `email_verified == true`, else `"verify"`).
  - Upsert `account_links` document with 30-minute expiry.
  - Send email via SMTP with reset link: `<PUBLIC_URL>/#reset=<token>`.

#### 1.3 `POST /api/account/email`
- **Location in Code**: Called in `public/auth-admin.js:13` (`saveAccountEmail`).
- **Access**: Authenticated Photographer or Admin (`user != nil`).
- **Request Payload**:
  ```json
  {
    "email": "newemail@example.com",
    "password": "currentpassword"
  }
  ```
- **Expected Response**:
  - HTTP 200 OK:
    ```json
    { "message": "Check your email to verify this address." }
    ```
  - Errors: HTTP 400 (invalid email or incorrect current password), HTTP 409 (email taken), HTTP 502/503 (SMTP error).
- **MongoDB Operations**:
  - Verify `security.CheckPassword(user.PasswordHash, input.Password)`.
  - Check `accounts` collection ensuring email is not in use by another account.
  - Insert link into `account_links` with `kind: "verify"` and send verification email.

#### 1.4 `POST /api/account/verify`
- **Location in Code**: Called in `public/auth-admin.js:11` (`finishAccountLink('verify')`).
- **Access**: Public / Unauthenticated.
- **Request Payload**:
  ```json
  { "token": "<64-hex token>" }
  ```
- **Expected Response**:
  - HTTP 200 OK:
    ```json
    { "message": "Account updated. You can sign in now." }
    ```
  - Errors: HTTP 400 (invalid or expired link, or email taken).
- **MongoDB Operations**:
  - Find document in `account_links` where `token_hash == HashToken(token)`, `kind == "verify"`, and `expires > now`.
  - Update `accounts` document: set `email: link.email`, `email_verified: true`.
  - Delete consumed link from `account_links`.

#### 1.5 `POST /api/account/reset`
- **Location in Code**: Called in `public/auth-admin.js:11` (`finishAccountLink('reset')`).
- **Access**: Public / Unauthenticated.
- **Request Payload**:
  ```json
  {
    "token": "<64-hex token>",
    "password": "newpassword123"
  }
  ```
- **Expected Response**:
  - HTTP 200 OK:
    ```json
    { "message": "Account updated. You can sign in now." }
    ```
  - Errors: HTTP 400 (invalid or expired link, or password < 12 characters).
- **MongoDB Operations**:
  - Find document in `account_links` where `token_hash == HashToken(token)`, `kind == "reset"`, and `expires > now`.
  - Hash new password with scrypt; generate new `session_version` token (revoking all active sessions).
  - Update `accounts` document with new `password_hash` and `session_version`.
  - Delete consumed link from `account_links`.

---

### Domain 2: Payouts & Wallet Management (Paystack Transfers & Webhooks)

Photographers earn funds when orders are paid via Paystack. The wallet view (`public/app.js:53-57`) allows photographers to view balance, resolve Nigerian bank accounts (NUBAN), initiate transfers, and sync transfer statuses.

#### 2.1 `GET /api/wallet` (Fix Stubbed Response)
- **Location in Code**: `internal/httpapi/wallet.go:10, 68`.
- **Current Behavior**: Returns `"configured": false`, disabling the withdrawal button.
- **Fix Required**:
  ```go
  reply(w, 200, bson.M{
      "earned": earned,
      "available": available,
      "feeBps": s.cfg.FeeBPS,
      "withdrawals": list,
      "configured": s.cfg.PaystackSecret != "",
  })
  ```

#### 2.2 `GET /api/banks`
- **Location in Code**: Called in `public/app.js:55` (`withdrawModal`).
- **Access**: Authenticated Photographer (`user != nil`).
- **Expected Response**:
  - HTTP 200 OK: JSON list of banks from Paystack:
    ```json
    [
      { "name": "Access Bank", "code": "044", ... },
      { "name": "Guaranty Trust Bank", "code": "058", ... }
    ]
    ```
  - Errors: HTTP 502/503 (Paystack unavailable).
- **Implementation**: Call Paystack client `c.Call(ctx, "GET", "/bank?country=nigeria&perPage=100", nil, &banks)`.

#### 2.3 `GET /api/banks/resolve`
- **Location in Code**: Called in `public/app.js:56` (`resolveBank`).
- **Access**: Authenticated Photographer (`user != nil`).
- **Query Parameters**: `?bank=<code>&account=<10-digit number>`
- **Expected Response**:
  - HTTP 200 OK:
    ```json
    {
      "account_number": "0123456789",
      "account_name": "JOHN DOE"
    }
    ```
  - Errors: HTTP 400 (invalid 10-digit account or bank code), HTTP 502 (Paystack verification failed).
- **Implementation**: Validate `account` matching `^\d{10}$` and `bank` matching `^\d{2,6}$`. Call Paystack `/bank/resolve?account_number=...&bank_code=...`.

#### 2.4 `POST /api/withdrawals`
- **Location in Code**: Called in `public/app.js:57` (`withdraw`).
- **Access**: Authenticated Photographer (`user != nil`).
- **Request Payload**:
  ```json
  {
    "amount": 500000,
    "account": "0123456789",
    "bank": "058",
    "name": "JOHN DOE"
  }
  ```
  *(Amount is in kobo: 500000 = 5,000.00 NGN)*
- **Expected Response**:
  - HTTP 200 OK:
    ```json
    {
      "reference": "lumen-...",
      "status": "pending" | "success" | "otp"
    }
    ```
  - Errors: HTTP 400 (insufficient balance, name mismatch, invalid parameters), HTTP 502 (Paystack transfer error), HTTP 503 (Paystack not configured).
- **MongoDB Operations & Logic**:
  1. Validate amount > 0, account (10 digits), bank code.
  2. Verify resolved account name with Paystack matches `name`.
  3. Query Paystack `/balance` to ensure balance >= amount.
  4. Query MongoDB `orders` and `withdrawals` for `studio_id == user.ID` to compute `available = earned - held`. Verify `amount <= available`.
  5. Call Paystack `/transferrecipient` (`type: "nuban"`, name, account, bank, currency: "NGN").
  6. Generate unique reference `lumen-<hex>` or `pictide-<hex>`.
  7. Insert document in `withdrawals` collection:
     ```bson
     {
       "id": "<token>",
       "studio_id": "<user.ID>",
       "reference": "<reference>",
       "amount": <amount>,
       "bank": "<bank>",
       "account_last4": "<account[6:]>",
       "status": "pending",
       "created": "<timestamp>"
     }
     ```
  8. Call Paystack `/transfer` (`source: "balance"`, amount, recipient, reference, reason: "Pictide studio payout").
  9. Update `withdrawals` document status in MongoDB.

#### 2.5 `POST /api/withdrawals/{reference}/sync`
- **Location in Code**: Called in `public/app.js:54` (`syncWithdrawal`).
- **Access**: Authenticated Photographer (`user != nil`).
- **Expected Response**:
  - HTTP 200 OK: `{ "status": "success" | "pending" | "failed" | "reversed" | "otp" }`
  - Errors: HTTP 404 (not found), HTTP 502 (Paystack verification failed).
- **MongoDB Operations**:
  - Query `withdrawals` where `reference == reference` and `studio_id == user.ID`.
  - Verify transfer status with Paystack `/transfer/verify/<reference>`.
  - Update status in `withdrawals` collection.

#### 2.6 `POST /api/v1/webhooks/paystack` (Fix Transfer Webhook 503)
- **Location in Code**: `internal/httpapi/commerce.go:236-239`.
- **Current Behavior**:
  ```go
  if strings.HasPrefix(event.Event, "transfer.") {
      w.WriteHeader(503)
      return
  }
  ```
- **Fix Required**:
  Handle `transfer.success`, `transfer.failed`, `transfer.reversed`:
  - Update `withdrawals` document where `reference == event.Data.Reference`: set `status: event.Data.Status`.
  - Return HTTP 200 OK.

---

### Domain 3: Guest Uploads / Contributions & Submissions Review

Guests can upload photos directly to published galleries when enabled by the photographer (`allow_guest_upload: 1`). Photographers review submissions and approve them (which adds them as free photos) or reject them.

#### 3.1 `PATCH /api/galleries/{id}/customization` (Fix 503 Blocker)
- **Location in Code**: `internal/httpapi/controls.go:150-153`.
- **Current Behavior**:
  ```go
  if k == "allow_guest_upload" && n == 1 {
      fail(w, 503, "Guest uploads are not available yet.")
      return
  }
  ```
- **Fix Required**: Remove the 503 error check so `allow_guest_upload: 1` can be saved to the gallery in MongoDB.

#### 3.2 `POST /api/galleries/{id}/guest-uploads`
- **Location in Code**: Called in `public/enhancements.js:62` (`submitGuestPhotos`).
- **Access**: Public / Guest (Unauthenticated).
- **Request Payload**: Multipart form with `photos` (1 to 5 image files, max 10MB each).
- **Expected Response**:
  - HTTP 200 OK:
    ```json
    {
      "token": "<64-hex token>",
      "added": [
        { "id": "<photo_id>", "name": "IMG_001.jpg", "status": "pending" }
      ],
      "errors": []
    }
    ```
  - Errors: HTTP 400 (too many files, invalid image), HTTP 403 (gallery not published, or guest uploads not enabled), HTTP 429 (IP upload rate limit).
- **MongoDB Operations**:
  - Verify gallery in `galleries` where `id == id`, `status == "Published"`, `archived != 1`, and `allow_guest_upload == 1`.
  - Rate limit by IP: max 30 uploads/hour.
  - Process image with `media.Process(data, media.Options{})`.
  - Save master preview into GridFS with ID `<photo_id>:pending`.
  - Insert records into `guest_uploads` collection:
    ```bson
    {
      "id": "<photo_id>",
      "studio_id": "<gallery.studio_id>",
      "gallery_id": "<gallery_id>",
      "name": "<filename>",
      "hash": "<sha256>",
      "token_hash": "<security.HashToken(token)>",
      "status": "pending",
      "created": "<timestamp>"
    }
    ```

#### 3.3 `POST /api/guest-uploads/status`
- **Location in Code**: Called in `public/enhancements.js:63` (`showMyUploads`).
- **Access**: Public / Guest (Unauthenticated).
- **Request Payload**:
  ```json
  { "tokens": ["<64-hex token>", ...] }
  ```
- **Expected Response**:
  - HTTP 200 OK:
    ```json
    [
      { "id": "<photo_id>", "gallery_id": "<id>", "name": "<filename>", "status": "pending" | "approved" | "rejected", "created": "<timestamp>" }
    ]
    ```

#### 3.4 `GET /api/galleries/{id}/guest-uploads`
- **Location in Code**: Called in `public/enhancements.js:60, 96` (`reviewUploads`, `loadContributionPreview`).
- **Access**: Authenticated Photographer (`user != nil`).
- **Expected Response**:
  - HTTP 200 OK:
    ```json
    [
      { "id": "<id>", "name": "<filename>", "status": "pending" | "approved" | "rejected", "created": "<timestamp>" }
    ]
    ```
- **MongoDB Operations**:
  - Query `guest_uploads` where `gallery_id == id` and `studio_id == user.ID`, sorted by `created: -1`.

#### 3.5 `GET /api/guest-uploads/{id}/preview`
- **Location in Code**: Used in `public/enhancements.js:60, 96` (`<img src="/api/guest-uploads/${p.id}/preview">`).
- **Access**: Public or Authenticated Photographer.
- **Expected Response**: HTTP 200 OK with `Content-Type: image/png` or `image/jpeg`.
- **MongoDB Operations**:
  - Verify record in `guest_uploads` with `id == id` and `status == "pending"`.
  - Stream `<id>:pending` from GridFS.

#### 3.6 `POST /api/guest-uploads/{id}/review`
- **Location in Code**: Called in `public/enhancements.js:61` (`reviewSubmission`).
- **Access**: Authenticated Photographer (`user != nil`).
- **Request Payload**:
  ```json
  { "action": "approve" | "reject" }
  ```
- **Expected Response**:
  - HTTP 200 OK: `{ "ok": true }`
  - Errors: HTTP 400 (invalid action), HTTP 404 (not found).
- **MongoDB Operations & Logic**:
  - Find `guest_uploads` document where `id == id`, `studio_id == user.ID`, and `status == "pending"`.
  - If `action == "approve"`:
    - Load gallery branding options from `galleries` and `settings`.
    - Retrieve pending image from GridFS (`<id>:pending`).
    - Process image via `media.Process(data, opts)`.
    - Store processed assets in GridFS (`:preview`, `:thumb`, `:clean`, `:clean_thumb`, `:master`) and R2 if configured.
    - Insert photo into `photos` collection with `price: 0` (free photo), `ready: true`, `archived: 0`.
    - Update `guest_uploads` document: `status: "approved"`.
    - Delete `<id>:pending` from GridFS.
  - If `action == "reject"`:
    - Update `guest_uploads` document: `status: "rejected"`.
    - Delete `<id>:pending` from GridFS.

---

### Domain 4: Studio & Gallery Branding Marks & Asset Regeneration

Photographers can upload studio logos and gallery logos, adjust proof watermarks, and regenerate existing photo assets so new brand marks are rendered onto photos.

#### 4.1 `POST /api/settings/logo`
- **Location in Code**: Called in `public/enhancements.js:34` (`uploadStudioLogo`).
- **Access**: Authenticated Photographer (`user != nil`).
- **Request Payload**: Multipart form with `logo` file (PNG, JPEG, WebP, max 2MB).
- **Expected Response**: HTTP 200 OK: `{ "ok": true }`.
- **MongoDB Operations**:
  - Decode image, resize/fit to 320x160 as PNG.
  - Save to GridFS with ID `<user.ID>:studio_logo`.
  - Update `settings` collection for `user.ID`: set `studio_logo: true`.

#### 4.2 `DELETE /api/settings/logo`
- **Location in Code**: Called in `public/enhancements.js:35` (`removeStudioLogo`).
- **Access**: Authenticated Photographer (`user != nil`).
- **Expected Response**: HTTP 200 OK: `{ "ok": true }`.
- **MongoDB Operations**:
  - Delete `<user.ID>:studio_logo` from GridFS.
  - Update `settings` collection: set `studio_logo: false`.

#### 4.3 `POST /api/galleries/{id}/logo`
- **Location in Code**: Called in `public/enhancements.js:40` (`saveBranding`).
- **Access**: Authenticated Photographer (`user != nil`).
- **Request Payload**: Multipart form with `logo` file (max 2MB).
- **Expected Response**: HTTP 200 OK: `{ "ok": true }`.
- **MongoDB Operations**:
  - Verify gallery belongs to `user.ID`.
  - Decode image, fit to 320x160 as PNG.
  - Save to GridFS with ID `<id>:logo`.
  - Update `galleries` collection: set `watermark_logo: "set"`.

#### 4.4 `DELETE /api/galleries/{id}/logo`
- **Location in Code**: Called in `public/enhancements.js:39` (`removeBrand`).
- **Access**: Authenticated Photographer (`user != nil`).
- **Expected Response**: HTTP 200 OK: `{ "ok": true }`.
- **MongoDB Operations**:
  - Verify gallery belongs to `user.ID`.
  - Delete `<id>:logo` from GridFS.
  - Update `galleries` collection: set `watermark_logo: ""`.

#### 4.5 `POST /api/galleries/{id}/regenerate`
- **Location in Code**: Called in `public/enhancements.js:40` (`saveBranding`).
- **Access**: Authenticated Photographer (`user != nil`).
- **Expected Response**:
  - HTTP 200 OK:
    ```json
    { "updated": 12, "errors": [] }
    ```
- **MongoDB Operations & Logic**:
  - Verify gallery belongs to `user.ID`.
  - Load gallery branding options and brand image (gallery logo if set, else studio logo if set).
  - Query all non-archived, ready photos for this gallery.
  - For each photo:
    - Read original photo data from GridFS (`<photo.id>:original`) or R2 (`r2PhotoKey(...)`).
    - Reprocess with `media.Process(originalData, opts)`.
    - Overwrite asset variants in GridFS (`:preview`, `:thumb`, `:clean`, `:clean_thumb`, `:master`) and R2.
  - Return `{ "updated": count, "errors": errList }`.

#### 4.6 `POST /api/settings/apply-brand`
- **Location in Code**: Called in `public/enhancements.js:36` (`applyStudioBrand`).
- **Access**: Authenticated Photographer (`user != nil`).
- **Expected Response**:
  - HTTP 200 OK:
    ```json
    { "updated": 45, "errors": [] }
    ```
- **MongoDB Operations & Logic**:
  - Find all non-archived galleries belonging to `user.ID`.
  - Execute photo regeneration across all galleries.
  - Return total updated count and any errors.

#### 4.7 `GET /api/settings` (Fix Hardcoded Studio Logo Flag)
- **Location in Code**: `internal/httpapi/workspace.go:269`.
- **Current Behavior**:
  ```go
  reply(w, 200, bson.M{"studio_name": doc.StudioName, "contact_email": doc.ContactEmail, "default_price": doc.DefaultPrice, "studio_logo": false})
  ```
- **Fix Required**:
  Decode `studio_logo: bool` from `settings` document and return `"studio_logo": doc.StudioLogo`.

---

### Domain 5: Orders Filtering, Sales Insights & Trends Analytics

In `public/order-insights.js`, the orders page allows filtering orders by date range (`from`, `to`), gallery, and status (`Paid`, `Pending`, `Failed`), and displays gross sales, net earnings, average order value, daily sales bars, and top galleries.

#### 5.1 `GET /api/orders` (Fix Stubbed Filtering & Empty Insights)
- **Location in Code**: `internal/httpapi/workspace.go:558-598` (`listOrders`).
- **Query Parameters**:
  - `limit`: int (default 50, max 100)
  - `offset`: int (default 0)
  - `status`: string (`"Paid"`, `"Pending"`, `"Failed"`)
  - `gallery`: string (gallery ID)
  - `from`: string (ISO datetime string)
  - `to`: string (ISO datetime string)
- **Expected Response**:
  ```json
  {
    "items": [
      {
        "id": "ord_...",
        "gallery_id": "gal_...",
        "gallery_name": "Wedding",
        "email": "client@example.com",
        "amount": 15000,
        "platform_fee": 0,
        "status": "Paid",
        "created": "2026-10-01T14:00:00Z"
      }
    ],
    "total": 1,
    "offset": 0,
    "limit": 50,
    "has_more": false,
    "galleries": [
      { "id": "gal_...", "name": "Wedding" }
    ],
    "insights": {
      "paid": 1,
      "pending": 0,
      "gross": 15000,
      "net": 15000,
      "average": 15000,
      "days": [
        ["2026-10-01", 15000]
      ],
      "galleries": [
        { "id": "gal_...", "name": "Wedding", "total": 15000, "count": 1 }
      ]
    }
  }
  ```
- **Current Defects**:
  - Ignores `status`, `gallery`, `from`, `to` query parameters.
  - Hardcodes `"galleries": []any{}` (so the gallery filter select in the UI has no options).
  - Hardcodes `"insights.days": []any{}` (so the daily sales trend chart is empty).
  - Hardcodes `"insights.galleries": []any{}` (so the top galleries list is empty).
- **MongoDB Operations Required**:
  - Build MongoDB filter matching `studio_id: user.ID`:
    - Add `status: status` if provided.
    - Add `gallery_id: gallery` if provided.
    - Add date range `created: { "$gte": from, "$lt": to }` if provided.
  - Aggregate/compute total count, paid count, pending count, gross amount, and fee totals.
  - Group paid orders by `created[:10]` (date) to generate the 12 most recent daily totals `[["YYYY-MM-DD", amount], ...]`.
  - Group paid orders by `gallery_id` to compute top 5 galleries by total sales: `[{ "id": ..., "name": ..., "total": ..., "count": ... }]`.
  - Find all distinct galleries associated with studio orders to populate the `galleries` choices list.

---

### Domain 6: Platform Administration Sync & Photographer Account Attributes

In `public/admin-console.js:22`, administrators have a "Refresh status" button to re-check pending orders or withdrawals directly with Paystack.

#### 6.1 `POST /api/admin/sync`
- **Location in Code**: Called in `public/admin-console.js:22` (`adminRefreshRecord`).
- **Access**: Administrator (`user.Role == "admin"`).
- **Request Payload**:
  ```json
  {
    "accountId": "photographer_id",
    "reference": "ref_or_order_id",
    "kind": "order" | "withdrawal"
  }
  ```
- **Expected Response**:
  - HTTP 200 OK: `{ "status": "Paid" | "Pending" | "Failed" | ... }`
  - Errors: HTTP 400 (invalid payload), HTTP 403 (not admin), HTTP 404 (account or record not found), HTTP 502 (Paystack verification failed).
- **MongoDB Operations**:
  - If `kind == "order"`:
    - Find order in `orders` collection where `id == reference` and `studio_id == accountId`.
    - Verify with Paystack `Verify(...)`.
    - Update `orders` status in MongoDB.
  - If `kind == "withdrawal"`:
    - Find withdrawal in `withdrawals` collection where `reference == reference` and `studio_id == accountId`.
    - Verify with Paystack `/transfer/verify/<reference>`.
    - Update `withdrawals` status in MongoDB.
  - Record audit log entry in `admin_audit` collection:
    ```bson
    {
      "id": "<token>",
      "actor": "<user.Username>",
      "action": "refreshed order" | "refreshed withdrawal",
      "target": "<target_username> · <reference>",
      "created": "<timestamp>"
    }
    ```

#### 6.2 `GET /api/admin/overview` (Fix Missing Email & Verified Fields)
- **Location in Code**: `internal/httpapi/workspace.go:184`.
- **Current Behavior**:
  Account objects in response omit `email` and `verified`, causing the admin UI to display `"No recovery email"` and `"Email unverified"`.
- **Fix Required**:
  Add `Email string` and `EmailVerified bool` to `account` struct and include in `accounts` array:
  ```go
  bson.M{
      "id": a.ID, "username": a.Username, "role": a.Role,
      "disabled": a.Disabled, "created": a.Created,
      "email": a.Email, "verified": a.EmailVerified,
      ...
  }
  ```

---

### Domain 7: Configuration & External Integrations Flags

#### 7.1 `GET /api/config`
- **Location in Code**: `internal/httpapi/server.go:206`.
- **Current Behavior**:
  ```go
  reply(w, 200, map[string]any{
      "payments": mode,
      "public_url": s.cfg.PublicURL,
      "email_configured": false,
      "imgbb_previews": false,
      "r2_uploads": s.r2 != nil,
  })
  ```
- **Fix Required**:
  - `"email_configured": s.cfg.SMTPHost != "" && s.cfg.SMTPFrom != ""`
  - `"imgbb_previews": s.cfg.ImgBBPreviews && s.cfg.ImgBBKey != ""`

---

## 3. MongoDB Schema & Index Requirements

To support the above functionality with high performance and data integrity, the following collections and indexes must be ensured:

| Collection | Key Fields | Required Indexes |
|---|---|---|
| `accounts` | `_id`, `username`, `email`, `email_verified`, `password_hash`, `role`, `session_version`, `disabled`, `created` | `username` (unique), `email` (sparse, unique) |
| `account_links` | `token_hash`, `account_id`, `email`, `kind`, `expires` | `token_hash` (unique), `expires` (for cleanup/TTL) |
| `galleries` | `id`, `studio_id`, `name`, `status`, `archived`, `price`, `cover_photo`, `allow_guest_upload`, `watermark_*`, `brand_*`, `card_design` | `id` (unique), `{studio_id: 1, id: 1}` |
| `photos` | `id`, `studio_id`, `gallery_id`, `name`, `hash`, `price`, `ready`, `archived`, `created`, `r2_master`, `r2_original` | `id` (unique), `{studio_id: 1, gallery_id: 1, hash: 1}` (unique) |
| `orders` | `id`, `studio_id`, `gallery_id`, `email`, `amount`, `status`, `created`, `platform_fee`, `items`, `token_hash`, `payment_provider` | `id` (unique), `{studio_id: 1, created: -1}`, `{token_hash: 1}` |
| `withdrawals` | `id`, `studio_id`, `reference`, `amount`, `bank`, `account_last4`, `status`, `created` | `reference` (unique), `{studio_id: 1, created: -1}` |
| `guest_uploads` | `id`, `studio_id`, `gallery_id`, `name`, `hash`, `token_hash`, `status`, `created` | `id` (unique), `{token_hash: 1}`, `{gallery_id: 1, status: 1}`, `{gallery_id: 1, hash: 1}` |
| `settings` | `_id` (=studio_id), `studio_name`, `contact_email`, `default_price`, `studio_logo` | `_id` (primary key) |
| `admin_audit` | `id`, `actor`, `action`, `target`, `created` | `{created: -1}` |
| `direct_uploads` | `_id`, `studio_id`, `gallery_id`, `name`, `size`, `object_key`, `status`, `expires_at` | `expires_at` (TTL index) |

GridFS Buckets:
- `fs.files` & `fs.chunks`:
  - Photos: `<photo_id>:original`, `<photo_id>:preview`, `<photo_id>:thumb`, `<photo_id>:clean`, `<photo_id>:clean_thumb`, `<photo_id>:master`
  - Logos: `<studio_id>:studio_logo`, `<gallery_id>:logo`
  - Guest submissions: `<upload_id>:pending`

---

## 4. Master Endpoint Audit Table

| HTTP Method | Route Pattern | Access Level | Calling File & Function | Current Status / Error | Target Handler / Action |
|---|---|---|---|---|---|
| `POST` | `/api/register` | Public (Unauthenticated) | `auth-admin.js:6` (`registerAccount`) | 401 Unauthorized / 503 Fallthrough | Self-registration & send verification email |
| `POST` | `/api/account/forgot` | Public (Unauthenticated) | `auth-admin.js:8` (`forgotAccount`) | 401 Unauthorized / 503 Fallthrough | Account recovery & send reset link |
| `POST` | `/api/account/verify` | Public (Unauthenticated) | `auth-admin.js:11` (`finishAccountLink`) | 401 Unauthorized / 503 Fallthrough | Verify account email address |
| `POST` | `/api/account/reset` | Public (Unauthenticated) | `auth-admin.js:11` (`finishAccountLink`) | 401 Unauthorized / 503 Fallthrough | Reset account password |
| `POST` | `/api/account/email` | Authenticated Photographer/Admin | `auth-admin.js:13` (`saveAccountEmail`) | 503 Fallthrough | Set recovery email for current user |
| `GET` | `/api/wallet` | Authenticated Photographer | `app.js:53` (`walletView`) | Hardcoded `"configured": false` | Enable payout when Paystack configured |
| `GET` | `/api/banks` | Authenticated Photographer | `app.js:55` (`withdrawModal`) | 503 Fallthrough | List Nigerian banks via Paystack |
| `GET` | `/api/banks/resolve` | Authenticated Photographer | `app.js:56` (`resolveBank`) | 503 Fallthrough | Resolve bank NUBAN via Paystack |
| `POST` | `/api/withdrawals` | Authenticated Photographer | `app.js:57` (`withdraw`) | 503 Fallthrough | Initiate Paystack wallet payout |
| `POST` | `/api/withdrawals/{ref}/sync` | Authenticated Photographer | `app.js:54` (`syncWithdrawal`) | 503 Fallthrough | Sync single transfer status with Paystack |
| `POST` | `/api/v1/webhooks/paystack` | Public (Webhook) | Paystack webhook event | Returns 503 on `transfer.*` events | Update withdrawal status on transfer events |
| `PATCH` | `/api/galleries/{id}/customization` | Authenticated Photographer | `enhancements.js:59` (`toggleGuestUploads`) | Returns 503 if `allow_guest_upload: 1` | Allow setting `allow_guest_upload: 1` |
| `POST` | `/api/galleries/{id}/guest-uploads` | Public (Guest) | `enhancements.js:62` (`submitGuestPhotos`) | 401 Unauthorized / 503 Fallthrough | Guest uploads photos for review |
| `GET` | `/api/galleries/{id}/guest-uploads` | Authenticated Photographer | `enhancements.js:60, 96` (`reviewUploads`) | 503 Fallthrough | List guest submissions for review |
| `GET` | `/api/guest-uploads/{id}/preview` | Public or Authenticated | `enhancements.js:60, 96` (`reviewUploads`) | 401 Unauthorized / 503 Fallthrough | Stream pending guest upload preview |
| `POST` | `/api/guest-uploads/{id}/review` | Authenticated Photographer | `enhancements.js:61` (`reviewSubmission`) | 503 Fallthrough | Approve or reject guest submission |
| `POST` | `/api/guest-uploads/status` | Public (Guest) | `enhancements.js:63` (`showMyUploads`) | 401 Unauthorized / 503 Fallthrough | Check status of guest submissions |
| `POST` | `/api/settings/logo` | Authenticated Photographer | `enhancements.js:34` (`uploadStudioLogo`) | 503 Fallthrough | Upload studio brand mark to GridFS |
| `DELETE` | `/api/settings/logo` | Authenticated Photographer | `enhancements.js:35` (`removeStudioLogo`) | 503 Fallthrough | Delete studio brand mark from GridFS |
| `POST` | `/api/galleries/{id}/logo` | Authenticated Photographer | `enhancements.js:40` (`saveBranding`) | 503 Fallthrough | Upload gallery logo to GridFS |
| `DELETE` | `/api/galleries/{id}/logo` | Authenticated Photographer | `enhancements.js:39` (`removeBrand`) | 503 Fallthrough | Delete gallery logo from GridFS |
| `POST` | `/api/galleries/{id}/regenerate` | Authenticated Photographer | `enhancements.js:40` (`saveBranding`) | 503 Fallthrough | Regenerate all watermarked previews |
| `POST` | `/api/settings/apply-brand` | Authenticated Photographer | `enhancements.js:36` (`applyStudioBrand`) | 503 Fallthrough | Apply brand mark across all galleries |
| `GET` | `/api/settings` | Authenticated Photographer | `app.js:3`, `enhancements.js:32` | Hardcoded `"studio_logo": false` | Return actual `studio_logo` flag |
| `GET` | `/api/orders` | Authenticated Photographer | `order-insights.js:4` (`orderPageURL`) | Filters ignored, insights days/galleries empty | Full filtering (`status`, `gallery`, `from`, `to`) & insights trends |
| `POST` | `/api/admin/sync` | Authenticated Admin | `admin-console.js:22` (`adminRefreshRecord`) | 503 Fallthrough | Refresh order or withdrawal with Paystack |
| `GET` | `/api/admin/overview` | Authenticated Admin | `admin-console.js:3`, `auth-admin.js:14` | Omits `email` and `verified` fields | Include photographer email & verification status |
| `GET` | `/api/config` | Public | `auth-admin.js:19`, `app.js:1` | Hardcodes `email_configured: false` | Reflect actual SMTP configuration |

---

## 5. Verification Checklist for Downstream Implementation

When the implementation agent fulfills this survey:
- [ ] No route in `ServeHTTP` or `s.api` returns `"This feature is still being converted to MongoDB."`.
- [ ] `allow_guest_upload: 1` succeeds with HTTP 200 without throwing 503.
- [ ] Paystack transfer webhooks return HTTP 200 and reconcile `withdrawals` status.
- [ ] Wallet payouts (`/api/banks`, `/api/banks/resolve`, `/api/withdrawals`, `/api/withdrawals/{ref}/sync`) are fully operational against Paystack and MongoDB.
- [ ] Guest photo submissions, status tracking, previewing, and photographer approval/rejection work end-to-end.
- [ ] Brand mark upload, removal, gallery logo, photo regeneration, and app-wide brand application work and persist to MongoDB GridFS.
- [ ] Orders filtering (`from`, `to`, `gallery`, `status`) and insight aggregations (`days`, `galleries`) return valid data for charts.
- [ ] Self-registration, password recovery, verification links, and SMTP delivery function without errors.
- [ ] Admin sync endpoint (`/api/admin/sync`) refreshes orders and payouts and logs audit entries.
- [ ] Automated tests cover all newly implemented features and pass completely.
