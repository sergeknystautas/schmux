# Remove Built-in Model List

## Problem

The dynamic model discovery spec was clarified: the remote registry replaces the built-in hardcoded model list entirely. The implementation kept `builtinModels` as a fallback layer instead of deleting it. This means deprecated models persist, the registry can never be the sole source of truth, and the hardcoded list still needs manual maintenance — defeating the purpose of dynamic discovery.

## Changes Required by Spec Clarification

### 1. Delete `builtinModels` and `GetBuiltinModels()`

**`internal/detect/models.go`:**

- Delete the `builtinModels` var (~400 lines, 35 models)
- Delete `GetBuiltinModels()` function

Everything else in this file stays: `Model` struct, `RunnerSpec`, `defaultModels`, `GetDefaultModels()`, `IsDefaultModel()`, `legacyIDMigrations`, `MigrateModelID()`, helper methods.

### 2. Manager catalog merge

**`internal/models/manager.go`:**

- Delete the `builtinModels` field from the `Manager` struct
- Remove `builtinModels: detect.GetBuiltinModels()` from `New()`
- In `rebuildCatalog()`, delete the "Layer 3: built-in" loop. Three sources merge into the index (last write wins on ID collision):
  1. Registry models
  2. User-defined models
  3. `default_*` models

  On ID collision, last write wins: user-defined overrides registry, `default_*` overrides both.

On first startup with no cache and a failed fetch, only `default_*` models are available. This matches the spec. The dashboard should log a warning ("model registry unavailable, showing defaults only") so this doesn't look broken. The `catalog_updated` WebSocket event fires once the fetch succeeds, and the frontend refreshes automatically.

### 3. Fix callers of `GetBuiltinModels()`

With the function deleted, 5 call sites in `config/` break.

**`config/secrets.go`** (4 call sites) and **`config/config.go`** (1 call site) use `GetBuiltinModels()` to look up model→provider mappings during config loading and secrets migration. These run at startup before the manager exists.

**Fix:** A static `map[string]string` of historical model ID → provider in `config/secrets.go`, used only by these callers. Only includes IDs that differ from the registry:

- Dropped models not in the registry (`opencode-zen`, `qwen3-coder-plus`)
- Old IDs renamed in the registry (`minimax-2.5`, `kimi-thinking`)
- Legacy aliases (`opus`, `minimax`)
- models.dev mixed-case IDs that are targets of ID migration (`MiniMax-M2.1`)

Models that exist in the registry with the same ID (e.g., `claude-opus-4-6`) don't need to be in the map — the registry provides their provider.

One runtime caller (`getProviderForModel`, called from `SaveModelSecrets`) doesn't need the map at all — its caller already has the provider from the manager. Pass the provider through instead.

### 4. Delete `GetBaseToolName()`

`GetBaseToolName()` resolves target → tool by searching `builtinModels`. With `builtinModels` deleted, this breaks. The manager already does target → tool resolution. All callers use the manager instead. `GetBaseToolName()` is deleted.

The 5 callers in `session/manager.go` and `workspace/ensure/manager.go` all need the same thing: given a target name, get the tool name. The manager's `FindModel()` returns the model, which has runners, which gives the tool name. Callers that already have a manager reference use it directly. The ensurer gets a manager reference via standard dependency injection.

`GetInstructionPathForTarget()` has no production callers — dead code, delete. `GetAgentInstructionConfigForTarget()` is called from the ensurer, which now has the manager and can resolve target → tool itself.

### 5. Deduplicate registry models

models.dev returns multiple IDs for the same underlying model. Without deduplication, the catalog shows confusing duplicates.

**Anthropic — alias/dated pairs (8 pairs):**

| Alias ID (latest pointer)  | Dated ID (concrete)          |
| -------------------------- | ---------------------------- |
| `claude-opus-4-0`          | `claude-opus-4-20250514`     |
| `claude-opus-4-1`          | `claude-opus-4-1-20250805`   |
| `claude-opus-4-5`          | `claude-opus-4-5-20251101`   |
| `claude-sonnet-4-0`        | `claude-sonnet-4-20250514`   |
| `claude-sonnet-4-5`        | `claude-sonnet-4-5-20250929` |
| `claude-haiku-4-5`         | `claude-haiku-4-5-20251001`  |
| `claude-3-5-haiku-latest`  | `claude-3-5-haiku-20241022`  |
| `claude-3-7-sonnet-latest` | `claude-3-7-sonnet-20250219` |

**OpenAI — chat-latest dupes (2 pairs):**

| Base ID   | Chat-latest alias     |
| --------- | --------------------- |
| `gpt-5.1` | `gpt-5.1-chat-latest` |
| `gpt-5.2` | `gpt-5.2-chat-latest` |

**Fix:** Add dedup rules to `ParseRegistry` as provider-specific alias rules in the provider profiles, not display-name string matching. Each provider profile can declare ID suffixes/patterns to skip (e.g., anthropic skips IDs ending in dated suffixes when an alias exists, openai skips `-chat-latest`). This is more robust than matching on display name, which models.dev could change at any time.

Note: the minimax case-mismatch dupes (`minimax-m2.1` vs `MiniMax-M2.1`) resolve themselves when builtins are deleted — only the registry's `MiniMax-*` IDs remain.

### 6. Update `docs/models.md`

`docs/models.md` has references to the `builtinModels` maintenance workflow that become misleading after deletion. Update to reflect that models come from the registry.

### 7. Tests

- Delete `TestGetBuiltinModels` (tests deleted function)
- Rewrite any test that asserts 35 built-in models or three-layer merge behavior
- **Merge precedence:** user-defined > registry collision, `default_*` always present
- **Dedup filtering:** alias/dated pairs resolve to one entry per provider profile rules
- **Startup fallback:** empty registry + empty cache = only `default_*` models
- **Historical migration map:** every model ID from the old `builtinModels` and every alias from `legacyIDMigrations` is present
- **Config/secrets migration:** migration from legacy model-keyed secrets using the historical map
- **Spawn integration:** spawn and workspace-ensure resolve tools correctly when models come from registry
- **GetBaseToolName removal:** callers use manager-resolved tool names, not hardcoded lookups

## Dead Code Cleanup (Separate PR)

Pre-existing dead code left by the feature commit, split into a separate PR to reduce rollout risk:

1. **`detect/models.go`** — `GetAvailableModels()` has no production callers (superseded by `manager.GetCatalog()`)
2. **`detect/models.go`** — "Model Catalog Maintenance" comment block (provider API docs for manually maintaining the now-deleted list)
3. **Tests** for dead functions
4. **Stale comments** in `manager.go` saying "Delegates to detect.FindModel" when the code uses `mergedIndex`

## Documentation

`docs/models.md` and `docs/api.md` (if contracts change) must be updated in the same PR. The existing CI check (`scripts/check-api-docs.sh`) enforces `docs/api.md` updates when API packages change.

## Behavioral Changes

- `opencode-zen` and `qwen3-coder-plus` are gone. Configs referencing them get "model not found."
- First startup with no cache + failed fetch: only `default_*` models until fetch succeeds (logged as warning)
- Deprecated/renamed models no longer persist from a hardcoded fallback layer
- Registry duplicates (alias/dated pairs) are filtered, reducing catalog noise
