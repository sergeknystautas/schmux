---
allowed-tools: Bash(git *)
description: Squash all feature branch commits into a single comprehensive commit
---

## Steps

### Step 1: Pre-flight checks

Run these commands:

- `git status` — if there are uncommitted changes, STOP and tell the user to commit or stash first.
- `git branch --show-current` — record the current branch name. If on `main`, STOP and tell the user this command is for feature branches.

### Step 2: Identify commits to squash

Run `git log --oneline origin/main..HEAD` to list all commits on this branch that are not on origin/main.

If there is only one commit, STOP and tell the user there is nothing to squash.

### Step 3: Gather full context

Run `git log --format='%s%n%n%b' origin/main..HEAD` to get the full subject and body of every commit.

Also run `git diff --stat origin/main..HEAD` to get the overall file change summary.

### Step 4: Compose the squashed commit message

Create a commit message with this structure:

```
<type>(<scope>): <one-line summary of the overall change>

<paragraph describing what this branch accomplishes and why, synthesized from all the individual commits>
```

Rules:

- The first line follows conventional commit format (`feat`, `fix`, `refactor`, `docs`, `ci`, `test`, `chore`).
- The body paragraph should synthesize the full intent and scope of the combined changes into a coherent description — not a list of individual commits.
- Do NOT add Co-Authored-By lines or AI-generated markers.

### Step 5: Perform the squash

```
git reset --soft origin/main
git commit -m "<composed message>"
```

### Step 6: Report

Show:

- The number of commits that were squashed
- The new commit message
- `git log --oneline -1` to confirm the result
