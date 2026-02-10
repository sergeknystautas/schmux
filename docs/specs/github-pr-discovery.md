# GitHub PR Discovery

## Overview

Discover GitHub pull requests for configured repos and let users create workspaces from them. PRs appear on the home page alongside remote branches. Clicking a PR creates a workspace and launches a session with PR context.

## Motivation

PRs contain context — title, description, author, branch info — that is lost when treating them as plain branches. This gives AI agents richer context than commit messages alone.

## Requirements

### 1. PR Discovery (daemon startup)

At daemon startup:

- Detect which `config.Repos` are GitHub repos by URL pattern (SSH or HTTPS).
- Check visibility via unauthenticated `GET /repos/{owner}/{repo}`. Public = 200 + `"private": false`. Private/missing = 404, skip. Errors = log and retry.
- For public repos, fetch open PRs via `GET /repos/{owner}/{repo}/pulls?state=open`. Limit to 5 PRs.
- Store public repo list and PR list in `state.json` to avoid repeat API calls against the rate limit.
- Re-run hourly.

### 2. Home Page

Below "Recent Branches", add a "Pull Requests" section showing:

- Repo name, PR number (links to GitHub), title, source → target branch, author, relative date.
- Refresh button that re-runs the daemon startup discovery logic.
- Loading and empty states.

### 3. Clicking a PR → Workspace Creation

This is a **workspace** operation (`internal/workspace/pull_request.go`), not the spawn flow.

- Workspace package creates a workspace from the PR ref: `git fetch origin refs/pull/{number}/head`, local branch `pr/{number}` or `pr/{fork-owner}/{number}`.
- Once the workspace is created, launch a session using `pr_review.target` with PR metadata as context (similar to how branch checkout provides commit messages, but with PR-specific information).
- Navigate to session detail page.

### 4. PR Context in Prompt

Similar to the commit message context provided when launching from a branch, but using PR metadata:

```
Pull Request #{Number}: {Title}
Repository: {RepoName}
Author: @{Author}
Branch: {SourceBranch} -> {TargetBranch}
URL: {HTMLURL}

{Body}

Please review this pull request. Consider code quality, correctness, test coverage, documentation, breaking changes, and performance implications.
```

## API

```
GET /api/prs
→ { prs: PullRequest[], last_fetched_at: string|null, error: string|null }

POST /api/prs/refresh
→ { prs: PullRequest[], fetched_count: number, error: string|null, retry_after_sec: number|null }

POST /api/prs/checkout
→ Creates workspace from PR ref, launches session with PR context.
  Request: { repo_url: string, pr_number: int }
  Response: { workspace_id: string, session_id: string }
```

### PullRequest

```json
{
  "number": 42,
  "title": "Add feature X",
  "body": "...",
  "state": "open",
  "repo_name": "schmux",
  "repo_url": "git@github.com:user/schmux.git",
  "source_branch": "feature-x",
  "target_branch": "main",
  "author": "someone",
  "created_at": "2025-01-15T...",
  "html_url": "https://github.com/user/schmux/pull/42",
  "fork_owner": "",
  "is_fork": false
}
```

## Config

```json
{
  "pr_review": {
    "target": "claude-code"
  }
}
```

Settings page gets a "PR Target" dropdown using the existing target list.

## State

Add to `State`: `pull_requests` (list of PullRequest) and `public_repos` (list of repo URL strings, re-checked every refresh). Use the single PullRequest type defined in contracts — no duplicate struct in state.

## Documentation

Update `docs/api.md` with the new endpoints in the implementation PR.

## Testing

Unit tests for the `internal/github/` package using httptest: URL parsing, visibility checks, PR response parsing, rate limit handling, prompt building.
