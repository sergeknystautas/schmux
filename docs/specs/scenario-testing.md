# Scenario Testing Design

## Overview

A regression testing system where plain English scenario descriptions are the source of truth, an LLM generates Playwright test code from them at authoring time, and CI runs the generated tests deterministically with no LLM involvement at runtime.

A scenario is something a user wants to accomplish. A scenario regression is when the user can no longer accomplish that goal because the system stopped behaving as expected.

## Architecture

Two testing layers run the same scenarios but catch different classes of regressions:

| Layer              | What it checks       | How                                                       |
| ------------------ | -------------------- | --------------------------------------------------------- |
| API assertions     | Backend correctness  | HTTP/WebSocket calls with exact value checks              |
| Browser assertions | Frontend correctness | Playwright drives headless Chromium against the dashboard |

Both layers are embedded in the same generated test file. If the API layer passes but the browser layer fails, it's a frontend problem. If the API layer fails, the backend is broken regardless of what the browser shows.

## Components

```
test/scenarios/
├── spawn-two-agents.md              # Human/agent-authored scenario (source of truth)
├── review-code-changes.md
├── resolve-merge-conflict.md
└── generated/
    ├── helpers.ts                    # Shared test harness (setup, teardown, API client)
    ├── spawn-two-agents.spec.ts      # Generated Playwright test
    ├── review-code-changes.spec.ts
    └── resolve-merge-conflict.spec.ts
```

### Scenario Files

Plain English markdown files. Each describes a user goal, the steps to achieve it, preconditions, and what success looks like. The format is conventional, not a schema:

```markdown
# Spawn a session with two agents

A user wants to start two AI agents working on the same task in a fresh workspace.

They navigate to the spawn page, type a task description like
"Fix the authentication timeout bug", select two agents (e.g., Claude Code
and Codex), and submit the form.

After submitting, they should land on a session detail page showing a live
terminal with output streaming. Going back to the home page, they should
see a workspace with two sessions listed under it.

## Preconditions

- The daemon is running with at least one repository configured
- At least two agents are configured in settings

## Verifications

- The spawn form accepts the input and submits without error
- The user lands on a session page with a streaming terminal
- The home page shows the workspace with both sessions
- GET /api/sessions returns two sessions under the same workspace
```

The `## Verifications` section mixes UI checks and API checks naturally. The generator separates them into Playwright assertions and HTTP assertions in the generated code.

The `## Preconditions` section tells the generator what setup the test needs (start daemon, seed config, create a test repo).

### Generator

A skill (`/generate-scenario-tests`) that an agent runs locally. It:

1. Reads all scenario files from `test/scenarios/`
2. Reads the relevant UI code — React components, route definitions, API handlers — to understand what selectors, URLs, and endpoints exist
3. Produces Playwright test files in `test/scenarios/generated/`, one per scenario file (e.g., `spawn-two-agents.md` → `spawn-two-agents.spec.ts`)
4. Produces a shared test harness (`helpers.ts`) that handles setup: starting the Docker container, waiting for the daemon health check, seeding config, tearing down

Generated files are committed to the repo. The generator doesn't run in CI — it's a local authoring tool. It regenerates everything each time (no incremental mode) to avoid stale partial state.

Example generated test:

```typescript
test('spawn a session with two agents', async ({ page }) => {
  // Precondition: daemon running, repo configured
  await setupDaemon({ repos: ['test-repo'], agents: ['claude-code', 'codex'] });

  // Navigate to spawn
  await page.goto('/spawn');

  // Fill prompt
  await page.getByRole('textbox', { name: /prompt/i }).fill('Fix the authentication timeout bug');

  // Select agents
  await page.getByRole('button', { name: /claude code/i }).click();
  await page.getByRole('button', { name: /codex/i }).click();

  // Submit
  await page.getByRole('button', { name: /engage/i }).click();

  // Verify: landed on session page with streaming terminal
  await expect(page).toHaveURL(/\/sessions\//);
  await expect(page.getByTestId('terminal')).toBeVisible();

  // Verify: API shows two sessions
  const res = await fetch('http://localhost:7337/api/sessions');
  const data = await res.json();
  const workspace = data.workspaces.find((w) => w.sessions.length === 2);
  expect(workspace).toBeDefined();
});
```

### CI Execution

Extends the existing Docker-based E2E setup. The Dockerfile adds:

- Headless Chromium (for Playwright)
- Node.js + Playwright (the test runner)
- The built schmux binary + dashboard (same as current E2E)

CI job sequence:

1. Build schmux binary and dashboard
2. Start the schmux daemon inside the container
3. Run Playwright tests against `localhost:7337`
4. Capture screenshots on failure as CI artifacts
5. Report pass/fail

Integration with `test.sh`:

```bash
./test.sh --scenarios    # Run scenario tests only
./test.sh --all          # Unit + E2E + scenarios
```

Scenario tests can be kept opt-in initially (`--all` runs unit + E2E, `--scenarios` is separate) until the suite is trusted.

## Agent Authoring Workflow

Two pieces of tooling make scenario authoring part of the development loop:

### `/scenario` Skill

An agent invokes this after implementing a feature or fixing a bug. The skill:

1. Examines the current diff (what files changed, what was added)
2. Reads existing scenarios to understand the format and avoid duplicates
3. Writes a new scenario file in `test/scenarios/` describing the user-facing behavior
4. Runs the generator to produce the Playwright test
5. Presents both files for review

### Coverage Hook

A post-commit or pre-diff hook that checks whether changed files touch UI routes or API handlers without a corresponding scenario update. It nudges, it doesn't block:

> You modified the spawn wizard but no scenario files were added or updated. Consider running `/scenario` to add coverage.

## Regeneration Triggers

Run the generator when:

- A scenario file is added or edited
- UI changes break existing generated tests
- You want to refresh all tests against the current codebase

## Day-to-Day Workflow

1. Implement a feature or fix a bug
2. Agent (or human) writes a scenario file in plain English
3. Agent runs the generator to produce Playwright tests
4. Review both the scenario and generated test, commit them
5. CI runs the generated tests deterministically on every PR
6. When UI changes break a test, regenerate from the unchanged scenario file
7. Coverage hook nudges when UI/API changes lack scenario coverage
