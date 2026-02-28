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

Additional rot:

- `detectedTargets` in useConfigForm state is always empty after filtering (all detected tools are model runner tools)
- TargetSelect has an empty "Detected Tools" optgroup
- `RunTargetTypePromptable`, `RunTargetSourceDetected`, `RunTargetSourceModel` constants exist only for this bridge
- `MergeDetectedRunTargets()` runs at daemon startup to inject detected tools as run_targets
- `splitRunTargets()` exists to split them back apart
- `GetDetectedRunTarget()` / `GetDetectedRunTargets()` exist only for this system

## Goal

Make `run_targets` a command-only concept. Models are models. Tools are tracked through the model catalog's runner detection. The "promptable" determination moves from a stored `type` field to a runtime check: is it a model ID or builtin tool name?

## Design

### Backend Changes

#### Delete the Bridge

| What                             | Where                       | Action |
| -------------------------------- | --------------------------- | ------ |
| `GetEnabledRunTargets()`         | `models/manager.go:281-307` | Delete |
| Bridge injection                 | `handlers_config.go:104`    | Delete |
| `MergeDetectedRunTargets()`      | `run_targets.go:202-215`    | Delete |
| `splitRunTargets()`              | `run_targets.go:185-198`    | Delete |
| `MergeDetectedRunTargets()` call | `daemon.go`                 | Delete |
| `GetDetectedRunTarget()`         | `config.go`                 | Delete |
| `GetDetectedRunTargets()`        | `config.go`                 | Delete |
| `RunTargetTypePromptable`        | `config.go:447`             | Delete |
| `RunTargetSourceDetected`        | `config.go:450`             | Delete |
| `RunTargetSourceModel`           | `config.go:451`             | Delete |

#### Simplify RunTarget

The `RunTarget` struct drops `Type` and `Source` fields. All entries are implicitly command/user:

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

Same change in `internal/api/contracts/config.go`.

#### Simplify Validation

`validateRunTargets()` becomes: check name and command non-empty, no duplicates, no collision with builtin tool names. No type/source branching.

#### Update `quickLaunchTargetPromptable()`

Remove the run_targets loop. Keep only the model ID and builtin tool name checks:

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

This function no longer needs the `targets []RunTarget` parameter. Rename to `isPromptableTarget` since it no longer scans run_targets.

Update all callers: `validateQuickLaunch`, `validateNudgenikConfig`, `validateCompoundConfig`, `validateQuickLaunchTargets`.

#### Update `IsModel()`

`models.Manager.IsModel()` currently falls back to `config.GetRunTarget(name)` and checks `target.Type == RunTargetTypePromptable`. After removing detected tools from run_targets, this fallback only finds command targets (never promptable). Change to check `detect.IsBuiltinToolName()`:

```go
func (m *Manager) IsModel(name string) (promptable bool, found bool) {
    if m.IsModelID(name) {
        _, err := m.ResolveModel(name)
        return true, err == nil
    }
    // Builtin tool name (e.g. "claude") — promptable
    if detect.IsBuiltinToolName(name) {
        return true, true
    }
    // Command target — not promptable
    if _, ok := m.config.GetRunTarget(name); ok {
        return false, true
    }
    return false, false
}
```

### Config Migration

Add a migration using the existing pattern (Name/Detect/Apply):

**Migration: `drop_detected_and_model_run_targets`**

Detect: any run_target with `source === "detected"` or `source === "model"`.

Apply:

1. Remove all detected/model entries from `cfg.RunTargets`
2. Strip `Type` and `Source` fields from remaining entries (all are command/user)

**Migration: `migrate_tool_name_targets`**

Detect: any quick_launch/nudgenik/compound/branch_suggest/conflict_resolve/desync/io_workspace_telemetry/pr_review/commit_message target field that is a builtin tool name (not a model ID, not a command target name).

Apply: for each tool-name reference, find the highest-tier enabled model with that tool as a runner. Rewrite the target to that model's ID. If no enabled model uses that tool, pick the first model in the catalog that has it.

### Frontend Changes

#### SpawnPage.tsx

Delete:

- `promptableTargets` state variable (line 154)
- Run_targets filtering logic (lines 255-272)
- `PromptableListItem` type (lines 386-389)

Replace `promptableList` useMemo (lines 391-397) — derive from `models` + `enabled_models`:

```tsx
const promptableList = useMemo(() => {
  const enabled = /* enabled_models from config */;
  const enabledModels = models.filter(m => {
    if (Object.keys(enabled).length === 0) {
      // No explicit enablement — all configured models available
      return m.configured;
    }
    return m.id in enabled;
  });
  return enabledModels.map(m => ({ name: m.id, label: m.display_name }));
}, [models, /* enabled_models */]);
```

The rest of SpawnPage (targetCounts, model selection mode, rendering) stays unchanged — it already works with `promptableList`.

#### SessionDetailPage.tsx

Lines 247-249, 301-302: replace `config.run_targets.find(t => t.type === 'promptable')` with model-based lookup:

```tsx
const firstModel = config.models?.find((m) => config.enabled_models?.[m.id]);
target = firstModel?.id || '';
```

#### AppShell.tsx (line 898-901) and SessionTabs.tsx (line 271-272)

Replace run_target lookup with model-based check:

```tsx
// Before
const runTarget = (config?.run_targets || []).find((t) => t.name === sess.target);
const isPromptable = runTarget ? runTarget.type === 'promptable' : true;

// After
const isCommand = (config?.run_targets || []).some((t) => t.name === sess.target);
const isPromptable = !isCommand;
```

After cleanup, `run_targets` only contains command targets. If the session's target is NOT in run_targets, it's a model (promptable). If it IS in run_targets, it's a command (not promptable).

#### TargetSelect.tsx

Delete `detectedTargets` prop. Delete "Detected Tools" optgroup. Keep only "Models" optgroup:

```tsx
type TargetSelectProps = {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  includeDisabledOption?: boolean;
  includeNoneOption?: string;
  models: Model[];
  className?: string;
};

// Render: single <optgroup label="Models"> with configured models
```

#### useConfigForm.ts

Delete `detectedTargets` field from state (line 98), initial state (line 245), and computed values (line 479).

The `modelTargetNames` computation changes from including detectedTargets to only including configured models (which it already does on the second line).

#### ConfigPage.tsx

Delete detected targets loading/filtering (lines 112-132). Delete passing `detectedTargets` to all tabs.

#### Tab Components

Remove `detectedTargets` prop from: AdvancedTab, CodeReviewTab, FloorManagerTab, QuickLaunchTab.

Update help text: "Select a promptable target" → "Select a model" (AdvancedTab lines 236, 398, 425; CodeReviewTab lines 59, 85).

Update warning text: "not available or not promptable" → "not available" (AdvancedTab lines 240, 402, 430; CodeReviewTab lines 63, 89).

#### Generated Types

After Go contract changes, run `go run ./cmd/gen-types`. The `RunTarget` interface loses `type` and `source` fields. `RunTargetResponse` in `types.ts` updated to match.

### Test Updates

Every test file constructing `{ type: 'promptable', source: 'detected' }` fixtures needs updating:

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
| `SessionsTab.test.tsx`            | Remove "does not render detected targets" test (nothing to assert against)          |

### What Survives

- **`RunTarget` struct** — name + command, for user-defined command targets only
- **`run_targets` in config.json** — command targets only
- **`run_targets` in API response** — command targets only
- **`models.Manager.IsModel()`** — determines promptable at runtime
- **`isPromptableTarget()`** (renamed from `quickLaunchTargetPromptable`) — model ID + tool name check
- **`ResolveTarget()`** — models through model resolution, commands through `GetRunTarget()`
- **Command Targets UI** in SessionsTab — unchanged
- **`ModelCatalog` component** — unchanged
- **`enabled_models` config/API** — unchanged
