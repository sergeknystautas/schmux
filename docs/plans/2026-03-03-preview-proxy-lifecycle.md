# Preview Proxy Lifecycle Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix stranded preview tabs by adding session ownership tracking and PID-tree port verification to the preview proxy lifecycle.

**Architecture:** Add `SourceSessionID` to `WorkspacePreview`. On session disposal, directly delete that session's previews. In the 5s reconcile loop, verify the source session's PID tree still owns the target port — if not, delete. Remove the external POST endpoint since previews are internal-only. On daemon restart, reconcile naturally recreates proxies for still-alive sessions.

**Tech Stack:** Go (backend state + preview manager), TypeScript type generation

---

### Task 1: Add `SourceSessionID` to preview state

**Files:**

- Modify: `internal/state/state.go:94-105` (WorkspacePreview struct)
- Modify: `internal/preview/manager.go:103-174` (CreateOrGet signature + usage)

**Step 1: Write failing test**

Add to `internal/preview/manager_test.go`:

```go
func TestManagerCreateOrGetSetsSourceSessionID(t *testing.T) {
	ws := state.Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()}
	st, _ := newPreviewTestState(t, ws)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	port := testServerPort(upstream)

	m := NewManager(st, 3, 20, false, 53000, 10, false, "", "", nil)
	defer m.Stop()

	ctx := context.Background()
	p, err := m.CreateOrGet(ctx, ws, "127.0.0.1", port, "sess-abc")
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}
	if p.SourceSessionID != "sess-abc" {
		t.Fatalf("expected source session 'sess-abc', got %q", p.SourceSessionID)
	}

	// Second call reuses existing, source session ID stays
	p2, err := m.CreateOrGet(ctx, ws, "127.0.0.1", port, "sess-xyz")
	if err != nil {
		t.Fatalf("reuse preview: %v", err)
	}
	if p2.SourceSessionID != "sess-abc" {
		t.Fatalf("expected original source session preserved, got %q", p2.SourceSessionID)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/preview/ -run TestManagerCreateOrGetSetsSourceSessionID -v`
Expected: compile error — `CreateOrGet` has wrong arity

**Step 3: Add field to state and update CreateOrGet signature**

In `internal/state/state.go`, add to `WorkspacePreview`:

```go
SourceSessionID string    `json:"source_session_id,omitempty"`
```

In `internal/preview/manager.go`, change `CreateOrGet` signature:

```go
func (m *Manager) CreateOrGet(ctx context.Context, ws state.Workspace, targetHost string, targetPort int, sourceSessionID string) (state.WorkspacePreview, error) {
```

Set `SourceSessionID: sourceSessionID` in the new preview struct (line ~140).

**Step 4: Fix all callers**

Both callers already have the session ID in scope:

- `internal/dashboard/preview_autodetect.go:71` — change to `s.previewManager.CreateOrGet(ctx, ws, lp.Host, lp.Port, sess.ID)`
- `internal/dashboard/preview_autodetect.go:140` — change to `s.previewManager.CreateOrGet(ctx, ws, lp.Host, lp.Port, sess.ID)`

**Step 5: Fix existing tests**

Update all `CreateOrGet` calls in `internal/preview/manager_test.go` to pass `""` as the last argument (no source session for unit tests that don't need it).

**Step 6: Run tests**

Run: `go test ./internal/preview/ -v && go test ./internal/dashboard/ -v`
Expected: PASS

**Step 7: Commit**

```
feat(preview): add SourceSessionID to preview state
```

---

### Task 2: Add `DeleteBySession` to preview manager

**Files:**

- Modify: `internal/preview/manager.go` (new method)

**Step 1: Write failing test**

Add to `internal/preview/manager_test.go`:

```go
func TestManagerDeleteBySession(t *testing.T) {
	ws := state.Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()}
	st, _ := newPreviewTestState(t, ws)

	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream1.Close()
	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream2.Close()

	m := NewManager(st, 3, 20, false, 53000, 10, false, "", "", nil)
	defer m.Stop()

	ctx := context.Background()
	p1, err := m.CreateOrGet(ctx, ws, "127.0.0.1", testServerPort(upstream1), "sess-a")
	if err != nil {
		t.Fatalf("create p1: %v", err)
	}
	p2, err := m.CreateOrGet(ctx, ws, "127.0.0.1", testServerPort(upstream2), "sess-b")
	if err != nil {
		t.Fatalf("create p2: %v", err)
	}

	deleted, err := m.DeleteBySession("sess-a")
	if err != nil {
		t.Fatalf("delete by session: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted, got %d", deleted)
	}

	if _, found := st.GetPreview(p1.ID); found {
		t.Fatal("expected p1 to be deleted")
	}
	if _, found := st.GetPreview(p2.ID); !found {
		t.Fatal("expected p2 to still exist")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/preview/ -run TestManagerDeleteBySession -v`
Expected: compile error — method doesn't exist

**Step 3: Implement DeleteBySession**

Add to `internal/preview/manager.go`:

```go
// DeleteBySession removes all previews created by the given session and stops their listeners.
func (m *Manager) DeleteBySession(sessionID string) (int, error) {
	previews := m.state.GetPreviews()
	deleted := 0
	for _, preview := range previews {
		if preview.SourceSessionID != sessionID {
			continue
		}
		if err := m.Delete(preview.WorkspaceID, preview.ID); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/preview/ -run TestManagerDeleteBySession -v`
Expected: PASS

**Step 5: Commit**

```
feat(preview): add DeleteBySession for session disposal cleanup
```

---

### Task 3: Call `DeleteBySession` on session disposal

**Files:**

- Modify: `internal/dashboard/handlers_dispose.go:15-62` (handleDispose)

**Step 1: Update handleDispose**

In `handleDispose`, replace the `ReconcileWorkspace` call (lines 46-53) with `DeleteBySession`:

```go
	// Delete previews owned by this session
	if s.previewManager != nil {
		if deleted, err := s.previewManager.DeleteBySession(sessionID); err != nil {
			previewLog := logging.Sub(s.logger, "preview")
			previewLog.Warn("preview cleanup on dispose failed", "session_id", sessionID, "err", err)
		} else if deleted > 0 {
			go s.BroadcastSessions()
		}
	}
```

This replaces the existing reconcile-based approach with direct ownership-based deletion.

**Step 2: Run tests**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS

**Step 3: Commit**

```
fix(preview): delete session-owned previews on session disposal
```

---

### Task 4: Replace reconcile logic with PID-tree port verification

**Files:**

- Modify: `internal/preview/manager.go:231-297` (ReconcileWorkspace)

This is the core change. Replace the entries-map tautology + TCP dial with PID-tree ownership verification.

**Step 1: Write failing test**

Add to `internal/preview/manager_test.go`:

```go
func TestManagerReconcileDeletesWhenSessionDead(t *testing.T) {
	ws := state.Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()}
	st, _ := newPreviewTestState(t, ws)
	// Add a session with PID that doesn't exist
	st.AddSession(state.Session{ID: "sess-dead", WorkspaceID: "ws-1", Pid: 999999})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	m := NewManager(st, 3, 20, false, 53000, 10, false, "", "", nil)
	defer m.Stop()

	ctx := context.Background()
	p, err := m.CreateOrGet(ctx, ws, "127.0.0.1", testServerPort(upstream), "sess-dead")
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}

	changed, err := m.ReconcileWorkspace(ws.ID)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !changed {
		t.Fatal("expected reconcile to report changes")
	}
	if _, found := st.GetPreview(p.ID); found {
		t.Fatal("expected preview to be removed when source session PID is dead")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/preview/ -run TestManagerReconcileDeletesWhenSessionDead -v`
Expected: FAIL — current reconcile doesn't check session PID

**Step 3: Rewrite ReconcileWorkspace**

The new `ReconcileWorkspace` needs access to `detectListeningPortsByPID`, which currently lives in `internal/dashboard/preview_autodetect.go`. Since the preview manager shouldn't depend on the dashboard package, inject a port-detection function.

Add a field to `Manager`:

```go
type PortDetector func(pid int) []ListeningPort

type ListeningPort struct {
	Host string
	Port int
}
```

Add `portDetector PortDetector` to `Manager` struct and `NewManager` params. Callers in `dashboard` pass `detectListeningPortsByPID`. Tests pass a stub.

Rewrite `ReconcileWorkspace`:

```go
func (m *Manager) ReconcileWorkspace(workspaceID string) (bool, error) {
	previews := m.state.GetWorkspacePreviews(workspaceID)
	if len(previews) == 0 {
		return false, nil
	}
	changed := false
	for _, preview := range previews {
		// Look up source session
		sess, hasSess := m.state.GetSession(preview.SourceSessionID)

		// No source session or PID dead → delete
		if !hasSess || sess.Pid <= 0 {
			if err := m.Delete(workspaceID, preview.ID); err != nil {
				return changed, err
			}
			changed = true
			continue
		}

		// Check if session's PID tree still owns the target port
		ownsPort := false
		if m.portDetector != nil {
			for _, lp := range m.portDetector(sess.Pid) {
				if lp.Port == preview.TargetPort {
					ownsPort = true
					break
				}
			}
		}

		if !ownsPort {
			if err := m.Delete(workspaceID, preview.ID); err != nil {
				return changed, err
			}
			changed = true
			continue
		}

		// Port still owned — ensure proxy listener is running
		m.mu.Lock()
		_, hasEntry := m.entries[preview.ID]
		m.mu.Unlock()
		if !hasEntry {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if _, err := m.ensureListener(ctx, preview); err != nil {
				if m.logger != nil {
					m.logger.Warn("failed to recreate listener", "preview_id", preview.ID, "err", err)
				}
			}
			cancel()
			changed = true
		}
	}
	if changed {
		if err := m.state.Save(); err != nil {
			return changed, err
		}
	}
	return changed, nil
}
```

**Step 4: Move `ListeningPort` type to preview package or a shared location**

The `ListeningPort` type is currently in `internal/dashboard/preview_autodetect.go`. Move it to `internal/preview/manager.go` (or a shared types file) so both packages can use it. Update `preview_autodetect.go` to use `preview.ListeningPort`.

**Step 5: Update NewManager signature and all callers**

Add `portDetector` parameter to `NewManager`. In `internal/dashboard/server.go` where `NewManager` is called, pass `detectListeningPortsByPID`.

**Step 6: Update test helper and existing reconcile tests**

- Update `newPreviewTestState` or test setup to pass a stub port detector
- Update `TestManagerReconcileWorkspaceRemovesStalePreview` — it tests old stale-grace logic which is being replaced. Rewrite to test the new PID-based logic.
- Ensure `TestManagerReconcileDeletesWhenSessionDead` passes

**Step 7: Run tests**

Run: `go test ./internal/preview/ -v && go test ./internal/dashboard/ -v`
Expected: PASS

**Step 8: Commit**

```
feat(preview): replace upstream TCP dial with PID-tree port verification in reconcile
```

---

### Task 5: Remove `handlePreviewsCreate` POST endpoint

**Files:**

- Modify: `internal/dashboard/server.go:651` (route registration)
- Modify: `internal/dashboard/handlers_workspace.go:108-138` (delete handler)
- Modify: `internal/dashboard/api_contract_test.go:474-538` (delete/rewrite tests)
- Modify: `docs/api.md:241-275` (remove POST docs)

**Step 1: Delete the route**

In `internal/dashboard/server.go:651`, remove:

```go
r.Post("/previews", s.handlePreviewsCreate)
```

**Step 2: Delete the handler**

In `internal/dashboard/handlers_workspace.go`, delete `handlePreviewsCreate` (lines 108-138).

**Step 3: Update tests**

- Delete `TestAPIContract_WorkspacePreviewCreateAndList` — it tests the POST endpoint
- Rewrite the list test to create a preview via state directly instead of the POST endpoint
- Delete `TestAPIContract_WorkspacePreviewRemoteWorkspaceBlocked` — it tests the POST endpoint with remote workspace

**Step 4: Update docs/api.md**

Remove the `POST /api/workspaces/{workspaceId}/previews` section (lines 241-275).

**Step 5: Run tests**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS

**Step 6: Commit**

```
fix(preview): remove external POST endpoint, previews are internal-only
```

---

### Task 6: Remove dead code and clean up

**Files:**

- Modify: `internal/preview/manager.go` (remove `cleanupLoop`, `checkUpstream`, stale grace constants)

**Step 1: Remove dead code**

- Delete `cleanupLoop` (lines 434-437) — it's a no-op
- Delete `checkUpstream` (lines 452-460) — no longer called after reconcile rewrite
- Delete `updateStatus` (lines 379-404) — only called from `ensureListener`, replace with simpler logic
- Remove `staleGrace` constant if no longer referenced
- Remove the `go m.cleanupLoop()` call in `NewManager`
- Simplify `ensureListener` — it no longer needs to call `updateStatus`, just start the listener and set status based on whether the listener bound successfully

**Step 2: Run full test suite**

Run: `./test.sh --quick`
Expected: PASS

**Step 3: Commit**

```
refactor(preview): remove dead code from pre-lifecycle cleanup
```

---

### Task 7: Regenerate TypeScript types and update frontend types

**Files:**

- Run: `go run ./cmd/gen-types`
- Verify: `assets/dashboard/src/lib/types.generated.ts` includes `source_session_id`

**Step 1: Regenerate types**

Run: `go run ./cmd/gen-types`

**Step 2: Verify**

Check that `types.generated.ts` has the new field. The frontend doesn't need to use it — it's backend-only context — but the types should be in sync.

**Step 3: Commit**

```
chore: regenerate TypeScript types for preview source_session_id
```

---

### Task 8: Final verification

**Step 1: Run full test suite**

Run: `./test.sh --quick`
Expected: PASS

**Step 2: Verify docs/api.md is updated**

The POST endpoint section should be gone. GET and DELETE should remain.

**Step 3: Final commit if needed**

Use `/commit` skill.
