---
allowed-tools: Bash(git status:*), Bash(git diff:*), Bash(git log:*), Bash(git add:*), Bash(git commit:*), Bash(./test.sh*), Bash(go vet:*), Bash(go build:*)
description: Create a git commit with definition-of-done enforcement
---

## How This Command Works

1. **Stage files first**: Before invoking `/commit`, use `git add <files>` to stage the changes you want to commit
2. **This command commits what is staged**: It runs definition-of-done checks against your staged changes, then commits them
3. **Format first**: Run `./format.sh` before staging to ensure files are formatted

## Context

- Current git status: !`git status`
- Staged changes: !`git diff --cached --stat`
- Current branch: !`git branch --show-current`
- Recent commits: !`git log --oneline -5`

## Definition of Done — Complete ALL steps before committing

You MUST work through every step below in order. If any step fails, STOP and do not proceed to the next step. Do not rationalize past a failure.

---

### Step 1: Analyze Staged Changes

Run `git diff --cached --name-only` to list all staged files. Categorize them:

**Behavioral files** — changes that can cause runtime regressions:

- `*.go` files (Go source)
- `*.ts`, `*.tsx`, `*.js`, `*.jsx` files under `assets/dashboard/src/` (dashboard app source — NOT `assets/dashboard/website/`)
- `go.mod`, `go.sum` (Go dependency changes)
- `assets/dashboard/package.json`, `assets/dashboard/package-lock.json` (JS dependency changes)

**Non-behavioral files** — changes that tests cannot validate:

- `*.md`, `docs/**` (documentation)
- `.claude/**` (agent/skill configuration)
- `.github/**` (CI — tests run there anyway)
- `assets/dashboard/website/**` (marketing website, separate build)
- `*.sh`, `scripts/**` (tooling scripts)
- `Dockerfile*` (build config)
- `*.css` (stylesheets)
- `assets/dashboard/public/**` (static assets)
- `LICENSE`, `CODEOWNERS`, `.gitignore`, `.editorconfig`

Record which categories are present — you'll use this in Steps 2 and 3.

Continue to Step 2.

---

### Step 2: API Documentation Check

If any staged file path starts with any of these prefixes:

- `internal/dashboard/`
- `internal/config/`
- `internal/state/`
- `internal/workspace/`
- `internal/session/`
- `internal/tmux/`

Then `docs/api.md` MUST also appear in the staged changes.

**If API-related files are staged but `docs/api.md` is not:** STOP. Update `docs/api.md` to reflect your API changes, stage it with `git add docs/api.md`, then re-invoke `/commit` from the beginning.

**If no API-related files are staged, or `docs/api.md` is already staged:** Continue to Step 3.

---

### Step 3: Run Tests and Checks (conditional)

Using the categorization from Step 1:

**If no behavioral files are staged** (all changes are non-behavioral): print "Skipping tests — no behavioral changes staged" and continue to Step 4.

**If any Go files (`.go`) are staged:**

- Run `go vet ./...`. If it fails, STOP.
- **Logger lint**: Check that no staged `internal/` Go file imports the standard `"log"` package. Run: `git diff --cached --name-only -- 'internal/*.go' | xargs grep -l '^\t"log"$' 2>/dev/null`. If any files are printed, STOP — they must use `charmbracelet/log` (via `internal/logging`) instead of stdlib `log`. Packages with a `*Server` receiver should use `s.logger`; standalone packages should accept a `*log.Logger` in their constructor or use the `SetLogger` pattern.
- Run `./test.sh --quick`. If it fails, STOP.

**If behavioral files are staged but none are Go files** (frontend-only changes):

- Run `./test.sh --quick`. If it fails, STOP.

Continue to Step 4.

---

### Step 4: Module Exclusion Build Check (conditional)

**Skip this step if no Go files are staged.**

Several packages can be excluded from the binary via build tags. If you modify files in an excludable module, the disabled build must still compile. Check the staged file list against this mapping:

| Staged path prefix                        | Tag to verify     |
| ----------------------------------------- | ----------------- |
| `internal/telemetry/`                     | `notelemetry`     |
| `internal/update/`                        | `noupdate`        |
| `internal/models/registry`                | `nomodelregistry` |
| `internal/dashboardsx/`                   | `nodashboardsx`   |
| `internal/dashboard/handlers_dashboardsx` | `nodashboardsx`   |
| `cmd/schmux/dashboardsx`                  | `nodashboardsx`   |
| `internal/repofeed/`                      | `norepofeed`      |
| `internal/dashboard/handlers_repofeed`    | `norepofeed`      |
| `cmd/schmux/repofeed`                     | `norepofeed`      |
| `internal/subreddit/`                     | `nosubreddit`     |
| `internal/dashboard/handlers_subreddit`   | `nosubreddit`     |
| `internal/tunnel/`                        | `notunnel`        |
| `internal/github/`                        | `nogithub`        |

**Broad-impact files** — if ANY of these are staged, verify ALL exclusion tags:

- `internal/daemon/daemon.go`
- `internal/dashboard/server.go`
- `internal/dashboard/handlers_features.go`
- `internal/api/contracts/features.go`

For each affected tag, run: `go build -tags <tag> ./cmd/schmux`

If verifying all tags (broad-impact file touched), run:

```
go build -tags notelemetry ./cmd/schmux
go build -tags noupdate ./cmd/schmux
go build -tags nomodelregistry ./cmd/schmux
go build -tags nodashboardsx ./cmd/schmux
go build -tags norepofeed ./cmd/schmux
go build -tags nosubreddit ./cmd/schmux
go build -tags notunnel ./cmd/schmux
go build -tags nogithub ./cmd/schmux
```

**If any exclusion build fails:** STOP. Update the corresponding `_disabled.go` stub to match your changes, then re-invoke `/commit` from the beginning.

**If all exclusion builds pass (or no excludable modules are staged):** Continue to Step 5.

---

### Step 5: Self-Assessment Checklist

Answer each item with YES or NO based on the actual state of your changes. A rationalized YES is a NO.

1. **Tests written**: For every new function, handler, component, or feature in this commit, is there a corresponding test? If you added behavior with no test, this is NO.

2. **No architecture drift**: Did you use existing patterns rather than inventing new ones? Specific things that are always NO:
   - Added polling where WebSocket state (`SessionsContext`, `/ws/dashboard`) already provides updates
   - Added new client-side state management where `SessionsContext` is the established pattern
   - Called `npm install`, `npm run build`, or `vite build` directly instead of `go run ./cmd/build-dashboard`
   - Edited `assets/dashboard/src/lib/types.generated.ts` directly instead of editing Go structs and running `go run ./cmd/gen-types`
   - Used `fmt.Print`/`fmt.Println`/`fmt.Printf` or stdlib `log.Printf` for logging in `internal/` packages instead of the project's logging system (`charmbracelet/log` via `internal/logging`). Packages with a `*Server` should use `s.logger`; standalone packages should use the `pkgLogger`/`SetLogger` pattern (see `internal/tunnel`, `internal/update`, `internal/dashboardsx` for examples). Direct stdout printing is only acceptable in `cmd/` packages for user-facing CLI output.
   - Used `sleep()`, `time.Sleep()`, `setTimeout` as a fixed delay in test code instead of waiting for a state transition (e.g., poll for a condition, wait for a WebSocket message, check a DOM element, use Playwright's auto-retry assertions). Fixed sleeps make tests both slow and flaky. The only acceptable exception is negative assertions where you must wait to verify something does NOT happen.
   - **Notification UX violations** in frontend code (`assets/dashboard/`): Informational-only messages (e.g., "action succeeded") must use toasts (`useToast().success()`), which are non-blocking and auto-dismiss. Error messages from operation failures (e.g., API errors, spawn failures, dispose failures) must use dialogs (`useModal().alert()`), which are blocking, readable, and allow the user to copy the error text. Showing operation errors as toasts is always NO — the user cannot read or copy a 3-second auto-dismissing message. Validation errors for user input (e.g., "field is required") may use toasts since the fix is immediately obvious. Never use native `window.confirm()` or `window.alert()` — always use `useModal().confirm()` / `useModal().alert()` from `ModalProvider`.

3. **Docs current**: Are all relevant docs updated? `docs/api.md` is covered by Step 2. Consider: does this change affect `docs/web.md`, `docs/cli.md`, `CLAUDE.md`, `AGENTS.md`, or any spec in `docs/specs/`?

**If any item is NO:** STOP. Fix the gap, then re-invoke `/commit` from the beginning.

**If all items are YES:** Continue to Step 6.

---

### Step 6: Create the Commit

All checks passed. Stage all relevant files and create a single commit.

Commit message format:

- First line: short imperative subject (`type(scope): description`, e.g. `feat(session): add resume support`)
- No body paragraphs or padding lines
- **Do NOT add a Co-Authored-By line** — agents should not attribute commits to themselves
- **Do NOT add "generated" markers** — no "AI-generated" or similar disclaimers
- **Focus on features, not code changes** — describe what the commit accomplishes, not just which files or functions were modified
