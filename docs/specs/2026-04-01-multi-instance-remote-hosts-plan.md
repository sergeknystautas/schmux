# Plan: Multi-Instance Remote Hosts

**Goal**: Allow multiple OD instances per remote flavor so each remote workspace gets its own isolated host, removing the current 1:1 flavor-to-host constraint.
**Architecture**: Keep RemoteHost and Workspace as separate entities. Remove connection-reuse logic in remote.Manager. Add hostID-based spawn path alongside flavorID-based path. Update API responses to support N hosts per flavor.
**Tech Stack**: Go (backend), TypeScript/React (frontend), Vitest (frontend tests), Go testing (backend tests)

### Changes from v1

Addresses all 8 critical issues from plan review:

- Fix 1: Step 2a test uses correct `StartConnect` signature `(string, error)`, gets hostID via state
- Fix 2: Step 1a test uses `&State{}` struct literal (not `NewState()`)
- Fix 3: Step 2a test uses `cfg.RemoteFlavors = ...` (not `SetRemoteFlavors`)
- Fix 4: Step 1 includes `StateStore` interface update + mock updates
- Fix 5: Step 7 explicitly addresses the fallback code path (lines 488-515)
- Fix 6: Added Step 8 for `docs/api.md` update
- Fix 7: `TestManager_ConnectRace` update moved into Step 2 (not deferred to Step 13)
- Fix 8: Step 9 uses `Disconnect(hostID)` (not nonexistent `RemoveConnection`)
- Suggestion E: Step 2 removes `connectMu` entirely for fully concurrent provisioning
- Suggestion D: Step 12 spells out dismiss button implementation

---

## Step 1: Add `GetRemoteHostsByFlavorID` to state + update interface

**Files**: `internal/state/state.go`, `internal/state/interfaces.go`, `internal/state/state_test.go`

### 1a. Write failing test

**File**: `internal/state/state_test.go`

```go
func TestGetRemoteHostsByFlavorID(t *testing.T) {
	s := &State{
		RemoteHosts: []RemoteHost{
			{ID: "remote-aaa", FlavorID: "www", Hostname: "devvm1.od"},
			{ID: "remote-bbb", FlavorID: "www", Hostname: "devvm2.od"},
			{ID: "remote-ccc", FlavorID: "gpu", Hostname: "devvm3.od"},
		},
	}

	hosts := s.GetRemoteHostsByFlavorID("www")
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}

	hosts = s.GetRemoteHostsByFlavorID("gpu")
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}

	hosts = s.GetRemoteHostsByFlavorID("nonexistent")
	if len(hosts) != 0 {
		t.Fatalf("expected 0 hosts, got %d", len(hosts))
	}
}
```

### 1b. Run test to verify it fails

```bash
go test ./internal/state/ -run TestGetRemoteHostsByFlavorID -count=1
```

### 1c. Write implementation

**File**: `internal/state/state.go` (after `GetRemoteHostByFlavorID` at line ~971)

```go
// GetRemoteHostsByFlavorID returns all remote hosts matching the given flavor ID.
func (s *State) GetRemoteHostsByFlavorID(flavorID string) []RemoteHost {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var hosts []RemoteHost
	for _, rh := range s.RemoteHosts {
		if rh.FlavorID == flavorID {
			hosts = append(hosts, rh)
		}
	}
	return hosts
}
```

### 1d. Update StateStore interface

**File**: `internal/state/interfaces.go` (line 58, after `GetRemoteHostByFlavorID`)

Add:

```go
GetRemoteHostsByFlavorID(flavorID string) []RemoteHost
```

### 1e. Update mock implementations

Search for all `mockStateStore` implementations in the codebase (`internal/workspace/manager_test.go`, `internal/workspace/worktree_test.go`) and add the new method:

```go
func (m *mockStateStore) GetRemoteHostsByFlavorID(flavorID string) []state.RemoteHost {
	return nil
}
```

### 1f. Run tests to verify it passes

```bash
go test ./internal/state/ -run TestGetRemoteHostsByFlavorID -count=1
go test ./internal/workspace/ -count=1
```

### 1g. Commit

```bash
git commit -m "feat(state): add GetRemoteHostsByFlavorID for multi-instance support"
```

---

## Step 2: Remove 1:1 enforcement in Manager + update broken tests

**Files**: `internal/remote/manager.go`, `internal/remote/manager_test.go`

The core behavioral change. `connectInternal()` (lines 211-376) has three stages that enforce 1:1:

- Stage 1 (lines 219-237): Read-lock scan — returns existing connection if same flavorID
- Stage 2 (lines 240-262): Write-lock double-check — re-checks under `connectMu`
- Stage 3 (lines 266-308): Cached reconnection — tries to reconnect to a previously-known host

`StartConnect()` (lines 55-192) has parallel enforcement:

- Lines 62-73: Read-lock scan returning existing provisioning session by flavorID
- Lines 76-91: `connectMu` re-check by flavorID
- Lines 101-107: Cleanup of old disconnected/expired connections for same flavorID

All must be removed. The `connectMu` mutex is also removed entirely — per the design, provisioning is fully concurrent.

### 2a. Write failing test

**File**: `internal/remote/manager_test.go`

```go
func TestManager_ConnectMultipleHostsSameFlavor(t *testing.T) {
	cfg := &config.Config{}
	cfg.RemoteFlavors = []config.RemoteFlavor{{
		ID:             "www",
		Flavor:         "www",
		DisplayName:    "WWW",
		WorkspacePath:  "~/fbsource",
		ConnectCommand: "echo connected",
	}}
	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}
	mgr := NewManager(cfg, st, nil)

	// Two StartConnect calls should produce two different provisioning sessions
	provID1, err := mgr.StartConnect("www")
	if err != nil {
		t.Fatalf("first StartConnect failed: %v", err)
	}

	provID2, err := mgr.StartConnect("www")
	if err != nil {
		t.Fatalf("second StartConnect failed: %v", err)
	}

	if provID1 == provID2 {
		t.Fatalf("expected different provisioning session IDs, both got %s", provID1)
	}

	// Verify two distinct hosts were created in state
	hosts := st.GetRemoteHostsByFlavorID("www")
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts in state, got %d", len(hosts))
	}
	if hosts[0].ID == hosts[1].ID {
		t.Fatalf("expected different host IDs")
	}
}
```

### 2b. Run test to verify it fails

```bash
go test ./internal/remote/ -run TestManager_ConnectMultipleHostsSameFlavor -count=1
```

### 2c. Write implementation

**File**: `internal/remote/manager.go`

In `connectInternal()`:

- **Remove** Stage 1 (lines 219-237): the read-lock scan that returns existing connection by flavorID
- **Remove** Stage 2 (lines 240-262): the `connectMu` double-check by flavorID
- **Remove** the `connectMu` field from the Manager struct and its acquisition — provisioning is fully concurrent
- **Remove** Stage 3 (lines 266-308): the cached reconnection by flavor. Reconnection is now explicit per-host via `Reconnect()` only.
- **Keep** the `NewConnection` + `Connect` call at the bottom

In `StartConnect()`:

- **Remove** lines 62-73: the read-lock scan that returns existing provisioning session by flavorID
- **Remove** lines 76-91: the `connectMu` re-check by flavorID
- **Remove** lines 101-107: the cleanup of old disconnected/expired connections for same flavorID
- **Keep** the `NewConnection` creation and goroutine launch

### 2d. Update `TestManager_ConnectRace`

This test currently asserts 10 concurrent goroutines all get the same connection (1:1 enforcement). Update to verify thread safety without assuming 1:1:

- 10 concurrent goroutines calling `StartConnect("test-flavor")` should all succeed
- Each should get a unique provisioning session ID
- No panics, no data races (run with `-race`)

### 2e. Run all tests

```bash
go test ./internal/remote/ -race -count=1
```

### 2f. Commit

```bash
git commit -m "feat(remote): remove 1:1 flavor-to-host constraint in Manager.Connect"
```

---

## Step 3: Add `GetConnectionsByFlavorID` and update `GetFlavorStatuses`

**File**: `internal/remote/manager.go`

### 3a. Write failing test

**File**: `internal/remote/manager_test.go`

```go
func TestManager_GetConnectionsByFlavorID(t *testing.T) {
	cfg := &config.Config{}
	cfg.RemoteFlavors = []config.RemoteFlavor{
		{ID: "www", Flavor: "www", DisplayName: "WWW", WorkspacePath: "~/fbsource", ConnectCommand: "echo connected"},
		{ID: "gpu", Flavor: "gpu", DisplayName: "GPU", WorkspacePath: "~/fbsource", ConnectCommand: "echo connected"},
	}
	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}
	mgr := NewManager(cfg, st, nil)

	// Create two "www" connections and one "gpu"
	mgr.StartConnect("www")
	mgr.StartConnect("www")
	mgr.StartConnect("gpu")

	wwwConns := mgr.GetConnectionsByFlavorID("www")
	if len(wwwConns) != 2 {
		t.Fatalf("expected 2 www connections, got %d", len(wwwConns))
	}

	gpuConns := mgr.GetConnectionsByFlavorID("gpu")
	if len(gpuConns) != 1 {
		t.Fatalf("expected 1 gpu connection, got %d", len(gpuConns))
	}

	noneConns := mgr.GetConnectionsByFlavorID("nonexistent")
	if len(noneConns) != 0 {
		t.Fatalf("expected 0 connections, got %d", len(noneConns))
	}
}

func TestManager_GetFlavorStatuses_MultiHost(t *testing.T) {
	cfg := &config.Config{}
	cfg.RemoteFlavors = []config.RemoteFlavor{
		{ID: "www", Flavor: "www", DisplayName: "WWW", WorkspacePath: "~/fbsource", ConnectCommand: "echo connected"},
	}
	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}
	mgr := NewManager(cfg, st, nil)

	mgr.StartConnect("www")
	mgr.StartConnect("www")

	statuses := mgr.GetFlavorStatuses()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 flavor status, got %d", len(statuses))
	}
	if len(statuses[0].Hosts) != 2 {
		t.Fatalf("expected 2 hosts in flavor status, got %d", len(statuses[0].Hosts))
	}
}
```

### 3b. Run test to verify it fails

```bash
go test ./internal/remote/ -run "TestManager_GetConnectionsByFlavorID|TestManager_GetFlavorStatuses_MultiHost" -count=1
```

### 3c. Write implementation

**File**: `internal/remote/manager.go`

Replace `GetConnectionByFlavorID` (lines 481-492) with:

```go
// GetConnectionsByFlavorID returns all connections for a flavor (may be empty).
func (m *Manager) GetConnectionsByFlavorID(flavorID string) []*Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var conns []*Connection
	for _, conn := range m.connections {
		if conn.flavor.ID == flavorID {
			conns = append(conns, conn)
		}
	}
	return conns
}
```

Update `FlavorStatus` struct (lines 846-852) to hold multiple hosts:

```go
type HostStatus struct {
	HostID   string `json:"host_id"`
	Hostname string `json:"hostname"`
	Status   string `json:"status"`
}

type FlavorStatus struct {
	Flavor config.RemoteFlavor `json:"flavor"`
	Hosts  []HostStatus        `json:"hosts"`
}
```

Update `GetFlavorStatuses()` (lines 855-883): iterate all connections per flavor, appending to `Hosts` slice instead of breaking on first match.

Also update `IsFlavorConnected()` (lines 503-512) — keep it returning `bool` (true if any host of that flavor is connected), but update the implementation comment to clarify multi-host semantics.

### 3d. Run tests

```bash
go test ./internal/remote/ -count=1
```

### 3e. Commit

```bash
git commit -m "feat(remote): add GetConnectionsByFlavorID, multi-host FlavorStatus"
```

---

## Step 4: Update `SpawnRemote` to accept hostID for existing hosts

**File**: `internal/session/manager.go`

The current `SpawnRemote(ctx, flavorID, ...)` always calls `m.remoteManager.Connect(ctx, flavorID)`. For "add session to existing workspace", we need to use the existing host's connection directly.

### 4a. Write failing test

**File**: `internal/session/manager_test.go`

```go
func TestSpawnRemote_UsesExistingHostWhenHostIDProvided(t *testing.T) {
	// Setup: create a Manager with a mock remoteManager that has a connection
	// Call SpawnRemote with hostID set
	// Verify it calls GetConnection(hostID), NOT Connect(flavorID)
	// Verify it does NOT create a new host
}
```

### 4b. Run test to verify it fails

```bash
go test ./internal/session/ -run TestSpawnRemote_UsesExistingHostWhenHostIDProvided -count=1
```

### 4c. Write implementation

**File**: `internal/session/manager.go`

Add `hostID` parameter to `SpawnRemote`:

```go
func (m *Manager) SpawnRemote(ctx context.Context, flavorID, hostID, targetName, prompt, nickname string) (*state.Session, error) {
	var conn *remote.Connection
	var err error

	if hostID != "" {
		// Spawn on existing host (add session to existing workspace)
		conn = m.remoteManager.GetConnection(hostID)
		if conn == nil {
			return nil, fmt.Errorf("remote host %s not found or not connected", hostID)
		}
	} else {
		// Create new host (new workspace)
		conn, err = m.remoteManager.Connect(ctx, flavorID)
		if err != nil {
			return nil, fmt.Errorf("remote connect: %w", err)
		}
	}
	// ... rest of SpawnRemote unchanged
}
```

### 4d. Update all callers of SpawnRemote

Callers:

1. `internal/dashboard/handlers_spawn.go` line 330-332 — updated in Step 5
2. E2E tests call `SpawnRemoteSession` which hits the HTTP API, not `SpawnRemote` directly — no change needed

### 4e. Run tests

```bash
go test ./internal/session/ -count=1
```

### 4f. Commit

```bash
git commit -m "feat(session): SpawnRemote accepts hostID for existing host reuse"
```

---

## Step 5: Update `handleSpawnPost` to pass hostID

**File**: `internal/dashboard/handlers_spawn.go`

### 5a. Write failing test

Test that spawning a session on a workspace with `RemoteHostID` passes that hostID to `SpawnRemote` instead of only converting to flavorID.

### 5b. Write implementation

**File**: `internal/dashboard/handlers_spawn.go`

Change the auto-detection block (lines 87-93). Currently:

```go
if req.RemoteFlavorID == "" && ws != nil && ws.RemoteHostID != "" {
    host, ok := s.state.GetRemoteHost(ws.RemoteHostID)
    if ok {
        req.RemoteFlavorID = host.FlavorID
    }
}
```

Change to pass the hostID through:

```go
var remoteHostID string
if ws != nil && ws.RemoteHostID != "" {
    remoteHostID = ws.RemoteHostID
    if req.RemoteFlavorID == "" {
        host, ok := s.state.GetRemoteHost(ws.RemoteHostID)
        if ok {
            req.RemoteFlavorID = host.FlavorID
        }
    }
}
```

Then at the spawn call (line 330):

```go
sess, err = s.session.SpawnRemote(ctx, req.RemoteFlavorID, remoteHostID, targetName, req.Prompt, nickname)
```

### 5c. Run tests

```bash
go test ./internal/dashboard/ -count=1
```

### 5d. Commit

```bash
git commit -m "fix(spawn): pass hostID when spawning on existing remote workspace"
```

---

## Step 6: Update `handleRemoteHostConnect` to remove reuse guard

**File**: `internal/dashboard/handlers_remote.go`

### 6a. Write failing test

Test that calling `POST /api/remote/hosts/connect` with a flavor that already has a connected host returns 202 (starts new provisioning), not 200 (returns existing).

### 6b. Write implementation

**File**: `internal/dashboard/handlers_remote.go`

Remove the guard at lines 316-332 that checks `GetConnectionByFlavorID` and returns early with 200 if already connected. Always proceed to `StartConnect()`. The response is always 202 with a provisioning session ID.

Also remove the now-dead `GetConnectionByFlavorID` method from `internal/remote/manager.go` (replaced by `GetConnectionsByFlavorID` in Step 3). Verify no other callers remain.

### 6c. Run tests

```bash
go test ./internal/dashboard/ -count=1
go test ./internal/remote/ -count=1
```

### 6d. Commit

```bash
git commit -m "feat(api): remote connect always provisions new host"
```

---

## Step 7: Update `handleRemoteFlavorStatuses` response for 1:N

**File**: `internal/dashboard/handlers_remote.go`

### 7a. Write failing test

Test that `GET /api/remote/flavor-statuses` returns multiple hosts per flavor when they exist.

### 7b. Write implementation

**File**: `internal/dashboard/handlers_remote.go`

Update `RemoteFlavorStatusResponse` (lines 456-462):

```go
type RemoteFlavorStatusResponse struct {
    Flavor RemoteFlavorResponse `json:"flavor"`
    Hosts  []RemoteHostStatus   `json:"hosts"`
}

type RemoteHostStatus struct {
    HostID    string `json:"host_id"`
    Hostname  string `json:"hostname"`
    Status    string `json:"status"`
    Connected bool   `json:"connected"`
}
```

Update the primary code path (lines 468-485) to map `FlavorStatus.Hosts` → `RemoteFlavorStatusResponse.Hosts`.

**Also update the fallback code path** (lines 488-515). Currently uses `map[string]state.RemoteHost` keyed by flavorID which silently drops duplicate hosts. Change to:

```go
// Fallback: use state-based connection status
hosts := s.state.GetRemoteHosts()

// Build a map of flavor ID -> all hosts
flavorToHosts := make(map[string][]state.RemoteHost)
for _, h := range hosts {
    flavorToHosts[h.FlavorID] = append(flavorToHosts[h.FlavorID], h)
}

response := make([]RemoteFlavorStatusResponse, len(flavors))
for i, f := range flavors {
    resp := RemoteFlavorStatusResponse{
        Flavor: toFlavorResponse(f),
    }
    for _, host := range flavorToHosts[f.ID] {
        resp.Hosts = append(resp.Hosts, RemoteHostStatus{
            HostID:    host.ID,
            Hostname:  host.Hostname,
            Status:    host.Status,
            Connected: host.Status == state.RemoteHostStatusConnected,
        })
    }
    response[i] = resp
}
```

### 7c. Run tests

```bash
go test ./internal/dashboard/ -count=1
```

### 7d. Commit

```bash
git commit -m "feat(api): flavor-statuses returns multiple hosts per flavor"
```

---

## Step 8: Update `docs/api.md`

**File**: `docs/api.md`

CI enforces that changes to `internal/dashboard/` include corresponding `docs/api.md` updates via `scripts/check-api-docs.sh`.

### 8a. Write implementation

Update the following sections in `docs/api.md`:

- `GET /api/remote/flavor-statuses` — document the new response shape with `hosts` array replacing singular `host_id`/`hostname`/`status`/`connected` fields
- `POST /api/remote/hosts/connect` — document that this always returns 202 (never 200 for existing), even if a host of the same flavor is already connected
- `DELETE /api/remote/hosts/{hostID}` — document the `?dismiss=true` query parameter that removes the host, its workspaces, and sessions from state

### 8b. Verify CI check passes

```bash
bash scripts/check-api-docs.sh
```

### 8c. Commit

```bash
git commit -m "docs(api): update remote host endpoints for multi-instance support"
```

---

## Step 9: Add dismiss action for expired/disconnected workspaces

**File**: `internal/dashboard/handlers_remote.go`

### 9a. Write failing test

Test that `DELETE /api/remote/hosts/{hostID}?dismiss=true` removes the RemoteHost from state AND removes associated sessions and workspaces.

### 9b. Write implementation

Update `handleRemoteHostDisconnect` to check for `?dismiss=true` query parameter. When set:

1. Get sessions: `s.state.GetSessionsByRemoteHostID(hostID)` → remove each via `s.state.RemoveSession`
2. Get workspaces: `s.state.GetWorkspacesByRemoteHostID(hostID)` → remove each via `s.state.RemoveWorkspace`
3. Remove the RemoteHost: `s.state.RemoveRemoteHost(hostID)`
4. Disconnect the connection: `s.remoteManager.Disconnect(hostID)` (this method exists at `manager.go:515`)
5. Save state: `s.state.Save()`

When `dismiss=false` (default, existing behavior): only disconnect, don't remove from state.

### 9c. Run tests

```bash
go test ./internal/dashboard/ -count=1
```

### 9d. Commit

```bash
git commit -m "feat(api): add dismiss action for remote hosts"
```

---

## Step 10: Update TypeScript types for 1:N

**File**: `assets/dashboard/src/lib/types.ts`

### 10a. Write implementation

Update `RemoteFlavorStatus` (lines 405-411):

```typescript
export interface RemoteHostStatus {
  host_id: string;
  hostname: string;
  status: 'provisioning' | 'connecting' | 'connected' | 'disconnected' | 'expired' | 'reconnecting';
  connected: boolean;
}

export interface RemoteFlavorStatus {
  flavor: RemoteFlavor;
  hosts: RemoteHostStatus[];
}
```

Remove the old singular `connected`, `status`, `hostname`, `host_id` fields from `RemoteFlavorStatus`.

### 10b. Fix TypeScript compilation errors

The type change will cause errors in any component that reads `flavorStatus.connected` or `flavorStatus.host_id`. Fix all consumers to use `flavorStatus.hosts[...]` instead. Primary consumers:

- `RemoteHostSelector.tsx` (updated in Step 11)
- Any other components referencing `RemoteFlavorStatus`

### 10c. Run tests

```bash
./test.sh --quick
```

### 10d. Commit

```bash
git commit -m "feat(types): update RemoteFlavorStatus for multi-host support"
```

---

## Step 11: Update RemoteHostSelector for multi-host display

**File**: `assets/dashboard/src/components/RemoteHostSelector.tsx`

### 11a. Write failing test

**File**: `assets/dashboard/src/components/RemoteHostSelector.test.tsx` (new file)

```typescript
import { render, screen } from '@testing-library/react';
import { RemoteHostSelector } from './RemoteHostSelector';

it('shows existing hosts plus new-host option for a flavor with connections', () => {
  // Mock getRemoteFlavorStatuses to return a flavor with 2 hosts
  // Render RemoteHostSelector
  // Expect: 2 host cards showing hostnames + 1 "New www host" card
  // Clicking existing connected host card selects that host (verify onSelect called with hostID)
  // Clicking "New" card triggers connect flow
});

it('shows single new-host card for a flavor with no connections', () => {
  // Mock getRemoteFlavorStatuses to return a flavor with empty hosts array
  // Render RemoteHostSelector
  // Expect: 1 card for the flavor (same as current behavior)
});
```

### 11b. Run test to verify it fails

```bash
./test.sh --quick
```

### 11c. Write implementation

Update `RemoteHostSelector.tsx`:

- When `flavorStatus.hosts` is non-empty, render a card per host showing hostname + status indicator
- Add a "+ New [flavor] host" card at the end of the host list
- Clicking an existing connected host → call `onSelect` with `{ type: 'remote', flavorId, hostId, ... }`
- Clicking an existing disconnected host → trigger reconnect for that specific hostID
- Clicking "+ New" → trigger connect flow via `POST /api/remote/hosts/connect` (same as today)
- When `flavorStatus.hosts` is empty, show a single flavor card (backward compatible with current UX)

The `onSelect` callback type gains an optional `hostId` field to distinguish "existing host" from "new host".

### 11d. Run tests

```bash
./test.sh --quick
```

### 11e. Commit

```bash
git commit -m "feat(ui): RemoteHostSelector shows per-host cards with new-host option"
```

---

## Step 12: Add dismiss button to home page workspace cards

**File**: `assets/dashboard/src/routes/HomePage.tsx` (or the workspace card component)

### 12a. Write implementation

Remote workspaces already appear as cards via `WorkspaceResponse`. Add:

- "Dismiss" button on workspace cards where `remote_host_status` is `"expired"` or `"disconnected"`
- Clicking "Dismiss" calls `DELETE /api/remote/hosts/{hostID}?dismiss=true`
- On success, workspace card disappears (removed from state, WebSocket broadcast updates UI)
- Confirmation dialog before dismiss: "This will remove the workspace and all its sessions. The remote host's filesystem is already gone."

### 12b. Run tests

```bash
./test.sh --quick
```

### 12c. Commit

```bash
git commit -m "feat(ui): add dismiss button for expired/disconnected remote workspaces"
```

---

## Step 13: Add hostname extraction fallback

**File**: `internal/remote/connection.go`

### 13a. Write failing test

**File**: `internal/remote/connection_test.go`

```go
func TestConnection_HostnameExtractionFallback(t *testing.T) {
	// Setup: create a connection with a hostnameRegex that won't match
	// Simulate provisioning completing without hostname extraction
	// After control mode is ready, verify the connection attempts
	// to get hostname via the tmux control channel
}
```

### 13b. Write implementation

After control mode is ready (in `Connect()` or after the ready handshake):

- If `conn.host.Hostname` is still empty
- Execute `display-message -p '#{host}'` via the tmux control channel
- Parse the response and set `conn.host.Hostname`
- If that also fails, the workspace shows the UUID as display name (graceful degradation)

Add a hard timeout on the `provisioning` state:

- If provisioning exceeds 5 minutes, transition to `failed` status
- Clean up the connection and notify via status change callback

### 13c. Run tests

```bash
go test ./internal/remote/ -count=1
```

### 13d. Commit

```bash
git commit -m "fix(remote): hostname extraction fallback via control channel"
```

---

## Step 14: End-to-end verification

### 14a. Run full test suite

```bash
./test.sh
```

### 14b. Manual verification checklist

- [ ] Start daemon, configure a remote flavor
- [ ] "+ Add workspace" with remote flavor provisions new OD
- [ ] "+ Add workspace" again with same flavor provisions SECOND OD (not reuse)
- [ ] Both appear as separate workspace cards on home page with distinct hostnames
- [ ] Adding a session to workspace A goes to host A, not host B
- [ ] Restarting daemon marks both as disconnected
- [ ] Reconnecting workspace A doesn't affect workspace B
- [ ] Dismissing expired workspace removes it and its sessions from state
- [ ] Flavor status API returns both hosts under the same flavor

---

## Task Dependencies

| Group | Steps                                     | Can Parallelize                 | Files Touched                                                                                                             |
| ----- | ----------------------------------------- | ------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| 1     | Step 1 (state accessor + interface)       | Independent                     | `internal/state/state.go`, `internal/state/interfaces.go`, `internal/state/state_test.go`, `internal/workspace/*_test.go` |
| 2     | Steps 2-3 (manager changes)               | Sequential (3 depends on 2)     | `internal/remote/manager.go`, `internal/remote/manager_test.go`                                                           |
| 3     | Steps 4-5 (session/spawn handler)         | Sequential (5 depends on 4)     | `internal/session/manager.go`, `internal/dashboard/handlers_spawn.go`                                                     |
| 4     | Steps 6-9 (API handlers + dismiss + docs) | 6,7 parallel; 8,9 after 7       | `internal/dashboard/handlers_remote.go`, `docs/api.md`                                                                    |
| 5     | Steps 10-12 (frontend)                    | Sequential (11,12 depend on 10) | `assets/dashboard/src/lib/types.ts`, `assets/dashboard/src/components/RemoteHostSelector.tsx`, home page component        |
| 6     | Step 13 (hostname fallback)               | Independent of Groups 3-5       | `internal/remote/connection.go`, `internal/remote/connection_test.go`                                                     |
| 7     | Step 14 (E2E)                             | After all above                 | —                                                                                                                         |

Groups 1 and 6 can run in parallel with everything else.
Groups 2 and 3 are sequential (session manager depends on remote manager changes).
Group 4 depends on Group 2 (uses new `GetConnectionsByFlavorID`).
Group 5 depends on Group 4 (frontend consumes updated API responses).
Group 7 is final.
