# Federated Document Store Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a per-repo federated document store that synchronizes markdown files across schmux installations via a special orphan git branch.

**Architecture:** A standalone `internal/docstore/` package provides Get/Put/Remove/List operations on markdown files stored in `<schema>/<filename>.md` on an orphan `schmux` branch. Each repo's bare clone gets a persistent worktree for this branch at `~/.schmux/docs/<repo>/`. The existing git poller fetches and fast-forwards this branch periodically.

**Tech Stack:** Go, git CLI, existing `internal/state` and `internal/config` packages.

---

### Task 1: Create Store Interface and Types

**Files:**

- Create: `internal/docstore/store.go`

**Step 1: Write the interface and constructor stub**

```go
package docstore

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/nicois/slog/log"

	"github.com/schmux/schmux/internal/config"
	"github.com/schmux/schmux/internal/state"
)

// Store provides per-repo federated document storage backed by an orphan git branch.
type Store interface {
	Get(ctx context.Context, repoURL, schema, filename string) (string, error)
	Remove(ctx context.Context, repoURL, schema, filename string) error
	Put(ctx context.Context, repoURL, schema, filename, data string) error
	List(ctx context.Context, repoURL, schema string) ([]string, error)
	Refresh(ctx context.Context, repoURL string) error
	RefreshAll(ctx context.Context)
}

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func validateName(kind, name string) error {
	if name == "" {
		return fmt.Errorf("docstore: %s must not be empty", kind)
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("docstore: %s must not contain path separators", kind)
	}
	if !validName.MatchString(name) {
		return fmt.Errorf("docstore: %s must be lowercase alphanumeric with hyphens/underscores: %q", kind, name)
	}
	return nil
}

type store struct {
	config *config.Config
	state  state.StateStore
	logger *log.Logger
	mu     sync.Map // per-repo mutexes: repoURL → *sync.Mutex
}

func New(cfg *config.Config, st state.StateStore, logger *log.Logger) Store {
	return &store{
		config: cfg,
		state:  st,
		logger: logger,
	}
}

func (s *store) repoMutex(repoURL string) *sync.Mutex {
	val, _ := s.mu.LoadOrStore(repoURL, &sync.Mutex{})
	return val.(*sync.Mutex)
}
```

**Step 2: Commit**

```
feat(docstore): add Store interface, types, and constructor
```

---

### Task 2: Worktree Path Resolution and Git Helpers

**Files:**

- Modify: `internal/docstore/store.go`

**Step 1: Add worktree path resolution and git runner**

The docstore worktree lives at `~/.schmux/docs/<bare-path-stem>/` (bare path minus `.git` suffix). For example, bare path `schmux.git` → `~/.schmux/docs/schmux/`, bare path `facebook/react.git` → `~/.schmux/docs/facebook/react/`.

```go
func (s *store) docsBasePath() string {
	base := s.config.GetWorktreeBasePath() // ~/.schmux/repos
	return filepath.Join(filepath.Dir(base), "docs")
}

// worktreePath returns the persistent worktree path for a repo's docstore branch.
// Returns ("", error) if the repo has no bare clone.
func (s *store) worktreePath(repoURL string) (string, error) {
	wb, ok := s.state.GetWorktreeBaseByURL(repoURL)
	if !ok {
		return "", fmt.Errorf("docstore: no worktree base for repo %q", repoURL)
	}
	stem := strings.TrimSuffix(filepath.Base(wb.Path), ".git")
	dir := filepath.Dir(wb.Path)
	// For namespaced paths like "facebook/react.git", include the namespace
	relFromRepos := strings.TrimPrefix(wb.Path, s.config.GetWorktreeBasePath())
	relFromRepos = strings.TrimPrefix(relFromRepos, string(filepath.Separator))
	relStem := strings.TrimSuffix(relFromRepos, ".git")
	if relStem == "" {
		relStem = stem
	}
	_ = dir // suppress unused
	return filepath.Join(s.docsBasePath(), relStem), nil
}

// bareClonePath returns the bare clone path for a repo.
func (s *store) bareClonePath(repoURL string) (string, error) {
	wb, ok := s.state.GetWorktreeBaseByURL(repoURL)
	if !ok {
		return "", fmt.Errorf("docstore: no worktree base for repo %q", repoURL)
	}
	return wb.Path, nil
}

func (s *store) runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return out, nil
}
```

**Step 2: Commit**

```
feat(docstore): add worktree path resolution and git helpers
```

---

### Task 3: Branch Bootstrap

**Files:**

- Modify: `internal/docstore/store.go`

**Step 1: Write the failing test**

Create `internal/docstore/store_test.go`:

```go
package docstore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nicois/slog/log"

	"github.com/schmux/schmux/internal/config"
	"github.com/schmux/schmux/internal/state"
)

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

// createTestRepo creates a bare "remote" repo and returns its path.
func createTestRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "remote.git")
	run(t, "", "git", "init", "--bare", dir)
	// Create an initial commit so the repo isn't empty.
	work := filepath.Join(t.TempDir(), "work")
	run(t, "", "git", "clone", dir, work)
	run(t, work, "git", "config", "user.email", "test@test.com")
	run(t, work, "git", "config", "user.name", "Test")
	writeTestFile(t, work, "README.md", "# test")
	run(t, work, "git", "add", ".")
	run(t, work, "git", "commit", "-m", "init")
	run(t, work, "git", "push", "origin", "main")
	return dir
}

// createBareClone clones the remote as a bare repo (mimicking schmux's worktree base).
func createBareClone(t *testing.T, remoteDir string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo.git")
	run(t, "", "git", "clone", "--bare", remoteDir, dir)
	run(t, dir, "git", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	return dir
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func testStore(t *testing.T, bareClonePath, repoURL string) Store {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := &config.Config{
		WorktreeBasePath: filepath.Join(tmpDir, "repos"),
	}
	st := state.New(filepath.Join(tmpDir, "state.json"), nil)
	st.AddWorktreeBase(state.WorktreeBase{
		RepoURL: repoURL,
		Path:    bareClonePath,
	})
	logger := log.NewWithOptions(os.Stderr, log.Options{})
	s := New(cfg, st, logger)
	// Override docsBasePath to use temp dir
	s.(*store).config = &config.Config{
		WorktreeBasePath: filepath.Dir(bareClonePath),
	}
	return s
}

func TestBootstrap_CreatesOrphanBranch(t *testing.T) {
	skipIfNoGit(t)
	remote := createTestRepo(t)
	bare := createBareClone(t, remote)
	repoURL := remote

	s := testStore(t, bare, repoURL)

	// Put should bootstrap the branch
	err := s.Put(context.Background(), repoURL, "notes", "hello", "# Hello\n")
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify the file exists via Get
	content, err := s.Get(context.Background(), repoURL, "notes", "hello")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if content != "# Hello\n" {
		t.Errorf("got %q, want %q", content, "# Hello\n")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/docstore/ -run TestBootstrap -v`
Expected: FAIL (methods not implemented yet)

**Step 3: Implement bootstrap**

Add to `store.go`:

```go
const branchName = "schmux"

// ensureWorktree ensures the persistent docstore worktree exists for a repo.
// Creates the orphan branch if it doesn't exist locally or on the remote.
func (s *store) ensureWorktree(ctx context.Context, repoURL string) (string, error) {
	wtPath, err := s.worktreePath(repoURL)
	if err != nil {
		return "", err
	}
	barePath, err := s.bareClonePath(repoURL)
	if err != nil {
		return "", err
	}

	// Already set up?
	if _, err := os.Stat(filepath.Join(wtPath, ".git")); err == nil {
		return wtPath, nil
	}

	if err := os.MkdirAll(filepath.Dir(wtPath), 0755); err != nil {
		return "", fmt.Errorf("docstore: mkdir: %w", err)
	}

	// Fetch to see if remote has the branch.
	s.runGit(ctx, barePath, "fetch", "origin") // ignore error; remote may be unreachable

	// Check if branch exists on remote.
	_, errRemote := s.runGit(ctx, barePath, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branchName)
	// Check if branch exists locally.
	_, errLocal := s.runGit(ctx, barePath, "show-ref", "--verify", "--quiet", "refs/heads/"+branchName)

	if errRemote == nil || errLocal == nil {
		// Branch exists — add worktree tracking it.
		if errLocal != nil && errRemote == nil {
			// Create local branch from remote.
			s.runGit(ctx, barePath, "branch", branchName, "origin/"+branchName)
		}
		_, err := s.runGit(ctx, barePath, "worktree", "add", wtPath, branchName)
		if err != nil {
			return "", fmt.Errorf("docstore: worktree add: %w", err)
		}
		return wtPath, nil
	}

	// Branch doesn't exist anywhere — create orphan.
	_, err = s.runGit(ctx, barePath, "worktree", "add", "--detach", wtPath)
	if err != nil {
		return "", fmt.Errorf("docstore: worktree add detach: %w", err)
	}
	s.runGit(ctx, wtPath, "checkout", "--orphan", branchName)
	// Remove any files that came from the detached HEAD.
	s.runGit(ctx, wtPath, "rm", "-rf", "--ignore-unmatch", ".")
	_, err = s.runGit(ctx, wtPath, "commit", "--allow-empty", "-m", "docstore: initialize")
	if err != nil {
		return "", fmt.Errorf("docstore: init commit: %w", err)
	}

	// Configure author for future commits in this worktree.
	s.runGit(ctx, wtPath, "config", "user.email", "schmux@localhost")
	s.runGit(ctx, wtPath, "config", "user.name", "schmux")

	// Push the new branch to origin.
	_, pushErr := s.runGit(ctx, wtPath, "push", "origin", branchName)
	if pushErr != nil {
		s.logger.Warn("docstore: failed to push new branch to origin", "err", pushErr)
	}

	return wtPath, nil
}
```

**Step 4: Run test to verify it still fails** (Put/Get not implemented yet — that's next)

**Step 5: Commit**

```
feat(docstore): add branch bootstrap — creates orphan schmux branch
```

---

### Task 4: Implement Put

**Files:**

- Modify: `internal/docstore/store.go`

**Step 1: Implement Put**

```go
func (s *store) Put(ctx context.Context, repoURL, schema, filename, data string) error {
	if err := validateName("schema", schema); err != nil {
		return err
	}
	if err := validateName("filename", filename); err != nil {
		return err
	}

	mu := s.repoMutex(repoURL)
	mu.Lock()
	defer mu.Unlock()

	wtPath, err := s.ensureWorktree(ctx, repoURL)
	if err != nil {
		return err
	}

	// Write the file.
	dir := filepath.Join(wtPath, schema)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("docstore: mkdir %s: %w", dir, err)
	}
	relPath := filepath.Join(schema, filename+".md")
	fullPath := filepath.Join(wtPath, relPath)
	if err := os.WriteFile(fullPath, []byte(data), 0644); err != nil {
		return fmt.Errorf("docstore: write: %w", err)
	}

	// Stage, commit, pull, push.
	if _, err := s.runGit(ctx, wtPath, "add", relPath); err != nil {
		return fmt.Errorf("docstore: git add: %w", err)
	}
	if _, err := s.runGit(ctx, wtPath, "commit", "-m", fmt.Sprintf("docstore: put %s/%s", schema, filename)); err != nil {
		return fmt.Errorf("docstore: git commit: %w", err)
	}
	// Pull to incorporate remote changes before pushing.
	s.runGit(ctx, wtPath, "pull", "--rebase", "origin", branchName) // best-effort
	// Push with force-with-lease.
	if _, err := s.runGit(ctx, wtPath, "push", "--force-with-lease", "origin", branchName); err != nil {
		return fmt.Errorf("docstore: push failed (remote may have diverged): %w", err)
	}
	return nil
}
```

**Step 2: Run test**

Run: `go test ./internal/docstore/ -run TestBootstrap -v`
Expected: FAIL (Get not implemented yet)

**Step 3: Commit**

```
feat(docstore): implement Put — write, commit, push
```

---

### Task 5: Implement Get and List

**Files:**

- Modify: `internal/docstore/store.go`

**Step 1: Implement Get and List**

```go
func (s *store) Get(ctx context.Context, repoURL, schema, filename string) (string, error) {
	if err := validateName("schema", schema); err != nil {
		return "", err
	}
	if err := validateName("filename", filename); err != nil {
		return "", err
	}

	wtPath, err := s.ensureWorktree(ctx, repoURL)
	if err != nil {
		return "", err
	}

	fullPath := filepath.Join(wtPath, schema, filename+".md")
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("docstore: %s/%s not found", schema, filename)
		}
		return "", fmt.Errorf("docstore: read: %w", err)
	}
	return string(data), nil
}

func (s *store) List(ctx context.Context, repoURL, schema string) ([]string, error) {
	if err := validateName("schema", schema); err != nil {
		return nil, err
	}

	wtPath, err := s.ensureWorktree(ctx, repoURL)
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(wtPath, schema)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // empty schema is not an error
		}
		return nil, fmt.Errorf("docstore: readdir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".md") {
			names = append(names, strings.TrimSuffix(name, ".md"))
		}
	}
	return names, nil
}
```

**Step 2: Run test**

Run: `go test ./internal/docstore/ -run TestBootstrap -v`
Expected: PASS

**Step 3: Commit**

```
feat(docstore): implement Get and List — read from local worktree
```

---

### Task 6: Implement Remove

**Files:**

- Modify: `internal/docstore/store.go`
- Modify: `internal/docstore/store_test.go`

**Step 1: Write the failing test**

Add to `store_test.go`:

```go
func TestRemove(t *testing.T) {
	skipIfNoGit(t)
	remote := createTestRepo(t)
	bare := createBareClone(t, remote)
	repoURL := remote

	s := testStore(t, bare, repoURL)
	ctx := context.Background()

	// Put a doc, then remove it.
	if err := s.Put(ctx, repoURL, "notes", "temp", "temporary"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Remove(ctx, repoURL, "notes", "temp"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Get should fail.
	_, err := s.Get(ctx, repoURL, "notes", "temp")
	if err == nil {
		t.Fatal("expected error after Remove, got nil")
	}

	// List should not include it.
	names, err := s.List(ctx, repoURL, "notes")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, n := range names {
		if n == "temp" {
			t.Fatal("removed file still appears in List")
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/docstore/ -run TestRemove -v`
Expected: FAIL (Remove not implemented)

**Step 3: Implement Remove**

```go
func (s *store) Remove(ctx context.Context, repoURL, schema, filename string) error {
	if err := validateName("schema", schema); err != nil {
		return err
	}
	if err := validateName("filename", filename); err != nil {
		return err
	}

	mu := s.repoMutex(repoURL)
	mu.Lock()
	defer mu.Unlock()

	wtPath, err := s.ensureWorktree(ctx, repoURL)
	if err != nil {
		return err
	}

	relPath := filepath.Join(schema, filename+".md")
	fullPath := filepath.Join(wtPath, relPath)

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return fmt.Errorf("docstore: %s/%s not found", schema, filename)
	}

	if _, err := s.runGit(ctx, wtPath, "rm", relPath); err != nil {
		return fmt.Errorf("docstore: git rm: %w", err)
	}
	if _, err := s.runGit(ctx, wtPath, "commit", "-m", fmt.Sprintf("docstore: remove %s/%s", schema, filename)); err != nil {
		return fmt.Errorf("docstore: git commit: %w", err)
	}
	s.runGit(ctx, wtPath, "pull", "--rebase", "origin", branchName)
	if _, err := s.runGit(ctx, wtPath, "push", "--force-with-lease", "origin", branchName); err != nil {
		return fmt.Errorf("docstore: push failed (remote may have diverged): %w", err)
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/docstore/ -run TestRemove -v`
Expected: PASS

**Step 5: Commit**

```
feat(docstore): implement Remove — delete, commit, push
```

---

### Task 7: Implement Refresh and RefreshAll

**Files:**

- Modify: `internal/docstore/store.go`
- Modify: `internal/docstore/store_test.go`

**Step 1: Write the failing test**

Add to `store_test.go`:

```go
func TestRefresh_PullsRemoteChanges(t *testing.T) {
	skipIfNoGit(t)
	remote := createTestRepo(t)
	bare1 := createBareClone(t, remote)
	bare2 := createBareClone(t, remote)
	repoURL := remote

	s1 := testStore(t, bare1, repoURL)
	s2 := testStore(t, bare2, repoURL)
	ctx := context.Background()

	// s1 writes a doc.
	if err := s1.Put(ctx, repoURL, "notes", "shared", "from s1"); err != nil {
		t.Fatalf("s1.Put: %v", err)
	}

	// s2 refreshes and should see it.
	if err := s2.Refresh(ctx, repoURL); err != nil {
		t.Fatalf("s2.Refresh: %v", err)
	}
	content, err := s2.Get(ctx, repoURL, "notes", "shared")
	if err != nil {
		t.Fatalf("s2.Get: %v", err)
	}
	if content != "from s1" {
		t.Errorf("got %q, want %q", content, "from s1")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/docstore/ -run TestRefresh -v`
Expected: FAIL

**Step 3: Implement Refresh and RefreshAll**

```go
func (s *store) Refresh(ctx context.Context, repoURL string) error {
	wtPath, err := s.worktreePath(repoURL)
	if err != nil {
		return err
	}
	barePath, err := s.bareClonePath(repoURL)
	if err != nil {
		return err
	}

	// If worktree doesn't exist yet, nothing to refresh.
	if _, err := os.Stat(filepath.Join(wtPath, ".git")); os.IsNotExist(err) {
		return nil
	}

	// Fetch in bare clone.
	if _, err := s.runGit(ctx, barePath, "fetch", "origin"); err != nil {
		return fmt.Errorf("docstore: fetch: %w", err)
	}

	// Check if remote branch exists.
	if _, err := s.runGit(ctx, barePath, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branchName); err != nil {
		return nil // remote branch doesn't exist, nothing to refresh
	}

	// Reset worktree to match remote.
	if _, err := s.runGit(ctx, wtPath, "fetch", "origin", branchName); err != nil {
		return fmt.Errorf("docstore: worktree fetch: %w", err)
	}
	if _, err := s.runGit(ctx, wtPath, "reset", "--hard", "origin/"+branchName); err != nil {
		return fmt.Errorf("docstore: reset: %w", err)
	}
	return nil
}

func (s *store) RefreshAll(ctx context.Context) {
	bases := s.state.GetWorktreeBases()
	var wg sync.WaitGroup
	for _, wb := range bases {
		wb := wb
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Refresh(ctx, wb.RepoURL); err != nil {
				s.logger.Warn("docstore: refresh failed", "repo", wb.RepoURL, "err", err)
			}
		}()
	}
	wg.Wait()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/docstore/ -run TestRefresh -v`
Expected: PASS

**Step 5: Commit**

```
feat(docstore): implement Refresh/RefreshAll — pull remote changes
```

---

### Task 8: Validation Tests

**Files:**

- Modify: `internal/docstore/store_test.go`

**Step 1: Write validation tests**

```go
func TestValidation(t *testing.T) {
	skipIfNoGit(t)
	remote := createTestRepo(t)
	bare := createBareClone(t, remote)
	repoURL := remote
	s := testStore(t, bare, repoURL)
	ctx := context.Background()

	tests := []struct {
		name     string
		schema   string
		filename string
		wantErr  bool
	}{
		{"valid", "notes", "hello", false},
		{"empty schema", "", "hello", true},
		{"empty filename", "notes", "", true},
		{"schema with slash", "no/slashes", "hello", true},
		{"filename with slash", "notes", "no/slashes", true},
		{"uppercase schema", "Notes", "hello", true},
		{"spaces", "notes", "hello world", true},
		{"dots only", "..", "hello", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.Put(ctx, repoURL, tt.schema, tt.filename, "test")
			if (err != nil) != tt.wantErr {
				t.Errorf("Put(%q, %q): err=%v, wantErr=%v", tt.schema, tt.filename, err, tt.wantErr)
			}
		})
	}
}

func TestList_EmptySchema(t *testing.T) {
	skipIfNoGit(t)
	remote := createTestRepo(t)
	bare := createBareClone(t, remote)
	repoURL := remote
	s := testStore(t, bare, repoURL)
	ctx := context.Background()

	// List a schema that doesn't exist — should return nil, not error.
	names, err := s.List(ctx, repoURL, "nonexistent")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if names != nil {
		t.Errorf("expected nil, got %v", names)
	}
}

func TestPutAndList_MultipleFiles(t *testing.T) {
	skipIfNoGit(t)
	remote := createTestRepo(t)
	bare := createBareClone(t, remote)
	repoURL := remote
	s := testStore(t, bare, repoURL)
	ctx := context.Background()

	s.Put(ctx, repoURL, "tasks", "alpha", "# Alpha")
	s.Put(ctx, repoURL, "tasks", "beta", "# Beta")
	s.Put(ctx, repoURL, "notes", "gamma", "# Gamma")

	tasks, err := s.List(ctx, repoURL, "tasks")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d: %v", len(tasks), tasks)
	}

	notes, err := s.List(ctx, repoURL, "notes")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(notes) != 1 {
		t.Errorf("expected 1 note, got %d: %v", len(notes), notes)
	}
}
```

**Step 2: Run all tests**

Run: `go test ./internal/docstore/ -v`
Expected: ALL PASS

**Step 3: Commit**

```
test(docstore): add validation, list, and multi-file tests
```

---

### Task 9: Wire Into Daemon Polling Loop

**Files:**

- Modify: `internal/daemon/daemon.go`

**Step 1: Add docstore field to Daemon struct**

In the `Daemon` struct (around line 65), add:

```go
docstore docstore.Store
```

Add import: `"github.com/schmux/schmux/internal/docstore"`

**Step 2: Initialize docstore in daemon startup**

Near line 461 where the workspace manager is created, add:

```go
ds := docstore.New(cfg, st, logger)
d.docstore = ds
```

**Step 3: Add RefreshAll to polling loop**

In the polling loop (around line 1101), add `ds.RefreshAll(ctx)` as another concurrent goroutine alongside the existing ones:

```go
wg.Add(1)
go func() {
	defer wg.Done()
	ds.RefreshAll(pollCtx)
}()
```

**Step 4: Build and verify**

Run: `go build ./cmd/schmux`
Expected: SUCCESS

**Step 5: Commit**

```
feat(docstore): wire into daemon — refresh on each poll cycle
```

---

### Task 10: Expose via Dashboard API

**Files:**

- Modify: `internal/dashboard/handlers.go` (or create `internal/dashboard/handlers_docstore.go`)

**Step 1: Add API endpoints**

```
GET    /api/docstore/{repoURL}/{schema}              → List
GET    /api/docstore/{repoURL}/{schema}/{filename}    → Get
PUT    /api/docstore/{repoURL}/{schema}/{filename}    → Put (body = markdown string)
DELETE /api/docstore/{repoURL}/{schema}/{filename}    → Remove
```

The `repoURL` should be URL-encoded in the path (or passed as a query parameter — check existing API patterns for the repo identifier convention).

**Step 2: Register routes in the server**

Add routes to the existing mux setup in `internal/dashboard/server.go` or wherever routes are registered.

**Step 3: Build and verify**

Run: `go build ./cmd/schmux`
Expected: SUCCESS

**Step 4: Commit**

```
feat(docstore): expose Get/Put/Remove/List via HTTP API
```

---

### Task 11: Run Full Test Suite

**Step 1: Run all tests**

Run: `./test.sh --quick`
Expected: ALL PASS

**Step 2: Fix any issues found**

**Step 3: Final commit if needed**

```
fix(docstore): address test suite issues
```
