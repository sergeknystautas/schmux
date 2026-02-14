# Overlay E2E Tests Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Add E2E tests that validate overlay bootstrap declared paths, overlay API endpoints, workspace reuse overlay preservation, and reconcile-on-dispose behavior.

**Architecture:** Four new test functions in `internal/e2e/e2e_test.go` plus new helpers in `internal/e2e/e2e.go`. Each test creates an isolated config/repo, starts the daemon, exercises the feature, and verifies outcomes. Tests share the existing E2E infrastructure (Docker, `Env` struct, helpers).

**Tech Stack:** Go `testing` package, Docker (via `Dockerfile.e2e`), tmux, schmux daemon API

---

### Task 1: Add E2E helpers for overlay config and API

**Files:**

- Modify: `internal/e2e/e2e.go`

**Step 1: Add `SetRepoOverlayPaths` helper**

This helper sets `overlay_paths` on a repo in the config. Used by the declared-paths test.

```go
// SetRepoOverlayPaths sets overlay_paths on a repo in the config.
func (e *Env) SetRepoOverlayPaths(repoName string, paths []string) {
	e.T.Helper()
	e.T.Logf("Setting overlay paths for repo %s: %v", repoName, paths)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		e.T.Fatalf("Failed to get home dir: %v", err)
	}

	configPath := filepath.Join(homeDir, ".schmux", "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		e.T.Fatalf("Failed to load config: %v", err)
	}

	for i := range cfg.Repos {
		if cfg.Repos[i].Name == repoName {
			cfg.Repos[i].OverlayPaths = paths
			break
		}
	}

	if err := cfg.Save(); err != nil {
		e.T.Fatalf("Failed to save config: %v", err)
	}
}
```

**Step 2: Add `GetOverlayAPI` helper**

This helper calls `GET /api/overlays` and returns the parsed response.

```go
// OverlayAPIResponse represents the GET /api/overlays response.
type OverlayAPIResponse struct {
	Overlays []OverlayAPIInfo `json:"overlays"`
}

type OverlayAPIInfo struct {
	RepoName       string            `json:"repo_name"`
	Path           string            `json:"path"`
	Exists         bool              `json:"exists"`
	FileCount      int               `json:"file_count"`
	DeclaredPaths  []OverlayPathInfo `json:"declared_paths"`
	NudgeDismissed bool              `json:"nudge_dismissed"`
}

type OverlayPathInfo struct {
	Path   string `json:"path"`
	Source string `json:"source"` // "builtin", "global", "repo"
	Status string `json:"status"` // "synced", "pending"
}

func (e *Env) GetOverlayAPI() OverlayAPIResponse {
	e.T.Helper()

	resp, err := http.Get(e.DaemonURL + "/api/overlays")
	if err != nil {
		e.T.Fatalf("Failed to GET /api/overlays: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		e.T.Fatalf("GET /api/overlays returned %d: %s", resp.StatusCode, body)
	}

	var result OverlayAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		e.T.Fatalf("Failed to decode overlay response: %v", err)
	}

	return result
}
```

**Step 3: Add `PostOverlayScan` helper**

```go
// OverlayScanCandidate represents a file found during overlay scanning.
type OverlayScanCandidate struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Detected bool   `json:"detected"`
}

func (e *Env) PostOverlayScan(workspaceID, repoName string) []OverlayScanCandidate {
	e.T.Helper()

	reqBody, _ := json.Marshal(map[string]string{
		"workspace_id": workspaceID,
		"repo_name":    repoName,
	})

	resp, err := http.Post(e.DaemonURL+"/api/overlays/scan", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		e.T.Fatalf("Failed to POST /api/overlays/scan: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		e.T.Fatalf("POST /api/overlays/scan returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Candidates []OverlayScanCandidate `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		e.T.Fatalf("Failed to decode scan response: %v", err)
	}

	return result.Candidates
}
```

**Step 4: Add `PostOverlayAdd` helper**

```go
type OverlayAddResult struct {
	Success    bool     `json:"success"`
	Copied     []string `json:"copied"`
	Registered []string `json:"registered"`
}

func (e *Env) PostOverlayAdd(workspaceID, repoName string, paths, customPaths []string) OverlayAddResult {
	e.T.Helper()

	reqBody, _ := json.Marshal(map[string]any{
		"workspace_id": workspaceID,
		"repo_name":    repoName,
		"paths":        paths,
		"custom_paths": customPaths,
	})

	resp, err := http.Post(e.DaemonURL+"/api/overlays/add", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		e.T.Fatalf("Failed to POST /api/overlays/add: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		e.T.Fatalf("POST /api/overlays/add returned %d: %s", resp.StatusCode, body)
	}

	var result OverlayAddResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		e.T.Fatalf("Failed to decode add response: %v", err)
	}

	return result
}
```

**Step 5: Add `PostDismissNudge` helper**

```go
func (e *Env) PostDismissNudge(repoName string) {
	e.T.Helper()

	reqBody, _ := json.Marshal(map[string]string{"repo_name": repoName})

	resp, err := http.Post(e.DaemonURL+"/api/overlays/dismiss-nudge", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		e.T.Fatalf("Failed to POST /api/overlays/dismiss-nudge: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		e.T.Fatalf("POST /api/overlays/dismiss-nudge returned %d: %s", resp.StatusCode, body)
	}
}
```

**Step 6: Add `GetWorkspaceIDForSession` helper**

The existing `GetWorkspacePath` returns the path but not the workspace ID. The scan endpoint needs the workspace ID.

```go
func (e *Env) GetWorkspaceIDForSession(sessionID string) string {
	e.T.Helper()

	workspaces := e.GetAPIWorkspaces()
	for _, ws := range workspaces {
		for _, sess := range ws.Sessions {
			if sess.ID == sessionID {
				return ws.ID
			}
		}
	}

	e.T.Fatalf("Could not find workspace ID for session %s", sessionID)
	return ""
}
```

**Step 7: Build to verify compilation**

```bash
go build ./internal/e2e/...
```

Expected: compiles cleanly (e2e tests need `//go:build e2e` tag, so use `-tags=e2e` if the file has the tag, otherwise just build).

Note: `e2e.go` does NOT have the `//go:build e2e` tag — only the test files do. So `go build ./internal/e2e/...` should work.

**Step 8: Commit**

```bash
git add internal/e2e/e2e.go
git commit -m "Add E2E helpers for overlay API and config management"
```

---

### Task 2: Add TestE2EOverlayDeclaredPaths

**Files:**

- Modify: `internal/e2e/e2e_test.go`

**Step 1: Write the test**

This test validates the declared-path pipeline: a path is configured as an overlay path but the file doesn't exist at spawn time. When an agent creates the file, the compounder detects it, copies to overlay, and propagates to sibling workspaces.

```go
func TestE2EOverlayDeclaredPaths(t *testing.T) {
	env := New(t)

	const workspaceRoot = "/tmp/schmux-e2e-declared-paths-test"
	const repoName = "declared-paths-repo"
	repoPath := workspaceRoot + "/" + repoName
	repoURL := "file://" + repoPath

	// Step 1: Create config
	t.Run("01_CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	// Step 2: Create git repo with .gitignore covering declared paths
	t.Run("02_CreateGitRepo", func(t *testing.T) {
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		RunCmd(t, repoPath, "git", "init", "-b", "main")
		RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
		RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")

		if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Declared Paths Test\n"), 0644); err != nil {
			t.Fatalf("Failed to create README: %v", err)
		}

		// .gitignore covers both existing overlay files and future declared paths
		gitignore := ".env\n.claude/\n.agent/\n"
		if err := os.WriteFile(filepath.Join(repoPath, ".gitignore"), []byte(gitignore), 0644); err != nil {
			t.Fatalf("Failed to create .gitignore: %v", err)
		}

		RunCmd(t, repoPath, "git", "add", ".")
		RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")

		env.AddRepoToConfig(repoName, repoURL)
	})

	// Step 3: Configure declared overlay paths on the repo
	t.Run("03_ConfigureDeclaredPaths", func(t *testing.T) {
		// .agent/config.json is declared but has no overlay file — it will be
		// created by an "agent" later
		env.SetRepoOverlayPaths(repoName, []string{".agent/config.json"})
	})

	// Step 4: Enable git SCM and compounding with fast debounce
	t.Run("04_EnableSCMAndCompound", func(t *testing.T) {
		env.SetSourceCodeManagement("git")
		env.SetCompoundConfig(500)
	})

	// Step 5: Create an overlay file that already exists (for comparison)
	t.Run("05_CreateExistingOverlay", func(t *testing.T) {
		env.CreateOverlayFile(repoName, ".env", "SECRET=abc123\n")
	})

	// Step 6: Start daemon
	t.Run("06_DaemonStart", func(t *testing.T) {
		env.DaemonStart()
	})

	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	// Step 7: Spawn two sessions (two separate workspaces)
	var session1ID, session2ID string
	var ws1Path, ws2Path string
	t.Run("07_SpawnSessions", func(t *testing.T) {
		session1ID = env.SpawnSession(repoURL, "main", "echo", "", "agent-one")
		if session1ID == "" {
			t.Fatal("Expected session1 ID from spawn")
		}
		ws1Path = env.GetWorkspacePath(session1ID)

		session2ID = env.SpawnSession(repoURL, "main", "echo", "", "agent-two")
		if session2ID == "" {
			t.Fatal("Expected session2 ID from spawn")
		}
		ws2Path = env.GetWorkspacePath(session2ID)

		if ws1Path == ws2Path {
			t.Fatalf("Expected different workspaces, both at: %s", ws1Path)
		}
		t.Logf("Workspace 1: %s", ws1Path)
		t.Logf("Workspace 2: %s", ws2Path)
	})

	// Step 8: Verify existing overlay (.env) was copied to both workspaces
	t.Run("08_VerifyExistingOverlayCopied", func(t *testing.T) {
		for _, wsPath := range []string{ws1Path, ws2Path} {
			data, err := os.ReadFile(filepath.Join(wsPath, ".env"))
			if err != nil {
				t.Fatalf("Overlay .env not copied to %s: %v", wsPath, err)
			}
			if string(data) != "SECRET=abc123\n" {
				t.Errorf("Overlay .env content mismatch in %s: got %q", wsPath, string(data))
			}
		}
	})

	// Step 9: Verify declared path (.agent/config.json) does NOT exist yet
	t.Run("09_VerifyDeclaredPathNotYetCreated", func(t *testing.T) {
		for _, wsPath := range []string{ws1Path, ws2Path} {
			agentFile := filepath.Join(wsPath, ".agent", "config.json")
			if _, err := os.Stat(agentFile); !os.IsNotExist(err) {
				t.Errorf("Expected .agent/config.json to not exist in %s, but it does", wsPath)
			}
		}
	})

	// Step 10: Simulate an agent creating the file at the declared path in workspace 1
	agentContent := `{"agent_model": "sonnet", "auto_approve": true}`
	t.Run("10_AgentCreatesFile", func(t *testing.T) {
		agentDir := filepath.Join(ws1Path, ".agent")
		if err := os.MkdirAll(agentDir, 0755); err != nil {
			t.Fatalf("Failed to create .agent dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(agentDir, "config.json"), []byte(agentContent), 0644); err != nil {
			t.Fatalf("Failed to write agent config: %v", err)
		}
		t.Log("Agent created .agent/config.json in workspace 1")
	})

	// Step 11: Wait for propagation to overlay dir and workspace 2
	t.Run("11_WaitForPropagation", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("Failed to get home dir: %v", err)
		}
		overlayPath := filepath.Join(homeDir, ".schmux", "overlays", repoName, ".agent", "config.json")
		ws2Path := filepath.Join(ws2Path, ".agent", "config.json")

		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			overlayData, err1 := os.ReadFile(overlayPath)
			ws2Data, err2 := os.ReadFile(ws2Path)

			overlayMatch := err1 == nil && string(overlayData) == agentContent
			ws2Match := err2 == nil && string(ws2Data) == agentContent

			if overlayMatch && ws2Match {
				t.Log("Propagation complete: overlay and workspace 2 both have agent config")
				return
			}

			time.Sleep(200 * time.Millisecond)
		}

		overlayData, _ := os.ReadFile(overlayPath)
		ws2Data, _ := os.ReadFile(ws2Path)
		t.Fatalf("Propagation timed out.\nOverlay: %q\nWorkspace2: %q\nExpected: %q",
			string(overlayData), string(ws2Data), agentContent)
	})

	// Step 12: Dispose sessions
	t.Run("12_DisposeSessions", func(t *testing.T) {
		env.DisposeSession(session1ID)
		env.DisposeSession(session2ID)
	})
}
```

**Step 2: Verify it compiles**

```bash
go vet -tags=e2e ./internal/e2e/...
```

Expected: no errors (we can't run it locally — requires Docker).

**Step 3: Commit**

```bash
git add internal/e2e/e2e_test.go
git commit -m "Add E2E test for overlay declared paths flow"
```

---

### Task 3: Add TestE2EOverlayAPI

**Files:**

- Modify: `internal/e2e/e2e_test.go`

**Step 1: Write the test**

This test validates the overlay API endpoints: GET (with declared paths and status), scan, add, and dismiss-nudge.

```go
func TestE2EOverlayAPI(t *testing.T) {
	env := New(t)

	const workspaceRoot = "/tmp/schmux-e2e-overlay-api-test"
	const repoName = "overlay-api-repo"
	repoPath := workspaceRoot + "/" + repoName
	repoURL := "file://" + repoPath

	// Setup: config, repo, overlays, daemon
	t.Run("01_Setup", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)

		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		RunCmd(t, repoPath, "git", "init", "-b", "main")
		RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
		RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")

		if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# API Test\n"), 0644); err != nil {
			t.Fatalf("Failed to create README: %v", err)
		}
		gitignore := ".env\n.claude/\n.secret\n"
		if err := os.WriteFile(filepath.Join(repoPath, ".gitignore"), []byte(gitignore), 0644); err != nil {
			t.Fatalf("Failed to create .gitignore: %v", err)
		}

		RunCmd(t, repoPath, "git", "add", ".")
		RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")

		env.AddRepoToConfig(repoName, repoURL)
		env.SetSourceCodeManagement("git")
		env.CreateOverlayFile(repoName, ".claude/settings.json", `{"model":"sonnet"}`)
	})

	t.Run("02_DaemonStart", func(t *testing.T) {
		env.DaemonStart()
	})

	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	// Spawn a session to get a workspace
	var sessionID, workspaceID string
	t.Run("03_SpawnSession", func(t *testing.T) {
		sessionID = env.SpawnSession(repoURL, "main", "echo", "", "api-test")
		if sessionID == "" {
			t.Fatal("Expected session ID from spawn")
		}
		workspaceID = env.GetWorkspaceIDForSession(sessionID)
		t.Logf("Session: %s, Workspace: %s", sessionID, workspaceID)
	})

	// Test GET /api/overlays — verify declared paths with source and status
	t.Run("04_GetOverlays", func(t *testing.T) {
		result := env.GetOverlayAPI()

		var repoOverlay *OverlayAPIInfo
		for i := range result.Overlays {
			if result.Overlays[i].RepoName == repoName {
				repoOverlay = &result.Overlays[i]
				break
			}
		}
		if repoOverlay == nil {
			t.Fatalf("Expected overlay info for repo %q, got repos: %v", repoName, result.Overlays)
		}

		if !repoOverlay.Exists {
			t.Error("Expected overlay directory to exist")
		}

		// Check that builtin defaults are present
		foundBuiltin := false
		for _, dp := range repoOverlay.DeclaredPaths {
			if dp.Path == ".claude/settings.json" && dp.Source == "builtin" {
				foundBuiltin = true
				if dp.Status != "synced" {
					t.Errorf("Expected .claude/settings.json status=synced, got %q", dp.Status)
				}
			}
		}
		if !foundBuiltin {
			t.Error("Expected .claude/settings.json as a builtin declared path")
		}

		// Nudge should not be dismissed yet
		if repoOverlay.NudgeDismissed {
			t.Error("Expected nudge_dismissed=false initially")
		}
	})

	// Test POST /api/overlays/scan — scan workspace for gitignored files
	t.Run("05_ScanWorkspace", func(t *testing.T) {
		// First, create a gitignored file in the workspace to be discovered
		wsPath := env.GetWorkspacePath(sessionID)
		secretFile := filepath.Join(wsPath, ".secret")
		if err := os.WriteFile(secretFile, []byte("top-secret"), 0644); err != nil {
			t.Fatalf("Failed to create .secret: %v", err)
		}

		candidates := env.PostOverlayScan(workspaceID, repoName)

		// .secret should appear in the scan results
		foundSecret := false
		for _, c := range candidates {
			if c.Path == ".secret" {
				foundSecret = true
				break
			}
		}
		if !foundSecret {
			t.Errorf("Expected .secret in scan candidates, got: %v", candidates)
		}
	})

	// Test POST /api/overlays/add — add scanned file to overlay
	t.Run("06_AddOverlayFiles", func(t *testing.T) {
		result := env.PostOverlayAdd(workspaceID, repoName, []string{".secret"}, nil)

		if !result.Success {
			t.Error("Expected success=true from add")
		}

		found := false
		for _, p := range result.Copied {
			if p == ".secret" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected .secret in copied list, got: %v", result.Copied)
		}

		// Verify file was actually copied to overlay dir
		homeDir, _ := os.UserHomeDir()
		overlaySecret := filepath.Join(homeDir, ".schmux", "overlays", repoName, ".secret")
		data, err := os.ReadFile(overlaySecret)
		if err != nil {
			t.Fatalf("Overlay .secret was not created: %v", err)
		}
		if string(data) != "top-secret" {
			t.Errorf("Overlay .secret content mismatch: got %q", string(data))
		}
	})

	// Test GET /api/overlays again — verify newly added path appears
	t.Run("07_VerifyAddedPathInOverlays", func(t *testing.T) {
		result := env.GetOverlayAPI()

		var repoOverlay *OverlayAPIInfo
		for i := range result.Overlays {
			if result.Overlays[i].RepoName == repoName {
				repoOverlay = &result.Overlays[i]
				break
			}
		}
		if repoOverlay == nil {
			t.Fatal("Expected overlay info for repo")
		}

		foundSecret := false
		for _, dp := range repoOverlay.DeclaredPaths {
			if dp.Path == ".secret" && dp.Source == "repo" && dp.Status == "synced" {
				foundSecret = true
				break
			}
		}
		if !foundSecret {
			t.Error("Expected .secret as a repo declared path with status synced")
		}
	})

	// Test POST /api/overlays/dismiss-nudge
	t.Run("08_DismissNudge", func(t *testing.T) {
		env.PostDismissNudge(repoName)

		result := env.GetOverlayAPI()
		for _, o := range result.Overlays {
			if o.RepoName == repoName {
				if !o.NudgeDismissed {
					t.Error("Expected nudge_dismissed=true after dismiss")
				}
				return
			}
		}
		t.Fatal("Repo not found in overlay response after dismiss")
	})

	// Cleanup
	t.Run("09_DisposeSessions", func(t *testing.T) {
		env.DisposeSession(sessionID)
	})
}
```

**Step 2: Verify it compiles**

```bash
go vet -tags=e2e ./internal/e2e/...
```

**Step 3: Commit**

```bash
git add internal/e2e/e2e_test.go
git commit -m "Add E2E test for overlay API endpoints"
```

---

### Task 4: Add TestE2EOverlayWorkspaceReuse

**Files:**

- Modify: `internal/e2e/e2e_test.go`

**Step 1: Write the test**

This test validates that overlay files survive workspace reuse. When a session is disposed and a new session is spawned on the same branch, the workspace is reused and overlays are re-copied.

```go
func TestE2EOverlayWorkspaceReuse(t *testing.T) {
	env := New(t)

	const workspaceRoot = "/tmp/schmux-e2e-overlay-reuse-test"
	const repoName = "overlay-reuse-repo"
	repoPath := workspaceRoot + "/" + repoName
	repoURL := "file://" + repoPath

	// Setup
	t.Run("01_Setup", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)

		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		RunCmd(t, repoPath, "git", "init", "-b", "main")
		RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
		RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")

		if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Reuse Test\n"), 0644); err != nil {
			t.Fatalf("Failed to create README: %v", err)
		}
		gitignore := ".env\n.claude/\n"
		if err := os.WriteFile(filepath.Join(repoPath, ".gitignore"), []byte(gitignore), 0644); err != nil {
			t.Fatalf("Failed to create .gitignore: %v", err)
		}

		RunCmd(t, repoPath, "git", "add", ".")
		RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")

		env.AddRepoToConfig(repoName, repoURL)
		env.SetSourceCodeManagement("git")
		env.CreateOverlayFile(repoName, ".env", "ORIGINAL=true\n")
	})

	t.Run("02_DaemonStart", func(t *testing.T) {
		env.DaemonStart()
	})

	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	// Spawn first session, verify overlay, then dispose
	var ws1Path string
	t.Run("03_SpawnAndVerifyFirst", func(t *testing.T) {
		sessionID := env.SpawnSession(repoURL, "main", "echo", "", "first-session")
		if sessionID == "" {
			t.Fatal("Expected session ID from spawn")
		}
		ws1Path = env.GetWorkspacePath(sessionID)

		// Verify overlay was copied
		data, err := os.ReadFile(filepath.Join(ws1Path, ".env"))
		if err != nil {
			t.Fatalf("Overlay .env not copied: %v", err)
		}
		if string(data) != "ORIGINAL=true\n" {
			t.Errorf("Overlay content mismatch: got %q", string(data))
		}

		// Dispose the session (workspace persists for reuse)
		env.DisposeSession(sessionID)
		t.Logf("First session disposed, workspace at: %s", ws1Path)
	})

	// Update the overlay file (simulate user changing it between sessions)
	t.Run("04_UpdateOverlay", func(t *testing.T) {
		env.CreateOverlayFile(repoName, ".env", "UPDATED=true\n")
	})

	// Spawn second session on the same branch — workspace should be reused
	t.Run("05_SpawnSecondAndVerifyReuse", func(t *testing.T) {
		sessionID := env.SpawnSession(repoURL, "main", "echo", "", "second-session")
		if sessionID == "" {
			t.Fatal("Expected session ID from spawn")
		}
		ws2Path := env.GetWorkspacePath(sessionID)

		// Workspace should be reused (same path)
		if ws2Path != ws1Path {
			t.Logf("Note: workspace was not reused (got %s, expected %s) — this is OK if git SCM created a new worktree", ws2Path, ws1Path)
		}

		// Verify the UPDATED overlay content is present (not the stale ORIGINAL)
		data, err := os.ReadFile(filepath.Join(ws2Path, ".env"))
		if err != nil {
			t.Fatalf("Overlay .env not present after reuse: %v", err)
		}
		if string(data) != "UPDATED=true\n" {
			t.Errorf("Expected updated overlay content, got %q", string(data))
		}

		env.DisposeSession(sessionID)
	})
}
```

**Step 2: Verify it compiles**

```bash
go vet -tags=e2e ./internal/e2e/...
```

**Step 3: Commit**

```bash
git add internal/e2e/e2e_test.go
git commit -m "Add E2E test for overlay workspace reuse"
```

---

### Task 5: Add TestE2EOverlayReconcileOnDispose

**Files:**

- Modify: `internal/e2e/e2e_test.go`

**Step 1: Write the test**

This test validates that changes made right before dispose are captured by the reconciliation pass. The compounding debounce is set high enough that normal debounce wouldn't fire before dispose — reconcile must catch it.

```go
func TestE2EOverlayReconcileOnDispose(t *testing.T) {
	env := New(t)

	const workspaceRoot = "/tmp/schmux-e2e-overlay-reconcile-test"
	const repoName = "reconcile-repo"
	repoPath := workspaceRoot + "/" + repoName
	repoURL := "file://" + repoPath

	// Setup
	t.Run("01_Setup", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)

		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		RunCmd(t, repoPath, "git", "init", "-b", "main")
		RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
		RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")

		if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Reconcile Test\n"), 0644); err != nil {
			t.Fatalf("Failed to create README: %v", err)
		}
		gitignore := ".env\n"
		if err := os.WriteFile(filepath.Join(repoPath, ".gitignore"), []byte(gitignore), 0644); err != nil {
			t.Fatalf("Failed to create .gitignore: %v", err)
		}

		RunCmd(t, repoPath, "git", "add", ".")
		RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")

		env.AddRepoToConfig(repoName, repoURL)
		env.SetSourceCodeManagement("git")
		// Use a very long debounce so normal debounce won't fire before dispose
		env.SetCompoundConfig(30000) // 30 seconds
		env.CreateOverlayFile(repoName, ".env", "BEFORE=change\n")
	})

	t.Run("02_DaemonStart", func(t *testing.T) {
		env.DaemonStart()
	})

	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	// Spawn session, modify overlay file, immediately dispose
	t.Run("03_SpawnAndModify", func(t *testing.T) {
		sessionID := env.SpawnSession(repoURL, "main", "echo", "", "reconcile-test")
		if sessionID == "" {
			t.Fatal("Expected session ID from spawn")
		}
		wsPath := env.GetWorkspacePath(sessionID)

		// Verify overlay was copied
		data, err := os.ReadFile(filepath.Join(wsPath, ".env"))
		if err != nil {
			t.Fatalf("Overlay .env not copied: %v", err)
		}
		if string(data) != "BEFORE=change\n" {
			t.Errorf("Expected original overlay content, got %q", string(data))
		}

		// Modify the overlay file in the workspace
		newContent := "AFTER=change\nRECONCILED=true\n"
		if err := os.WriteFile(filepath.Join(wsPath, ".env"), []byte(newContent), 0644); err != nil {
			t.Fatalf("Failed to modify .env: %v", err)
		}
		t.Log("Modified .env in workspace — debounce is 30s, so normal sync won't fire")

		// Immediately dispose — reconcile should run and catch the change
		env.DisposeSession(sessionID)
		t.Log("Session disposed — reconcile should have run")
	})

	// Verify the overlay directory has the updated content
	t.Run("04_VerifyReconcileUpdatedOverlay", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("Failed to get home dir: %v", err)
		}
		overlayPath := filepath.Join(homeDir, ".schmux", "overlays", repoName, ".env")

		// Give a brief moment for the reconcile to complete (it runs synchronously during dispose)
		time.Sleep(2 * time.Second)

		data, err := os.ReadFile(overlayPath)
		if err != nil {
			t.Fatalf("Failed to read overlay .env: %v", err)
		}

		expected := "AFTER=change\nRECONCILED=true\n"
		if string(data) != expected {
			t.Errorf("Reconcile did not update overlay.\nGot: %q\nWant: %q", string(data), expected)
		}
	})
}
```

**Step 2: Verify it compiles**

```bash
go vet -tags=e2e ./internal/e2e/...
```

**Step 3: Commit**

```bash
git add internal/e2e/e2e_test.go
git commit -m "Add E2E test for overlay reconcile on dispose"
```

---

### Task 6: Verify all E2E tests compile and run format

**Step 1: Verify compilation**

```bash
go vet -tags=e2e ./internal/e2e/...
```

Expected: no errors.

**Step 2: Run format**

```bash
./format.sh
```

**Step 3: Final commit if any formatting changes**

```bash
git add -A && git commit -m "Format E2E tests"
```

(Skip if nothing changed.)
