# Spec: Codex Model Support via CLI Flags

**Status:** Implemented
**Created:** 2026-02-06
**Completed:** 2026-02-06

## Summary

Add support for Codex-specific models that use command-line flags (`-m <model_id>`) instead of environment variables for model selection. This enables spawning sessions with native Codex models like `gpt-5.2-codex`, `gpt-5.3-codex`, `gpt-5.1-codex-max`, and `gpt-5.1-codex-mini`.

## Motivation

Currently, **all models** use environment variables (`ANTHROPIC_MODEL`) for model selection. This works for Claude Code and Anthropic-compatible third-party providers, but Codex uses a different mechanism:

- **Claude Code**: `ANTHROPIC_MODEL=claude-sonnet-4-5-20250929 claude 'prompt'`
- **Codex**: `codex -m gpt-5.2-codex 'prompt'`

Codex doesn't support model selection via environment variables - it requires the `-m` CLI flag. To support Codex-specific models, we need to:

1. Add a mechanism to specify that a model uses CLI flags instead of env vars
2. Inject the CLI flag into commands for both interactive and oneshot modes
3. Define Codex-specific models in the builtin models list

## Codex Models to Add

| ID | Display Name | Model Value |
|----|--------------|-------------|
| `gpt-5.2-codex` | gpt-5.2 codex | `gpt-5.2-codex` |
| `gpt-5.3-codex` | gpt-5.3 codex | `gpt-5.3-codex` |
| `gpt-5.1-codex-max` | gpt-5.1 codex max | `gpt-5.1-codex-max` |
| `gpt-5.1-codex-mini` | gpt-5.1 codex mini | `gpt-5.1-codex-mini` |

## Data Model Changes

### Current: `detect.Model`

```go
type Model struct {
	ID              string   // e.g., "claude-sonnet", "kimi-thinking"
	DisplayName     string   // e.g., "claude sonnet 4.5", "kimi k2 thinking"
	BaseTool        string   // e.g., "claude" (the CLI tool to invoke)
	Provider        string   // e.g., "anthropic", "moonshot", "zai", "minimax"
	Endpoint        string   // API endpoint (empty = default Anthropic)
	ModelValue      string   // Value for ANTHROPIC_MODEL env var
	RequiredSecrets []string // e.g., ["ANTHROPIC_AUTH_TOKEN"] for third-party
	UsageURL        string   // Signup/pricing page
	Category        string   // "native" or "third-party" (for UI grouping)
}
```

### After: Add `ModelFlag` field

```go
type Model struct {
	ID              string   // e.g., "claude-sonnet", "codex-gpt-5.2"
	DisplayName     string   // e.g., "claude sonnet 4.5", "gpt-5.2 codex"
	BaseTool        string   // e.g., "claude", "codex"
	Provider        string   // e.g., "anthropic", "openai"
	Endpoint        string   // API endpoint (empty = default)
	ModelValue      string   // Value for ANTHROPIC_MODEL env var OR -m flag value
	ModelFlag       string   // CLI flag for model (e.g., "-m") - empty means use env var
	RequiredSecrets []string // e.g., ["ANTHROPIC_AUTH_TOKEN"] for third-party
	UsageURL        string   // Signup/pricing page
	Category        string   // "native" or "third-party"
}
```

The new `ModelFlag` field indicates:
- Empty string (`""`): Use environment variables (current behavior for all Claude models)
- Non-empty (e.g., `"-m"`): Use CLI flag injection (new behavior for Codex models)
```

The new `ModelFlag` field indicates:
- Empty string (`""`): Use environment variables (current behavior for all Claude models)
- Non-empty (e.g., `"-m"`): Use CLI flag injection (new behavior for Codex models)

### Updated `BuildEnv()` Method

```go
func (m Model) BuildEnv() map[string]string {
	// If model uses CLI flag, don't build env vars for model selection
	if m.ModelFlag != "" {
		// Still build endpoint/env if it's a third-party provider
		env := map[string]string{}
		if m.Endpoint != "" {
			env["ANTHROPIC_BASE_URL"] = m.Endpoint
			env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = m.ModelValue
			env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = m.ModelValue
			env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = m.ModelValue
			env["CLAUDE_CODE_SUBAGENT_MODEL"] = m.ModelValue
		}
		return env
	}

	// Existing behavior: use ANTHROPIC_MODEL env var
	env := map[string]string{
		"ANTHROPIC_MODEL": m.ModelValue,
	}
	if m.Endpoint != "" {
		env["ANTHROPIC_BASE_URL"] = m.Endpoint
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = m.ModelValue
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = m.ModelValue
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = m.ModelValue
		env["CLAUDE_CODE_SUBAGENT_MODEL"] = m.ModelValue
	}
	return env
}
```

### New Codex Models

```go
{
	ID:          "gpt-5.2-codex",
	DisplayName: "gpt-5.2 codex",
	BaseTool:    "codex",
	Provider:    "openai",
	ModelValue:  "gpt-5.2-codex",
	ModelFlag:   "-m",
	Category:    "native",
},
{
	ID:          "gpt-5.3-codex",
	DisplayName: "gpt-5.3 codex",
	BaseTool:    "codex",
	Provider:    "openai",
	ModelValue:  "gpt-5.3-codex",
	ModelFlag:   "-m",
	Category:    "native",
},
{
	ID:          "gpt-5.1-codex-max",
	DisplayName: "gpt-5.1 codex max",
	BaseTool:    "codex",
	Provider:    "openai",
	ModelValue:  "gpt-5.1-codex-max",
	ModelFlag:   "-m",
	Category:    "native",
},
{
	ID:          "gpt-5.1-codex-mini",
	DisplayName: "gpt-5.1 codex mini",
	BaseTool:    "codex",
	Provider:    "openai",
	ModelValue:  "gpt-5.1-codex-mini",
	ModelFlag:   "-m",
	Category:    "native",
},
```

## Command Generation

### Interactive Mode (`session/manager.go`)

**Current behavior** (all models use env vars):

```bash
ANTHROPIC_MODEL=claude-sonnet-4-5-20250929 claude 'prompt'
```

**New behavior** for Codex models (CLI flag):

```bash
codex -m 'gpt-5.2-codex' 'prompt'
```

**Implementation**: Modify `buildCommand()` in `session/manager.go`:

```go
func buildCommand(target ResolvedTarget, prompt string) (string, error) {
	trimmedPrompt := strings.TrimSpace(prompt)
	if target.Promptable {
		if trimmedPrompt == "" {
			return "", fmt.Errorf("prompt is required for target %s", target.Name)
		}

		// Check if this is a model with CLI flag
		model, ok := detect.FindModel(target.Name)
		var cmd string
		if ok && model.ModelFlag != "" {
			// Inject model flag into command: codex -m gpt-5.2-codex 'prompt'
			cmd = fmt.Sprintf("%s %s %s %s", target.Command, model.ModelFlag, model.ModelValue, shellQuote(trimmedPrompt))
		} else {
			// Standard: command 'prompt'
			cmd = fmt.Sprintf("%s %s", target.Command, shellQuote(trimmedPrompt))
		}

		if len(target.Env) > 0 {
			return fmt.Sprintf("%s %s", buildEnvPrefix(target.Env), cmd), nil
		}
		return cmd, nil
	}
	// ... rest for non-promptable
}
```

### Oneshot Mode (`oneshot/oneshot.go` + `detect/commands.go`)

**Current behavior** (all models use env vars):

```bash
# Claude (with ANTHROPIC_MODEL env var)
claude -p --dangerously-skip-permissions --output-format json 'prompt'

# Codex (no model selection)
codex exec --json 'prompt'
```

**New behavior** for Codex models (CLI flag):

```bash
# Claude (unchanged - uses ANTHROPIC_MODEL env var)
claude -p --dangerously-skip-permissions --output-format json 'prompt'

# Codex with model selection
codex exec --json -m gpt-5.2-codex 'prompt'
```

**Implementation**: Update `BuildCommandParts()` in `detect/commands.go`:

```go
// BuildCommandParts builds command parts for the given detected tool.
// The model parameter is optional; if provided, it will inject model-specific CLI flags.
func BuildCommandParts(toolName, detectedCommand string, mode ToolMode, jsonSchema string, model *detect.Model) ([]string, error) {
	parts := strings.Fields(detectedCommand)
	if len(parts) == 0 {
		return nil, fmt.Errorf("tool %s: empty command", toolName)
	}

	if mode == ToolModeInteractive {
		return parts, nil
	}

	baseCmd := parts[0]
	existingArgs := parts[1:]

	var newArgs []string
	switch toolName {
	case "claude":
		newArgs = append(existingArgs, "-p", "--dangerously-skip-permissions", "--output-format", "json")
		if jsonSchema != "" {
			newArgs = append(newArgs, "--json-schema", jsonSchema)
		}
	case "codex":
		newArgs = append(existingArgs, "exec", "--json")
		// Inject model flag if model uses CLI flag
		if model != nil && model.ModelFlag != "" {
			newArgs = append(newArgs, model.ModelFlag, model.ModelValue)
		}
		if jsonSchema != "" {
			newArgs = append(newArgs, "--output-schema", jsonSchema)
		}
	case "gemini":
		return nil, fmt.Errorf("tool %s: oneshot mode with JSON schema is not supported", toolName)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}

	return append([]string{baseCmd}, newArgs...), nil
}
```

**Thread model through oneshot execution chain** in `oneshot/oneshot.go`:

```go
type resolvedTarget struct {
	Name       string
	Kind       string
	ToolName   string
	Command    string
	Promptable bool
	Env        map[string]string
	Model      *detect.Model // NEW: Include model for CLI flag injection
}

func resolveTarget(cfg *config.Config, targetName string) (resolvedTarget, error) {
	// ...
	model, ok := detect.FindModel(targetName)
	if ok {
		// ... validation ...
		return resolvedTarget{
			Name:       model.ID,
			Kind:       targetKindModel,
			ToolName:   model.BaseTool,
			Command:    baseTarget.Command,
			Promptable: true,
			Env:        mergeEnvMaps(model.BuildEnv(), secrets),
			Model:      &model, // NEW
		}, nil
	}
	// ...
}

func Execute(ctx context.Context, agentName, agentCommand, prompt, schemaLabel string, env map[string]string, dir string, model *detect.Model) (string, error) {
	// ... existing validation ...

	// Build command parts safely, passing model
	cmdParts, err := detect.BuildCommandParts(agentName, agentCommand, detect.ToolModeOneshot, schemaArg, model)
	// ... rest unchanged ...
}

func ExecuteTarget(ctx context.Context, cfg *config.Config, targetName, prompt, schemaLabel string, timeout time.Duration, dir string) (string, error) {
	// ... existing validation ...
	target, err := resolveTarget(cfg, targetName)
	// ...

	if target.Kind == targetKindUser {
		return ExecuteCommand(timeoutCtx, target.Command, prompt, target.Env, dir)
	}
	// Pass model for model targets
	return Execute(timeoutCtx, target.ToolName, target.Command, prompt, schemaLabel, target.Env, dir, target.Model)
}
```

## Files to Change

| File | Change |
|------|--------|
| `internal/detect/models.go` | Add `ModelFlag` field to `Model` struct; add 4 Codex models to `builtinModels`; update `BuildEnv()` to skip `ANTHROPIC_MODEL` when `ModelFlag != ""` |
| `internal/detect/commands.go` | Add optional `model *detect.Model` parameter to `BuildCommandParts()`; inject `-m <model>` for Codex models |
| `internal/oneshot/oneshot.go` | Add `Model *detect.Model` field to `resolvedTarget` struct; update `resolveTarget()` to include model; update `Execute()` signature to accept model; update `ExecuteTarget()` to pass model |
| `internal/session/manager.go` | Update `buildCommand()` to inject model flag for Codex models in interactive mode |
| `docs/api.md` | No changes needed - models endpoint auto-exposes new models via existing API contract |

## Behavior After Changes

### Model List

The models list will include:

**Claude (Native):**
- claude-opus (uses env var)
- claude-sonnet (uses env var)
- claude-haiku (uses env var)

**Codex (Native):**
- gpt-5.2-codex (uses `-m` flag)
- gpt-5.3-codex (uses `-m` flag)
- gpt-5.1-codex-max (uses `-m` flag)
- gpt-5.1-codex-mini (uses `-m` flag)

**Third-Party (all use env var):**
- kimi-thinking
- kimi-k2.5
- glm-4.7
- minimax
- qwen3-coder-plus

### Interactive Mode Commands

| Model | Command |
|-------|---------|
| claude-sonnet | `ANTHROPIC_MODEL=claude-sonnet-4-5-20250929 claude 'prompt'` |
| gpt-5.2-codex | `codex -m gpt-5.2-codex 'prompt'` |

### Oneshot Mode Commands

| Model | Command |
|-------|---------|
| claude-sonnet | `claude -p --dangerously-skip-permissions --output-format json 'prompt'` (with `ANTHROPIC_MODEL` env var) |
| gpt-5.2-codex | `codex exec --json -m gpt-5.2-codex 'prompt'` |

## Detection Requirements

Codex must be detected as a tool before Codex models appear in the available models list. Detection is already implemented in `internal/detect/agents.go`:

- Via PATH (`codex` command)
- Via npm global (`@openai/codex`)
- Via Homebrew formula

## Testing

### Unit Tests

- `Model.BuildEnv()` returns nil for `ANTHROPIC_MODEL` when `ModelFlag` is set
- `buildCommand()` injects `-m <model>` for Codex models
- `buildCommand()` uses env var prefix for Claude models
- `BuildCommandParts()` injects model flag for Codex in oneshot mode
- `BuildCommandParts()` doesn't inject flag for Claude models
- Model resolution finds Codex models correctly

### Integration Tests

- Spawn session with `gpt-5.2-codex` model (interactive mode)
- Verify tmux session command includes `-m gpt-5.2-codex`
- Run oneshot with `gpt-5.2-codex` model
- Verify command includes `exec --json -m gpt-5.2-codex`

### E2E Tests

- Add E2E test spawning with Codex model (requires Codex to be installed)

## Open Questions

1. **Model flag position**: The spec assumes the model flag should come after `exec --json` in oneshot mode. Verify this is the correct position for Codex's CLI.

2. **Secrets for Codex**: Native Codex models don't require secrets in schmux (Codex handles its own auth). If third-party Codex-compatible providers are added later, they would use the existing `RequiredSecrets` mechanism.

3. **Model aliases**: Should we add short aliases like `gpt-5.2` → `gpt-5.2-codex`? (Similar to `opus` → `claude-opus`)

4. **Future: Gemini models**: If Gemini adds model selection via CLI flags, this same mechanism would apply (`ModelFlag` field).
