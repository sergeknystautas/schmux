# LLM Provider Variants

## Overview

LLM provider variants are **profiles over detected tool run targets**. A variant redirects a detected tool to an alternative provider or model by applying a fixed set of environment variables (or flags) at runtime.

**Example**: `kimi-thinking` applies `ANTHROPIC_BASE_URL=https://api.moonshot.ai/anthropic` and related model settings, then runs the detected `claude` tool.

Variants **only apply to detected tools** (officially supported, auto-detected). They do **not** apply to user-supplied run targets.

For the broader execution model (run targets and contexts), see `docs/run-targets.md`.

## Supported Variants

| Name | Provider | Base Tool | Base URL |
|------|----------|-----------|----------|
| `kimi-thinking` | Moonshot AI | claude | `https://api.moonshot.ai/anthropic` |
| `glm-4.7` | Z.AI | claude | `https://api.z.ai/api/anthropic` |
| `minimax` | MiniMax | claude | `https://api.minimax.io/anthropic` |

All currently supported variants are based on the Claude Code CLI tool (`claude`).

## Environment Variable Pattern

All variants follow the same pattern:

```bash
export ANTHROPIC_BASE_URL=<provider-specific>
export ANTHROPIC_AUTH_TOKEN=<user-api-key>
export ANTHROPIC_MODEL=<model-name>
export ANTHROPIC_DEFAULT_OPUS_MODEL=<model-name>
export ANTHROPIC_DEFAULT_SONNET_MODEL=<model-name>
export ANTHROPIC_DEFAULT_HAIKU_MODEL=<model-name>
export CLAUDE_CODE_SUBAGENT_MODEL=<model-name>
~/.claude/local/claude "$@"
```

## Configuration

Variants are **not** configured in `config.json`. The registry is fixed and tied to detected tools. Users only provide secrets in `~/.schmux/secrets.json`.

### Runtime Resolution

When spawning a session with a variant:

1. Resolve the variant’s `base_tool` to its detected tool command (e.g., `claude` → `~/.claude/local/claude`)
2. Merge variant `env` with user-provided secrets from `~/.schmux/secrets.json`
3. Apply the combined environment to the run target
4. Execute the detected tool in the current context (interactive or oneshot)

### Secrets File

Create `~/.schmux/secrets.json` to store API keys:

```json
{
  "kimi-thinking": {
    "ANTHROPIC_AUTH_TOKEN": "sk-..."
  },
  "glm-4.7": {
    "ANTHROPIC_AUTH_TOKEN": "..."
  }
}
```

This file is:
- Created automatically when user first configures a variant
- Never logged or displayed in the UI
- Read-only to the daemon

## Auto-Detection

Variants are NOT auto-detected. They are:

1. Defined in a hardcoded registry in `internal/detect/variants.go`
2. Only enabled if their `base_tool` is detected
3. Optional - user can enable/disable them in settings

## Context Compatibility

Variants are available anywhere their base detected tool is allowed:

- **Internal use** (oneshot mode)
- **Wizard** (interactive mode)
- **Quick Launch** (interactive or oneshot, depending on preset)

Variants are **not** available for user-supplied run targets.

## Spawn Wizard Flow

### Run Target Selection

When spawning a session, the wizard shows detected tools and their variants:

```
Detected Tools:
├── claude
├── codex
└── gemini

Claude Variants (requires claude):
├── Kimi K2 Thinking (kimi-thinking)
├── GLM 4.7 (glm-4.7)
└── MiniMax M2.1 (minimax)
```

### Variant Configuration

When user selects a variant:

1. Check if secrets exist in `~/.schmux/secrets.json`
2. If missing, show configuration modal:
   - Display variant name and description
   - Input field for API key
   - Link to provider's usage dashboard
3. Save to secrets file
4. Proceed with spawn

## Data Structures

```go
// Variant represents an LLM provider variant.
type Variant struct {
    Name             string            // e.g., "kimi-thinking"
    DisplayName      string            // e.g., "Kimi K2 Thinking"
    BaseTool         string            // e.g., "claude" - must be detected
    Env              map[string]string // template environment variables
    RequiredSecrets  []string          // keys user must provide
    UsageURL         string            // link to provider's dashboard
}

// VariantConfig represents a variant in config.json.
// Used when users customize or disable variants.
type VariantConfig struct {
    Name    string            `json:"name"`
    Enabled *bool             `json:"enabled,omitempty"` // nil = enabled by default
    Env     map[string]string `json:"env,omitempty"`     // overrides
}
```

## API Changes

### Config.Load()
- Parse `variants` array if present
- Merge with built-in variant registry

### Session Manager.Spawn()
- Detect if target is a variant
- If so:
  - Resolve base tool command (detected tool)
  - Fetch secrets from secrets file
  - Apply variant env

### Detect Package
- Add `GetAvailableVariants() []Variant` function
- Returns variants whose base tool is detected
- Called by dashboard to populate spawn wizard

### Dashboard API
- `GET /api/variants` - list available variants
- `POST /api/variants/:name/secrets` - store API keys
- `GET /api/variants/:name/configured` - check if secrets exist

## Frontend Changes

### Spawn Wizard
- Show variants under their base detected tool
- Group by base tool (e.g., "Claude Variants")
- Show configuration badge (✓ configured, ⚠ not configured)
- On click, show config modal if not configured

### Settings Page
- Add "LLM Variants" tab
- List all known variants
- Show configuration status
- Allow editing API keys
- Enable/disable toggle

## Migration Path

1. Add variant registry to `internal/detect/variants.go`
2. Extend config schema (variants remain optional)
3. Add secrets file handling
4. Update session spawn to inject environment variables
5. Update dashboard spawn wizard
6. Update settings page

## Future Extensions

### Model Selection
Some providers offer multiple models. Could add:
- Variant model options (dropdown in wizard)
- Separate variant entries per model

### Base Tool Expansion
Future variants could support:
- Cursor variants (if it supports env vars)
- Other Claude-compatible tools

### Per-Session Overrides
Advanced users could:
- Override base URL per session
- Test different models without config changes
- Use alternate API keys for testing
