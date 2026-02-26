package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/conflictresolve"
	"github.com/sergeknystautas/schmux/internal/state"
)

// ---------------------------------------------------------------------------
// testCommit describes a commit to create in a test repo
// ---------------------------------------------------------------------------

type testCommit struct {
	message string
	files   map[string]string // filename -> content
	deletes []string          // files to delete
}

// tc creates a testCommit with a message and "filename:content" pairs.
// Each pair must contain exactly one colon separating the filename from its content.
func tc(message string, filePairs ...string) testCommit {
	files := make(map[string]string, len(filePairs))
	for _, pair := range filePairs {
		idx := strings.Index(pair, ":")
		if idx < 0 {
			panic(fmt.Sprintf("tc: invalid file pair (missing ':'): %q", pair))
		}
		files[pair[:idx]] = pair[idx+1:]
	}
	return testCommit{
		message: message,
		files:   files,
	}
}

// tcDelete creates a testCommit that deletes files.
func tcDelete(message string, filesToDelete ...string) testCommit {
	return testCommit{
		message: message,
		deletes: filesToDelete,
	}
}

// ---------------------------------------------------------------------------
// Builder
// ---------------------------------------------------------------------------

// rebaseFixtureBuilder accumulates configuration for a rebase test fixture.
type rebaseFixtureBuilder struct {
	t              *testing.T
	localBranch    string            // default: "feature"
	remoteCommits  []testCommit      // pushed to main AFTER fork point
	localCommits   []testCommit      // committed on the local branch
	stagedChanges  map[string]string // filename -> content (staged but not committed)
	unstagedMods   map[string]string // filename -> content (modifications to tracked files, not staged)
	untrackedFiles map[string]string // filename -> content (new files, not added)
	preCommitHook  string            // shell script body for .git/hooks/pre-commit
	timeout        time.Duration     // context timeout for sync operations
	mockLLM        ConflictResolverFunc
}

// newRebaseFixture creates a new builder with sensible defaults.
func newRebaseFixture(t *testing.T) *rebaseFixtureBuilder {
	t.Helper()
	return &rebaseFixtureBuilder{
		t:           t,
		localBranch: "feature",
	}
}

// WithLocalBranch sets the name of the local branch (default: "feature").
func (b *rebaseFixtureBuilder) WithLocalBranch(branch string) *rebaseFixtureBuilder {
	b.localBranch = branch
	return b
}

// WithRemoteCommits sets commits to push to main AFTER the fork point.
func (b *rebaseFixtureBuilder) WithRemoteCommits(commits ...testCommit) *rebaseFixtureBuilder {
	b.remoteCommits = commits
	return b
}

// WithLocalCommits sets commits to create on the local branch.
func (b *rebaseFixtureBuilder) WithLocalCommits(commits ...testCommit) *rebaseFixtureBuilder {
	b.localCommits = commits
	return b
}

// WithStagedChanges sets files to stage but not commit.
func (b *rebaseFixtureBuilder) WithStagedChanges(files map[string]string) *rebaseFixtureBuilder {
	b.stagedChanges = files
	return b
}

// WithUnstagedChanges sets modifications to tracked files that are not staged.
func (b *rebaseFixtureBuilder) WithUnstagedChanges(files map[string]string) *rebaseFixtureBuilder {
	b.unstagedMods = files
	return b
}

// WithUntrackedFiles sets new files that are not added to git.
func (b *rebaseFixtureBuilder) WithUntrackedFiles(files map[string]string) *rebaseFixtureBuilder {
	b.untrackedFiles = files
	return b
}

// WithPreCommitHook installs a pre-commit hook with the given shell script body.
func (b *rebaseFixtureBuilder) WithPreCommitHook(script string) *rebaseFixtureBuilder {
	b.preCommitHook = script
	return b
}

// WithTimeout sets a context timeout for sync operations.
func (b *rebaseFixtureBuilder) WithTimeout(d time.Duration) *rebaseFixtureBuilder {
	b.timeout = d
	return b
}

// WithMockLLM injects a mock conflict resolver function.
func (b *rebaseFixtureBuilder) WithMockLLM(fn ConflictResolverFunc) *rebaseFixtureBuilder {
	b.mockLLM = fn
	return b
}

// ---------------------------------------------------------------------------
// Built fixture
// ---------------------------------------------------------------------------

// rebaseFixture is the built test fixture containing repos, manager, and state.
type rebaseFixture struct {
	t              *testing.T
	remoteDir      string
	cloneDir       string
	manager        *Manager
	st             *state.State
	wsID           string
	localBranch    string
	stagedChanges  map[string]string
	unstagedMods   map[string]string
	untrackedFiles map[string]string
	timeout        time.Duration

	mu           sync.Mutex
	emittedSteps []ResolveConflictStep
}

// Build constructs the test repos, manager, and state from the builder configuration.
func (b *rebaseFixtureBuilder) Build() *rebaseFixture {
	b.t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		b.t.Skip("git not available")
	}

	// 1. Create the remote repo (bare-ish, with a working tree for pushing commits)
	remoteDir := gitTestWorkTree(b.t) // creates repo on "main" with initial commit

	// 2. Clone the remote into a temp directory
	tmpDir := b.t.TempDir()
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(b.t, tmpDir, "clone", remoteDir, "clone")

	// 3. Configure git user in the clone
	runGit(b.t, cloneDir, "config", "user.email", "test@test.com")
	runGit(b.t, cloneDir, "config", "user.name", "Test User")

	// 4. Create local branch (if not main)
	if b.localBranch != "main" {
		runGit(b.t, cloneDir, "checkout", "-b", b.localBranch)
	}

	// 5. Apply local commits on the local branch
	for _, c := range b.localCommits {
		applyTestCommit(b.t, cloneDir, c)
	}

	// 6. Apply remote commits to the remote repo (on main, after the fork)
	for _, c := range b.remoteCommits {
		applyTestCommit(b.t, remoteDir, c)
	}

	// 7. Fetch in clone so origin/main is up to date
	if len(b.remoteCommits) > 0 {
		runGit(b.t, cloneDir, "fetch", "origin")
	}

	// 8. Apply staged changes
	for name, content := range b.stagedChanges {
		writeFile(b.t, cloneDir, name, content)
		runGit(b.t, cloneDir, "add", name)
	}

	// 9. Apply unstaged modifications (file must already be tracked)
	for name, content := range b.unstagedMods {
		writeFile(b.t, cloneDir, name, content)
	}

	// 10. Apply untracked files
	for name, content := range b.untrackedFiles {
		writeFile(b.t, cloneDir, name, content)
	}

	// 11. Install pre-commit hook if requested
	if b.preCommitHook != "" {
		hooksDir := filepath.Join(cloneDir, ".git", "hooks")
		if err := os.MkdirAll(hooksDir, 0755); err != nil {
			b.t.Fatalf("failed to create hooks dir: %v", err)
		}
		hookPath := filepath.Join(hooksDir, "pre-commit")
		hookContent := "#!/bin/sh\n" + b.preCommitHook + "\n"
		if err := os.WriteFile(hookPath, []byte(hookContent), 0755); err != nil {
			b.t.Fatalf("failed to write pre-commit hook: %v", err)
		}
	}

	// 12. Create Manager + State
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	// Cache the default branch
	m.setDefaultBranch(remoteDir, "main")

	// Inject mock LLM if provided
	if b.mockLLM != nil {
		m.conflictResolver = b.mockLLM
	}

	// 13. Register workspace in state
	wsID := "test-ws-001"
	w := state.Workspace{
		ID:     wsID,
		Repo:   remoteDir,
		Branch: b.localBranch,
		Path:   cloneDir,
	}
	if err := st.AddWorkspace(w); err != nil {
		b.t.Fatalf("failed to add workspace to state: %v", err)
	}

	return &rebaseFixture{
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
}

// applyTestCommit writes files, deletes files, stages everything, and commits.
func applyTestCommit(t *testing.T, dir string, c testCommit) {
	t.Helper()
	for name, content := range c.files {
		// Ensure parent directory exists for nested paths
		parent := filepath.Dir(filepath.Join(dir, name))
		if err := os.MkdirAll(parent, 0755); err != nil {
			t.Fatalf("failed to create parent dir for %s: %v", name, err)
		}
		writeFile(t, dir, name, content)
	}
	for _, name := range c.deletes {
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatalf("failed to delete %s: %v", name, err)
		}
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", c.message)
}

// ---------------------------------------------------------------------------
// Run helpers
// ---------------------------------------------------------------------------

// RunLinearSyncFromDefault runs LinearSyncFromDefault with an optional timeout.
func (f *rebaseFixture) RunLinearSyncFromDefault() (*LinearSyncResult, error) {
	f.t.Helper()
	ctx := context.Background()
	if f.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.timeout)
		defer cancel()
	}
	return f.manager.LinearSyncFromDefault(ctx, f.wsID)
}

// RunLinearSyncResolveConflict runs LinearSyncResolveConflict, recording emitted steps.
func (f *rebaseFixture) RunLinearSyncResolveConflict() (*LinearSyncResolveConflictResult, error) {
	f.t.Helper()
	ctx := context.Background()
	if f.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.timeout)
		defer cancel()
	}

	onStep := func(step ResolveConflictStep) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.emittedSteps = append(f.emittedSteps, step)
	}

	return f.manager.LinearSyncResolveConflict(ctx, f.wsID, onStep)
}

// EmittedSteps returns a copy of the steps emitted during RunLinearSyncResolveConflict.
func (f *rebaseFixture) EmittedSteps() []ResolveConflictStep {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ResolveConflictStep, len(f.emittedSteps))
	copy(out, f.emittedSteps)
	return out
}

// ---------------------------------------------------------------------------
// Invariant assertions (call after EVERY test)
// ---------------------------------------------------------------------------

// AssertInvariants checks universal post-conditions that must hold after any
// LinearSyncFromDefault or LinearSyncResolveConflict call.
func (f *rebaseFixture) AssertInvariants() {
	f.t.Helper()

	// 1. No rebase in progress
	if rebaseInProgress(f.cloneDir) {
		f.t.Error("INVARIANT VIOLATED: rebase still in progress (rebase-merge or rebase-apply directory exists)")
	}

	// 2. HEAD is not detached
	cmd := exec.Command("git", "symbolic-ref", "HEAD")
	cmd.Dir = f.cloneDir
	if out, err := cmd.CombinedOutput(); err != nil {
		f.t.Errorf("INVARIANT VIOLATED: HEAD is detached (git symbolic-ref HEAD failed: %v: %s)", err, strings.TrimSpace(string(out)))
	}

	// 3. On expected branch
	branch := currentBranch(f.t, f.cloneDir)
	if branch != f.localBranch {
		f.t.Errorf("INVARIANT VIOLATED: expected branch %q, got %q", f.localBranch, branch)
	}

	// 4. No "WIP:" commit as HEAD
	headCmd := exec.Command("git", "log", "-1", "--format=%s")
	headCmd.Dir = f.cloneDir
	headOut, err := headCmd.Output()
	if err != nil {
		f.t.Errorf("INVARIANT VIOLATED: failed to read HEAD commit message: %v", err)
	} else {
		msg := strings.TrimSpace(string(headOut))
		if strings.HasPrefix(msg, "WIP:") {
			f.t.Errorf("INVARIANT VIOLATED: HEAD commit is a WIP commit: %q", msg)
		}
	}

	// 5. Repository is not corrupt (git status succeeds)
	statusCmd := exec.Command("git", "status")
	statusCmd.Dir = f.cloneDir
	if out, err := statusCmd.CombinedOutput(); err != nil {
		f.t.Errorf("INVARIANT VIOLATED: git status failed (repo may be corrupt): %v: %s", err, strings.TrimSpace(string(out)))
	}
}

// ---------------------------------------------------------------------------
// Scenario assertions
// ---------------------------------------------------------------------------

// AssertSuccess asserts that LinearSyncFromDefault succeeded with the expected commit count.
func (f *rebaseFixture) AssertSuccess(result *LinearSyncResult, err error, expectedCount int) {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		f.t.Fatal("expected non-nil result")
	}
	if !result.Success {
		f.t.Errorf("expected Success=true, got false (conflicting_hash=%s)", result.ConflictingHash)
	}
	if result.SuccessCount != expectedCount {
		f.t.Errorf("expected SuccessCount=%d, got %d", expectedCount, result.SuccessCount)
	}
}

// AssertConflict asserts that LinearSyncFromDefault stopped due to a conflict after
// applying expectedSuccessCount commits.
func (f *rebaseFixture) AssertConflict(result *LinearSyncResult, err error, expectedSuccessCount int) {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("expected conflict result (not error), got error: %v", err)
	}
	if result == nil {
		f.t.Fatal("expected non-nil result")
	}
	if result.Success {
		f.t.Error("expected Success=false (conflict), got true")
	}
	if result.ConflictingHash == "" {
		f.t.Error("expected non-empty ConflictingHash")
	}
	if result.SuccessCount != expectedSuccessCount {
		f.t.Errorf("expected SuccessCount=%d before conflict, got %d", expectedSuccessCount, result.SuccessCount)
	}
}

// AssertResolveSuccess asserts that LinearSyncResolveConflict completed successfully.
func (f *rebaseFixture) AssertResolveSuccess(result *LinearSyncResolveConflictResult, err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("expected resolve success, got error: %v", err)
	}
	if result == nil {
		f.t.Fatal("expected non-nil result")
	}
	if !result.Success {
		f.t.Errorf("expected Success=true, got false (message=%s)", result.Message)
	}
}

// AssertResolveFailure asserts that LinearSyncResolveConflict failed with a message
// containing the given substring.
func (f *rebaseFixture) AssertResolveFailure(result *LinearSyncResolveConflictResult, err error, msgSubstring string) {
	f.t.Helper()
	if err != nil {
		// An error return (not just Success=false) is also a valid failure
		if msgSubstring != "" && !strings.Contains(err.Error(), msgSubstring) {
			f.t.Errorf("expected error containing %q, got: %v", msgSubstring, err)
		}
		return
	}
	if result == nil {
		f.t.Fatal("expected non-nil result")
	}
	if result.Success {
		f.t.Error("expected Success=false, got true")
	}
	if msgSubstring != "" && !strings.Contains(result.Message, msgSubstring) {
		f.t.Errorf("expected message containing %q, got: %q", msgSubstring, result.Message)
	}
}

// AssertErrorContains asserts that an error was returned and its message contains the substring.
func (f *rebaseFixture) AssertErrorContains(err error, substring string) {
	f.t.Helper()
	if err == nil {
		f.t.Fatalf("expected error containing %q, got nil", substring)
	}
	if !strings.Contains(err.Error(), substring) {
		f.t.Errorf("expected error containing %q, got: %v", substring, err)
	}
}

// AssertLocalChangesPreserved verifies that untracked files, unstaged modifications,
// and staged changes survived the sync operation.
func (f *rebaseFixture) AssertLocalChangesPreserved() {
	f.t.Helper()

	// Check untracked files exist with expected content
	for name, expectedContent := range f.untrackedFiles {
		path := filepath.Join(f.cloneDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			f.t.Errorf("untracked file %q not found after sync: %v", name, err)
			continue
		}
		if string(data) != expectedContent {
			f.t.Errorf("untracked file %q content mismatch: got %q, want %q", name, string(data), expectedContent)
		}
	}

	// Check unstaged modifications appear in git diff
	if len(f.unstagedMods) > 0 {
		cmd := exec.Command("git", "diff", "--name-only")
		cmd.Dir = f.cloneDir
		out, err := cmd.Output()
		if err != nil {
			f.t.Errorf("git diff --name-only failed: %v", err)
		} else {
			diffFiles := strings.TrimSpace(string(out))
			for name := range f.unstagedMods {
				if !strings.Contains(diffFiles, name) {
					f.t.Errorf("expected unstaged modification to %q in git diff, not found in: %s", name, diffFiles)
				}
			}
		}
	}

	// Check staged changes appear in git diff --cached
	if len(f.stagedChanges) > 0 {
		cmd := exec.Command("git", "diff", "--cached", "--name-only")
		cmd.Dir = f.cloneDir
		out, err := cmd.Output()
		if err != nil {
			f.t.Errorf("git diff --cached --name-only failed: %v", err)
		} else {
			cachedFiles := strings.TrimSpace(string(out))
			for name := range f.stagedChanges {
				if !strings.Contains(cachedFiles, name) {
					f.t.Errorf("expected staged change to %q in git diff --cached, not found in: %s", name, cachedFiles)
				}
			}
		}
	}
}

// AssertAncestorOf asserts that the ancestor ref is an ancestor of the descendant ref.
func (f *rebaseFixture) AssertAncestorOf(ancestor, descendant string) {
	f.t.Helper()
	cmd := exec.Command("git", "merge-base", "--is-ancestor", ancestor, descendant)
	cmd.Dir = f.cloneDir
	if err := cmd.Run(); err != nil {
		f.t.Errorf("expected %q to be ancestor of %q, but it is not", ancestor, descendant)
	}
}

// AssertHeadContains asserts that the HEAD commit message contains the given substring.
func (f *rebaseFixture) AssertHeadContains(substring string) {
	f.t.Helper()
	cmd := exec.Command("git", "log", "-1", "--format=%s")
	cmd.Dir = f.cloneDir
	out, err := cmd.Output()
	if err != nil {
		f.t.Fatalf("git log -1 --format=%%s failed: %v", err)
	}
	msg := strings.TrimSpace(string(out))
	if !strings.Contains(msg, substring) {
		f.t.Errorf("expected HEAD message to contain %q, got %q", substring, msg)
	}
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

// gitLogMessages returns the last n commit messages (subject line only).
func (f *rebaseFixture) gitLogMessages(n int) []string {
	f.t.Helper()
	cmd := exec.Command("git", "log", fmt.Sprintf("-%d", n), "--format=%s")
	cmd.Dir = f.cloneDir
	out, err := cmd.Output()
	if err != nil {
		f.t.Fatalf("git log -%d --format=%%s failed: %v", n, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// remoteMainHash returns the commit hash of origin/main.
func (f *rebaseFixture) remoteMainHash() string {
	f.t.Helper()
	return gitCommitHash(f.t, f.cloneDir, "origin/main")
}

// ---------------------------------------------------------------------------
// Mock LLM conflict resolver factories
// ---------------------------------------------------------------------------

// mockLLMResolveAll returns a ConflictResolverFunc that writes each file's content
// to disk (or deletes if content is empty), then returns AllResolved:true, Confidence:"high".
func mockLLMResolveAll(resolvedContents map[string]string) ConflictResolverFunc {
	return func(_ context.Context, _ *config.Config, _ string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		files := make(map[string]conflictresolve.FileAction, len(resolvedContents))
		for name, content := range resolvedContents {
			fullPath := filepath.Join(workspacePath, name)
			if content == "" {
				// Delete the file
				os.Remove(fullPath)
				files[name] = conflictresolve.FileAction{Action: "deleted", Description: "Resolved by deletion"}
			} else {
				// Write resolved content
				if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
					return conflictresolve.OneshotResult{}, "", fmt.Errorf("mock: mkdir failed: %w", err)
				}
				if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
					return conflictresolve.OneshotResult{}, "", fmt.Errorf("mock: write failed: %w", err)
				}
				files[name] = conflictresolve.FileAction{Action: "modified", Description: "Resolved by merging both sides"}
			}
		}
		return conflictresolve.OneshotResult{
			AllResolved: true,
			Confidence:  "high",
			Summary:     "All conflicts resolved",
			Files:       files,
		}, "", nil
	}
}

// mockLLMLowConfidence writes resolved content to disk but returns Confidence:"medium".
func mockLLMLowConfidence(resolvedContents map[string]string) ConflictResolverFunc {
	return func(_ context.Context, _ *config.Config, _ string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		files := make(map[string]conflictresolve.FileAction, len(resolvedContents))
		for name, content := range resolvedContents {
			fullPath := filepath.Join(workspacePath, name)
			if content == "" {
				os.Remove(fullPath)
				files[name] = conflictresolve.FileAction{Action: "deleted", Description: "Resolved by deletion"}
			} else {
				if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
					return conflictresolve.OneshotResult{}, "", fmt.Errorf("mock: mkdir failed: %w", err)
				}
				if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
					return conflictresolve.OneshotResult{}, "", fmt.Errorf("mock: write failed: %w", err)
				}
				files[name] = conflictresolve.FileAction{Action: "modified", Description: "Resolved with low confidence"}
			}
		}
		return conflictresolve.OneshotResult{
			AllResolved: true,
			Confidence:  "medium",
			Summary:     "Resolved with medium confidence",
			Files:       files,
		}, "", nil
	}
}

// mockLLMNotAllResolved writes resolved content to disk but returns AllResolved:false.
func mockLLMNotAllResolved(resolvedContents map[string]string) ConflictResolverFunc {
	return func(_ context.Context, _ *config.Config, _ string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		files := make(map[string]conflictresolve.FileAction, len(resolvedContents))
		for name, content := range resolvedContents {
			fullPath := filepath.Join(workspacePath, name)
			if content == "" {
				os.Remove(fullPath)
				files[name] = conflictresolve.FileAction{Action: "deleted", Description: "Resolved by deletion"}
			} else {
				if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
					return conflictresolve.OneshotResult{}, "", fmt.Errorf("mock: mkdir failed: %w", err)
				}
				if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
					return conflictresolve.OneshotResult{}, "", fmt.Errorf("mock: write failed: %w", err)
				}
				files[name] = conflictresolve.FileAction{Action: "modified", Description: "Partially resolved"}
			}
		}
		return conflictresolve.OneshotResult{
			AllResolved: false,
			Confidence:  "high",
			Summary:     "Not all conflicts could be resolved",
			Files:       files,
		}, "", nil
	}
}

// mockLLMError returns a ConflictResolverFunc that always returns an error.
func mockLLMError() ConflictResolverFunc {
	return func(_ context.Context, _ *config.Config, _ string, _ string) (conflictresolve.OneshotResult, string, error) {
		return conflictresolve.OneshotResult{}, "", fmt.Errorf("LLM service unavailable")
	}
}

// mockLLMOmitsFile resolves everything except omitFile (skips it from the Files map).
func mockLLMOmitsFile(resolvedContents map[string]string, omitFile string) ConflictResolverFunc {
	return func(_ context.Context, _ *config.Config, _ string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		files := make(map[string]conflictresolve.FileAction, len(resolvedContents))
		for name, content := range resolvedContents {
			fullPath := filepath.Join(workspacePath, name)
			if name == omitFile {
				// Still write the file to disk (so it doesn't have markers), but omit from response
				if content != "" {
					if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
						return conflictresolve.OneshotResult{}, "", fmt.Errorf("mock: mkdir failed: %w", err)
					}
					if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
						return conflictresolve.OneshotResult{}, "", fmt.Errorf("mock: write failed: %w", err)
					}
				}
				continue // omit from Files map
			}
			if content == "" {
				os.Remove(fullPath)
				files[name] = conflictresolve.FileAction{Action: "deleted", Description: "Resolved by deletion"}
			} else {
				if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
					return conflictresolve.OneshotResult{}, "", fmt.Errorf("mock: mkdir failed: %w", err)
				}
				if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
					return conflictresolve.OneshotResult{}, "", fmt.Errorf("mock: write failed: %w", err)
				}
				files[name] = conflictresolve.FileAction{Action: "modified", Description: "Resolved"}
			}
		}
		return conflictresolve.OneshotResult{
			AllResolved: true,
			Confidence:  "high",
			Summary:     "Resolved (omitting one file)",
			Files:       files,
		}, "", nil
	}
}

// mockLLMDeletedButExists claims a file is "deleted" but writes content to it instead.
func mockLLMDeletedButExists(file string) ConflictResolverFunc {
	return func(_ context.Context, _ *config.Config, _ string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		fullPath := filepath.Join(workspacePath, file)
		// Write content instead of deleting — production should catch this
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return conflictresolve.OneshotResult{}, "", fmt.Errorf("mock: mkdir failed: %w", err)
		}
		if err := os.WriteFile(fullPath, []byte("still here!"), 0644); err != nil {
			return conflictresolve.OneshotResult{}, "", fmt.Errorf("mock: write failed: %w", err)
		}
		return conflictresolve.OneshotResult{
			AllResolved: true,
			Confidence:  "high",
			Summary:     "Deleted file (but actually didn't)",
			Files: map[string]conflictresolve.FileAction{
				file: {Action: "deleted", Description: "File removed"},
			},
		}, "", nil
	}
}

// mockLLMLeaveMarkers writes conflict markers to a file but claims Action:"modified".
func mockLLMLeaveMarkers(file string) ConflictResolverFunc {
	return func(_ context.Context, _ *config.Config, _ string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		fullPath := filepath.Join(workspacePath, file)
		markerContent := "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> branch\n"
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return conflictresolve.OneshotResult{}, "", fmt.Errorf("mock: mkdir failed: %w", err)
		}
		if err := os.WriteFile(fullPath, []byte(markerContent), 0644); err != nil {
			return conflictresolve.OneshotResult{}, "", fmt.Errorf("mock: write failed: %w", err)
		}
		return conflictresolve.OneshotResult{
			AllResolved: true,
			Confidence:  "high",
			Summary:     "Resolved (but left markers)",
			Files: map[string]conflictresolve.FileAction{
				file: {Action: "modified", Description: "Merged changes"},
			},
		}, "", nil
	}
}

// mockLLMUnknownAction returns Action:"renamed" (invalid) for a file.
func mockLLMUnknownAction(file string) ConflictResolverFunc {
	return func(_ context.Context, _ *config.Config, _ string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		fullPath := filepath.Join(workspacePath, file)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return conflictresolve.OneshotResult{}, "", fmt.Errorf("mock: mkdir failed: %w", err)
		}
		if err := os.WriteFile(fullPath, []byte("resolved content"), 0644); err != nil {
			return conflictresolve.OneshotResult{}, "", fmt.Errorf("mock: write failed: %w", err)
		}
		return conflictresolve.OneshotResult{
			AllResolved: true,
			Confidence:  "high",
			Summary:     "Resolved with rename action",
			Files: map[string]conflictresolve.FileAction{
				file: {Action: "renamed", Description: "File renamed"},
			},
		}, "", nil
	}
}

// mockLLMSequential returns successive results from provided mocks on each call.
// Thread-safe. Errors if called more times than mocks provided.
func mockLLMSequential(mocks ...ConflictResolverFunc) ConflictResolverFunc {
	var mu sync.Mutex
	callIdx := 0
	return func(ctx context.Context, cfg *config.Config, prompt string, workspacePath string) (conflictresolve.OneshotResult, string, error) {
		mu.Lock()
		idx := callIdx
		callIdx++
		mu.Unlock()
		if idx >= len(mocks) {
			return conflictresolve.OneshotResult{}, "", fmt.Errorf("mockLLMSequential: called %d times but only %d mocks provided", idx+1, len(mocks))
		}
		return mocks[idx](ctx, cfg, prompt, workspacePath)
	}
}
