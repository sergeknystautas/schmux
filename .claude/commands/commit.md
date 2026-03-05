---
allowed-tools: Bash(git status:*), Bash(git diff:*), Bash(git log:*), Bash(git add:*), Bash(git commit:*), Bash(./test.sh*), Bash(go vet:*)
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
- Run `./test.sh --quick`. If it fails, STOP.

**If behavioral files are staged but none are Go files** (frontend-only changes):

- Run `./test.sh --quick`. If it fails, STOP.

Continue to Step 4.

---

### Step 4: Self-Assessment Checklist

Answer each item with YES or NO based on the actual state of your changes. A rationalized YES is a NO.

1. **Tests written**: For every new function, handler, component, or feature in this commit, is there a corresponding test? If you added behavior with no test, this is NO.

2. **No architecture drift**: Did you use existing patterns rather than inventing new ones? Specific things that are always NO:
   - Added polling where WebSocket state (`SessionsContext`, `/ws/dashboard`) already provides updates
   - Added new client-side state management where `SessionsContext` is the established pattern
   - Called `npm install`, `npm run build`, or `vite build` directly instead of `go run ./cmd/build-dashboard`
   - Edited `assets/dashboard/src/lib/types.generated.ts` directly instead of editing Go structs and running `go run ./cmd/gen-types`
   - Used `fmt.Print`/`fmt.Println`/`fmt.Printf` or stdlib `log.Printf` for logging in `internal/` packages instead of the project's logging system (`charmbracelet/log` via `internal/logging`). Packages with a `*Server` should use `s.logger`; standalone packages should use the `pkgLogger`/`SetLogger` pattern (see `internal/tunnel`, `internal/update`, `internal/dashboardsx` for examples). Direct stdout printing is only acceptable in `cmd/` packages for user-facing CLI output.
   - **Notification UX violations** in frontend code (`assets/dashboard/`): Informational-only messages (e.g., "action succeeded") must use toasts (`useToast().success()`), which are non-blocking and auto-dismiss. Error messages from operation failures (e.g., API errors, spawn failures, dispose failures) must use dialogs (`useModal().alert()`), which are blocking, readable, and allow the user to copy the error text. Showing operation errors as toasts is always NO — the user cannot read or copy a 3-second auto-dismissing message. Validation errors for user input (e.g., "field is required") may use toasts since the fix is immediately obvious. Never use native `window.confirm()` or `window.alert()` — always use `useModal().confirm()` / `useModal().alert()` from `ModalProvider`.

3. **Docs current**: Are all relevant docs updated? `docs/api.md` is covered by Step 2. Consider: does this change affect `docs/web.md`, `docs/cli.md`, `CLAUDE.md`, `AGENTS.md`, or any spec in `docs/specs/`?

**If any item is NO:** STOP. Fix the gap, then re-invoke `/commit` from the beginning.

**If all items are YES:** Continue to Step 5.

---

### Step 5: Create the Commit

All checks passed. Stage all relevant files and create a single commit.

Commit message format:

- First line: short imperative subject (`type(scope): description`, e.g. `feat(session): add resume support`)
- No body paragraphs or padding lines
- **Do NOT add a Co-Authored-By line** — agents should not attribute commits to themselves
- **Do NOT add "generated" markers** — no "AI-generated" or similar disclaimers
- **Focus on features, not code changes** — describe what the commit accomplishes, not just which files or functions were modified
