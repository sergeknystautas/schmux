---
allowed-tools: Bash(git *)
description: Push current branch to remote, handling divergence and edge cases
---

## Steps

### Step 1: Pre-flight checks

Run these commands:

- `git status` — if there are uncommitted changes, STOP and tell the user to commit or stash first.
- `git branch --show-current` — record the current branch name. If on `main`, STOP and warn the user they are about to push directly to main. Ask for confirmation before proceeding.

### Step 2: Check remote state

Run `git fetch origin` to update remote tracking refs.

Then check if the remote branch exists:

- `git rev-parse --verify origin/<branch>` — if this fails, the remote branch doesn't exist yet. Skip to Step 4 (simple push).

### Step 3: Handle divergence

If the remote branch exists, check the relationship:

```
git rev-list --left-right --count origin/<branch>...HEAD
```

This outputs `<behind>\t<ahead>`.

- **Behind = 0, Ahead > 0**: Local is strictly ahead. Skip to Step 4 (simple push).
- **Behind > 0, Ahead = 0**: Local is behind remote. STOP and tell the user their branch is behind the remote — they should pull or rebase first.
- **Behind > 0, Ahead > 0**: Branches have diverged. This typically happens after a rebase or squash. Ask the user to choose:
  - **Force push** (`git push --force-with-lease`): Use this if you rebased or squashed locally and want to replace the remote branch. `--force-with-lease` is safe — it will refuse if someone else pushed commits you haven't seen.
  - **Abort**: Stop and let the user decide what to do.

### Step 4: Push

- If the remote branch does not exist: `git push -u origin <branch>`
- If the remote branch exists and local is ahead: `git push origin <branch>`
- If the user chose force push: `git push --force-with-lease origin <branch>`

### Step 5: Report

Show:

- `git log --oneline origin/<branch>..` (should be empty, confirming push succeeded)
- The remote branch URL if available (`git remote get-url origin` to construct it)
