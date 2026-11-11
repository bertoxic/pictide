# Dispatch: Explorer M1.2 — Database Schemas & Indexes Strategy

You are explorer_m1_2 (teamwork_explorer).
Working Directory: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_2\`
Project Scope: `c:\Users\Hp\Desktop\lumen v2\PROJECT.md`
Original Request: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\ORIGINAL_REQUEST.md`

## Milestone
Milestone M1: Core DB Schemas, Indexes & API Gateway Routing.

## Objective
Analyze `internal/mongostore/` and `internal/httpapi/server.go` to design the exact implementation strategy to:
1. Define BSON models and collection helpers in `internal/mongostore/` for `withdrawals`, `guest_uploads`, `account_links`, and `admin_audit`.
2. Provision all required indexes at startup in `httpapi.New` (`server.go`):
   - `accounts`: `{username: 1}` (unique), `{email: 1}` (sparse)
   - `account_links`: `{token: 1}` (unique), `{expires_at: 1}` (TTL index, expireAfterSeconds: 0)
   - `withdrawals`: `{id: 1}` (unique), `{studio_id: 1, created_at: -1}`, `{reference: 1}`
   - `guest_uploads`: `{id: 1}` (unique), `{gallery_id: 1, created_at: -1}`, `{studio_id: 1}`
   - `admin_audit`: `{id: 1}` (unique), `{created_at: -1}`
3. Provide the exact code diff plan for the Worker to implement in `internal/mongostore/` and `internal/httpapi/server.go`.

Write your strategy to:
- `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_2\strategy.md`
- `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_2\handoff.md`


## 2026-11-10T20:58:26Z
Read the original request at: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\ORIGINAL_REQUEST.md
Read the project scope at: c:\Users\Hp\Desktop\lumen v2\PROJECT.md
Read your dispatch instructions at: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_2\DISPATCH.md

Your working directory is: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_2\

## Milestone M1: Schemas & Indexes Strategy
Analyze internal/mongostore/ and internal/httpapi/server.go and provide the exact strategy and code diff plan for the Worker:
1. Define BSON models for missing collections: withdrawals, guest_uploads, account_links, admin_audit.
2. Index provisioning in httpapi.New (server.go).

Write your strategy to:
- c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_2\strategy.md
- c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_2\handoff.md
Send a message when done.
