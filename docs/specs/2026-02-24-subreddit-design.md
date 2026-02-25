# Subreddit Digest Design

A "subreddit-style" digest that summarizes recent commits across all configured repos, written from the perspective of an enthusiastic user sharing news with fellow users.

## Overview

The subreddit digest generates a casual, conversational summary of changes that landed on `origin/<default-branch>` across all configured repos. It runs hourly in the daemon's background loop and caches the result for the home page to display.

## User Voice

The digest is written as if by an enthusiastic schmux user posting to a subreddit — not a developer explaining their work, but someone who uses the product daily and is excited to share what's new with peers.

Tone characteristics:

- Casual and conversational
- Focuses on what users would care about
- Light opinions acceptable ("finally fixed that", "solid win")
- No author attribution or implementation details
- Concise — a few paragraphs at most

## Configuration

Add to `config.json`:

```json
{
  "subreddit": {
    "target": "sonnet",
    "hours": 24
  }
}
```

- `target`: LLM target to use for generation. Empty or missing = feature disabled.
- `hours`: Lookback window for commits. Defaults to 24.

## Data Flow

```
1. Daemon background loop (hourly)
   ↓
2. For each configured repo:
   - Query bare clone at ~/.schmux/query/<bare_path>
   - git log --since="{hours} hours ago" origin/<default-branch>
   ↓
3. Aggregate commits, build prompt
   ↓
4. Call oneshot LLM with "subreddit" schema
   ↓
5. Cache result to ~/.schmux/subreddit.json
   ↓
6. Home page GET /api/subreddit serves cached content
```

## Package Structure

```
internal/subreddit/
├── subreddit.go      # Generate(), Result struct, schema registration
└── subreddit_test.go # Unit tests
```

## Result Schema

```go
type Result struct {
    Content string `json:"content" required:"true"`
}
```

Single field containing the full narrative.

## Cache File

`~/.schmux/subreddit.json`:

```json
{
  "content": "Big day for schmux — the diff viewer finally landed...",
  "generated_at": "2026-02-24T10:00:00Z",
  "hours": 24,
  "commit_count": 12
}
```

## API Endpoint

`GET /api/subreddit`

Response:

```json
{
  "content": "...",
  "generated_at": "2026-02-24T10:00:00Z",
  "hours": 24,
  "commit_count": 12,
  "enabled": true
}
```

Returns empty content with `enabled: false` if the feature is disabled.

## Prompt

```
You are an enthusiastic user of schmux, a multi-agent AI orchestration tool.

Write a casual subreddit-style post summarizing the recent changes. You're sharing
what's new with fellow users — not as a developer, but as someone who uses the
product daily and is excited about improvements.

Guidelines:
- Conversational tone, like talking to peers who also use the tool
- Focus on what users would care about: new features, quality-of-life fixes, bugs squashed
- Light opinions are fine ("finally fixed that annoying bug", "solid quality-of-life win")
- Don't name authors or get technical about implementation
- Keep it concise — a few paragraphs at most
- If there are no commits, write a brief "quiet period" message

Here are the commits from the last {hours} hours:

{COMMITS}
```

## UI Placement

Bottom of left column on `HomePage.tsx`, below Pull Requests section.

- Wide card format matching existing section cards
- Shows cached content directly
- Subtle timestamp showing when digest was generated
- No refresh button — relies on hourly daemon refresh

## Commit Gathering

Uses existing bare clones in `~/.schmux/query/` (same infrastructure as `GetRecentBranches`):

```bash
git log --since="{hours} hours ago" \
        --format="%s" \
        origin/<default-branch>
```

Iterate over all configured repos, aggregate commit subjects, pass to LLM.

## Error Handling

- If target not configured: feature disabled, no generation, API returns `enabled: false`
- If LLM call fails: log warning, keep existing cache, retry next hour
- If no commits in window: still generate, LLM writes "quiet period" message
- If bare clone missing: skip that repo, continue with others

## Integration Points

1. **Config** (`internal/config/`): Add `subreddit` config struct
2. **Schema** (`internal/schema/`): Register `LabelSubreddit = "subreddit"`
3. **Daemon** (`internal/daemon/`): Add hourly generation to background loop
4. **Dashboard** (`internal/dashboard/`): Add `/api/subreddit` endpoint
5. **HomePage** (`assets/dashboard/src/routes/HomePage.tsx`): Add subreddit card below PRs
