# Dispatch: Explorer M1 Fix 1

You are explorer_m1_fix_1 (teamwork_explorer).
Working Directory: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_fix_1\`
Project Scope: `c:\Users\Hp\Desktop\lumen v2\PROJECT.md`
Original Request: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\ORIGINAL_REQUEST.md`
Gate Status: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\orchestrator\GATE_STATUS.md`
Reviewer Feedback: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\reviewer_m1_1\handoff.md`

## Milestone M1 Retry: Router Fix Strategy
Reviewer M1.1 found:
In `internal/httpapi/server.go:529`, `isAuthRoute` matches all `/api/galleries/:id/guest-uploads` paths regardless of HTTP method.
Unauthenticated callers attempting `POST /api/galleries/:id/guest-uploads` receive HTTP 401 "Please sign in to your studio.", which violates `PROJECT.md` § Interface Contracts (line 57) and causes `TestE2E_Tier1/Feature01` to fail.

Analyze `internal/httpapi/server.go` around `isAuthRoute` and `authenticatedAPI` / `publicAPI`:
1. Design the exact code fix so that `POST /api/galleries/:id/guest-uploads` is handled in `publicAPI` (or excluded from `isAuthRoute` when method is `POST`) without requiring authentication.
2. Ensure `GET /api/galleries/:id/guest-uploads` remains protected for studio owners in `authenticatedAPI`.
3. Provide the exact drop-in code diff plan for the Worker.

Write your report to `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_fix_1\fix_strategy.md` and `handoff.md`. Send a message when done.

## 2026-11-10T21:51:10Z
Read the original request at: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\ORIGINAL_REQUEST.md
Read the project scope at: c:\Users\Hp\Desktop\lumen v2\PROJECT.md
Read gate status at: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\orchestrator\GATE_STATUS.md
Read reviewer feedback at: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\reviewer_m1_1\handoff.md
Read dispatch instructions at: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_fix_1\DISPATCH.md

Your working directory is: c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_fix_1\

Analyze server.go and design the exact fix for isAuthRoute / authenticatedAPI / publicAPI to exempt POST /api/galleries/:id/guest-uploads from authentication while keeping GET authenticated. Write fix_strategy.md and handoff.md, then send a message when done.
