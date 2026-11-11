# Database Architecture & MongoDB Migration Survey Report

**Generated Date**: 2026-11-10T20:48:00Z  
**Investigator**: Explorer Survey 2  
**Target Repository**: `pictide.local/studio` (`c:\Users\Hp\Desktop\lumen v2`)  
**Objective**: Comprehensive investigation into the database architecture, MongoDB connection lifecycle, data models, legacy data layer vs MongoDB transition, and specifications for all collections, indexes, and queries required to bring stubbed and partially implemented features to production readiness.

---

## 1. Executive Summary

Pictide Studio is undergoing a transition from an embedded SQLite multi-tenant architecture to a centralized, horizontally scalable MongoDB data store (`go.mongodb.org/mongo-driver/v2 v2.3.1`). 

While basic studio workspace capabilities (gallery creation, image uploads via multipart or Cloudflare R2 direct uploads, photo pricing, covers, card styling, and basic order checkouts) have been wired to MongoDB, key features were stubbed during the conversion. Specifically:
- Authenticated endpoints that have not yet been migrated fall through `internal/httpapi/server.go:327-331` and return `503 {"error":"This feature is still being converted to MongoDB."}`.
- Guest uploads return `503 {"error":"Guest uploads are not available yet."}` at `internal/httpapi/controls.go:151`.
- Paystack webhook handlers discard transfer notifications with `503` at `internal/httpapi/commerce.go:237`.
- Payouts/withdrawals, guest submissions, account email recovery, self-registration, brand mark logo upload/regeneration, admin sync, and advanced order analytics filters are either missing routes or return placeholder responses.

This report documents the exact database architecture, connection management, legacy SQLite schema comparison, full collection catalog, index definitions, and query patterns needed to make every feature production-ready on MongoDB.

---

## 2. MongoDB Initialization, Connection Lifecycle & Configuration

### 2.1 Configuration
Configuration is managed through `internal/config/config.go` (`Config` struct) and loaded from `.env` via `godotenv`:
- `MONGODB_URI`: Connection string (defaults to `mongodb://127.0.0.1:27017` in `.env.example`).
- `MONGODB_DATABASE`: Target database name (defaults to `pictide`).

### 2.2 Connection Initialization and Lifecycle
Connection management is isolated in `internal/mongostore/connect.go`:
```go
func Connect(ctx context.Context, uri string) (*mongo.Client, error) {
    if uri == "" {
        return nil, errors.New("MONGODB_URI is required")
    }
    client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(10 * time.Second))
    if err != nil {
        return nil, errors.New("invalid MongoDB configuration")
    }
    if err := client.Ping(ctx, readpref.Primary()); err != nil {
        _ = client.Disconnect(context.Background())
        return nil, errors.New("MongoDB connection failed")
    }
    return client, nil
}
```

In `internal/app/app.go` (`Run()`, lines 29–48):
1. A connection timeout context of 15 seconds is created.
2. `mongostore.Connect` establishes the `*mongo.Client`.
3. Graceful disconnection is deferred (`client.Disconnect`).
4. Gallery repository is instantiated (`mongostore.NewGalleries`) and initial indexes are ensured (`EnsureIndexes`).
5. The `httpapi.Server` is instantiated via `httpapi.New(ctx, client.Database(c.MongoDBDatabase), c)`.

### 2.3 Existing Collections & Indexes Created on Startup
In `internal/httpapi/server.go` (`New()`, lines 55–91):
1. `accounts`:
   - Unique index on `username`: `bson.D{{Key: "username", Value: 1}}`
   - Default upsert of legacy admin account: `_id: "legacy"`, `username: "admin"`.
2. `galleries`:
   - Unique index on `id`: `bson.D{{Key: "id", Value: 1}}`
   - Plus studio isolation index created by `mongostore.Galleries`: `bson.D{{Key: "studio_id", Value: 1}, {Key: "id", Value: 1}}`
3. `photos`:
   - Unique index on `id`: `bson.D{{Key: "id", Value: 1}}`
   - Unique composite index on `{studio_id: 1, gallery_id: 1, hash: 1}` (prevents duplicate images within the same gallery)
4. `orders`:
   - Unique index on `id`: `bson.D{{Key: "id", Value: 1}}`
5. `direct_uploads` (when Cloudflare R2 is configured):
   - TTL index on `expires_at`: `bson.D{{Key: "expires_at", Value: 1}}` with `SetExpireAfterSeconds(0)`
6. GridFS Bucket:
   - Accessed via `s.db.GridFSBucket()` (using default `fs.files` and `fs.chunks` collections).

### 2.4 Hybrid Storage Architecture: MongoDB GridFS & Cloudflare R2
The system utilizes a tiered storage strategy:
- **Small/Preview Assets & Local Fallback**: Stored directly in MongoDB GridFS with filenames `<photo_id>:preview`, `<photo_id>:thumb`, `<photo_id>:clean`, `<photo_id>:clean_thumb`, `<photo_id>:master`, `<photo_id>:original`.
- **Large Master & Original Files (when R2 configured)**:
  - Browser uploads directly to R2 with pre-signed PUT URLs (`direct_uploads` tracks completion).
  - Clean/master previews remain in GridFS for low-latency delivery.
  - Full-resolution downloads stream from R2 using short-lived signed GET URLs (`commerce.go:273-305`).
  - Fallback: If R2 is not configured, GridFS stores all variants.

---

## 3. Legacy Architecture vs. MongoDB Transition

### 3.1 Legacy Architecture (SQLite & Filesystem)
As observed in commit `ebf6b3c` (`internal/database/database.go` and `internal/platform/platform.go`):
- **Platform SQLite Database (`platform.db`)**:
  - `accounts`: `id`, `username`, `password_hash`, `role`, `session_version`, `created`, `email`, `email_verified`, `disabled`.
  - `account_links`: `token_hash`, `account_id`, `email`, `kind`, `expires`.
  - `admin_audit`: `id`, `actor`, `action`, `target`, `created`.
- **Per-Studio SQLite Databases (`data/studios/<studio_id>/studio.db`)**:
  - `galleries`: id, name, client, location, event_date, price, status, archived, watermark_*, brand_*, share_*, cover_photo, card_design, allow_guest_upload.
  - `photos`: id, gallery_id, name, hash, created, price, archived.
  - `orders`: id, gallery_id, email, amount, status, created, platform_fee.
  - `order_photos`: order_id, photo_id, unit_price.
  - `entitlements`: token_hash, order_id, downloads_used, max_downloads.
  - `withdrawals`: id, reference, amount, bank, account_last4, status, created.
  - `settings`: key, value (e.g. `studio_name`, `default_price`).
  - `guest_uploads`: id, gallery_id, name, hash, token_hash, status, created.
  - `imgbb_previews`: photo_id, variant, signature, url, expires, retry_after.
- **Per-Studio Filesystem Media (`data/studios/<studio_id>/media/`)**:
  - Files stored on disk as `<photo_id>.png`, `<photo_id>.jpg`, `<photo_id>_thumb.jpg`, `<photo_id>_clean.jpg`, `<photo_id>_thumb_clean.jpg`, `<photo_id>.original`, `<photo_id>_pending.png`, `studio_logo.png`, `<gallery_id>_logo.png`.

### 3.2 MongoDB Consolidated Multi-Tenant Architecture
Instead of multiple SQLite database files and disk directories, MongoDB consolidates all data into a single database:
- **Tenant Isolation**: Every resource belongs to a studio via `studio_id` (or `_id` for studio settings).
- **Orders & Entitlements Embedded**: `orders` collection embeds line items (`items: [{photo_id, unit_price}]`) and entitlement limits (`token_hash`, `downloads_used`, `max_downloads`), eliminating the need for `order_photos` and `entitlements` join tables.
- **Binary Assets in GridFS**: Files previously on disk are stored in GridFS or Cloudflare R2, enabling stateless application server deployments (e.g., Vercel, Docker).

---

## 4. Comprehensive Feature Gap Analysis

Below is the inventory of all features currently unimplemented, stubbed with `{"error":"This feature is still being converted to MongoDB."}`, or returning `503`:

| Feature Area | Endpoint / Trigger | Current Code Location & State | Target MongoDB Behavior |
|---|---|---|---|
| **Catch-All Stub** | Any unhandled `/api/...` route | `internal/httpapi/server.go:330` returns 503 `"This feature is still being converted to MongoDB."` | Should be 0 unhandled routes; all valid endpoints handled, invalid routes return 404 |
| **Guest Upload Customization** | `PATCH /api/galleries/:id/customization` | `internal/httpapi/controls.go:150-153` returns `503 "Guest uploads are not available yet."` when `allow_guest_upload == 1` | Persist `allow_guest_upload: 1` in `galleries` collection |
| **Guest Photo Submission** | `POST /api/galleries/:id/guest-uploads` | Public route not caught in `server.go`, falls through to auth / 401 | Validate gallery `allow_guest_upload == 1`, store image in GridFS `<id>:pending`, insert document into `guest_uploads` with `status: "pending"` |
| **Guest Submission Status** | `POST /api/guest-uploads/status` | Public route not caught in `server.go`, falls through to auth / 401 | Query `guest_uploads` by `token_hash` in `$in: [hashes...]`, return list |
| **Guest Submissions Review** | `GET /api/galleries/:id/guest-uploads` | Unhandled in `workspace.go:api`, returns 503 stub | Query `guest_uploads` where `studio_id == user.ID` and `gallery_id == id`, sort by `created: -1` |
| **Guest Submission Preview** | `GET /api/guest-uploads/:id/preview` | Unhandled in `server.go` / `workspace.go`, returns 503 stub | Stream image from GridFS `<id>:pending` |
| **Guest Submission Action** | `POST /api/guest-uploads/:id/review` | Unhandled in `workspace.go:api`, returns 503 stub | `approve`: process image with watermark via `media.Process`, insert to `photos` with `price: 0`, save GridFS/R2 variants, mark `status: "approved"`. `reject`: mark `status: "rejected"`, remove pending GridFS file |
| **Banks List** | `GET /api/banks` | Unhandled in `server.go` / `workspace.go:api`, returns 503 stub | Call Paystack `/bank?country=nigeria&perPage=100`, return JSON bank array |
| **Bank Account Resolve** | `GET /api/banks/resolve` | Unhandled in `server.go` / `workspace.go:api`, returns 503 stub | Call Paystack `/bank/resolve?account_number=...&bank_code=...`, return resolved account |
| **Withdrawal Request** | `POST /api/withdrawals` | Unhandled in `workspace.go:api`, returns 503 stub | Verify balance in `orders` vs `withdrawals`, initiate Paystack transfer, insert record into `withdrawals` collection |
| **Withdrawal Status Sync** | `POST /api/withdrawals/:reference/sync` | Unhandled in `workspace.go:api`, returns 503 stub | Verify with Paystack `/transfer/verify/:ref`, update `status` in `withdrawals` collection |
| **Transfer Webhooks** | `POST /api/v1/webhooks/paystack` | `commerce.go:236-239` returns `503` on `transfer.*` events | Handle `transfer.success`, `transfer.failed`, `transfer.reversed`, update `withdrawals` status, return 200 |
| **Wallet Configuration Flag** | `GET /api/wallet` | `wallet.go:68` returns `"configured": false` | Set `"configured": s.cfg.PaystackSecret != ""` |
| **Self-Registration** | `POST /api/register` | Public route not intercepted before auth check, returns 401 | Validate input, insert account with `email_verified: false`, insert `account_links` token, send SMTP STARTTLS email |
| **Email Verification** | `POST /api/account/verify` | Public route not intercepted, returns 401 | Consume `account_links` token (`kind: "verify"`), set `email_verified: true` in `accounts` |
| **Password Forgot** | `POST /api/account/forgot` | Public route not intercepted, returns 401 | Check email in `accounts`, insert `account_links` token (`kind: "reset"`), send SMTP email |
| **Password Reset** | `POST /api/account/reset` | Public route not intercepted, returns 401 | Consume `account_links` token (`kind: "reset"`), update `password_hash` and `session_version` in `accounts` |
| **Account Email Bind** | `POST /api/account/email` | Authenticated route unhandled in `workspace.go:api`, returns 503 stub | Verify current password, insert `account_links` token, send verification email |
| **Studio Logo Upload** | `POST /api/settings/logo` | Unhandled in `workspace.go:api`, returns 503 stub | Validate and resize logo to 320x160 PNG, store in GridFS `logo:studio:<studio_id>`, set `studio_logo: true` in `settings` |
| **Studio Logo Delete** | `DELETE /api/settings/logo` | Unhandled in `workspace.go:api`, returns 503 stub | Delete GridFS `logo:studio:<studio_id>`, set `studio_logo: false` in `settings` |
| **Gallery Logo Upload** | `POST /api/galleries/:id/logo` | Unhandled in `workspace.go:api`, returns 503 stub | Validate logo, store in GridFS `logo:gallery:<gallery_id>`, set `watermark_logo: "set"` in `galleries` |
| **Gallery Logo Delete** | `DELETE /api/galleries/:id/logo` | Unhandled in `workspace.go:api`, returns 503 stub | Delete GridFS `logo:gallery:<gallery_id>`, set `watermark_logo: ""` in `galleries` |
| **Apply Brand to All Photos** | `POST /api/settings/apply-brand` | Unhandled in `workspace.go:api`, returns 503 stub | Iterate all studio galleries, reload original image from GridFS/R2, reprocess assets with current brand options, replace GridFS/R2 variants |
| **Gallery Regenerate** | `POST /api/galleries/:id/regenerate` | Unhandled in `workspace.go:api`, returns 503 stub | Reprocess all photos in specified gallery with updated watermark/logo |
| **Admin Refresh Record** | `POST /api/admin/sync` | Unhandled in `workspace.go:api`, returns 503 stub | Admin refreshes order or withdrawal directly, updates DB, records in `admin_audit` |
| **Order Search & Trends** | `GET /api/orders` | `workspace.go:597` returns hardcoded empty `galleries: []`, `days: []`, `galleries: []` | Implement filters (`status`, `gallery`, `from`, `to`), calculate daily sales trends and top galleries |
| **Config Flags** | `GET /api/config` | `server.go:206` hardcodes `email_configured: false`, `imgbb_previews: false` | Return dynamic booleans based on SMTP credentials and ImgBB configuration |
| **Health Check Status** | `GET /api/v1/health` | `server.go:210` returns `status: "partial"` | Return `status: "ok"` when database ping succeeds |

---

## 5. Complete MongoDB Data Layer Specification

### 5.1 Collection: `accounts`
Stores administrative and photographer tenant accounts.

**Schema Fields**:
- `_id`: `string` (UUID or `"legacy"`)
- `username`: `string` (lowercase, 3–64 chars)
- `password_hash`: `string` (scrypt hash `salt:hash`)
- `role`: `string` (`"admin"` or `"photographer"`)
- `session_version`: `string` (UUID/random token for session invalidation)
- `email`: `string` (optional, lowercase email address)
- `email_verified`: `bool` (default `false`)
- `disabled`: `bool` (default `false`)
- `created`: `string` (RFC3339Nano)

**Indexes**:
```go
mongo.IndexModel{Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetUnique(true)}
mongo.IndexModel{
    Keys: bson.D{{Key: "email", Value: 1}}, 
    Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{"email": bson.M{"$type": "string", "$gt": ""}}),
}
mongo.IndexModel{Keys: bson.D{{Key: "created", Value: -1}}}
```

---

### 5.2 Collection: `account_links`
Stores single-use security tokens for email verification and password resets.

**Schema Fields**:
- `_id`: `string` (Token ID)
- `token_hash`: `string` (SHA-256 hex string of 64-char random token)
- `account_id`: `string` (foreign key to `accounts._id`)
- `email`: `string` (target email address)
- `kind`: `string` (`"verify"` or `"reset"`)
- `expires_at`: `time.Time` (30 minutes from creation)
- `created`: `string` (RFC3339Nano)

**Indexes**:
```go
mongo.IndexModel{Keys: bson.D{{Key: "token_hash", Value: 1}}, Options: options.Index().SetUnique(true)}
mongo.IndexModel{Keys: bson.D{{Key: "account_id", Value: 1}, {Key: "kind", Value: 1}}}
mongo.IndexModel{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)}
```

---

### 5.3 Collection: `galleries`
Stores event photo galleries scoped to studios.

**Schema Fields**:
- `id`: `string` (random 64-char token, primary external identifier)
- `studio_id`: `string` (foreign key to `accounts._id`)
- `name`: `string` (gallery title)
- `client`: `string` (client name)
- `location`: `string` (location)
- `event_date`: `string` (date string)
- `price`: `int64` (default photo price in kobo/cents)
- `status`: `string` (`"Draft"` or `"Published"`)
- `archived`: `int` (`0` for active, `1` for archived)
- `cover_photo`: `string` (photo ID or empty)
- `watermark_text`: `string` (default `"PICTIDE · PROOF"`)
- `watermark_style`: `string` (`"diagonal"`, `"center"`, `"tile"`, `"corner"`)
- `watermark_logo`: `string` (`"set"` or `""`)
- `watermark_color`: `string` (`#ffffff`)
- `watermark_opacity`: `int` (0–100)
- `watermark_size`: `int` (1–10)
- `brand_position`: `string` (`"top-left"`, `"top-right"`, `"bottom-left"`, `"bottom-right"`)
- `brand_size`: `int` (5–30)
- `brand_opacity`: `int` (0–100)
- `share_title`: `string`
- `share_message`: `string`
- `share_color`: `string` (`#4f5b2a`)
- `share_qr_background`: `string` (`#ffffff`)
- `allow_guest_upload`: `int` (`0` = disabled, `1` = enabled)
- `card_design`: `string` (JSON layout document)
- `created`: `string` (RFC3339Nano)

**Indexes**:
```go
mongo.IndexModel{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)}
mongo.IndexModel{Keys: bson.D{{Key: "studio_id", Value: 1}, {Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)}
mongo.IndexModel{Keys: bson.D{{Key: "studio_id", Value: 1}, {Key: "archived", Value: 1}, {Key: "created", Value: -1}}}
mongo.IndexModel{Keys: bson.D{{Key: "status", Value: 1}, {Key: "archived", Value: 1}}}
```

---

### 5.4 Collection: `photos`
Stores photograph metadata, prices, processing states, and storage pointers.

**Schema Fields**:
- `id`: `string` (random 64-char token)
- `studio_id`: `string`
- `gallery_id`: `string`
- `name`: `string` (original filename)
- `hash`: `string` (SHA-256 hex string of original image content)
- `price`: `*int64` (`nil` = use gallery default price; `0` = free photo; `>0` = custom price)
- `ready`: `bool` (set to `true` after all variants are written)
- `archived`: `int` (`0` = active, `1` = archived)
- `r2_master`: `bool` (indicates whether master variant is stored in R2)
- `r2_original`: `bool` (indicates whether original file is stored in R2)
- `created`: `string` (RFC3339Nano)

**Indexes**:
```go
mongo.IndexModel{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)}
mongo.IndexModel{
    Keys: bson.D{{Key: "studio_id", Value: 1}, {Key: "gallery_id", Value: 1}, {Key: "hash", Value: 1}},
    Options: options.Index().SetUnique(true),
}
mongo.IndexModel{Keys: bson.D{{Key: "gallery_id", Value: 1}, {Key: "ready", Value: 1}, {Key: "archived", Value: 1}, {Key: "created", Value: -1}}}
mongo.IndexModel{Keys: bson.D{{Key: "studio_id", Value: 1}, {Key: "ready", Value: 1}, {Key: "archived", Value: 1}}}
```

---

### 5.5 Collection: `orders`
Stores customer orders, payment status, entitlements, and purchased photo IDs.

**Schema Fields**:
- `id`: `string` (order / transaction reference, e.g. token)
- `studio_id`: `string`
- `gallery_id`: `string`
- `email`: `string`
- `amount`: `int64` (in kobo/cents)
- `platform_fee`: `int64` (calculated fee)
- `status`: `string` (`"Pending"`, `"Paid"`, `"Failed"`)
- `payment_provider`: `string` (`"paystack"`)
- `token_hash`: `string` (SHA-256 hash of buyer entitlement token)
- `downloads_used`: `int`
- `max_downloads`: `int`
- `items`: `[]orderItem` (`orderItem`: `{photo_id: string, unit_price: int64}`)
- `created`: `string` (RFC3339Nano)

**Indexes**:
```go
mongo.IndexModel{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)}
mongo.IndexModel{Keys: bson.D{{Key: "token_hash", Value: 1}}}
mongo.IndexModel{Keys: bson.D{{Key: "studio_id", Value: 1}, {Key: "status", Value: 1}, {Key: "payment_provider", Value: 1}}}
mongo.IndexModel{Keys: bson.D{{Key: "studio_id", Value: 1}, {Key: "created", Value: -1}}}
mongo.IndexModel{Keys: bson.D{{Key: "gallery_id", Value: 1}, {Key: "created", Value: -1}}}
mongo.IndexModel{Keys: bson.D{{Key: "items.photo_id", Value: 1}}}
```

---

### 5.6 Collection: `withdrawals`
Stores studio payout requests and transfer statuses via Paystack.

**Schema Fields**:
- `_id`: `string` (UUID)
- `studio_id`: `string`
- `reference`: `string` (unique Paystack transfer reference, e.g. `"lumen-<uuid>"`)
- `amount`: `int64` (amount in kobo)
- `bank`: `string` (Paystack bank code, e.g. `"058"`)
- `account_last4`: `string` (last 4 digits of bank account)
- `status`: `string` (`"pending"`, `"otp"`, `"success"`, `"failed"`, `"reversed"`, `"abandoned"`)
- `created`: `string` (RFC3339Nano)

**Indexes**:
```go
mongo.IndexModel{Keys: bson.D{{Key: "reference", Value: 1}}, Options: options.Index().SetUnique(true)}
mongo.IndexModel{Keys: bson.D{{Key: "studio_id", Value: 1}, {Key: "status", Value: 1}}}
mongo.IndexModel{Keys: bson.D{{Key: "studio_id", Value: 1}, {Key: "created", Value: -1}}}
mongo.IndexModel{Keys: bson.D{{Key: "status", Value: 1}}}
```

---

### 5.7 Collection: `guest_uploads`
Stores guest-contributed photographs awaiting photographer moderation.

**Schema Fields**:
- `id`: `string` (UUID)
- `studio_id`: `string`
- `gallery_id`: `string`
- `name`: `string` (original filename)
- `hash`: `string` (SHA-256 hex string)
- `token_hash`: `string` (SHA-256 hash of batch token given to guest)
- `status`: `string` (`"pending"`, `"approved"`, `"rejected"`)
- `created`: `string` (RFC3339Nano)

**Indexes**:
```go
mongo.IndexModel{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)}
mongo.IndexModel{Keys: bson.D{{Key: "token_hash", Value: 1}, {Key: "created", Value: -1}}}
mongo.IndexModel{Keys: bson.D{{Key: "gallery_id", Value: 1}, {Key: "status", Value: 1}, {Key: "created", Value: -1}}}
mongo.IndexModel{Keys: bson.D{{Key: "gallery_id", Value: 1}, {Key: "hash", Value: 1}, {Key: "status", Value: 1}}}
mongo.IndexModel{Keys: bson.D{{Key: "status", Value: 1}}}
```

---

### 5.8 Collection: `admin_audit`
Tracks admin console actions for security and compliance.

**Schema Fields**:
- `id`: `string` (UUID or token)
- `actor`: `string` (admin username)
- `action`: `string` (e.g. `"viewed account activity"`, `"refreshed order"`)
- `target`: `string` (target studio username or identifier)
- `created`: `string` (RFC3339Nano)

**Indexes**:
```go
mongo.IndexModel{Keys: bson.D{{Key: "created", Value: -1}}}
```

---

### 5.9 Collection: `settings`
Stores studio profile configuration.

**Schema Fields**:
- `_id`: `string` (studio ID matching `accounts._id`, e.g. `"legacy"` or UUID)
- `studio_name`: `string` (default `"Pictide Studio"`)
- `contact_email`: `string` (contact email)
- `default_price`: `float64` (default price, e.g. 5000)
- `studio_logo`: `bool` (`true` if a custom studio logo is stored in GridFS)

**Indexes**: Default `_id` index.

---

### 5.10 Collection: `direct_uploads`
Tracks temporary R2 direct upload presigned leases.

**Schema Fields**:
- `_id`: `string` (upload token)
- `studio_id`: `string`
- `gallery_id`: `string`
- `name`: `string`
- `size`: `int64`
- `object_key`: `string`
- `status`: `string` (`"pending"`, `"processing"`, `"complete"`)
- `lease`: `string` (lease lock token)
- `lease_until`: `time.Time`
- `expires_at`: `time.Time`

**Indexes**:
```go
mongo.IndexModel{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)}
```

---

### 5.11 GridFS Collections (`fs.files`, `fs.chunks`)
GridFS stores all binary image variants and uploads:
- Photos: `<photo_id>:preview`, `<photo_id>:thumb`, `<photo_id>:clean`, `<photo_id>:clean_thumb`, `<photo_id>:master`, `<photo_id>:original`
- Studio Logo: `logo:studio:<studio_id>`
- Gallery Logo: `logo:gallery:<gallery_id>`
- Guest Pending Uploads: `<upload_id>:pending`

---

## 6. Key MongoDB Queries and Aggregations Needed

### 6.1 Wallet Balance Calculation
Calculated atomically from orders and withdrawals without separate ledger corruption:
```go
// 1. Total Earned from Paid Orders:
pipeline := mongo.Pipeline{
    bson.D{{Key: "$match", Value: bson.M{
        "studio_id": studioID, 
        "status": "Paid", 
        "payment_provider": "paystack",
    }}},
    bson.D{{Key: "$group", Value: bson.M{
        "_id": nil,
        "total": bson.M{"$sum": bson.M{"$subtract": bson.A{"$amount", "$platform_fee"}}},
    }}},
}

// 2. Total Held Withdrawals:
pipelineHeld := mongo.Pipeline{
    bson.D{{Key: "$match", Value: bson.M{
        "studio_id": studioID,
        "status": bson.M{"$nin": bson.A{"failed", "reversed"}},
    }}},
    bson.D{{Key: "$group", Value: bson.M{
        "_id": nil,
        "held": bson.M{"$sum": "$amount"},
    }}},
}
// available = max(0, total - held)
```

### 6.2 Order Analytics, Trends, and Gallery Breakdown
To satisfy `GET /api/orders` with filters (`status`, `gallery`, `from`, `to`):
```go
filter := bson.M{"studio_id": studioID}
if status != "" { filter["status"] = status }
if gallery != "" { filter["gallery_id"] = gallery }
if from != "" || to != "" {
    dateFilter := bson.M{}
    if from != "" { dateFilter["$gte"] = from }
    if to != "" { dateFilter["$lt"] = to }
    filter["created"] = dateFilter
}

// 1. Filtered Orders (paginated)
cursor, err := s.db.Collection("orders").Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "created", Value: -1}}).SetLimit(limit).SetSkip(offset))

// 2. Daily Sales Trend (last 12 days for paid orders)
dailyTrendPipeline := mongo.Pipeline{
    bson.D{{Key: "$match", Value: bson.M{"studio_id": studioID, "status": "Paid"}}},
    bson.D{{Key: "$project", Value: bson.M{
        "day": bson.M{"$substrCP": bson.A{"$created", 0, 10}},
        "amount": "$amount",
    }}},
    bson.D{{Key: "$group", Value: bson.M{
        "_id": "$day",
        "amount": bson.M{"$sum": "$amount"},
    }}},
    bson.D{{Key: "$sort", Value: bson.D{{Key: "_id", Value: -1}}}},
    bson.D{{Key: "$limit", Value: 12}},
}

// 3. Top Galleries
topGalleriesPipeline := mongo.Pipeline{
    bson.D{{Key: "$match", Value: bson.M{"studio_id": studioID, "status": "Paid"}}},
    bson.D{{Key: "$group", Value: bson.M{
        "_id": "$gallery_id",
        "total": bson.M{"$sum": "$amount"},
        "count": bson.M{"$sum": 1},
    }}},
    bson.D{{Key: "$sort", Value: bson.D{{Key: "total", Value: -1}}}},
    bson.D{{Key: "$limit", Value: 5}},
}
```

### 6.3 Guest Upload Moderation Queries
```go
// Check duplicate before saving pending submission:
count, err := s.db.Collection("photos").CountDocuments(ctx, bson.M{"gallery_id": galleryID, "hash": hash})
// and in guest_uploads:
pendingCount, err := s.db.Collection("guest_uploads").CountDocuments(ctx, bson.M{"gallery_id": galleryID, "hash": hash, "status": "pending"})

// Review guest upload:
// Update status atomically
_, err = s.db.Collection("guest_uploads").UpdateOne(ctx, 
    bson.M{"id": uploadID, "status": "pending"},
    bson.M{"$set": bson.M{"status": action}}, // "approved" or "rejected"
)
```

### 6.4 Brand Logo & Regeneration
```go
// Studio logo:
s.db.GridFSBucket().UploadFromStreamWithID(ctx, "logo:studio:"+studioID, filename, reader)
s.db.Collection("settings").UpdateOne(ctx, bson.M{"_id": studioID}, bson.M{"$set": bson.M{"studio_logo": true}})

// Gallery logo:
s.db.GridFSBucket().UploadFromStreamWithID(ctx, "logo:gallery:"+galleryID, filename, reader)
s.db.Collection("galleries").UpdateOne(ctx, bson.M{"id": galleryID, "studio_id": studioID}, bson.M{"$set": bson.M{"watermark_logo": "set"}})

// Regeneration:
// Read photo:original from GridFS (or R2) -> run media.Process with studio/gallery logo -> overwrite preview, thumb, clean, clean_thumb, master in GridFS
```

---

## 7. Implementation Roadmap & Architecture Recommendations

1. **Routing Consolidation in `internal/httpapi/server.go`**:
   - Register the missing public routes before the `strings.HasPrefix(p, "/api/")` authentication guard:
     - `POST /api/register`
     - `POST /api/account/verify`
     - `POST /api/account/forgot`
     - `POST /api/account/reset`
     - `POST /api/guest-uploads/status`
     - `POST /api/galleries/:id/guest-uploads`
     - `GET /api/guest-uploads/:id/preview`
     - `GET /api/banks`
     - `GET /api/banks/resolve`
   - In `workspace.go:api`, handle the authenticated routes so none fall through to `fail(w, 503, "This feature is still being converted to MongoDB.")`:
     - `/api/withdrawals` (POST)
     - `/api/withdrawals/:reference/sync` (POST)
     - `/api/galleries/:id/guest-uploads` (GET)
     - `/api/guest-uploads/:id/review` (POST)
     - `/api/account/email` (POST)
     - `/api/settings/logo` (POST, DELETE)
     - `/api/galleries/:id/logo` (POST, DELETE)
     - `/api/settings/apply-brand` (POST)
     - `/api/galleries/:id/regenerate` (POST)
     - `/api/admin/sync` (POST)
2. **Index Initialization**:
   - Update `httpapi.New` in `internal/httpapi/server.go` to create all required indexes for `withdrawals`, `guest_uploads`, `account_links`, and `accounts.email`.
3. **Automated Test Harness**:
   - Mirror `TestMongoWorkspace` in `internal/httpapi/workspace_test.go` to test each newly implemented feature against a temporary MongoDB database (`pictide_test_<id>`).
   - Mock Paystack external calls where applicable (using `httptest.Server` or fake Paystack transport) so wallet, bank resolution, and transfer tests execute reliably in automated test suites without live credentials.
