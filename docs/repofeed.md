# Repofeed

## What it does

Cross-developer intent federation — publishes what you're working on and surfaces what others are doing, organized by repository. Uses a git orphan branch (`dev-repofeed`) as the transport mechanism, requiring no new infrastructure beyond the shared git remote.

## Key files

| File                                                 | Purpose                                                     |
| ---------------------------------------------------- | ----------------------------------------------------------- |
| `internal/repofeed/types.go`                         | Core types: `DeveloperFile`, `Activity`, `ActivityStatus`   |
| `internal/repofeed/git.go`                           | Git plumbing: read/write orphan branch without working dirs |
| `internal/repofeed/publisher.go`                     | `EventHandler` impl — tracks sessions → activities          |
| `internal/repofeed/consumer.go`                      | Fetches remote dev files, merges, filters own email         |
| `internal/dashboard/handlers_repofeed.go`            | `GET /api/repofeed`, `GET /api/repofeed/{slug}`, broadcast  |
| `internal/daemon/daemon.go`                          | Wiring: creates publisher/consumer, starts fetch loop       |
| `cmd/schmux/repofeed.go`                             | CLI: `schmux repofeed [--repo] [--json]`                    |
| `assets/dashboard/src/routes/RepofeedPage.tsx`       | Dashboard page with repo tabs, filters, intent cards        |
| `assets/dashboard/src/routes/config/RepofeedTab.tsx` | Config tab: enable, timing, per-repo toggles                |

## Architecture decisions

- **Git as transport, not a service.** Intent data is pushed to a `dev-repofeed` orphan branch on the shared remote. Each developer owns one file (named by SHA256 of email). No two developers write the same file, so merge conflicts are structurally impossible.
- **Own intents filtered on the consumer side.** The consumer checks `OwnEmail` and excludes your own entries — you don't need to see your own activity fed back to you.
- **Git plumbing, not porcelain.** The `GitOps` struct uses `hash-object`, `update-index`, `write-tree`, `commit-tree`, `update-ref` to write directly to the orphan branch without ever touching a working directory or checkout.
- **Publisher as EventHandler.** The publisher implements the `events.EventHandler` interface and is registered in the daemon event pipeline. It listens for `status` events (spawn/completed) to track activity.
- **Subreddit data not yet merged.** The API response includes a `landed` array field for subreddit post data, but it is currently always empty. The infrastructure is in place for a future merge of intent + landed data into a unified feed.

## Gotchas

- **Temp index file must not exist.** `GitOps.WriteDevFile` creates a temp file for `GIT_INDEX_FILE`, then immediately deletes it so git can create a fresh index. If the 0-byte file exists, git errors with "index file smaller than expected".
- **`RepoSlug()` exists in three places with a known divergence.** `repofeed.RepoSlug()` (Go, exported), `dashboard.repoSlug()` (Go, unexported in handlers_subreddit.go), and `repoSlug()` (TypeScript, in RepofeedTab.tsx). The TypeScript version strips leading/trailing hyphens via `.replace(/^-|-$/g, '')` but the Go versions do not. This means they can produce different output for repo names starting or ending with special characters.
- **`TOTAL_STEPS` in ConfigPage.tsx.** When adding a new config tab, you must bump `TOTAL_STEPS` — it controls how many tab buttons are rendered. Missing this causes tabs after the new one to be invisible.
- **Privacy: nothing is auto-published.** Intent data stays in memory until the user explicitly pushes via `POST /api/repofeed/publish/push`. The preview endpoint (`GET /api/repofeed/publish/preview`) reads from memory — no data is written to git until the user approves. This prevents sensitive prompt text from leaking to the shared remote.
- **Push is mutex-guarded.** Only one push can run at a time. Concurrent requests return HTTP 409. The push endpoint re-checks `GetRepofeedEnabled()` at push time to prevent races with config changes.
- **Consumer has fetch backoff.** After 3 consecutive fetch failures for a repo, the consumer backs off exponentially (up to ~64 ticks) with ±25% jitter. This avoids hammering remotes where no one has published yet.
- **No LLM summarization yet.** The design spec calls for LLM-based prompt summarization for long prompts. The current publisher stores intents verbatim from event data.
- **`FetchFromRemote` returns an error on failure.** If the `dev-repofeed` branch doesn't exist on the remote, `FetchFromRemote` returns an error. The consumer uses backoff to reduce noise.

## Common modification patterns

- **To auto-publish (removing the approval gate):** Add a `startRepofeedPublisher` goroutine in `daemon.go` that calls `publisher.GetCurrentState()` on each tick, then `gitOps.WriteDevFile(email, file)` and `gitOps.PushToRemote("origin")` for each enabled repo. Gate behind a config flag so users can opt in.
- **To merge subreddit data into the landed field:** In `handleRepofeedRepo`, read subreddit posts from the subreddit cache directory and populate the `Landed` array in the response.
- **To add LLM summarization:** In `publisher.HandleEvent`, when the intent text exceeds ~100 chars, call `oneshot.ExecuteTarget()` with a summarization prompt before storing the intent.
- **To change timing defaults:** Edit the getter functions in `internal/config/config.go` (`GetRepofeedPublishInterval`, `GetRepofeedFetchInterval`, `GetRepofeedCompletedRetention`).
- **To add the sidebar unread badge:** In `ToolsSection.tsx`, add a `badge` prop to the Repofeed nav item, tracking new entries via localStorage (same pattern as lore/overlays badges).
