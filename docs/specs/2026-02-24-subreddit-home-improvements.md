# Home Page Subreddit Section Improvements

## Overview

Improve the "What's New" section on the home page with better naming, right-aligned timing, and visible scheduled generation times.

## Changes

### 1. Rename Section Title

- Change "What's New" → "r/schmux"
- Keep 📢 emoji

### 2. Right-Aligned Time Since Update

Move the meta info (commit count + relative time) to the right side of the section header:

```
[📢 r/schmux]                    [X commits · 37m ago]
```

### 3. Show Scheduled Generation Time During Loading

When no content exists yet, show when the next generation is scheduled:

```
Generating digest... (scheduled 2:45 PM)
```

## Backend Changes

### API Response

Add `next_generation_at` field to `SubredditResponse`:

```typescript
interface SubredditResponse {
  content?: string;
  generated_at?: string; // when last digest was created
  next_generation_at?: string; // NEW - when next digest is scheduled
  hours?: number;
  commit_count?: number;
  enabled: boolean;
}
```

### Implementation

1. Track next generation time in the daemon's hourly generator
2. Store in Server struct or subreddit cache
3. Expose via `/api/subreddit` endpoint

## Frontend Changes

### Title

```tsx
<h2 className={styles.sectionTitle}>
  <span style={{ fontSize: '1.1em' }}>📢</span>
  r/schmux
</h2>
```

### Right-Aligned Meta

```tsx
{
  subreddit.generated_at && (
    <span className={styles.subredditMeta}>
      {subreddit.commit_count ?? 0} commits · {formatRelativeDate(subreddit.generated_at)}
    </span>
  );
}
```

### Loading State

```tsx
{
  subreddit.content ? (
    <div className={styles.subredditContent}>{subreddit.content}</div>
  ) : (
    <div className={styles.subredditContent} style={{ opacity: 0.6, fontStyle: 'italic' }}>
      Generating digest... (scheduled {formatAbsoluteTime(subreddit.next_generation_at)})
    </div>
  );
}
```

### Helper Function

```tsx
function formatAbsoluteTime(isoDate: string): string {
  const date = new Date(isoDate);
  return date.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
}
```

## Files to Modify

- `internal/dashboard/handlers_subreddit.go` - add `next_generation_at` field
- `internal/daemon/daemon.go` - track and expose next generation time
- `assets/dashboard/src/lib/types.ts` - add field to `SubredditResponse`
- `assets/dashboard/src/routes/HomePage.tsx` - UI changes
- `assets/dashboard/src/styles/home.module.css` - styling for right-aligned meta
