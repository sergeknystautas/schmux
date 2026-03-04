# Model Picker Cleanup

## Problem

The config UI has ~10 TargetSelect instances that all receive the raw model catalog (`state.models`). They don't filter by enablement or capability. The new model/runner system has this information but nobody uses it.

## Design

### Go: Add `Capabilities()` to ToolAdapter

New method on the `ToolAdapter` interface:

```go
Capabilities() []string
```

Returns the tool modes this adapter supports:

| Adapter  | Capabilities                              |
| -------- | ----------------------------------------- |
| claude   | `["interactive", "oneshot", "streaming"]` |
| codex    | `["interactive", "oneshot"]`              |
| gemini   | `["interactive"]`                         |
| opencode | `["interactive", "oneshot"]`              |

Add `Capabilities []string` to `contracts.RunnerInfo`. `GetCatalog()` populates it from the adapter.

### Frontend: Rename `state.models` → `state.modelCatalog`

The raw catalog from the API. Used only by ModelCatalog (the enable/disable UI in SessionsTab). Nothing else touches it.

### Frontend: `useConfigForm` derives two filtered lists

Computed alongside existing derived values like `modelTargetNames`:

- **`models`** — `modelCatalog` filtered to models present in `enabledModels`.
- **`oneshotModels`** — `models` filtered to those whose preferred runner (from `enabledModels[id]`) has `"oneshot"` in its capabilities.

### Frontend: ConfigPage wiring

- **SessionsTab / ModelCatalog**: receives `state.modelCatalog`
- **AdvancedTab, CodeReviewTab, FloorManagerTab, QuickLaunchTab**: receive `oneshotModels`
- **SpawnPage**: uses `models` (separate component tree, same filtering logic)

### TargetSelect

Stays dumb. Takes `Model[]`, renders options. No filtering, no extra props.

## Files to change

### Go

- `internal/detect/adapter.go` — add `Capabilities()` to interface
- `internal/detect/adapter_claude.go` — implement
- `internal/detect/adapter_codex.go` — implement
- `internal/detect/adapter_gemini.go` — implement
- `internal/detect/adapter_opencode.go` — implement
- `internal/api/contracts/config.go` — add `Capabilities` to `RunnerInfo`
- `internal/models/manager.go` — populate capabilities in `GetCatalog()`
- Regenerate types: `go run ./cmd/gen-types`

### Frontend

- `assets/dashboard/src/routes/config/useConfigForm.ts` — rename `models` → `modelCatalog` in state, derive `models` and `oneshotModels`
- `assets/dashboard/src/routes/ConfigPage.tsx` — pass `oneshotModels` to tabs, `modelCatalog` to SessionsTab
- `assets/dashboard/src/routes/config/SessionsTab.tsx` — accept `modelCatalog` prop name
- `assets/dashboard/src/routes/config/ModelCatalog.tsx` — accept `modelCatalog` prop name
- `assets/dashboard/src/routes/config/AdvancedTab.tsx` — no change (receives `models`, now filtered)
- `assets/dashboard/src/routes/config/CodeReviewTab.tsx` — no change
- `assets/dashboard/src/routes/config/FloorManagerTab.tsx` — no change
- `assets/dashboard/src/routes/config/QuickLaunchTab.tsx` — no change
- `assets/dashboard/src/routes/SpawnPage.tsx` — use same filtering pattern
