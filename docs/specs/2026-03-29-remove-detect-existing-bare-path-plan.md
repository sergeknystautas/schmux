# Remove detectExistingBarePath — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Normalize bare repo filesystem layout so BarePath always equals `{name}.git`, add repo name uniqueness validation, and extract a reusable `relocateBareRepo` function.

**Architecture:** A new `RelocateBareRepo()` function in the config package handles the low-level rename + worktree fixup. A `NormalizeBarePaths()` function in the config package (takes state as parameter) is called from daemon startup for each non-conforming repo. Duplicate name validation is added to the two entry points that create repos.

**Tech Stack:** Go standard library (os, filepath, strings), existing config/state/workspace packages.

**Spec:** `docs/specs/2026-03-29-remove-detect-existing-bare-path-design.md`

---

### Task 1: `relocateBareRepo` — failing tests

Write tests for the standalone function that moves a bare repo and fixes up worktree `.git` files.

**Files:**

- Create: `internal/config/relocate_bare_repo.go`
- Create: `internal/config/relocate_bare_repo_test.go`

- [ ] **Step 1: Write the test file**

```go
package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// createBareRepoWithWorktrees sets up a bare repo at basePath with worktrees.
// Returns the bare repo path and a slice of worktree paths.
func createBareRepoWithWorktrees(t *testing.T, basePath string, numWorktrees int) (string, []string) {
	t.Helper()

	// Create a "remote" repo with an initial commit
	remoteDir := filepath.Join(t.TempDir(), "remote.git")
	seedDir := filepath.Join(t.TempDir(), "seed")
	runGit(t, "", "init", "--bare", remoteDir)
	runGit(t, "", "clone", remoteDir, seedDir)
	os.WriteFile(filepath.Join(seedDir, "file.txt"), []byte("hello"), 0644)
	runGit(t, seedDir, "add", "file.txt")
	runGit(t, seedDir, "commit", "-m", "initial")
	runGit(t, seedDir, "push", "origin", "main")

	// Clone bare repo at the specified path
	runGit(t, "", "clone", "--bare", remoteDir, basePath)

	// Create worktrees
	var worktrees []string
	for i := 0; i < numWorktrees; i++ {
		wtPath := filepath.Join(t.TempDir(), "worktree", "wt-"+string(rune('0'+i)))
		branch := "branch-" + string(rune('0'+i))
		runGit(t, basePath, "worktree", "add", "-b", branch, wtPath, "main")
		worktrees = append(worktrees, wtPath)
	}

	return basePath, worktrees
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func TestRelocateBareRepo_RenamesAndFixesWorktrees(t *testing.T) {
	tmpDir := t.TempDir()
	oldPath := filepath.Join(tmpDir, "repos", "facebook", "react.git")
	newPath := filepath.Join(tmpDir, "repos", "react.git")
	os.MkdirAll(filepath.Dir(oldPath), 0755)

	_, worktrees := createBareRepoWithWorktrees(t, oldPath, 2)

	// Verify worktrees point to old path before relocation
	for _, wt := range worktrees {
		data, _ := os.ReadFile(filepath.Join(wt, ".git"))
		if !strings.Contains(string(data), oldPath) {
			t.Fatalf("worktree .git should contain old path before relocation")
		}
	}

	if err := RelocateBareRepo(oldPath, newPath); err != nil {
		t.Fatalf("RelocateBareRepo() error: %v", err)
	}

	// Old path should not exist
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old path should not exist after relocation")
	}

	// New path should exist
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("new path should exist after relocation: %v", err)
	}

	// Worktree .git files should point to new path
	for _, wt := range worktrees {
		data, err := os.ReadFile(filepath.Join(wt, ".git"))
		if err != nil {
			t.Fatalf("failed to read worktree .git: %v", err)
		}
		content := string(data)
		if strings.Contains(content, oldPath) {
			t.Errorf("worktree .git still contains old path: %s", content)
		}
		if !strings.Contains(content, newPath) {
			t.Errorf("worktree .git should contain new path, got: %s", content)
		}
	}

	// Git operations should work in worktrees
	for _, wt := range worktrees {
		cmd := exec.Command("git", "status")
		cmd.Dir = wt
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("git status failed in relocated worktree: %v\n%s", err, out)
		}
	}
}

func TestRelocateBareRepo_NoBareWorktreesDir(t *testing.T) {
	// A bare repo with no worktrees/ directory should just rename without error
	tmpDir := t.TempDir()
	oldPath := filepath.Join(tmpDir, "old.git")
	newPath := filepath.Join(tmpDir, "new.git")

	_, _ = createBareRepoWithWorktrees(t, oldPath, 0)

	if err := RelocateBareRepo(oldPath, newPath); err != nil {
		t.Fatalf("RelocateBareRepo() error: %v", err)
	}

	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("new path should exist: %v", err)
	}
}

func TestRelocateBareRepo_TargetExists(t *testing.T) {
	tmpDir := t.TempDir()
	oldPath := filepath.Join(tmpDir, "old.git")
	newPath := filepath.Join(tmpDir, "new.git")

	createBareRepoWithWorktrees(t, oldPath, 0)
	os.MkdirAll(newPath, 0755) // target already exists

	err := RelocateBareRepo(oldPath, newPath)
	if err == nil {
		t.Fatal("expected error when target exists")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestRelocateBareRepo -v`
Expected: FAIL — `RelocateBareRepo` undefined

---

### Task 2: `relocateBareRepo` — implementation

**Files:**

- Create: `internal/config/relocate_bare_repo.go`

- [ ] **Step 1: Write the implementation**

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RelocateBareRepo moves a bare repo from oldPath to newPath and updates
// all worktree .git files to point to the new location.
// It does not touch config or state — the caller handles that.
func RelocateBareRepo(oldPath, newPath string) error {
	// Ensure target doesn't already exist
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("target path already exists: %s", newPath)
	}

	// Ensure parent directory of target exists
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Rename the bare repo
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("failed to rename %s to %s: %w", oldPath, newPath, err)
	}

	// Fix up worktree .git files
	worktreesDir := filepath.Join(newPath, "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No worktrees, nothing to fix
		}
		return fmt.Errorf("failed to read worktrees dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Read the gitdir file to find the worktree location
		gitdirFile := filepath.Join(worktreesDir, entry.Name(), "gitdir")
		gitdirData, err := os.ReadFile(gitdirFile)
		if err != nil {
			return fmt.Errorf("failed to read gitdir for worktree %s: %w", entry.Name(), err)
		}

		// gitdir contains the absolute path to the worktree's .git file
		worktreeGitFile := strings.TrimSpace(string(gitdirData))

		// Read the worktree's .git file
		dotGitData, err := os.ReadFile(worktreeGitFile)
		if err != nil {
			return fmt.Errorf("failed to read worktree .git file %s: %w", worktreeGitFile, err)
		}

		// Replace old path with new path in the gitdir: line
		updated := strings.Replace(string(dotGitData), oldPath, newPath, 1)
		if updated == string(dotGitData) {
			continue // Nothing to replace (shouldn't happen, but safe to skip)
		}

		if err := os.WriteFile(worktreeGitFile, []byte(updated), 0644); err != nil {
			return fmt.Errorf("failed to update worktree .git file %s: %w", worktreeGitFile, err)
		}
	}

	return nil
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestRelocateBareRepo -v`
Expected: All 3 tests PASS

- [ ] **Step 3: Commit**

```
feat: add RelocateBareRepo for moving bare repos with worktree fixup
```

---

### Task 3: `normalizeBarePaths` — failing tests

Write tests for the daemon-startup normalization function.

**Files:**

- Create: `internal/config/normalize_bare_paths.go`
- Create: `internal/config/normalize_bare_paths_test.go`

The function lives in the config package (it needs access to config internals like `GetWorktreeBasePath`, `GetQueryRepoPath`, `GetRepos`) but takes state as a parameter.

- [ ] **Step 1: Write the test file**

```go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

func TestNormalizeBarePaths_RenamesNonConforming(t *testing.T) {
	tmpDir := t.TempDir()
	reposDir := filepath.Join(tmpDir, "repos")
	configPath := filepath.Join(tmpDir, "config.json")

	// Create a bare repo at the namespaced path
	oldBarePath := filepath.Join(reposDir, "facebook", "react.git")
	createBareRepoWithWorktrees(t, oldBarePath, 1)

	// Set up config with non-conforming BarePath
	cfg := CreateDefault(configPath)
	cfg.WorktreeBasePath = reposDir
	cfg.Repos = []Repo{
		{Name: "react", URL: "https://github.com/facebook/react.git", BarePath: "facebook/react.git"},
	}
	cfg.Save()

	// Set up state with the old path
	st := state.New(filepath.Join(tmpDir, "state.json"), nil)
	st.AddRepoBase(state.RepoBase{RepoURL: "https://github.com/facebook/react.git", Path: oldBarePath})

	NormalizeBarePaths(cfg, st)

	// Config should be updated
	if cfg.Repos[0].BarePath != "react.git" {
		t.Errorf("BarePath = %q, want %q", cfg.Repos[0].BarePath, "react.git")
	}

	// Old path should not exist, new path should
	newBarePath := filepath.Join(reposDir, "react.git")
	if _, err := os.Stat(oldBarePath); !os.IsNotExist(err) {
		t.Errorf("old path should not exist")
	}
	if _, err := os.Stat(newBarePath); err != nil {
		t.Errorf("new path should exist: %v", err)
	}

	// State should be updated
	rb, found := st.GetRepoBaseByURL("https://github.com/facebook/react.git")
	if !found {
		t.Fatal("repo base should exist in state")
	}
	if rb.Path != newBarePath {
		t.Errorf("state path = %q, want %q", rb.Path, newBarePath)
	}
}

func TestNormalizeBarePaths_SkipsAlreadyConforming(t *testing.T) {
	tmpDir := t.TempDir()
	reposDir := filepath.Join(tmpDir, "repos")
	configPath := filepath.Join(tmpDir, "config.json")

	// Create a bare repo at the correct path
	correctPath := filepath.Join(reposDir, "react.git")
	createBareRepoWithWorktrees(t, correctPath, 0)

	cfg := CreateDefault(configPath)
	cfg.WorktreeBasePath = reposDir
	cfg.Repos = []Repo{
		{Name: "react", URL: "https://github.com/facebook/react.git", BarePath: "react.git"},
	}

	st := state.New(filepath.Join(tmpDir, "state.json"), nil)

	NormalizeBarePaths(cfg, st)

	// Should remain unchanged
	if cfg.Repos[0].BarePath != "react.git" {
		t.Errorf("BarePath = %q, want %q", cfg.Repos[0].BarePath, "react.git")
	}
}

func TestNormalizeBarePaths_SkipsSapling(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := CreateDefault(configPath)
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	cfg.Repos = []Repo{
		{Name: "myrepo", URL: "myrepo-id", VCS: "sapling", BarePath: "custom-path"},
	}

	st := state.New(filepath.Join(tmpDir, "state.json"), nil)

	NormalizeBarePaths(cfg, st)

	// Sapling repos should not be touched
	if cfg.Repos[0].BarePath != "custom-path" {
		t.Errorf("BarePath = %q, want %q (sapling should be skipped)", cfg.Repos[0].BarePath, "custom-path")
	}
}

func TestNormalizeBarePaths_SkipsWhenNotOnDisk(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := CreateDefault(configPath)
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	cfg.Repos = []Repo{
		{Name: "react", URL: "https://github.com/facebook/react.git", BarePath: "facebook/react.git"},
	}

	st := state.New(filepath.Join(tmpDir, "state.json"), nil)

	NormalizeBarePaths(cfg, st)

	// Nothing on disk, BarePath should remain unchanged
	if cfg.Repos[0].BarePath != "facebook/react.git" {
		t.Errorf("BarePath = %q, want %q (should skip when not on disk)", cfg.Repos[0].BarePath, "facebook/react.git")
	}
}

func TestNormalizeBarePaths_SkipsOnCollision(t *testing.T) {
	tmpDir := t.TempDir()
	reposDir := filepath.Join(tmpDir, "repos")
	configPath := filepath.Join(tmpDir, "config.json")

	// Create bare repos at both old and target paths
	oldPath := filepath.Join(reposDir, "facebook", "react.git")
	targetPath := filepath.Join(reposDir, "react.git")
	createBareRepoWithWorktrees(t, oldPath, 0)
	createBareRepoWithWorktrees(t, targetPath, 0)

	cfg := CreateDefault(configPath)
	cfg.WorktreeBasePath = reposDir
	cfg.Repos = []Repo{
		{Name: "react", URL: "https://github.com/facebook/react.git", BarePath: "facebook/react.git"},
	}

	st := state.New(filepath.Join(tmpDir, "state.json"), nil)

	NormalizeBarePaths(cfg, st)

	// Should not rename — target already exists
	if cfg.Repos[0].BarePath != "facebook/react.git" {
		t.Errorf("BarePath = %q, want %q (should skip on collision)", cfg.Repos[0].BarePath, "facebook/react.git")
	}
}

func TestNormalizeBarePaths_NormalizesQueryDir(t *testing.T) {
	tmpDir := t.TempDir()
	// Simulate ~/.schmux layout
	schmuxDir := filepath.Join(tmpDir, ".schmux")
	reposDir := filepath.Join(schmuxDir, "repos")
	queryDir := filepath.Join(schmuxDir, "query")
	configPath := filepath.Join(schmuxDir, "config.json")

	// Create a bare repo in query/ at the non-conforming path
	oldQueryPath := filepath.Join(queryDir, "facebook", "react.git")
	createBareRepoWithWorktrees(t, oldQueryPath, 0)

	cfg := CreateDefault(configPath)
	cfg.WorktreeBasePath = reposDir
	cfg.Repos = []Repo{
		{Name: "react", URL: "https://github.com/facebook/react.git", BarePath: "facebook/react.git"},
	}

	st := state.New(filepath.Join(schmuxDir, "state.json"), nil)

	NormalizeBarePaths(cfg, st)

	// Query repo should be renamed
	newQueryPath := filepath.Join(queryDir, "react.git")
	if _, err := os.Stat(oldQueryPath); !os.IsNotExist(err) {
		t.Errorf("old query path should not exist")
	}
	if _, err := os.Stat(newQueryPath); err != nil {
		t.Errorf("new query path should exist: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestNormalizeBarePaths -v`
Expected: FAIL — `NormalizeBarePaths` undefined

---

### Task 4: `normalizeBarePaths` — implementation

**Files:**

- Create: `internal/config/normalize_bare_paths.go`

- [ ] **Step 1: Write the implementation**

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sergeknystautas/schmux/internal/state"
)

// NormalizeBarePaths renames non-conforming bare repo directories to {name}.git
// and updates config, state, and worktree references.
// Skips Sapling repos (different base-path semantics).
// Should be called at daemon startup after config and state are loaded.
func NormalizeBarePaths(cfg *Config, st *state.State) {
	reposPath := cfg.GetWorktreeBasePath()
	queryPath := cfg.GetQueryRepoPath()
	var changed bool

	for i := range cfg.Repos {
		repo := &cfg.Repos[i]

		// Skip Sapling repos
		if repo.VCS == "sapling" {
			continue
		}

		// Skip already-conforming repos
		canonical := repo.Name + ".git"
		if repo.BarePath == canonical {
			continue
		}

		// Skip repos with empty BarePath (populateBarePaths handles these)
		if repo.BarePath == "" {
			continue
		}

		// Try to normalize in both repos/ and query/ directories
		for _, basePath := range []string{reposPath, queryPath} {
			if basePath == "" {
				continue
			}

			oldPath := filepath.Join(basePath, repo.BarePath)
			if _, err := os.Stat(oldPath); err != nil {
				continue // Not on disk in this base path
			}

			newPath := filepath.Join(basePath, canonical)

			if _, err := os.Stat(newPath); err == nil {
				fmt.Fprintf(os.Stderr, "[config] cannot normalize repo %q: target %s already exists — rename one of the repos with duplicate name %q\n", repo.Name, newPath, repo.Name)
				continue
			}

			if err := RelocateBareRepo(oldPath, newPath); err != nil {
				fmt.Fprintf(os.Stderr, "[config] failed to normalize repo %q from %s to %s: %v\n", repo.Name, oldPath, newPath, err)
				continue
			}

			fmt.Fprintf(os.Stderr, "[config] normalized bare path for repo %q: %s → %s\n", repo.Name, repo.BarePath, canonical)

			// Update state RepoBase if this is the repos/ directory
			if basePath == reposPath {
				if rb, found := st.GetRepoBaseByURL(repo.URL); found {
					rb.Path = newPath
					st.AddRepoBase(rb)
				}
			}
		}

		repo.BarePath = canonical
		changed = true
	}

	if changed {
		if err := cfg.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "[config] warning: could not save normalized bare paths: %v\n", err)
		}
		if err := st.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "[config] warning: could not save state after normalization: %v\n", err)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestNormalizeBarePaths -v`
Expected: All 6 tests PASS

- [ ] **Step 3: Commit**

```
feat: add NormalizeBarePaths to enforce {name}.git convention at startup
```

---

### Task 5: Wire `NormalizeBarePaths` into daemon startup

**Files:**

- Modify: `internal/daemon/daemon.go:467` (between state load and manager creation)

- [ ] **Step 1: Add the call to daemon startup**

In `internal/daemon/daemon.go`, in the `Run()` method, add the normalization call after the `st.SetNeedsRestart` block (around line 466) and before manager creation (line 468):

```go
	// Normalize bare repo paths — rename non-conforming directories to {name}.git
	config.NormalizeBarePaths(cfg, st)
```

Insert this between:

```go
	// Clear needs_restart flag on daemon start (config changes now taking effect)
	if st.GetNeedsRestart() {
		st.SetNeedsRestart(false)
		st.Save()
	}

	// Normalize bare repo paths — rename non-conforming directories to {name}.git
	config.NormalizeBarePaths(cfg, st)

	// Create managers
	ensure.SetLogger(logging.Sub(workspaceLog, "ensure"))
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./cmd/schmux`
Expected: Builds successfully

- [ ] **Step 3: Commit**

```
feat: wire NormalizeBarePaths into daemon startup
```

---

### Task 6: Repo name uniqueness — failing tests

**Files:**

- Modify: `internal/dashboard/api_contract_test.go`
- Modify: `internal/workspace/manager_test.go`

- [ ] **Step 1: Write the dashboard handler test**

Add to `internal/dashboard/api_contract_test.go`:

```go
func TestAPIContract_ConfigUpdateRejectsDuplicateRepoNames(t *testing.T) {
	server, _, _ := newTestServer(t)

	body := []byte(`{"repos":[{"name":"react","url":"https://github.com/facebook/react.git"},{"name":"react","url":"https://github.com/preactjs/react.git"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	server.handleConfigUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for duplicate repo names, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "duplicate repo name") {
		t.Errorf("error message should mention duplicate repo name, got: %s", rr.Body.String())
	}
}
```

Add `"strings"` to the import block if not already present.

- [ ] **Step 2: Write the CreateLocalRepo test**

Find the test file that tests `CreateLocalRepo`. Add a test that attempts to create a local repo with a name that already exists in config.

Add to `internal/workspace/manager_test.go`:

```go
func TestCreateLocalRepo_RejectsDuplicateName(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	statePath := filepath.Join(tmpDir, "state.json")

	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.Repos = []config.Repo{
		{Name: "myrepo", URL: "https://github.com/user/myrepo.git", BarePath: "myrepo.git"},
	}
	cfg.Save()

	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	_, err := m.CreateLocalRepo(context.Background(), "myrepo", "main")
	if err == nil {
		t.Fatal("expected error for duplicate repo name")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention name already exists, got: %v", err)
	}
}
```

Add `"strings"` to the import block if not already present.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/dashboard/ -run TestAPIContract_ConfigUpdateRejectsDuplicateRepoNames -v`
Expected: FAIL — returns 200 instead of 400

Run: `go test ./internal/workspace/ -run TestCreateLocalRepo_RejectsDuplicateName -v`
Expected: FAIL — returns nil error instead of duplicate error

---

### Task 7: Repo name uniqueness — implementation

**Files:**

- Modify: `internal/dashboard/handlers_config.go:270-281`
- Modify: `internal/workspace/manager.go:682`

- [ ] **Step 1: Add duplicate name check to config save handler**

In `internal/dashboard/handlers_config.go`, inside the `req.Repos != nil` block, after the existing validation loop (after line 281), add duplicate name detection:

```go
		// Check for duplicate repo names
		seenNames := make(map[string]bool, len(req.Repos))
		for _, repo := range req.Repos {
			if seenNames[repo.Name] {
				http.Error(w, fmt.Sprintf("duplicate repo name: %q", repo.Name), http.StatusBadRequest)
				return
			}
			seenNames[repo.Name] = true
		}
```

Insert this between the existing validation loop's closing `}` (line 281) and the `// Build lookup of existing repos` comment (line 282).

- [ ] **Step 2: Add duplicate name check to CreateLocalRepo**

In `internal/workspace/manager.go`, in the `CreateLocalRepo` method, before the `m.config.Repos = append(...)` line (line 682), add:

```go
	// Reject duplicate repo names
	if _, found := m.config.FindRepo(repoName); found {
		return nil, fmt.Errorf("repo name %q already exists in config", repoName)
	}
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `go test ./internal/dashboard/ -run TestAPIContract_ConfigUpdateRejectsDuplicateRepoNames -v`
Expected: PASS

Run: `go test ./internal/workspace/ -run TestCreateLocalRepo_RejectsDuplicateName -v`
Expected: PASS

- [ ] **Step 4: Commit**

```
feat: add repo name uniqueness validation to config save and CreateLocalRepo
```

---

### Task 8: Run full test suite

- [ ] **Step 1: Run `./test.sh`**

Run: `./test.sh`
Expected: All tests pass. The existing `TestAPIContract_ConfigUpdatePreservesBarePath` test should still pass since it uses a single repo with a unique name.

- [ ] **Step 2: Fix any failures**

If any existing tests fail, investigate and fix. The most likely candidate is tests that set up repos with duplicate names (unlikely given the codebase conventions).

- [ ] **Step 3: Commit any fixes**

Only if Step 2 required changes.
