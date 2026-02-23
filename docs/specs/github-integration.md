# GitHub Integration

## Problem

Schmux has no coherent GitHub integration story. GitHub-specific features leak into contexts where they don't apply (PR list on the home page shows for everyone), and useful GitHub features are absent or half-built. Meanwhile, creating a local repo is a dead end — if someone pushes it to GitHub, schmux still thinks it's `local:name` and the config is permanently wrong.

The root cause is that schmux has no way to answer: "can this user do GitHub things?"

## Foundation: `gh auth status` Capability Check

At daemon startup, run `gh auth status` once. Store the result in memory on the `Daemon` struct:

```go
type GitHubStatus struct {
    Available    bool      // gh CLI is installed and authenticated
    Username     string    // authenticated GitHub username
    CheckedAt    time.Time // when we last checked
}
```

This is the single gate for all GitHub features. One boolean: `Available`.

### Re-checking

- If a GitHub operation fails with an auth error, mark `Available = false` and surface the status change.
- Re-run `gh auth status` when explicitly triggered via a refresh action in the dashboard.

### API

Expose the GitHub status so the frontend can gate UI. Broadcast via the existing WebSocket dashboard channel so the frontend gets updates without polling.

---

## Phase 1: Foundation

### 1. Home Page GitHub Section

**Currently:** A "Pull Requests" section always renders, showing PRs from public GitHub repos via unauthenticated API calls. Rate-limited, public-only, and shows a confusing empty state for non-GitHub users.

**Change:**

Gate the entire section on `GitHubStatus.Available`.

**When `Available = false`:**
Show a compact status indicator in the section (same visual style as the current PR section header):

- "GitHub: not connected" with a brief note like "Install and authenticate the `gh` CLI to enable GitHub features."
- No PR list, no refresh button, no wasted space.

**When `Available = true`:**

- Show a small status line at the top: "Connected as @{username}" (subtle, not a banner).
- Below that, the PR list (same data for now, but retrieved via `gh` CLI per section 2).

### 2. Publish to GitHub (Local Repos)

**Currently:** Creating a local repo in schmux sets `URL: "local:reponame"` in config permanently. If the user pushes to GitHub by other means, schmux still treats it as local. The config is stuck.

**Change:**

When `GitHubStatus.Available` is true, local repo workspaces get a "Publish to GitHub" action on the session page.

**Flow:**

1. User clicks "Publish to GitHub" on the session page for a local repo workspace.
2. A small form appears:
   - **Repo name** (default: current repo name, editable)
   - **Visibility** (private/public toggle, default: private)
   - **Owner** (if the user belongs to orgs, show a dropdown; otherwise just their username)
3. Schmux runs:
   ```bash
   gh repo create {owner}/{name} --private --source={workspace_path} --push
   ```
4. On success:
   - Update the config entry: change `URL` from `local:reponame` to the new GitHub URL.
   - Set up the bare path for origin queries.
   - The next git check cycle picks up the remote and branch tracking starts working.
   - The workspace header updates to show GitHub links.

**Org list:** Use `gh api user/orgs --jq '.[].login'` to populate the owner dropdown. Cache this alongside the auth status.

**When `GitHubStatus.Available` is false:** Don't show the publish action. No tease, no dead button.

### 3. Git Polling for Local Repos

**Currently:** Periodic git checks run on all workspaces, including `local:` repos. Remote-related checks (ahead/behind, remote branch existence) fail because there's no origin.

**Change:**

In the git polling loop, skip remote-related checks for `local:` repos:

- Skip `git rev-list --left-right HEAD...origin/{defaultBranch}` (ahead/behind).
- Skip `RemoteBranchExists` check.
- Skip origin fetch operations.
- Still run local-only checks: dirty status, current branch, uncommitted changes.

This eliminates the error spam without changing the polling architecture.

### 4. Lore Auto-PR Gating

**Currently:** The lore system's `auto_pr` feature calls `gh pr create` directly. If `gh` isn't available, it logs a warning but the push still succeeds.

**Change:**

Gate `auto_pr` on `GitHubStatus.Available`:

- If available: current behavior (create PR after push).
- If not available: skip the PR creation silently. The config option can still be set, but it only activates when `gh` is available.
- In the lore settings UI: show `auto_pr` toggle as disabled with a note when GitHub is not connected.

### 5. GitHub Links on Workspace Header

**Currently:** `WorkspaceHeader.tsx` shows a branch link using `BuildGitBranchURL()` only when `remoteBranchExists` is true. The link goes to the branch tree view (e.g., `github.com/owner/repo/tree/branch`). No link for local-only branches.

**Change:**

When `GitHubStatus.Available` is true and the workspace repo is a GitHub URL:

**Branch link (existing, improve):**

- Keep the current branch link to the tree view when the remote branch exists.
- When the branch is local-only, don't link (current behavior is correct — nothing to link to).

**Repo link (new):**

- Add a small repo indicator that links to the repository on GitHub (e.g., `github.com/owner/repo`). This always works for GitHub repos regardless of branch state.

When `GitHubStatus.Available` is false, or the repo is not on GitHub, don't show GitHub links. The branch name still displays, just not as a link.

---

## Phase 2: PR Workflow (builds on Phase 1)

With `gh` auth established and the CLI as our interface, the PR workflow becomes straightforward. No more unauthenticated API calls, URL parsing, visibility checks, or rate limit handling.

### 6. PR Retrieval via `gh` CLI

**Currently:** Uses unauthenticated GitHub REST API (`GET /repos/{owner}/{repo}/pulls`). Only works for public repos. Rate limited to 60 requests/hour. Complex rate-limit retry logic.

**Change:**

Replace the entire `Discovery` system with `gh` CLI calls:

```bash
gh pr list --repo {owner}/{repo} --json number,title,body,state,headRefName,baseRefName,author,createdAt,url --limit 10
```

- Authenticated access — private repos work.
- No `CheckVisibility()`, no `publicRepos` tracking, no HTTP client, no rate-limit error parsing.
- Only runs when `GitHubStatus.Available` is true.

### 7. PR Association on Workspaces

For any workspace on a GitHub repo, check if its branch has an associated PR:

```bash
gh pr list --head {branch} --repo {owner}/{repo} --json number,url --limit 1
```

One CLI call. If a PR exists, store the number and URL on the workspace and display it:

- **Session page (workspace header):** `PR #{number}` as a link next to the branch name.
- **Home page (workspace card):** `PR #{number}` badge.

No URL parsing. No implicit branch name matching against cached data. Just ask `gh`.

### 8. PR Checkout Flow

**Currently:** Fetches `refs/pull/{number}/head` into the bare clone with hardcoded GitHub ref format. Requires `pr_review.target` or fails with a confusing error.

**Change:**

Use `gh pr checkout` or equivalent:

1. User clicks a PR on the home page.
2. Backend creates a workspace and checks out the PR branch.
3. Session launches with PR context (title, body, author, branch info).
4. If `pr_review.target` is configured, use it. If not, use the default agent. Don't block the checkout.

---

## What This Doesn't Cover

- **GitHub OAuth / web-based auth flow**: The existing `schmux auth github` CLI handles OAuth setup for the dashboard. That's decoupled — `gh auth status` uses the user's local `gh` CLI credentials.
- **GitLab / Bitbucket / other providers**: This spec is GitHub-specific via `gh` CLI. `BuildGitBranchURL` already supports multiple hosts for link generation — that stays.
- **GitHub Actions / CI integration**: Out of scope.
- **GitHub API key management**: `gh` handles its own auth. We just check if it works.
