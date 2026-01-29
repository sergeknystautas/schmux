# Spec: Rename Variants to Models

**Status:** Implemented
**Created:** 2026-01-27

## Summary

Rename "variants" to "models" throughout the codebase. Expand the concept to include native Claude model selection (Opus, Sonnet, Haiku) alongside third-party providers (Kimi, GLM, MiniMax).

## Motivation

The current "variant" terminology is confusing:
- Users think in terms of **models** (Sonnet, Opus, Kimi)
- "Variant" sounds like a configuration tweak, not a model choice
- Native Claude model selection (Opus vs Sonnet vs Haiku) doesn't fit the current model

After the rename:
- Users select a **model** when spawning
- Some models are just different `ANTHROPIC_MODEL` values (Claude native)
- Some models also change the endpoint (Kimi, GLM, MiniMax)
- The data model is unified and intuitive

## Model Inventory

### Claude Native Models (no endpoint change)

| ID | Display Name | Model Value |
|----|--------------|-------------|
| `claude-opus` | claude opus 4.5 | `claude-opus-4-5-20251101` |
| `claude-sonnet` | claude sonnet 4.5 | `claude-sonnet-4-5-20250929` |
| `claude-haiku` | claude haiku 4.5 | `claude-haiku-4-5-20251001` |

### Third-Party Models (endpoint + model change)

| ID | Display Name | Provider | Endpoint | Model Value |
|----|--------------|----------|----------|-------------|
| `kimi-thinking` | kimi k2 thinking | Moonshot | `api.moonshot.ai/anthropic` | `kimi-thinking` |
| `kimi-k2.5` | kimi k2.5 | Moonshot | `api.moonshot.ai/anthropic` | `kimi-k2.5` |
| `glm-4.7` | glm 4.7 | Z.AI | `api.z.ai/api/anthropic` | `glm-4.7` |
| `minimax-m2.1` | minimax m2.1 | MiniMax | `api.minimax.io/anthropic` | `minimax-m2.1` |

**Not included (experimental/outdated):**
- Kimi K2 Instruct - not used
- Claude Sonnet 4, Haiku 3.5 - superseded by 4.5 versions
- GLM 4.5 - superseded by 4.7
- MiniMax M2 - superseded by M2.1

### Pricing (per million tokens, as of 2026-01-27)

| Model | Input | Output | Source |
|-------|-------|--------|--------|
| Claude Opus 4.5 | $5.00 | $25.00 | [Anthropic](https://platform.claude.com/docs/en/about-claude/pricing) |
| Claude Sonnet 4.5 | $3.00 | $15.00 | [Anthropic](https://platform.claude.com/docs/en/about-claude/pricing) |
| Claude Haiku 4.5 | $1.00 | $5.00 | [Anthropic](https://platform.claude.com/docs/en/about-claude/pricing) |
| kimi k2 thinking | $0.60 | $2.50 | [Moonshot](https://platform.moonshot.ai/docs/pricing/chat) |
| kimi k2.5 | $0.60 | $3.00 | [VentureBeat](https://venturebeat.com/orchestration/moonshot-ai-debuts-kimi-k2-5-most-powerful-open-source-llm-beating-opus-4-5) |
| glm 4.7 | $0.60 | $2.20 | [Z.AI](https://docs.z.ai/guides/overview/pricing) |
| minimax m2.1 | $0.27 | $1.12 | [MiniMax](https://platform.minimax.io/docs/guides/pricing) |

## Data Model Changes

### Before: `detect.Variant`

```go
type Variant struct {
    Name            string
    DisplayName     string
    BaseTool        string            // e.g., "claude"
    Env             map[string]string // All env vars bundled
    RequiredSecrets []string
    UsageURL        string
}
```

### After: `detect.Model`

```go
type Model struct {
    ID              string            // e.g., "claude-sonnet", "kimi-thinking"
    DisplayName     string            // e.g., "claude sonnet 4.5", "kimi k2 thinking"
    BaseTool        string            // e.g., "claude" (the CLI tool to invoke)
    Provider        string            // e.g., "anthropic", "moonshot", "zai", "minimax"
    Endpoint        string            // API endpoint (empty = default Anthropic)
    ModelValue      string            // Value for ANTHROPIC_MODEL env var
    RequiredSecrets []string          // e.g., ["ANTHROPIC_AUTH_TOKEN"] for third-party
    UsageURL        string            // Signup/pricing page
    Category        string            // "native" or "third-party" (for UI grouping)
}

// Helper to build Env map from Model fields
func (m Model) BuildEnv() map[string]string {
    env := map[string]string{
        "ANTHROPIC_MODEL": m.ModelValue,
    }
    if m.Endpoint != "" {
        env["ANTHROPIC_BASE_URL"] = m.Endpoint
        // Third-party models need all tier overrides
        env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = m.ModelValue
        env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = m.ModelValue
        env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = m.ModelValue
        env["CLAUDE_CODE_SUBAGENT_MODEL"] = m.ModelValue
    }
    return env
}
```

### Config Changes

#### `config.json`

The `variants` array is removed entirely - no user overrides needed.

```diff
{
-  "variants": [
-    {
-      "name": "kimi-thinking",
-      "enabled": false
-    }
-  ]
}
```

#### `secrets.json`

```diff
{
-  "variants": {
+  "models": {
     "kimi-thinking": {
       "ANTHROPIC_AUTH_TOKEN": "sk-..."
     }
   }
}
```

## Files to Change

### Core Data Model

| File | Change |
|------|--------|
| `internal/detect/variants.go` | Rename to `models.go`, update `Variant` → `Model` struct |
| `internal/config/variants.go` | Rename to `models.go`, update types and functions |
| `internal/config/config.go` | Remove `Variants []VariantConfig` field entirely |
| `internal/config/secrets.go` | Update secrets key from `"variants"` to `"models"` |

### Session Management

| File | Change |
|------|--------|
| `internal/session/manager.go` | Update `ResolveTarget` to use `Model` instead of `Variant` |

### API Layer

| File | Change |
|------|--------|
| `internal/api/contracts/config.go` | Add `Model` type, remove `Variant` type, update `ConfigResponse` |
| `internal/dashboard/handlers.go` | Add `models` to config response, remove `/api/variants`, rename secrets endpoints to `/api/models/` |
| `internal/dashboard/server.go` | Update route registration |
| `pkg/cli/daemon_client.go` | Update client types |

### Frontend

| File | Change |
|------|--------|
| `assets/dashboard/src/lib/types.ts` | Add `Model` type, remove `Variant` types |
| `assets/dashboard/src/lib/api.ts` | Remove `getVariants()`, use `models` from `getConfig()`, rename secrets functions to `configureModelSecrets`/`removeModelSecrets` |
| `assets/dashboard/src/routes/ConfigPage.tsx` | Use `models` from config instead of separate variants fetch |

### Documentation

| File | Change |
|------|--------|
| `docs/targets.md` | Rename "Variants" section to "Models" |
| `docs/api.md` | Update API endpoint documentation |
| `CLAUDE.md` | Update references |

## Migration Strategy

### Config Version

Bump `config_version` to trigger migration.

### Migration Logic (`config.Migrate()`)

```go
func (c *Config) Migrate() error {
    // Migration: remove variants array (config_version < "1.1")
    if c.needsMigration("1.1") {
        c.Variants = nil // Just drop it - no longer used
        c.ConfigVersion = "1.1"
    }
    return nil
}
```

### JSON Backward Compatibility

The `variants` field in config.json is simply ignored/dropped on load - no migration needed since we're removing the concept entirely.

### Secrets Migration

Same approach for `secrets.json`:

```go
type Secrets struct {
    Models   map[string]map[string]string `json:"models,omitempty"`
    Variants map[string]map[string]string `json:"variants,omitempty"` // deprecated
}
```

## API Changes

### Consolidate into `/api/config`

Remove the separate `/api/variants` endpoint. Model metadata moves into `/api/config`:

**Before:** Two endpoints
- `GET /api/config` → includes `variants` (user overrides) and adds configured variants to `run_targets`
- `GET /api/variants` → model metadata (display name, required secrets, usage URL, configured)

**After:** Single endpoint
- `GET /api/config` → includes `models` with full metadata
- `DELETE /api/variants` endpoint (or keep as deprecated alias)

### `/api/config` Response Changes

```typescript
interface ConfigResponse {
  // ... existing fields ...

  // REPLACES: variants array removed entirely
  // NEW: full model metadata with configuration status
  models: Model[];
}

interface Model {
  id: string;                 // e.g., "claude-sonnet", "kimi-thinking"
  display_name: string;       // e.g., "Claude Sonnet 4.5"
  base_tool: string;          // e.g., "claude"
  provider: string;           // "anthropic", "moonshot", "zai", "minimax"
  category: string;           // "native" or "third-party"
  required_secrets: string[]; // e.g., ["ANTHROPIC_AUTH_TOKEN"] for third-party
  usage_url: string;          // signup/pricing page
  configured: boolean;        // true for native, true for third-party if secrets exist
}
```

No user override layer - if you don't want a model, don't configure its secrets.

### Secrets Endpoints

Keep the secrets management endpoints but rename:

| Before | After |
|--------|-------|
| `POST /api/variants/{name}/secrets` | `POST /api/models/{name}/secrets` |
| `DELETE /api/variants/{name}/secrets` | `DELETE /api/models/{name}/secrets` |

## UI Changes

### Spawn Wizard

**Before:** Single dropdown mixing tools and variants
**After:** Model selector with grouped options

```
┌─ Model ──────────────────────────────────────┐
│ ▼ Claude Sonnet 4.5                          │
├──────────────────────────────────────────────┤
│ Claude (Native)                              │
│   ○ Claude Opus 4.5                          │
│   ● Claude Sonnet 4.5                        │
│   ○ Claude Haiku 4.5                         │
├──────────────────────────────────────────────┤
│ Third-Party                                  │
│   ○ Kimi K2 Thinking                         │
│   ○ GLM 4.7                                  │
│   ○ MiniMax M2.1                             │
└──────────────────────────────────────────────┘
```

### Model Configuration Page

Accessible from Settings. Shows:
- All available models
- Which are configured (secrets present for third-party)
- Links to provider signup pages
- Secret management

## Implementation Phases

### Phase 1: Rename + Migration
1. Rename `Variant` → `Model` in detect package
2. Add new fields (`Provider`, `Category`, `Endpoint`, `ModelValue`)
3. Add native Claude models to builtin list
4. Update config.json/secrets.json field names with backward compat
5. Update session manager
6. Update API endpoints and contracts
7. Run `go run ./cmd/gen-types` to regenerate TS types

### Phase 2: Frontend
1. Update API calls (`/api/models`)
2. Update spawn wizard UI with grouped model selector
3. Update settings/configuration pages
4. Update documentation references

### Phase 3: Cleanup
1. Remove deprecated `variants` JSON field support (after N releases)
2. Remove backward compat code

## Testing

### Unit Tests
- Model resolution with native models
- Model resolution with third-party models
- Config migration from `variants` to `models`
- Secrets migration

### E2E Tests
- Spawn with native Claude model selection
- Spawn with third-party model (if secrets configured)

## Open Questions

1. **Default model**: When user selects "claude" tool without specifying model, what's the default?
   - Option A: No override (let Claude Code pick)
   - Option B: Always default to Sonnet 4.5
   - **Recommendation**: Option A - no override, user can explicitly pick if they want

2. **Model aliases**: Should we support short aliases like "opus", "sonnet", "haiku"?
   - **Recommendation**: Yes, map them to full model IDs internally
   - **Implementation note**: The dashboard mirrors these aliases in the UI. Keep `assets/dashboard/src/routes/ConfigPage.tsx` model alias map in sync with `internal/detect/models.go`.

3. **Backward compat duration**: How long to support `variants` field name?
   - **Recommendation**: 2 minor versions, then remove

4. **Non-Claude tools**: Should Codex/Gemini also have model variants?
   - **Recommendation**: Future scope - they have different env vars and model ecosystems
