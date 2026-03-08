# Subreddit

## What it does

Generates a per-repository news feed of commit summaries written in a casual Reddit-style voice. Posts are created and updated incrementally as new commits arrive, with LLM-assigned importance scores (0-5 upvotes). Displayed on the home page with repo tabs.

## Key files

| File                                       | Purpose                                                                              |
| ------------------------------------------ | ------------------------------------------------------------------------------------ |
| `internal/subreddit/subreddit.go`          | `Generate()`, `BuildPrompt()`, `ParseResult()`, `GatherCommits()`, per-repo file I/O |
| `internal/subreddit/subreddit_test.go`     | Unit tests for prompt building, result parsing, cache staleness, cleanup             |
| `internal/dashboard/handlers_subreddit.go` | `GET /api/subreddit` endpoint, WebSocket broadcast on update                         |
| `assets/dashboard/src/routes/HomePage.tsx` | "r/schmux" card with repo tabs, post rendering, upvote display                       |

## Architecture decisions

- **Repo-centric, not monolithic.** Each repo gets its own JSON file at `~/.schmux/subreddit/{repo-slug}.json`. The API returns all repos in a single response. The old single-digest `~/.schmux/subreddit.json` is deleted on first run.
- **Post-based with create/update semantics.** The LLM decides per generation cycle whether to create a new post or update an existing one. Updated posts increment their `revision` counter. This avoids redundant posts when related commits land in sequence.
- **Importance scoring via upvotes.** Each post gets a 0-5 upvote score (logarithmic scale: 0=trivial, 3=significant, 5=landmark). LLM-assigned at creation and revisable on update.
- **Feature is opt-in via config.** `subreddit.target` must be set in config. Empty or missing disables generation. Per-repo opt-in via `subreddit.repos` map (default: all enabled).
- **Uses existing bare clones.** Commits are gathered from `~/.schmux/query/<bare_path>` -- the same infrastructure as `GetRecentBranches`.
- **LLM generation via the oneshot system.** Uses `oneshot.ExecuteTarget()` with a registered schema (`schema.LabelSubreddit`).
- **Incremental with dedup.** Commits already incorporated into existing posts are tracked via the `commits` field. Only new commits trigger generation.
- **Lifecycle cleanup.** Each generation cycle prunes posts older than `max_age` (default 14 days) and caps per-repo posts at `max_posts` (default 30).

## Gotchas

- **`ParseResult()` handles two response shapes.** Incremental responses are a single object with `action: "create"|"update"`. Bootstrap responses are an array of post objects. The parser strips markdown code fences before unmarshaling.
- **The checking range controls the update window, not the lookback.** `checking_range` (default 48 hours) determines how old a post can be and still be a candidate for LLM-driven updates. It is not the commit lookback window.
- **Bootstrap runs automatically for new repos.** When a repo has no post history, the first generation looks back the full `max_age` window (default 14 days) and asks the LLM to batch-create posts from all commits.
- **The `fullCfg` parameter to `Generate()` must be a `*config.Config`** (type-asserted at runtime) because `oneshot.ExecuteTarget` needs the full config for target resolution.
- **WebSocket updates are broadcast-based.** The handler calls `BroadcastSubreddit()` after writing new posts. The frontend re-fetches the full subreddit response on each broadcast -- it does not receive incremental post updates via WebSocket.
- **Upvote display uses star characters.** The frontend renders `★` repeated `upvotes` times, not the Reddit arrow style from the spec. Posts with 0 upvotes show no stars.
- **`repoSlug()` exists in multiple places.** The unexported `repoSlug()` in `handlers_subreddit.go` must produce identical output to `repofeed.RepoSlug()` (Go, exported) and the frontend's `repoSlug()` in `RepofeedTab.tsx`. Config repo toggles break if they diverge.

## Common modification patterns

- **To change the post tone or style:** Edit the prompt constants in `internal/subreddit/subreddit.go` (separate prompts for bootstrap and incremental).
- **To adjust generation timing:** The polling interval is `subreddit.interval` in config (default 30 minutes). The daemon loop in `daemon.go` respects this.
- **To change the API response shape:** Edit `internal/dashboard/handlers_subreddit.go` and update the contract type in `internal/api/contracts/`. Then run `go run ./cmd/gen-types`.
- **To change cleanup behavior:** Modify `cleanupPosts()` in `subreddit.go`. It's called after every generation cycle with `maxPosts` and `maxAge` from config.
- **To add per-repo config UI:** The config struct already has `subreddit.repos` (map of slug to enabled boolean). The frontend config page needs a checkbox list of repos.
