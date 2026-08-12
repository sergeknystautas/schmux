# Git Features

## What it does

Provides real-time git status monitoring, a visual commit history DAG (modeled after Sapling ISL), per-commit detail/diff views, per-commit push (one CI build per commit), and GitHub PR discovery with one-click workspace creation from open pull requests.

## Key files

| File                                                   | Purpose                                                                                                 |
| ------------------------------------------------------ | ------------------------------------------------------------------------------------------------------- |
| `internal/workspace/git.go`                            | Git status polling, `UpdateVCSStatus`, default branch detection                                         |
| `internal/workspace/git_watcher.go`                    | fsnotify-based watcher for `.git` metadata; debounced refresh + broadcast                               |
| `internal/workspace/git_watcher_test.go`               | Watcher unit tests (resolve gitdir, debounce, worktree shared refs)                                     |
| `internal/workspace/git_graph.go`                      | `GetGitGraph` — fork-point detection, divergence-region scoping, ISL-style topo sort                    |
| `internal/workspace/git_graph_test.go`                 | Graph unit tests (ahead/behind, merge commits, trimming, max commits)                                   |
| `internal/workspace/git_commit.go`                     | `GetCommitDetail` — full commit metadata + file diffs for a single commit                               |
| `internal/workspace/git_commit_test.go`                | Commit detail tests (root commits, renames, binary detection, hash validation)                          |
| `internal/workspace/vcs_poll_round.go`                 | Per-sweep caches: deduplicates `git fetch` and `git worktree list` across workspaces                    |
| `internal/vcs/vcs.go`                                  | `CommandBuilder` interface — generates shell command strings for git/sapling operations                 |
| `internal/vcs/git.go`                                  | `GitCommandBuilder` — git-flavoured commands (uses `origin/<branch>` upstream refs)                     |
| `internal/vcs/sapling.go`                              | `SaplingCommandBuilder` — sapling-flavoured commands (uses `remote/<branch>` bookmarks)                 |
| `internal/workspace/giturl.go`                         | Git URL parsing (SSH/HTTPS normalization)                                                               |
| `internal/api/contracts/commit_graph.go`               | `CommitGraphResponse`, `CommitGraphNode`, `CommitGraphBranch`, `CommitGraphDirtyState`                  |
| `internal/api/contracts/commit_detail.go`              | `CommitDetailResponse`, `FileDiff`                                                                      |
| `internal/api/contracts/pr.go`                         | `PullRequest`, `PRsResponse`, `PRCheckoutRequest/Response`                                              |
| `internal/github/discovery.go`                         | `Discovery` — hourly PR polling, `Refresh`, `Seed` from cached state                                    |
| `internal/github/client.go`                            | `CheckVisibility`, `FetchOpenPRs` — unauthenticated GitHub API calls                                    |
| `internal/github/repo.go`                              | `ParseRepoURL`, `IsGitHubURL` — SSH/HTTPS pattern matching                                              |
| `internal/github/prompt.go`                            | `BuildReviewPrompt` — PR metadata formatted as agent context                                            |
| `internal/dashboard/handlers_git.go`                   | HTTP handlers: `handleWorkspaceCommitGraph`, `handleWorkspaceCommitDetail`, `handleStage`               |
| `internal/dashboard/handlers_pr.go`                    | HTTP handlers: `handlePRs`, `handlePRRefresh`, `handlePRCheckout`                                       |
| `assets/dashboard/src/lib/gitGraphLayout.ts`           | `computeLayout` — column assignment, virtual node insertion, lane lines                                 |
| `assets/dashboard/src/lib/gitGraphLayout.test.ts`      | Layout unit tests                                                                                       |
| `assets/dashboard/src/components/GitHistoryDAG.tsx`    | SVG rendering: column lines, edges, node circles, commit rows                                           |
| `assets/dashboard/src/routes/GitGraphPage.tsx`         | Route `/commits/:workspaceId` — workspace header + tabs + DAG                                           |
| `assets/dashboard/src/routes/GitCommitPage.tsx`        | Route `/commits/:workspaceId/:shortHash` — commit detail with diff viewer                               |
| `internal/workspace/push_commits.go`                   | `PushCommits` — validated, lock-guarded push of commits up to a chosen sha; bulk or one push per commit |
| `internal/workspace/push_commits_test.go`              | Bare-remote fixture tests: push counting via `post-receive` hooks, mid-loop rejection via `pre-receive` |
| `internal/api/contracts/push_commits.go`               | `PushCommitsResult` — response contract with machine-readable `reason` codes                            |
| `assets/dashboard/src/lib/commitReachability.ts`       | `reachableFrom` / `countUnpushed` — parent-walk reachability over loaded graph nodes                    |
| `assets/dashboard/src/components/PushCommitsModal.tsx` | Target (main/branch) + mode (bulk/per-commit) chooser; diverged force-confirm flow                      |

## Architecture decisions

- **ISL-style topo sort instead of date sort.** The backend performs a DFS topological sort with `sortAscCompare` tie-breaks (phase, date, hash), then reverses for rendering. This keeps children above ancestors and avoids misleading long edges that a date-only sort would create. The frontend does not re-sort.
- **Request-count scoping instead of full history.** The node list is bounded by the requested count (`max_total`), not by the fork point: `git log HEAD -n <count>` walks back through default-branch ancestry once the branch's own commits run out. `main_context` (default 5) is a floor, not a ceiling — it logs that many commits backwards from the fork point so the branch visibly detaches from main even when the HEAD log is short. When the log comes back full, `local_truncated` tells the client older commits exist. Main-ahead commits remain excluded from the node list and are represented as a count + summary row, keeping the payload small.
- **fsnotify watcher + slow poller fallback instead of pure polling.** The watcher gives sub-second updates when git metadata changes (commit, checkout, merge). The 10s poller remains for resilience if the watcher fails. Both call the same `updateGitStatusWithTrigger` path; last writer wins, no per-workspace mutex needed.
- **Watcher watches gitdir + logs/ but not refs/.** Watching `refs/` was too noisy (especially remote-tracking refs during `git fetch`). The poller handles ref changes at the 10s interval; the watcher targets fast local feedback for HEAD and index changes.
- **Suppression of self-triggered events.** When schmux runs its own git commands (e.g., `git fetch` during polling), it suppresses watcher events for those paths via `BeginInternalGitSuppressionForDir` with a 750ms grace period. This prevents a feedback loop where the poller's fetch triggers the watcher, which triggers another status check.
- **Unauthenticated GitHub API for PR discovery.** Only public repos are supported. Visibility is checked via `GET /repos/{owner}/{repo}` (public = 200 + `private: false`). This avoids OAuth token management. Rate limit errors return `retry_after_sec` to the frontend.
- **Short hash in commit detail URL.** The 7-char short hash is human-readable. The backend resolves to a full hash with `git rev-parse` and validates with `git cat-file -t` (defense in depth against path injection).
- **Commit diffs against first parent only.** For merge commits, `GetCommitDetail` diffs against `parents[0]`, matching standard `git show` behavior. The API sets `is_merge: true` so the frontend can display a badge.
- **Single graph color with highlight.** All lane lines and node strokes use `--color-text-muted`. Only the working-copy column (the column containing "you-are-here") uses `--color-graph-lane-1` for visual emphasis. No per-branch coloring, following ISL conventions.
- **`CommandBuilder.Log` (pipe) vs `CommandBuilder.LogParseable` (NUL).** Both methods exist deliberately. Local execution uses `LogParseable` — Git emits NUL-delimited fields so commit subjects containing `|` parse correctly. Remote execution (SSH via `internal/dashboard/handlers_vcs.go`) uses `Log` because tmux's `capture-pane -p` reads the terminal display buffer and silently drops non-printable bytes including NUL; pipe-delimited output survives. Sapling templates can't emit raw NULs, so its `LogParseable` aliases `Log`.
- **VCS-aware ref naming via `cb.DefaultBranchRef`.** `GetGitGraph` and the inspect/branches handlers compute the upstream ref through `cb.DefaultBranchRef(defaultBranch)`, not by string-concatenating `"origin/" + branch`. Git returns `origin/main`; Sapling returns `remote/main`. Hardcoding `origin/` made Sapling fall through to the no-divergence path (graph showing only the last few commits) without erroring — see the `e13eecce4` regression and its follow-up fix.

## Per-commit push

Every unpushed commit row gets a hover `Push…` button opening `PushCommitsModal`: pick a target (`origin/<default>`, fast-forward only, or `origin/<branch>`, `--force-with-lease`) and a mode (one bulk push, or one push per commit oldest→newest so CI systems that build per push produce one build per commit). Backend: `POST /api/workspaces/{id}/push-commits` → `workspace.PushCommits`. Full request/response contract, `reason` codes, and HTTP error mapping live in `docs/api.md`.

Safety model (each layer exists for a specific failure):

- **Hash validation is layered and server-side.** Regex (`^[0-9a-f]{40|64}$`) before any git invocation — flag-shaped or abbreviated input never reaches git; `cat-file -e <hash>^{commit}` — must be a commit object; `merge-base --is-ancestor <hash> HEAD` — must be on this branch (stale graphs after amend/uncommit → `409`). The per-commit loop pushes shas from `rev-list` output, never from the client.
- **Checkout must match the workspace's recorded branch** before anything is pushed — this catches detached HEAD and wrong-branch states. Note `git rev-parse --abbrev-ref HEAD` returns the literal string `"HEAD"` on a detached checkout _without erroring_, so the check is a name comparison, not an error check.
- **Workspace lock for the whole operation** (`LockWorkspace`) so an uncommit/amend can't rewrite history mid-loop. The older `LinearSyncToDefault`/`PushToBranch` paths don't take this lock; a racing top-button push is caught (non-fast-forward → `push_rejected`), not prevented.
- **`git fetch origin --prune`** — without prune, a branch deleted on the remote leaves a stale tracking ref that poisons ancestor checks and force-with-lease.
- **No origin remote:** a workspace whose repo was created locally (`local:` repo) has no `origin`. `PushCommits` returns `reason: "no_origin"` without attempting a fetch. The dashboard hides per-commit push buttons for these workspaces and offers the "Connect to GitHub" banner instead.
- **Fully-qualified `refs/heads/` push destinations** — never misresolved to a same-named tag.
- **Mid-loop failure is always a consistent state**: every completed push was a fast-forward, so the remote sits at the last successful commit and a retry recomputes the range from the remote's new position. The loop checks `ctx.Err()` between pushes.
- **Upstream retracking** (set upstream + ff-only realign, inherited from `LinearSyncToDefault`) runs only on a _full_ push to the default branch from a feature branch; partial pushes never touch tracking.
- **Default-branch guards**: on the default branch there is exactly one push semantics — fast-forward-only to `origin/<default>`. The UI hides "Push to branch" and the modal's branch target there; both `PushCommits` (`target:"branch"` → `reason:"unsupported"`) and `PushToBranch` reject it server-side regardless of `confirm`, closing every force-with-lease route to the default branch.
- **Diverged branch target** returns `needs_confirm` + the `diverged_commits` that a force push would overwrite; the UI shows a danger-styled confirm and retries with `confirm:true`. In a per-commit force push, only the first push rewrites remote history — the rest are fast-forwards, and the bare `--force-with-lease` lease stays fresh across iterations because each successful push updates the local remote-tracking ref.

Frontend eligibility and counts are reachability walks over the loaded graph (`commitReachability.ts`): a commit is pushable iff reachable from the local head and not reachable from `origin/<default>`. Two traps make the "on origin/<default>" set non-obvious — see the gotchas below.

## Gotchas

- **Worktree git dir resolution.** A worktree's `.git` is a file containing `gitdir: <path>`, not a directory. `resolveGitDir()` handles both cases. The watcher watches the worktree-specific gitdir and `logs/` but intentionally does NOT watch `refs/` (too noisy during fetches). The poller handles ref changes at the 10s interval.
- **Null-byte vs pipe delimiters in git log.** Local handlers call `cb.LogParseable` (NUL-delimited for Git, pipe for Sapling); remote handlers call `cb.Log` (always pipe — tmux strips NUL). `ParseGitLogOutput` auto-detects the delimiter per-line and tolerates either. Adding a new local commit-log query? Use `LogParseable`. Adding a new remote one? Use `Log`.
- **Don't hardcode `"origin/" + branch`.** Sapling has no `origin/<branch>` ref — its upstream bookmark is `remote/<branch>`. Always compute the upstream ref via `cb.DefaultBranchRef(defaultBranch)`. Hardcoding the prefix makes Sapling silently fall into the no-divergence branch with an empty graph and no error.
- **Sapling null hash filtering.** Sapling VCS uses `0000...0000` as a sentinel for absent parents. `ParseGitLogOutput` filters these out so they don't create phantom edges.
- **Commit hash validation is two-layer.** Format check (`^[a-fA-F0-9]{4,40}$` + forbidden characters) at the handler layer, existence check (`git cat-file -t`) at the workspace layer. Both are needed: format check rejects injection attempts early, existence check catches valid-format hashes from other repos.
- **Binary detection checks first 8KB for null bytes.** `getFileAtCommit` caps content at 1MB and scans the first 8KB for null bytes. If found, returns empty string. This matches the existing diff endpoint behavior in `handlers.go`.
- **Poll round caches are per-sweep.** `gitFetchPollRound` and `worktreeListCache` deduplicate `git fetch` and `git worktree list` across workspaces sharing the same bare clone within a single polling cycle. They are recreated each sweep to avoid stale data.
- **PR discovery stores results in `state.json`.** Cached PRs and public repo list persist across daemon restarts to avoid redundant API calls. The hourly ticker re-fetches. A manual refresh is available via `POST /api/prs/refresh`.
- **Column 0 lane line always extends to the top.** Even when no main-branch commit exists at the top rows, the column 0 dashed line runs alongside branch commits. This is the ISL column-reservation pattern providing visual continuity.
- **Disconnected graph reordering.** When `maxCommits` truncates the graph, the backend's ISL sort may place context commits before branch commits after reversal. The frontend detects this and reorders so the HEAD commit appears first.
- **`branches` map collapses on the default branch.** `BuildGraphResponse` keys the branches map by name; when the workspace is ON the default branch, the local-branch entry overwrites the default-branch entry, so `branches['main'].head` is the _local_ head, not origin/main. The unambiguous origin position is `remote_branch_head` (`origin/<localBranch>`, populated on every git workspace for exactly this reason).
- **Origin/main's head may not be in the loaded nodes.** When origin/<default> is ahead, its commits are excluded (collapsed into the "Pull from main" row) — a reachability walk from `branches['main'].head` returns an empty set and everything looks unpushed. The frontend falls back to the response's `fork_point`, the same boundary `BuildGraphResponse` uses for branch membership. Symptom when this regresses: the push modal counts the entire loaded graph ("Push 26 commits") and Push buttons appear on fork-point ancestors.
- **Per-commit branch counts are estimates when `remote_branch_head` is missing or outside the truncated window.** The modal labels them "up to N" (bounded by the count vs origin/<default>); the success toast reports actual backend counts. At N=1 the mode choice is a false choice (bulk ≡ per-commit) and the modal omits it.

## Common modification patterns

- **Add a new field to the git graph response:** Edit the Go struct in `internal/api/contracts/commit_graph.go`, populate it in `internal/workspace/git_graph.go` (either `GetGitGraph` or `BuildGraphResponse`), run `go run ./cmd/gen-types` to regenerate TypeScript types, then consume the field in `assets/dashboard/src/lib/gitGraphLayout.ts` or `assets/dashboard/src/components/GitHistoryDAG.tsx`.
- **Add a new virtual node type to the graph:** Add the type string to `LayoutNode.nodeType` in `gitGraphLayout.ts`, insert the node at the right position in `computeLayout`, add rendering logic in `GitHistoryDAG.tsx`, and add layout tests in `gitGraphLayout.test.ts`.
- **Change the git status polling interval:** The default is in `internal/config/config.go` (`git_status_poll_interval_ms`, default 10000). The config key is `sessions.git_status_poll_interval_ms`.
- **Change the watcher debounce window:** Config key `sessions.git_status_watch_debounce_ms` (default 1000). Accessed via `cfg.GitStatusWatchDebounce()`.
- **Add a new git API endpoint:** Register the route in `internal/dashboard/server.go` (under the `/api/workspaces/{workspaceID}` group), implement the handler in `internal/dashboard/handlers_git.go`, and implement the git logic in a new or existing method on `workspace.Manager`.
- **Support PR discovery for private repos:** Replace the unauthenticated `CheckVisibility` call in `internal/github/client.go` with an authenticated flow using the OAuth token from `internal/github/auth.go`. Update `FetchOpenPRs` to include the `Authorization` header.
- **Add a new commit detail field:** Edit `CommitDetailResponse` in `internal/api/contracts/commit_detail.go`, populate it in `internal/workspace/git_commit.go` (`GetCommitDetail`), run `go run ./cmd/gen-types`, and consume it in `assets/dashboard/src/routes/GitCommitPage.tsx`.
- **Add a new push rejection reason:** Add the constant in `internal/workspace/push_commits.go`, return it with a populated `Message` from the preflight, document it in `docs/api.md`, and map it to friendly copy in `useSync.handlePushCommits`. Test it with the bare-remote fixtures (`setupPushTest` + `m.setDefaultBranch(remoteDir, "main")` — default-branch detection doesn't work against local fixture remotes without seeding the cache).
