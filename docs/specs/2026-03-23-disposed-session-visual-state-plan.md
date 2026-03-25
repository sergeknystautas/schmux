# Disposed Session/Workspace Visual State — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a session or workspace is being disposed, gray it out in the sidebar immediately so the user knows it's being torn down.

**Architecture:** Add `disposing` status to sessions and workspaces, set it before teardown begins, broadcast via WebSocket. Client renders disposing items with reduced opacity and disables interaction. Workspaces also gain `provisioning`/`running`/`failed` lifecycle statuses.

**Tech Stack:** Go (backend state + handlers), React/TypeScript (dashboard), CSS (visual styling)

---

### Task 1: Add workspace status constants and field

**Files:**

- Modify: `internal/state/state.go:60-93`

- [ ] **Step 1: Write the test**

Create a test that verifies workspace status constants exist and the `Status` field is persisted in JSON.

```go
// In internal/state/state_test.go
func TestWorkspaceStatusConstants(t *testing.T) {
	if state.WorkspaceStatusProvisioning != "provisioning" {
		t.Errorf("expected provisioning, got %s", state.WorkspaceStatusProvisioning)
	}
	if state.WorkspaceStatusRunning != "running" {
		t.Errorf("expected running, got %s", state.WorkspaceStatusRunning)
	}
	if state.WorkspaceStatusFailed != "failed" {
		t.Errorf("expected failed, got %s", state.WorkspaceStatusFailed)
	}
	if state.WorkspaceStatusDisposing != "disposing" {
		t.Errorf("expected disposing, got %s", state.WorkspaceStatusDisposing)
	}
}

func TestWorkspaceStatusPersisted(t *testing.T) {
	w := state.Workspace{
		ID:     "test-1",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   "/tmp/test",
		Status: state.WorkspaceStatusRunning,
	}
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"status":"running"`) {
		t.Errorf("expected status in JSON, got %s", string(data))
	}

	var w2 state.Workspace
	if err := json.Unmarshal(data, &w2); err != nil {
		t.Fatal(err)
	}
	if w2.Status != state.WorkspaceStatusRunning {
		t.Errorf("expected running after roundtrip, got %s", w2.Status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/state/ -run TestWorkspaceStatus -v`
Expected: FAIL — constants and field don't exist yet.

- [ ] **Step 3: Add constants and field**

In `internal/state/state.go`, after the session status constants block (line 67), add:

```go
// Workspace status constants.
const (
	WorkspaceStatusProvisioning = "provisioning"
	WorkspaceStatusRunning      = "running"
	WorkspaceStatusFailed       = "failed"
	WorkspaceStatusDisposing    = "disposing"
)
```

Add `Status` field to the `Workspace` struct (after `PortBlock` field, line 92):

```go
	Status          string            `json:"status,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/state/ -run TestWorkspaceStatus -v`
Expected: PASS

- [ ] **Step 5: Add session disposing constant**

In `internal/state/state.go`, add to the session status constants block (after line 66):

```go
	SessionStatusDisposing    = "disposing"
```

Update the comment on `Session.Status` (line 137) from:

```go
	Status       string `json:"status,omitempty"` // Status for remote sessions: "provisioning", "running", "failed"
```

to:

```go
	Status       string `json:"status,omitempty"` // "provisioning", "running", "failed", "disposing" (used for all sessions during disposal, remote sessions during lifecycle)
```

- [ ] **Step 6: Run all state tests**

Run: `go test ./internal/state/ -v`
Expected: PASS

---

### Task 2: Add workspace status to API response

**Files:**

- Modify: `internal/dashboard/handlers_sessions.go:54-83` (WorkspaceResponseItem struct)
- Modify: `internal/dashboard/handlers_sessions.go:158-184` (buildSessionsResponse)

- [ ] **Step 1: Write the test**

Add a test to `internal/dashboard/broadcast_test.go` that verifies the broadcast includes workspace status.

```go
func TestBroadcastIncludesWorkspaceStatus(t *testing.T) {
	// Use existing test setup pattern from broadcast_test.go
	// Add a workspace with Status set
	st.AddWorkspace(state.Workspace{
		ID:     "ws-status",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
		Status: state.WorkspaceStatusRunning,
	})
	srv.BroadcastSessions()

	// Wait for debounce + margin
	msg := readDashboardMsg(t, conn, 500*time.Millisecond)
	workspaces := msg["workspaces"].([]interface{})
	ws := workspaces[0].(map[string]interface{})
	if ws["status"] != "running" {
		t.Errorf("expected status=running, got %v", ws["status"])
	}
}
```

Note: Adapt this to match the exact test setup pattern used in existing `broadcast_test.go` tests (check the file for `srv`, `st`, `conn` variable setup).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dashboard/ -run TestBroadcastIncludesWorkspaceStatus -v`
Expected: FAIL — `WorkspaceResponseItem` has no `Status` field yet.

- [ ] **Step 3: Add Status field to WorkspaceResponseItem**

In `internal/dashboard/handlers_sessions.go`, add to `WorkspaceResponseItem` struct after the `Previews` field (line 82), not after `RemoteUniqueCommits`:

```go
	Status                  string                `json:"status,omitempty"`
```

- [ ] **Step 4: Copy status in buildSessionsResponse**

In `internal/dashboard/handlers_sessions.go`, inside the `workspaceMap[ws.ID] = &WorkspaceResponseItem{` block (around line 158-186), add after the `Previews` assignment:

```go
			Status:                  ws.Status,
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/dashboard/ -run TestBroadcastIncludesWorkspaceStatus -v`
Expected: PASS

---

### Task 3: Set disposing status in session manager

**Files:**

- Modify: `internal/session/manager.go:1147-1218` (Dispose method)

- [ ] **Step 1: Write the test**

In `internal/session/manager_test.go` (or the appropriate test file), write a test that verifies:

1. After calling a method to mark a session as disposing, its status is `disposing`
2. The status is persisted (state.Save called)

The test should verify that when `Dispose()` is called, the session status is set to `disposing` early in the flow. Since `Dispose()` also removes the session, we need to verify the intermediate state. One approach: add a `MarkDisposing(sessionID)` method that the handler calls before `Dispose()`.

Actually, based on the spec, the manager should have a `MarkSessionDisposing` method the handler calls before the blocking `Dispose()`:

```go
func TestMarkSessionDisposing(t *testing.T) {
	// Setup: create state with a session in "stopped" status
	st := state.New(filepath.Join(t.TempDir(), "state.json"), slog.Default())
	st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "https://example.com/r.git", Branch: "main", Path: t.TempDir()})
	st.AddSession(state.Session{ID: "sess-1", WorkspaceID: "ws-1", Target: "claude", TmuxSession: "test", Status: "stopped"})

	// Create manager with this state (adapt to actual constructor pattern)
	// Call MarkSessionDisposing("sess-1")
	prevStatus, err := mgr.MarkSessionDisposing("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if prevStatus != "stopped" {
		t.Errorf("expected previous status 'stopped', got %q", prevStatus)
	}

	// Verify session now has disposing status
	sess, found := st.GetSession("sess-1")
	if !found {
		t.Fatal("session not found")
	}
	if sess.Status != state.SessionStatusDisposing {
		t.Errorf("expected disposing, got %q", sess.Status)
	}
}

func TestMarkSessionDisposingIdempotent(t *testing.T) {
	// Setup: session already in "disposing" status
	// Call MarkSessionDisposing — should return "disposing" as previous, no error
	prevStatus, err := mgr.MarkSessionDisposing("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if prevStatus != state.SessionStatusDisposing {
		t.Errorf("expected disposing (idempotent), got %q", prevStatus)
	}
}
```

Adapt to match existing test patterns in the session package (constructor, mock state, etc.).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestMarkSessionDisposing -v`
Expected: FAIL — method doesn't exist.

- [ ] **Step 3: Implement MarkSessionDisposing**

Add to `internal/session/manager.go`:

```go
// MarkSessionDisposing sets a session's status to "disposing" and saves state.
// Called by handlers before the blocking Dispose() call to give immediate visual feedback.
// Returns the previous status (for rollback on failure) and any error.
func (m *Manager) MarkSessionDisposing(sessionID string) (previousStatus string, err error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}
	if sess.Status == state.SessionStatusDisposing {
		return state.SessionStatusDisposing, nil // already disposing — idempotent
	}
	previousStatus = sess.Status
	if !m.state.UpdateSessionFunc(sessionID, func(s *state.Session) {
		s.Status = state.SessionStatusDisposing
	}) {
		return previousStatus, fmt.Errorf("session disappeared during status update: %s", sessionID)
	}
	if err := m.state.Save(); err != nil {
		return previousStatus, fmt.Errorf("failed to save state: %w", err)
	}
	return previousStatus, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/session/ -run TestMarkSessionDisposing -v`
Expected: PASS

- [ ] **Step 5: Add RevertSessionStatus method**

```go
// RevertSessionStatus restores a session's status after a failed disposal.
func (m *Manager) RevertSessionStatus(sessionID, previousStatus string) {
	m.state.UpdateSessionFunc(sessionID, func(s *state.Session) {
		s.Status = previousStatus
	})
	if err := m.state.Save(); err != nil {
		m.logger.Error("failed to save state after status revert", "session_id", sessionID, "err", err)
	}
}
```

- [ ] **Step 6: Run all session tests**

Run: `go test ./internal/session/ -v`
Expected: PASS

---

### Task 4: Set disposing status in workspace manager

**Files:**

- Modify: `internal/workspace/manager.go`

- [ ] **Step 1: Write the test**

```go
func TestMarkWorkspaceDisposing(t *testing.T) {
	// Setup state with a workspace with Status "running"
	// Call MarkWorkspaceDisposing
	// Verify status is "disposing"
	// Verify state was saved
}

func TestMarkWorkspaceDisposingIdempotent(t *testing.T) {
	// Setup state with a workspace with Status "disposing"
	// Call MarkWorkspaceDisposing
	// Should return "disposing" as previous status, no error
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/workspace/ -run TestMarkWorkspaceDisposing -v`
Expected: FAIL

- [ ] **Step 3: Implement MarkWorkspaceDisposing and RevertWorkspaceStatus**

Add to `internal/workspace/manager.go`:

```go
// MarkWorkspaceDisposing sets a workspace's status to "disposing" and saves state.
// Returns the previous status (for rollback) and any error.
func (m *Manager) MarkWorkspaceDisposing(workspaceID string) (previousStatus string, err error) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return "", fmt.Errorf("workspace not found: %s", workspaceID)
	}
	if w.Status == state.WorkspaceStatusDisposing {
		return state.WorkspaceStatusDisposing, nil
	}
	previousStatus = w.Status
	w.Status = state.WorkspaceStatusDisposing
	if err := m.state.UpdateWorkspace(w); err != nil {
		return previousStatus, fmt.Errorf("failed to update workspace: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return previousStatus, fmt.Errorf("failed to save state: %w", err)
	}
	return previousStatus, nil
}

// RevertWorkspaceStatus restores a workspace's status after a failed disposal.
func (m *Manager) RevertWorkspaceStatus(workspaceID, previousStatus string) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return
	}
	w.Status = previousStatus
	if err := m.state.UpdateWorkspace(w); err != nil {
		m.logger.Error("failed to revert workspace status", "workspace_id", workspaceID, "err", err)
		return
	}
	if err := m.state.Save(); err != nil {
		m.logger.Error("failed to save state after status revert", "workspace_id", workspaceID, "err", err)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/workspace/ -run TestMarkWorkspaceDisposing -v`
Expected: PASS

---

### Task 5: Wire disposing status into dispose handlers

**Files:**

- Modify: `internal/dashboard/handlers_dispose.go`

- [ ] **Step 1: Write the test**

Write a test for `handleDispose` that verifies:

1. A session's status becomes `disposing` before the actual disposal happens
2. If the session is already `disposing`, the handler returns 200 immediately

Adapt to existing handler test patterns in the dashboard package.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dashboard/ -run TestHandleDisposeMarksDisposing -v`
Expected: FAIL

- [ ] **Step 3: Update handleDispose (session)**

Replace the body of `handleDispose` in `internal/dashboard/handlers_dispose.go:15-56`:

```go
func (s *Server) handleDispose(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		writeJSONError(w, "session ID is required", http.StatusBadRequest)
		return
	}

	// Mark as disposing and broadcast immediately for visual feedback
	sessionLog := logging.Sub(s.logger, "session")
	prevStatus, err := s.session.MarkSessionDisposing(sessionID)
	if err != nil {
		sessionLog.Error("mark disposing failed", "session_id", sessionID, "err", err)
		writeJSONError(w, fmt.Sprintf("Failed to dispose session: %v", err), http.StatusInternalServerError)
		return
	}
	// Idempotent: if already disposing, return success
	if prevStatus == "disposing" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}
	s.BroadcastSessions()

	// Proceed with actual teardown
	ctx, cancel := context.WithTimeout(context.Background(), s.config.DisposeGracePeriod()+10*time.Second)
	defer cancel()
	if err := s.session.Dispose(ctx, sessionID); err != nil {
		sessionLog.Error("dispose failed", "session_id", sessionID, "err", err)
		// Revert status on failure
		s.session.RevertSessionStatus(sessionID, prevStatus)
		s.BroadcastSessions()
		writeJSONError(w, fmt.Sprintf("Failed to dispose session: %v", err), http.StatusInternalServerError)
		return
	}
	sessionLog.Info("dispose success", "session_id", sessionID)

	// Clean up rotation lock for disposed session
	s.rotationLocksMu.Lock()
	delete(s.rotationLocks, sessionID)
	s.rotationLocksMu.Unlock()

	// Delete previews owned by this session
	if s.previewManager != nil {
		if deleted, err := s.previewManager.DeleteBySession(sessionID); err != nil {
			previewLog := logging.Sub(s.logger, "preview")
			previewLog.Warn("preview cleanup on dispose failed", "session_id", sessionID, "err", err)
		} else if deleted > 0 {
			go s.BroadcastSessions()
		}
	}

	// Broadcast update (session now removed from state)
	go s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		s.logger.Error("failed to encode response", "handler", "dispose-session", "err", err)
	}
}
```

- [ ] **Step 4: Update handleDisposeWorkspace**

Replace the body of `handleDisposeWorkspace` in `internal/dashboard/handlers_dispose.go:58-100`:

```go
func (s *Server) handleDisposeWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		writeJSONError(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	// Block disposal of the workspace that is live in dev mode
	if devPath := s.devSourceWorkspacePath(); devPath != "" {
		if ws, ok := s.state.GetWorkspace(workspaceID); ok && ws.Path == devPath {
			writeJSONError(w, "cannot dispose workspace that is live in dev mode", http.StatusConflict)
			return
		}
	}

	// Mark as disposing and broadcast immediately
	workspaceLog := logging.Sub(s.logger, "workspace")
	prevStatus, err := s.workspace.MarkWorkspaceDisposing(workspaceID)
	if err != nil {
		workspaceLog.Error("mark disposing failed", "workspace_id", workspaceID, "err", err)
		writeJSONError(w, fmt.Sprintf("Failed to dispose workspace: %v", err), http.StatusInternalServerError)
		return
	}
	if prevStatus == "disposing" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}
	s.BroadcastSessions()

	// Proceed with actual teardown
	wsCtx, wsCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer wsCancel()

	if err := s.workspace.Dispose(wsCtx, workspaceID); err != nil {
		workspaceLog.Error("dispose failed", "workspace_id", workspaceID, "err", err)
		s.workspace.RevertWorkspaceStatus(workspaceID, prevStatus)
		s.BroadcastSessions()
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.previewManager != nil {
		if err := s.previewManager.DeleteWorkspace(workspaceID); err != nil {
			previewLog := logging.Sub(s.logger, "preview")
			previewLog.Warn("dispose cleanup failed", "workspace_id", workspaceID, "err", err)
		}
	}
	workspaceLog.Info("dispose success", "workspace_id", workspaceID)

	go s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		s.logger.Error("failed to encode response", "handler", "dispose-workspace", "err", err)
	}
}
```

- [ ] **Step 5: Update handleDisposeWorkspaceAll**

Replace the body of `handleDisposeWorkspaceAll` in `internal/dashboard/handlers_dispose.go:102-190`. Key changes:

1. Mark workspace + all sessions as disposing, broadcast before teardown
2. Add idempotency check
3. Add post-completion broadcast
4. Revert workspace on failure

```go
func (s *Server) handleDisposeWorkspaceAll(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		writeJSONError(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	// Block disposal of the workspace that is live in dev mode
	if devPath := s.devSourceWorkspacePath(); devPath != "" {
		if ws, ok := s.state.GetWorkspace(workspaceID); ok && ws.Path == devPath {
			writeJSONError(w, "cannot dispose workspace that is live in dev mode", http.StatusConflict)
			return
		}
	}

	// Mark workspace as disposing
	workspaceLog := logging.Sub(s.logger, "workspace")
	prevWsStatus, err := s.workspace.MarkWorkspaceDisposing(workspaceID)
	if err != nil {
		workspaceLog.Error("mark disposing failed", "workspace_id", workspaceID, "err", err)
		writeJSONError(w, fmt.Sprintf("Failed to dispose workspace: %v", err), http.StatusInternalServerError)
		return
	}
	if prevWsStatus == "disposing" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	// Mark all sessions as disposing
	sessions := s.state.GetSessions()
	var wsSessions []string
	for _, sess := range sessions {
		if sess.WorkspaceID == workspaceID {
			wsSessions = append(wsSessions, sess.ID)
			if _, markErr := s.session.MarkSessionDisposing(sess.ID); markErr != nil {
				workspaceLog.Warn("failed to mark session disposing", "session_id", sess.ID, "err", markErr)
			}
		}
	}

	// Broadcast immediately — everything grays out at once
	s.BroadcastSessions()

	// Dispose all sessions concurrently
	type disposeResult struct {
		sessionID string
		err       error
	}
	results := make(chan disposeResult, len(wsSessions))
	for _, sid := range wsSessions {
		go func(id string) {
			// Use a generous fixed timeout independent of DisposeGracePeriod.
			// DisposeGracePeriod controls the interactive user-facing delay,
			// but bulk disposal (especially under CPU/IO contention) needs
			// enough headroom for tmux subprocess operations to complete.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			results <- disposeResult{sessionID: id, err: s.session.Dispose(ctx, id)}
		}(sid)
	}

	var sessionsDisposed []string
	for range wsSessions {
		res := <-results
		if res.err != nil {
			workspaceLog.Error("dispose-all session failed", "session_id", res.sessionID, "err", res.err)
		} else {
			sessionsDisposed = append(sessionsDisposed, res.sessionID)
			workspaceLog.Info("dispose-all session disposed", "session_id", res.sessionID)
		}
	}

	// Clean up rotation locks for disposed sessions
	s.rotationLocksMu.Lock()
	for _, sid := range sessionsDisposed {
		delete(s.rotationLocks, sid)
	}
	s.rotationLocksMu.Unlock()

	// Dispose the workspace
	wsCtx, wsCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer wsCancel()
	if err := s.workspace.DisposeForce(wsCtx, workspaceID); err != nil {
		workspaceLog.Error("dispose-all workspace failed", "workspace_id", workspaceID, "err", err)
		s.workspace.RevertWorkspaceStatus(workspaceID, prevWsStatus)
		go s.BroadcastSessions()
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.previewManager != nil {
		if err := s.previewManager.DeleteWorkspace(workspaceID); err != nil {
			previewLog := logging.Sub(s.logger, "preview")
			previewLog.Warn("dispose-all cleanup failed", "workspace_id", workspaceID, "err", err)
		}
	}
	workspaceLog.Info("dispose-all success", "workspace_id", workspaceID, "sessions_disposed", len(sessionsDisposed))

	// Post-completion broadcast (was missing from original handler)
	go s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status":            "ok",
		"sessions_disposed": len(sessionsDisposed),
	}); err != nil {
		s.logger.Error("failed to encode response", "handler", "dispose-all", "err", err)
	}
}
```

- [ ] **Step 6: Run handler tests**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS

---

### Task 6: Wire workspace status into creation flow

**Files:**

- Modify: `internal/workspace/manager.go:480-578` (create method)
- Modify: `internal/workspace/manager.go:378-477` (GetOrCreate method)

- [ ] **Step 1: Write the test**

```go
func TestWorkspaceCreationSetsProvisioningThenRunning(t *testing.T) {
	// After create() returns, workspace status should be "provisioning"
	// (it will be set to running after prepare() succeeds in GetOrCreate)
	// Test the create() method sets provisioning
}

func TestReusedWorkspaceGetsRunningStatus(t *testing.T) {
	// Pre-existing workspace with empty status
	// After GetOrCreate reuses it, status should be "running"
}
```

Adapt to match existing workspace manager test patterns.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/workspace/ -run TestWorkspaceCreation -v`
Expected: FAIL

- [ ] **Step 3: Set provisioning status in create()**

In `internal/workspace/manager.go`, in the `create()` method, when constructing the workspace struct (line 565):

```go
	w := state.Workspace{
		ID:     workspaceID,
		Repo:   repoURL,
		Branch: branch,
		Path:   workspacePath,
		VCS:    repoConfig.VCS,
		Status: state.WorkspaceStatusProvisioning,
	}
```

- [ ] **Step 4: Set running status after prepare() in GetOrCreate()**

In `internal/workspace/manager.go`, in `GetOrCreate()`:

After `m.prepare(ctx, w.ID, branch)` succeeds for the **new workspace path** (around line 472-474), add:

```go
	// Prepare the workspace
	if err := m.prepare(ctx, w.ID, w.Branch); err != nil {
		// Mark workspace as failed
		w.Status = state.WorkspaceStatusFailed
		m.state.UpdateWorkspace(*w)
		m.state.Save()
		return nil, fmt.Errorf("failed to prepare workspace: %w", err)
	}
	// Mark workspace as running after successful prepare
	w.Status = state.WorkspaceStatusRunning
	m.state.UpdateWorkspace(*w)
	m.state.Save()
```

For the **reused workspace paths** (around lines 408 and 443), after `m.prepare()` succeeds:

```go
	// Ensure reused workspace has running status (backfill for pre-existing workspaces)
	if w.Status == "" {
		w.Status = state.WorkspaceStatusRunning
		m.state.UpdateWorkspace(w)
		m.state.Save()
	}
```

Note: The `create()` function returns a `*state.Workspace`, and `GetOrCreate()` uses the returned pointer. Read the actual code carefully to use the right variable type (`w` vs `*w`) and determine exact insertion points. The `prepare()` call for new workspaces is at line 472; for reused workspaces at lines 408 and 443.

**Out of scope:** Remote workspace creation (`RemoteHostID != ""`) follows a different path. Wiring `provisioning`/`running` into remote workspace creation is deferred — remote workspaces will have empty status until disposed, and the client treats empty status the same as `running`.

- [ ] **Step 5: Write test for workspace creation failure setting `failed` status**

```go
func TestWorkspaceCreationFailureSetsFailedStatus(t *testing.T) {
	// Setup: mock prepare() to return an error
	// Call GetOrCreate
	// Verify workspace status is "failed"
}
```

Adapt to match existing workspace manager test patterns.

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/workspace/ -run TestWorkspaceCreation -v`
Expected: PASS

- [ ] **Step 7: Run all workspace tests**

Run: `go test ./internal/workspace/ -v`
Expected: PASS

---

### Task 7: Add TypeScript status fields

**Files:**

- Modify: `assets/dashboard/src/lib/types.ts:1-24` (SessionResponse)
- Modify: `assets/dashboard/src/lib/types.ts:26-53` (WorkspaceResponse)

- [ ] **Step 1: Add status to SessionResponse**

In `assets/dashboard/src/lib/types.ts`, add after the `nudge_seq` field (line 13):

```typescript
  status?: string;
```

- [ ] **Step 2: Add status to WorkspaceResponse**

In `assets/dashboard/src/lib/types.ts`, add after the `previews` field (line 52):

```typescript
  status?: string;
```

- [ ] **Step 3: Run frontend tests**

Run: `./test.sh --quick`
Expected: PASS (type additions are non-breaking)

---

### Task 8: Sidebar visual state for disposing

**Files:**

- Modify: `assets/dashboard/src/components/AppShell.tsx` (sidebar rendering)
- Modify: `assets/dashboard/src/styles/global.css` (new CSS classes)

- [ ] **Step 1: Write the test**

In the appropriate test file for AppShell (check `assets/dashboard/src/` for existing AppShell tests), add:

```typescript
test('disposing workspace gets nav-workspace--disposing class', () => {
  // Render AppShell with a workspace that has status: 'disposing'
  // Assert the workspace header div has className including 'nav-workspace--disposing'
});

test('disposing session gets nav-session--disposing class', () => {
  // Render with a session that has status: 'disposing'
  // Assert the session div has className including 'nav-session--disposing'
});

test('disposing session is not clickable', () => {
  // Render with a disposing session
  // Click on it
  // Assert navigation did NOT happen
});
```

Adapt to match existing test patterns (React Testing Library, mock contexts, etc.).

- [ ] **Step 2: Run test to verify it fails**

Run: `./test.sh --quick`
Expected: FAIL — classes not applied yet.

- [ ] **Step 3: Add CSS classes**

In `assets/dashboard/src/styles/global.css`, after the `.nav-session--active` block (around line 1138-1140), add:

```css
.nav-workspace--disposing {
  opacity: 0.5;
  pointer-events: none;
}

.nav-workspace--disposing .nav-workspace__header {
  color: var(--color-text-muted, #888);
}

.nav-session--disposing {
  opacity: 0.5;
  pointer-events: none;
  color: var(--color-text-muted, #888);
}
```

- [ ] **Step 4: Add disposing class to workspace header**

In `assets/dashboard/src/components/AppShell.tsx`, find the workspace div that applies `nav-workspace--active` class (search for `nav-workspace--active`). Add the disposing class conditionally:

```tsx
const isDisposing = workspace.status === 'disposing';
// In the className:
className={`nav-workspace${isActiveWorkspace ? ' nav-workspace--active' : ''}${isDisposing ? ' nav-workspace--disposing' : ''}`}
```

- [ ] **Step 5: Add disposing class to session row**

Find the session div that applies `nav-session--active` (around line 931). Add:

```tsx
const isSessionDisposing = sess.status === 'disposing';
// In the className (line 931):
className={`nav-session${isActive ? ' nav-session--active' : ''}${isSessionDisposing ? ' nav-session--disposing' : ''}`}
```

- [ ] **Step 6: Disable click handler for disposing sessions**

In the `onClick` handler for session rows (around line 935):

```tsx
onClick={() => !isSessionDisposing && handleSessionClick(sess.id)}
```

And the `onKeyDown` handler (around line 938-943):

```tsx
onKeyDown={(e) => {
  if (isSessionDisposing) return;
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault();
    handleSessionClick(sess.id);
  }
}}
```

- [ ] **Step 7: Disable click handler for disposing workspaces**

Find the workspace click handler (`handleWorkspaceClick` or equivalent, around line 778 in AppShell.tsx). Add a guard at the top:

```tsx
if (workspace.status === 'disposing') return;
```

This prevents navigating to a disposing workspace when clicking its header in the sidebar.

- [ ] **Step 8: Run test to verify it passes**

Run: `./test.sh --quick`
Expected: PASS

---

### Task 9: Disable dispose triggers for disposing items

**Files:**

- Modify: `assets/dashboard/src/components/WorkspaceHeader.tsx`
- Modify: `assets/dashboard/src/routes/SessionDetailPage.tsx`
- Modify: `assets/dashboard/src/components/SessionTabs.tsx`
- Modify: `assets/dashboard/src/components/AppShell.tsx` (keyboard shortcuts)

- [ ] **Step 1: Write the tests**

Test that dispose buttons are disabled when status is `disposing`.

- [ ] **Step 2: Disable dispose button in WorkspaceHeader**

In `assets/dashboard/src/components/WorkspaceHeader.tsx`, find the dispose button `disabled` prop (around line 260):

```tsx
disabled={isLocked || isDevLive || hasRunningSessions}
```

Change to:

```tsx
disabled={isLocked || isDevLive || hasRunningSessions || workspace.status === 'disposing'}
```

Note: Check the exact variable name — the workspace prop might be named differently. Read the component to verify.

- [ ] **Step 3: Disable dispose in SessionDetailPage**

In `assets/dashboard/src/routes/SessionDetailPage.tsx`, the `W` keyboard shortcut registers a `handleDispose` callback (around line 431). The guard must go inside the `handleDispose` function itself (around line 406), not in the useEffect registration block, so the handler reference includes the check:

```tsx
// Inside handleDispose function body, at the top:
if (session?.status === 'disposing') return;
```

Also find the dispose button and add `disabled={session?.status === 'disposing'}` if it doesn't already have a disabled prop.

- [ ] **Step 4: Disable dispose in SessionTabs**

In `assets/dashboard/src/components/SessionTabs.tsx`, the dispose button uses a local `disabled` variable (around line 355: `const disabled = isLocked;`), not a prop. Update this to include disposing status:

```tsx
const disabled = isLocked || sess.status === 'disposing';
```

Also add a guard in the `handleDispose` callback (around line 323):

```tsx
const handleDispose = async (sessionId: string, event: React.MouseEvent) => {
    event.stopPropagation();
    const sess = sessions.find((s) => s.id === sessionId);
    if (sess?.status === 'disposing') return;
    // ... rest of handler
```

- [ ] **Step 5: Guard keyboard shortcuts in AppShell**

In `assets/dashboard/src/components/AppShell.tsx`, find the `Shift+W` keyboard handler (around line 537-562). Add a check:

```tsx
if (workspace.status === 'disposing') return;
```

This should go alongside the existing `hasRunningSessions` check.

- [ ] **Step 6: Skip disposing workspaces in keyboard navigation**

In `assets/dashboard/src/lib/navigation.ts`, update `findNextWorkspaceWithSessions` (line 39-48):

```typescript
export function findNextWorkspaceWithSessions(
  workspaces: WorkspaceResponse[],
  currentIndex: number,
  direction: 1 | -1
): number {
  for (let i = currentIndex + direction; i >= 0 && i < workspaces.length; i += direction) {
    if (workspaces[i].sessions?.length && workspaces[i].status !== 'disposing') return i;
  }
  return -1;
}
```

- [ ] **Step 7: Run all frontend tests**

Run: `./test.sh --quick`
Expected: PASS

---

### Task 10: Daemon startup reconciliation

**Files:**

- Modify: `internal/daemon/daemon.go` (near line 679, after stale hosts reconciliation)

- [ ] **Step 1: Write the test**

Test that on startup, workspaces/sessions stuck in `disposing` status are retried or reverted.

- [ ] **Step 2: Run test to verify it fails**

Expected: FAIL — reconciliation logic doesn't exist.

- [ ] **Step 3: Implement reconciliation**

In `internal/daemon/daemon.go`, after the stale hosts reconciliation block (after line 682), add. This runs in a background goroutine to avoid blocking daemon startup (disposal can take up to 60s per workspace):

```go
	// Reconcile workspaces/sessions stuck in "disposing" status from a previous crash.
	// Run in a goroutine to avoid blocking daemon startup.
	go func() {
		for _, w := range st.GetWorkspaces() {
			if w.Status == state.WorkspaceStatusDisposing {
				logger.Info("retrying stuck workspace disposal", "workspace_id", w.ID)
				retryCtx, retryCancel := context.WithTimeout(context.Background(), 60*time.Second)
				if err := wm.DisposeForce(retryCtx, w.ID); err != nil {
					logger.Warn("stuck workspace disposal failed, reverting to running", "workspace_id", w.ID, "err", err)
					wm.RevertWorkspaceStatus(w.ID, state.WorkspaceStatusRunning)
				}
				retryCancel()
			}
		}

		// Reconcile sessions stuck in "disposing" status.
		for _, sess := range st.GetSessions() {
			if sess.Status == state.SessionStatusDisposing {
				logger.Info("retrying stuck session disposal", "session_id", sess.ID)
				retryCtx, retryCancel := context.WithTimeout(context.Background(), 30*time.Second)
				if err := sm.Dispose(retryCtx, sess.ID); err != nil {
					logger.Warn("stuck session disposal failed, reverting to stopped", "session_id", sess.ID, "err", err)
					sm.RevertSessionStatus(sess.ID, state.SessionStatusStopped)
				}
				retryCancel()
			}
		}

		server.BroadcastSessions()
	}()
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/ -v`
Expected: PASS

---

### Task 11: Update docs/api.md

**Files:**

- Modify: `docs/api.md`

- [ ] **Step 1: Document workspace status field**

Add `status` to the workspace response object documentation. Values: `provisioning`, `running`, `failed`, `disposing`. Omitted for pre-existing workspaces (treat as `running`).

- [ ] **Step 2: Document session disposing status**

Add `disposing` to the documented session status values.

- [ ] **Step 3: Document idempotent dispose behavior**

Note that dispose endpoints return 200 OK if the item is already in `disposing` status.

---

### Task 12: Full test suite and format

- [ ] **Step 1: Format code**

Run: `./format.sh`
Expected: exit code 0 or 2 (both are success)

- [ ] **Step 2: Run full test suite**

Run: `./test.sh --quick`
Expected: All tests pass (backend + frontend)

- [ ] **Step 3: Verify build**

Run: `go build ./cmd/schmux`
Expected: Build succeeds

- [ ] **Step 4: Build dashboard**

Run: `go run ./cmd/build-dashboard`
Expected: Build succeeds
