# Model Picker Cleanup Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make all model picker dropdowns in the config UI show only enabled models with appropriate capability filtering, powered by a new `Capabilities()` method on `ToolAdapter`.

**Architecture:** Add `Capabilities() []string` to the `ToolAdapter` interface, surface it through `RunnerInfo` in the API contract, rename `state.models` to `state.modelCatalog` in the frontend form state, and derive `models` (enabled) and `oneshotModels` (enabled + oneshot-capable) as computed values from `useConfigForm`.

**Tech Stack:** Go (detect, contracts, models packages), TypeScript/React (useConfigForm, ConfigPage, tab components), gen-types code generator.

---

### Task 1: Add `Capabilities()` to ToolAdapter and implement on all adapters

**Files:**

- Modify: `internal/detect/adapter.go:46-110`
- Modify: `internal/detect/adapter_claude.go`
- Modify: `internal/detect/adapter_codex.go`
- Modify: `internal/detect/adapter_gemini.go`
- Modify: `internal/detect/adapter_opencode.go`

**Step 1: Write the failing test**

Add to `internal/detect/adapter_test.go`:

```go
func TestAdapterCapabilities(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tool string
		want []string
	}{
		{"claude", []string{"interactive", "oneshot", "streaming"}},
		{"codex", []string{"interactive", "oneshot"}},
		{"gemini", []string{"interactive"}},
		{"opencode", []string{"interactive", "oneshot"}},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			got := GetAdapter(tt.tool).Capabilities()
			assertSliceEqual(t, got, tt.want)
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/detect/ -run TestAdapterCapabilities -v`
Expected: compilation error — `Capabilities` not in interface

**Step 3: Add to interface and implement**

In `internal/detect/adapter.go`, add to the `ToolAdapter` interface (after `ModelFlag()`):

```go
	// Capabilities returns the tool modes this adapter supports.
	// Valid values: "interactive", "oneshot", "streaming".
	Capabilities() []string
```

In `internal/detect/adapter_claude.go`, add:

```go
func (a *ClaudeAdapter) Capabilities() []string {
	return []string{"interactive", "oneshot", "streaming"}
}
```

In `internal/detect/adapter_codex.go`, add:

```go
func (a *CodexAdapter) Capabilities() []string {
	return []string{"interactive", "oneshot"}
}
```

In `internal/detect/adapter_gemini.go`, add:

```go
func (a *GeminiAdapter) Capabilities() []string {
	return []string{"interactive"}
}
```

In `internal/detect/adapter_opencode.go`, add:

```go
func (a *OpencodeAdapter) Capabilities() []string {
	return []string{"interactive", "oneshot"}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/detect/ -run TestAdapterCapabilities -v`
Expected: PASS

**Step 5: Commit**

```
feat(detect): add Capabilities() to ToolAdapter interface
```

---

### Task 2: Add `Capabilities` to `contracts.RunnerInfo` and populate in `GetCatalog()`

**Files:**

- Modify: `internal/api/contracts/config.go:44-49`
- Modify: `internal/models/manager.go:29-91`

**Step 1: Write the failing test**

Add to `internal/models/manager_test.go` (create if needed — check if it exists first):

```go
func TestGetCatalogIncludesCapabilities(t *testing.T) {
	cfg := testConfig(t)
	mgr := New(cfg, []detect.Tool{
		{Name: "claude", Command: "claude"},
	})
	catalog, err := mgr.GetCatalog()
	if err != nil {
		t.Fatalf("GetCatalog error: %v", err)
	}
	// Find a model with a claude runner
	for _, m := range catalog {
		if ri, ok := m.Runners["claude"]; ok {
			if len(ri.Capabilities) == 0 {
				t.Errorf("model %s runner claude has no capabilities", m.ID)
			}
			// Claude should have interactive, oneshot, streaming
			want := map[string]bool{"interactive": true, "oneshot": true, "streaming": true}
			got := map[string]bool{}
			for _, c := range ri.Capabilities {
				got[c] = true
			}
			for k := range want {
				if !got[k] {
					t.Errorf("model %s runner claude missing capability %q, got %v", m.ID, k, ri.Capabilities)
				}
			}
			return
		}
	}
	t.Fatal("no model with claude runner found in catalog")
}
```

Note: Check if `manager_test.go` exists and has a `testConfig` helper. If not, create one that returns a minimal `*config.Config`. Look at existing test patterns in the package.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/models/ -run TestGetCatalogIncludesCapabilities -v`
Expected: FAIL — `Capabilities` field doesn't exist on `RunnerInfo`

**Step 3: Add field to contract and populate in GetCatalog**

In `internal/api/contracts/config.go`, update `RunnerInfo`:

```go
type RunnerInfo struct {
	Available       bool     `json:"available"`
	Configured      bool     `json:"configured"`
	RequiredSecrets []string `json:"required_secrets,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
}
```

In `internal/models/manager.go`, in the `GetCatalog()` method, update the runner loop (inside `for toolName, spec := range model.Runners`). After computing `available` and `configured`, look up the adapter's capabilities:

```go
		var capabilities []string
		if adapter := detect.GetAdapter(toolName); adapter != nil {
			capabilities = adapter.Capabilities()
		}
		runners[toolName] = contracts.RunnerInfo{
			Available:       available,
			Configured:      configured,
			RequiredSecrets: spec.RequiredSecrets,
			Capabilities:    capabilities,
		}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/models/ -run TestGetCatalogIncludesCapabilities -v`
Expected: PASS

**Step 5: Run all Go tests**

Run: `go test ./internal/detect/ ./internal/models/ -v`
Expected: all PASS

**Step 6: Commit**

```
feat(contracts): add Capabilities to RunnerInfo, populate from adapter in GetCatalog
```

---

### Task 3: Regenerate TypeScript types

**Step 1: Regenerate**

Run: `go run ./cmd/gen-types`

**Step 2: Verify the generated types include capabilities**

Read `assets/dashboard/src/lib/types.generated.ts` and confirm `RunnerInfo` now has:

```typescript
export interface RunnerInfo {
  available: boolean;
  configured: boolean;
  required_secrets?: string[];
  capabilities?: string[];
}
```

**Step 3: Commit**

```
chore: regenerate TypeScript types for RunnerInfo capabilities
```

---

### Task 4: Rename `state.models` to `state.modelCatalog` in useConfigForm

**Files:**

- Modify: `assets/dashboard/src/routes/config/useConfigForm.ts`

**Step 1: Rename in state type, initial state, and reducer**

In `useConfigForm.ts`:

1. In `ConfigFormState` (line 102): rename `models: Model[]` to `modelCatalog: Model[]`
2. In `initialState` (line 248): rename `models: []` to `modelCatalog: []`
3. In `SET_MODELS` reducer case (line 411-412): change to `return { ...state, modelCatalog: action.models }`
4. In `modelTargetNames` computation (line 476-478): change `state.models` to `state.modelCatalog`

**Step 2: Verify frontend tests still pass**

Run: `./test.sh --quick`
Expected: TypeScript compilation errors in ConfigPage.tsx and other files that reference `state.models` — that's expected, we'll fix those in the next tasks.

Actually — do this rename and the next task together so the build isn't broken between commits.

---

### Task 5: Derive `models` and `oneshotModels` in useConfigForm, update ConfigPage

**Files:**

- Modify: `assets/dashboard/src/routes/config/useConfigForm.ts`
- Modify: `assets/dashboard/src/routes/ConfigPage.tsx`
- Modify: `assets/dashboard/src/routes/config/SessionsTab.tsx`

Do Tasks 4 and 5 as a single commit so the build is never broken.

**Step 1: Add derived lists to useConfigForm**

In `useConfigForm.ts`, after the existing `modelTargetNames` computation (around line 476), add:

```typescript
const models = useMemo(() => {
  const enabled = state.enabledModels;
  const hasExplicit = Object.keys(enabled).length > 0;
  return state.modelCatalog.filter((m) => (hasExplicit ? m.id in enabled : m.configured));
}, [state.modelCatalog, state.enabledModels]);

const oneshotModels = useMemo(() => {
  return models.filter((m) => {
    const preferredRunner = state.enabledModels[m.id];
    if (!preferredRunner) return false;
    const runner = m.runners[preferredRunner];
    return runner?.capabilities?.includes('oneshot') ?? false;
  });
}, [models, state.enabledModels]);
```

Add `useMemo` to the imports from React if not already there.

Return `models` and `oneshotModels` from the hook (alongside existing `modelTargetNames`, etc.):

```typescript
return {
  state,
  dispatch,
  models,
  oneshotModels,
  modelTargetNames,
  // ... rest unchanged
};
```

**Step 2: Update ConfigPage**

In `ConfigPage.tsx`:

1. Destructure the new values from `useConfigForm()`:

   ```typescript
   const { state, dispatch, models, oneshotModels, modelTargetNames, ... } = useConfigForm();
   ```

2. Change `SessionsTab` to pass `state.modelCatalog` instead of `state.models`:

   ```tsx
   <SessionsTab
     models={state.modelCatalog}
     ...
   ```

3. Change ALL other tab components to pass `oneshotModels` instead of `state.models`:
   - `QuickLaunchTab`: `models={oneshotModels}`
   - `CodeReviewTab`: `models={oneshotModels}`
   - `FloorManagerTab`: `models={oneshotModels}`
   - `AdvancedTab`: `models={oneshotModels}`

4. Update `reloadModels` function — it dispatches `SET_MODELS` which now sets `modelCatalog`. No change needed to the dispatch call, just verify the reducer is correct from Task 4.

5. Update the `LOAD_CONFIG` dispatch in `loadConfig` — change `models:` to `modelCatalog:`:
   ```typescript
   modelCatalog: data.models || [],
   ```

**Step 3: Update SessionsTab prop name for clarity (optional but recommended)**

In `SessionsTab.tsx`, rename the prop from `models` to `modelCatalog` so it's clear this component gets the full catalog:

```typescript
type SessionsTabProps = {
  modelCatalog: Model[];
  // ... rest unchanged
};
```

And update the destructuring and the `<ModelCatalog models={modelCatalog} .../>` call.

Then update ConfigPage's `<SessionsTab modelCatalog={state.modelCatalog} .../>`.

**Step 4: Run tests**

Run: `./test.sh --quick`
Expected: PASS (all frontend + backend tests)

**Step 5: Commit**

```
refactor(dashboard): derive models/oneshotModels from modelCatalog in useConfigForm

Renames state.models to state.modelCatalog (full catalog, only used by
ModelCatalog). Derives models (enabled only) and oneshotModels (enabled +
oneshot-capable) as computed values. All TargetSelect consumers now receive
oneshotModels instead of the raw catalog.
```

---

### Task 6: Update SpawnPage to use the same filtering pattern

**Files:**

- Modify: `assets/dashboard/src/routes/SpawnPage.tsx:376-382`

**Step 1: Update the `availableModels` computation**

The SpawnPage already filters by `enabledModels`. It doesn't need oneshot filtering (spawn is interactive). But verify it uses capabilities for consistency if desired, or leave it as-is since it already does the right filtering.

Current code (line 376-382):

```typescript
const availableModels = useMemo(() => {
  const enabled = config?.enabled_models || {};
  const hasExplicit = Object.keys(enabled).length > 0;
  return models
    .filter((m) => (hasExplicit ? m.id in enabled : m.configured))
    .map((m) => ({ name: m.id, label: m.display_name }));
}, [models, config]);
```

This is already correct — it filters by enablement for interactive use. No change needed unless you want to also filter by `interactive` capability (all tools support interactive, so it would be a no-op today).

**Step 2: Verify**

Run: `./test.sh --quick`
Expected: PASS

**Step 3: Commit (skip if no changes)**

---

### Task 7: Final verification

**Step 1: Build the dashboard**

Run: `go run ./cmd/build-dashboard`
Expected: builds successfully

**Step 2: Build the binary**

Run: `go build ./cmd/schmux`
Expected: builds successfully

**Step 3: Run full test suite**

Run: `./test.sh --quick`
Expected: all tests PASS

**Step 4: Manual smoke test (optional)**

Start dev mode (`./dev.sh`), navigate to Settings, verify:

- Models tab shows full catalog with enable/disable checkboxes
- Advanced tab TargetSelect dropdowns show only enabled oneshot-capable models
- Code Review tab TargetSelect dropdowns show only enabled oneshot-capable models
- Floor Manager tab TargetSelect dropdown shows only enabled oneshot-capable models
- Quick Launch tab target dropdown shows only enabled oneshot-capable models
- Spawn page shows only enabled models
