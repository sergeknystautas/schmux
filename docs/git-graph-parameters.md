# Git Graph Parameters - Clear Semantics

## Problem

The current `max_commits` parameter conflates three distinct concerns:

1. **Main-ahead commits**: Commits on `origin/main` AFTER the fork point (what "sync summary" collapses)
2. **Local branch commits**: Commits on the current feature branch
3. **Main-before-base context**: Commits on `origin/main` BEFORE the fork point (historical context)

When there are 15+ main-ahead commits and we limit to 15 total, the local commits get cut off entirely.

## Solution: Separate Parameters

| Parameter        | Purpose | Default                                                     | Description |
| ---------------- | ------- | ----------------------------------------------------------- | ----------- |
| `max_total`      | 200     | Total commits to display (after applying individual limits) |
| `max_main_ahead` | 200     | Commits on main AFTER fork point (sync summary region)      |
| `max_local`      | 50      | Commits on local feature branch                             |
| `main_context`   | 15      | Commits on main BEFORE fork point (historical context)      |

## Algorithm

1. Fetch commits with limits:
   - Main-ahead commits: limited by `max_main_ahead` (200)
   - Local branch commits: limited by `max_local` (50)
   - Context commits: limited by `main_context` (15)

2. Build result from: local + main-ahead + context

3. If result < `min_display` (e.g., 20), expand `main_context` proportionally until satisfied

## Display Order

ISL sort order (heads first):

```
[sync summary: 15 commits]  <- main-ahead, collapsed
[you are here]             <- local branch head
local commit 1
local commit 2
[fork point]
context commit 1
context commit 2
...
```

## API Contract Change

### Before

```
GET /api/workspaces/{id}/git-graph?max_commits=200&context=5
```

### After

```
GET /api/workspaces/{id}/git-graph?max_total=200&main_context=5

Note: max_main_ahead and max_local are server-side defaults, not exposed to UI.
The UI only controls total commits and context size.
```
