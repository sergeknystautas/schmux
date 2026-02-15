# Commit Detail View Spec

## Context

The git history DAG (`/git/:workspaceId`) shows commit rows with hash, message, author, and timestamp. Clicking the hash copies it to clipboard. Users need to view full commit details including the diff.

## Goals

- Click commit hash → navigate to `/git/:workspaceId/:shortHash`
- Show commit header: author, date, full message
- Show diff (changes introduced by this commit)
- Keyboard navigation between file list and diff viewer
- Reuse `react-diff-viewer-continued` component

## Routes

- `/git/:workspaceId` - Commit graph (existing)
- `/git/:workspaceId/:shortHash` - Commit detail view (new, 7-char short hash)

## UI Layout

```
┌─────────────────────────────────────────────┐
│ WorkspaceHeader                             │
│ SessionTabs (Git tab active)                │
├─────────────────────────────────────────────┤
│ ← Graph  abc1234   Author • 2d ago          │
│ Full commit message (truncated to 3 lines)  │
│ [Show all X lines]                          │
├─────────────────────────────────────────────┤
│ Diff Viewer                                 │
│ ┌─────────────┬───────────────────────────┐ │
│ │ Changed     │ [focused border]          │ │
│ │ Files (N)   │                           │ │
│ │ file1.ts +10│ diff content              │ │
│ │ file2.css -2│                           │ │
│ │             │                           │ │
│ │ ↑↓ navigate │                           │ │
│ │ ←→ switch   │                           │ │
│ └─────────────┴───────────────────────────┘ │
└─────────────────────────────────────────────┘
```

## Visual Design

**Commit Header:**

- First row: "← Graph" link + hash (muted color, not blue)
- Second row: Author name + bullet separator + relative date (e.g., "2d ago")
- Merge commits: Show "Merge commit (diff vs first parent)" badge
- Message: Max 3 lines, "Show all X lines" button when truncated

**Diff Viewer:**

- Sidebar: file list with +/- line counts
- Main area: unified diff view (matches terminal `git diff`)
- Visual focus indicator: 2px accent border on focused column

**Keyboard Navigation:**

- `←` / `→`: Switch focus between file list and diff viewer
- `↑` / `↓` or `j` / `k`: Navigate files (when file list focused)
- `↑` / `↓`: Scroll diff (when diff viewer focused)
- Help text at bottom of file list: "↑↓ navigate ←→ switch"

## Interaction

- Hash becomes a `<Link>` (not a button that copies)
- Browser back returns to commit graph
- SessionTabs: Git tab stays active on `/git/:workspaceId/:shortHash`
- Full git-tab context: workspace header, keyboard shortcuts, tab cycling all work

## API

### Endpoint

`GET /api/workspaces/:workspaceId/git-commit/:shortHash`

Short hash (7 chars) in URL. Backend resolves to full hash internally.

### Response

```json
{
  "hash": "abc1234def567890...",
  "short_hash": "abc1234",
  "author_name": "John Doe",
  "author_email": "john@example.com",
  "timestamp": "2026-02-12T15:45:00-08:00",
  "message": "Add new feature\n\nThis is the commit body.",
  "parents": ["parenthash1", "parenthash2"],
  "is_merge": false,
  "files": [
    {
      "old_path": "src/file.ts",
      "new_path": "src/file.ts",
      "old_content": "...",
      "new_content": "...",
      "status": "modified",
      "lines_added": 10,
      "lines_removed": 2,
      "is_binary": false
    }
  ]
}
```

## Implementation Guide

### Key Decisions

**1. Short Hash in URL**

Use 7-char short hash in URL for readability. Backend resolves to full hash with `git rev-parse`.

**2. Hash Validation**

Two-step validation:

1. **Format check**: Regex `^[a-fA-F0-9]{4,40}$` (reject obvious garbage)
2. **Existence check**: `git cat-file -t <hash>` in workspace directory - must return "commit"

Returns 404 if hash doesn't exist in this repo (not just invalid format).

**3. Merge Commits**

- Diff against first parent only (standard `git show` behavior)
- API includes `is_merge: true` and full `parents` array
- UI shows badge: "Merge commit (diff vs first parent)"
- No combined diff in v1

**4. Reuse DiffPage Logic**

Follow existing `handleDiff` implementation in `internal/dashboard/handlers.go:1956-2155`:

- **Binary detection**: Use `difftool.IsBinaryFile()` (checks null bytes in first 8KB, falls back to git)
- **Content limit**: 1MB per file (see `getFileContent()` at handlers.go:2238-2256)
- **No file count limit**: Return all files
- **Line counts**: Parse from `git diff --numstat` output
- **Binary files**: `is_binary: true`, `old_content`/`new_content` empty

**5. Route Parsing**

Add a standardized helper for `/api/workspaces/{id}/...` routes:

```go
// internal/dashboard/path_utils.go
func parseWorkspaceSubpath(path, subpath string) (workspaceID, remainder string, ok bool) {
    afterWorkspaces := strings.TrimPrefix(path, "/api/workspaces/")
    return strings.Cut(afterWorkspaces, "/"+subpath+"/")
}
```

Use for this handler and consider migrating other handlers.

### Git Commands & Gotchas

**1. Resolving Short Hash**

```bash
git rev-parse abc1234
# Returns full 40-char hash, or error if not found
```

**2. Validating Hash Exists in Repo**

```bash
git cat-file -t abc1234
# Returns "commit" for valid commit, errors otherwise
```

**3. Timestamp Format**

Use `%aI` in git log format string for ISO 8601:

```bash
git log -1 --format="%aI" <hash>
# Output: 2026-02-12T15:45:00-08:00
```

**4. Getting Parent Commits**

```bash
git rev-parse abc1234^@
# Output: one hash per line, empty for root commits
```

**5. Root Commits Have No Parents**

`git diff-tree` returns empty output for root commits (no parent to diff against).

Use `git show --name-status` instead:

```bash
git show --name-status --format="" <hash>
# Works for all commits including root
```

For root commits, treat all files as "added" with no old content.

**6. Binary File Detection**

Use `git diff --numstat`:

```bash
git diff --numstat <parent>..<hash>
# Output:
# 10    2    src/file.ts        (normal file: added/removed/path)
# -     -    assets/image.png   (binary file: dash dash path)
```

**7. Renamed Files**

Status from git includes similarity percentage:

```
R100    old/path.ts    new/path.ts
```

Extract first character only to get status code: `R`, `A`, `D`, `M`, `C`.

**8. Line Counts**

Parse from `git diff --numstat` output:

```
added<tab>deleted<tab>filename
10       2            src/file.ts
```

### Security Validation

Commit hash comes from URL - must validate before passing to git commands.

**Validation function** (`validateCommitish`):

- Regex: `^[a-fA-F0-9]{4,40}$` (hex only, 4-40 chars)
- Block: empty string, `..`, `$`, backticks, `;`, `|`, `&`, spaces

Validate at both:

1. HTTP handler layer (early rejection, 400 Bad Request)
2. Workspace method (defense in depth)

Then verify hash exists in repo with `git cat-file -t` (404 if not found).

### File Structure

**Backend:**

- `internal/dashboard/path_utils.go` - Route parsing helper (new)
- `internal/api/contracts/git_commit.go` - Response struct
- `internal/dashboard/handlers.go` - Add route and handler
- `internal/workspace/git_commit.go` - `GetCommitDetail()` method
- `internal/workspace/git_commit_test.go` - Tests

**Frontend:**

- `assets/dashboard/src/App.tsx` - Add route
- `assets/dashboard/src/routes/GitCommitPage.tsx` - New component
- `assets/dashboard/src/lib/api.ts` - Add `getCommitDetail()` function
- `assets/dashboard/src/global.css` - Focus indicator styles

**Modify existing:**

- Commit graph component: change hash from copy-button to `<Link>`

### Test Cases

1. Basic commit with modified files
2. Root commit (no parents)
3. Renamed file (R100 status)
4. Deleted file
5. Binary file detection
6. Invalid hash format (400)
7. Valid format but nonexistent hash (404)
8. Hash from different repo (404)
9. ISO 8601 timestamp format
10. Accurate line counts
11. Merge commit (multiple parents, is_merge=true)
12. Large file (>1MB truncation)

## Out of Scope (v1)

- Remote workspace support (would need SSH command execution)
- Combined diff for merge commits (shows diff against first parent only)
- Porting keyboard navigation to existing DiffPage

## Acceptance Criteria

1. Clicking hash on commit graph navigates to detail page
2. Header shows author, relative date, merge badge if applicable
3. Message truncated to 3 lines with expand button
4. Diff viewer shows files with +/- line counts
5. Keyboard navigation works (←→ switch columns, ↑↓/jk navigate)
6. Visual focus indicators on active column
7. Browser back returns to commit graph
8. Invalid hash format shows 400 error
9. Nonexistent hash shows 404 error
10. Large files handled (1MB truncation per existing DiffPage logic)
11. Binary files show placeholder (not raw content)
12. Full git-tab context (SessionTabs, workspace header, shortcuts)
