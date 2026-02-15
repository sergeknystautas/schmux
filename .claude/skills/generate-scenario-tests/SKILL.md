---
name: generate-scenario-tests
description: Generate Playwright test files from plain English scenario descriptions in test/scenarios/
---

# Generate Scenario Tests

You are generating Playwright test files from plain English scenario descriptions.

## Process

1. Read ALL scenario files from `test/scenarios/*.md`
2. Read the shared test harness at `test/scenarios/generated/helpers.ts`
3. Read the existing generated test as a template: `test/scenarios/generated/spawn-single-session.spec.ts`
4. For each scenario file, read the relevant React components to understand:
   - What `data-testid` attributes exist (search for `data-testid` in `assets/dashboard/src/`)
   - What routes exist (`assets/dashboard/src/App.tsx`)
   - What API endpoints are called (`internal/dashboard/handlers.go`)

5. For each scenario file, generate a `.spec.ts` file in `test/scenarios/generated/`
   - File name: same as scenario file but with `.spec.ts` extension
   - Follow the exact patterns from the template test
   - Use `data-testid` selectors where available, fall back to role-based selectors
   - Include both browser assertions (Playwright expects) and API assertions (fetch calls)
   - Map `## Preconditions` to `test.beforeAll` setup
   - Map `## Verifications` to test assertions

## Rules

- Use helpers from `helpers.ts` — do NOT duplicate helper logic in tests
- Use `data-testid` selectors as the primary selector strategy
- Each scenario file produces exactly one `.spec.ts` file
- Each `.spec.ts` file has one `test.describe` block with one or more `test` blocks
- Use real agent commands like `sh -c 'echo hello; sleep 600'` for test agents (mirrors `internal/e2e/e2e.go` line 240)
- Always call `waitForHealthy()` in `beforeAll`
- Always call `waitForDashboardLive(page)` after navigation
- Set reasonable timeouts for async operations (15s for spawn, 10s for WebSocket)

## Output

After generating all test files, run:

```bash
cd test/scenarios/generated && npx tsc --noEmit
```

Report any type errors and fix them before presenting the results.
