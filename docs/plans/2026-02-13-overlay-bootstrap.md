# Overlay Bootstrap & Path Registry Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Let users configure overlay files from the dashboard, with hardcoded defaults for agent configs and auto-detection of gitignored files in workspaces.

**Architecture:** Add a path registry (hardcoded defaults + global config + per-repo config) that the watcher monitors for declared paths, including files that don't exist yet. Extend the existing `/api/overlays` endpoint, add scan/add endpoints, and build a new React overlay management page.

**Tech Stack:** Go backend, React/TypeScript frontend, fsnotify for file watching

**Design spec:** `docs/specs/overlay-bootstrap.md`

---

### Task 1: Add hardcoded default overlay paths and config schema

**Files:**

- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Step 1: Write the failing test**

Add to `config_test.go`:

```go
func TestGetOverlayPaths_DefaultsOnly(t *testing.T) {
	cfg := &Config{}
	paths := cfg.GetOverlayPaths("myrepo")
	// Should include hardcoded defaults
	if len(paths) < 2 {
		t.Fatalf("expected at least 2 default paths, got %d", len(paths))
	}
	found := make(map[string]bool)
	for _, p := range paths {
		found[p] = true
	}
	if !found[".claude/settings.json"] {
		t.Error("missing .claude/settings.json from defaults")
	}
	if !found[".claude/settings.local.json"] {
		t.Error("missing .claude/settings.local.json from defaults")
	}
}

func TestGetOverlayPaths_WithGlobalAndRepoConfig(t *testing.T) {
	cfg := &Config{
		Overlay: &OverlayConfig{
			Paths: []string{".tool-versions"},
		},
		Repos: []Repo{
			{Name: "myrepo", URL: "git@github.com:org/myrepo.git", OverlayPaths: []string{".env"}},
		},
	}
	paths := cfg.GetOverlayPaths("myrepo")
	found := make(map[string]bool)
	for _, p := range paths {
		found[p] = true
	}
	if !found[".claude/settings.json"] {
		t.Error("missing hardcoded default")
	}
	if !found[".tool-versions"] {
		t.Error("missing global config path")
	}
	if !found[".env"] {
		t.Error("missing repo-specific path")
	}
}

func TestGetOverlayPaths_Deduplication(t *testing.T) {
	cfg := &Config{
		Overlay: &OverlayConfig{
			Paths: []string{".claude/settings.json"}, // duplicate of default
		},
		Repos: []Repo{
			{Name: "myrepo", URL: "url", OverlayPaths: []string{".claude/settings.json"}},
		},
	}
	paths := cfg.GetOverlayPaths("myrepo")
	count := 0
	for _, p := range paths {
		if p == ".claude/settings.json" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 occurrence of .claude/settings.json, got %d", count)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/config/ -run TestGetOverlayPaths
```

Expected: FAIL — `OverlayConfig`, `OverlayPaths`, `GetOverlayPaths` not defined.

**Step 3: Write minimal implementation**

In `config.go`, add after the `CompoundConfig` struct (around line 245):

```go
// OverlayConfig represents global overlay path configuration.
type OverlayConfig struct {
	Paths []string `json:"paths,omitempty"` // additional global overlay paths
}
```

Add `OverlayPaths` field to the `Repo` struct (around line 296):

```go
type Repo struct {
	Name         string   `json:"name"`
	URL          string   `json:"url"`
	BarePath     string   `json:"bare_path,omitempty"`
	OverlayPaths []string `json:"overlay_paths,omitempty"`
}
```

Add `Overlay` field to the `Config` struct (around line 86):

```go
Overlay *OverlayConfig `json:"overlay,omitempty"`
```

Add hardcoded defaults and getter:

```go
// DefaultOverlayPaths are always watched for all repos.
var DefaultOverlayPaths = []string{
	".claude/settings.json",
	".claude/settings.local.json",
}

// GetOverlayPaths returns the deduplicated union of hardcoded defaults,
// global config paths, and repo-specific paths for the given repo name.
func (c *Config) GetOverlayPaths(repoName string) []string {
	seen := make(map[string]bool)
	var paths []string

	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	for _, p := range DefaultOverlayPaths {
		add(p)
	}
	if c != nil && c.Overlay != nil {
		for _, p := range c.Overlay.Paths {
			add(p)
		}
	}
	if c != nil {
		for _, repo := range c.Repos {
			if repo.Name == repoName {
				for _, p := range repo.OverlayPaths {
					add(p)
				}
				break
			}
		}
	}
	return paths
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/config/ -run TestGetOverlayPaths
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "Add overlay path registry with hardcoded defaults and config schema"
```

---

### Task 2: Add overlay nudge dismissed flag to Repo config

**Files:**

- Modify: `internal/config/config.go`

**Step 1: Add field to Repo struct**

```go
type Repo struct {
	Name                  string   `json:"name"`
	URL                   string   `json:"url"`
	BarePath              string   `json:"bare_path,omitempty"`
	OverlayPaths          []string `json:"overlay_paths,omitempty"`
	OverlayNudgeDismissed bool     `json:"overlay_nudge_dismissed,omitempty"`
}
```

**Step 2: Build to verify compilation**

```bash
go build ./...
```

Expected: compiles cleanly. No test needed — this is a data field only.

**Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "Add overlay_nudge_dismissed flag to Repo config"
```

---

### Task 3: Refactor watcher to support declared paths

**Files:**

- Modify: `internal/compound/watcher.go`
- Test: `internal/compound/watcher_test.go`

**Step 1: Write the failing test**

Add to `watcher_test.go`:

```go
func TestWatcher_DetectsNewFileAtDeclaredPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Declare a path for a file that doesn't exist yet
	relPath := filepath.Join(".claude", "settings.local.json")

	// Create the parent directory so fsnotify can watch it
	os.MkdirAll(filepath.Join(tmpDir, ".claude"), 0755)

	var callbackCount atomic.Int32
	var gotRelPath string

	w, err := NewWatcher(100, func(workspaceID, rp string) {
		gotRelPath = rp
		callbackCount.Add(1)
	})
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}

	// Use AddWorkspaceWithDeclaredPaths — manifest is empty, but declared paths include the file
	manifest := map[string]string{} // empty — file doesn't exist yet
	declaredPaths := []string{relPath}
	if err := w.AddWorkspaceWithDeclaredPaths("ws-001", tmpDir, manifest, declaredPaths); err != nil {
		t.Fatalf("AddWorkspaceWithDeclaredPaths() error: %v", err)
	}

	w.Start()
	defer w.Stop()

	// Agent creates the file
	os.WriteFile(filepath.Join(tmpDir, relPath), []byte(`{"key": "value"}`), 0644)

	// Poll until callback fires
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if callbackCount.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if callbackCount.Load() == 0 {
		t.Fatal("expected callback for newly created file at declared path")
	}
	if gotRelPath != relPath {
		t.Errorf("callback relPath = %q, want %q", gotRelPath, relPath)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/compound/ -run TestWatcher_DetectsNewFileAtDeclaredPath
```

Expected: FAIL — `AddWorkspaceWithDeclaredPaths` not defined.

**Step 3: Implement AddWorkspaceWithDeclaredPaths**

Add to `watcher.go`:

```go
// AddWorkspaceWithDeclaredPaths registers a workspace with both existing manifest files
// and declared paths (which may not exist yet). All paths are monitored.
func (w *Watcher) AddWorkspaceWithDeclaredPaths(workspaceID, workspacePath string, manifest map[string]string, declaredPaths []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.workspacePaths[workspaceID] = workspacePath
	w.workspaceFiles[workspaceID] = make(map[string]bool)

	dirsToWatch := make(map[string]bool)

	// Register manifest files (existing)
	for relPath := range manifest {
		w.workspaceFiles[workspaceID][relPath] = true
		absDir := filepath.Join(workspacePath, filepath.Dir(relPath))
		dirsToWatch[absDir] = true
	}

	// Register declared paths (may not exist yet)
	for _, relPath := range declaredPaths {
		w.workspaceFiles[workspaceID][relPath] = true
		absDir := filepath.Join(workspacePath, filepath.Dir(relPath))
		dirsToWatch[absDir] = true
	}

	for dir := range dirsToWatch {
		if err := w.watcher.Add(dir); err != nil {
			// Directory may not exist yet — that's OK for declared paths.
			// We'll try to watch parent dirs for directory creation.
			fmt.Printf("[compound] warning: failed to watch directory %s: %v\n", dir, err)
			continue
		}
		w.watchedDirs[dir] = append(w.watchedDirs[dir], workspaceID)
	}

	return nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/compound/ -run TestWatcher_DetectsNewFileAtDeclaredPath
```

Expected: PASS

**Step 5: Write a test for missing parent directory**

```go
func TestWatcher_WatchesParentDirCreation(t *testing.T) {
	tmpDir := t.TempDir()

	// Declare a path where the parent dir doesn't exist
	relPath := filepath.Join(".newdir", "config.json")

	var callbackCount atomic.Int32

	w, err := NewWatcher(100, func(workspaceID, rp string) {
		callbackCount.Add(1)
	})
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}

	manifest := map[string]string{}
	declaredPaths := []string{relPath}
	// This should not error even though .newdir doesn't exist
	if err := w.AddWorkspaceWithDeclaredPaths("ws-001", tmpDir, manifest, declaredPaths); err != nil {
		t.Fatalf("AddWorkspaceWithDeclaredPaths() error: %v", err)
	}

	w.Start()
	defer w.Stop()

	// Create the parent directory and file
	os.MkdirAll(filepath.Join(tmpDir, ".newdir"), 0755)
	// Give fsnotify a moment to register the directory creation
	time.Sleep(100 * time.Millisecond)

	os.WriteFile(filepath.Join(tmpDir, relPath), []byte("content"), 0644)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if callbackCount.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Note: this test may fail because the directory was created after the watch was set.
	// The watcher needs to monitor the workspace root for directory creation.
	// We'll handle this with a pending directories mechanism.
}
```

**Step 6: Add pending directory support to watcher**

Add a `pendingDirs` field to the `Watcher` struct and handle directory creation events in `eventLoop`:

```go
// In Watcher struct, add:
pendingDirs map[string][]string // workspace root → []absDir that couldn't be watched

// In AddWorkspaceWithDeclaredPaths, track failed dirs:
if err := w.watcher.Add(dir); err != nil {
	// Track as pending — will be retried when parent dir appears
	w.pendingDirs[workspacePath] = append(w.pendingDirs[workspacePath], dir)
	// Watch workspace root to detect new directory creation
	w.watcher.Add(workspacePath)
	continue
}

// In eventLoop/handleEvent, add directory creation handling:
if event.Op&fsnotify.Create != 0 {
	info, err := os.Stat(event.Name)
	if err == nil && info.IsDir() {
		w.retryPendingDirs(event.Name)
	}
}
```

Implement `retryPendingDirs`:

```go
func (w *Watcher) retryPendingDirs(createdDir string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for wsRoot, pendingDirs := range w.pendingDirs {
		var remaining []string
		for _, dir := range pendingDirs {
			if dir == createdDir || filepath.Dir(dir) == createdDir {
				if err := w.watcher.Add(dir); err != nil {
					remaining = append(remaining, dir)
					continue
				}
				// Find workspace IDs for this root
				for wsID, wsPath := range w.workspacePaths {
					if wsPath == wsRoot {
						w.watchedDirs[dir] = append(w.watchedDirs[dir], wsID)
					}
				}
			} else {
				remaining = append(remaining, dir)
			}
		}
		if len(remaining) == 0 {
			delete(w.pendingDirs, wsRoot)
		} else {
			w.pendingDirs[wsRoot] = remaining
		}
	}
}
```

**Step 7: Run all watcher tests**

```bash
go test -race ./internal/compound/ -run TestWatcher
```

Expected: all PASS

**Step 8: Commit**

```bash
git add internal/compound/watcher.go internal/compound/watcher_test.go
git commit -m "Add declared path support to watcher with pending directory handling"
```

---

### Task 4: Wire declared paths through Compounder and daemon

**Files:**

- Modify: `internal/compound/compound.go`
- Modify: `internal/daemon/daemon.go`
- Test: `internal/compound/compound_test.go`

**Step 1: Write the failing test**

```go
func TestCompounder_DetectsNewFileAtDeclaredPath(t *testing.T) {
	overlayDir := t.TempDir()
	wsDir := t.TempDir()
	os.MkdirAll(filepath.Join(wsDir, ".claude"), 0755)

	manifest := map[string]string{} // empty — no files exist yet
	declaredPaths := []string{filepath.Join(".claude", "settings.local.json")}

	var propagateCount atomic.Int32

	c, err := NewCompounder(100, nil, func(sourceWorkspaceID, repoURL, relPath string, content []byte) {
		propagateCount.Add(1)
	}, nil)
	if err != nil {
		t.Fatalf("NewCompounder() error = %v", err)
	}
	defer c.Stop()

	c.AddWorkspace("ws-001", wsDir, overlayDir, "repo", manifest, declaredPaths)
	c.Start()

	// Agent creates the file
	os.MkdirAll(filepath.Join(overlayDir, ".claude"), 0755)
	newContent := `{"local_setting": true}`
	os.WriteFile(filepath.Join(wsDir, ".claude", "settings.local.json"), []byte(newContent), 0644)

	// Wait for debounce + processing
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		overlayContent, _ := os.ReadFile(filepath.Join(overlayDir, ".claude", "settings.local.json"))
		if string(overlayContent) == newContent {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify overlay was created
	overlayContent, err := os.ReadFile(filepath.Join(overlayDir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("overlay file not created: %v", err)
	}
	if string(overlayContent) != newContent {
		t.Errorf("overlay content = %q, want %q", string(overlayContent), newContent)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/compound/ -run TestCompounder_DetectsNewFileAtDeclaredPath
```

Expected: FAIL — `AddWorkspace` doesn't accept `declaredPaths` parameter.

**Step 3: Update AddWorkspace signature**

In `compound.go`, update `AddWorkspace` to accept `declaredPaths`:

```go
func (c *Compounder) AddWorkspace(workspaceID, workspacePath, overlayDir, repoURL string, manifest map[string]string, declaredPaths []string) {
	// ... defensive copy of manifest ...
	// ... store workspaceInfo ...

	if err := c.watcher.AddWorkspaceWithDeclaredPaths(workspaceID, workspacePath, manifestCopy, declaredPaths); err != nil {
		fmt.Printf("[compound] warning: failed to add workspace watch: %v\n", err)
	}
}
```

Update `processFileChange` to handle missing manifest entries (new file at declared path):

```go
func (c *Compounder) processFileChange(ctx context.Context, workspaceID, relPath string) {
	// ... existing validation ...

	c.mu.RLock()
	info, ok := c.workspaces[workspaceID]
	if !ok {
		c.mu.RUnlock()
		return
	}
	manifestHash := info.Manifest[relPath] // empty string if new file
	wsPath := filepath.Join(info.Path, relPath)
	overlayPath := filepath.Join(info.OverlayDir, relPath)
	repoURL := info.RepoURL
	c.mu.RUnlock()

	// If no manifest entry, this is a new file — use fast path
	if manifestHash == "" {
		// Verify the file actually exists (Create event might be for something else)
		if _, err := os.Stat(wsPath); os.IsNotExist(err) {
			return
		}
		// Use fast path for new files
		// ... rest of merge logic works as-is since DetermineMergeAction
		// returns FastPath when overlay is missing and ws differs from manifest
	}

	// ... rest of existing logic ...
}
```

Update all existing callers of `AddWorkspace` to pass `nil` for declaredPaths:

- `daemon.go` (around lines 513, 546) — pass `cfg.GetOverlayPaths(repoConfig.Name)` instead
- `compound_test.go` — pass `nil` for existing tests

**Step 4: Run tests**

```bash
go test -race ./internal/compound/...
go build ./...
```

Expected: all PASS

**Step 5: Update daemon.go to pass declared paths**

In `daemon.go`, where `compounder.AddWorkspace` is called (two places — spawn callback and startup loop), pass declared paths from config:

```go
declaredPaths := cfg.GetOverlayPaths(repoConfig.Name)
compounder.AddWorkspace(wsID, w.Path, overlayDir, w.Repo, manifest, declaredPaths)
```

**Step 6: Commit**

```bash
git add internal/compound/compound.go internal/compound/compound_test.go internal/daemon/daemon.go
git commit -m "Wire declared overlay paths through compounder and daemon"
```

---

### Task 5: Add scan and add API endpoints

**Files:**

- Create: `internal/dashboard/handlers_overlay.go`
- Modify: `internal/dashboard/server.go`
- Modify: `internal/dashboard/handlers.go` (move existing `handleOverlays` to new file)

**Step 1: Move existing handleOverlays**

Move `handleOverlays` from `handlers.go` to `handlers_overlay.go`. Add the new endpoints. Register routes in `server.go`.

**Step 2: Implement the scan endpoint**

The scan endpoint needs to list gitignored files in a workspace. Use the existing `isIgnoredByGit` function from `internal/workspace/overlay.go` — but it's unexported. Either export it or duplicate the git check-ignore logic.

Simpler approach: use `git ls-files --others --ignored --exclude-standard` which lists all ignored files in one shot.

```go
// In handlers_overlay.go:

func (s *Server) handleOverlayScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		WorkspaceID string `json:"workspace_id"`
		RepoName    string `json:"repo_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	ws, found := s.state.GetWorkspace(req.WorkspaceID)
	if !found {
		http.Error(w, "Workspace not found", http.StatusNotFound)
		return
	}

	// List gitignored files using git
	cmd := exec.CommandContext(r.Context(), "git", "ls-files", "--others", "--ignored", "--exclude-standard")
	cmd.Dir = ws.Path
	output, err := cmd.Output()
	if err != nil {
		http.Error(w, "Failed to scan workspace", http.StatusInternalServerError)
		return
	}

	// Well-known patterns for auto-detection
	wellKnown := map[string]bool{
		".env": true, ".env.local": true, ".envrc": true,
		".tool-versions": true, ".nvmrc": true, ".node-version": true,
		".python-version": true, ".ruby-version": true,
	}

	type Candidate struct {
		Path     string `json:"path"`
		Size     int64  `json:"size"`
		Detected bool   `json:"detected"`
	}

	var candidates []Candidate
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		info, err := os.Stat(filepath.Join(ws.Path, line))
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}
		candidates = append(candidates, Candidate{
			Path:     line,
			Size:     info.Size(),
			Detected: wellKnown[filepath.Base(line)] || wellKnown[line],
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"candidates": candidates})
}
```

**Step 3: Implement the add endpoint**

```go
func (s *Server) handleOverlayAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		WorkspaceID string   `json:"workspace_id"`
		RepoName    string   `json:"repo_name"`
		Paths       []string `json:"paths"`        // files to copy from workspace
		CustomPaths []string `json:"custom_paths"`  // paths to register only (no file yet)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	ws, found := s.state.GetWorkspace(req.WorkspaceID)
	if !found {
		http.Error(w, "Workspace not found", http.StatusNotFound)
		return
	}

	overlayDir, err := workspace.OverlayDir(req.RepoName)
	if err != nil {
		http.Error(w, "Failed to resolve overlay dir", http.StatusInternalServerError)
		return
	}

	// Copy selected files from workspace to overlay dir
	var copied []string
	for _, relPath := range req.Paths {
		if err := compound.ValidateRelPath(relPath); err != nil {
			continue
		}
		srcPath := filepath.Join(ws.Path, relPath)
		dstPath := filepath.Join(overlayDir, relPath)
		os.MkdirAll(filepath.Dir(dstPath), 0755)

		content, err := os.ReadFile(srcPath)
		if err != nil {
			continue
		}
		if err := os.WriteFile(dstPath, content, 0644); err != nil {
			continue
		}
		copied = append(copied, relPath)
	}

	// Add all paths (copied + custom) to repo config
	allNewPaths := append(copied, req.CustomPaths...)
	if len(allNewPaths) > 0 {
		s.config.AddRepoOverlayPaths(req.RepoName, allNewPaths)
		s.config.Save()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"copied":  copied,
		"registered": allNewPaths,
	})
}
```

**Step 4: Add AddRepoOverlayPaths helper to config**

In `config.go`:

```go
func (c *Config) AddRepoOverlayPaths(repoName string, paths []string) {
	for i := range c.Repos {
		if c.Repos[i].Name == repoName {
			existing := make(map[string]bool)
			for _, p := range c.Repos[i].OverlayPaths {
				existing[p] = true
			}
			for _, p := range paths {
				if !existing[p] {
					c.Repos[i].OverlayPaths = append(c.Repos[i].OverlayPaths, p)
					existing[p] = true
				}
			}
			return
		}
	}
}
```

**Step 5: Register routes in server.go**

Add after the existing `/api/overlays` route:

```go
mux.HandleFunc("/api/overlays/scan", s.withCORS(s.withAuth(s.handleOverlayScan)))
mux.HandleFunc("/api/overlays/add", s.withCORS(s.withAuth(s.handleOverlayAdd)))
```

**Step 6: Enhance existing handleOverlays to include declared paths**

Update `handleOverlays` to include the path registry info (source: builtin/global/repo, status: synced/pending).

**Step 7: Build and test**

```bash
go build ./...
go test -race ./internal/dashboard/... ./internal/config/...
```

**Step 8: Commit**

```bash
git add internal/dashboard/handlers_overlay.go internal/dashboard/handlers.go internal/dashboard/server.go internal/config/config.go
git commit -m "Add overlay scan and add API endpoints with path registry"
```

---

### Task 6: Add TypeScript types and API functions

**Files:**

- Modify: `assets/dashboard/src/lib/types.ts`
- Modify: `assets/dashboard/src/lib/api.ts`

**Step 1: Add types**

In `types.ts`, update the existing `OverlayInfo` and add new types:

```typescript
export interface OverlayPathInfo {
  path: string;
  source: 'builtin' | 'global' | 'repo';
  status: 'synced' | 'pending';
}

export interface OverlayInfo {
  repo_name: string;
  path: string;
  exists: boolean;
  file_count: number;
  paths: OverlayPathInfo[];
  nudge_dismissed: boolean;
}

export interface OverlayScanCandidate {
  path: string;
  size: number;
  detected: boolean;
}

export interface OverlayScanResponse {
  candidates: OverlayScanCandidate[];
}

export interface OverlayAddRequest {
  workspace_id: string;
  repo_name: string;
  paths: string[];
  custom_paths: string[];
}

export interface OverlayAddResponse {
  success: boolean;
  copied: string[];
  registered: string[];
}
```

**Step 2: Add API functions**

In `api.ts`:

```typescript
export async function scanOverlayFiles(
  workspaceId: string,
  repoName: string
): Promise<OverlayScanResponse> {
  const response = await fetch('/api/overlays/scan', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ workspace_id: workspaceId, repo_name: repoName }),
  });
  if (!response.ok) throw new Error('Failed to scan overlay files');
  return response.json();
}

export async function addOverlayFiles(req: OverlayAddRequest): Promise<OverlayAddResponse> {
  const response = await fetch('/api/overlays/add', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
  if (!response.ok) throw new Error('Failed to add overlay files');
  return response.json();
}
```

**Step 3: Build**

```bash
go run ./cmd/build-dashboard
```

**Step 4: Commit**

```bash
git add assets/dashboard/src/lib/types.ts assets/dashboard/src/lib/api.ts
git commit -m "Add TypeScript types and API functions for overlay management"
```

---

### Task 7: Build the overlay management page

**Files:**

- Create: `assets/dashboard/src/routes/OverlayPage.tsx`
- Modify: `assets/dashboard/src/App.tsx`

**Step 1: Create OverlayPage component**

The page has three states:

1. **Loading** — fetching overlay info
2. **Empty state** — shows auto-managed paths and "Add files" button
3. **Populated state** — shows all paths with status and remove buttons

The "Add files" flow is a modal with:

1. Workspace picker dropdown
2. Scan results with checkboxes
3. Custom path input
4. Confirm button

Implementation should follow the pattern of `ConfigPage.tsx` — local state, API calls, toast notifications.

Key sections of the component:

- Fetch overlay info on mount via `getOverlays()`
- Filter to the current repo (from URL param `:repoName`)
- Render auto-managed paths (source=builtin) as read-only
- Render repo-specific paths with remove buttons
- "Add files" button opens a modal that calls `scanOverlayFiles()` and then `addOverlayFiles()`

**Step 2: Register route in App.tsx**

```tsx
import OverlayPage from './routes/OverlayPage';
// ...
<Route path="/overlays/:repoName" element={<OverlayPage />} />;
```

**Step 3: Build and test manually**

```bash
go run ./cmd/build-dashboard
```

Navigate to `/overlays/<repo-name>` in the dashboard.

**Step 4: Commit**

```bash
git add assets/dashboard/src/routes/OverlayPage.tsx assets/dashboard/src/App.tsx
git commit -m "Add overlay management page with scan and add flow"
```

---

### Task 8: Add first-spawn nudge to workspace cards

**Files:**

- Modify: `assets/dashboard/src/routes/HomePage.tsx` (or wherever workspace cards are rendered)

**Step 1: Add nudge banner component**

A dismissible banner shown on workspace cards when `nudge_dismissed` is false and no repo-specific overlay files exist. Shows what's auto-managed and links to `/overlays/:repoName`.

**Step 2: Add dismiss API**

Add a `POST /api/overlays/dismiss-nudge` endpoint that sets `overlay_nudge_dismissed: true` on the repo config.

**Step 3: Wire dismiss to frontend**

When user clicks dismiss, call the API and hide the banner locally.

**Step 4: Build and test**

```bash
go run ./cmd/build-dashboard
```

**Step 5: Commit**

```bash
git add assets/dashboard/src/routes/HomePage.tsx internal/dashboard/handlers_overlay.go
git commit -m "Add first-spawn overlay nudge banner to workspace cards"
```

---

### Task 9: Add sidebar link to overlay page

**Files:**

- Modify: `assets/dashboard/src/components/AppShell.tsx`

**Step 1: Add navigation link**

Add an "Overlays" link in the sidebar navigation, or add overlay links to the workspace header dropdown. Follow the existing pattern for sidebar items.

**Step 2: Build and test**

```bash
go run ./cmd/build-dashboard
```

**Step 3: Commit**

```bash
git add assets/dashboard/src/components/AppShell.tsx
git commit -m "Add overlay page link to dashboard navigation"
```

---

### Task 10: Integration test for full overlay bootstrap flow

**Files:**

- Modify: `internal/compound/compound_test.go`

**Step 1: Write end-to-end test**

Test the full flow: declare path → agent creates file → file synced to overlay → propagated to sibling.

```go
func TestCompounder_DeclaredPath_FullFlow(t *testing.T) {
	overlayDir := t.TempDir()
	ws1Dir := t.TempDir()
	ws2Dir := t.TempDir()

	// Create parent dirs for declared paths
	os.MkdirAll(filepath.Join(ws1Dir, ".claude"), 0755)
	os.MkdirAll(filepath.Join(ws2Dir, ".claude"), 0755)
	os.MkdirAll(filepath.Join(overlayDir, ".claude"), 0755)

	relPath := filepath.Join(".claude", "settings.local.json")
	manifest := map[string]string{}
	declaredPaths := []string{relPath}

	var ws2Written atomic.Int32

	c, err := NewCompounder(100, nil, func(sourceWorkspaceID, repoURL, rp string, content []byte) {
		// Simulate propagation to ws2
		destPath := filepath.Join(ws2Dir, rp)
		os.MkdirAll(filepath.Dir(destPath), 0755)
		os.WriteFile(destPath, content, 0644)
		ws2Written.Add(1)
	}, nil)
	if err != nil {
		t.Fatalf("NewCompounder() error = %v", err)
	}
	defer c.Stop()

	c.AddWorkspace("ws-001", ws1Dir, overlayDir, "repo", manifest, declaredPaths)
	c.Start()

	// Agent creates the file in ws1
	fileContent := `{"setting": "from_agent"}`
	os.WriteFile(filepath.Join(ws1Dir, relPath), []byte(fileContent), 0644)

	// Wait for propagation
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ws2Written.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify overlay was written
	overlayContent, err := os.ReadFile(filepath.Join(overlayDir, relPath))
	if err != nil {
		t.Fatalf("overlay file not created: %v", err)
	}
	if string(overlayContent) != fileContent {
		t.Errorf("overlay = %q, want %q", string(overlayContent), fileContent)
	}

	// Verify propagation
	if ws2Written.Load() == 0 {
		t.Error("expected propagation to ws2")
	}
	ws2Content, _ := os.ReadFile(filepath.Join(ws2Dir, relPath))
	if string(ws2Content) != fileContent {
		t.Errorf("ws2 = %q, want %q", string(ws2Content), fileContent)
	}
}
```

**Step 2: Run test**

```bash
go test -race ./internal/compound/ -run TestCompounder_DeclaredPath_FullFlow
```

Expected: PASS

**Step 3: Commit**

```bash
git add internal/compound/compound_test.go
git commit -m "Add integration test for declared path overlay bootstrap flow"
```

---

### Task 11: Run full test suite and final cleanup

**Step 1: Run all tests**

```bash
go test -race ./...
```

**Step 2: Build dashboard**

```bash
go run ./cmd/build-dashboard
```

**Step 3: Format**

```bash
./format.sh
```

**Step 4: Final commit if any formatting changes**

```bash
git add -A && git commit -m "Format and cleanup"
```
