# Phase 3: Model Mapping Manager

## Goal

Replace the current multi-layered model gating (tool detection → secrets → hardcoded catalog) with a model mapping manager. Users see all possible models, pick favorites, and assign a preferred tool per model. The spawn form stays unchanged — pick a model, that's it.

## Problem

The current system conflates models with their runners:

- `BaseTool` on `Model` ties each model to exactly one CLI tool
- `ModelFlag` on `Model` is actually a tool property (claude always uses `--model`, codex always uses `-m`)
- `BuildEnv()` on `Model` contains Claude-specific env var logic (`ANTHROPIC_BASE_URL`, tier overrides)
- `RequiredSecrets` on `Model` assumes one runner — but running `claude-sonnet-4-6` through opencode requires no secrets (opencode manages its own auth), while running it through claude requires nothing, and running `kimi-thinking` through claude requires `ANTHROPIC_AUTH_TOKEN`
- `modelAliases` maps tier shorthand ("opus" → "claude-opus") that obscures actual vendor model IDs
- `GetAvailableModels()` filters by detected `BaseTool`, so models are invisible if their one assigned tool isn't installed

## Design

### Model Struct

Replace `BaseTool`, `ModelValue`, `ModelFlag`, `Endpoint`, and `RequiredSecrets` with a flat `Runners` map:

```go
type Model struct {
    ID          string                  // Vendor-defined. e.g., "claude-opus-4-6", "gpt-5.2-codex"
    DisplayName string                  // e.g., "Claude Opus 4.6", "GPT 5.2 Codex"
    Provider    string                  // e.g., "anthropic", "openai", "moonshot"
    UsageURL    string                  // Signup/pricing page
    Category    string                  // "native" or "third-party"
    Runners     map[string]RunnerSpec   // tool name → how to run this model with that tool
}
```

### RunnerSpec

Each entry in `Runners` describes how a specific tool executes this model:

```go
type RunnerSpec struct {
    ModelValue      string   // Value passed to the tool (e.g., "claude-opus-4-6", "anthropic/claude-opus-4-6")
    Endpoint        string   // API endpoint override (empty = tool's default)
    RequiredSecrets []string // Secrets needed when using THIS tool for THIS model
}
```

`ModelFlag` is deliberately absent — the adapter owns its flag (`claude` → `--model`, `codex` → `-m`, `opencode` → `--model`). The adapter reads `RunnerSpec.ModelValue` and applies its own flag.

### Catalog Examples

```go
// Native Claude model — runnable by claude natively, or by opencode via provider syntax
{
    ID:          "claude-opus-4-6",
    DisplayName: "Claude Opus 4.6",
    Provider:    "anthropic",
    Category:    "native",
    Runners: map[string]RunnerSpec{
        "claude":   {ModelValue: "claude-opus-4-6"},
        "opencode": {ModelValue: "anthropic/claude-opus-4-6"},
    },
},

// Third-party model — currently only runnable via claude with proxy endpoint
{
    ID:          "kimi-thinking",
    DisplayName: "Kimi K2 Thinking",
    Provider:    "moonshot",
    UsageURL:    "https://platform.moonshot.ai/console/account",
    Category:    "third-party",
    Runners: map[string]RunnerSpec{
        "claude":   {ModelValue: "kimi-thinking", Endpoint: "https://api.moonshot.ai/anthropic", RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"}},
        "opencode": {ModelValue: "moonshot/kimi-thinking"},
    },
},

// Codex model — only runnable by codex
{
    ID:          "gpt-5.2-codex",
    DisplayName: "GPT 5.2 Codex",
    Provider:    "openai",
    Category:    "native",
    Runners: map[string]RunnerSpec{
        "codex": {ModelValue: "gpt-5.2-codex"},
    },
},

// OpenCode-only model
{
    ID:          "opencode-zen",
    DisplayName: "OpenCode Zen (free)",
    Provider:    "opencode-zen",
    Category:    "native",
    Runners: map[string]RunnerSpec{
        "opencode": {ModelValue: ""},
    },
},
```

### BuildEnv Moves to Adapter

`Model.BuildEnv()` is deleted. The adapter gains a new method:

```go
type ToolAdapter interface {
    // ... existing methods ...

    // BuildRunnerEnv constructs environment variables for running a model with this tool.
    BuildRunnerEnv(spec RunnerSpec) map[string]string
}
```

The Claude adapter's implementation handles the proxy env vars:

```go
func (a *ClaudeAdapter) BuildRunnerEnv(spec RunnerSpec) map[string]string {
    env := map[string]string{}
    if spec.Endpoint != "" {
        env["ANTHROPIC_BASE_URL"] = spec.Endpoint
        env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = spec.ModelValue
        env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = spec.ModelValue
        env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = spec.ModelValue
        env["CLAUDE_CODE_SUBAGENT_MODEL"] = spec.ModelValue
    }
    return env
}
```

Other adapters return empty maps or tool-specific env.

### ModelFlag Moves to Adapter

The adapter owns its model flag. The `InteractiveArgs`, `OneshotArgs`, etc. methods already receive a `*Model` — they will be updated to receive `RunnerSpec` (or the resolved model value) and inject the adapter's own flag:

```go
func (a *ClaudeAdapter) InteractiveArgs(model *Model, resume bool) []string {
    if resume {
        return []string{"--continue"}
    }
    if model != nil {
        spec := model.RunnerFor("claude")
        if spec != nil && spec.ModelValue != "" {
            return []string{"--model", spec.ModelValue}  // claude always uses --model
        }
    }
    return nil
}
```

### Config: Enabled Models

New config section for user preferences:

```go
type ModelsConfig struct {
    Enabled map[string]string `json:"enabled"` // model ID → preferred tool name
}
```

When `Enabled` is empty/nil, all models with a detected runner are shown (backward compat). When populated, only enabled models appear in the spawn form, using the specified tool.

Example `~/.schmux/config.json`:

```json
{
  "models": {
    "enabled": {
      "claude-opus-4-6": "claude",
      "claude-sonnet-4-6": "opencode",
      "kimi-thinking": "opencode",
      "gpt-5.2-codex": "codex"
    }
  }
}
```

### ResolveTarget Changes

Current flow:

1. `FindModel(targetName)` → model with `BaseTool`
2. Verify `BaseTool` is detected
3. Get secrets, call `model.BuildEnv()`
4. Return resolved target with `BaseTool`'s command

New flow:

1. `FindModel(targetName)` → model with `Runners`
2. Determine tool: if config has `Enabled[model.ID]`, use that tool; otherwise pick first detected runner
3. Look up `RunnerSpec` from `model.Runners[toolName]`
4. Verify tool is detected, load secrets for `spec.RequiredSecrets`
5. Get adapter, call `adapter.BuildRunnerEnv(spec)`
6. Return resolved target with the chosen tool's command

### GetAvailableModels Changes

Current: filters by `BaseTool` being detected.

New: a model is available if ANY of its runners' tools are detected AND (if runner has `RequiredSecrets`) those secrets are present. The preferred tool comes from config `Enabled` or defaults to the first available runner.

### Secrets

Secrets remain per-model in `~/.schmux/secrets.json`. The key difference: `RequiredSecrets` is now per-runner, so the same model may need secrets for one tool but not another. The secrets UI shows requirements based on the user's chosen tool for that model.

### Remove modelAliases

The tier aliases ("opus" → "claude-opus", "sonnet" → "claude-sonnet") are removed. Model IDs are vendor-defined (e.g., `claude-opus-4-6`). Any existing config referencing old aliases gets a one-time migration.

### Remove Version Pinning

`GetModelVersion()`, `SetModelVersions()`, and the `PinnedVersion` field in the API contract are removed. Each model entry in the catalog is specific — users pick the one they want.

## Settings UI

The settings page gets a "Models" section:

- Shows the full catalog grouped by provider
- Each model shows which tools can run it (based on detection)
- User toggles favorites on/off (populates `Enabled` map)
- For models with multiple runners, user picks preferred tool
- When no favorites are set, all detected models show in spawn form

## What This Removes

- `Model.BaseTool` — replaced by `Runners` map
- `Model.ModelValue` — moved into `RunnerSpec`
- `Model.ModelFlag` — adapter-owned
- `Model.Endpoint` — moved into `RunnerSpec`
- `Model.RequiredSecrets` — moved into `RunnerSpec`
- `Model.BuildEnv()` — moved to `adapter.BuildRunnerEnv()`
- `modelAliases` — removed entirely
- `GetModelVersion()` / `SetModelVersions()` — removed
- Version pinning config and API fields — removed

## What This Adds

- `RunnerSpec` struct
- `Runners map[string]RunnerSpec` on `Model`
- `BuildRunnerEnv(RunnerSpec)` on `ToolAdapter`
- `ModelsConfig.Enabled` in config
- Settings UI for model catalog management
- Migration logic for old aliases and config format

## Migration

1. Old model IDs in state/config ("claude-opus", "claude-sonnet") map to new vendor IDs via a one-time migration
2. Existing `secrets.json` keys migrate from old IDs to new IDs
3. Quick launch presets and nudgenik targets referencing old IDs get migrated
