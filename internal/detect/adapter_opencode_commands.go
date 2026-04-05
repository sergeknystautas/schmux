package detect

import (
	"fmt"
	"os"
	"path/filepath"
)

// opencodeCommitCommand is the definition-of-done commit workflow for OpenCode.
// Written to .opencode/commands/commit.md in each workspace.
const opencodeCommitCommand = `---
description: Create a git commit with definition-of-done enforcement
---

## How This Command Works

1. **Stage files first**: Before invoking /commit, use ` + "`git add <files>`" + ` to stage the changes you want to commit
2. **This command commits what is staged**: It runs definition-of-done checks against your staged changes, then commits them
3. **Format first**: Run ` + "`./format.sh`" + ` before staging to ensure files are formatted

## Context

- Current git status: !` + "`git status`" + `
- Staged changes: !` + "`git diff --cached --stat`" + `
- Current branch: !` + "`git branch --show-current`" + `
- Recent commits: !` + "`git log --oneline -5`" + `

## Definition of Done — Complete ALL steps before committing

You MUST work through every step below in order. If any step fails, STOP and do not proceed to the next step. Do not rationalize past a failure.

---

### Step 1: Run Tests

Run ` + "`./test.sh`" + ` now and wait for it to complete.

**If tests fail:** STOP. Do not commit. Fix the failing tests, then re-invoke /commit from the beginning.

**If tests pass:** Continue to Step 2.

---

### Step 2: Run Go Vet

Run ` + "`go vet ./...`" + ` now and wait for it to complete.

**If go vet reports issues:** STOP. Do not commit. Fix the issues, then re-invoke /commit from the beginning.

**If go vet passes:** Continue to Step 3.

---

### Step 3: API Documentation Check

Examine the staged changes. If any staged file path starts with any of these prefixes:

- ` + "`internal/dashboard/`" + `
- ` + "`internal/config/`" + `
- ` + "`internal/state/`" + `
- ` + "`internal/workspace/`" + `
- ` + "`internal/session/`" + `
- ` + "`internal/tmux/`" + `

Then ` + "`docs/api.md`" + ` MUST also appear in the staged changes.

Run ` + "`git diff --cached --name-only`" + ` to see what is staged.

**If API-related files are staged but ` + "`docs/api.md`" + ` is not:** STOP. Update ` + "`docs/api.md`" + ` to reflect your API changes, stage it with ` + "`git add docs/api.md`" + `, then re-invoke /commit from the beginning.

**If no API-related files are staged, or ` + "`docs/api.md`" + ` is already staged:** Continue to Step 4.

---

### Step 4: Self-Assessment Checklist

Answer each item with YES or NO based on the actual state of your changes. A rationalized YES is a NO.

1. **Tests written**: For every new function, handler, component, or feature in this commit, is there a corresponding test? If you added behavior with no test, this is NO.

2. **No architecture drift**: Did you use existing patterns rather than inventing new ones? Specific things that are always NO:
   - Added polling where WebSocket state already provides updates
   - Added new client-side state management where existing context is the established pattern
   - Called ` + "`npm install`" + `, ` + "`npm run build`" + `, or ` + "`vite build`" + ` directly instead of ` + "`go run ./cmd/build-dashboard`" + `
   - Edited ` + "`assets/dashboard/src/lib/types.generated.ts`" + ` directly instead of editing Go structs and running ` + "`go run ./cmd/gen-types`" + `

3. **Docs current**: Are all relevant docs updated? ` + "`docs/api.md`" + ` is covered by Step 3. Consider: does this change affect ` + "`docs/web.md`" + `, ` + "`docs/cli.md`" + `, ` + "`CLAUDE.md`" + `, ` + "`AGENTS.md`" + `, or any spec in ` + "`docs/specs/`" + ` or plan in ` + "`docs/plans/`" + `?

**If any item is NO:** STOP. Fix the gap, then re-invoke /commit from the beginning.

**If all items are YES:** Continue to Step 5.

---

### Step 5: Create the Commit

All checks passed. Stage all relevant files and create a single commit.

Commit message format:

- First line: short imperative subject (` + "`type(scope): description`" + `, e.g. ` + "`feat(session): add resume support`" + `)
- No body paragraphs or padding lines
- **Do NOT add a Co-Authored-By line** — agents should not attribute commits to themselves
- **Do NOT add "generated" markers** — no "AI-generated" or similar disclaimers
- **Focus on features, not code changes** — describe what the commit accomplishes, not just which files or functions were modified
`

// SetupCommands writes tool-specific command files into the workspace.
// For OpenCode, this creates .opencode/commands/commit.md.
func (a *OpencodeAdapter) SetupCommands(workspacePath string) error {
	commandsDir := filepath.Join(workspacePath, ".opencode", "commands")
	commitPath := filepath.Join(commandsDir, "commit.md")

	if err := os.MkdirAll(commandsDir, 0755); err != nil {
		return fmt.Errorf("failed to create .opencode/commands directory: %w", err)
	}
	if err := os.WriteFile(commitPath, []byte(opencodeCommitCommand), 0644); err != nil {
		return fmt.Errorf("failed to write commit command: %w", err)
	}
	return nil
}
