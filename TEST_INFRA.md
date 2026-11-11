# Pictide Studio Test Infrastructure & Quality Architecture

## 1. Overview & Strategy

This document specifies the end-to-end (E2E) test infrastructure for Pictide Studio's production architecture. Pictide Studio uses Go 1.24+ for the backend services with MongoDB Atlas / local MongoDB and GridFS as the primary persistent data store, and Node.js for client-side frontend tests.

The testing architecture provides deterministic, isolated, zero-leakage automated verification across all 30 features documented in `PROJECT.md § Feature Inventory`.

---

## 2. Test Architecture & Multi-Tenant Isolation

### 2.1 Isolated Ephemeral Test Databases
Every Go integration and E2E test suite allocates an isolated temporary database namespace:
```go
dbName := fmt.Sprintf("pictide_test_%s_%s", tierPrefix, randomToken[:16])
db := client.Database(dbName)
defer db.Drop(context.Background())
```
**Safety Guarantees**:
- Tests NEVER touch or modify the configured production database (`c.MongoDBDatabase` / `pictide`).
- Each tier or scenario runs inside its own distinct database sandbox.
- Drop hooks ensure all collections, indexes, and GridFS buckets (`fs.files`, `fs.chunks`) are dropped on completion, maintaining zero side effects across runs.

### 2.2 In-Memory Mocking & HTTP Test Harness
The Go test harness uses Go's standard `net/http/httptest` package:
- `httptest.NewRecorder` captures HTTP response codes, headers, and JSON bodies.
- `httptest.NewRequest` constructs requests with full control over headers (`Content-Type`, `Origin`, `X-Paystack-Signature`), cookies (`lumen_platform`), query parameters, and multipart form payloads.
- Fast execution: Requests are dispatched in-process without TCP network port bindings, maximizing speed and reliability while testing the actual `httpapi.Server` routing and handler logic.

### 2.3 Paystack & SMTP Test Contracts
- **Paystack Webhook Verification**: Uses SHA512 HMAC signatures calculated against `cfg.PaystackSecret` to test `charge.success` and `transfer.*` events.
- **Paystack External API**: Tested through contract validation and mockable HTTP clients or deterministic test keys (`sk_test_...`).
- **Email Delivery (SMTP)**: Configured with test/dummy credentials to verify token generation and link dispatching without requiring real external mail servers.

---

## 3. Four-Tier Test Suite Structure

The test suites are organized into 4 structured tiers corresponding to software quality maturity:

### Tier 1 — Feature Coverage (Features 1 to 30)
Isolated, representative tests for each individual feature:
1. `Feature 01`: Public vs Authenticated Routing Dispatcher (unauthenticated access to public endpoints, 401 gate on protected endpoints, removal of 503 catch-all)
2. `Feature 02`: MongoDB Collections & Indexes Provisioning (validation of indexes on `accounts`, `galleries`, `photos`, `orders`, `withdrawals`, `guest_uploads`, `account_links`, `admin_audit`, GridFS)
3. `Feature 03`: Dynamic App Config & Health Endpoints (`/api/config` and `/api/v1/health`)
4. `Feature 04`: Photographer Self-Registration (`POST /api/register`)
5. `Feature 05`: Email Verification (`POST /api/account/verify`)
6. `Feature 06`: Password Reset Request (`POST /api/account/forgot`)
7. `Feature 07`: Password Reset Submission (`POST /api/account/reset`)
8. `Feature 08`: Contact Email Update (`POST /api/account/email`)
9. `Feature 09`: Bank List Retrieval (`GET /api/banks`)
10. `Feature 10`: Bank Account Resolution (`GET /api/banks/resolve`)
11. `Feature 11`: Withdrawal Request Creation (`POST /api/withdrawals`)
12. `Feature 12`: Withdrawal Status Sync (`POST /api/withdrawals/{ref}/sync`)
13. `Feature 13`: Payout Webhook Processing (`transfer.*` webhooks in `commerce.go`)
14. `Feature 14`: Wallet Balance & Config Reporting (`GET /api/wallet`)
15. `Feature 15`: Enable Guest Uploads Toggle (`allow_guest_upload: 1` in `controls.go`)
16. `Feature 16`: Guest Upload Submission (`POST /api/galleries/{id}/guest-uploads`)
17. `Feature 17`: Guest Uploads Listing (`GET /api/galleries/{id}/guest-uploads`)
18. `Feature 18`: Guest Upload Preview (`GET /api/guest-uploads/{id}/preview`)
19. `Feature 19`: Guest Upload Review (`POST /api/guest-uploads/{id}/review` approve/reject)
20. `Feature 20`: Guest Uploads Status Management (`POST /api/guest-uploads/status`)
21. `Feature 21`: Studio Brand Mark Upload & Delete (`/api/settings/logo`)
22. `Feature 22`: Gallery Watermark Logo Upload & Delete (`/api/galleries/{id}/logo`)
23. `Feature 23`: Apply Brand Mark Across Photos (`POST /api/settings/apply-brand`)
24. `Feature 24`: Regenerate Gallery Previews (`POST /api/galleries/{id}/regenerate`)
25. `Feature 25`: Studio Logo Status in Workspace (`GET /api/settings` reports `studio_logo: bool`)
26. `Feature 26`: Admin Synchronization (`POST /api/admin/sync`)
27. `Feature 27`: Admin Overview Details (`GET /api/admin/overview` includes `email` and `verified`)
28. `Feature 28`: Orders Query Filtering (`GET /api/orders` with `from`, `to`, `gallery`, `status`)
29. `Feature 29`: Orders Insights & Trends Analytics (`insights.days` and `insights.galleries`)
30. `Feature 30`: Full E2E & Adversarial Hardening (Zero 503 errors remain, robust error handling)

### Tier 2 — Boundary, Corner Cases & Adversarial Verification
Edge cases, invalid inputs, and security gates:
- **Registration & Auth**: Short passwords (<12 chars), invalid email syntax, username regex validation, duplicate username/email collisions (HTTP 409).
- **Token Security**: Expired tokens, already consumed tokens (anti-replay), malformed tokens.
- **Access Control**: Missing session cookie, expired session timestamp, forged HMAC session signature, suspended/disabled accounts.
- **Financial & Wallet**: Negative/zero withdrawal amounts, withdrawal exceeding available balance, invalid 10-digit NUBAN account format.
- **Guest Uploads**: Uploading to non-existent or unpublished gallery, uploading when guest uploads are disabled, oversized files (>10MB), non-image mime types, batch limit (>5 files).
- **Branding**: Logos exceeding 2MB, corrupted image streams, invalid watermark styles or color hex codes.
- **Analytics Filters**: Non-existent gallery IDs, inverted date ranges (`from` > `to`), non-numeric limits or offsets.
- **Role Permissions**: Photographers attempting to access administrator-only endpoints (`/api/admin/*`).

### Tier 3 — Cross-Feature Combinations & Workflows
Pairwise and cross-module interactions:
- **Auth to Gallery Lifecycle**: Registration -> email verification -> session login -> studio profile settings -> gallery creation -> customization.
- **Guest Upload to Photo Promotion**: Gallery creation -> toggle guest uploads -> public guest upload submission -> photographer review & approval -> photo promoted to gallery at price 0 -> guest status reflects approval.
- **Order Checkout to Payout Lifecycle**: Photo upload -> order checkout -> Paystack webhook `charge.success` -> wallet balance updates -> withdrawal request created -> webhook `transfer.success` -> available balance reconciliation.
- **Branding Upload to Asset Regeneration**: Studio mark upload -> gallery logo override -> photo upload -> asset regeneration -> watermark removal -> clean preview serving.
- **Multi-Tenant Isolation**: Two separate photographer studios; Studio A cannot read, edit, delete, or upload to Studio B's galleries, photos, orders, or withdrawals.
- **Admin Governance & Suspension**: Admin reviews overview -> syncs pending order/withdrawal -> suspends photographer -> photographer's active session is immediately revoked.

### Tier 4 — Real-World Application Scenarios
Multi-step end-to-end simulations of actual production workloads:
- Full studio lifecycle simulation encompassing account creation, branding configuration, gallery setup, guest submissions, customer purchases, order reconciliation, and fund withdrawals.

---

## 4. Test Execution Commands

### 4.1 Running Backend Go Tests (with Isolated MongoDB)
In PowerShell:
```powershell
# Set environment flag to execute MongoDB-backed E2E tests
$env:PICTIDE_TEST_MONGODB="1"

# Run all E2E test suites
go test -v -run "TestE2E_" ./internal/httpapi

# Run specific tiers
go test -v -run "TestE2E_Tier1" ./internal/httpapi
go test -v -run "TestE2E_Tier2" ./internal/httpapi
go test -v -run "TestE2E_Tier3" ./internal/httpapi
go test -v -run "TestE2E_Tier4" ./internal/httpapi

# Run a single feature test (e.g. Feature 4: Photographer Registration)
go test -v -run "TestE2E_Tier1/Feature04" ./internal/httpapi
```

In Bash / Linux:
```bash
# Run all E2E test suites
PICTIDE_TEST_MONGODB=1 go test -v -run "TestE2E_" ./internal/httpapi
```

### 4.2 Running Frontend & Client-Side Tests
```bash
node --test test/*.test.js
```

### 4.3 Running Unit Tests (without external dependencies)
```bash
go test ./...
```

---

## 5. Coverage & Quality Thresholds

| Metric | Target | Verification Method |
|---|---|---|
| Feature Coverage | 100% (30 / 30 features) | Tier 1 test matrix |
| Boundary & Error Path Coverage | 100% of defined edge cases | Tier 2 test matrix |
| Cross-Module Interactions | 100% of defined workflows | Tier 3 test matrix |
| Zero 503 Errors | 0 occurrences | Asserted across all tiers |
| Multi-Tenant Isolation | 100% data separation | Tier 3 tenant isolation tests |
| Ephemeral Database Drop | 100% cleanup rate | Defer drop in all test suites |
