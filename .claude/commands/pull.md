---
allowed-tools: Bash(git *), Bash(./format.sh*), Bash(go build *), Bash(./test.sh*), Bash(./badcode.sh*)
description: Pull remote changes and rebase local commits on top
---

## Steps

### Step 1: Pre-flight checks

Run these commands:

- `git status` — if there are uncommitted changes, STOP and tell the user to commit or stash first.
- `git branch --show-current` — record the current branch name.

### Step 2: Fetch remote

Run `git fetch origin`.

Check if the remote tracking branch exists:

- `git rev-parse --verify origin/<branch>` — if this fails, STOP and tell the user there is no remote branch to pull from.

### Step 3: Check relationship

```
git rev-list --left-right --count origin/<branch>...HEAD
```

- **Behind = 0**: Local is already up to date. STOP and tell the user there is nothing to pull.
- **Behind > 0, Ahead = 0**: Local is behind with no local commits. Do a fast-forward: `git merge --ff-only origin/<branch>`. Skip to Step 5.
- **Behind > 0, Ahead > 0**: Remote has new commits and there are local commits to rebase. Continue to Step 4.

### Step 4: Rebase local commits onto remote

```
git rebase origin/<branch>
```

**If rebase succeeds with no conflicts:** continue to Step 5.

**If rebase stops with conflicts:** for each conflicting file:

1. Read the file to understand both sides of the conflict.
2. Understand the intent of the local changes and the incoming remote changes.
3. Resolve the conflict by merging both intents correctly — do not blindly pick one side.
4. After editing, run `git add <file>` to mark it resolved.
5. Run `./format.sh` to ensure formatting is correct.

Once all conflicts are resolved, run `git rebase --continue`.

If new conflicts appear in subsequent commits, repeat this process.

**Important:**

- NEVER run `git rebase --abort` unless the user explicitly asks to abort.
- NEVER run `git rebase --skip` — every commit should be preserved.

### Step 5: Verify

After pulling, verify the result builds and passes tests:

1. Run `go build ./cmd/schmux` to confirm compilation.
2. Run `./test.sh` to confirm tests pass.

**If any check fails:** the pulled changes may have introduced a breakage. Report the failure to the user.

### Step 6: Report

Show:

- `git log --oneline -5` to show the current state
- Whether it was a fast-forward or a rebase
- Whether any conflicts were resolved (and in which files)
- Build and test results
