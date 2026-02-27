# Model Mapping Manager Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the single-tool-per-model system with a multi-runner model catalog where users pick favorites and assign preferred tools.

**Architecture:** Add `Runners map[string]RunnerSpec` to Model, move `BuildEnv`/`ModelFlag` logic into adapters via `BuildRunnerEnv`, and add config `Enabled map[string]string` for user preferences. Model IDs become vendor-defined (e.g., `claude-opus-4-6` instead of `claude-opus`).

**Tech Stack:** Go (backend), React/TypeScript (dashboard), Go code generation for TS types

---

## Context

Read these files before starting any task:

- `docs/specs/2026-02-26-opencode-phase3-design.md` — the design this plan implements
- `internal/detect/models.go` — current Model struct and builtinModels catalog
- `internal/detect/adapter.go` — ToolAdapter interface
- `internal/session/manager.go:896-1035` — ResolveTarget and buildCommand
- `internal/dashboard/handlers_models.go` — API model handlers
- `internal/api/contracts/config.go:46-58` — Model API contract
- `internal/config/config.go:439-442` — ModelsConfig struct

The current Model struct has: `ID, DisplayName, BaseTool, Provider, Endpoint, ModelValue, ModelFlag, RequiredSecrets, UsageURL, Category`. Phase 3 replaces `BaseTool, ModelValue, ModelFlag, Endpoint, RequiredSecrets` with `Runners map[string]RunnerSpec`.

**Model IDs are vendor-defined facts.** The current "claude-opus", "claude-sonnet", "claude-haiku" IDs are tier aliases — not actual model IDs. Real IDs: `claude-opus-4-6`, `claude-sonnet-4-6`, `claude-haiku-4-5`. Third-party and Codex model IDs are already vendor-defined and stay the same.

---

### Task 1: Add RunnerSpec Type and BuildRunnerEnv Interface Method

Add the new types without removing anything. All existing code continues to compile and pass.

**Files:**

- Modify: `internal/detect/models.go`
- Modify: `internal/detect/adapter.go`
- Modify: `internal/detect/adapter_claude.go`
- Modify: `internal/detect/adapter_codex.go`
- Modify: `internal/detect/adapter_gemini.go`
- Modify: `internal/detect/adapter_opencode.go`
- Test: `internal/detect/models_test.go`

**Step 1: Write failing test for RunnerSpec and BuildRunnerEnv**

Add to `internal/detect/models_test.go`:

```go
func TestRunnerSpec(t *testing.T) {
	model := Model{
		ID: "test-model",
		Runners: map[string]RunnerSpec{
			"claude":   {ModelValue: "test-model"},
			"opencode": {ModelValue: "anthropic/test-model"},
		},
	}
	spec, ok := model.RunnerFor("claude")
	if !ok {
		t.Fatal("expected runner for claude")
	}
	if spec.ModelValue != "test-model" {
		t.Errorf("ModelValue = %q, want %q", spec.ModelValue, "test-model")
	}
	_, ok = model.RunnerFor("nonexistent")
	if ok {
		t.Error("expected no runner for nonexistent tool")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/detect/ -run TestRunnerSpec -v`
Expected: FAIL — `RunnerSpec` type not defined

**Step 3: Add RunnerSpec type, Runners field, and RunnerFor method**

In `internal/detect/models.go`, add:

```go
// RunnerSpec describes how a specific tool executes a model.
type RunnerSpec struct {
	ModelValue      string   // Value passed to the tool (e.g., "claude-opus-4-6", "anthropic/claude-opus-4-6")
	Endpoint        string   // API endpoint override (empty = tool's default)
	RequiredSecrets []string // Secrets needed when using THIS tool for THIS model
}

// RunnerFor returns the RunnerSpec for the given tool, if this model supports it.
func (m Model) RunnerFor(tool string) (RunnerSpec, bool) {
	if m.Runners == nil {
		return RunnerSpec{}, false
	}
	spec, ok := m.Runners[tool]
	return spec, ok
}
```

Add `Runners` field to `Model` struct (alongside existing fields — do not remove anything yet):

```go
type Model struct {
	// ... existing fields stay ...
	Runners map[string]RunnerSpec // tool name -> how to run this model with that tool
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/detect/ -run TestRunnerSpec -v`
Expected: PASS

**Step 5: Add BuildRunnerEnv to ToolAdapter interface**

In `internal/detect/adapter.go`, add to the `ToolAdapter` interface:

```go
// BuildRunnerEnv constructs environment variables for running a model with this tool.
BuildRunnerEnv(spec RunnerSpec) map[string]string
```

**Step 6: Implement BuildRunnerEnv on all 4 adapters**

`adapter_claude.go` — Claude sets proxy env vars when Endpoint is present:

```go
func (a *ClaudeAdapter) BuildRunnerEnv(spec RunnerSpec) map[string]string {
	env := map[string]string{}
	if spec.Endpoint != "" {
		env["ANTHROPIC_MODEL"] = spec.ModelValue
		env["ANTHROPIC_BASE_URL"] = spec.Endpoint
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = spec.ModelValue
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = spec.ModelValue
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = spec.ModelValue
		env["CLAUDE_CODE_SUBAGENT_MODEL"] = spec.ModelValue
	}
	return env
}
```

`adapter_codex.go`, `adapter_gemini.go`, `adapter_opencode.go` — return empty maps:

```go
func (a *CodexAdapter) BuildRunnerEnv(spec RunnerSpec) map[string]string {
	return map[string]string{}
}
```

(Same for Gemini and OpenCode adapters.)

**Step 7: Write test for BuildRunnerEnv**

Add to `internal/detect/models_test.go`:

```go
func TestBuildRunnerEnv(t *testing.T) {
	adapter := GetAdapter("claude")
	spec := RunnerSpec{
		ModelValue: "kimi-thinking",
		Endpoint:   "https://api.moonshot.ai/anthropic",
	}
	env := adapter.BuildRunnerEnv(spec)
	if env["ANTHROPIC_BASE_URL"] != "https://api.moonshot.ai/anthropic" {
		t.Errorf("ANTHROPIC_BASE_URL = %q", env["ANTHROPIC_BASE_URL"])
	}
	if env["ANTHROPIC_MODEL"] != "kimi-thinking" {
		t.Errorf("ANTHROPIC_MODEL = %q", env["ANTHROPIC_MODEL"])
	}

	// Native model (no endpoint) should return empty env
	nativeSpec := RunnerSpec{ModelValue: "claude-opus-4-6"}
	nativeEnv := adapter.BuildRunnerEnv(nativeSpec)
	if len(nativeEnv) != 0 {
		t.Errorf("expected empty env for native model, got %v", nativeEnv)
	}
}
```

**Step 8: Run all tests**

Run: `go test ./internal/detect/ -v`
Expected: ALL PASS

**Step 9: Commit**

```bash
git add internal/detect/models.go internal/detect/adapter.go \
  internal/detect/adapter_claude.go internal/detect/adapter_codex.go \
  internal/detect/adapter_gemini.go internal/detect/adapter_opencode.go \
  internal/detect/models_test.go
```

Commit message: `feat(detect): add RunnerSpec type and BuildRunnerEnv adapter method`

---

### Task 2: Populate Runners on All Builtin Models

Add `Runners` map to every entry in `builtinModels`. Keep old fields intact — this is additive only.

**Files:**

- Modify: `internal/detect/models.go`
- Test: `internal/detect/models_test.go`

**Step 1: Write failing test**

Add to `internal/detect/models_test.go`:

```go
func TestAllModelsHaveRunners(t *testing.T) {
	for _, m := range GetBuiltinModels() {
		if len(m.Runners) == 0 {
			t.Errorf("model %q has no Runners", m.ID)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/detect/ -run TestAllModelsHaveRunners -v`
Expected: FAIL — all models have empty Runners

**Step 3: Add Runners to every builtinModels entry**

In `internal/detect/models.go`, add `Runners` field to each model entry in `builtinModels`. Examples:

Native Claude models:

```go
{
	ID:          "claude-opus",
	// ... existing fields stay for now ...
	Runners: map[string]RunnerSpec{
		"claude":   {ModelValue: "claude-opus-4-6"},
		"opencode": {ModelValue: "anthropic/claude-opus-4-6"},
	},
},
```

Third-party models (e.g., kimi-thinking):

```go
{
	ID:          "kimi-thinking",
	// ... existing fields stay ...
	Runners: map[string]RunnerSpec{
		"claude":   {ModelValue: "kimi-thinking", Endpoint: "https://api.moonshot.ai/anthropic", RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"}},
		"opencode": {ModelValue: "moonshot/kimi-thinking"},
	},
},
```

Codex models:

```go
{
	ID:          "gpt-5.2-codex",
	// ... existing fields stay ...
	Runners: map[string]RunnerSpec{
		"codex": {ModelValue: "gpt-5.2-codex"},
	},
},
```

OpenCode-only model:

```go
{
	ID:          "opencode-zen",
	// ... existing fields stay ...
	Runners: map[string]RunnerSpec{
		"opencode": {ModelValue: ""},
	},
},
```

Apply the same pattern to ALL 16 models. Third-party models that use Claude's proxy API get both a `"claude"` runner (with Endpoint + RequiredSecrets) and an `"opencode"` runner (with provider/model syntax). Codex models only get a `"codex"` runner. Native Claude models get both `"claude"` and `"opencode"` runners.

**Step 4: Run test**

Run: `go test ./internal/detect/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/detect/models.go internal/detect/models_test.go
```

Commit message: `feat(detect): populate Runners on all builtin models`

---

### Task 3: Add Enabled Config and Model Resolution Helpers

Add the `Enabled` map to config and a helper that resolves which tool to use for a model.

**Files:**

- Modify: `internal/config/config.go` (ModelsConfig struct, getters/setters)
- Modify: `internal/detect/models.go` (new GetAvailableModelsMultiRunner function)
- Test: `internal/config/config_test.go`
- Test: `internal/detect/models_test.go`

**Step 1: Write failing test for config Enabled**

Add to `internal/config/config_test.go`:

```go
func TestModelsEnabled(t *testing.T) {
	cfg := &Config{}

	// Empty config returns nil
	if got := cfg.GetEnabledModels(); got != nil {
		t.Errorf("expected nil, got %v", got)
	}

	// Set enabled models
	enabled := map[string]string{"claude-opus-4-6": "claude", "kimi-thinking": "opencode"}
	cfg.SetEnabledModels(enabled)

	got := cfg.GetEnabledModels()
	if got["claude-opus-4-6"] != "claude" {
		t.Errorf("expected claude, got %q", got["claude-opus-4-6"])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestModelsEnabled -v`
Expected: FAIL — methods not defined

**Step 3: Add Enabled field and methods**

In `internal/config/config.go`, update `ModelsConfig`:

```go
type ModelsConfig struct {
	Versions map[string]string `json:"versions,omitempty"` // keep for now, removed later
	Enabled  map[string]string `json:"enabled,omitempty"`  // modelID -> preferred tool
}
```

Add getter/setter methods:

```go
func (c *Config) GetEnabledModels() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Models == nil || c.Models.Enabled == nil {
		return nil
	}
	return c.Models.Enabled
}

func (c *Config) SetEnabledModels(enabled map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Models == nil {
		c.Models = &ModelsConfig{}
	}
	c.Models.Enabled = enabled
}

// PreferredTool returns the user's preferred tool for a model, or empty string.
func (c *Config) PreferredTool(modelID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Models == nil || c.Models.Enabled == nil {
		return ""
	}
	return c.Models.Enabled[modelID]
}
```

**Step 4: Run test**

Run: `go test ./internal/config/ -run TestModelsEnabled -v`
Expected: PASS

**Step 5: Write test for multi-runner model availability**

Add to `internal/detect/models_test.go`:

```go
func TestGetAvailableModelsMultiRunner(t *testing.T) {
	// With only opencode detected, models with opencode runner should be available
	detected := []Tool{{Name: "opencode", Command: "opencode", Source: "PATH", Agentic: true}}
	available := GetAvailableModelsMultiRunner(detected)

	// Should include claude models (they have opencode runner) and opencode-zen
	foundClaude := false
	foundZen := false
	for _, m := range available {
		if m.ID == "claude-opus" { // will be claude-opus-4-6 after ID rename
			foundClaude = true
		}
		if m.ID == "opencode-zen" {
			foundZen = true
		}
	}
	if !foundClaude {
		t.Error("claude model should be available via opencode runner")
	}
	if !foundZen {
		t.Error("opencode-zen should be available")
	}
}
```

**Step 6: Implement GetAvailableModelsMultiRunner**

In `internal/detect/models.go`:

```go
// GetAvailableModelsMultiRunner returns models where at least one runner's tool is detected.
func GetAvailableModelsMultiRunner(detected []Tool) []Model {
	tools := make(map[string]bool, len(detected))
	for _, tool := range detected {
		tools[tool.Name] = true
	}

	var out []Model
	for _, m := range builtinModels {
		if len(m.Runners) == 0 {
			// Fall back to old BaseTool check for models not yet migrated
			if tools[m.BaseTool] {
				out = append(out, m)
			}
			continue
		}
		for toolName := range m.Runners {
			if tools[toolName] {
				out = append(out, m)
				break
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}
```

**Step 7: Run all tests**

Run: `go test ./internal/detect/ ./internal/config/ -v`
Expected: ALL PASS

**Step 8: Commit**

Commit message: `feat(config): add Enabled models config and multi-runner availability`

---

### Task 4: Rewrite ResolveTarget to Use Runners

Switch the session manager's model resolution from `BaseTool` to `Runners` + `PreferredTool`.

**Files:**

- Modify: `internal/session/manager.go:896-937` (ResolveTarget)
- Test: `internal/session/manager_test.go`

**Step 1: Write failing test for Runners-based resolution**

Add to `internal/session/manager_test.go` a test that spawns using a model with `Runners` populated and `Enabled` config set. The test should verify the correct tool is chosen.

```go
func TestResolveTarget_UsesRunners(t *testing.T) {
	// Set up a model with Runners populated
	// Set Enabled config to prefer opencode
	// Verify ResolveTarget picks opencode's command
}
```

The exact test setup depends on the existing test infrastructure in manager_test.go — read it to match the pattern.

**Step 2: Rewrite ResolveTarget**

Replace the `model.BaseTool` logic in `ResolveTarget` (lines 896-938) with:

```go
func (m *Manager) ResolveTarget(_ context.Context, targetName string) (ResolvedTarget, error) {
	model, ok := detect.FindModel(targetName)
	if ok {
		// Determine which tool to use
		toolName := m.resolveToolForModel(model)
		if toolName == "" {
			return ResolvedTarget{}, fmt.Errorf("no available runner for model %s", model.ID)
		}

		// Get the runner spec
		spec, hasSpec := model.RunnerFor(toolName)
		if !hasSpec {
			return ResolvedTarget{}, fmt.Errorf("model %s has no runner for tool %s", model.ID, toolName)
		}

		// Verify the tool is detected
		baseTarget, found := m.config.GetDetectedRunTarget(toolName)
		if !found {
			return ResolvedTarget{}, fmt.Errorf("model %s requires tool %s which is not available", model.ID, toolName)
		}

		// Load and verify secrets
		secrets, err := config.GetEffectiveModelSecrets(model)
		if err != nil {
			return ResolvedTarget{}, fmt.Errorf("failed to load secrets for model %s: %w", model.ID, err)
		}
		for _, key := range spec.RequiredSecrets {
			if strings.TrimSpace(secrets[key]) == "" {
				return ResolvedTarget{}, fmt.Errorf("model %s requires secret %s", model.ID, key)
			}
		}

		// Build env using the adapter
		adapter := detect.GetAdapter(toolName)
		env := adapter.BuildRunnerEnv(spec)
		env = mergeEnvMaps(env, secrets)

		return ResolvedTarget{
			Name:       model.ID,
			Kind:       TargetKindModel,
			Command:    baseTarget.Command,
			Promptable: true,
			Env:        env,
			Model:      &model,
		}, nil
	}

	// ... rest of method (user/detected targets) stays the same ...
}

// resolveToolForModel picks which tool to use for a model.
func (m *Manager) resolveToolForModel(model detect.Model) string {
	// 1. Check user preference
	if preferred := m.config.PreferredTool(model.ID); preferred != "" {
		if _, ok := model.RunnerFor(preferred); ok {
			return preferred
		}
	}

	// 2. Fall back to first detected runner
	detectedTools := config.DetectedToolsFromConfig(m.config)
	detected := make(map[string]bool, len(detectedTools))
	for _, t := range detectedTools {
		detected[t.Name] = true
	}

	// 3. Try runners in deterministic order
	for _, toolName := range sortedRunnerKeys(model.Runners) {
		if detected[toolName] {
			return toolName
		}
	}
	return ""
}
```

Note: You'll need a `sortedRunnerKeys` helper to ensure deterministic tool selection when no preference is set. Add it to `models.go`:

```go
func sortedRunnerKeys(runners map[string]RunnerSpec) []string {
	keys := make([]string, 0, len(runners))
	for k := range runners {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
```

**Important:** Keep the old `ensureModelSecrets` function working for backward compatibility. The new code uses `spec.RequiredSecrets` directly instead of `model.RequiredSecrets`, so the old function is no longer called here. Remove the call to `ensureModelSecrets` and inline the check as shown above.

**Step 3: Update buildCommand to use adapter flag**

In `internal/session/manager.go:956-1017`, the `buildCommand` function currently does:

```go
if model != nil && model.ModelFlag != "" {
	baseCommand = fmt.Sprintf("%s %s %s", baseCommand, model.ModelFlag, shellutil.Quote(model.ModelValue))
}
```

This must change because the adapter owns the model flag. However, the adapter's `InteractiveArgs` already injects the model flag. The issue is `buildCommand` is adding it a second time for non-interactive spawn.

Look at how `buildCommand` is called — if the command already has the model flag injected by `InteractiveArgs`/`OneshotArgs` (via `BuildCommandParts`), then this block is only for the direct-command path. Read the call sites carefully.

The approach: remove the `model.ModelFlag` block from `buildCommand`. The adapter's `InteractiveArgs`/`OneshotArgs` methods already handle flag injection. If there are paths where model args aren't injected by the adapter, update those paths to use the adapter.

**Step 4: Update adapter args to use RunnerSpec**

Each adapter's `InteractiveArgs` and `OneshotArgs` currently read `model.ModelFlag` and `model.ModelValue`:

```go
if model != nil && model.ModelFlag != "" && model.ModelValue != "" {
	return []string{model.ModelFlag, model.ModelValue}
}
```

Change to read from the adapter's own flag and the model's `RunnerFor(adapterName)`:

```go
// ClaudeAdapter
func (a *ClaudeAdapter) InteractiveArgs(model *Model, resume bool) []string {
	if resume {
		return []string{"--continue"}
	}
	if model != nil {
		if spec, ok := model.RunnerFor("claude"); ok && spec.ModelValue != "" {
			return []string{"--model", spec.ModelValue}
		}
	}
	return nil
}
```

Apply the same pattern to all 4 adapters:

- `ClaudeAdapter`: flag is `--model`
- `CodexAdapter`: flag is `-m`
- `GeminiAdapter`: flag is `--model` (check current adapter to confirm)
- `OpencodeAdapter`: flag is `--model`

**Step 5: Run all tests**

Run: `./test.sh --quick`
Expected: ALL PASS (some model tests may need updating — fix any that fail due to the new field structure)

**Step 6: Commit**

Commit message: `refactor(session): resolve models via Runners and adapter-owned flags`

---

### Task 5: Update Remaining Go Callers

Update oneshot, floormanager, ensure, tools.go, handlers, and config to use Runners.

**Files:**

- Modify: `internal/oneshot/oneshot.go:583-620`
- Modify: `internal/floormanager/manager.go:415-475`
- Modify: `internal/detect/tools.go:55-90`
- Modify: `internal/workspace/ensure/manager.go`
- Modify: `internal/config/run_targets.go`
- Modify: `internal/config/models.go`
- Modify: `internal/dashboard/handlers_models.go`
- Modify: `internal/dashboard/handlers_config.go`

**Step 1: Update oneshot.go**

The oneshot package has its own model resolution at lines 583-620. Apply the same Runners pattern as Task 4's ResolveTarget rewrite:

- Replace `model.BaseTool` with runner resolution
- Replace `model.BuildEnv()` with `adapter.BuildRunnerEnv(spec)`
- Replace `model.RequiredSecrets` check with `spec.RequiredSecrets`

**Step 2: Update floormanager**

`internal/floormanager/manager.go` at lines 415-475 has similar `model.ModelFlag` and `model.BaseTool` usage. Apply the same pattern.

**Step 3: Update tools.go**

`internal/detect/tools.go` has `GetBaseToolName()` which looks up `model.BaseTool`. Update to check `model.Runners` — return the first available runner tool name (or the preferred tool if config is accessible).

Actually, `GetBaseToolName` is a detect-package function without config access. It should look at `model.Runners` and return the first key (sorted). The caller in `session/manager.go` already handles resolution properly after Task 4, so `GetBaseToolName` may just need to return the first runner key from the sorted runners map.

**Step 4: Update ensure/manager.go**

`internal/workspace/ensure/manager.go` calls `GetBaseToolName` to determine which adapter to use for a workspace. Update these call sites to pass through the resolved tool name instead of re-resolving from the model.

**Step 5: Update config/run_targets.go**

Line 63 checks `target.Name == model.BaseTool`. Update to check if target name is in `model.Runners` keys.

**Step 6: Update config/models.go**

Replace `GetAvailableModels` wrapper to call `GetAvailableModelsMultiRunner`.

**Step 7: Update handlers_models.go**

`buildAvailableModels` constructs `contracts.Model` from `detect.Model`. Update to use the new fields. The `modelConfigured` function checks `model.RequiredSecrets` — update to check runner-specific secrets based on the preferred tool.

**Step 8: Update handlers_config.go**

Lines referencing `model.BaseTool` in config response building.

**Step 9: Run all tests**

Run: `./test.sh --quick`
Fix any compile errors or test failures.

**Step 10: Commit**

Commit message: `refactor: update all callers to use Runners instead of BaseTool`

---

### Task 6: Remove Old Model Fields and Legacy Code

Now that all callers use Runners, remove the old fields and dead code.

**Files:**

- Modify: `internal/detect/models.go`
- Modify: `internal/detect/models_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 1: Remove old fields from Model struct**

In `internal/detect/models.go`, remove these fields from the `Model` struct:

- `BaseTool`
- `Endpoint`
- `ModelValue`
- `ModelFlag`
- `RequiredSecrets`

**Step 2: Remove old field values from builtinModels**

Strip the old field assignments from every entry in `builtinModels`. Each entry should now only have: `ID, DisplayName, Provider, UsageURL, Category, Runners`.

**Step 3: Remove BuildEnv method**

Delete the `BuildEnv()` method from `Model`.

**Step 4: Remove modelAliases**

Delete the `modelAliases` map and remove the alias lookup from `FindModel` and `IsModelID`.

**Step 5: Remove version pinning**

In `internal/config/config.go`:

- Remove `Versions` field from `ModelsConfig` (keep `Enabled`)
- Remove `GetModelVersion()`, `GetModelVersions()`, `SetModelVersions()` methods

In `internal/config/config_test.go`:

- Remove `TestGetModelVersion`

In `internal/session/manager.go`:

- Remove the version override block: `if override := m.config.GetModelVersion(model.ID); override != "" { ... }`

**Step 6: Remove old GetAvailableModels**

Replace `GetAvailableModels` with `GetAvailableModelsMultiRunner` (rename to `GetAvailableModels`).

**Step 7: Fix all compile errors**

Run `go build ./...` and fix any remaining references to removed fields.

**Step 8: Update model tests**

Rewrite `TestBuildEnv` → test `BuildRunnerEnv` (Task 1 already added this).
Update `TestFindModel` to remove alias test cases.
Update `TestIsModelID` to remove alias test cases.
Update `TestGetAvailableModels` to use Runners-based expectations.
Update `TestOpencodeModelExists` to check Runners instead of BaseTool.
Update `TestGetBuiltinModels` to verify correct model count.

**Step 9: Run all tests**

Run: `./test.sh --quick`
Expected: ALL PASS

**Step 10: Commit**

Commit message: `refactor(detect): remove BaseTool, ModelValue, ModelFlag, BuildEnv, aliases, version pinning`

---

### Task 7: Update Model IDs to Vendor-Defined

Change the Claude model IDs from tier aliases to actual vendor model IDs. This is a separate task from removing old fields because it changes externally-visible identifiers.

**Files:**

- Modify: `internal/detect/models.go`
- Modify: `internal/detect/models_test.go`
- Modify: All test files referencing old model IDs

**Step 1: Rename Claude model IDs**

In `builtinModels`:

- `"claude-opus"` → `"claude-opus-4-6"`, DisplayName `"Claude Opus 4.6"`
- `"claude-sonnet"` → `"claude-sonnet-4-6"`, DisplayName `"Claude Sonnet 4.6"`
- `"claude-haiku"` → `"claude-haiku-4-5"`, DisplayName `"Claude Haiku 4.5"`

Also rename minimax to match vendor ID:

- `"minimax"` → `"minimax-m2.1"`, keep DisplayName `"MiniMax M2.1"`

**Step 2: Add migration aliases**

Create a `legacyIDMigrations` map for backward compatibility in state/config:

```go
var legacyIDMigrations = map[string]string{
	"claude-opus":   "claude-opus-4-6",
	"claude-sonnet": "claude-sonnet-4-6",
	"claude-haiku":  "claude-haiku-4-5",
	"opus":          "claude-opus-4-6",
	"sonnet":        "claude-sonnet-4-6",
	"haiku":         "claude-haiku-4-5",
	"minimax":       "minimax-m2.1",
	"minimax-m2.1":  "minimax-m2.1", // no-op, already correct
}
```

Add a `MigrateModelID` function:

```go
// MigrateModelID converts a legacy model ID to the current vendor-defined ID.
func MigrateModelID(id string) string {
	if newID, ok := legacyIDMigrations[id]; ok {
		return newID
	}
	return id
}
```

Update `FindModel` to try migration before lookup:

```go
func FindModel(id string) (Model, bool) {
	id = MigrateModelID(id)
	for _, m := range builtinModels {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}
```

**Step 3: Update all tests with old model IDs**

Search all test files for `"claude-opus"`, `"claude-sonnet"`, `"claude-haiku"` and update to the new IDs. Key files:

- `internal/detect/models_test.go`
- `internal/session/manager_test.go`
- `internal/detect/commands_test.go`
- `internal/detect/tools_test.go`
- `internal/oneshot/oneshot_test.go`
- `internal/workspace/ensure/manager_test.go`

**Step 4: Run all tests**

Run: `./test.sh --quick`
Expected: ALL PASS

**Step 5: Commit**

Commit message: `feat(detect): rename model IDs to vendor-defined values`

---

### Task 8: Update API Contracts and TypeScript Types

Update the API contract to reflect the new model structure and regenerate TypeScript types.

**Files:**

- Modify: `internal/api/contracts/config.go`
- Modify: `internal/dashboard/handlers_models.go`
- Modify: `internal/dashboard/handlers_config.go`
- Run: `go run ./cmd/gen-types` (regenerates `assets/dashboard/src/lib/types.generated.ts`)

**Step 1: Update contracts.Model**

In `internal/api/contracts/config.go`, update the Model struct:

```go
type Model struct {
	ID          string              `json:"id"`
	DisplayName string              `json:"display_name"`
	Provider    string              `json:"provider"`
	Category    string              `json:"category"`
	UsageURL    string              `json:"usage_url,omitempty"`
	Configured  bool                `json:"configured"`
	Runners     map[string]RunnerInfo `json:"runners"`
	PreferredTool string            `json:"preferred_tool,omitempty"`
}

type RunnerInfo struct {
	Available       bool     `json:"available"`
	RequiredSecrets []string `json:"required_secrets,omitempty"`
	Configured      bool     `json:"configured"`
}
```

Remove old fields: `BaseTool`, `RequiredSecrets`, `PinnedVersion`, `DefaultValue`.

**Step 2: Update ConfigResponse**

Remove `ModelVersions map[string]string` from ConfigResponse.
Add `EnabledModels map[string]string` to ConfigResponse.
Remove `ModelVersions` from ConfigUpdateRequest.
Add `EnabledModels` to ConfigUpdateRequest.

**Step 3: Update handlers_models.go buildAvailableModels**

Rewrite to build the new contract format. For each model, iterate its `Runners` to build `RunnerInfo` entries indicating which tools are available and configured.

**Step 4: Update handlers_config.go**

- Remove version pinning from config save/load.
- Add enabled models to config save/load.

**Step 5: Regenerate TypeScript types**

Run: `go run ./cmd/gen-types`
Verify: `assets/dashboard/src/lib/types.generated.ts` has the new Model interface.

**Step 6: Run Go tests**

Run: `go test ./internal/dashboard/ ./internal/api/... -v`
Expected: PASS (frontend tests will fail until Task 9)

**Step 7: Commit**

Commit message: `feat(api): update Model contract for multi-runner support`

---

### Task 9: Update React Frontend

Update all React components and tests that reference old model fields.

**Files:**

- Modify: `assets/dashboard/src/routes/SpawnPage.tsx`
- Modify: `assets/dashboard/src/routes/ConfigPage.tsx`
- Modify: `assets/dashboard/src/routes/config/useConfigForm.ts`
- Modify: `assets/dashboard/src/routes/config/AdvancedTab.tsx`
- Modify: `assets/dashboard/src/routes/config/SessionsTab.tsx`
- Modify: Test files for each of the above
- Modify: `pkg/cli/daemon_client.go` (CLI client Model struct)

**Step 1: Remove modelAliases from useConfigForm.ts**

Delete the `modelAliases` export (lines 12-17) and update all references (lines 587-617) to use the model ID directly — no alias normalization needed since IDs are now canonical.

**Step 2: Update SpawnPage.tsx**

Remove `base_tool` filtering logic (lines 255-261). The spawn page should use the new `runners` field to determine which targets are model-backed. If a target is a model runner, it shouldn't appear separately in the target list.

**Step 3: Update ConfigPage.tsx**

Remove `base_tool` filtering (lines 113-115). Same rationale.

**Step 4: Update AdvancedTab.tsx**

Remove the version pinning input (line 508 area with `default_value` placeholder). Version pinning is gone.

**Step 5: Update SessionsTab.tsx**

Remove `base: {model.base_tool}` display (line 87). Replace with preferred tool info from `preferred_tool` field.

**Step 6: Update pkg/cli/daemon_client.go**

Update the CLI client's Model struct (line 315 area) to match the new API contract.

**Step 7: Update all frontend test files**

Update test fixtures in:

- `SpawnPage.agent-select.test.tsx`
- `useConfigForm.test.ts`
- `TargetSelect.test.tsx`
- `AdvancedTab.test.tsx`
- `SessionsTab.test.tsx`

Remove `base_tool`, `pinned_version`, `default_value` from test model objects. Add `runners` and `preferred_tool`.

**Step 8: Build dashboard**

Run: `go run ./cmd/build-dashboard`
Expected: Build succeeds

**Step 9: Run all tests**

Run: `./test.sh --quick`
Expected: ALL PASS

**Step 10: Commit**

Commit message: `feat(dashboard): update frontend for multi-runner model system`

---

### Task 10: Migration and Full Regression

Add migration logic for existing config/state files that reference old model IDs, and run the full test suite.

**Files:**

- Modify: `internal/config/secrets.go` (migrate old model ID keys)
- Modify: `internal/state/state.go` (migrate session Target values)
- Modify: `internal/config/config.go` (migrate quick launch, nudgenik, etc.)
- Test: migration tests

**Step 1: Add secrets migration**

In `internal/config/secrets.go`, after loading secrets in `LoadSecretsFile`, migrate old model ID keys:

```go
func migrateSecretKeys(secrets *SecretsFile) bool {
	changed := false
	for oldID, newID := range detect.LegacyIDMigrations() {
		if oldID == newID { continue }
		if s, ok := secrets.Models[oldID]; ok {
			if _, exists := secrets.Models[newID]; !exists {
				secrets.Models[newID] = s
			}
			delete(secrets.Models, oldID)
			changed = true
		}
	}
	return changed
}
```

Call this from `LoadSecretsFile` and auto-save if changed.

**Step 2: Add config migration**

In config loading, migrate model IDs in quick launch presets, nudgenik target, branch suggest target, conflict resolve target, PR review target, and commit message target.

**Step 3: Add state migration**

In `internal/state/state.go`, migrate `Session.Target` values that are old model IDs.

**Step 4: Write migration tests**

Test that old model IDs in secrets, config, and state are correctly migrated to new IDs.

**Step 5: Run full test suite**

Run: `./test.sh`
Expected: ALL PASS

**Step 6: Commit**

Commit message: `feat(config): migrate legacy model IDs to vendor-defined values`

---

## Execution Notes

- The plan preserves compilation at every commit — old fields coexist with new until Task 6 removes them
- Tasks 1-3 are purely additive (no breaking changes)
- Tasks 4-5 switch callers to new paths while old fields still exist
- Task 6 removes dead code
- Task 7 renames IDs (separate from field removal to isolate breakage)
- Tasks 8-9 update the API surface and frontend
- Task 10 handles migration
- Run `./test.sh --quick` after every commit; run `./test.sh` for the final commit
- Use `/commit` for all commits (enforces definition of done)
