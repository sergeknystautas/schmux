# Git Features

## What it does

Provides real-time git status monitoring, a visual commit history DAG (modeled after Sapling ISL), per-commit detail/diff views, and GitHub PR discovery with one-click workspace creation from open pull requests.

## Key files

| File                                                | Purpose                                                                                   |
| --------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| `internal/workspace/git.go`                         | Git status polling, `UpdateVCSStatus`, default branch detection                           |
| `internal/workspace/git_watcher.go`                 | fsnotify-based watcher for `.git` metadata; debounced refresh + broadcast                 |
| `internal/workspace/git_watcher_test.go`            | Watcher unit tests (resolve gitdir, debounce, worktree shared refs)                       |
| `internal/workspace/git_graph.go`                   | `GetGitGraph` — fork-point detection, divergence-region scoping, ISL-style topo sort      |
| `internal/workspace/git_graph_test.go`              | Graph unit tests (ahead/behind, merge commits, trimming, max commits)                     |
| `internal/workspace/git_commit.go`                  | `GetCommitDetail` — full commit metadata + file diffs for a single commit                 |
| `internal/workspace/git_commit_test.go`             | Commit detail tests (root commits, renames, binary detection, hash validation)            |
| `internal/workspace/vcs_poll_round.go`              | Per-sweep caches: deduplicates `git fetch` and `git worktree list` across workspaces      |
| `internal/workspace/giturl.go`                      | Git URL parsing (SSH/HTTPS normalization)                                                 |
| `internal/api/contracts/git_graph.go`               | `GitGraphResponse`, `GitGraphNode`, `GitGraphBranch`, `GitGraphDirtyState`                |
| `internal/api/contracts/git_commit.go`              | `GitCommitDetailResponse`, `FileDiff`                                                     |
| `internal/api/contracts/pr.go`                      | `PullRequest`, `PRsResponse`, `PRCheckoutRequest/Response`                                |
| `internal/github/discovery.go`                      | `Discovery` — hourly PR polling, `Refresh`, `Seed` from cached state                      |
| `internal/github/client.go`                         | `CheckVisibility`, `FetchOpenPRs` — unauthenticated GitHub API calls                      |
| `internal/github/repo.go`                           | `ParseRepoURL`, `IsGitHubURL` — SSH/HTTPS pattern matching                                |
| `internal/github/prompt.go`                         | `BuildReviewPrompt` — PR metadata formatted as agent context                              |
| `internal/dashboard/handlers_git.go`                | HTTP handlers: `handleWorkspaceCommitGraph`, `handleWorkspaceCommitDetail`, `handleStage` |
| `internal/dashboard/handlers_pr.go`                 | HTTP handlers: `handlePRs`, `handlePRRefresh`, `handlePRCheckout`                         |
| `assets/dashboard/src/lib/gitGraphLayout.ts`        | `computeLayout` — column assignment, virtual node insertion, lane lines                   |
| `assets/dashboard/src/lib/gitGraphLayout.test.ts`   | Layout unit tests                                                                         |
| `assets/dashboard/src/components/GitHistoryDAG.tsx` | SVG rendering: column lines, edges, node circles, commit rows                             |
| `assets/dashboard/src/routes/GitGraphPage.tsx`      | Route `/commits/:workspaceId` — workspace header + tabs + DAG                             |
| `assets/dashboard/src/routes/GitCommitPage.tsx`     | Route `/commits/:workspaceId/:shortHash` — commit detail with diff viewer                 |

## Architecture decisions

- **ISL-style topo sort instead of date sort.** The backend performs a DFS topological sort with `sortAscCompare` tie-breaks (phase, date, hash), then reverses for rendering. This keeps children above ancestors and avoids misleading long edges that a date-only sort would create. The frontend does not re-sort.
- **Divergence-region scoping instead of full history.** The graph only includes commits between the fork point and branch tips, plus a small context window below the fork point (default 5). Main-ahead commits are excluded from the node list and represented as a count + summary row, keeping the payload small.
- **fsnotify watcher + slow poller fallback instead of pure polling.** The watcher gives sub-second updates when git metadata changes (commit, checkout, merge). The 10s poller remains for resilience if the watcher fails. Both call the same `updateGitStatusWithTrigger` path; last writer wins, no per-workspace mutex needed.
- **Watcher watches gitdir + logs/ but not refs/.** Watching `refs/` was too noisy (especially remote-tracking refs during `git fetch`). The poller handles ref changes at the 10s interval; the watcher targets fast local feedback for HEAD and index changes.
- **Suppression of self-triggered events.** When schmux runs its own git commands (e.g., `git fetch` during polling), it suppresses watcher events for those paths via `BeginInternalGitSuppressionForDir` with a 750ms grace period. This prevents a feedback loop where the poller's fetch triggers the watcher, which triggers another status check.
- **Unauthenticated GitHub API for PR discovery.** Only public repos are supported. Visibility is checked via `GET /repos/{owner}/{repo}` (public = 200 + `private: false`). This avoids OAuth token management. Rate limit errors return `retry_after_sec` to the frontend.
- **Short hash in commit detail URL.** The 7-char short hash is human-readable. The backend resolves to a full hash with `git rev-parse` and validates with `git cat-file -t` (defense in depth against path injection).
- **Commit diffs against first parent only.** For merge commits, `GetCommitDetail` diffs against `parents[0]`, matching standard `git show` behavior. The API sets `is_merge: true` so the frontend can display a badge.
- **Single graph color with highlight.** All lane lines and node strokes use `--color-text-muted`. Only the working-copy column (the column containing "you-are-here") uses `--color-graph-lane-1` for visual emphasis. No per-branch coloring, following ISL conventions.

## Gotchas

- **Worktree git dir resolution.** A worktree's `.git` is a file containing `gitdir: <path>`, not a directory. `resolveGitDir()` handles both cases. The watcher watches the worktree-specific gitdir and `logs/` but intentionally does NOT watch `refs/` (too noisy during fetches). The poller handles ref changes at the 10s interval.
- **Null-byte vs pipe delimiters in git log.** The local handler uses `%x00` (null-byte) field delimiters to avoid pipe collisions in commit messages. The remote handler uses `|` (pipe) for shell compatibility. `ParseGitLogOutput` auto-detects the delimiter.
- **Sapling null hash filtering.** Sapling VCS uses `0000...0000` as a sentinel for absent parents. `ParseGitLogOutput` filters these out so they don't create phantom edges.
- **Commit hash validation is two-layer.** Format check (`^[a-fA-F0-9]{4,40}$` + forbidden characters) at the handler layer, existence check (`git cat-file -t`) at the workspace layer. Both are needed: format check rejects injection attempts early, existence check catches valid-format hashes from other repos.
- **Binary detection checks first 8KB for null bytes.** `getFileAtCommit` caps content at 1MB and scans the first 8KB for null bytes. If found, returns empty string. This matches the existing diff endpoint behavior in `handlers.go`.
- **Poll round caches are per-sweep.** `gitFetchPollRound` and `worktreeListCache` deduplicate `git fetch` and `git worktree list` across workspaces sharing the same bare clone within a single polling cycle. They are recreated each sweep to avoid stale data.
- **PR discovery stores results in `state.json`.** Cached PRs and public repo list persist across daemon restarts to avoid redundant API calls. The hourly ticker re-fetches. A manual refresh is available via `POST /api/prs/refresh`.
- **Column 0 lane line always extends to the top.** Even when no main-branch commit exists at the top rows, the column 0 dashed line runs alongside branch commits. This is the ISL column-reservation pattern providing visual continuity.
- **Disconnected graph reordering.** When `maxCommits` truncates the graph, the backend's ISL sort may place context commits before branch commits after reversal. The frontend detects this and reorders so the HEAD commit appears first.

## Common modification patterns

- **Add a new field to the git graph response:** Edit the Go struct in `internal/api/contracts/git_graph.go`, populate it in `internal/workspace/git_graph.go` (either `GetGitGraph` or `BuildGraphResponse`), run `go run ./cmd/gen-types` to regenerate TypeScript types, then consume the field in `assets/dashboard/src/lib/gitGraphLayout.ts` or `assets/dashboard/src/components/GitHistoryDAG.tsx`.
- **Add a new virtual node type to the graph:** Add the type string to `LayoutNode.nodeType` in `gitGraphLayout.ts`, insert the node at the right position in `computeLayout`, add rendering logic in `GitHistoryDAG.tsx`, and add layout tests in `gitGraphLayout.test.ts`.
- **Change the git status polling interval:** The default is in `internal/config/config.go` (`git_status_poll_interval_ms`, default 10000). The config key is `sessions.git_status_poll_interval_ms`.
- **Change the watcher debounce window:** Config key `sessions.git_status_watch_debounce_ms` (default 1000). Accessed via `cfg.GitStatusWatchDebounce()`.
- **Add a new git API endpoint:** Register the route in `internal/dashboard/server.go` (under the `/api/workspaces/{workspaceID}` group), implement the handler in `internal/dashboard/handlers_git.go`, and implement the git logic in a new or existing method on `workspace.Manager`.
- **Support PR discovery for private repos:** Replace the unauthenticated `CheckVisibility` call in `internal/github/client.go` with an authenticated flow using the OAuth token from `internal/github/auth.go`. Update `FetchOpenPRs` to include the `Authorization` header.
- **Add a new commit detail field:** Edit `GitCommitDetailResponse` in `internal/api/contracts/git_commit.go`, populate it in `internal/workspace/git_commit.go` (`GetCommitDetail`), run `go run ./cmd/gen-types`, and consume it in `assets/dashboard/src/routes/GitCommitPage.tsx`.
