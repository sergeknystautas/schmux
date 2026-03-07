# Design: Repo-Centric Subreddit with Post-Level Updates

## Summary

Restructure the subreddit feature from a single rolling digest across all repos into a per-repository, post-based news feed with intelligent create/update semantics and importance scoring.

## Current State

- Single monolithic digest across all repos, stored as one markdown blob in `~/.schmux/subreddit.json`
- Fixed 4-hour regeneration cycle with configurable lookback window (default 24 hours)
- LLM generates a conversational summary of all commits
- No per-repo breakdown, no post granularity, no importance scoring
- Config buried in Advanced tab (target + hours)

## Goals

1. **Repo-centric**: Each repository gets its own set of posts, viewable via tabs
2. **Post-based with updates**: Individual posts created per topic, updated when related changes arrive (not a monolithic digest)
3. **Importance scoring**: Each post gets an LLM-assigned upvote score (0-5, logarithmic) displayed Reddit-style
4. **Per-repo opt-in**: Users choose which repos generate subreddit posts
5. **Bootstrap**: New repos immediately populate with up to 14 days of history

## Data Model

### Post Schema

```json
{
  "id": "post-1709712000",
  "title": "Short headline",
  "content": "Markdown body",
  "upvotes": 3,
  "created_at": "2026-03-06T10:00:00Z",
  "updated_at": "2026-03-06T14:00:00Z",
  "revision": 1,
  "commits": [
    { "sha": "abc1234", "subject": "feat: add workspace switcher" },
    { "sha": "def5678", "subject": "fix: switcher not updating vite" }
  ]
}
```

- `id`: Unique identifier (timestamp-based or UUID)
- `title`: LLM-generated headline for scanning
- `content`: Markdown body in Reddit conversational voice
- `upvotes`: 0-5 importance score (logarithmic), LLM-assigned
- `created_at`: When post was first created
- `updated_at`: When post was last revised (equals `created_at` on first creation)
- `revision`: Starts at 1, increments on each update
- `commits`: All commit SHAs and subjects incorporated into this post (for dedup and LLM context)

### Per-Repo Storage

One JSON file per repo at `~/.schmux/subreddit/{repo-slug}.json`:

```json
{
  "repo": "schmux",
  "posts": [ ... ]
}
```

### Lifecycle Rules

- **Max posts per repo**: Configurable (default 30)
- **Max age**: Configurable (default 14 days) -- posts older than this are pruned
- **Update window**: Configurable (default 48 hours) -- posts newer than this are candidates for LLM-driven updates
- Cleanup runs each generation cycle

### Migration

On first run of the new system, delete the old `~/.schmux/subreddit.json` file.

## Generation Flow

### Periodic Check (default every 30 minutes)

1. For each enabled repo, fetch commits since the last known SHA
2. If no new commits, skip that repo
3. Load existing posts (filtered to update window as candidates)
4. Send to LLM: new commits + recent post titles/content summaries
5. LLM responds with action:
   - `"action": "create"` -- new post with title, content, upvotes
   - `"action": "update", "post_id": "..."` -- revised title, content, upvotes for an existing post
6. Write back to repo JSON file; increment revision if update
7. Broadcast change via WebSocket so dashboard updates live

### Bootstrap

When a repo has no post history (first run or newly added repo):

1. Look back the full max-age window (default 14 days) of commits
2. Send all commits to LLM with batch instructions
3. LLM groups them into distinct posts, each with title, content, upvotes
4. Write all posts to the repo's JSON file

### LLM Prompt Structure

**System prompt**: Same Reddit-voice tone as today, updated for structured output.

**User prompt for incremental generation**:

- New commits: `[sha_short] subject`
- Existing recent posts (candidates for update): `id`, `title`, brief content summary
- Instruction: Return JSON -- either create a new post or update an existing one

**User prompt for bootstrap**:

- All commits from the lookback window
- Instruction: Group into distinct posts, return JSON array

**Response schema (incremental)**:

```json
{
  "action": "create | update",
  "post_id": "only if action is update",
  "title": "short headline",
  "content": "markdown body",
  "upvotes": 0
}
```

**Response schema (bootstrap)**:

```json
[
  {
    "title": "headline",
    "content": "markdown body",
    "upvotes": 3,
    "commits": ["sha1", "sha2"]
  }
]
```

### Upvote Scale

0-5, logarithmic interpretation:

- **0**: Trivial (typo fix, dependency bump)
- **1**: Minor (small bug fix, minor refactor)
- **2**: Moderate (notable bug fix, small feature)
- **3**: Significant (new feature, important change)
- **4**: Major (large feature, architectural change)
- **5**: Landmark (major milestone, breaking change, v2.0)

## Configuration

### Config Struct

```go
type SubredditConfig struct {
    Target        string          `json:"target,omitempty"`         // LLM target, empty = disabled
    Interval      int             `json:"interval,omitempty"`       // polling interval in minutes, default 30
    CheckingRange int             `json:"checking_range,omitempty"` // update window in hours, default 48
    MaxPosts      int             `json:"max_posts,omitempty"`      // per-repo post cap, default 30
    MaxAge        int             `json:"max_age,omitempty"`        // post expiry in days, default 14
    Repos         map[string]bool `json:"repos,omitempty"`          // repo slug -> enabled, default true
}
```

Old `Hours` field is removed.

### Config UI

**Remove** subreddit settings from the Advanced tab.

**New "Subreddit" config tab** with:

1. **LLM Target** dropdown -- global on/off toggle (selecting "None" disables everything)
2. **Per-repo checkboxes** -- list of all configured repos with on/off toggles. Default ON. State persists even when global target is set to None (so toggling back preserves selections).
3. **Polling interval** -- minutes between checks (default 30)
4. **Checking range** -- hours that posts remain updatable (default 48)
5. **Max posts** -- per-repo cap (default 30)
6. **Max age** -- days before posts expire (default 14)

## Frontend

### Home Page -- r/schmux Card Redesign

- **Tabs** across the top, one per enabled repo (only repos that have posts)
- Each tab shows a scrollable list of posts, sorted by recency (most recently updated first)
- Each post displays:
  - **Upvote count** in Reddit-style (left side, vertical orientation)
  - **Title** (bold)
  - **Content** (markdown rendered)
  - **Footer**: "3h ago" or "Updated 2x . 1h ago" with a subtle visual indicator for updated posts
- Empty/disabled states handled gracefully

### Config Page -- New Subreddit Tab

As described in Configuration section above.

## API Changes

### GET /api/subreddit

Response changes from a single content blob to:

```json
{
  "enabled": true,
  "repos": [
    {
      "name": "schmux",
      "slug": "schmux",
      "posts": [
        {
          "id": "post-1709712000",
          "title": "New workspace switching",
          "content": "Markdown...",
          "upvotes": 4,
          "created_at": "2026-03-06T10:00:00Z",
          "updated_at": "2026-03-06T14:00:00Z",
          "revision": 3
        }
      ]
    }
  ]
}
```

Note: Commit details are omitted from the API response (they're internal bookkeeping). Posts are returned in recency order.

### WebSocket

Dashboard WebSocket broadcasts include subreddit update events so the UI updates live without polling.

### Config Endpoints

Updated to handle the new SubredditConfig shape (target, interval, checking_range, max_posts, max_age, repos map).

## What Stays the Same

- LLM target mechanism (oneshot)
- Reddit/conversational voice and tone
- Schema registration pattern
- WebSocket-driven dashboard updates
- Bare clone commit gathering infrastructure
