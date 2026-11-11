# Dispatch: Explorer M1.3 — Dynamic Config, Health & Automated Tests Strategy

You are explorer_m1_3 (teamwork_explorer).
Working Directory: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_3\`
Project Scope: `c:\Users\Hp\Desktop\lumen v2\PROJECT.md`
Original Request: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\ORIGINAL_REQUEST.md`

## Milestone
Milestone M1: Core DB Schemas, Indexes & API Gateway Routing.

## Objective
Analyze `internal/httpapi/server.go` and test suites to design the exact implementation strategy to:
1. Replace hardcoded `email_configured: false`, `imgbb_previews: false` in `/api/config` (`server.go:206`) with dynamic checks based on `s.cfg.SMTPHost != ""` and `s.cfg.ImgBBApiKey != ""`.
2. Update `/api/v1/health` (`server.go:210`) to perform a ping on MongoDB (`s.db.Client().Ping(...)`) and return `{"app": "pictide", "status": "ok", "database": "connected"}`.
3. Design automated integration tests for M1 in `internal/httpapi/m1_test.go` verifying:
   - MongoDB database connection and all collections/indexes provisioned without error.
   - Dynamic `/api/config` and `/api/v1/health` responses.
   - Unrecognized routes return 404 instead of 503 `"This feature is still being converted to MongoDB."`.
4. Provide the exact code diff plan for the Worker.

Write your strategy to:
- `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_3\strategy.md`
- `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_3\handoff.md`

## 2026-11-10T20:58:34Z
Read the original request at: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\ORIGINAL_REQUEST.md
Read the project scope at: c:\Users\Hp\Desktop\lumen v2\PROJECT.md
Read your dispatch instructions at: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_3\DISPATCH.md

Your working directory is: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_3\

## Milestone M1: Dynamic Config, Health & Automated Tests Strategy
Analyze internal/httpapi/server.go and test suites to design the exact implementation strategy to:
1. Dynamic config (/api/config) based on SMTP and ImgBB keys.
2. Dynamic health (/api/v1/health) with live MongoDB ping.
3. Automated integration test plan in internal/httpapi/m1_test.go verifying M1 against MongoDB.

Write your strategy to:
- c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_3\strategy.md
- c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_3\handoff.md
Send a message when done.
