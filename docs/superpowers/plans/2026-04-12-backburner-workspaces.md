# Backburner Workspaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a per-workspace "backburner" toggle that dims and sorts backburnered workspaces to the bottom of all workspace lists, gated behind an experimental feature flag.

**Architecture:** New `Backburner` boolean on the workspace state struct, mutated via a dedicated `POST /api/workspaces/{id}/backburner` endpoint. Frontend reads the flag from workspace response data and applies opacity + sort partitioning in the sidebar and home page. The entire feature is gated behind a `backburner_enabled` config toggle in the Experimental tab.

**Tech Stack:** Go (chi router, state persistence), React (TypeScript, CSS modules, Vitest)

**Spec:** `docs/superpowers/specs/2026-04-12-backburner-workspaces-design.md`

---

## File Map

### Go (backend)

- **Modify:** `internal/state/state.go:123-148` — Add `Backburner` field to `Workspace` struct
- **Modify:** `internal/config/config.go:109-110` — Add `BackburnerEnabled` field to `Config` struct
- **Modify:** `internal/api/contracts/config.go:185-186` — Add `BackburnerEnabled` to `ConfigResponse`
- **Modify:** `internal/api/contracts/config.go:331-332` — Add `BackburnerEnabled` to `ConfigUpdateRequest`
- **Modify:** `internal/dashboard/handlers_sessions.go:62-93` — Add `Backburner` to `WorkspaceResponseItem`
- **Modify:** `internal/dashboard/handlers_sessions.go:178-208` — Map `Backburner` in `buildSessionsResponse`
- **Modify:** `internal/dashboard/server.go:782-786` — Register backburner route
- **Create:** `internal/dashboard/handlers_backburner.go` — Backburner toggle handler
- **Modify:** `internal/dashboard/handlers_config.go:778-779` — Process `BackburnerEnabled` in config update
- **Create:** `internal/dashboard/handlers_backburner_test.go` — Handler tests

### TypeScript (frontend)

- **Modify:** `assets/dashboard/src/lib/types.generated.ts` — Regenerated via `go run ./cmd/gen-types`
- **Modify:** `assets/dashboard/src/lib/types.ts:30-60` — Add `backburner` to `WorkspaceResponse`
- **Modify:** `assets/dashboard/src/lib/api.ts` — Add `setBackburner` function
- **Modify:** `assets/dashboard/src/routes/config/experimentalRegistry.ts` — Add backburner entry
- **Modify:** `assets/dashboard/src/routes/config/useConfigForm.ts:174-175` — Add `backburnerEnabled` to form state
- **Modify:** `assets/dashboard/src/routes/config/buildConfigUpdate.ts:132-133` — Include `backburner_enabled`
- **Modify:** `assets/dashboard/src/routes/ConfigPage.tsx:204-205` — Map config response to form state
- **Modify:** `assets/dashboard/src/components/WorkspaceHeader.tsx:242-268` — Add backburner button
- **Modify:** `assets/dashboard/src/components/AppShell.tsx:123-166` — Partition sort + opacity
- **Modify:** `assets/dashboard/src/routes/HomePage.tsx:631-684` — Sort + opacity

---

### Task 1: Go Data Model — State, Config, Contracts, Response

**Files:**

- Modify: `internal/state/state.go:147`
- Modify: `internal/config/config.go:110`
- Modify: `internal/api/contracts/config.go:186,332`
- Modify: `internal/dashboard/handlers_sessions.go:92,207`

- [ ] **Step 1: Add `Backburner` to `Workspace` state struct**

In `internal/state/state.go`, add after line 147 (`ResolveConflicts`):

```go
Backburner        bool              `json:"backburner,omitempty"`
```

- [ ] **Step 2: Add `BackburnerEnabled` to `Config` struct**

In `internal/config/config.go`, add after `CommStylesEnabled` (line 110):

```go
BackburnerEnabled          bool                        `json:"backburner_enabled,omitempty"`
```

- [ ] **Step 3: Add `BackburnerEnabled` to `ConfigResponse` contract**

In `internal/api/contracts/config.go`, add after `CommStylesEnabled` (line 186):

```go
BackburnerEnabled          bool                   `json:"backburner_enabled,omitempty"`
```

- [ ] **Step 4: Add `BackburnerEnabled` to `ConfigUpdateRequest` contract**

In `internal/api/contracts/config.go`, add after `CommStylesEnabled` (line 332):

```go
BackburnerEnabled          *bool                       `json:"backburner_enabled,omitempty"`
```

- [ ] **Step 5: Add `Backburner` to `WorkspaceResponseItem`**

In `internal/dashboard/handlers_sessions.go`, add after `Status` (line 92):

```go
Backburner              bool                    `json:"backburner,omitempty"`
```

- [ ] **Step 6: Map `Backburner` in `buildSessionsResponse`**

In `internal/dashboard/handlers_sessions.go`, add after `Status: ws.Status,` (line 207):

```go
Backburner:              ws.Backburner,
```

- [ ] **Step 7: Verify it compiles**

Run: `go build ./...`
Expected: Clean build, no errors.

- [ ] **Step 8: Commit**

```
feat: add backburner fields to workspace state, config, and API response
```

---

### Task 2: Go Backburner API Endpoint

**Files:**

- Create: `internal/dashboard/handlers_backburner.go`
- Modify: `internal/dashboard/server.go:785`
- Create: `internal/dashboard/handlers_backburner_test.go`

- [ ] **Step 1: Write the test file**

Create `internal/dashboard/handlers_backburner_test.go`:

```go
package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

func TestHandleBackburnerWorkspace(t *testing.T) {
	t.Run("sets backburner true", func(t *testing.T) {
		server, cfg, st := newTestServer(t)
		cfg.BackburnerEnabled = true
		ws := addWorkspaceToServer(t, st, "ws-bb-1")

		body, _ := json.Marshal(map[string]bool{"backburner": true})
		req := makeWorkspaceRequest(t, http.MethodPost,
			"/api/workspaces/"+ws.ID+"/backburner", ws.ID, body)
		rr := httptest.NewRecorder()
		server.handleBackburnerWorkspace(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		updated, ok := st.GetWorkspace(ws.ID)
		if !ok {
			t.Fatal("workspace not found after update")
		}
		if !updated.Backburner {
			t.Error("expected Backburner to be true")
		}
	})

	t.Run("sets backburner false", func(t *testing.T) {
		server, cfg, st := newTestServer(t)
		cfg.BackburnerEnabled = true
		ws := addWorkspaceToServer(t, st, "ws-bb-2")

		// First set to true
		wsState, _ := st.GetWorkspace(ws.ID)
		wsState.Backburner = true
		st.UpdateWorkspace(wsState)

		body, _ := json.Marshal(map[string]bool{"backburner": false})
		req := makeWorkspaceRequest(t, http.MethodPost,
			"/api/workspaces/"+ws.ID+"/backburner", ws.ID, body)
		rr := httptest.NewRecorder()
		server.handleBackburnerWorkspace(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		updated, _ := st.GetWorkspace(ws.ID)
		if updated.Backburner {
			t.Error("expected Backburner to be false")
		}
	})

	t.Run("returns 404 when feature disabled", func(t *testing.T) {
		server, cfg, st := newTestServer(t)
		cfg.BackburnerEnabled = false
		ws := addWorkspaceToServer(t, st, "ws-bb-3")

		body, _ := json.Marshal(map[string]bool{"backburner": true})
		req := makeWorkspaceRequest(t, http.MethodPost,
			"/api/workspaces/"+ws.ID+"/backburner", ws.ID, body)
		rr := httptest.NewRecorder()
		server.handleBackburnerWorkspace(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("returns 404 for unknown workspace", func(t *testing.T) {
		server, cfg, _ := newTestServer(t)
		cfg.BackburnerEnabled = true

		body, _ := json.Marshal(map[string]bool{"backburner": true})
		req := makeWorkspaceRequest(t, http.MethodPost,
			"/api/workspaces/ws-nonexistent/backburner", "ws-nonexistent", body)
		rr := httptest.NewRecorder()
		server.handleBackburnerWorkspace(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dashboard/ -run TestHandleBackburnerWorkspace -v`
Expected: FAIL — `handleBackburnerWorkspace` not defined.

- [ ] **Step 3: Write the handler**

Create `internal/dashboard/handlers_backburner.go`:

```go
package dashboard

import (
	"encoding/json"
	"net/http"
)

// handleBackburnerWorkspace toggles the backburner state of a workspace.
func (s *Server) handleBackburnerWorkspace(w http.ResponseWriter, r *http.Request) {
	if !s.config.BackburnerEnabled {
		writeJSONError(w, "backburner feature is not enabled", http.StatusNotFound)
		return
	}

	ws, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		Backburner bool `json:"backburner"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	ws.Backburner = req.Backburner
	if err := s.state.UpdateWorkspace(ws); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

- [ ] **Step 4: Register the route**

In `internal/dashboard/server.go`, inside the workspace route group (after line 785, before the closing `})`), add:

```go
// Backburner route
r.Post("/backburner", s.handleBackburnerWorkspace)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/dashboard/ -run TestHandleBackburnerWorkspace -v`
Expected: PASS — all 4 subtests pass.

- [ ] **Step 6: Commit**

```
feat: add POST /api/workspaces/{id}/backburner endpoint
```

---

### Task 3: Go Config Update Handler

**Files:**

- Modify: `internal/dashboard/handlers_config.go:778-779`

- [ ] **Step 1: Add `BackburnerEnabled` processing to `handleConfigUpdate`**

In `internal/dashboard/handlers_config.go`, add after the `CommStylesEnabled` block (after line 778):

```go
if req.BackburnerEnabled != nil {
	cfg.BackburnerEnabled = *req.BackburnerEnabled
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Clean build.

- [ ] **Step 3: Commit**

```
feat: process backburner_enabled in config update handler
```

---

### Task 4: TypeScript Type Generation + Manual Types

**Files:**

- Regenerate: `assets/dashboard/src/lib/types.generated.ts`
- Modify: `assets/dashboard/src/lib/types.ts`

- [ ] **Step 1: Regenerate TypeScript types from Go contracts**

Run: `go run ./cmd/gen-types`
Expected: `types.generated.ts` updated with `backburner_enabled` on both `ConfigResponse` and `ConfigUpdateRequest`.

- [ ] **Step 2: Verify generated types include new fields**

Check that `assets/dashboard/src/lib/types.generated.ts` now contains:

- `backburner_enabled?: boolean` in `ConfigResponse`
- `backburner_enabled?: boolean` in `ConfigUpdateRequest`

- [ ] **Step 3: Add `backburner` to `WorkspaceResponse` manual type**

In `assets/dashboard/src/lib/types.ts`, add after the `status` field (around line 59):

```typescript
backburner?: boolean;
```

- [ ] **Step 4: Commit**

```
feat: add backburner types to generated and manual TypeScript types
```

---

### Task 5: Frontend Experimental Toggle

**Files:**

- Modify: `assets/dashboard/src/routes/config/experimentalRegistry.ts`
- Modify: `assets/dashboard/src/routes/config/useConfigForm.ts:174-175`
- Modify: `assets/dashboard/src/routes/config/buildConfigUpdate.ts:132-133`
- Modify: `assets/dashboard/src/routes/ConfigPage.tsx:204-205`

- [ ] **Step 1: Add backburner to experimental registry**

In `assets/dashboard/src/routes/config/experimentalRegistry.ts`, add a new entry to the `EXPERIMENTAL_FEATURES` array:

```typescript
{
  id: 'backburner',
  name: 'Backburner',
  description: 'Dim and sort workspaces you want to set aside',
  enabledKey: 'backburnerEnabled',
  configPanel: null,
},
```

- [ ] **Step 2: Add `backburnerEnabled` to `ConfigFormState`**

In `assets/dashboard/src/routes/config/useConfigForm.ts`, add after `commStylesEnabled` (around line 175):

```typescript
backburnerEnabled: boolean;
```

Also add the default value in the initial state object (find where `personasEnabled: false` is set and add alongside):

```typescript
backburnerEnabled: false,
```

- [ ] **Step 3: Include `backburner_enabled` in `buildConfigUpdate`**

In `assets/dashboard/src/routes/config/buildConfigUpdate.ts`, add after `comm_styles_enabled: state.commStylesEnabled,` (line 133):

```typescript
backburner_enabled: state.backburnerEnabled,
```

- [ ] **Step 4: Map config response to form state**

In `assets/dashboard/src/routes/ConfigPage.tsx`, add after `commStylesEnabled: data.comm_styles_enabled ?? false,` (line 205):

```typescript
backburnerEnabled: data.backburner_enabled ?? false,
```

- [ ] **Step 5: Verify it compiles**

Run: `go run ./cmd/build-dashboard`
Expected: Clean build.

- [ ] **Step 6: Commit**

```
feat: add backburner experimental toggle to config UI
```

---

### Task 6: Frontend API Function

**Files:**

- Modify: `assets/dashboard/src/lib/api.ts`

- [ ] **Step 1: Add `setBackburner` function**

In `assets/dashboard/src/lib/api.ts`, add near the other workspace mutation functions (near `disposeWorkspace`):

```typescript
export async function setBackburner(
  workspaceId: string,
  backburner: boolean
): Promise<{ status: string }> {
  const response = await apiFetch(`/api/workspaces/${encodeURIComponent(workspaceId)}/backburner`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ backburner }),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to update backburner');
  }
  return response.json();
}
```

- [ ] **Step 2: Commit**

```
feat: add setBackburner API function
```

---

### Task 7: WorkspaceHeader Backburner Button

**Files:**

- Modify: `assets/dashboard/src/components/WorkspaceHeader.tsx`

- [ ] **Step 1: Write the test**

Find or create the appropriate test file for WorkspaceHeader. Add a test that:

- Renders WorkspaceHeader with a workspace that has `backburner: false` and config `backburner_enabled: true`
- Asserts the backburner button is present with aria-label "Backburner"
- Renders with `backburner: true`
- Asserts the button has aria-label "Wake up"
- Renders with config `backburner_enabled: false` (or missing)
- Asserts no backburner button is rendered

```typescript
describe('backburner button', () => {
  it('renders when feature enabled and workspace not backburnered', () => {
    // Set config.backburner_enabled = true, workspace.backburner = false
    renderPage();
    const btn = screen.getByLabelText('Backburner');
    expect(btn).toBeInTheDocument();
  });

  it('shows wake up label when workspace is backburnered', () => {
    // Set workspace.backburner = true
    renderPage();
    const btn = screen.getByLabelText('Wake up');
    expect(btn).toBeInTheDocument();
  });

  it('hidden when feature disabled', () => {
    // Set config.backburner_enabled = false
    renderPage();
    expect(screen.queryByLabelText('Backburner')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Wake up')).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./test.sh --quick`
Expected: FAIL — backburner button not found.

- [ ] **Step 3: Implement the backburner button**

In `assets/dashboard/src/components/WorkspaceHeader.tsx`:

Add imports at the top:

```typescript
import { setBackburner } from '../lib/api';
```

Read config using `useConfig`:

```typescript
const { config } = useConfig();
```

(If `useConfig` is already imported/used in this component, just reference `config` from the existing destructure.)

Add state for loading:

```typescript
const [togglingBackburner, setTogglingBackburner] = useState(false);
```

Add the toggle handler:

```typescript
const handleToggleBackburner = async () => {
  setTogglingBackburner(true);
  try {
    await setBackburner(workspace.id, !workspace.backburner);
  } catch {
    // Error will show via WebSocket state update failure
  } finally {
    setTogglingBackburner(false);
  }
};
```

In the `app-header__actions` div, insert before the VS Code `<Tooltip>` (before line 243):

```tsx
{
  config.backburner_enabled && (
    <Tooltip content={workspace.backburner ? 'Wake up' : 'Backburner'}>
      <button
        className="btn btn--sm btn--ghost btn--bordered"
        style={
          workspace.backburner
            ? {
                background: 'rgba(184,169,224,0.1)',
                borderColor: 'rgba(184,169,224,0.4)',
              }
            : undefined
        }
        disabled={togglingBackburner}
        onClick={handleToggleBackburner}
        aria-label={workspace.backburner ? 'Wake up' : 'Backburner'}
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke={workspace.backburner ? '#b8a9e0' : 'currentColor'}
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M2 10h6l-6 8h6" />
          <path d="M10 5h5l-5 7h5" />
          <path d="M16 2h5l-5 6h5" />
        </svg>
      </button>
    </Tooltip>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./test.sh --quick`
Expected: PASS.

- [ ] **Step 5: Commit**

```
feat: add backburner toggle button to workspace header
```

---

### Task 8: Sidebar Sorting + Dimming

**Files:**

- Modify: `assets/dashboard/src/components/AppShell.tsx:123-166`

- [ ] **Step 1: Write the test**

Add a test (in the AppShell test file or a new focused test) that verifies:

- When backburner feature is enabled, backburnered workspaces sort after non-backburnered
- Alphabetical sort within each group is preserved
- Backburnered workspace rows have `opacity: 0.38`

```typescript
describe('backburner sorting', () => {
  it('sorts backburnered workspaces to bottom in alpha mode', () => {
    // Provide 3 workspaces: alpha (not bb), charlie (bb), bravo (not bb)
    // Expected order: alpha, bravo, charlie
    renderWithWorkspaces([
      { id: 'ws-a', branch: 'alpha', backburner: false },
      { id: 'ws-c', branch: 'charlie', backburner: true },
      { id: 'ws-b', branch: 'bravo', backburner: false },
    ]);
    const items = screen.getAllByRole('button', { name: /nav-workspace/ });
    // Verify order: alpha, bravo, charlie
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./test.sh --quick`
Expected: FAIL — sorting does not account for backburner.

- [ ] **Step 3: Modify the sorting logic**

In `assets/dashboard/src/components/AppShell.tsx`, modify the `sortedWorkspaces` useMemo (around line 123). Read the config to check if backburner is enabled:

```typescript
const { config } = useConfig();
```

(If already imported, just reference `config`.)

Replace the sorting logic to wrap with backburner partitioning:

```typescript
const sortedWorkspaces = useMemo(() => {
  if (!workspaces) return workspaces;

  const sorted = [...workspaces];

  const compareAlpha = (a: WorkspaceResponse, b: WorkspaceResponse) => {
    const repoA = a.repo_name || getRepoName(a.repo);
    const repoB = b.repo_name || getRepoName(b.repo);
    if (repoA !== repoB) return repoA.localeCompare(repoB);
    return a.branch.localeCompare(b.branch);
  };

  const compareTime = (a: WorkspaceResponse, b: WorkspaceResponse) => {
    const getTime = (ws: WorkspaceResponse): number => {
      const times =
        ws.sessions
          ?.filter((s) => s.last_output_at)
          .map((s) => new Date(s.last_output_at!).getTime()) || [];
      return times.length > 0 ? Math.max(...times) : 0;
    };
    const timeA = getTime(a);
    const timeB = getTime(b);
    if (timeA === 0 && timeB === 0) return compareAlpha(a, b);
    if (timeA === 0) return 1;
    if (timeB === 0) return -1;
    if (timeA !== timeB) return timeB - timeA;
    return compareAlpha(a, b);
  };

  const compare = workspaceSort === 'alpha' ? compareAlpha : compareTime;

  sorted.sort((a, b) => {
    // Partition by backburner when feature is enabled
    if (config.backburner_enabled) {
      const aBB = a.backburner ? 1 : 0;
      const bBB = b.backburner ? 1 : 0;
      if (aBB !== bBB) return aBB - bBB;
    }
    return compare(a, b);
  });

  return sorted;
}, [workspaces, workspaceSort, getRepoName, config.backburner_enabled]);
```

- [ ] **Step 4: Add opacity to backburnered workspace rows**

In the workspace row rendering (around line 793), add an inline style for opacity on the `nav-workspace` div:

```tsx
<div
  key={workspace.id}
  ref={isWorkspaceActive ? activeWorkspaceRef : null}
  className={`nav-workspace${isWorkspaceActive ? ' nav-workspace--active' : ''}${isDevLive ? ' nav-workspace--dev-live' : ''}${workspace.status === 'disposing' ? ' nav-workspace--disposing' : ''}`}
  style={
    config.backburner_enabled && workspace.backburner
      ? { opacity: 0.38 }
      : undefined
  }
>
```

- [ ] **Step 5: Run test to verify it passes**

Run: `./test.sh --quick`
Expected: PASS.

- [ ] **Step 6: Commit**

```
feat: add backburner sorting and dimming to sidebar
```

---

### Task 9: Home Page Sorting + Dimming

**Files:**

- Modify: `assets/dashboard/src/routes/HomePage.tsx:631-684`

- [ ] **Step 1: Write the test**

Add a test in the HomePage test file that verifies:

- Backburnered workspaces render with opacity 0.38
- Backburnered workspaces sort after non-backburnered

```typescript
describe('backburner workspaces', () => {
  it('dims backburnered workspace rows', () => {
    // Mock config with backburner_enabled: true
    // Provide workspace with backburner: true
    renderPage();
    const row = screen.getByTestId('workspace-ws-bb');
    expect(row.style.opacity).toBe('0.38');
  });

  it('sorts backburnered to bottom', () => {
    // Provide ws-a (not bb), ws-b (bb), ws-c (not bb)
    renderPage();
    const rows = screen.getAllByTestId(/^workspace-/);
    // Verify order: ws-a, ws-c, ws-b
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./test.sh --quick`
Expected: FAIL.

- [ ] **Step 3: Add sorting to the home page workspace list**

In `assets/dashboard/src/routes/HomePage.tsx`, read config:

```typescript
const { config } = useConfig();
```

(If already imported, reference the existing destructure.)

Add a sorted workspaces memo before the JSX where workspaces are mapped (before line 631):

```typescript
const sortedHomeWorkspaces = useMemo(() => {
  if (!workspaces || !config.backburner_enabled) return workspaces;
  return [...workspaces].sort((a, b) => {
    const aBB = a.backburner ? 1 : 0;
    const bBB = b.backburner ? 1 : 0;
    if (aBB !== bBB) return aBB - bBB;
    return a.branch.localeCompare(b.branch);
  });
}, [workspaces, config.backburner_enabled]);
```

Replace `workspaces.map((ws) =>` with `sortedHomeWorkspaces.map((ws) =>` in the JSX.

- [ ] **Step 4: Add opacity to backburnered rows**

On the workspace row `<button>` element (around line 641), add inline style:

```tsx
<button
  key={ws.id}
  className={styles.workspaceRow}
  onClick={() => handleWorkspaceClick(ws.id)}
  type="button"
  data-testid={`workspace-${ws.id}`}
  style={
    config.backburner_enabled && ws.backburner
      ? { opacity: 0.38 }
      : undefined
  }
>
```

- [ ] **Step 5: Run test to verify it passes**

Run: `./test.sh --quick`
Expected: PASS.

- [ ] **Step 6: Commit**

```
feat: add backburner sorting and dimming to home page
```

---

### Task 10: Full Test Suite + API Docs

**Files:**

- Modify: `docs/api.md` (if workspace API section exists)

- [ ] **Step 1: Run full test suite**

Run: `./test.sh`
Expected: All tests pass (unit + frontend + typecheck).

- [ ] **Step 2: Update API documentation**

In `docs/api.md`, add the new endpoint under the workspace routes section:

````markdown
### POST /api/workspaces/{id}/backburner

Toggle backburner state for a workspace. Requires `backburner_enabled` in config.

**Request body:**

```json
{ "backburner": true }
```
````

**Response:**

```json
{ "status": "ok" }
```

**Errors:**

- 404 if feature is disabled or workspace not found
- 400 if request body is invalid

```

Also document the `backburner` field on workspace responses and `backburner_enabled` on config.

- [ ] **Step 3: Run full test suite again**

Run: `./test.sh`
Expected: All tests pass (including API docs check).

- [ ] **Step 4: Commit**

```

docs: add backburner API endpoint to api.md

```

```
