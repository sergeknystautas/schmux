# Plan: Remote Sapling VCS Support — Phase 1 (Correctness Gates)

**Goal**: Unblock commit graph and fix diff correctness for remote Sapling workspaces
**Architecture**: Extend the existing `CommandBuilder` VCS abstraction with one new method (`NewestTimestamp`), replace `IsGitVCS` gates with `HasVCSSupport`, fix Sapling's `ShowFile` HEAD mapping, and add `--limit` safety to `LogRange`
**Tech Stack**: Go (backend), TypeScript/React (frontend), Vitest (frontend tests)
**Design Spec**: `docs/specs/2026-04-03-remote-sapling-vcs-support-final.md`

---

## Task Dependencies

| Group | Steps            | Can Parallelize                            | Notes                   |
| ----- | ---------------- | ------------------------------------------ | ----------------------- |
| 1     | Steps 1, 2, 3, 4 | Yes (independent files)                    | Core backend fixes      |
| 2     | Step 5           | No (depends on Step 1 for `HasVCSSupport`) | Handler + state wiring  |
| 3     | Step 6           | Yes (frontend, independent)                | Frontend default fix    |
| 4     | Step 7           | No (depends on all above)                  | End-to-end verification |

---

## Step 1: Add `HasVCSSupport` predicate and `parseRangeToRevset` helper

**Files**: `internal/workspace/vcs.go`, `internal/vcs/sapling.go`

### 1a. Write failing tests

**File**: `internal/workspace/vcs_test.go`

```go
func TestHasVCSSupport(t *testing.T) {
	tests := []struct {
		vcs  string
		want bool
	}{
		{"", true},
		{"git", true},
		{"git-worktree", true},
		{"git-clone", true},
		{"sapling", true},
		{"mercurial", false},
		{"svn", false},
	}
	for _, tt := range tests {
		if got := HasVCSSupport(tt.vcs); got != tt.want {
			t.Errorf("HasVCSSupport(%q) = %v, want %v", tt.vcs, got, tt.want)
		}
	}
}
```

**File**: `internal/vcs/vcs_test.go` — append test for `parseRangeToRevset`:

```go
func TestParseRangeToRevset(t *testing.T) {
	tests := []struct {
		input              string
		wantExclude, wantInclude string
	}{
		{"HEAD..origin/main", ".", "origin/main"},
		{"abc123..def456", "abc123", "def456"},
		{"notarange", "", "notarange"},
	}
	for _, tt := range tests {
		exc, inc := parseRangeToRevset(tt.input)
		if exc != tt.wantExclude || inc != tt.wantInclude {
			t.Errorf("parseRangeToRevset(%q) = (%q, %q), want (%q, %q)",
				tt.input, exc, inc, tt.wantExclude, tt.wantInclude)
		}
	}
}
```

### 1b. Run tests to verify they fail

```bash
go test ./internal/workspace/ -run TestHasVCSSupport -count=1
go test ./internal/vcs/ -run TestParseRangeToRevset -count=1
```

### 1c. Write implementation

**File**: `internal/workspace/vcs.go` — add after the existing `IsGitVCS` function (line 53):

```go
// HasVCSSupport returns true if the VCS type has diff and commit graph support.
// This is broader than IsGitVCS — it includes Sapling.
func HasVCSSupport(vcs string) bool {
	switch vcs {
	case "", "git", "git-worktree", "git-clone", "sapling":
		return true
	default:
		return false
	}
}
```

**File**: `internal/vcs/sapling.go` — add helper function and refactor `RevListCount` to use it. Add before the `SaplingCommandBuilder` struct (after the imports):

```go
// parseRangeToRevset converts git-style range notation "A..B" to Sapling revset
// operands (exclude, include). Returns ("", rangeSpec) if not in A..B format.
// Maps "HEAD" to "." (Sapling's working copy parent, equivalent to git HEAD).
func parseRangeToRevset(rangeSpec string) (exclude, include string) {
	parts := strings.Split(rangeSpec, "..")
	if len(parts) != 2 {
		return "", rangeSpec
	}
	exclude, include = parts[0], parts[1]
	if exclude == "HEAD" {
		exclude = "."
	}
	return exclude, include
}
```

Then refactor `RevListCount` (lines 93-107) to use the helper:

```go
func (s *SaplingCommandBuilder) RevListCount(rangeSpec string) string {
	exclude, include := parseRangeToRevset(rangeSpec)
	if exclude != "" {
		revset := fmt.Sprintf("only(%s, %s)", include, exclude)
		return fmt.Sprintf("sl log -T '.' -r %s | wc -l", shellutil.Quote(revset))
	}
	return fmt.Sprintf("sl log -T '.' -r %s | wc -l", shellutil.Quote(rangeSpec))
}
```

### 1d. Run tests to verify they pass

```bash
go test ./internal/workspace/ -run TestHasVCSSupport -count=1
go test ./internal/vcs/ -run TestParseRangeToRevset -count=1
go test ./internal/vcs/ -run TestSaplingRevListCount -count=1
```

---

## Step 2: Fix `ShowFile` HEAD mapping (BUG-2)

**File**: `internal/vcs/sapling.go`

### 2a. Write failing test

**File**: `internal/vcs/vcs_test.go` — append:

```go
func TestSaplingShowFile_HEADMapping(t *testing.T) {
	cb := &SaplingCommandBuilder{}
	got := cb.ShowFile("foo.txt", "HEAD")
	// In Sapling, "." is the working copy parent (equivalent to git HEAD).
	// ".^" would be the grandparent — one commit too far back.
	if !strings.Contains(got, "-r '.'") {
		t.Errorf("ShowFile(foo.txt, HEAD) = %q, want command with -r '.' (not '.^')", got)
	}
	if strings.Contains(got, ".^") {
		t.Errorf("ShowFile(foo.txt, HEAD) = %q, must NOT contain '.^'", got)
	}
}
```

### 2b. Run test to verify it fails

```bash
go test ./internal/vcs/ -run TestSaplingShowFile_HEADMapping -count=1
```

### 2c. Write implementation

**File**: `internal/vcs/sapling.go` — replace lines 17-22:

Old:

```go
func (s *SaplingCommandBuilder) ShowFile(path, revision string) string {
	// In sapling, .^ means "parent of working copy", equivalent to git's HEAD
	slRev := revision
	if revision == "HEAD" {
		slRev = ".^"
	}
	return fmt.Sprintf("sl cat -r %s %s", shellutil.Quote(slRev), shellutil.Quote(path))
}
```

New:

```go
func (s *SaplingCommandBuilder) ShowFile(path, revision string) string {
	// In Sapling, "." is the working copy parent — equivalent to git's HEAD.
	slRev := revision
	if revision == "HEAD" {
		slRev = "."
	}
	return fmt.Sprintf("sl cat -r %s %s", shellutil.Quote(slRev), shellutil.Quote(path))
}
```

### 2d. Run test to verify it passes

```bash
go test ./internal/vcs/ -run TestSaplingShowFile_HEADMapping -count=1
```

---

## Step 3: Add `NewestTimestamp` to `CommandBuilder` interface

**Files**: `internal/vcs/vcs.go`, `internal/vcs/git.go`, `internal/vcs/sapling.go`

### 3a. Write failing test

**File**: `internal/vcs/vcs_test.go` — append:

```go
func TestGitNewestTimestamp(t *testing.T) {
	cb := &GitCommandBuilder{}
	got := cb.NewestTimestamp("HEAD..origin/main")
	want := "git log --format=%aI -1 'HEAD..origin/main'"
	if got != want {
		t.Errorf("NewestTimestamp() = %q, want %q", got, want)
	}
}

func TestSaplingNewestTimestamp(t *testing.T) {
	cb := &SaplingCommandBuilder{}
	got := cb.NewestTimestamp("HEAD..origin/main")
	// Should convert A..B to "last(only(B, A))" revset with HEAD→"."
	if !strings.Contains(got, "last(only(origin/main, .))") {
		t.Errorf("NewestTimestamp(HEAD..origin/main) = %q, want revset with last(only(origin/main, .))", got)
	}
	if !strings.Contains(got, "--limit 1") {
		t.Errorf("NewestTimestamp() = %q, want --limit 1", got)
	}
}
```

### 3b. Run tests to verify they fail

```bash
go test ./internal/vcs/ -run TestGitNewestTimestamp -count=1
go test ./internal/vcs/ -run TestSaplingNewestTimestamp -count=1
```

### 3c. Write implementation

**File**: `internal/vcs/vcs.go` — add to the `CommandBuilder` interface, before the mutation commands section (before line 41):

```go
	// NewestTimestamp returns a command that outputs the ISO timestamp of the
	// newest commit in the given range (e.g., "HEAD..origin/main").
	NewestTimestamp(rangeSpec string) string
```

**File**: `internal/vcs/git.go` — add method:

```go
func (g *GitCommandBuilder) NewestTimestamp(rangeSpec string) string {
	return fmt.Sprintf("git log --format=%%aI -1 %s", shellutil.Quote(rangeSpec))
}
```

**File**: `internal/vcs/sapling.go` — add method:

```go
func (s *SaplingCommandBuilder) NewestTimestamp(rangeSpec string) string {
	exclude, include := parseRangeToRevset(rangeSpec)
	if exclude != "" {
		revset := fmt.Sprintf("last(only(%s, %s))", include, exclude)
		return fmt.Sprintf("sl log -T '{date|isodate}\\n' -r %s --limit 1", shellutil.Quote(revset))
	}
	return fmt.Sprintf("sl log -T '{date|isodate}\\n' -r %s --limit 1", shellutil.Quote(rangeSpec))
}
```

### 3d. Run tests to verify they pass

```bash
go test ./internal/vcs/ -run "TestGitNewestTimestamp|TestSaplingNewestTimestamp" -count=1
```

---

## Step 4: Add `--limit` to Sapling `LogRange` (BUG-8) and fix `GetDefaultBranch` (BUG-11)

**Files**: `internal/vcs/sapling.go`, `internal/workspace/vcs_sapling.go`

### 4a. Write failing test

**File**: `internal/vcs/vcs_test.go` — append:

```go
func TestSaplingLogRange_HasLimit(t *testing.T) {
	cb := &SaplingCommandBuilder{}
	got := cb.LogRange([]string{"HEAD"}, "abc123")
	if !strings.Contains(got, "--limit") {
		t.Errorf("LogRange() = %q, want --limit to prevent unbounded output", got)
	}
}
```

### 4b. Run test to verify it fails

```bash
go test ./internal/vcs/ -run TestSaplingLogRange_HasLimit -count=1
```

### 4c. Write implementation

**File**: `internal/vcs/sapling.go` — modify `LogRange` (line 64). Change the return statement from:

```go
	return fmt.Sprintf("sl log -T '{node}|{short(node)}|{desc|firstline}|{author|user}|{date|isodate}|{p1node} {p2node}\\n' -r %s", shellutil.Quote(revset))
```

to:

```go
	return fmt.Sprintf("sl log -T '{node}|{short(node)}|{desc|firstline}|{author|user}|{date|isodate}|{p1node} {p2node}\\n' -r %s --limit 5000", shellutil.Quote(revset))
```

**File**: `internal/workspace/vcs_sapling.go` — fix `GetDefaultBranch` (line 272-274). Replace:

```go
func (s *SaplingBackend) GetDefaultBranch(ctx context.Context, repoBasePath string) (string, error) {
	return "main", nil
}
```

with:

```go
func (s *SaplingBackend) GetDefaultBranch(ctx context.Context, repoBasePath string) (string, error) {
	output, err := s.manager.runCmd(ctx, "sl", "", RefreshTriggerExplicit, repoBasePath,
		"config", "remotenames.selectivepulldefault")
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return "main", nil // fallback
	}
	return strings.TrimSpace(string(output)), nil
}
```

### 4d. Run tests to verify they pass

```bash
go test ./internal/vcs/ -run TestSaplingLogRange_HasLimit -count=1
go test ./internal/workspace/ -count=1
```

---

## Step 5: Wire `HasVCSSupport` into handlers and state

**Files**: `internal/dashboard/handlers_git.go`, `internal/state/state.go`

### 5a. Write failing test

No new test file — verify via existing tests after modification. The key behavioral change: a workspace with `VCS: "sapling"` should NOT get a 400 error from the git-graph endpoint.

**File**: `internal/dashboard/handlers_git_test.go` — find or add a test for sapling workspace graph access. If no test file exists for this specific handler, add:

```go
func TestHandleWorkspaceGitGraph_SaplingVCSAllowed(t *testing.T) {
	// This test verifies that the IsGitVCS gate has been replaced with HasVCSSupport,
	// allowing sapling workspaces to access the git graph endpoint.
	// The test creates a workspace with VCS="sapling" and verifies the endpoint
	// does not return 400 "commit graph not available for this VCS type".
}
```

(The actual test depends on the test infrastructure in handlers_git_test.go — see step 5c for details.)

### 5b. Implementation

**File**: `internal/dashboard/handlers_git.go` — line 32, replace:

```go
	if !workspace.IsGitVCS(ws.VCS) {
		writeJSONError(w, "commit graph not available for this VCS type", http.StatusBadRequest)
		return
	}
```

with:

```go
	if !workspace.HasVCSSupport(ws.VCS) {
		writeJSONError(w, "commit graph not available for this VCS type", http.StatusBadRequest)
		return
	}
```

**File**: `internal/dashboard/handlers_git.go` — line 300, same replacement:

```go
	if !workspace.HasVCSSupport(ws.VCS) {
		writeJSONError(w, "commit detail not available for this VCS type", http.StatusBadRequest)
		return
	}
```

**File**: `internal/dashboard/handlers_git.go` — line 189, replace the hardcoded `git log` with `cb.NewestTimestamp`:

Old:

```go
		if out, err := conn.RunCommand(ctx, workdir, fmt.Sprintf("git log --format=%%aI -1 HEAD..%s", defaultBranchRef)); err == nil {
```

New:

```go
		if out, err := conn.RunCommand(ctx, workdir, cb.NewestTimestamp("HEAD.."+defaultBranchRef)); err == nil {
```

**File**: `internal/state/state.go` — line 390, replace:

```go
		if vcs == "" || vcs == "git" || vcs == "sapling" {
```

with:

```go
		if workspace.HasVCSSupport(vcs) {
```

(This requires adding an import for `workspace` package. Check if it's already imported — if circular, inline the same switch logic instead.)

**File**: `internal/state/state.go` — line 553, same replacement:

```go
		if workspace.HasVCSSupport(vcs) {
```

### 5c. Verify

```bash
go test ./internal/dashboard/ -run "GitGraph|GitCommit" -count=1
go test ./internal/state/ -run "TestLoadMigratesTabsForExistingWorkspaces|TestAddWorkspaceSeedsTabs" -count=1
```

Note: If `state` cannot import `workspace` due to circular dependency, use the same inline switch (`vcs == "" || vcs == "git" || vcs == "git-worktree" || vcs == "git-clone" || vcs == "sapling"`). This is less DRY but avoids the cycle. The spec acknowledges this trade-off.

---

## Step 6: Change frontend VCS default

**File**: `assets/dashboard/src/routes/RemoteSettingsPage.tsx`

### 6a. Implementation

Line 28 — change `vcs: 'sapling'` to `vcs: 'git'` in the `emptyForm` constant.

### 6b. Verify

```bash
./test.sh --quick
```

---

## Step 7: End-to-end verification

### 7a. Run full test suite

```bash
./test.sh
```

### 7b. Manual verification checklist

- [ ] `go test ./internal/vcs/ -count=1` — all VCS tests pass
- [ ] `go test ./internal/workspace/ -count=1` — all workspace tests pass
- [ ] `go test ./internal/dashboard/ -count=1` — all dashboard tests pass
- [ ] `go test ./internal/state/ -count=1` — all state tests pass
- [ ] `go run ./cmd/gen-types` — no type generation errors (if contracts changed)
- [ ] `go build ./cmd/schmux` — binary builds cleanly
- [ ] Grep for `.^` in `sapling.go` — should not appear (BUG-2 fixed)
- [ ] Grep for `IsGitVCS` in `handlers_git.go` — should not appear (replaced with `HasVCSSupport`)
- [ ] Grep for hardcoded `git log` in `handlers_git.go:189` area — should use `cb.NewestTimestamp`

### 7c. Information barrier check

- [ ] No internal hostnames, repo paths, or org-specific references in any changed file
- [ ] All Sapling commands use standard `sl` CLI
- [ ] Test fixtures use generic examples
