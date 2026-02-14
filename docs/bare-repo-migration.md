# Bare Repo Migration: URL-derived paths to tracked bare_path field

## Goal

Across schmux, all filesystem paths should use `Repo.Name` from config - consistent with how workspaces are named. Transition gracefully without forced directory moves.

## Current Inconsistency

**Workspaces (correct):**

```
workspaces/schmux-001          ← uses repo.name + numbering
workspaces/schmux-002
```

**Bare clones (wrong):**

```
repos/schmux.git               ← calculated from URL suffix each time
repos/stefanom/schmux.git      ← calculated from GitHub URL parsing each time
repos/beats-of-the-ancients.git ← calculated from URL suffix (typo in name)

query/schmux.git               ← calculated from URL suffix each time
query/stefanom/schmux.git      ← calculated from GitHub URL parsing each time
```

## Problem

- Workspaces already use `repo.name` ✓
- Bare clones recalculate paths from URLs every time ✗
- Multiple calculation methods (URL suffix, namespaced parsing) create inconsistency
- `extractRepoName`, `extractRepoPath`, and `legacyBareRepoPath` are URL-parsing hacks
- Can't transition to `repo.name`-based paths without breaking existing repos

## Solution: Add `bare_path` field to track calculated path

Store the calculated bare repo path in config as a hidden field. Calculate once, use forever.

```json
{
  "repos": [
    {
      "name": "schmux-sm",
      "url": "https://github.com/stefanom/schmux",
      "bare_path": "stefanom/schmux.git"  ← calculated once, stored
    },
    {
      "name": "schmux",
      "url": "https://github.com/sergeknystautas/schmux",
      "bare_path": "schmux.git"           ← calculated once, stored
    },
    {
      "name": "new-repo",
      "url": "https://github.com/user/new-repo",
      "bare_path": "new-repo.git"          ← new repos get {name}.git
    }
  ]
}
```

## Migration Scenarios

### Scenario 1 - Old flat paths (URL suffix)

Already flat, but calculated from URL end each time.

```
Current: Calculate "schmux.git" from URL each time
After:   Store "bare_path": "schmux.git" once
Action:  On config load, if bare_path empty, calculate and store
```

### Scenario 2 - Namespaced paths (GitHub URL parsing)

Created by `extractRepoPath()` parsing GitHub URLs for owner/repo structure.

```
Current: Calculate "stefanom/schmux.git" from URL each time
After:   Store "bare_path": "stefanom/schmux.git" once
Action:  On config load, if bare_path empty, calculate and store
```

### Scenario 3 - New repos

New repos get `{name}.git` immediately.

```
Current: Would calculate from URL (could be inconsistent)
After:   Set "bare_path": "{name}.git" on creation
Action:  When adding new repo, set bare_path = "{name}.git"
```

## Implementation Steps

### Phase 1 - Add field and populate

1. **Add `bare_path` field to `Repo` struct**

   ```go
   type Repo struct {
       Name     string `json:"name"`
       URL      string `json:"url"`
       BarePath string `json:"bare_path,omitempty"` // NEW - hidden field
   }
   ```

2. **Populate on config load**
   - If `bare_path` is empty, calculate using current logic
   - Store it in config
   - Use `extractRepoPath()` for existing repos
   - Use `{name}.git` for new repos

3. **Update code to use `bare_path`**
   - `ensureWorktreeBase()` - use `repo.BarePath` instead of calculating
   - `getQueryRepoPath()` - use `repo.BarePath` instead of calculating
   - Remove URL parsing from hot path

### Phase 2 - Remove URL parsing

Once all repos have `bare_path` populated:

1. **Remove calculation functions**
   - `extractRepoPath()` - no longer called
   - `legacyBareRepoPath()` - no longer called
   - `extractRepoName()` - no longer called (except tests)

2. **Simplify path logic**
   - Just use `repo.BarePath` directly
   - No more URL parsing in hot path

## Benefits

- **No directory moves** - existing repos keep working as-is
- **No forced migration** - users can migrate on their own time
- **Graceful transition** - old repos work, new repos use `repo.name`
- **Eliminates URL parsing** - once populated, no more calculation
- **Single source of truth** - `bare_path` is what we use, period

## Migration Logic

```go
// On config load, populate bare_path if empty
func (c *Config) populateBarePaths() {
    for i := range c.Repos {
        if c.Repos[i].BarePath == "" {
            // Calculate using current logic for existing repos
            c.Repos[i].BarePath = extractRepoPath(c.Repos[i].URL)
        }
    }
    c.Save() // Persist the calculated paths
}

// When adding new repo
func (c *Config) AddRepo(name, url string) error {
    repo := Repo{
        Name:     name,
        URL:      url,
        BarePath: name + ".git", // New repos get {name}.git
    }
    // ... add to config
}
```

## Code Changes Required

### Add to Repo struct

```go
type Repo struct {
    Name     string `json:"name"`
    URL      string `json:"url"`
    BarePath string `json:"bare_path,omitempty"` // NEW
}
```

### Update functions to use BarePath

- `ensureWorktreeBase()` - use `repo.BarePath`
- `getQueryRepoPath()` - use `repo.BarePath`
- Remove calls to `extractRepoPath()` and `legacyBareRepoPath()`

### Add population logic

- `populateBarePaths()` - calculate and store on config load
- Call during daemon startup or config load

## Testing Strategy

1. **Unit tests** - Test bare_path population logic
2. **Integration test** - Test with all three scenarios
3. **Manual test** - Run on real environment with existing repos
4. **Verify** - Existing repos keep working, new repos use `{name}.git`

## Rollback Plan

- `bare_path` is optional (omitempty)
- If migration fails, just delete the field
- Old code still works without it

## Future Cleanup

Once all repos have `bare_path` populated:

1. Remove `extractRepoPath()`
2. Remove `legacyBareRepoPath()`
3. Remove `extractRepoName()`
4. Remove tests for these functions

## Repo Name Validation

As part of this migration, add validation for repo names:

1. **Uniqueness** - No two repos can have the same name
2. **Filename-safe** - Reject names with `/`, `\`, `:`, or `..`
3. **Required** - Name cannot be empty

Add `validateRepos()` function (similar to `validateRunTargets()`) and call it during config validation.

```go
func validateRepos(repos []Repo) error {
    seen := make(map[string]struct{})
    for _, repo := range repos {
        name := strings.TrimSpace(repo.Name)
        if name == "" {
            return fmt.Errorf("repo name is required")
        }
        if strings.ContainsAny(name, "/\\:") {
            return fmt.Errorf("repo name %q contains invalid characters", name)
        }
        if strings.Contains(name, "..") {
            return fmt.Errorf("repo name %q contains directory traversal", name)
        }
        if _, exists := seen[name]; exists {
            return fmt.Errorf("duplicate repo name: %s", name)
        }
        seen[name] = struct{}{}
    }
    return nil
}
```

## Open Questions

- Should we validate `bare_path` is filename-safe?
- Should we prevent users from manually editing `bare_path`?
- Run population on daemon startup or on config save?

## Notes for the Implementing Agent

### Current State

**Functions to eventually remove:**

- `extractRepoPath()` in `internal/workspace/git.go` - does GitHub URL parsing for namespaced paths
- `legacyBareRepoPath()` in `internal/workspace/git.go` - detects old flat paths for backward compat
- `extractRepoName()` in `internal/workspace/git.go` - extracts repo name from URL

**Functions that call them:**

- `ensureWorktreeBase()` in `internal/workspace/worktree.go`
- `getQueryRepoPath()` in `internal/workspace/origin_queries.go`

### Test Environment

Sergek's computer has both migration scenarios for testing:

- Scenario 1 (old flat): `schmux.git`, `beats-of-the-ancients.git`, etc.
- Scenario 2 (namespaced): `stefanom/schmux.git`

**Note:** There's a typo - config has `beats-of-the-ancient` but disk has `beats-of-the-ancients.git`.

### TypeScript Types

- `internal/api/contracts/config.go` has `Repo` type - will need `BarePath` field added
- Run `go run ./cmd/gen-types` after modifying contracts to regenerate TypeScript

### Implementation Notes

- Add `bare_path` field as `omitempty` for backward compatibility
- Consider Phase 1 (add field) and Phase 2 (remove URL parsing) as separate PRs
- The migration should be idempotent - running it multiple times should be safe
