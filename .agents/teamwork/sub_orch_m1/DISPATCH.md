# Dispatch: Sub-orchestrator M1

You are the Sub-orchestrator for Milestone M1 (Core DB Schemas, Indexes & API Gateway Routing).
Working Directory: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\sub_orch_m1\`
Project Scope: `c:\Users\Hp\Desktop\lumen v2\PROJECT.md`
Original Request: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\ORIGINAL_REQUEST.md`

## Mission
Execute Milestone M1: Core DB Schemas, Indexes & API Gateway Routing.

### Detailed Scope:
1. **MongoDB Collections & Indexes**:
   - Define structures, collection names, and indexes in `internal/mongostore/` and `internal/httpapi/server.go`.
   - Collections: `accounts`, `account_links`, `galleries`, `photos`, `orders`, `withdrawals`, `guest_uploads`, `admin_audit`, `settings`, `direct_uploads`, GridFS buckets.
   - Indexes: Ensure indexes are provisioned on startup in `httpapi.New` (including unique indexes on `accounts.username`, `galleries.id`, `photos.id`, TTL index on `account_links.expires_at`, indexes on `withdrawals.studio_id`, `guest_uploads.gallery_id`, `admin_audit.created_at`).
2. **API Gateway Routing Dispatcher**:
   - Refactor `internal/httpapi/server.go` router to properly dispatch public vs authenticated `/api/` endpoints.
   - Public endpoints (such as `/api/register`, `/api/account/verify`, `/api/account/forgot`, `/api/account/reset`, `/api/galleries/:id/guest-uploads`) must not be blocked by the `user == nil` 401 gate.
   - Remove the catch-all 503 `"This feature is still being converted to MongoDB."` fallthrough. Return appropriate 404 Not Found or 405 Method Not Allowed for unknown endpoints.
3. **Dynamic Config & Health**:
   - In `/api/config`, replace hardcoded `email_configured: false`, `imgbb_previews: false` with dynamic checks based on actual server configuration.
   - In `/api/v1/health`, report accurate system health and MongoDB connectivity.
4. **Automated Tests**:
   - Write automated unit and integration tests verifying schema/index creation, router dispatching, and dynamic config/health against MongoDB using the isolated test database pattern (`pictide_test_<randomToken>`).

Follow the Sub-orchestrator procedure:
1. Assess scope and write `SCOPE.md` in your working directory.
2. Run iteration cycle: Explorer(s) -> Worker -> Reviewers + Challengers + Forensic Auditor -> Gate.
3. When gate passes, update `SCOPE.md` and report completion back to the Project Orchestrator (`963791b5-32ce-4fe6-ad97-67f5764269a0`).
