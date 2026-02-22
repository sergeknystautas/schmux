# Definition of Done — Approach A Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create a project-level `/commit` command that enforces definition-of-done checks before any commit is created, and update CLAUDE.md and AGENTS.md to require agents to use it.

**Architecture:** A single `.claude/commands/commit.md` file overrides the plugin `/commit` command for this repo. It runs mechanical checks (tests, API docs) as shell commands and requires a structured self-assessment before allowing `git commit` to proceed. Humans can still use `git commit` directly; only the agent-facing `/commit` command is gated.

**Tech Stack:** Claude Code command files (markdown with YAML frontmatter), bash, `./test.sh`, `git`

---

### Task 1: Create `.claude/commands/commit.md`

**Files:**

- Create: `.claude/commands/commit.md`

**Step 1: Create the commands directory if needed**

Check whether `.claude/commands/` exists. It already exists (`.claude/skills/` is present, commands directory may need creation). Create it if missing:

```bash
mkdir -p .claude/commands
```

**Step 2: Write the commit command file**

Create `.claude/commands/commit.md` with this exact content:

```markdown
---
allowed-tools: Bash(git status:*), Bash(git diff:*), Bash(git log:*), Bash(git add:*), Bash(git commit:*), Bash(./test.sh*)
description: Create a git commit with definition-of-done enforcement
---

## Context

- Current git status: !`git status`
- Staged changes: !`git diff --cached --stat`
- Current branch: !`git branch --show-current`
- Recent commits: !`git log --oneline -5`

## Definition of Done — Complete ALL steps before committing

You MUST work through every step below in order. If any step fails, STOP and do not proceed to the next step. Do not rationalize past a failure.

---

### Step 1: Run Tests

Run `./test.sh` now and wait for it to complete.

**If tests fail:** STOP. Do not commit. Fix the failing tests, then re-invoke `/commit` from the beginning.

**If tests pass:** Continue to Step 2.

---

### Step 2: API Documentation Check

Examine the staged changes. If any staged file path starts with any of these prefixes:

- `internal/dashboard/`
- `internal/config/`
- `internal/state/`
- `internal/workspace/`
- `internal/session/`
- `internal/tmux/`

Then `docs/api.md` MUST also appear in the staged changes.

Run `git diff --cached --name-only` to see what is staged.

**If API-related files are staged but `docs/api.md` is not:** STOP. Update `docs/api.md` to reflect your API changes, stage it with `git add docs/api.md`, then re-invoke `/commit` from the beginning.

**If no API-related files are staged, or `docs/api.md` is already staged:** Continue to Step 3.

---

### Step 3: Self-Assessment Checklist

Answer each item with YES or NO based on the actual state of your changes. A rationalized YES is a NO.

1. **Tests written**: For every new function, handler, component, or feature in this commit, is there a corresponding test? If you added behavior with no test, this is NO.

2. **No architecture drift**: Did you use existing patterns rather than inventing new ones? Specific things that are always NO:
   - Added polling where WebSocket state (`SessionsContext`, `/ws/dashboard`) already provides updates
   - Added new client-side state management where `SessionsContext` is the established pattern
   - Called `npm install`, `npm run build`, or `vite build` directly instead of `go run ./cmd/build-dashboard`
   - Edited `assets/dashboard/src/lib/types.generated.ts` directly instead of editing Go structs and running `go run ./cmd/gen-types`

3. **Docs current**: Are all relevant docs updated? `docs/api.md` is covered by Step 2. Consider: does this change affect `docs/web.md`, `docs/cli.md`, `CLAUDE.md`, `AGENTS.md`, or any spec in `docs/specs/`?

**If any item is NO:** STOP. Fix the gap, then re-invoke `/commit` from the beginning.

**If all items are YES:** Continue to Step 4.

---

### Step 4: Create the Commit

All checks passed. Stage all relevant files and create a single commit.

Commit message format:

- First line: short imperative subject (`type(scope): description`, e.g. `feat(session): add resume support`)
- No body paragraphs or padding lines
- **Do NOT add a Co-Authored-By line** — agents should not attribute commits to themselves
- **Do NOT add "generated" markers** — no "AI-generated" or similar disclaimers
- **Focus on features, not code changes** — describe what the commit accomplishes, not just which files or functions were modified
```

**Step 3: Verify the file renders correctly**

Read back `.claude/commands/commit.md` and confirm:

- YAML frontmatter is valid (no stray characters)
- All four steps are present
- The `allowed-tools` line includes `Bash(./test.sh*)`
- No content was accidentally truncated

---

### Task 2: Update CLAUDE.md

**Files:**

- Modify: `CLAUDE.md` — add a commit command section to the Pre-Commit Requirements section

**Step 1: Find the Pre-Commit Requirements section**

Read `CLAUDE.md`. Locate the `## Pre-Commit Requirements` section. It currently reads:

```
Before committing changes, you MUST run:

1. **Run all tests**: `./test.sh --all`
2. **Format code**: `./format.sh` (or let the pre-commit hook handle it automatically)
```

**Step 2: Add the commit command requirement**

Replace the Pre-Commit Requirements section content so it reads:

```markdown
## Pre-Commit Requirements

**ALWAYS use `/commit` to create commits. NEVER run `git commit` directly.**

The `/commit` command enforces the definition of done before every commit:

- Runs `./test.sh` and aborts if tests fail
- Checks that `docs/api.md` is updated when API-related packages change
- Requires a structured self-assessment (tests written, no architecture drift, docs current)

Before the `/commit` command runs, ensure:

1. **Format code**: `./format.sh` (or let the pre-commit hook handle it automatically)

The pre-commit hook automatically formats staged Go, TypeScript, JavaScript, CSS, Markdown, and JSON files. Running `./format.sh` auto-installs the hook if missing.

For faster iteration during development:

- Run unit tests only: `./test.sh` (or `go test ./...`)
- Skip E2E tests and let CI handle them on PRs
```

**Step 3: Read back and verify**

Read the modified section in `CLAUDE.md` and confirm the `/commit` instruction appears prominently at the top of the section.

---

### Task 3: Update AGENTS.md

**Files:**

- Modify: `AGENTS.md` — update the Pre-Commit Requirements section

**Step 1: Find the Pre-Commit Requirements section**

Read `AGENTS.md`. Locate the `## Pre-Commit Requirements` section. It currently reads:

```
Before committing changes, you MUST run:

1. **Run all tests**: `./test.sh --all`
2. **Format code**: `./format.sh`
```

**Step 2: Add the commit command requirement**

Replace it so it reads:

```markdown
## Pre-Commit Requirements

**ALWAYS use `/commit` to create commits. NEVER run `git commit` directly.**

The `/commit` command enforces the definition of done:

- Runs `./test.sh` and aborts if tests fail
- Checks that `docs/api.md` is updated when API-related packages change
- Requires a structured self-assessment (tests written, no architecture drift, docs current)

Before invoking `/commit`, run `./format.sh` to format all staged files.

❌ **WRONG**: `git commit -m "message"`
✅ **RIGHT**: `/commit`
```

**Step 3: Read back and verify**

Read the modified section in `AGENTS.md` and confirm the instruction is clear and the ❌/✅ pattern matches the style used elsewhere in the file.

---

### Task 4: Test the Command

**Before committing**, verify the command works.

**Step 1: Start a new Claude Code session**

The `/commit` command may not be recognized in the current session because command files are loaded at session start. Start a fresh session:

```bash
# Exit current Claude Code session, then:
claude
```

**Step 2: Verify the command is available**

In the new session, check that `/commit` appears in the available commands:

- Look for `commit: Create a git commit with definition-of-done enforcement` in the system reminders
- Or try invoking it: type `/commit` and verify it loads the command prompt

**Step 3: Test the command flow**

With files staged (`git add .claude/commands/commit.md CLAUDE.md AGENTS.md`), invoke `/commit` and verify:

1. **Context loads**: Shows git status, staged changes, branch, recent commits
2. **Step 1 runs**: `./test.sh` executes
3. **Step 2 checks**: Verifies no API files are staged (or docs/api.md is staged if they are)
4. **Step 3 prompts**: Self-assessment checklist is presented
5. **Step 4 commits**: Creates the commit with proper format

**If the command is not recognized:**

The command file needs to be committed first for the skill loader to pick it up reliably. Use the fallback:

```bash
git add .claude/commands/commit.md CLAUDE.md AGENTS.md
git commit -m "feat(dev): add definition-of-done enforcement to /commit command"
```

Then start a new session and verify `/commit` works for future commits.

---

### Task 5: Commit

**Step 1: Format**

Run `./format.sh` to format any files touched.

**Step 2: Commit using the new command**

Use `/commit` — this is the first real-world test of the new command. It should:

1. Run `./test.sh` (unit tests only is fine for this change — no code was modified)
2. Check staged files — no API packages changed, so `docs/api.md` check passes automatically
3. Walk through the self-assessment (all YES: the command file itself is the new feature with no new functions to test, docs were updated in Tasks 2 and 3)
4. Create the commit

If `/commit` is not available in this session, fall back to:

```bash
git add .claude/commands/commit.md CLAUDE.md AGENTS.md
git commit -m "feat(dev): add definition-of-done enforcement to /commit command"
```
