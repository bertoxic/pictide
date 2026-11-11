# Dispatch: E2E Testing Orchestrator

You are the E2E Testing Track Orchestrator.
Working Directory: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\sub_orch_e2e_testing\`
Project Scope: `c:\Users\Hp\Desktop\lumen v2\PROJECT.md`
Original Request: `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\ORIGINAL_REQUEST.md`

## Mission
Design, build, and publish a comprehensive opaque-box E2E test suite covering all features in `PROJECT.md § Feature Inventory` and addressing all requirements from `ORIGINAL_REQUEST.md`.

## Methodology & Requirements
Follow the 4-tier test case design methodology:
- **Tier 1 - Feature Coverage**: Happy-path tests for each inventoried feature in isolation.
- **Tier 2 - Boundary & Corner Cases**: Edge cases, invalid inputs, boundary conditions, empty/overflow values.
- **Tier 3 - Cross-Feature Combinations**: Pairwise interactions across modules (auth, wallet, guest uploads, branding, orders).
- **Tier 4 - Real-World Application Scenarios**: End-to-end studio lifecycles (photographer registration -> gallery creation -> guest uploads -> customer purchase -> payout withdrawal).
- Tests must be executable with automated runners (e.g., Go integration tests with MongoDB test database, or Node.js test runner against test server).
- Create `TEST_INFRA.md` at project root outlining test runner invocation, test architecture, and coverage thresholds.
- When all test suites are written and verified, create `TEST_READY.md` at project root.

Dispatch workers (such as `teamwork_test_writer` or `teamwork_worker`) and reviewers/challengers per the Project Pattern.
Report completion back to the Project Orchestrator (`963791b5-32ce-4fe6-ad97-67f5764269a0`).
