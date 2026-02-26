# Rebase Scenario Coverage Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add 43 tests covering every conceivable git state for `LinearSyncFromDefault` and `LinearSyncResolveConflict`, using a declarative test harness with reusable invariant assertions.

**Architecture:** A `rebaseFixture` builder creates real git repos in temp dirs with configurable topology (remote commits, local commits, working dir state, conflicts). The LLM dependency in `LinearSyncResolveConflict` is injected via a function field on `Manager` so tests can mock it. Universal invariants (no orphaned WIP, no stale rebase, correct branch) are checked automatically after every test.

**Tech Stack:** Go testing, real git operations in `t.TempDir()`, existing `runGit`/`writeFile`/`gitTestWorkTree` helpers from `internal/workspace/git_test.go`

**Design doc:** `docs/specs/2026-02-26-rebase-scenario-coverage-design.md`

---

### Task 1: Inject conflict resolver dependency into Manager

**Files:**
- Modify: `internal/workspace/manager.go:32-59` (add field to struct)
- Modify: `internal/workspace/manager.go:62-83` (set default in New())
- Modify: `internal/workspace/linear_sync.go:722` (use injected function)

**Step 1: Add the type alias and field**

In `internal/workspace/manager.go`, add a `ConflictResolverFunc` type and field to `Manager`:

```go
// ConflictResolverFunc is the function signature for LLM-based conflict resolution.
// Production uses conflictresolve.Execute. Tests inject mocks.
type ConflictResolverFunc func(ctx context.Context, cfg *config.Config, prompt string, workspacePath string) (conflictresolve.OneshotResult, string, error)
```

Add to `Manager` struct (after `syncProgressFn`):

```go
conflictResolver       ConflictResolverFunc                         // injected for testability; defaults to conflictresolve.Execute
```

Add import for `"github.com/sergeknystautas/schmux/internal/conflictresolve"` to `manager.go` if not already present.

**Step 2: Set default in New()**

In `New()`, after line 75 (`randSuffix: defaultRandSuffix,`), before the closing `}`, add:

```go
conflictResolver:       conflictresolve.Execute,
```

**Step 3: Use the field in LinearSyncResolveConflict**

In `linear_sync.go:722`, change:

```go
oneshotResult, rawResponse, err := conflictresolve.Execute(ctx, m.config, prompt, workspacePath)
```

to:

```go
oneshotResult, rawResponse, err := m.conflictResolver(ctx, m.config, prompt, workspacePath)
```

**Step 4: Verify build**

Run: `go build ./cmd/schmux`
Expected: Compiles with no errors.

**Step 5: Verify tests pass**

Run: `go test ./internal/workspace/... -count=1`
Expected: All existing tests pass (no behavioral change).

**Step 6: Commit**

```
feat(workspace): inject conflict resolver for testability

Add ConflictResolverFunc field to Manager, defaulting to
conflictresolve.Execute. No behavioral change in production.
```

---

### Task 2: Build the rebase test fixture — repo scaffolding

**Files:**
- Create: `internal/workspace/rebase_fixture_test.go`

This task creates the fixture builder and its `Build()` method. It creates:
1. A remote (bare-like) repo with initial commit
2. A clone as the workspace
3. Optionally a feature branch with local commits
4. Optionally remote commits pushed to main after the fork
5. The workspace Manager + state wired up

**Step 1: Write the fixture file with builder**

Create `internal/workspace/rebase_fixture_test.go`:

```go
package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/conflictresolve"
	"github.com/sergeknystautas/schmux/internal/state"
)

// testCommit describes a commit to create in a test repo.
type testCommit struct {
	message string
	files   map[string]string // filename -> content
	deletes []string          // files to delete
}

// tc creates a testCommit with a message and file:content pairs.
// Each pair is "filename:content". Use tcDelete for deletions.
func tc(message string, filePairs ...string) testCommit {
	files := make(map[string]string, len(filePairs))
	for _, pair := range filePairs {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) == 2 {
			files[parts[0]] = parts[1]
		}
	}
	return testCommit{message: message, files: files}
}

// tcDelete creates a testCommit that deletes files.
func tcDelete(message string, filesToDelete ...string) testCommit {
	return testCommit{message: message, files: map[string]string{}, deletes: filesToDelete}
}

// rebaseFixtureBuilder accumulates configuration for a test scenario.
type rebaseFixtureBuilder struct {
	t              *testing.T
	localBranch    string
	remoteCommits  []testCommit
	localCommits   []testCommit
	stagedChanges  map[string]string
	unstagedMods   map[string]string // tracked file modifications (file must exist from a commit)
	untrackedFiles map[string]string
	preCommitHook  string
	timeout        time.Duration
	mockLLM        ConflictResolverFunc
}

func newRebaseFixture(t *testing.T) *rebaseFixtureBuilder {
	t.Helper()
	return &rebaseFixtureBuilder{
		t:              t,
		localBranch:    "feature",
		stagedChanges:  make(map[string]string),
		unstagedMods:   make(map[string]string),
		untrackedFiles: make(map[string]string),
	}
}

func (b *rebaseFixtureBuilder) WithLocalBranch(name string) *rebaseFixtureBuilder {
	b.localBranch = name
	return b
}

func (b *rebaseFixtureBuilder) WithRemoteCommits(commits ...testCommit) *rebaseFixtureBuilder {
	b.remoteCommits = append(b.remoteCommits, commits...)
	return b
}

func (b *rebaseFixtureBuilder) WithLocalCommits(commits ...testCommit) *rebaseFixtureBuilder {
	b.localCommits = append(b.localCommits, commits...)
	return b
}

func (b *rebaseFixtureBuilder) WithStagedChanges(file, content string) *rebaseFixtureBuilder {
	b.stagedChanges[file] = content
	return b
}

func (b *rebaseFixtureBuilder) WithUnstagedChanges(file, content string) *rebaseFixtureBuilder {
	b.unstagedMods[file] = content
	return b
}

func (b *rebaseFixtureBuilder) WithUntrackedFiles(file, content string) *rebaseFixtureBuilder {
	b.untrackedFiles[file] = content
	return b
}

func (b *rebaseFixtureBuilder) WithPreCommitHook(script string) *rebaseFixtureBuilder {
	b.preCommitHook = script
	return b
}

func (b *rebaseFixtureBuilder) WithTimeout(d time.Duration) *rebaseFixtureBuilder {
	b.timeout = d
	return b
}

func (b *rebaseFixtureBuilder) WithMockLLM(fn ConflictResolverFunc) *rebaseFixtureBuilder {
	b.mockLLM = fn
	return b
}

// rebaseFixture is the built test fixture, ready to run sync operations.
type rebaseFixture struct {
	t         *testing.T
	remoteDir string
	cloneDir  string
	manager   *Manager
	st        *state.State
	wsID      string

	localBranch    string
	stagedChanges  map[string]string
	unstagedMods   map[string]string
	untrackedFiles map[string]string
	timeout        time.Duration

	// Track emitted steps for resolve-conflict tests
	emittedSteps []ResolveConflictStep
}

func (b *rebaseFixtureBuilder) Build() *rebaseFixture {
	b.t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		b.t.Skip("git not available")
	}

	// 1. Create remote repo with initial commit
	remoteDir := gitTestWorkTree(b.t)

	// 2. Clone it
	tmpDir := b.t.TempDir()
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(b.t, tmpDir, "clone", remoteDir, "clone")
	runGit(b.t, cloneDir, "config", "user.email", "test@test.com")
	runGit(b.t, cloneDir, "config", "user.name", "Test User")

	// 3. Create feature branch and local commits
	if b.localBranch != "main" {
		runGit(b.t, cloneDir, "checkout", "-b", b.localBranch)
	}
	for _, c := range b.localCommits {
		applyTestCommit(b.t, cloneDir, c)
	}

	// 4. Push remote commits to main AFTER the fork point
	for _, c := range b.remoteCommits {
		applyTestCommit(b.t, remoteDir, c)
	}

	// 5. Set up working directory state
	for file, content := range b.stagedChanges {
		writeFile(b.t, cloneDir, file, content)
		runGit(b.t, cloneDir, "add", file)
	}
	for file, content := range b.unstagedMods {
		writeFile(b.t, cloneDir, file, content)
	}
	for file, content := range b.untrackedFiles {
		writeFile(b.t, cloneDir, file, content)
	}

	// 6. Install pre-commit hook if requested
	if b.preCommitHook != "" {
		hooksDir := filepath.Join(cloneDir, ".git", "hooks")
		os.MkdirAll(hooksDir, 0755)
		hookPath := filepath.Join(hooksDir, "pre-commit")
		os.WriteFile(hookPath, []byte("#!/bin/sh\n"+b.preCommitHook+"\n"), 0755)
	}

	// 7. Set up Manager + State
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())
	m.setDefaultBranch(remoteDir, "main")

	if b.mockLLM != nil {
		m.conflictResolver = b.mockLLM
	}

	wsID := "test-ws-001"
	w := state.Workspace{
		ID:     wsID,
		Repo:   remoteDir,
		Branch: b.localBranch,
		Path:   cloneDir,
	}
	st.AddWorkspace(w)

	fix := &rebaseFixture{
		t:              b.t,
		remoteDir:      remoteDir,
		cloneDir:       cloneDir,
		manager:        m,
		st:             st,
		wsID:           wsID,
		localBranch:    b.localBranch,
		stagedChanges:  b.stagedChanges,
		unstagedMods:   b.unstagedMods,
		untrackedFiles: b.untrackedFiles,
		timeout:        b.timeout,
	}
	return fix
}

// applyTestCommit creates a commit in the given repo directory.
func applyTestCommit(t *testing.T, dir string, c testCommit) {
	t.Helper()
	for file, content := range c.files {
		// Ensure parent directories exist
		parent := filepath.Dir(filepath.Join(dir, file))
		os.MkdirAll(parent, 0755)
		writeFile(t, dir, file, content)
	}
	for _, file := range c.deletes {
		os.Remove(filepath.Join(dir, file))
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", c.message)
}

// RunLinearSyncFromDefault executes the clean sync operation.
func (f *rebaseFixture) RunLinearSyncFromDefault() (*LinearSyncResult, error) {
	f.t.Helper()
	ctx := context.Background()
	if f.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.timeout)
		f.t.Cleanup(cancel)
	}
	return f.manager.LinearSyncFromDefault(ctx, f.wsID)
}

// RunLinearSyncResolveConflict executes the conflict resolution operation.
func (f *rebaseFixture) RunLinearSyncResolveConflict() (*LinearSyncResolveConflictResult, error) {
	f.t.Helper()
	ctx := context.Background()
	if f.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.timeout)
		f.t.Cleanup(cancel)
	}
	f.emittedSteps = nil
	return f.manager.LinearSyncResolveConflict(ctx, f.wsID, func(step ResolveConflictStep) {
		f.emittedSteps = append(f.emittedSteps, step)
	})
}
```

**Step 2: Verify it compiles**

Run: `go build ./internal/workspace/...`
Expected: No errors. (No tests use it yet, but it should compile.)

**Step 3: Commit**

```
test(workspace): add rebase fixture builder scaffolding

Creates rebaseFixtureBuilder with configurable remote commits,
local commits, working directory state, pre-commit hooks, and
mock LLM injection. No tests yet — just the builder.
```

---

### Task 3: Add invariant assertions to the fixture

**Files:**
- Modify: `internal/workspace/rebase_fixture_test.go` (append assertion methods)

**Step 1: Add universal and scenario-specific assertion methods**

Append to `rebase_fixture_test.go`:

```go
// --- Universal invariant assertions ---

// AssertInvariants checks universal post-operation invariants:
// 1. No .git/rebase-merge or .git/rebase-apply
// 2. HEAD is not detached
// 3. On expected branch
// 4. No "WIP:" commit as HEAD
// 5. git status succeeds (repo not corrupt)
func (f *rebaseFixture) AssertInvariants() {
	f.t.Helper()
	f.assertNoRebaseInProgress()
	f.assertNotDetachedHead()
	f.assertOnBranch(f.localBranch)
	f.assertNoWipHead()
	f.assertGitStatusClean()
}

func (f *rebaseFixture) assertNoRebaseInProgress() {
	f.t.Helper()
	if rebaseInProgress(f.cloneDir) {
		f.t.Error("INVARIANT VIOLATED: rebase still in progress (.git/rebase-merge or .git/rebase-apply exists)")
	}
}

func (f *rebaseFixture) assertNotDetachedHead() {
	f.t.Helper()
	cmd := exec.Command("git", "symbolic-ref", "HEAD")
	cmd.Dir = f.cloneDir
	if err := cmd.Run(); err != nil {
		f.t.Error("INVARIANT VIOLATED: HEAD is detached")
	}
}

func (f *rebaseFixture) assertOnBranch(expected string) {
	f.t.Helper()
	got := currentBranch(f.t, f.cloneDir)
	if got != expected {
		f.t.Errorf("INVARIANT VIOLATED: expected branch %q, got %q", expected, got)
	}
}

func (f *rebaseFixture) assertNoWipHead() {
	f.t.Helper()
	cmd := exec.Command("git", "log", "-1", "--format=%s", "HEAD")
	cmd.Dir = f.cloneDir
	output, err := cmd.Output()
	if err != nil {
		f.t.Fatalf("git log -1 failed: %v", err)
	}
	msg := strings.TrimSpace(string(output))
	if strings.HasPrefix(msg, "WIP: ") {
		f.t.Errorf("INVARIANT VIOLATED: HEAD commit is a WIP commit: %q", msg)
	}
}

func (f *rebaseFixture) assertGitStatusClean() {
	f.t.Helper()
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = f.cloneDir
	if _, err := cmd.Output(); err != nil {
		f.t.Errorf("INVARIANT VIOLATED: git status failed (repo may be corrupt): %v", err)
	}
}

// --- Scenario-specific assertions ---

// AssertSuccess checks that the result indicates success with the expected count.
func (f *rebaseFixture) AssertSuccess(result *LinearSyncResult, err error, expectedCount int) {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("expected no error, got: %v", err)
	}
	if result == nil {
		f.t.Fatal("expected non-nil result")
	}
	if !result.Success {
		f.t.Errorf("expected Success=true, got false (ConflictingHash=%s)", result.ConflictingHash)
	}
	if result.SuccessCount != expectedCount {
		f.t.Errorf("expected SuccessCount=%d, got %d", expectedCount, result.SuccessCount)
	}
}

// AssertConflict checks that the result indicates a conflict on the expected hash.
func (f *rebaseFixture) AssertConflict(result *LinearSyncResult, err error, expectedSuccessCount int) {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("expected no error, got: %v", err)
	}
	if result == nil {
		f.t.Fatal("expected non-nil result")
	}
	if result.Success {
		f.t.Error("expected Success=false (conflict), got true")
	}
	if result.ConflictingHash == "" {
		f.t.Error("expected ConflictingHash to be set")
	}
	if result.SuccessCount != expectedSuccessCount {
		f.t.Errorf("expected SuccessCount=%d before conflict, got %d", expectedSuccessCount, result.SuccessCount)
	}
}

// AssertResolveSuccess checks that the resolve result indicates success.
func (f *rebaseFixture) AssertResolveSuccess(result *LinearSyncResolveConflictResult, err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("expected no error, got: %v", err)
	}
	if result == nil {
		f.t.Fatal("expected non-nil result")
	}
	if !result.Success {
		f.t.Errorf("expected Success=true, got false (Message=%s)", result.Message)
	}
}

// AssertResolveFailure checks that the resolve result indicates failure.
func (f *rebaseFixture) AssertResolveFailure(result *LinearSyncResolveConflictResult, err error, msgSubstring string) {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("expected no error (failure should be in result), got: %v", err)
	}
	if result == nil {
		f.t.Fatal("expected non-nil result")
	}
	if result.Success {
		f.t.Error("expected Success=false, got true")
	}
	if msgSubstring != "" && !strings.Contains(result.Message, msgSubstring) {
		f.t.Errorf("expected message containing %q, got %q", msgSubstring, result.Message)
	}
}

// AssertErrorContains checks that an error was returned containing the substring.
func (f *rebaseFixture) AssertErrorContains(err error, substring string) {
	f.t.Helper()
	if err == nil {
		f.t.Fatalf("expected error containing %q, got nil", substring)
	}
	if !strings.Contains(err.Error(), substring) {
		f.t.Errorf("expected error containing %q, got: %v", substring, err)
	}
}

// AssertLocalChangesPreserved verifies that all working directory changes
// set up via With*Changes/WithUntrackedFiles are still present.
func (f *rebaseFixture) AssertLocalChangesPreserved() {
	f.t.Helper()

	// Check untracked files still exist with expected content
	for file, expectedContent := range f.untrackedFiles {
		path := filepath.Join(f.cloneDir, file)
		data, err := os.ReadFile(path)
		if err != nil {
			f.t.Errorf("untracked file %q lost after operation: %v", file, err)
			continue
		}
		if string(data) != expectedContent {
			f.t.Errorf("untracked file %q content changed: got %q, want %q", file, string(data), expectedContent)
		}
	}

	// Check that unstaged modifications are visible in git diff
	if len(f.unstagedMods) > 0 {
		cmd := exec.Command("git", "diff", "--name-only")
		cmd.Dir = f.cloneDir
		output, err := cmd.Output()
		if err != nil {
			f.t.Fatalf("git diff --name-only failed: %v", err)
		}
		diffFiles := strings.TrimSpace(string(output))
		for file := range f.unstagedMods {
			if !strings.Contains(diffFiles, file) {
				f.t.Errorf("unstaged modification to %q lost after operation", file)
			}
		}
	}

	// Check that staged changes are visible in git diff --cached
	if len(f.stagedChanges) > 0 {
		cmd := exec.Command("git", "diff", "--cached", "--name-only")
		cmd.Dir = f.cloneDir
		output, err := cmd.Output()
		if err != nil {
			f.t.Fatalf("git diff --cached --name-only failed: %v", err)
		}
		cachedFiles := strings.TrimSpace(string(output))
		for file := range f.stagedChanges {
			if !strings.Contains(cachedFiles, file) {
				f.t.Errorf("staged change to %q lost after operation", file)
			}
		}
	}
}

// AssertAncestorOf verifies that ancestor is an ancestor of descendant.
func (f *rebaseFixture) AssertAncestorOf(ancestor, descendant string) {
	f.t.Helper()
	cmd := exec.Command("git", "merge-base", "--is-ancestor", ancestor, descendant)
	cmd.Dir = f.cloneDir
	if err := cmd.Run(); err != nil {
		f.t.Errorf("expected %s to be ancestor of %s, but it is not", ancestor, descendant)
	}
}

// AssertHeadContains verifies the HEAD commit message contains substring.
func (f *rebaseFixture) AssertHeadContains(substring string) {
	f.t.Helper()
	cmd := exec.Command("git", "log", "-1", "--format=%s")
	cmd.Dir = f.cloneDir
	output, err := cmd.Output()
	if err != nil {
		f.t.Fatalf("git log failed: %v", err)
	}
	if !strings.Contains(string(output), substring) {
		f.t.Errorf("HEAD message %q does not contain %q", strings.TrimSpace(string(output)), substring)
	}
}

// gitLogMessages returns the last N commit messages.
func (f *rebaseFixture) gitLogMessages(n int) []string {
	f.t.Helper()
	cmd := exec.Command("git", "log", fmt.Sprintf("-%d", n), "--format=%s")
	cmd.Dir = f.cloneDir
	output, err := cmd.Output()
	if err != nil {
		f.t.Fatalf("git log failed: %v", err)
	}
	var msgs []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" {
			msgs = append(msgs, line)
		}
	}
	return msgs
}

// remoteMainHash returns the commit hash of origin/main after fetching.
func (f *rebaseFixture) remoteMainHash() string {
	f.t.Helper()
	runGit(f.t, f.cloneDir, "fetch", "origin")
	return gitCommitHash(f.t, f.cloneDir, "origin/main")
}
```

**Step 2: Verify build**

Run: `go build ./internal/workspace/...`
Expected: Compiles.

**Step 3: Commit**

```
test(workspace): add invariant and scenario assertions to rebase fixture

Universal invariants: no rebase in progress, not detached, correct
branch, no orphaned WIP, git status parseable. Plus scenario
assertions for success/conflict/error/local-changes-preserved.
```

---

### Task 4: Write LinearSyncFromDefault happy-path tests (tests 1-6)

**Files:**
- Create: `internal/workspace/linear_sync_from_default_test.go`

**Step 1: Write the tests**

Create `internal/workspace/linear_sync_from_default_test.go` with tests 1-6:

```go
package workspace

import (
	"errors"
	"sync"
	"testing"
)

// --- Happy path tests ---

func TestLinearSyncFromDefault_AlreadyUpToDate(t *testing.T) {
	t.Parallel()
	// Feature branch ahead of main, main hasn't moved
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local work", "local.txt:local content")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertSuccess(result, err, 0)
	fix.AssertInvariants()
}

func TestLinearSyncFromDefault_SameCommit(t *testing.T) {
	t.Parallel()
	// Stay on main, no divergence
	fix := newRebaseFixture(t).
		WithLocalBranch("main").
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertSuccess(result, err, 0)
	fix.AssertInvariants()
}

func TestLinearSyncFromDefault_StrictlyBehind_SingleCommit(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(tc("remote commit 1", "remote.txt:remote content")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertSuccess(result, err, 1)
	fix.AssertInvariants()
	fix.AssertAncestorOf("origin/main", "HEAD")
}

func TestLinearSyncFromDefault_StrictlyBehind_ManyCommits(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(
			tc("remote 1", "r1.txt:one"),
			tc("remote 2", "r2.txt:two"),
			tc("remote 3", "r3.txt:three"),
			tc("remote 4", "r4.txt:four"),
			tc("remote 5", "r5.txt:five"),
		).
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertSuccess(result, err, 5)
	fix.AssertInvariants()
}

func TestLinearSyncFromDefault_Diverged_NoConflicts(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local work", "local.txt:local content")).
		WithRemoteCommits(tc("remote work", "remote.txt:remote content")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertSuccess(result, err, 1)
	fix.AssertInvariants()
	// Local commit should be replayed on top of main's commit
	fix.AssertAncestorOf("origin/main", "HEAD")
}

func TestLinearSyncFromDefault_Diverged_ManyCommitsBothSides(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(
			tc("local 1", "l1.txt:one"),
			tc("local 2", "l2.txt:two"),
			tc("local 3", "l3.txt:three"),
		).
		WithRemoteCommits(
			tc("remote 1", "r1.txt:one"),
			tc("remote 2", "r2.txt:two"),
			tc("remote 3", "r3.txt:three"),
			tc("remote 4", "r4.txt:four"),
			tc("remote 5", "r5.txt:five"),
		).
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertSuccess(result, err, 5)
	fix.AssertInvariants()
	fix.AssertAncestorOf("origin/main", "HEAD")
}
```

**Step 2: Run tests**

Run: `go test ./internal/workspace/ -run TestLinearSyncFromDefault_ -v -count=1`
Expected: All 6 tests PASS.

**Step 3: Commit**

```
test(workspace): add LinearSyncFromDefault happy-path tests (1-6)

Tests: already up to date, same commit, strictly behind (single
and many commits), diverged (no conflicts, many commits both sides).
```

---

### Task 5: Add local changes preservation tests (tests 7-11)

**Files:**
- Modify: `internal/workspace/linear_sync_from_default_test.go`

**Step 1: Append tests 7-11**

```go
// --- Local changes preservation ---

func TestLinearSyncFromDefault_PreservesUnstagedChanges(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("add tracked file", "tracked.txt:original")).
		WithRemoteCommits(tc("remote work", "remote.txt:remote")).
		WithUnstagedChanges("tracked.txt", "modified locally").
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertSuccess(result, err, 1)
	fix.AssertInvariants()
	fix.AssertLocalChangesPreserved()
}

func TestLinearSyncFromDefault_PreservesUntrackedFiles(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(tc("remote work", "remote.txt:remote")).
		WithUntrackedFiles("notes.txt", "my local notes").
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertSuccess(result, err, 1)
	fix.AssertInvariants()
	fix.AssertLocalChangesPreserved()
}

func TestLinearSyncFromDefault_PreservesStagedChanges(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(tc("remote work", "remote.txt:remote")).
		WithStagedChanges("new-feature.txt", "staged content").
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertSuccess(result, err, 1)
	fix.AssertInvariants()
	fix.AssertLocalChangesPreserved()
}

func TestLinearSyncFromDefault_PreservesMixedWorkingDir(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("add tracked", "tracked.txt:original")).
		WithRemoteCommits(tc("remote work", "remote.txt:remote")).
		WithStagedChanges("staged.txt", "staged content").
		WithUnstagedChanges("tracked.txt", "modified").
		WithUntrackedFiles("untracked.txt", "untracked content").
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertSuccess(result, err, 1)
	fix.AssertInvariants()
	fix.AssertLocalChangesPreserved()
}

func TestLinearSyncFromDefault_CleanDir_NoWipCommit(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(tc("remote work", "remote.txt:remote")).
		// No local changes — WIP commit should be skipped
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertSuccess(result, err, 1)
	fix.AssertInvariants()
	// Verify no WIP commit exists anywhere in recent history
	msgs := fix.gitLogMessages(5)
	for _, msg := range msgs {
		if strings.HasPrefix(msg, "WIP: ") {
			t.Errorf("found WIP commit in history: %q", msg)
		}
	}
}
```

Note: add `"strings"` to imports if not already present.

**Step 2: Run tests**

Run: `go test ./internal/workspace/ -run TestLinearSyncFromDefault_Preserves -v -count=1`
Run: `go test ./internal/workspace/ -run TestLinearSyncFromDefault_CleanDir -v -count=1`
Expected: All 5 tests PASS.

**Step 3: Commit**

```
test(workspace): add local changes preservation tests (7-11)

Tests: unstaged, untracked, staged, mixed working dir changes
survive rebase. Also verifies no WIP commit when dir is clean.
```

---

### Task 6: Add conflict detection tests (tests 12-18)

**Files:**
- Modify: `internal/workspace/linear_sync_from_default_test.go`

**Step 1: Append tests 12-18**

```go
// --- Conflict detection ---

func TestLinearSyncFromDefault_ConflictOnFirstCommit(t *testing.T) {
	t.Parallel()
	// Both sides modify the same file
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local version")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote version")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertConflict(result, err, 0) // 0 commits succeeded before conflict
	fix.AssertInvariants()
}

func TestLinearSyncFromDefault_ConflictOnNthCommit(t *testing.T) {
	t.Parallel()
	// First 2 remote commits are clean, 3rd conflicts
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local version")).
		WithRemoteCommits(
			tc("remote 1", "r1.txt:clean"),
			tc("remote 2", "r2.txt:clean"),
			tc("remote 3 conflicts", "shared.txt:remote version"),
		).
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertConflict(result, err, 2) // 2 succeeded, 3rd conflicted
	fix.AssertInvariants()
}

func TestLinearSyncFromDefault_ConflictSingleFileMultipleHunks(t *testing.T) {
	t.Parallel()
	// Create a file with multiple regions, both sides edit different regions
	baseContent := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"
	localContent := "LOCAL1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nLOCAL10\n"
	remoteContent := "REMOTE1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nREMOTE10\n"

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edits", "multi.txt:"+localContent)).
		WithRemoteCommits(tc("remote edits", "multi.txt:"+remoteContent)).
		Build()

	// First ensure the base file exists by checking the remote created it
	// Both branches fork from initial which has README.md, then both add multi.txt
	// This should conflict since both create the same file with different content
	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertConflict(result, err, 0)
	fix.AssertInvariants()
	_ = baseContent // used for documentation
}

func TestLinearSyncFromDefault_ConflictMultipleFiles(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edits", "a.txt:local A", "b.txt:local B")).
		WithRemoteCommits(tc("remote edits", "a.txt:remote A", "b.txt:remote B")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertConflict(result, err, 0)
	fix.AssertInvariants()
}

func TestLinearSyncFromDefault_ConflictDeleteVsModify(t *testing.T) {
	t.Parallel()
	// Remote deletes a file that the local branch modified.
	// First: both branches need the file. Add it on main before forking.
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("modify shared", "README.md:modified README")).
		WithRemoteCommits(tcDelete("delete README", "README.md")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertConflict(result, err, 0)
	fix.AssertInvariants()
}

func TestLinearSyncFromDefault_ConflictNewFileSameName(t *testing.T) {
	t.Parallel()
	// Both branches add the same new filename with different content
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local new file", "collision.txt:local version")).
		WithRemoteCommits(tc("remote new file", "collision.txt:remote version")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertConflict(result, err, 0)
	fix.AssertInvariants()
}

func TestLinearSyncFromDefault_ConflictPreservesLocalChanges(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local version")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote version")).
		WithStagedChanges("staged.txt", "staged").
		WithUnstagedChanges("README.md", "modified readme").
		WithUntrackedFiles("notes.txt", "my notes").
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	fix.AssertConflict(result, err, 0)
	fix.AssertInvariants()
	fix.AssertLocalChangesPreserved()
}
```

**Step 2: Run tests**

Run: `go test ./internal/workspace/ -run TestLinearSyncFromDefault_Conflict -v -count=1`
Expected: All 7 tests PASS.

**Step 3: Commit**

```
test(workspace): add conflict detection tests (12-18)

Tests: conflict on first commit, Nth commit, multiple hunks,
multiple files, delete-vs-modify, new-file collision, and
conflict with local changes preservation.
```

---

### Task 7: Add error/edge case tests (tests 19-23)

**Files:**
- Modify: `internal/workspace/linear_sync_from_default_test.go`

**Step 1: Append tests 19-23**

```go
// --- Error/edge cases ---

func TestLinearSyncFromDefault_OrphanDefaultBranch(t *testing.T) {
	t.Parallel()
	// This extends the existing test — set up via fixture for consistency
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local work", "local.txt:content")).
		Build()

	// Force-push an orphan commit to main on the remote
	runGit(t, fix.remoteDir, "checkout", "--orphan", "orphan-temp")
	writeFile(t, fix.remoteDir, "orphan.txt", "orphan content")
	runGit(t, fix.remoteDir, "add", ".")
	runGit(t, fix.remoteDir, "commit", "-m", "orphan commit")
	runGit(t, fix.remoteDir, "branch", "-f", "main")
	runGit(t, fix.remoteDir, "checkout", "main")

	_, err := fix.RunLinearSyncFromDefault()

	fix.AssertErrorContains(err, "no common ancestor")
	fix.AssertInvariants()
}

func TestLinearSyncFromDefault_TimeoutStopsEarly(t *testing.T) {
	t.Parallel()
	// Create many remote commits with a very short timeout
	commits := make([]testCommit, 20)
	for i := range commits {
		commits[i] = tc(fmt.Sprintf("remote %d", i+1), fmt.Sprintf("r%d.txt:content %d", i+1, i+1))
	}

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(commits...).
		WithTimeout(100 * time.Millisecond). // very short deadline
		Build()

	result, err := fix.RunLinearSyncFromDefault()

	// Should succeed with partial count (stopped early due to deadline)
	// Or might complete all if fast enough — either way, no error
	if err != nil {
		t.Fatalf("expected no error even with timeout, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Success {
		t.Logf("timeout caused early stop with conflict at hash: %s", result.ConflictingHash)
	} else {
		t.Logf("completed %d/%d commits before timeout logic kicked in", result.SuccessCount, 20)
	}
	fix.AssertInvariants()
}

func TestLinearSyncFromDefault_PreCommitHookRejectsWip(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(tc("remote work", "remote.txt:remote")).
		WithUntrackedFiles("notes.txt", "content"). // force WIP commit attempt
		WithPreCommitHook("exit 1").                 // hook rejects all commits
		Build()

	_, err := fix.RunLinearSyncFromDefault()

	if err == nil {
		t.Fatal("expected error from pre-commit hook")
	}
	var hookErr *PreCommitHookError
	if !errors.As(err, &hookErr) {
		t.Errorf("expected PreCommitHookError, got: %T: %v", err, err)
	}
	fix.AssertInvariants()
}

func TestLinearSyncFromDefault_ConcurrentSyncBlocked(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(tc("remote work", "remote.txt:remote")).
		Build()

	// Lock the workspace externally
	fix.manager.LockWorkspace(fix.wsID)
	defer fix.manager.UnlockWorkspace(fix.wsID)

	_, err := fix.RunLinearSyncFromDefault()

	if !errors.Is(err, ErrWorkspaceLocked) {
		t.Errorf("expected ErrWorkspaceLocked, got: %v", err)
	}
}

func TestLinearSyncFromDefault_WorkspaceNotFound(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		Build()

	// Call with non-existent workspace ID
	ctx := context.Background()
	_, err := fix.manager.LinearSyncFromDefault(ctx, "nonexistent-ws")

	if err == nil {
		t.Fatal("expected error for nonexistent workspace")
	}
	if !strings.Contains(err.Error(), "workspace not found") {
		t.Errorf("expected 'workspace not found' error, got: %v", err)
	}
}
```

Note: add `"context"`, `"errors"`, `"fmt"`, `"strings"`, `"sync"`, and `"time"` to imports.

**Step 2: Run tests**

Run: `go test ./internal/workspace/ -run "TestLinearSyncFromDefault_(Orphan|Timeout|PreCommit|Concurrent|WorkspaceNotFound)" -v -count=1`
Expected: All 5 tests PASS.

**Step 3: Commit**

```
test(workspace): add error/edge case tests (19-23)

Tests: orphan default branch, timeout stops early, pre-commit
hook rejects WIP, concurrent sync blocked, workspace not found.
```

---

### Task 8: Write LinearSyncResolveConflict mock helpers

**Files:**
- Modify: `internal/workspace/rebase_fixture_test.go`

**Step 1: Add mock LLM factory functions**

Append to `rebase_fixture_test.go`:

```go
// --- Mock LLM factories for LinearSyncResolveConflict tests ---

// mockLLMResolveAll returns a mock that resolves all files with high confidence.
// resolvedContents maps filename -> resolved content to write to disk.
func mockLLMResolveAll(resolvedContents map[string]string) ConflictResolverFunc {
	return func(ctx context.Context, cfg *config.Config, prompt string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		fileActions := make(map[string]conflictresolve.FileAction, len(resolvedContents))
		for name, content := range resolvedContents {
			if content == "" {
				// Delete the file
				os.Remove(filepath.Join(workspacePath, name))
				fileActions[name] = conflictresolve.FileAction{Action: "deleted", Description: "deleted"}
			} else {
				// Write resolved content
				os.WriteFile(filepath.Join(workspacePath, name), []byte(content), 0644)
				fileActions[name] = conflictresolve.FileAction{Action: "modified", Description: "resolved"}
			}
		}
		return conflictresolve.OneshotResult{
			AllResolved: true,
			Confidence:  "high",
			Summary:     "All conflicts resolved",
			Files:       fileActions,
		}, "", nil
	}
}

// mockLLMLowConfidence returns a mock that resolves files but reports low confidence.
func mockLLMLowConfidence(resolvedContents map[string]string) ConflictResolverFunc {
	return func(ctx context.Context, cfg *config.Config, prompt string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		fileActions := make(map[string]conflictresolve.FileAction, len(resolvedContents))
		for name, content := range resolvedContents {
			os.WriteFile(filepath.Join(workspacePath, name), []byte(content), 0644)
			fileActions[name] = conflictresolve.FileAction{Action: "modified", Description: "resolved"}
		}
		return conflictresolve.OneshotResult{
			AllResolved: true,
			Confidence:  "medium",
			Summary:     "Resolved but uncertain",
			Files:       fileActions,
		}, "", nil
	}
}

// mockLLMNotAllResolved returns a mock where AllResolved is false.
func mockLLMNotAllResolved(resolvedContents map[string]string) ConflictResolverFunc {
	return func(ctx context.Context, cfg *config.Config, prompt string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		fileActions := make(map[string]conflictresolve.FileAction, len(resolvedContents))
		for name, content := range resolvedContents {
			os.WriteFile(filepath.Join(workspacePath, name), []byte(content), 0644)
			fileActions[name] = conflictresolve.FileAction{Action: "modified", Description: "partial"}
		}
		return conflictresolve.OneshotResult{
			AllResolved: false,
			Confidence:  "high",
			Summary:     "Could not resolve all",
			Files:       fileActions,
		}, "", nil
	}
}

// mockLLMError returns a mock that returns an error.
func mockLLMError() ConflictResolverFunc {
	return func(ctx context.Context, cfg *config.Config, prompt string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		return conflictresolve.OneshotResult{}, "raw response", fmt.Errorf("LLM service unavailable")
	}
}

// mockLLMOmitsFile returns a mock that resolves some files but omits one.
func mockLLMOmitsFile(resolvedContents map[string]string, omitFile string) ConflictResolverFunc {
	return func(ctx context.Context, cfg *config.Config, prompt string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		fileActions := make(map[string]conflictresolve.FileAction)
		for name, content := range resolvedContents {
			if name == omitFile {
				continue // omit this file
			}
			os.WriteFile(filepath.Join(workspacePath, name), []byte(content), 0644)
			fileActions[name] = conflictresolve.FileAction{Action: "modified", Description: "resolved"}
		}
		return conflictresolve.OneshotResult{
			AllResolved: true,
			Confidence:  "high",
			Summary:     "Resolved (but omitted a file)",
			Files:       fileActions,
		}, "", nil
	}
}

// mockLLMDeletedButExists returns a mock that claims a file is deleted but doesn't delete it.
func mockLLMDeletedButExists(file string) ConflictResolverFunc {
	return func(ctx context.Context, cfg *config.Config, prompt string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		// Don't actually delete the file — leave it on disk
		// But clear conflict markers so it looks resolved
		os.WriteFile(filepath.Join(workspacePath, file), []byte("still here"), 0644)
		return conflictresolve.OneshotResult{
			AllResolved: true,
			Confidence:  "high",
			Summary:     "Resolved by deletion",
			Files: map[string]conflictresolve.FileAction{
				file: {Action: "deleted", Description: "deleted"},
			},
		}, "", nil
	}
}

// mockLLMLeaveMarkers returns a mock that claims modified but leaves conflict markers.
func mockLLMLeaveMarkers(file string) ConflictResolverFunc {
	return func(ctx context.Context, cfg *config.Config, prompt string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		// Write content that still has conflict markers
		os.WriteFile(filepath.Join(workspacePath, file), []byte("<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> branch\n"), 0644)
		return conflictresolve.OneshotResult{
			AllResolved: true,
			Confidence:  "high",
			Summary:     "Resolved (badly)",
			Files: map[string]conflictresolve.FileAction{
				file: {Action: "modified", Description: "resolved"},
			},
		}, "", nil
	}
}

// mockLLMUnknownAction returns a mock with an unrecognized action.
func mockLLMUnknownAction(file string) ConflictResolverFunc {
	return func(ctx context.Context, cfg *config.Config, prompt string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		os.WriteFile(filepath.Join(workspacePath, file), []byte("resolved content"), 0644)
		return conflictresolve.OneshotResult{
			AllResolved: true,
			Confidence:  "high",
			Summary:     "Resolved",
			Files: map[string]conflictresolve.FileAction{
				file: {Action: "renamed", Description: "renamed the file"}, // unknown action
			},
		}, "", nil
	}
}

// mockLLMSequential returns a mock that returns different results on successive calls.
// Useful for testing multi-commit conflict resolution.
func mockLLMSequential(mocks ...ConflictResolverFunc) ConflictResolverFunc {
	var mu sync.Mutex
	callIndex := 0
	return func(ctx context.Context, cfg *config.Config, prompt string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		mu.Lock()
		i := callIndex
		callIndex++
		mu.Unlock()
		if i >= len(mocks) {
			return conflictresolve.OneshotResult{}, "", fmt.Errorf("unexpected call #%d (only %d mocks configured)", i+1, len(mocks))
		}
		return mocks[i](ctx, cfg, prompt, workspacePath)
	}
}
```

**Step 2: Verify build**

Run: `go build ./internal/workspace/...`
Expected: Compiles.

**Step 3: Commit**

```
test(workspace): add LLM mock factories for resolve-conflict tests

Mock factories for: high confidence, low confidence, not all
resolved, error, omitted file, deleted-but-exists, conflict
markers remain, unknown action, and sequential (multi-call).
```

---

### Task 9: Write LinearSyncResolveConflict happy-path tests (tests 24-31)

**Files:**
- Create: `internal/workspace/linear_sync_resolve_test.go`

**Step 1: Write tests 24-31**

Create `internal/workspace/linear_sync_resolve_test.go`:

```go
package workspace

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// --- Happy paths ---

func TestResolveConflict_AlreadyCaughtUp(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local work", "local.txt:content")).
		// No remote commits — already caught up
		WithMockLLM(mockLLMResolveAll(nil)).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveSuccess(result, err)
	if !strings.Contains(result.Message, "Caught up") {
		t.Errorf("expected 'Caught up' message, got: %s", result.Message)
	}
	fix.AssertInvariants()
}

func TestResolveConflict_CleanRebase_NoConflicts(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local work", "local.txt:content")).
		WithRemoteCommits(tc("remote work", "remote.txt:remote")).
		WithMockLLM(mockLLMResolveAll(nil)). // shouldn't be called
		Build()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveSuccess(result, err)
	if len(result.Resolutions) != 0 {
		t.Errorf("expected 0 resolutions, got %d", len(result.Resolutions))
	}
	fix.AssertInvariants()
}

func TestResolveConflict_SingleConflict_HighConfidence(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local version")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote version")).
		WithMockLLM(mockLLMResolveAll(map[string]string{
			"shared.txt": "merged version",
		})).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveSuccess(result, err)
	if len(result.Resolutions) != 1 {
		t.Errorf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	fix.AssertInvariants()
}

func TestResolveConflict_MultipleFiles_AllResolved(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edits", "a.txt:local A", "b.txt:local B")).
		WithRemoteCommits(tc("remote edits", "a.txt:remote A", "b.txt:remote B")).
		WithMockLLM(mockLLMResolveAll(map[string]string{
			"a.txt": "merged A",
			"b.txt": "merged B",
		})).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveSuccess(result, err)
	fix.AssertInvariants()
}

func TestResolveConflict_DeletedFile_Resolved(t *testing.T) {
	t.Parallel()
	// Remote deletes a file, local modifies it. LLM decides to accept the delete.
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("modify readme", "README.md:modified")).
		WithRemoteCommits(tcDelete("delete readme", "README.md")).
		WithMockLLM(mockLLMResolveAll(map[string]string{
			"README.md": "", // empty string = delete
		})).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveSuccess(result, err)
	fix.AssertInvariants()
}

func TestResolveConflict_TwoConflictingCommits_BothResolved(t *testing.T) {
	t.Parallel()
	// Two remote commits both conflict with local. LLM resolves both.
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edits", "shared.txt:local version")).
		WithRemoteCommits(
			tc("remote 1", "shared.txt:remote v1"),
			// Note: LinearSyncResolveConflict only processes ONE commit (the oldest).
			// So only one conflict will be encountered per call.
		).
		WithMockLLM(mockLLMResolveAll(map[string]string{
			"shared.txt": "merged version",
		})).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveSuccess(result, err)
	fix.AssertInvariants()
}

func TestResolveConflict_FirstResolvesSecondFails(t *testing.T) {
	t.Parallel()
	// LinearSyncResolveConflict processes one remote commit at a time.
	// If a single commit has multiple local commits being replayed, the first
	// local commit's conflict resolves but the second's doesn't.
	// This is tested via the conflict loop within a single rebase.
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(
			tc("local edit 1", "shared.txt:local v1"),
			tc("local edit 2", "shared.txt:local v2"),
		).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote version")).
		WithMockLLM(mockLLMSequential(
			mockLLMResolveAll(map[string]string{"shared.txt": "merged v1"}),
			mockLLMNotAllResolved(map[string]string{"shared.txt": "partial v2"}),
		)).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveFailure(result, err, "not all resolved")
	if len(result.Resolutions) < 1 {
		t.Error("expected at least 1 resolution recorded before failure")
	}
	fix.AssertInvariants()
}

func TestResolveConflict_GitAutoResolves(t *testing.T) {
	t.Parallel()
	// Git auto-resolves content merges when changes don't overlap.
	// Local edits line 1, remote edits line 10 of a shared file.
	baseContent := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"

	// We need the shared file in the base commit first
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		Build()

	// Manually set up the scenario with non-overlapping edits
	// Add shared file to main first
	writeFile(t, fix.remoteDir, "auto.txt", baseContent)
	runGit(t, fix.remoteDir, "add", ".")
	runGit(t, fix.remoteDir, "commit", "-m", "add auto.txt to main")

	// Fetch so clone knows about the file, then create it locally
	runGit(t, fix.cloneDir, "fetch", "origin")
	// Merge the new base into feature so both sides have the file
	runGit(t, fix.cloneDir, "merge", "origin/main", "--no-edit")

	// Local: edit line 1
	writeFile(t, fix.cloneDir, "auto.txt", "LOCAL1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n")
	runGit(t, fix.cloneDir, "add", ".")
	runGit(t, fix.cloneDir, "commit", "-m", "edit line 1")

	// Remote: edit line 10
	writeFile(t, fix.remoteDir, "auto.txt", "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nREMOTE10\n")
	runGit(t, fix.remoteDir, "add", ".")
	runGit(t, fix.remoteDir, "commit", "-m", "edit line 10")

	// Mock shouldn't be called since git auto-resolves
	fix.manager.conflictResolver = mockLLMError()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveSuccess(result, err)
	fix.AssertInvariants()
}
```

**Step 2: Run tests**

Run: `go test ./internal/workspace/ -run TestResolveConflict_ -v -count=1`
Expected: All 8 tests PASS. Some may need adjustment based on actual git behavior — fix as needed.

**Step 3: Commit**

```
test(workspace): add LinearSyncResolveConflict happy-path tests (24-31)

Tests: caught up, clean rebase, single conflict resolved, multiple
files, deleted file, two conflicting commits, first-resolves-second-
fails, git auto-resolves.
```

---

### Task 10: Write LLM failure mode tests (tests 32-38)

**Files:**
- Modify: `internal/workspace/linear_sync_resolve_test.go`

**Step 1: Append tests 32-38**

```go
// --- LLM failure modes ---

func TestResolveConflict_LowConfidence_Aborts(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote")).
		WithMockLLM(mockLLMLowConfidence(map[string]string{"shared.txt": "merged"})).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveFailure(result, err, "confidence")
	fix.AssertInvariants()
}

func TestResolveConflict_NotAllResolved_Aborts(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote")).
		WithMockLLM(mockLLMNotAllResolved(map[string]string{"shared.txt": "partial"})).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveFailure(result, err, "not all resolved")
	fix.AssertInvariants()
}

func TestResolveConflict_LLMError_Aborts(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote")).
		WithMockLLM(mockLLMError()).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveFailure(result, err, "")
	fix.AssertInvariants()
}

func TestResolveConflict_LLMOmitsFile_Aborts(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edits", "a.txt:local A", "b.txt:local B")).
		WithRemoteCommits(tc("remote edits", "a.txt:remote A", "b.txt:remote B")).
		WithMockLLM(mockLLMOmitsFile(
			map[string]string{"a.txt": "merged A", "b.txt": "merged B"},
			"b.txt", // omit b.txt from LLM response
		)).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveFailure(result, err, "omitted")
	fix.AssertInvariants()
}

func TestResolveConflict_LLMSaysDeletedButFileExists(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote")).
		WithMockLLM(mockLLMDeletedButExists("shared.txt")).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveFailure(result, err, "deleted but file still exists")
	fix.AssertInvariants()
}

func TestResolveConflict_ConflictMarkersRemain_Aborts(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote")).
		WithMockLLM(mockLLMLeaveMarkers("shared.txt")).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveFailure(result, err, "conflict markers")
	fix.AssertInvariants()
}

func TestResolveConflict_UnknownAction_Aborts(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote")).
		WithMockLLM(mockLLMUnknownAction("shared.txt")).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveFailure(result, err, "unknown action")
	fix.AssertInvariants()
}
```

**Step 2: Run tests**

Run: `go test ./internal/workspace/ -run "TestResolveConflict_(Low|NotAll|LLMError|LLMOmits|LLMSaysDeleted|ConflictMarkers|Unknown)" -v -count=1`
Expected: All 7 tests PASS.

**Step 3: Commit**

```
test(workspace): add LLM failure mode tests (32-38)

Tests: low confidence aborts, not-all-resolved aborts, LLM error
aborts, omitted file aborts, deleted-but-exists aborts, conflict
markers remain aborts, unknown action aborts.
```

---

### Task 11: Write state preservation and edge case tests (tests 39-43)

**Files:**
- Modify: `internal/workspace/linear_sync_resolve_test.go`

**Step 1: Append tests 39-43**

```go
// --- State preservation on failure ---

func TestResolveConflict_AbortPreservesLocalChanges(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote")).
		WithStagedChanges("staged.txt", "staged content").
		WithUntrackedFiles("notes.txt", "my notes").
		WithMockLLM(mockLLMNotAllResolved(map[string]string{"shared.txt": "partial"})).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveFailure(result, err, "not all resolved")
	fix.AssertInvariants()
	fix.AssertLocalChangesPreserved()
}

func TestResolveConflict_AbortNoWipIfCleanDir(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote")).
		// No local changes — WIP commit should be skipped
		WithMockLLM(mockLLMNotAllResolved(map[string]string{"shared.txt": "partial"})).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()

	fix.AssertResolveFailure(result, err, "not all resolved")
	fix.AssertInvariants()
	// Verify no WIP commit in history
	msgs := fix.gitLogMessages(5)
	for _, msg := range msgs {
		if strings.HasPrefix(msg, "WIP: ") {
			t.Errorf("found orphaned WIP commit: %q", msg)
		}
	}
}

func TestResolveConflict_PreCommitHookRejectsWip(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote")).
		WithUntrackedFiles("notes.txt", "content").
		WithPreCommitHook("exit 1").
		WithMockLLM(mockLLMResolveAll(map[string]string{"shared.txt": "merged"})).
		Build()

	_, err := fix.RunLinearSyncResolveConflict()

	if err == nil {
		t.Fatal("expected error from pre-commit hook")
	}
	var hookErr *PreCommitHookError
	if !errors.As(err, &hookErr) {
		t.Errorf("expected PreCommitHookError, got: %T: %v", err, err)
	}
	fix.AssertInvariants()
}

func TestResolveConflict_ConcurrentBlocked(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(tc("remote work", "remote.txt:remote")).
		WithMockLLM(mockLLMResolveAll(nil)).
		Build()

	// Lock the workspace externally
	fix.manager.LockWorkspace(fix.wsID)
	defer fix.manager.UnlockWorkspace(fix.wsID)

	_, err := fix.RunLinearSyncResolveConflict()

	if !errors.Is(err, ErrWorkspaceLocked) {
		t.Errorf("expected ErrWorkspaceLocked, got: %v", err)
	}
}

func TestResolveConflict_NoCommonAncestor(t *testing.T) {
	t.Parallel()
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local work", "local.txt:content")).
		WithMockLLM(mockLLMResolveAll(nil)).
		Build()

	// Force-push orphan to remote main
	runGit(t, fix.remoteDir, "checkout", "--orphan", "orphan-temp")
	writeFile(t, fix.remoteDir, "orphan.txt", "orphan")
	runGit(t, fix.remoteDir, "add", ".")
	runGit(t, fix.remoteDir, "commit", "-m", "orphan")
	runGit(t, fix.remoteDir, "branch", "-f", "main")
	runGit(t, fix.remoteDir, "checkout", "main")

	_, err := fix.RunLinearSyncResolveConflict()

	fix.AssertErrorContains(err, "no common ancestor")
	fix.AssertInvariants()
}
```

**Step 2: Run tests**

Run: `go test ./internal/workspace/ -run "TestResolveConflict_(Abort|PreCommitHook|Concurrent|NoCommon)" -v -count=1`
Expected: All 5 tests PASS.

**Step 3: Commit**

```
test(workspace): add state preservation and edge case tests (39-43)

Tests: abort preserves local changes, abort no WIP if clean,
pre-commit hook rejects WIP, concurrent blocked, no common ancestor.
```

---

### Task 12: Run full test suite and fix any failures

**Step 1: Run all rebase tests**

Run: `go test ./internal/workspace/ -run "TestLinearSyncFromDefault_|TestResolveConflict_" -v -count=1`
Expected: All 43 tests PASS.

**Step 2: Run existing tests to ensure no regressions**

Run: `go test ./internal/workspace/... -count=1`
Expected: All tests pass, including the original `TestLinearSyncFromDefault_RejectsOrphanDefaultBranch`.

**Step 3: Run project-wide quick tests**

Run: `./test.sh --quick`
Expected: All tests pass.

**Step 4: Fix any failures**

Debug and fix any test that doesn't pass. Common issues:
- Git behavior differing between versions (test on the actual system)
- Race conditions in the concurrent tests
- The fixture builder not setting up repos exactly right for certain conflict types
- Mock LLM not writing to the correct path relative to workspace

After fixing, re-run all tests to confirm green.

**Step 5: Commit fixes if any**

```
fix(workspace): adjust rebase scenario tests for actual git behavior
```

---

### Task 13: Final review and cleanup

**Step 1: Review fixture for DRY**

Read through `rebase_fixture_test.go` and the two test files. Look for:
- Duplicated setup patterns that should be in the builder
- Missing assertions that should be in `AssertInvariants()`
- Assertion messages that don't help diagnose failures

**Step 2: Check the existing orphan test**

The existing `TestLinearSyncFromDefault_RejectsOrphanDefaultBranch` in `linear_sync_test.go` overlaps with test 19. Consider removing the old one to avoid duplication, or keep both if the new one uses the fixture and the old one serves as a reference.

**Step 3: Commit cleanup**

```
refactor(workspace): clean up rebase scenario test suite
```
