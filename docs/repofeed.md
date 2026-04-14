# Repofeed

## What it does

Cross-developer intent federation — publishes what you're working on and surfaces what others are doing. Uses a git orphan branch (`dev-repofeed`) as the transport mechanism, requiring no new infrastructure beyond the shared git remote.

## Key files

| File                                                 | Purpose                                                                    |
| ---------------------------------------------------- | -------------------------------------------------------------------------- |
| `internal/repofeed/types.go`                         | Core types: `DeveloperFile`, `Activity`, `Intent`, `ActivityStatus`        |
| `internal/repofeed/git.go`                           | Git plumbing: read/write orphan branch without working dirs                |
| `internal/repofeed/publisher.go`                     | `EventHandler` impl — tracks sessions → activities                         |
| `internal/repofeed/consumer.go`                      | Fetches remote dev files, merges, filters own email. Handles v1+v2 formats |
| `internal/repofeed/summaries.go`                     | LLM summary cache persistence (`~/.schmux/repofeed-summaries.json`)        |
| `internal/repofeed/dismissed.go`                     | Recipient-side dismissed intents (`~/.schmux/repofeed-dismissed.json`)     |
| `internal/dashboard/handlers_repofeed.go`            | API handlers: list, detail, preview, push, dismiss                         |
| `internal/dashboard/handlers_share_intent.go`        | `POST /api/workspaces/{id}/share-intent` toggle (on `WorkspaceHandlers`)   |
| `internal/daemon/daemon.go`                          | Wiring: publisher/consumer, publish goroutine with LLM summarization       |
| `cmd/schmux/repofeed.go`                             | CLI: `schmux repofeed [--repo] [--json]`                                   |
| `assets/dashboard/src/routes/RepofeedPage.tsx`       | Dashboard page with outgoing/incoming sections                             |
| `assets/dashboard/src/routes/config/RepofeedTab.tsx` | Config tab: enable, timing, per-repo toggles                               |

## Architecture decisions

- **Git as transport, not a service.** Intent data is pushed to a `dev-repofeed` orphan branch on the shared remote. Each developer owns one file (named by SHA256 of email). No two developers write the same file, so merge conflicts are structurally impossible.
- **Own intents filtered on the consumer side.** The consumer checks `OwnEmail` and excludes your own entries — you don't need to see your own activity fed back to you.
- **Git plumbing, not porcelain.** The `GitOps` struct uses `hash-object`, `update-index`, `write-tree`, `commit-tree`, `update-ref` to write directly to the orphan branch without ever touching a working directory or checkout.
- **Publisher as EventHandler.** The publisher implements the `events.EventHandler` interface and is registered in the daemon event pipeline. It listens for `status` events (spawn/completed) to track activity.
- **Workspace-level privacy.** Each workspace has an `IntentShared` flag (default: false). Only explicitly shared workspaces have their intent published. The flag is on `state.Workspace`, following the backburner pattern.
- **LLM intent summarization.** Instead of publishing raw session prompts, the system uses `oneshot.ExecuteTarget()` to generate a one-sentence summary (max 100 chars). Summaries are cached by prompt hash and rate-limited to at most once per hour per workspace.
- **v2 data format.** The `DeveloperFile` supports both v1 (repo-keyed activities) and v2 (flat workspace intents). The consumer handles both formats for backward compatibility during rollout.

## Gotchas

- **Temp index file must not exist.** `GitOps.WriteDevFile` creates a temp file for `GIT_INDEX_FILE`, then immediately deletes it so git can create a fresh index. If the 0-byte file exists, git errors with "index file smaller than expected".
- **`RepoSlug()` exists in three places with a known divergence.** `repofeed.RepoSlug()` (Go, exported), `dashboard.repoSlug()` (Go, unexported in handlers_subreddit.go), and `repoSlug()` (TypeScript, in RepofeedTab.tsx). The TypeScript version strips leading/trailing hyphens via `.replace(/^-|-$/g, '')` but the Go versions do not.
- **`TOTAL_STEPS` in ConfigPage.tsx.** When adding a new config tab, you must bump `TOTAL_STEPS`.
- **Privacy: private by default.** Workspaces are private until the user explicitly toggles sharing. The publish goroutine only includes workspaces with `IntentShared = true`.
- **Backburner → inactive.** Backburnered workspaces are published as `inactive` regardless of session state.
- **Push is mutex-guarded.** Only one push can run at a time. Concurrent requests return HTTP 409.
- **Consumer has fetch backoff.** After 3 consecutive fetch failures for a repo, the consumer backs off exponentially (up to ~64 ticks) with ±25% jitter.
- **Consumer reads files after fetch failure.** This is intentional — it serves last-known data from the local branch until the remote becomes available.
- **Dismissed entries are hashed.** The dismissed store uses SHA256 hashes of `developer:workspace_id` to avoid leaking identifiers in the local file.

## Common modification patterns

- **To disable auto-publish:** Remove the `startRepofeedPublisher` goroutine in `daemon.go`. The manual push endpoint (`POST /api/repofeed/publish/push`) can serve as a fallback.
- **To merge subreddit data into the landed field:** In `handleRepofeedRepo`, read subreddit posts from the subreddit cache directory and populate the `Landed` array in the response.
- **To change timing defaults:** Edit the getter functions in `internal/config/config.go` (`GetRepofeedPublishInterval`, `GetRepofeedFetchInterval`, `GetRepofeedCompletedRetention`).
- **To add the sidebar unread badge:** In `ToolsSection.tsx`, add a `badge` prop to the Repofeed nav item, tracking new entries via localStorage (same pattern as autolearn/overlays badges).
