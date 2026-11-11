# Survey Report: Test Harness, Configuration, Dependencies & MongoDB Test Infrastructure

**Agent:** explorer_survey_3  
**Date:** 2026-11-10  
**Repository:** Pictide Studio (`lumen v2`)  
**Mission:** Survey the test harness, test runner, build/run scripts, environment configuration, existing test suites, database test fixtures, and identify required test infrastructure to verify all unimplemented/fixed features against MongoDB.

---

## 1. Executive Summary

Pictide Studio is a Go 1.23.3 web application (`pictide.local/studio`) with an embedded HTML5/Vanilla JavaScript frontend (`public/assets.go` via `embed.FS`). 

Key architectural findings regarding testing:
1. **Dual Test Suites**:
   - **Go Backend Tests** (`go test -p 1 ./...`): Cover unit logic for configuration, security, QR generation, media processing, Paystack webhook HMAC, ImgBB proxying, and card layout validation. Includes an opt-in integration suite (`TestMongoWorkspace` in `internal/httpapi/workspace_test.go`) and an opt-in R2 cloud storage suite (`TestR2DirectUpload` in `internal/httpapi/r2_integration_test.go`).
   - **Node.js Frontend Tests** (`node --test test/*.test.js`): 6 test files containing 13 tests evaluating frontend DOM/UI behaviors (modals, batch pagination, navigation state, date/filter logic, R2 uploads).
2. **Test Runner Gap**:
   - `package.json` contains `"test": "go test -p 1 ./..."`. It completely omits the Node frontend test runner (`node --test test/*.test.js`).
   - Running `go test -p 1 ./...` without environment variables skips `TestMongoWorkspace` and `TestR2DirectUpload`.
3. **MongoDB Test Fixtures & Infrastructure**:
   - Neither `mongodb-memory-server` nor local `mongod` nor `docker` is available or installed on the host environment.
   - However, a real MongoDB Atlas cluster is configured in `.env` (`mongodb+srv://...`), reachable, and verified.
   - The project uses a temporary database fixture pattern (`pictide_test_<randomToken>`) that initializes collections and indexes, runs integration tests, and cleanly drops the database via `defer db.Drop(ctx)`.
   - Running `PICTIDE_TEST_MONGODB=1 go test -v -run '^TestMongoWorkspace$' ./internal/httpapi` passed completely in 13.13 seconds.
4. **Missing Test Coverage for Incomplete Features**:
   - All routes currently returning `503` or `"This feature is still being converted to MongoDB."` (Payouts/Wallet, Guest Uploads, Branding/Logos, Self-Registration/Email Verification/Password Reset, Admin Sync) have **zero** automated tests.
   - The data layer package `internal/mongostore` currently has **no test files**.

---

## 2. Test Runner & Harness Architecture

### 2.1 Backend Test Runner (Go)
- **Engine**: Go standard testing tool (`testing` package).
- **Current package.json script**: `"test": "go test -p 1 ./..."`
- **Execution characteristics**:
  - Flags: `-p 1` ensures sequential package testing to prevent resource contention or port collisions.
  - Test discovery: Any file matching `*_test.go` across all packages.
  - Standard run output:
    ```
    ?   pictide.local/studio                    [no test files]
    ?   pictide.local/studio/cmd/pictide        [no test files]
    ?   pictide.local/studio/internal/app       [no test files]
    ok  pictide.local/studio/internal/config    (cached)
    ok  pictide.local/studio/internal/httpapi   0.122s
    ok  pictide.local/studio/internal/imgbb     (cached)
    ok  pictide.local/studio/internal/media     (cached)
    ?   pictide.local/studio/internal/mongostore [no test files]
    ok  pictide.local/studio/internal/paystack  (cached)
    ok  pictide.local/studio/internal/qr        (cached)
    ok  pictide.local/studio/internal/security  (cached)
    ?   pictide.local/studio/public             [no test files]
    ```
  - Total duration (unit suite): ~0.2s without integration tests; ~13.5s with `PICTIDE_TEST_MONGODB=1`.

### 2.2 Frontend Test Runner (Node.js)
- **Engine**: Node.js built-in test runner (`node:test`, `node:assert/strict`, `node:vm`).
- **Runner command**: `node --test test/*.test.js`
- **Current status**: 13 tests across 6 files, 0 failures, duration ~300ms.
- **Execution gap**: Not executed by `npm test` or any script in `package.json`.

---

## 3. Inventory of Existing Tests & Suites

| Package | Test File | Test Functions | Scope / Type | External Dependencies / Fixtures | Default Execution Status |
|---|---|---|---|---|---|
| `internal/config` | `config_test.go` | `TestLoadEnvAndRejectUnsafeMode`<br>`TestImgBBPreviewConfiguration`<br>`TestMongoDBConfiguration` | Unit: Environment parsing, validation, and defaults | Temp directory `.env` files via `t.TempDir()`, `t.Setenv()` | Executed & PASS |
| `internal/httpapi` | `card_test.go` | `TestCardValidation`<br>`TestCardShapeAndLogoValidation` | Unit: Card JSON validation, SVG injection protection, size limits | In-memory synthetic images & JSON payloads | Executed & PASS |
| `internal/httpapi` | `workspace_test.go` | `TestMongoWorkspace` | E2E Integration: Admin login, accounts, activity, insights, settings, gallery CRUD, photo upload, media preview, customization, publishing, QR, print cards | Requires `PICTIDE_TEST_MONGODB=1`. Connects to MongoDB Atlas, creates `pictide_test_<token>` DB, uses `httptest.NewRecorder()` | Skipped by default; **PASS (13.1s)** when flag set |
| `internal/httpapi` | `r2_integration_test.go` | `TestR2DirectUpload`<br>`TestR2CORSStatus` | Integration: Large image (>4.5MB) presigned direct R2 upload, completion, zip download, CORS check | Requires `PICTIDE_TEST_R2=1` + Cloudflare R2 bucket credentials | Skipped by default |
| `internal/imgbb` | `imgbb_test.go` | `TestUploadAndDownload`<br>`TestRejectsUntrustedUploadURL`<br>`TestNonExpiringPNG` | Unit / Mock Integration: ImgBB image upload, download, SSRF safety | Local mock server via `httptest.NewServer` | Executed & PASS |
| `internal/media` | `media_test.go` | `TestProcessOutputsAndRejectsInvalid` | Unit: Image decoding, resizing, watermarking, generation of 5 image variants | In-memory synthetic PNGs | Executed & PASS |
| `internal/paystack` | `paystack_test.go` | `TestInitializeAndSignature` | Unit / Mock Integration: Checkout initialization, HMAC SHA-512 signature verification | Local mock server via `httptest.NewServer` | Executed & PASS |
| `internal/qr` | `qr_test.go` | `TestSVG` | Unit: SVG QR code generation, color XSS injection prevention | In-memory strings | Executed & PASS |
| `internal/security` | `security_test.go` | `TestPasswordAndSignedDownloads` | Unit: Bcrypt password hashing, session tokens, HMAC download link signatures | In-memory time and string tokens | Executed & PASS |
| `test/` (Node.js) | `dialogs.test.js` | Confirmation dialog focus trap, cancel/escape behavior | Frontend UI Unit | Mock DOM via `node:vm` | Executed via `node --test` & PASS |
| `test/` (Node.js) | `gallery-batches.test.js` | Guest photo batch selection, owner photo batch pagination, filter drawer open/close | Frontend UI Unit | Mock DOM via `node:vm` | Executed via `node --test` & PASS |
| `test/` (Node.js) | `navigation.test.js` | Wallet navigation loading state and concurrent API fetch | Frontend UI Unit | Mock DOM and mock `fetch()` via `node:vm` | Executed via `node --test` & PASS |
| `test/` (Node.js) | `onboarding.test.js` | Returning visitor landing view vs auth view | Frontend UI Unit | Mock DOM via `node:vm` | Executed via `node --test` & PASS |
| `test/` (Node.js) | `order-insights.test.js` | Order date boundaries, local time filtering, sales summation | Frontend Logic Unit | In-memory test objects via `node:vm` | Executed via `node --test` & PASS |
| `test/` (Node.js) | `r2-uploads.test.js` | Direct R2 upload, fallback to server upload, CORS error reporting | Frontend Upload Logic Unit | Mock `fetch()` via `node:vm` | Executed via `node --test` & PASS |

### 3.1 Untested Packages & Gaps
- `internal/mongostore`: Contains `connect.go` and `galleries.go`. Contains core database logic (CRUD, query filters, case-insensitive regex search, pagination, unique compound indexes). **Has zero unit or integration tests**.
- `internal/app`: Server startup, graceful shutdown, working directory resolution. **No tests**.
- `cmd/pictide`: Main entry point wrapper. **No tests**.
- `public`: Asset embedding. **No tests**.

---

## 4. Database Test Infrastructure & Fixtures

### 4.1 In-Memory MongoDB & Local Database Checks
- **`mongodb-memory-server`**: Not installed or present in `package.json`. Node is not the application runtime (only used for frontend scripts and testing static JS).
- **Local `mongod`**: Not found on PATH (`ObjectNotFound: mongod`).
- **`docker`**: Not found on PATH (`ObjectNotFound: docker`).

### 4.2 Live MongoDB Test Infrastructure
The repository relies on a reachable MongoDB instance supplied via `MONGODB_URI`.
In `.env`:
`MONGODB_URI="mongodb+srv://bert:bert@bertoxic.dyqdd75.mongodb.net/Lumen?retryWrites=true&w=majority"`

#### Test Fixture Pattern (`workspace_test.go`):
1. **Connection**:
   ```go
   client, err := mongostore.Connect(ctx, os.Getenv("MONGODB_URI"))
   ```
2. **Database Isolation**:
   ```go
   id, err := security.Token()
   db := client.Database("pictide_test_" + id[:16])
   defer db.Drop(context.Background())
   ```
   Every test invocation generates a globally unique, randomly named database (`pictide_test_<token>`), ensuring 100% test isolation with zero risk of mutating development or production data.
3. **Automated Schema & Index Bootstrapping**:
   ```go
   server, err := New(ctx, db, config.Config{SigningKey: strings.Repeat("k", 32), AdminPassword: "demo-password-123"})
   ```
   Calling `httpapi.New(ctx, db, cfg)` automatically creates all indexes on `accounts`, `galleries`, `photos`, `orders`, and upserts the default admin account.
4. **Clean Teardown**:
   ```go
   defer db.Drop(context.Background())
   defer client.Disconnect(context.Background())
   ```
   The database is dropped immediately upon test completion.
5. **Port-Free HTTP Execution**:
   Uses `httptest.NewRecorder()` and calls `server.ServeHTTP(w, req)` directly, avoiding TCP port binding issues or network firewall interference.

---

## 5. Build/Run Scripts, Configuration & Environment Variables

### 5.1 Build and Run Scripts
- `start.ps1`:
  ```powershell
  $ErrorActionPreference = 'Stop'
  Set-Location -LiteralPath $PSScriptRoot
  & go run -p 1 ./cmd/pictide
  if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
  ```
- `package.json`:
  ```json
  {
    "name": "pictide-studio",
    "version": "2.0.0",
    "private": true,
    "description": "Go-powered photo proofing and delivery studio",
    "scripts": { "start": "go run -p 1 ./cmd/pictide", "test": "go test -p 1 ./..." }
  }
  ```

### 5.2 Environment Variables Matrix (`internal/config/config.go`)

| Variable | Description | Default / Example in `.env` | Required / Constraints |
|---|---|---|---|
| `LUMEN_ENV_FILE` | Path to custom `.env` file | `.env` | Optional |
| `PORT` | Listening port | `8080` (defaults to `3000` in code) | Optional |
| `LUMEN_HOST` | Host address to bind | `127.0.0.1` | Optional (`0.0.0.0` on Vercel) |
| `MONGODB_URI` | MongoDB connection URI | `mongodb+srv://...` | **Required** |
| `MONGODB_DATABASE` | MongoDB database name | `pictide` | Optional |
| `LUMEN_SIGNING_KEY` | HMAC signing key for sessions/downloads | 64-hex char secret | **Required** (>= 32 characters) |
| `LUMEN_ADMIN_PASSWORD` | Initial admin account password | Secret string | **Required** (>= 12 characters) |
| `PUBLIC_URL` | Public origin for CORS/links | Empty locally, HTTPS origin in production | Must be HTTPS if non-empty |
| `LUMEN_PAYMENT_MODE` | Payment provider | `paystack` | Must be `paystack` |
| `PAYSTACK_SECRET_KEY` | Paystack API Secret | `sk_test_...` | **Required** when payment mode is `paystack` |
| `PAYSTACK_PUBLIC_KEY` | Paystack API Public Key | `pk_test_...` | Optional |
| `PAYSTACK_WEBHOOK_SECRET` | Paystack Webhook Secret | Empty or secret | Optional |
| `LUMEN_PLATFORM_FEE_BPS` | Platform fee basis points | `0` (0-10000) | Optional |
| `LUMEN_DATA_DIR` | Local data directory | `./data/storage` | Optional |
| `LUMEN_MEDIA_DIR` | Local media storage directory | `./data/storage/media` | Optional |
| `LUMEN_SMTP_HOST` | SMTP server for emails | Empty in `.env` | Required for registration & recovery |
| `LUMEN_SMTP_PORT` | SMTP port | `587` | Optional |
| `LUMEN_SMTP_USER` | SMTP username | Empty in `.env` | Optional |
| `LUMEN_SMTP_PASSWORD` | SMTP password | Empty in `.env` | Optional |
| `LUMEN_SMTP_FROM` | SMTP sender address | Empty in `.env` | Optional |
| `IMGBB_API_KEY` | API key for ImgBB preview hosting | Configured in `.env` | Required if `LUMEN_IMGBB_PREVIEWS=true` |
| `LUMEN_IMGBB_PREVIEWS` | Enable ImgBB proxying | `true` in `.env` | Boolean |
| `R2_ACCOUNT_ID` | Cloudflare account ID | Configured in `.env` | Required if using R2 |
| `R2_ACCESS_KEY_ID` | Cloudflare R2 access key | Configured in `.env` | Required if using R2 |
| `R2_SECRET_ACCESS_KEY` | Cloudflare R2 secret key | Configured in `.env` | Required if using R2 |
| `R2_BUCKET_NAME` | Cloudflare R2 bucket name | `pictide` | Required if using R2 |
| `PICTIDE_TEST_MONGODB` | Test switch to enable MongoDB workspace test | Unset by default; set to `1` to run | Test only |
| `PICTIDE_TEST_R2` | Test switch to enable R2 integration test | Unset by default; set to `1` to run | Test only |

---

## 6. Required Test Infrastructure for Unimplemented / Fixed Features

To satisfy **Requirement R3** and **Acceptance Criteria 27 & 28** ("Automated tests cover each newly implemented or fixed feature; all tests pass successfully and verify operations against MongoDB"), the following automated tests and fixtures must be established:

### 6.1 Test Suites to Create

#### 1. Payouts & Wallet Suite (`internal/httpapi/wallet_test.go`)
- **Endpoints to test**:
  - `GET /api/banks`: Verify returning supported bank list (Paystack bank list or fallback).
  - `GET /api/banks/resolve?bank=...&account=...`: Verify account number resolution and error handling.
  - `POST /api/withdrawals`: Test creating withdrawal request, verifying:
    - Sufficient available balance (earned sales minus prior non-failed withdrawals).
    - Deduction and creation of withdrawal record in MongoDB `withdrawals` collection.
    - Status transitions (`pending`, `success`, `failed`).
  - `POST /api/withdrawals/:reference/sync`: Test syncing transfer status with Paystack and updating MongoDB record.
- **Fixtures required**:
  - Seeded orders in MongoDB with `status: "Paid"` and `payment_provider: "paystack"` to create available balance.
  - Mock Paystack HTTP server (`httptest.NewServer`) responding to `/bank`, `/bank/resolve`, `/transferrecipient`, `/transfer`.

#### 2. Guest Submissions Suite (`internal/httpapi/guest_uploads_test.go`)
- **Endpoints to test**:
  - `POST /api/galleries/:id/guest-uploads`: Submit guest photos (multipart form), verifying creation of records in MongoDB `guest_uploads` collection with status `pending`, token generation, and rate limiting / file count limits (max 5).
  - `GET /api/galleries/:id/guest-uploads`: Authenticated photographer listing of pending guest submissions.
  - `GET /api/guest-uploads/:id/preview`: Preview/thumbnail retrieval of pending guest upload.
  - `POST /api/guest-uploads/:id/review`:
    - `action: "approve"`: Photo moved/promoted into gallery `photos` collection as a free photo (`price: 0`), status updated to `approved`.
    - `action: "reject"`: Status updated to `rejected`, photo excluded from gallery.
  - `POST /api/guest-uploads/status`: Guest checking status by tokens stored in localStorage.
- **Fixtures required**:
  - Published gallery with `allow_guest_upload: 1`.
  - Synthetic images for upload.

#### 3. Studio & Gallery Branding Suite (`internal/httpapi/branding_test.go`)
- **Endpoints to test**:
  - `POST /api/galleries/:id/logo`: Upload watermark PNG/JPEG for a gallery, verifying storage in GridFS or media store and database update.
  - `DELETE /api/galleries/:id/logo`: Removal of gallery watermark logo.
  - `POST /api/galleries/:id/regenerate`: Re-processes and regenerates watermarked previews and clean thumbnails for all photos in the gallery.
  - `POST /api/settings/logo`: Upload studio brand mark logo.
  - `DELETE /api/settings/logo`: Remove studio brand mark logo.
  - `POST /api/settings/apply-brand`: Batch update all photos across all studio galleries with the studio brand mark.
- **Fixtures required**:
  - Gallery with existing uploaded photos.
  - Sample brand logo PNG with alpha transparency.

#### 4. Account Lifecycle & Email Verification Suite (`internal/httpapi/auth_lifecycle_test.go`)
- **Endpoints to test**:
  - `POST /api/register`: Self-registration creating unverified account in MongoDB with verification token and expiry.
  - `POST /api/account/verify`: Validating token and activating account.
  - `POST /api/account/forgot`: Password reset request generating token and sending email.
  - `POST /api/account/reset`: Setting new password with valid token, rejecting expired/used token.
  - `POST /api/account/email`: Photographer updating account email address.
- **Fixtures required**:
  - Mock SMTP server / mock mailer to capture verification and password reset emails without external network dependencies.
  - MongoDB accounts collection entries with tokens.

#### 5. Admin Sync Suite (`internal/httpapi/admin_sync_test.go`)
- **Endpoints to test**:
  - `POST /api/admin/sync`: Administrator refreshing status of an order or withdrawal reference across accounts.
- **Fixtures required**:
  - Admin session cookie.
  - Pending orders and withdrawals in MongoDB.

#### 6. Mongostore Package Test Suite (`internal/mongostore/galleries_test.go`)
- **Unit & integration test for data layer**:
  - `Galleries.Create`, `Get`, `List`, `Update`, `Archive`.
  - Pagination, limit boundaries (1-100), offset boundaries.
  - Case-insensitive regex search on `name`, `client`, `location`.
  - Compound indexing `{studio_id: 1, id: 1}` isolation.

---

## 7. Actionable Recommendations for Implementation & Verification

1. **Update `package.json` Test Script**:
   Configure `package.json` to run both test suites:
   ```json
   "scripts": {
     "start": "go run -p 1 ./cmd/pictide",
     "test:go": "go test -p 1 ./...",
     "test:node": "node --test test/*.test.js",
     "test": "go test -p 1 ./... && node --test test/*.test.js"
   }
   ```
2. **MongoDB Test Flag Optimization**:
   Ensure all newly created MongoDB integration tests respect `PICTIDE_TEST_MONGODB=1` (or run them whenever `MONGODB_URI` is present in the environment), using the proven random database fixture pattern (`pictide_test_<randomToken>`) to ensure isolation and zero cleanup leakage.
3. **Mock Third-Party External Services in Tests**:
   - For Paystack checkout, transfers, and bank resolution: use `httptest.NewServer` or mock handlers (as demonstrated in `internal/paystack/paystack_test.go`).
   - For SMTP email sending: inject an email sender interface that records sent messages in test mode.
4. **End-to-End Verification Pipeline**:
   The implementer can verify all requirements by running:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"
   go test -v -p 1 ./...
   node --test test/*.test.js
   ```
   Both passing with 0 failures will satisfy all R3 and AC28 criteria.
