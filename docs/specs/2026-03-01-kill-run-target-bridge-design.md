# Kill the Run Target Bridge

## Problem

The model configuration editor (Feb 27) replaced the Sessions tab's three sections with a clean model catalog UI. But the plumbing underneath was never cleaned up. Enabled models still travel a pointless roundtrip:

```
Config enabled_models
  → models.GetEnabledRunTargets()           # converts to fake RunTarget{source:"model"}
  → API response run_targets[]              # mixed bag: detected + user + model entries
  → SpawnPage filters type==="promptable"   # extracts models back out
  → promptableTargets state                 # stores as RunTargetResponse[]
  → promptableList useMemo                  # maps names back to model display_names
  → render
```

Meanwhile the API already returns `models` and `enabled_models` directly.

### Concrete Rot Inventory

**Category 1 — Bridge Functions** (Go code that converts models ↔ fake RunTargets):

- `GetEnabledRunTargets()` — converts models to fake RunTarget entries
- `MergeDetectedRunTargets()` — injects detected tools as fake RunTargets at startup
- `splitRunTargets()` — exists only to undo MergeDetectedRunTargets
- `GetDetectedRunTarget()` / `GetDetectedRunTargets()` — only callers are the bridge
- `DetectedToolsFromConfig()` — reverse of MergeDetectedRunTargets
- `normalizeRunTargets()` — sets Source field that shouldn't exist
- handlers_config.go line 104 — injects bridge output into API response

**Category 2 — Dead Constants and Struct Fields:**

- `RunTargetTypePromptable` / `RunTargetSourceDetected` / `RunTargetSourceModel` constants
- `Type` and `Source` fields on RunTarget struct (config + contracts)
- 75% of `validateRunTargets()` — type/source validation for things that shouldn't exist

**Category 3 — Frontend Ghost State:**

- `detectedTargets` in useConfigForm — always empty after filtering
- `detectedTargets` prop threaded to ALL 4 config tabs
- TargetSelect "Detected Tools" optgroup — renders empty list
- `promptableTargets` state in SpawnPage — reconstructs models from run_targets roundtrip
- `PromptableListItem` type in SpawnPage
- ConfigPage lines 112-132 — filtering that produces empty arrays

**Category 4 — Scattered `type === 'promptable'` Checks:**

- AppShell.tsx, SessionTabs.tsx, SessionDetailPage.tsx all do run_target type lookups

## Goal

Make `run_targets` a command-only concept. Models are models. Tools are tracked through the model catalog's runner detection. The "promptable" determination moves from a stored `type` field to a runtime check: is it a model ID or builtin tool name?

## What Doesn't Change

- **Model manager** — `ResolveToolForModel()`, `GetCatalog()`, `IsModelID()`, `GetEnabledModels()` — all untouched
- **`enabled_models` config** — untouched
- **ModelCatalog component** — untouched
- **Command Targets UI** — untouched
- **Spawn wizard rendering** — it already works with a `promptableList` array; we just change how that array is derived

## Design

### Phase 1: Gut the Backend Bridge

**Delete these functions entirely:**

| What                             | Where                       | Action |
| -------------------------------- | --------------------------- | ------ |
| `GetEnabledRunTargets()`         | `models/manager.go:281-307` | Delete |
| Bridge injection                 | `handlers_config.go:104`    | Delete |
| `MergeDetectedRunTargets()`      | `run_targets.go:202-215`    | Delete |
| `splitRunTargets()`              | `run_targets.go:185-198`    | Delete |
| `normalizeRunTargets()`          | `run_targets.go:177-183`    | Delete |
| `MergeDetectedRunTargets()` call | `daemon.go:494`             | Delete |
| `GetDetectedRunTarget()`         | `config.go`                 | Delete |
| `GetDetectedRunTargets()`        | `config.go`                 | Delete |
| `DetectedToolsFromConfig()`      | `config.go`                 | Delete |
| `RunTargetTypePromptable`        | `config.go:447`             | Delete |
| `RunTargetSourceDetected`        | `config.go:450`             | Delete |
| `RunTargetSourceModel`           | `config.go:451`             | Delete |

**Strip RunTarget struct** — remove `Type` and `Source` fields from both `internal/config/config.go` and `internal/api/contracts/config.go`:

```go
// Before
type RunTarget struct {
    Name    string `json:"name"`
    Type    string `json:"type"`
    Command string `json:"command"`
    Source  string `json:"source,omitempty"`
}

// After
type RunTarget struct {
    Name    string `json:"name"`
    Command string `json:"command"`
}
```

**Simplify `validateRunTargets()`** — check name non-empty, command non-empty, no duplicates, no collision with builtin tool names. No type/source branching.

**Rename `quickLaunchTargetPromptable()` to `isPromptableTarget()`** — drop the `targets []RunTarget` parameter:

```go
func isPromptableTarget(targetName string) (bool, bool) {
    if detect.IsModelID(targetName) {
        return true, true
    }
    if detect.IsBuiltinToolName(targetName) {
        return true, true
    }
    return false, false
}
```

Callers (`validateQuickLaunch`, `validateNudgenikConfig`, `validateCompoundConfig`) that need "does this target exist?" now also check command targets via a separate lookup.

**Update `IsModel()` in manager.go** — remove the `GetRunTarget` fallback that checks `target.Type == RunTargetTypePromptable`. Replace with `detect.IsBuiltinToolName()` check.

**Fix all Go compilation errors** from deleted constants/functions/fields.

### Phase 2: Config Migration

Two migrations using the existing Name/Detect/Apply pattern:

#### Migration: `drop_detected_and_model_run_targets`

**Detect**: any run_target with `source === "detected"` or `source === "model"`.

**Apply**:

1. Remove all entries where `source` is `"detected"` or `"model"`
2. Strip `type` and `source` fields from remaining entries

Before:

```json
{
  "run_targets": [
    {
      "name": "claude",
      "type": "promptable",
      "command": "/usr/local/bin/claude",
      "source": "detected"
    },
    {
      "name": "codex",
      "type": "promptable",
      "command": "/usr/local/bin/codex",
      "source": "detected"
    },
    { "name": "build", "type": "command", "command": "go build ./...", "source": "user" }
  ]
}
```

After:

```json
{
  "run_targets": [{ "name": "build", "command": "go build ./..." }]
}
```

#### Migration: `rewrite_tool_name_targets_to_model_ids`

**Detect**: any target field in `quick_launch`, `nudgenik`, `compound`, `branch_suggest`, `conflict_resolve`, `desync`, `io_workspace_telemetry`, `pr_review`, `commit_message` that is a builtin tool name (not a model ID, not a command target name).

**Apply**: for each tool-name reference, find the highest-tier enabled model with that tool as preferred runner. Rewrite the target to that model's ID. If no enabled model uses that tool, pick the first model in the catalog that has it.

Before:

```json
{
  "quick_launch": [{ "name": "review", "target": "claude", "prompt": "review this code" }]
}
```

After:

```json
{
  "quick_launch": [
    { "name": "review", "target": "claude-sonnet-4-6", "prompt": "review this code" }
  ]
}
```

### Phase 3: Regen Types and Fix Frontend

**Run `go run ./cmd/gen-types`** — RunTarget interface loses `type` and `source` fields.

**Update `RunTargetResponse` in types.ts** — match the new shape (name + command only).

**Delete from useConfigForm.ts:**

- `detectedTargets` field from state type (line 98)
- `detectedTargets` initial value (line 245)
- `detectedTargets` in computed values (line 479)

**Delete from ConfigPage.tsx:**

- Detected target filtering logic (lines 112-132)
- `detectedTargets` prop passed to all 4 tabs (lines 1211, 1237, 1250, 1325)

**Delete from TargetSelect.tsx:**

- `detectedTargets` prop
- "Detected Tools" optgroup (lines 34-40)

**Delete `detectedTargets` prop from:** AdvancedTab, CodeReviewTab, FloorManagerTab, QuickLaunchTab — and from all their test files.

### Phase 4: Fix SpawnPage Model Derivation

**Delete from SpawnPage.tsx:**

- `promptableTargets` state (line 154)
- Run_targets filtering logic (lines 255-272)
- `PromptableListItem` type (lines 386-389)

**Replace `promptableList` derivation** — derive from `models` + `enabled_models` directly:

```tsx
const promptableList = useMemo(() => {
  const enabled = config?.enabled_models || {};
  const hasExplicit = Object.keys(enabled).length > 0;
  return models
    .filter((m) => (hasExplicit ? m.id in enabled : m.configured))
    .map((m) => ({ name: m.id, label: m.display_name }));
}, [models, config]);
```

### Phase 5: Fix Scattered Promptable Checks

**AppShell.tsx and SessionTabs.tsx** — replace:

```tsx
// Before
const runTarget = (config?.run_targets || []).find((t) => t.name === sess.target);
const isPromptable = runTarget ? runTarget.type === 'promptable' : true;

// After: run_targets only contains command targets now
const isCommand = (config?.run_targets || []).some((t) => t.name === sess.target);
const isPromptable = !isCommand;
```

**SessionDetailPage.tsx** — replace `config.run_targets.find(t => t.type === 'promptable')` with model-based lookup:

```tsx
const firstModel = config.models?.find((m) => config.enabled_models?.[m.id]);
target = firstModel?.id || '';
```

### Phase 6: Fix All Tests

Every test file constructing `{ type: 'promptable', source: 'detected' }` fixtures gets updated:

| File                              | Change                                                                              |
| --------------------------------- | ----------------------------------------------------------------------------------- |
| `run_targets_test.go`             | Remove all promptable/detected/model test cases. Add command-only validation tests. |
| `config_test.go`                  | Update config JSON fixtures, remove `GetDetectedRunTargets` tests                   |
| `api_contract_test.go`            | Remove `"promptable"` target references in spawn test cases, use model IDs          |
| `handlers_test.go`                | Update spawn test expectations                                                      |
| `manager_test.go` (session)       | Update RunTarget fixtures                                                           |
| `SpawnPage.agent-select.test.tsx` | Use models instead of promptable run_targets                                        |
| `ConfigPage.test.tsx`             | Remove detected run_target from mock config                                         |
| `CodeReviewTab.test.tsx`          | Remove detectedTargets prop                                                         |
| `AdvancedTab.test.tsx`            | Remove detectedTargets prop                                                         |
| `TargetSelect.test.tsx`           | Remove detectedTargets prop and "Detected Tools" tests                              |
| `useConfigForm.test.ts`           | Remove detectedTargets from state assertions                                        |
| `ConfigModals.test.tsx`           | Update promptable target rendering test                                             |
| `mockTransport.test.ts`           | Remove promptable run_target assertion                                              |
| `SessionsTab.test.tsx`            | Remove "does not render detected targets" test                                      |

### Phase 7: Verify

- `go test ./...`
- Frontend tests via vitest
- `./test.sh --quick`
- `go run ./cmd/gen-types` (confirm generated types match)
- `go run ./cmd/build-dashboard` (confirm dashboard builds)

## What Survives

- **`RunTarget` struct** — name + command, for user-defined command targets only
- **`run_targets` in config.json** — command targets only
- **`run_targets` in API response** — command targets only
- **`models.Manager.IsModel()`** — determines promptable at runtime
- **`isPromptableTarget()`** (renamed from `quickLaunchTargetPromptable`) — model ID + tool name check
- **`ResolveTarget()`** — models through model resolution, commands through `GetRunTarget()`
- **Command Targets UI** in SessionsTab — unchanged
- **`ModelCatalog` component** — unchanged
- **`enabled_models` config/API** — unchanged
