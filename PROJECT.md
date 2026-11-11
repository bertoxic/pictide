# Project: Pictide Studio MongoDB Conversion & Production Completion

## Architecture
- **Language / Runtime**: Go 1.24+ backend, HTML5 / Vanilla JS frontend with Node.js test runner.
- **Data Layer**: MongoDB (`go.mongodb.org/mongo-driver/v2`) + GridFS (`go.mongodb.org/mongo-driver/v2/mongo/gridfs`) for binary asset storage, with optional Cloudflare R2 integration.
- **HTTP Routing**: Standard Go `net/http` multiplexing in `internal/httpapi/server.go`, with sub-routers for public and authenticated routes.
- **External Integrations**: Paystack API for payment processing, bank resolution, and transfers; SMTP for transactional emails.
- **Isolation Model**: Multi-tenant database architecture partitioned by `studio_id` across collections (`galleries`, `photos`, `orders`, `withdrawals`, `guest_uploads`, `settings`).

## Feature Inventory
| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | Public & Authenticated Routing Dispatcher | Route public vs authenticated `/api/` endpoints cleanly in `server.go`, removing catch-all 503 `"This feature is still being converted to MongoDB."` | M1 | survey |
| 2 | MongoDB Collections & Indexes Provisioning | Define collections, schema structures, and unique/TTL indexes for `withdrawals`, `guest_uploads`, `account_links`, `admin_audit`, and GridFS in `mongostore` and `server.go` | M1 | survey |
| 3 | Dynamic App Config & Health Endpoints | Replace hardcoded `email_configured: false`, `imgbb_previews: false`, and health status with dynamic runtime checks in `/api/config` and `/api/v1/health` | M1 | survey |
| 4 | Photographer Self-Registration | `POST /api/register` creates studio account in `accounts`, creates email verification token in `account_links`, sends email if SMTP is configured | M2 | survey |
| 5 | Email Verification | `POST /api/account/verify` verifies token from email and marks account verified in `accounts` | M2 | survey |
| 6 | Password Reset Request | `POST /api/account/forgot` generates password reset token in `account_links` and sends reset link | M2 | survey |
| 7 | Password Reset Submission | `POST /api/account/reset` validates token and sets new password hash in `accounts` | M2 | survey |
| 8 | Contact Email Update | `POST /api/account/email` updates studio contact email address | M2 | survey |
| 9 | Bank List Retrieval | `GET /api/banks` fetches list of supported commercial banks from Paystack | M3 | survey |
| 10 | Bank Account Resolution | `GET /api/banks/resolve` validates account number and bank code via Paystack | M3 | survey |
| 11 | Withdrawal Request Creation | `POST /api/withdrawals` creates transfer recipient, initiates payout, and records withdrawal record in `withdrawals` | M3 | survey |
| 12 | Withdrawal Status Sync | `POST /api/withdrawals/{reference}/sync` syncs payout status from Paystack | M3 | survey |
| 13 | Payout Webhook Processing | Handle `transfer.*` webhooks in `commerce.go:236` (replacing 503) to update withdrawal status in MongoDB | M3 | survey |
| 14 | Wallet Balance & Config Reporting | Remove hardcoded `configured: false` in `wallet.go:68` and report real studio wallet status | M3 | survey |
| 15 | Enable Guest Uploads Toggle | Remove 503 in `controls.go:151` and persist `allow_guest_upload: 1` in gallery settings | M4 | survey |
| 16 | Guest Upload Submission | `POST /api/galleries/{id}/guest-uploads` public endpoint to submit photos to GridFS and `guest_uploads` collection | M4 | survey |
| 17 | Guest Uploads Listing | `GET /api/galleries/{id}/guest-uploads` studio endpoint to list submissions | M4 | survey |
| 18 | Guest Upload Preview | `GET /api/guest-uploads/{id}/preview` view thumbnail preview of submission from GridFS | M4 | survey |
| 19 | Guest Upload Review | `POST /api/guest-uploads/{id}/review` approve or reject submission, moving approved photos into gallery `photos` | M4 | survey |
| 20 | Guest Uploads Status Management | `POST /api/guest-uploads/status` manage gallery guest upload settings | M4 | survey |
| 21 | Studio Brand Mark Upload & Delete | `POST /api/settings/logo` and `DELETE /api/settings/logo` for studio watermark mark in GridFS | M5 | survey |
| 22 | Gallery Watermark Logo Upload & Delete | `POST /api/galleries/{id}/logo` and `DELETE /api/galleries/{id}/logo` for gallery-specific watermark | M5 | survey |
| 23 | Apply Brand Mark Across Photos | `POST /api/settings/apply-brand` to apply studio watermark to all photos | M5 | survey |
| 24 | Regenerate Gallery Previews | `POST /api/galleries/{id}/regenerate` to re-watermark and recreate previews in gallery | M5 | survey |
| 25 | Studio Logo Status in Workspace | Fix `workspace.go:269` to report real `studio_logo` boolean from GridFS/settings | M5 | survey |
| 26 | Admin Synchronization | `POST /api/admin/sync` to verify and synchronize database and storage records | M6 | survey |
| 27 | Admin Overview Details | Populate `email` and `verified` fields in `adminOverview` (`workspace.go:184`) | M6 | survey |
| 28 | Orders Query Filtering | Implement `from`, `to`, `gallery`, and `status` query parameter filtering in `listOrders` | M6 | survey |
| 29 | Orders Insights & Trends Analytics | Compute real daily insights trends and gallery breakdown in `listOrders` (`workspace.go:597`) | M6 | survey |
| 30 | Full E2E Test Suite & Adversarial Hardening | Pass 100% of Tiers 1-4 tests, perform Tier 5 white-box adversarial stress testing, and verify zero 503 errors remain | M7 | survey |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| M1 | Core DB Schemas, Indexes & API Gateway Routing | Setup collections, models, indexes in `mongostore`/`server.go`; setup public vs auth route dispatcher in `server.go`; dynamic `/api/config` | none | PLANNED |
| M2 | Auth Lifecycle, Registration & Password Recovery | `POST /api/register`, `/api/account/verify`, `/api/account/forgot`, `/api/account/reset`, `/api/account/email`, tokens in `account_links`, SMTP email | M1 | PLANNED |
| M3 | Wallet, Bank Payouts & Transfer Webhooks | `GET /api/banks`, `/api/banks/resolve`, `POST /api/withdrawals`, sync, `transfer.*` webhooks, remove 503 in `commerce.go`, real wallet status | M1 | PLANNED |
| M4 | Guest Uploads & Submission Review | Remove 503 in `controls.go`, public submission `POST /api/galleries/{id}/guest-uploads`, preview, review approve/reject, promote to photos | M1 | PLANNED |
| M5 | Studio & Gallery Branding & Watermark Regeneration | `/api/settings/logo`, `/api/galleries/{id}/logo`, `/api/settings/apply-brand`, `/api/galleries/{id}/regenerate`, fix `studio_logo` flag | M1 | PLANNED |
| M6 | Admin Sync, Overview & Order Analytics Filtering | `POST /api/admin/sync`, admin overview email/verified fields, `listOrders` query filters (`from`/`to`/`gallery`/`status`) and insights | M1 | PLANNED |
| M7 | Final Acceptance: 100% E2E Pass & Adversarial Hardening | Execute full test suite from E2E Testing Track (Tiers 1-4); Tier 5 adversarial coverage hardening; confirm zero 503 stubs | M2, M3, M4, M5, M6 | PLANNED |

## Interface Contracts
### HTTP API Gateway ↔ Feature Handlers
- Public endpoints (`/api/register`, `/api/account/verify`, `/api/account/forgot`, `/api/account/reset`, `/api/galleries/{id}/guest-uploads`) MUST NOT require authentication session.
- Authenticated endpoints MUST validate session cookie and extract `*security.Account`.
- All JSON errors MUST return `{"error": "<human readable message>"}` with appropriate HTTP status (400, 401, 403, 404, 500).
- No endpoint may return HTTP 503 `"This feature is still being converted to MongoDB."`.

### Mongo Data Layer Contracts
- `accounts`: `{id: string, username: string, email: string, verified: bool, created_at: int64, password_hash: string, password_salt: string, role: string}`
- `account_links`: `{token: string, account_id: string, purpose: string, expires_at: date}` (TTL index on expires_at)
- `withdrawals`: `{id: string, studio_id: string, amount: int64, fee: int64, currency: string, recipient_code: string, reference: string, status: string, bank_code: string, account_number: string, account_name: string, created_at: int64, updated_at: int64}`
- `guest_uploads`: `{id: string, studio_id: string, gallery_id: string, guest_name: string, filename: string, size: int64, status: string, file_id: ObjectID, created_at: int64}`
- `admin_audit`: `{id: string, action: string, details: string, created_at: int64}`

## Code Layout
- `cmd/pictide/main.go`: Application entrypoint.
- `internal/app/app.go`: Application lifecycle, server startup, and graceful shutdown.
- `internal/config/config.go`: Environment variable parsing and validation.
- `internal/httpapi/server.go`: Primary HTTP router, route dispatching, error handlers, static asset serving.
- `internal/httpapi/workspace.go`: Authenticated studio workspace API router and handlers.
- `internal/httpapi/wallet.go`: Bank and payout endpoints.
- `internal/httpapi/commerce.go`: Payment callbacks and Paystack webhooks.
- `internal/httpapi/controls.go`: Gallery settings and control toggles.
- `internal/mongostore/`: MongoDB client connection, repository methods, and collection helpers.
- `test/`: Frontend tests (`node --test test/*.test.js`).
- `internal/httpapi/*_test.go`: Go unit and MongoDB integration tests.
