# Dispatch: Explorer M1.1 — Routing Dispatcher Strategy

You are explorer_m1_1 (teamwork_explorer).
Working Directory: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_1\`
Project Scope: `c:\Users\Hp\Desktop\lumen v2\PROJECT.md`
Original Request: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\ORIGINAL_REQUEST.md`

## Milestone
Milestone M1: Core DB Schemas, Indexes & API Gateway Routing.

## Objective
Analyze `internal/httpapi/server.go` and design the exact implementation strategy to:
1. Split the HTTP request router so that public `/api/` endpoints (e.g. `/api/register`, `/api/account/verify`, `/api/account/forgot`, `/api/account/reset`, `/api/galleries/:id/guest-uploads`) are routed before the authenticated gate (`user == nil`).
2. Remove the catch-all 503 `"This feature is still being converted to MongoDB."` fallthrough at line 330 of `server.go`.
3. Provide clean HTTP 404 / 405 error responses for unrecognized API routes.
4. Provide the exact code diff plan for the Worker to implement in `server.go`.

Write your strategy to:
- `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_1\strategy.md`
- `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_1\handoff.md`


## 2026-11-10T20:58:21Z
Read the original request at: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\ORIGINAL_REQUEST.md
Read the project scope at: c:\Users\Hp\Desktop\lumen v2\PROJECT.md
Read your dispatch instructions at: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_1\DISPATCH.md

Your working directory is: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_1\

## Milestone M1: Routing Dispatcher Strategy
Analyze internal/httpapi/server.go and provide the exact strategy and code diff plan for the Worker:
1. Split public vs authenticated /api/ routing.
2. Remove catch-all 503 "This feature is still being converted to MongoDB." at line 330 of server.go.
3. Clean 404/405 for unrecognized endpoints.

Write your strategy to:
- c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_1\strategy.md
- c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_1\handoff.md
Send a message when done.
