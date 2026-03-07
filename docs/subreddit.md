# Subreddit Digest

## What it does

The subreddit digest generates a casual, conversational summary of recent commits across all configured repos, written from the perspective of an enthusiastic user sharing news with fellow users. It runs hourly in the daemon's background loop and caches the result for the home page to display.

## Key files

| File                                       | Purpose                                                                               |
| ------------------------------------------ | ------------------------------------------------------------------------------------- |
| `internal/subreddit/subreddit.go`          | `Generate()`, `ParseResult()`, `BuildPrompt()`, `GatherCommits()`, `Cache` read/write |
| `internal/subreddit/subreddit_test.go`     | Unit tests for prompt building, result parsing, cache staleness                       |
| `internal/dashboard/handlers_subreddit.go` | `GET /api/subreddit` endpoint with `next_generation_at` field                         |
| `assets/dashboard/src/routes/HomePage.tsx` | "r/schmux" card at bottom of left column                                              |

## Architecture decisions

- **Feature is opt-in via config.** The subreddit target must be set in `config.json` (`subreddit.target`). Empty or missing means the feature is disabled -- the API returns `enabled: false` and no generation runs.
- **Uses existing bare clones.** Commits are gathered from `~/.schmux/query/<bare_path>` -- the same infrastructure as `GetRecentBranches`. No additional cloning or fetching is needed.
- **LLM generation via the oneshot system.** Uses `oneshot.ExecuteTarget()` with a registered schema (`schema.LabelSubreddit`). The schema is registered in an `init()` function.
- **Disk cache for resilience.** Results are cached to `~/.schmux/subreddit.json` with `generated_at`, `hours`, and `commit_count`. If the LLM call fails, the existing cache is kept and retried next hour.
- **Section title is "r/schmux."** The home page card shows the title, commit count and relative time right-aligned in the header, and the next scheduled generation time during loading state.

## Gotchas

- The `Result` struct has a single field (`Content`). The LLM response is expected to be JSON containing a `content` key. `ParseResult()` strips markdown code fences and searches for JSON object bounds before unmarshaling.
- The prompt explicitly tells the LLM not to use phrases like "this week" -- the lookback window is configurable via `subreddit.hours` (default 24).
- `GatherCommits()` runs `git log --since="N.hours.ago" --pretty=format:%s origin/<default-branch>` against each bare clone. If a bare clone is missing, that repo is skipped silently.
- The `Generate()` function accepts either a `GatherFunc` callback or a pre-built `[]CommitInfo` slice, but not both. If `gatherFunc` is non-nil, it takes precedence.
- The `fullCfg` parameter to `Generate()` must be a `*config.Config` (type-asserted at runtime) because `oneshot.ExecuteTarget` needs the full config for target resolution.

## Common modification patterns

- **To change the digest tone or style:** Edit the `Prompt` constant in `internal/subreddit/subreddit.go`.
- **To adjust the generation interval:** The hourly loop is in `daemon.go`. The lookback window is `subreddit.hours` in config.
- **To change the API response shape:** Edit `internal/dashboard/handlers_subreddit.go` and update the contract type in `internal/api/contracts/`. Then run `go run ./cmd/gen-types`.
- **To add a refresh button:** Currently no manual refresh exists. Add a `POST /api/subreddit/refresh` handler that calls `Generate()` on demand.
- **To change where the card appears on the home page:** Edit `assets/dashboard/src/routes/HomePage.tsx`. The card sits below the Pull Requests section in the left column.
