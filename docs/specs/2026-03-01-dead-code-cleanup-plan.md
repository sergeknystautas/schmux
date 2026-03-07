# Dead Code Cleanup Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove all remnant bridge code (isPromptableTarget, validateRunTargetDependencies, vestigial optgroups, broken tests, wrong variable names) left over from the model config editor and kill-bridge refactors.

**Architecture:** Three backend changes (gut config validation, simplify IsModel, update call site) and three frontend changes (flatten TargetSelect, rename promptableList, fix tests). Each task is independent after Task 1.

**Tech Stack:** Go (backend validation), React/TypeScript (frontend components), Vitest (frontend tests)

---

## Context

Read these files before starting any task:

- `docs/specs/2026-03-01-dead-code-cleanup-design.md` — the design this plan implements
- `internal/config/run_targets.go` — validation functions being simplified (148 lines)
- `internal/config/run_targets_test.go` — tests being updated (225 lines)
- `internal/config/config.go:747-756` — validate() call site
- `internal/models/manager.go:267-282` — IsModel() being simplified
- `assets/dashboard/src/routes/config/TargetSelect.tsx` — component being flattened (41 lines)
- `assets/dashboard/src/routes/config/TargetSelect.test.tsx` — broken test (63 lines)
- `assets/dashboard/src/routes/SpawnPage.tsx:362-368` — promptableList being renamed

---

### Task 1: Gut Config Validation — Remove isPromptableTarget and Dependencies

Strip `run_targets.go` down to structural-only validation. Delete `isPromptableTarget()`, `validateRunTargetDependencies()`, `validateQuickLaunchTargets()`. Simplify `validateQuickLaunch()`, `validateNudgenikConfig()`, `validateCompoundConfig()` to only check structural integrity (non-empty names, no duplicates, non-empty targets). Remove the `targets []RunTarget` parameter from all of them.

**Files:**

- Modify: `internal/config/run_targets.go` (full rewrite)
- Modify: `internal/config/config.go:747-756` (update call site)
- Modify: `internal/config/run_targets_test.go` (delete/update tests)
- Modify: `internal/config/config_test.go:1981-1998` (delete target-not-found and promptable test cases)

**Step 1: Rewrite `run_targets.go`**

Replace the entire file with:

```go
package config

import (
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/internal/detect"
)

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

func validateQuickLaunch(presets []QuickLaunch) error {
	seen := make(map[string]struct{})

	for _, preset := range presets {
		name := strings.TrimSpace(preset.Name)
		if name == "" {
			return fmt.Errorf("%w: quick launch name is required", ErrInvalidConfig)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("%w: duplicate quick launch name: %s", ErrInvalidConfig, name)
		}
		targetName := strings.TrimSpace(preset.Target)
		if targetName == "" {
			return fmt.Errorf("%w: quick launch target is required for %s", ErrInvalidConfig, name)
		}

		seen[name] = struct{}{}
	}
	return nil
}

func validateNudgenikConfig(nudgenik *NudgenikConfig) error {
	if nudgenik == nil {
		return nil
	}
	targetName := strings.TrimSpace(nudgenik.Target)
	if targetName == "" {
		return nil
	}
	return nil
}

func validateCompoundConfig(compound *CompoundConfig) error {
	if compound == nil {
		return nil
	}
	targetName := strings.TrimSpace(compound.Target)
	if targetName == "" {
		return nil
	}
	return nil
}
```

**Step 2: Update `config.go` call site**

In `internal/config/config.go`, find lines 747-756 and replace:

```go
	if err := validateRunTargets(c.RunTargets); err != nil {
		return nil, err
	}
	if err := validateQuickLaunch(c.QuickLaunch, c.RunTargets); err != nil {
		return nil, err
	}
	if err := validateRunTargetDependencies(c.RunTargets, c.QuickLaunch, c.Nudgenik, c.Compound); err != nil {
		return nil, err
	}
```

with:

```go
	if err := validateRunTargets(c.RunTargets); err != nil {
		return nil, err
	}
	if err := validateQuickLaunch(c.QuickLaunch); err != nil {
		return nil, err
	}
	if err := validateNudgenikConfig(c.Nudgenik); err != nil {
		return nil, err
	}
	if err := validateCompoundConfig(c.Compound); err != nil {
		return nil, err
	}
```

**Step 3: Rewrite `run_targets_test.go`**

Replace the entire file with:

```go
package config

import (
	"strings"
	"testing"
)

func TestValidateRunTargets(t *testing.T) {
	tests := []struct {
		name         string
		targets      []RunTarget
		wantErr      bool
		wantContains string
	}{
		{
			name: "valid command target",
			targets: []RunTarget{
				{Name: "my-script", Command: "bash run.sh"},
			},
			wantErr: false,
		},
		{
			name: "empty name",
			targets: []RunTarget{
				{Name: "", Command: "echo hi"},
			},
			wantErr:      true,
			wantContains: "name is required",
		},
		{
			name: "empty command",
			targets: []RunTarget{
				{Name: "my-agent", Command: ""},
			},
			wantErr:      true,
			wantContains: "command is required",
		},
		{
			name: "duplicate names",
			targets: []RunTarget{
				{Name: "agent", Command: "echo a"},
				{Name: "agent", Command: "echo b"},
			},
			wantErr:      true,
			wantContains: "duplicate run target name",
		},
		{
			name: "name collides with builtin tool",
			targets: []RunTarget{
				{Name: "claude", Command: "echo hi"},
			},
			wantErr:      true,
			wantContains: "collides with detected tool",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRunTargets(tt.targets)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantContains)
				}
				if !strings.Contains(err.Error(), tt.wantContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateQuickLaunch(t *testing.T) {
	prompt := "do something"
	tests := []struct {
		name         string
		presets      []QuickLaunch
		wantErr      bool
		wantContains string
	}{
		{
			name: "valid quick launch",
			presets: []QuickLaunch{
				{Name: "preset", Target: "claude", Prompt: &prompt},
			},
			wantErr: false,
		},
		{
			name: "empty name",
			presets: []QuickLaunch{
				{Name: "", Target: "claude", Prompt: &prompt},
			},
			wantErr:      true,
			wantContains: "name is required",
		},
		{
			name: "duplicate names",
			presets: []QuickLaunch{
				{Name: "preset", Target: "claude", Prompt: &prompt},
				{Name: "preset", Target: "codex", Prompt: &prompt},
			},
			wantErr:      true,
			wantContains: "duplicate quick launch name",
		},
		{
			name: "empty target",
			presets: []QuickLaunch{
				{Name: "preset", Target: "", Prompt: &prompt},
			},
			wantErr:      true,
			wantContains: "target is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateQuickLaunch(tt.presets)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantContains)
				}
				if !strings.Contains(err.Error(), tt.wantContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
```

**Step 4: Update `config_test.go` — delete dead test cases**

In `internal/config/config_test.go`, find `TestValidate_NegativeCases` and delete these two test cases (lines ~1981-1998):

1. `"target not found in quick launch"` — we no longer validate target existence at config time
2. `"promptable target without prompt in quick launch"` — we no longer validate promptability at config time

**Step 5: Run Go tests for config package**

Run: `go test ./internal/config/ -v -run "TestValidate|TestValidateRunTargets|TestValidateQuickLaunch"`
Expected: ALL PASS

**Step 6: Run full Go tests**

Run: `go test ./...`
Expected: ALL PASS (no other package calls the deleted functions)

**Step 7: Commit**

Message: `refactor(config): remove isPromptableTarget and config-time target existence checks`

---

### Task 2: Simplify `models.Manager.IsModel()` — Remove RunTarget Fallback

> **Status: NOT YET IMPLEMENTED.** The `GetRunTarget` fallback still exists in `manager.go:290`.

Remove the `GetRunTarget` check from `IsModel()`. This method should only answer "is this a model or builtin tool?" — it should not check command targets. Callers (`handlers_spawn.go`, `workspace/config.go`) already handle the "not found" case.

**Files:**

- Modify: `internal/models/manager.go:267-282`

**Step 1: Simplify IsModel()**

In `internal/models/manager.go`, replace lines 267-282:

```go
// IsModel returns whether the named target is a model (or detected tool) that
// accepts prompts, and whether it exists at all. Models and detected tools are
// promptable; user-defined targets are always commands.
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

with:

```go
// IsModel returns whether the named target is a model or detected tool that
// accepts prompts. Returns (true, true) for model IDs and builtin tool names,
// (false, false) otherwise. Does not check command targets — callers handle
// that separately via config.GetRunTarget().
func (m *Manager) IsModel(name string) (promptable bool, found bool) {
	if m.IsModelID(name) {
		_, err := m.ResolveModel(name)
		return true, err == nil
	}
	if detect.IsBuiltinToolName(name) {
		return true, true
	}
	return false, false
}
```

**Step 2: Check callers still work**

Search for all callers of `IsModel()` and verify they handle the `(false, false)` case for command targets. Key callers:

- `handlers_spawn.go:231` — calls `s.models.IsModel(targetName)`, then falls through to handle command targets. Verify the fallthrough path works when `found` is `false`.
- `workspace/config.go:162` — calls `mm.IsModel(target)`, uses result for prompt validation. Verify it handles non-model targets.

Run: `go test ./internal/models/ ./internal/dashboard/ ./internal/workspace/ -v`
Expected: ALL PASS

**Step 3: Run full Go tests**

Run: `go test ./...`
Expected: ALL PASS

**Step 4: Commit**

Message: `refactor(models): remove command target fallback from IsModel()`

---

### Task 3: Flatten TargetSelect — Remove Vestigial Optgroup

Remove the `<optgroup label="Models">` wrapper since there's only one group now. Fix the broken test that asserts filtering which no longer exists.

**Files:**

- Modify: `assets/dashboard/src/routes/config/TargetSelect.tsx:32-38`
- Modify: `assets/dashboard/src/routes/config/TargetSelect.test.tsx:32-36`

**Step 1: Flatten the select options**

In `TargetSelect.tsx`, replace lines 32-38:

```tsx
<optgroup label="Models">
  {models.map((model) => (
    <option key={model.id} value={model.id}>
      {model.display_name}
    </option>
  ))}
</optgroup>
```

with:

```tsx
{
  models.map((model) => (
    <option key={model.id} value={model.id}>
      {model.display_name}
    </option>
  ));
}
```

**Step 2: Fix the broken test**

In `TargetSelect.test.tsx`, replace lines 32-36:

```tsx
it('renders only configured models', () => {
  render(<TargetSelect value="" onChange={() => {}} models={models} />);
  expect(screen.getByText('GPT-4')).toBeInTheDocument();
  expect(screen.queryByText('Unconfigured')).not.toBeInTheDocument();
});
```

with:

```tsx
it('renders all models passed to it', () => {
  render(<TargetSelect value="" onChange={() => {}} models={models} />);
  expect(screen.getByText('GPT-4')).toBeInTheDocument();
  expect(screen.getByText('Unconfigured')).toBeInTheDocument();
});
```

**Step 3: Run frontend tests**

Run: `cd assets/dashboard && npx vitest run src/routes/config/TargetSelect.test.tsx`
Expected: ALL PASS

**Step 4: Commit**

Message: `refactor(dashboard): flatten TargetSelect, fix stale filtering test`

---

### Task 4: Rename `promptableList` to `availableModels` in SpawnPage

The name `promptableList` is a holdover from the bridge era. Rename to `availableModels` throughout SpawnPage.tsx.

**Files:**

- Modify: `assets/dashboard/src/routes/SpawnPage.tsx` (rename all occurrences)

**Step 1: Rename `promptableList` → `availableModels`**

Use find-and-replace across `SpawnPage.tsx` to rename every occurrence of `promptableList` to `availableModels`. There are ~20 occurrences (lines 362, 377, 381, 388, 395, 400, 403, 409, 414, 478, 1004, 1023, 1035, 1044, 1123, 1135, 1144, 1244, 1277).

**Step 2: Run frontend tests**

Run: `cd assets/dashboard && npx vitest run src/routes/SpawnPage`
Expected: ALL PASS

**Step 3: Run full frontend tests**

Run: `cd assets/dashboard && npx vitest run`
Expected: ALL PASS

**Step 4: Commit**

Message: `refactor(dashboard): rename promptableList to availableModels in SpawnPage`

---

### Task 5: Full Regression

Run everything to verify the cleanup didn't break anything.

**Step 1: Run Go tests**

Run: `go test ./...`
Expected: ALL PASS

**Step 2: Run frontend tests**

Run: `cd assets/dashboard && npx vitest run`
Expected: ALL PASS

**Step 3: Build dashboard**

Run: `go run ./cmd/build-dashboard`
Expected: Build succeeds

**Step 4: Run quick test suite**

Run: `./test.sh --quick`
Expected: ALL PASS

---

## Execution Notes

- Task 1 is the biggest — it touches 4 Go files but is self-contained within the config package.
- Task 2 is independent of Task 1 (different package). Can run in parallel.
- Tasks 3 and 4 are frontend-only and independent of each other. Can run in parallel.
- Task 5 is the final gate — run after all others complete.
- After this cleanup, any remaining bridge-era naming issues will be more visible for a second pass.
