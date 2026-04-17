---
allowed-tools: Bash(git *), Bash(go build *), Bash(go run *), Bash(./format.sh*), Bash(./test.sh*), Bash(./badcode.sh*)
description: Rebase current branch onto origin/main and resolve conflicts
---

## Steps

### Step 1: Pre-flight checks

Run these commands and report the results:

- `git status` — if there are uncommitted changes, STOP and tell the user to commit or stash first.
- `git branch --show-current` — record the current branch name. If on `main`, STOP and tell the user this command is for feature branches.

### Step 2: Fetch and rebase

```
git fetch origin main
git rebase origin/main
```

**If rebase succeeds with no conflicts:** skip to Step 4.

**If rebase stops with conflicts:** continue to Step 3.

### Step 3: Resolve conflicts

For each conflicting file:

1. Read the file to understand both sides of the conflict.
2. Understand the intent of the local changes (this branch) and the incoming changes (origin/main).
3. Resolve the conflict by merging both intents correctly — do not blindly pick one side.
4. After editing, run `git add <file>` to mark it resolved.

Once all conflicts in the current step are resolved, run `git rebase --continue`.

If new conflicts appear in subsequent commits, repeat this step.

**Important:**

- NEVER run `git rebase --abort` unless the user explicitly asks to abort.
- NEVER run `git rebase --skip` — every commit should be preserved.
- After resolving conflicts in generated files (`types.generated.ts`), regenerate them instead: `go run ./cmd/gen-types`
- After resolving conflicts in any source files, run `./format.sh` to ensure formatting is correct.

### Step 4: Verify

After the rebase completes successfully:

1. Run `./format.sh` to ensure all files are properly formatted.
2. Run `go build ./cmd/schmux` to confirm the project compiles.
3. Run `./test.sh` to confirm tests pass.
4. Run `./badcode.sh` to confirm static analysis passes.

**If build, tests, or badcode fail:** investigate and fix the issue, then amend the current HEAD commit with the fix (`git add <files> && git commit --amend --no-edit`).

### Step 5: Report

Summarize:

- How many commits were rebased
- Whether any conflicts were resolved (and in which files)
- Build and test results
