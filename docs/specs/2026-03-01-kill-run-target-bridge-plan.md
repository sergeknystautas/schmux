# Kill the Run Target Bridge — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove the fake RunTarget bridge that launders models through run_targets, making run_targets a command-only concept.

**Architecture:** Delete bridge functions/constants/struct fields from backend, pass detected tools directly to model manager, then cascade fixes through frontend (remove ghost state, fix promptable checks, regen types). Two config migrations handle existing user configs.

**Tech Stack:** Go (backend), React/TypeScript (frontend), Vitest + React Testing Library (frontend tests), Go testing (backend tests)

---

## Context

Read these before starting:

- `docs/specs/2026-03-01-kill-run-target-bridge-design.md` — the design this plan implements
- `internal/config/run_targets.go` — bridge functions to delete (216 lines)
- `internal/config/config.go` — struct/constants to delete (WARNING: large file, use grep/offset)
- `internal/models/manager.go` — bridge method + DetectedToolsFromConfig callers
- `internal/dashboard/handlers_config.go` — bridge injection + config update handler
- `assets/dashboard/src/routes/config/useConfigForm.ts` — ghost state to delete
- `assets/dashboard/src/routes/ConfigPage.tsx` — ghost prop threading to delete
- `assets/dashboard/src/routes/SpawnPage.tsx` — promptableTargets roundtrip to delete

---

### Task 1: Pass Detected Tools Directly to Model Manager

The model manager currently extracts detected tools from RunTargets via `DetectedToolsFromConfig()`. After we delete the bridge, detected tools won't be in RunTargets. Give the manager its own copy.

**Files:**

- Modify: `internal/models/manager.go:16-23`
- Modify: `internal/daemon/daemon.go:489-503`
- Modify: `internal/dashboard/handlers_test.go:181`
- Modify: `internal/dashboard/api_contract_test.go:48`
- Modify: `internal/dashboard/handlers_models_test.go:12,108`
- Modify: `internal/dashboard/handlers_remote_access_test.go:29`

**Step 1: Update Manager struct and constructor**

In `internal/models/manager.go`, change lines 16-23:

```go
// Before
type Manager struct {
	config *config.Config
}

func New(cfg *config.Config) *Manager {
	return &Manager{config: cfg}
}

// After
type Manager struct {
	config        *config.Config
	detectedTools []detect.Tool
}

func New(cfg *config.Config, detectedTools []detect.Tool) *Manager {
	return &Manager{config: cfg, detectedTools: detectedTools}
}
```

**Step 2: Replace DetectedToolsFromConfig calls in manager**

In `internal/models/manager.go`, replace line 30:

```go
// Before
detectedTools := config.DetectedToolsFromConfig(m.config)

// After
detectedTools := m.detectedTools
```

Replace line 172:

```go
// Before
detectedTools := config.DetectedToolsFromConfig(m.config)

// After
detectedTools := m.detectedTools
```

Remove the `config` import if it's no longer used (it is — for `config.Config` type and other calls).

**Step 3: Update daemon.go constructor call**

In `internal/daemon/daemon.go`, change line 503:

```go
// Before
mm := models.New(cfg)

// After
mm := models.New(cfg, detectedTargets)
```

The `detectedTargets` variable is already in scope from line 489.

**Step 4: Update all test constructor calls**

In each test file, change `models.New(cfg)` → `models.New(cfg, nil)`:

- `internal/dashboard/handlers_test.go:181`
- `internal/dashboard/api_contract_test.go:48`
- `internal/dashboard/handlers_models_test.go:12,108`
- `internal/dashboard/handlers_remote_access_test.go:29`

**Step 5: Run tests**

Run: `go test ./internal/models/ ./internal/dashboard/ ./internal/daemon/ -count=1`
Expected: ALL PASS

**Step 6: Commit**

Message: `refactor(models): pass detected tools directly to manager constructor`

---

### Task 2: Delete Bridge Functions and Constants

Gut the core bridge infrastructure from Go code.

**Files:**

- Modify: `internal/config/run_targets.go` — delete `splitRunTargets`, `MergeDetectedRunTargets`, `normalizeRunTargets`
- Modify: `internal/config/config.go` — delete constants, struct fields, methods, `DetectedToolsFromConfig`
- Modify: `internal/api/contracts/config.go` — strip RunTarget struct
- Modify: `internal/daemon/daemon.go` — delete MergeDetectedRunTargets call
- Modify: `internal/dashboard/handlers_config.go` — delete bridge injection and simplify update handler

**Step 1: Delete bridge functions from run_targets.go**

Delete these function bodies entirely:

- Lines 177-183: `normalizeRunTargets()`
- Lines 185-198: `splitRunTargets()`
- Lines 200-215: `MergeDetectedRunTargets()`

After deletion, remove the `detect` import if unused (it's still used by `quickLaunchTargetPromptable` → `detect.IsModelID`, `detect.IsBuiltinToolName`).

**Step 2: Delete constants from config.go**

Delete from the const block (around lines 446-451):

- `RunTargetTypePromptable = "promptable"` (line 447)
- `RunTargetTypeCommand = "command"` (line 448)
- `RunTargetSourceDetected = "detected"` (line 450)
- `RunTargetSourceModel = "model"` (line 451)

Keep `RunTargetSourceUser = "user"` ONLY if something still references it. Check — if nothing does after this cleanup, delete it too.

**Step 3: Strip RunTarget struct fields**

In `internal/config/config.go`, change the RunTarget struct (lines 419-424):

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

In `internal/api/contracts/config.go`, make the same change (lines 24-29):

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

**Step 4: Delete methods from config.go**

Delete these methods entirely:

- `GetDetectedRunTarget()` (lines 1216-1225)
- `GetDetectedRunTargets()` (lines 1228-1238)
- `DetectedToolsFromConfig()` (lines 2036-2045)

Delete the two `normalizeRunTargets()` calls:

- Line 1311: `normalizeRunTargets(newCfg.RunTargets)` — delete this line
- Line 1437: `normalizeRunTargets(cfg.RunTargets)` — delete this line

**Step 5: Delete daemon startup merge**

In `internal/daemon/daemon.go`, delete lines 487-500 (the entire detected targets block):

```go
// DELETE all of this:
// Detect run targets once on daemon start and persist to config
detectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
detectedTargets, err := detect.DetectAvailableToolsContext(detectCtx, false)
cancel()
if err != nil {
    configLog.Warn("failed to detect run targets", "err", err)
} else {
    cfg.RunTargets = config.MergeDetectedRunTargets(cfg.RunTargets, detectedTargets)
    if err := cfg.Validate(); err != nil {
        configLog.Warn("failed to validate config after detection", "err", err)
    } else if err := cfg.Save(); err != nil {
        configLog.Warn("failed to save config after detection", "err", err)
    }
}
```

BUT we still need `detectedTargets` for the model manager constructor (Task 1). Replace with:

```go
// Detect available tools for model catalog
detectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
detectedTargets, err := detect.DetectAvailableToolsContext(detectCtx, false)
cancel()
if err != nil {
    configLog.Warn("failed to detect tools", "err", err)
    detectedTargets = nil
}
```

**Step 6: Delete bridge injection from handlers_config.go**

Delete line 104:

```go
// DELETE:
runTargetResp = append(runTargetResp, s.models.GetEnabledRunTargets(seenTargets)...)
```

Also delete the `seenTargets` map that feeds it (should be a few lines above — check context).

**Step 7: Simplify config update handler in handlers_config.go**

Lines 281-310 currently validate source/type and calls `MergeDetectedRunTargets`. Replace with simple command-target-only logic:

```go
if req.RunTargets != nil {
    userTargets := make([]config.RunTarget, len(req.RunTargets))
    for i, t := range req.RunTargets {
        if t.Name == "" {
            http.Error(w, "run target name is required", http.StatusBadRequest)
            return
        }
        if t.Command == "" {
            http.Error(w, fmt.Sprintf("run target command is required for %s", t.Name), http.StatusBadRequest)
            return
        }
        userTargets[i] = config.RunTarget{Name: t.Name, Command: t.Command}
    }
    cfg.RunTargets = userTargets
}
```

**Step 8: Delete GetEnabledRunTargets from manager.go**

Delete lines 278-307 in `internal/models/manager.go` (the entire `GetEnabledRunTargets` method).

**Step 9: Update IsModel in manager.go**

Replace lines 263-276:

```go
// Before
func (m *Manager) IsModel(name string) (promptable bool, found bool) {
	if m.IsModelID(name) {
		_, err := m.ResolveModel(name)
		return true, err == nil
	}
	if target, ok := m.config.GetRunTarget(name); ok {
		return target.Type == config.RunTargetTypePromptable, true
	}
	return false, false
}

// After
func (m *Manager) IsModel(name string) (promptable bool, found bool) {
	if m.IsModelID(name) {
		_, err := m.ResolveModel(name)
		return true, err == nil
	}
	if detect.IsBuiltinToolName(name) {
		return true, true
	}
	if _, ok := m.config.GetRunTarget(name); ok {
		return false, true
	}
	return false, false
}
```

**Step 10: Attempt compilation**

Run: `go build ./...`
Expected: Compilation errors from test files and any remaining references. List them and fix in Task 3.

---

### Task 3: Simplify Validation and Fix Remaining Go References

**Files:**

- Modify: `internal/config/run_targets.go` — simplify validateRunTargets, rename quickLaunchTargetPromptable
- Modify: `internal/config/config.go` — update validateRunTargetDependencies caller

**Step 1: Rewrite validateRunTargets**

Replace lines 10-57 in `run_targets.go`:

```go
func validateRunTargets(targets []RunTarget) error {
	seen := make(map[string]struct{})
	for _, target := range targets {
		name := strings.TrimSpace(target.Name)
		if name == "" {
			return fmt.Errorf("%w: run target name is required", ErrInvalidConfig)
		}
		if target.Command == "" {
			return fmt.Errorf("%w: run target command is required for %s", ErrInvalidConfig, name)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("%w: duplicate run target name: %s", ErrInvalidConfig, name)
		}
		if detect.IsBuiltinToolName(name) {
			return fmt.Errorf("%w: run target name %s collides with detected tool", ErrInvalidConfig, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}
```

**Step 2: Rename quickLaunchTargetPromptable to isPromptableTarget**

Replace lines 162-175:

```go
// isPromptableTarget returns whether the target is a model or tool (promptable)
// and whether it exists. Command targets return (false, true).
func isPromptableTarget(targetName string, commandTargets []RunTarget) (bool, bool) {
	if detect.IsModelID(targetName) {
		return true, true
	}
	if detect.IsBuiltinToolName(targetName) {
		return true, true
	}
	for _, target := range commandTargets {
		if target.Name == targetName {
			return false, true
		}
	}
	return false, false
}
```

Note: we keep the `commandTargets` parameter so callers can still check "does this target exist?" for command targets. The function just no longer checks `target.Type`.

**Step 3: Update all callers of quickLaunchTargetPromptable**

In `run_targets.go`, replace every `quickLaunchTargetPromptable(` with `isPromptableTarget(`:

- Line 75 in `validateQuickLaunch`
- Line 106 in `validateNudgenikConfig`
- Line 123 in `validateQuickLaunchTargets`
- Line 139 in `validateCompoundConfig`

The `targets []RunTarget` parameter name stays the same — it's just command targets now.

**Step 4: Update validateRunTargetDependencies signature**

No change needed — it still takes `targets []RunTarget` and passes them through. The meaning of `targets` changes from "all targets" to "command targets only" but the signature is the same.

**Step 5: Update the convert_user_promptable_to_command migration**

The migration at lines 516-542 references `RunTargetTypePromptable`, `RunTargetSourceUser`, and `RunTarget.Type`/`.Source` which we're deleting. This migration needs to use raw JSON since the struct no longer has those fields.

Replace with a raw-JSON migration:

```go
{
    Name: "convert_user_promptable_to_command",
    Detect: func(raw map[string]json.RawMessage, _ *Config) bool {
        var targets []struct {
            Source string `json:"source"`
            Type   string `json:"type"`
        }
        if data, ok := raw["run_targets"]; ok {
            if json.Unmarshal(data, &targets) == nil {
                for _, t := range targets {
                    source := t.Source
                    if source == "" {
                        source = "user"
                    }
                    if source == "user" && t.Type == "promptable" {
                        return true
                    }
                }
            }
        }
        return false
    },
    Apply: func(raw map[string]json.RawMessage, cfg *Config) error {
        // Migration was already applied by drop_detected_and_model_run_targets
        // which removes type/source. This is a no-op if that migration ran first.
        return nil
    },
},
```

Actually, simpler: just delete this migration entirely and fold its intent into the new `drop_detected_and_model_run_targets` migration (Task 4).

**Step 6: Compile**

Run: `go build ./...`
Expected: PASS (all Go source compiles). Test files will still fail.

---

### Task 4: Add Config Migrations

**Files:**

- Modify: `internal/config/config.go` — add two new migrations, delete old one

**Step 1: Replace convert_user_promptable_to_command migration**

Delete the `convert_user_promptable_to_command` migration block (lines 516-542). Replace with:

```go
{
    Name: "drop_run_target_bridge_fields",
    Detect: func(raw map[string]json.RawMessage, _ *Config) bool {
        var targets []struct {
            Type   string `json:"type"`
            Source string `json:"source"`
        }
        if data, ok := raw["run_targets"]; ok {
            if json.Unmarshal(data, &targets) == nil {
                for _, t := range targets {
                    if t.Type != "" || t.Source != "" {
                        return true
                    }
                }
            }
        }
        return false
    },
    Apply: func(_ map[string]json.RawMessage, cfg *Config) error {
        // RunTarget struct no longer has Type/Source, so just filter out
        // entries that were detected or model-sourced (they'll have empty
        // Name/Command after round-tripping through the new struct).
        // Actually, since the struct dropped those fields, JSON unmarshal
        // already stripped them. We just need to remove entries that had
        // source="detected" or source="model" — but those are already gone
        // because the struct can't hold them. The only thing left is to
        // remove any entries with empty commands (detected tools that had
        // commands but were also model entries).
        var cleaned []RunTarget
        for _, t := range cfg.RunTargets {
            if t.Name != "" && t.Command != "" {
                cleaned = append(cleaned, t)
            }
        }
        cfg.RunTargets = cleaned
        return nil
    },
},
```

Wait — this won't work correctly because by the time `Apply` runs, the JSON has already been unmarshalled into the Config struct, so Type/Source are lost. The `Detect` function uses raw JSON to check, but `Apply` works on the struct.

Better approach: use raw JSON in Apply too:

```go
{
    Name: "drop_run_target_bridge_fields",
    Detect: func(raw map[string]json.RawMessage, _ *Config) bool {
        var targets []struct {
            Source string `json:"source"`
        }
        if data, ok := raw["run_targets"]; ok {
            if json.Unmarshal(data, &targets) == nil {
                for _, t := range targets {
                    if t.Source == "detected" || t.Source == "model" || t.Source != "" {
                        return true
                    }
                }
            }
        }
        return false
    },
    Apply: func(raw map[string]json.RawMessage, cfg *Config) error {
        type legacyTarget struct {
            Name    string `json:"name"`
            Command string `json:"command"`
            Source  string `json:"source"`
        }
        var old []legacyTarget
        if data, ok := raw["run_targets"]; ok {
            if err := json.Unmarshal(data, &old); err != nil {
                return err
            }
        }
        var cleaned []RunTarget
        for _, t := range old {
            source := t.Source
            if source == "" {
                source = "user"
            }
            if source == "detected" || source == "model" {
                continue
            }
            cleaned = append(cleaned, RunTarget{Name: t.Name, Command: t.Command})
        }
        cfg.RunTargets = cleaned
        return nil
    },
},
```

**Step 2: Add tool-name-to-model-ID migration**

Add after the previous migration:

```go
{
    Name: "rewrite_tool_name_targets_to_model_ids",
    Detect: func(_ map[string]json.RawMessage, cfg *Config) bool {
        // Check if any config target field is a builtin tool name
        for _, ql := range cfg.QuickLaunch {
            if detect.IsBuiltinToolName(ql.Target) {
                return true
            }
        }
        if cfg.Nudgenik != nil && detect.IsBuiltinToolName(cfg.Nudgenik.Target) {
            return true
        }
        if cfg.Compound != nil && detect.IsBuiltinToolName(cfg.Compound.Target) {
            return true
        }
        return false
    },
    Apply: func(_ map[string]json.RawMessage, cfg *Config) error {
        resolve := func(toolName string) string {
            // Find highest-tier enabled model with this tool as runner
            models := detect.GetBuiltinModels()
            enabled := cfg.ModelsConfig.Enabled
            for _, m := range models {
                if _, ok := m.RunnerFor(toolName); !ok {
                    continue
                }
                if preferred, isEnabled := enabled[m.ID]; isEnabled && preferred == toolName {
                    return m.ID
                }
            }
            // Fall back to first model that has this tool
            for _, m := range models {
                if _, ok := m.RunnerFor(toolName); ok {
                    return m.ID
                }
            }
            return toolName // give up, keep as-is
        }

        for i := range cfg.QuickLaunch {
            if detect.IsBuiltinToolName(cfg.QuickLaunch[i].Target) {
                cfg.QuickLaunch[i].Target = resolve(cfg.QuickLaunch[i].Target)
            }
        }
        if cfg.Nudgenik != nil && detect.IsBuiltinToolName(cfg.Nudgenik.Target) {
            cfg.Nudgenik.Target = resolve(cfg.Nudgenik.Target)
        }
        if cfg.Compound != nil && detect.IsBuiltinToolName(cfg.Compound.Target) {
            cfg.Compound.Target = resolve(cfg.Compound.Target)
        }
        return nil
    },
},
```

**Step 3: Compile and run migration tests**

Run: `go build ./...`
Run: `go test ./internal/config/ -run TestMigration -v`
Expected: May need to update existing migration tests. Fix compilation errors first.

**Step 4: Commit**

Message: `feat(config): add migrations to drop run target bridge fields and rewrite tool-name targets`

---

### Task 5: Fix All Go Test Files

Every Go test that references deleted constants, struct fields, or functions needs updating.

**Files:**

- Modify: `internal/config/run_targets_test.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/dashboard/api_contract_test.go`
- Modify: `internal/dashboard/handlers_test.go`
- Modify: `internal/session/manager_test.go`
- Modify: `internal/oneshot/oneshot_test.go`
- Modify: `cmd/schmux/spawn_test.go`

**Step 1: Fix run_targets_test.go**

Remove all test cases that reference `RunTargetTypePromptable`, `RunTargetSourceDetected`, `RunTargetSourceModel`, `Type`, `Source` fields. Replace with command-only test cases.

Remove tests:

- `TestValidateRunTargets_SourceValidation` — rewrite for command-only validation
- `TestValidateRunTargets_ModelSource` — delete entirely
- `TestSplitRunTargets` — delete entirely
- `TestNormalizeRunTargets` — delete entirely
- Any `TestMergeDetectedRunTargets` — delete entirely

Update `TestValidateQuickLaunch_CommandTargetWithPrompt` and `TestValidateRunTargetDependencies` to use RunTarget structs without Type/Source.

**Step 2: Fix config_test.go**

- Remove `TestGetDetectedRunTarget` test
- Remove `TestGetDetectedRunTargets` test
- Remove `TestDetectedToolsFromConfig` test
- Remove `TestMergeDetectedRunTargets` test
- Update all RunTarget fixtures to remove `Type` and `Source` fields
- Update JSON config fixtures in TestLoadConfig tests to not include `"type"` and `"source"` in run_targets

**Step 3: Fix remaining test files**

For each file, remove `Type:` and `Source:` from RunTarget struct literals, remove `config.RunTargetTypePromptable` references, replace with simple `{Name: "x", Command: "y"}`.

**Step 4: Run all Go tests**

Run: `go test ./... -count=1`
Expected: ALL PASS

**Step 5: Commit**

Message: `test: update all Go tests for command-only run targets`

---

### Task 6: Regen Types and Fix Frontend Types

**Files:**

- Run: `go run ./cmd/gen-types`
- Modify: `assets/dashboard/src/lib/types.ts` — update RunTargetResponse
- Modify: `assets/dashboard/src/lib/types.generated.ts` — auto-generated

**Step 1: Regen types**

Run: `go run ./cmd/gen-types`
Expected: `types.generated.ts` now has RunTarget with only `name` and `command`.

**Step 2: Update RunTargetResponse in types.ts**

In `assets/dashboard/src/lib/types.ts`, change lines 78-83:

```typescript
// Before
export interface RunTargetResponse {
  name: string;
  type: string;
  command: string;
  source?: string;
}

// After
export interface RunTargetResponse {
  name: string;
  command: string;
}
```

**Step 3: Commit**

Message: `refactor: regen types for command-only run targets`

---

### Task 7: Delete Frontend Ghost State

**Files:**

- Modify: `assets/dashboard/src/routes/config/useConfigForm.ts`
- Modify: `assets/dashboard/src/routes/ConfigPage.tsx`
- Modify: `assets/dashboard/src/routes/config/TargetSelect.tsx`
- Modify: `assets/dashboard/src/routes/config/AdvancedTab.tsx`
- Modify: `assets/dashboard/src/routes/config/CodeReviewTab.tsx`
- Modify: `assets/dashboard/src/routes/config/FloorManagerTab.tsx`
- Modify: `assets/dashboard/src/routes/config/QuickLaunchTab.tsx`

**Step 1: Delete detectedTargets from useConfigForm.ts**

- Delete `detectedTargets: RunTargetResponse[];` from ConfigFormState type (line 98)
- Delete `detectedTargets: [],` from initial state (line 245)
- In `modelTargetNames` computation (lines 478-481), delete the detectedTargets spread:

```typescript
// Before
const modelTargetNames = new Set([
  ...state.detectedTargets.map((target) => target.name),
  ...state.models.filter((model) => model.configured).map((model) => model.id),
]);

// After
const modelTargetNames = new Set(
  state.models.filter((model) => model.configured).map((model) => model.id)
);
```

**Step 2: Delete detectedTargets from ConfigPage.tsx**

- Delete the filtering logic (lines 112-118 and line 132) — the `detectedItems`, `modelRunnerTools`, `filteredDetectedItems` variables and the `detectedTargets: filteredDetectedItems` assignment
- Delete `detectedTargets={state.detectedTargets}` from all 4 tab props:
  - Line 1208 (QuickLaunchTab)
  - Line 1234 (CodeReviewTab)
  - Line 1247 (FloorManagerTab)
  - Line 1322 (AdvancedTab)

**Step 3: Delete detectedTargets from TargetSelect.tsx**

Remove `detectedTargets` prop and the "Detected Tools" optgroup:

```tsx
// After
type TargetSelectProps = {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  includeDisabledOption?: boolean;
  includeNoneOption?: string;
  models: Model[];
  className?: string;
};

export default function TargetSelect({
  value,
  onChange,
  disabled,
  includeDisabledOption = true,
  includeNoneOption,
  models,
  className = 'input',
}: TargetSelectProps) {
  return (
    <select
      className={className}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      disabled={disabled}
    >
      {includeDisabledOption && <option value="">Disabled</option>}
      {includeNoneOption && <option value="">{includeNoneOption}</option>}
      <optgroup label="Models">
        {models
          .filter((model) => model.configured)
          .map((model) => (
            <option key={model.id} value={model.id}>
              {model.display_name}
            </option>
          ))}
      </optgroup>
    </select>
  );
}
```

**Step 4: Delete detectedTargets prop from all tab components**

In AdvancedTab.tsx, CodeReviewTab.tsx, FloorManagerTab.tsx, QuickLaunchTab.tsx:

- Remove `detectedTargets: RunTargetResponse[]` from props type
- Remove `detectedTargets,` from destructuring
- Remove `detectedTargets={detectedTargets}` from every TargetSelect usage

In AdvancedTab.tsx and CodeReviewTab.tsx, update help text:

- "Select a promptable target" → "Select a model"
- "not available or not promptable" → "not available"

In QuickLaunchTab.tsx, remove the `...detectedTargets.map(...)` spread from the target options arrays.

**Step 5: Attempt build**

Run: `go run ./cmd/build-dashboard`
Expected: Build succeeds (or shows remaining TypeScript errors to fix)

**Step 6: Commit**

Message: `refactor(dashboard): delete detectedTargets ghost state from config UI`

---

### Task 8: Fix SpawnPage Model Derivation

**Files:**

- Modify: `assets/dashboard/src/routes/SpawnPage.tsx`

**Step 1: Delete promptableTargets roundtrip**

- Delete `promptableTargets` state (line 154)
- Delete `PromptableListItem` type (lines 386-389)
- Delete run_targets filtering logic (lines 258-268)
- Delete `setPromptableTargets(promptableItems)` call

**Step 2: Replace promptableList derivation**

Replace the useMemo (lines 391-397):

```tsx
// Before
const promptableList = useMemo<PromptableListItem[]>(() => {
  const modelLabels = new Map(models.map((model) => [model.id, model.display_name]));
  return promptableTargets.map((target) => ({
    name: target.name,
    label: modelLabels.get(target.name) || target.name,
  }));
}, [models, promptableTargets]);

// After
const promptableList = useMemo(() => {
  const enabled = config?.enabled_models || {};
  const hasExplicit = Object.keys(enabled).length > 0;
  return models
    .filter((m) => (hasExplicit ? m.id in enabled : m.configured))
    .map((m) => ({ name: m.id, label: m.display_name }));
}, [models, config]);
```

**Step 3: Verify build**

Run: `go run ./cmd/build-dashboard`
Expected: PASS

**Step 4: Commit**

Message: `refactor(dashboard): derive spawn page models from catalog instead of run_targets bridge`

---

### Task 9: Fix Scattered Promptable Checks

**Files:**

- Modify: `assets/dashboard/src/components/AppShell.tsx`
- Modify: `assets/dashboard/src/components/SessionTabs.tsx`
- Modify: `assets/dashboard/src/routes/SessionDetailPage.tsx`

**Step 1: Fix AppShell.tsx**

Around line 901, replace:

```tsx
// Before
const runTarget = (config?.run_targets || []).find((t) => t.name === sess.target);
const isPromptable = runTarget ? runTarget.type === 'promptable' : true;

// After
const isCommand = (config?.run_targets || []).some((t) => t.name === sess.target);
const isPromptable = !isCommand;
```

**Step 2: Fix SessionTabs.tsx**

Around line 272, same pattern:

```tsx
// Before
const runTarget = (config?.run_targets || []).find((t) => t.name === sess.target);
const isPromptable = runTarget ? runTarget.type === 'promptable' : true;

// After
const isCommand = (config?.run_targets || []).some((t) => t.name === sess.target);
const isPromptable = !isCommand;
```

**Step 3: Fix SessionDetailPage.tsx**

Lines 248 and 301, replace:

```tsx
// Before
const promptable = config.run_targets?.find((t) => t.type === 'promptable');

// After
const firstModel = config.models?.find((m) => config.enabled_models?.[m.id]);
```

And use `firstModel?.id` instead of `promptable?.name`.

**Step 4: Build and verify**

Run: `go run ./cmd/build-dashboard`
Expected: PASS

**Step 5: Commit**

Message: `refactor(dashboard): replace type==='promptable' checks with command target exclusion`

---

### Task 10: Fix All Frontend Tests

**Files:**

- Modify: `assets/dashboard/src/routes/config/TargetSelect.test.tsx`
- Modify: `assets/dashboard/src/routes/config/AdvancedTab.test.tsx`
- Modify: `assets/dashboard/src/routes/config/CodeReviewTab.test.tsx`
- Modify: `assets/dashboard/src/routes/config/useConfigForm.test.ts`
- Modify: `assets/dashboard/src/routes/config/ConfigPage.test.tsx`
- Modify: `assets/dashboard/src/routes/config/ConfigModals.test.tsx`
- Modify: `assets/dashboard/src/routes/config/SessionsTab.test.tsx`
- Modify: `assets/dashboard/src/routes/SpawnPage.agent-select.test.tsx`
- Modify: `assets/dashboard/src/demo/mockTransport.test.ts`

**Step 1: Fix all test fixtures**

Remove `type`, `source` fields from all RunTargetResponse fixtures.
Remove `detectedTargets` prop from all test renders.
Remove "Detected Tools" optgroup assertions from TargetSelect tests.
Update SpawnPage agent-select tests to use models instead of promptable run_targets.
Remove `detectedTargets` from useConfigForm state assertions.

**Step 2: Run frontend tests**

Run: `cd assets/dashboard && npx vitest run`
Expected: ALL PASS

**Step 3: Commit**

Message: `test: update all frontend tests for command-only run targets`

---

### Task 11: Full Regression

**Step 1: Run Go tests**

Run: `go test ./... -count=1`
Expected: ALL PASS

**Step 2: Run frontend tests**

Run: `cd assets/dashboard && npx vitest run`
Expected: ALL PASS

**Step 3: Build dashboard**

Run: `go run ./cmd/build-dashboard`
Expected: PASS

**Step 4: Run full quick suite**

Run: `./test.sh --quick`
Expected: ALL PASS

**Step 5: Commit any remaining fixes**

If any test needed fixing, commit with: `fix: address remaining test failures from bridge removal`
