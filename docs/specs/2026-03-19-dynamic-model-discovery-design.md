# Dynamic Model Discovery

## Problem

Model IDs are hardcoded in `internal/detect/models.go` (`builtinModels`). Every new model requires a code change and release. Third-party models routed through Claude via `ANTHROPIC_BASE_URL` are a core use case and especially painful to maintain this way.

## Design

### Two-Layer Model Catalog

Models come from two sources, merged with this priority:

1. **User-defined models** (highest) — local config at `~/.schmux/models.json`, editable from the dashboard. Overrides everything. For private/internal models.
2. **Remote registry** — fetched from `models.dev/api.json`, cached locally. This is the sole source of non-user models.

The `default_*` models (see below) are always present regardless of registry state, but they are generated in code — not a "built-in list." There is no hardcoded model list. The old `builtinModels` variable is deleted entirely.

**Single source of truth:** `internal/models/manager.go` owns the merged catalog. The static helpers in `internal/detect/` (`FindModel`, `IsModelID`, `GetBaseToolName`) are removed or refactored to delegate to the manager. No code should read a hardcoded model list.

### Default Tool Models

Each runner has a synthetic `default_<tool>` model:

- `default_claude`
- `default_codex`
- `default_gemini`
- `default_opencode`

These are special: they are only available for their respective runner, and when spawned, **no `--model` flag is passed**. The harness uses whatever its own default model is. This means when a harness updates its default (e.g., Claude promotes a new model), schmux picks it up automatically without knowing the model ID.

These models are always present in the catalog regardless of registry state. They are generated in code, not fetched. They carry explicit metadata (`IsDefault: true`) so the UI can group and display them without string-parsing the ID.

### Provider Profiles

schmux maintains a small set of **provider profiles** that map models.dev providers to schmux runners. This is the only schmux-specific configuration — ~10 lines per provider, not per model.

Each profile defines:

- **runner**: which schmux runner executes these models (claude, codex, gemini, opencode)
- **endpoint**: API endpoint override (empty = runner's default)
- **secrets**: required environment variables
- **schmux_provider**: internal provider name if different from models.dev name
- **opencode_prefix**: prefix for the opencode runner value (when it differs from the models.dev provider name)
- **usage_url**: signup/pricing page for the provider
- **category**: `native` or `third-party` (for UI grouping)

```
anthropic   → runner: claude,   category: native,      opencode_prefix: anthropic
openai      → runner: codex,    category: native,      opencode_prefix: openai
google      → runner: gemini,   category: native,      opencode_prefix: google
moonshotai  → runner: claude,   category: third-party,  opencode_prefix: moonshot,
              endpoint: https://api.moonshot.ai/anthropic,
              secrets: [ANTHROPIC_AUTH_TOKEN],
              schmux_provider: moonshot,
              usage_url: https://platform.moonshot.ai/console/account
zai         → runner: claude,   category: third-party,  opencode_prefix: zhipu,
              endpoint: https://api.z.ai/api/anthropic,
              secrets: [ANTHROPIC_AUTH_TOKEN],
              usage_url: https://z.ai/manage-apikey/subscription
minimax     → runner: claude,   category: third-party,  opencode_prefix: minimax,
              endpoint: https://api.minimax.io/anthropic,
              secrets: [ANTHROPIC_AUTH_TOKEN],
              usage_url: https://platform.minimax.io/user-center/payment/coding-plan
```

When a new model appears in models.dev from an existing provider, it works in schmux automatically. A new provider requires adding one profile.

**Provider identity model:** The canonical provider ID stored on each `Model.Provider` is the `schmux_provider` value (or the models.dev provider name if no `schmux_provider` override). This means `moonshotai` models get `Provider: "moonshot"`, `zai` models get `Provider: "zai"`. The models.dev provider name is used only during fetch-time matching. All downstream code (secrets, grouping, UI) uses the canonical provider ID.

**Runner construction from profiles:**

- Primary runner: `ModelValue` = the model ID from models.dev. Passed via `--model <id>`.
- Opencode runner: `ModelValue` = `{opencode_prefix}/{model_id}`. Added to every model.
- `UsageURL`, `Category` derived from the provider profile, not per-model.

### Model IDs

schmux adopts models.dev model IDs going forward. Existing IDs that differ (e.g., `minimax-2.5` → `MiniMax-M2.5`, `kimi-thinking` → `kimi-k2-thinking`) are added to `legacyIDMigrations` in `models.go`. The migration runs at config load time and updates `enabledModels`, `secrets.json`, and session state references. Existing legacy chains (e.g., `minimax` → `minimax-m2.1`) are updated to resolve transitively to the final models.dev ID.

Note: models.dev uses mixed-case IDs (e.g., `MiniMax-M2.5`). We adopt these as-is.

### Filtering

Not all models from models.dev are relevant. Filter criteria:

- `tool_call == true`
- `text` in output modalities
- Released within the last 12 months (`release_date` field)
- Provider is one of: `anthropic`, `openai`, `google`, `moonshotai`, `zai`, `minimax`

This currently yields ~83 models, down from 700+ in the full registry. Models with missing fields are skipped gracefully (treated as not matching the filter).

Models that fall out of the 12-month filter are removed from the catalog. Configs referencing removed models get "model not found" errors. Legacy model IDs are handled via `legacyIDMigrations`, not by retaining stale registry entries.

### Fetch Strategy

- Fetch on daemon startup (non-blocking — serve cached data while fetching)
- Re-fetch daily via a background ticker goroutine
- Cache to `~/.schmux/cache/models-dev.json` with a schema version field
- If models.dev is unreachable, use cached data; if cache is also missing or corrupt (invalid JSON or wrong schema version), only `default_*` models are available until the next successful fetch
- When a fetch completes, the model catalog is hot-swapped behind a `sync.RWMutex` — reads continue unblocked, the write lock is held only for the pointer swap

**Refresh lifecycle:** The fetch ticker is owned by `internal/daemon/daemon.go`. It is started after the daemon initializes and stopped via context cancellation on daemon shutdown. The ticker goroutine respects the context, so no goroutine leaks on stop/restart. The hot-swap writes a new catalog pointer under the write lock; no concurrent fetch can race because the ticker serializes fetches.

**Frontend freshness:** When the catalog is hot-swapped, the daemon broadcasts a `catalog_updated` event on the existing `/ws/dashboard` WebSocket. The frontend receives this and re-fetches `/api/config` to get the updated model list. No manual reload needed.

### Secrets Model

**Change:** Move from model-scoped to provider-scoped secrets storage. Today, `secrets.json` keys are model IDs and provider secrets are inferred by scanning models. With dynamic models, this scanning is fragile.

New `secrets.json` structure:

```json
{
  "providers": {
    "moonshot": { "ANTHROPIC_AUTH_TOKEN": "sk-..." },
    "zai": { "ANTHROPIC_AUTH_TOKEN": "sk-..." },
    "minimax": { "ANTHROPIC_AUTH_TOKEN": "sk-..." }
  }
}
```

Provider secrets apply to all models from that provider. The old `models` key in `secrets.json` is migrated to `providers` at startup (grouping by model provider). `GetProviderSecrets` reads directly from the `providers` map — no scanning needed, deterministic regardless of catalog ordering.

### User-Defined Models

Users can add custom models in `~/.schmux/models.json`:

```json
{
  "models": [
    {
      "id": "my-internal-model",
      "display_name": "Internal LLM v3",
      "provider": "internal",
      "runner": "claude",
      "endpoint": "https://llm.internal.corp/anthropic",
      "required_secrets": ["ANTHROPIC_AUTH_TOKEN"]
    }
  ]
}
```

Schema: `id`, `display_name`, `provider`, `runner`, `endpoint` (optional), `required_secrets` (optional).

User-defined models get their specified runner plus an opencode runner via `{provider}/{id}` convention. This is also editable from the dashboard settings UI via new CRUD API endpoints (see API changes below).

**Collision policy:** If a user-defined model ID matches a registry ID (case-sensitive), the user-defined version wins. The UI shows a badge indicating the model is user-defined. This is the documented behavior of the priority layers.

**Validation rules** (enforced server-side on `PUT /api/user-models`):

- `id`: required, non-empty, must not start with `default_` (reserved prefix), no duplicates within the user models list
- `runner`: required, must be one of the detected tool names (`claude`, `codex`, `gemini`, `opencode`)
- `endpoint`: if provided, must be a valid URL (https or http scheme)
- `required_secrets`: if provided, must be an array of non-empty strings
- `display_name`: defaults to `id` if omitted
- `provider`: defaults to `"custom"` if omitted

### Bonus Data from models.dev

models.dev provides data schmux doesn't currently have:

- **Cost**: input/output price per million tokens
- **Context window**: max input tokens
- **Max output**: max output tokens
- **Reasoning**: whether the model supports extended thinking
- **Release date**: when the model was released

This data is stored alongside models and surfaced in the UI (see UI changes below). Fields are optional — if models.dev changes its schema, missing fields result in "unknown" in the UI, not errors.

## UI Changes

### Model Catalog (`ModelCatalog.tsx`)

- Show context window per model
- Default tool models (`default_claude`, etc.) shown in a separate "Defaults" group at the top

### Session Detail Page (`SessionDetailPage.tsx`)

- **Remove**: Branch field (already shown along the top of the page)
- **Remove**: Status field
- **Keep**: Target (moves up)
- **Add below target**: Context window, Pricing (input/output per MTok)

### Dashboard Settings

- New section for user-defined models (add/edit/remove custom model entries)

## API Changes

New endpoints for user-defined model CRUD:

- `GET /api/user-models` — list user-defined models
- `PUT /api/user-models` — save user-defined models (full replacement), with server-side validation (see validation rules above)

The `contracts.Model` struct gains new optional fields: `ContextWindow`, `MaxOutput`, `CostInputPerMTok`, `CostOutputPerMTok`, `Reasoning`, `ReleaseDate`, `IsDefault`, `IsUserDefined`. These are populated when available from the registry. TypeScript types regenerated via `go run ./cmd/gen-types`.

**Documentation commitment:** `docs/api.md` is updated in the same PR that introduces new endpoints and struct changes. The project's existing CI check (`scripts/check-api-docs.sh`) enforces this.

## Migration Plan

### Startup Migration Order

All migrations run synchronously at startup, before the daemon begins serving:

1. Load config from `~/.schmux/config.json`
2. Migrate model IDs in `enabledModels` via `legacyIDMigrations`
3. Migrate secrets from model-keyed to provider-keyed format in `secrets.json`
4. Migrate model IDs in `state.json` session references
5. Save migrated config/secrets/state files
6. Initialize model catalog (load cache, start background fetch)
7. Begin serving

No component reads stale IDs because migration completes before any catalog queries.

### One-Time ID Migration

Add to `legacyIDMigrations` in `models.go`:

| Old schmux ID   | New models.dev ID       |
| --------------- | ----------------------- |
| `kimi-thinking` | `kimi-k2-thinking`      |
| `kimi-k2.5`     | `kimi-k2.5` (no change) |
| `minimax-m2.1`  | `MiniMax-M2.1`          |
| `minimax-2.5`   | `MiniMax-M2.5`          |
| `minimax-2.7`   | `MiniMax-M2.7`          |

Existing chains (e.g., `minimax` → `minimax-m2.1`) updated to resolve to final ID (`MiniMax-M2.1`).

### ModelValue Fix

The three Claude models using shortcut aliases (`opus`, `sonnet`, `haiku`) as ModelValues have already been fixed to use full model IDs (`claude-opus-4-6`, `claude-sonnet-4-6`, `claude-haiku-4-5`).

### dashscope / Alibaba

The `qwen3-coder-plus` model (provider `dashscope`) is intentionally dropped. Alibaba/dashscope is removed from the provider list. Can be re-added later if there's demand.

## Test Requirements

Required before merge:

- **Registry fetch:** Unit tests for fetching, parsing, filtering models.dev data. Mock HTTP responses for offline testing.
- **Two-layer merge:** Tests that user-defined > registry priority is respected, including collision cases.
- **Provider profile mapping:** Tests that models.dev models get correct runner specs, endpoints, secrets, opencode prefixes.
- **Default models:** Tests that `default_*` models produce commands with no `--model` flag.
- **Catalog dedup:** Tests that dated variants, `-latest` suffixes, and `-chat-latest` patterns are deduplicated.
- **ID migration:** Tests for legacy chain resolution and that config/secrets/state are all migrated.
- **Secrets migration:** Tests for model-keyed → provider-keyed migration.
- **User model validation:** Tests for each validation rule (reserved prefix, runner allowlist, URL format, duplicates).
- **Hot-swap concurrency:** Test that concurrent reads during a catalog swap don't panic or return partial data.
- **Adapter empty ModelValue:** Tests that each adapter skips `--model` when ModelValue is empty.
- **Frontend:** Tests for catalog_updated WebSocket event handling, model catalog display, session detail field changes.

## What Changes, What Doesn't

### Changes

- `internal/detect/models.go` — `builtinModels` deleted entirely; `default_*` models added; legacy migrations updated; static helpers removed or delegated to manager
- `internal/detect/adapter*.go` — adapters handle empty `ModelValue` (skip `--model` flag) for default models
- `internal/models/manager.go` — single source of truth for merged catalog; RWMutex for hot-swap; daily refresh ticker; provider profile matching
- `internal/config/` — load/save user-defined models, migration logic
- `internal/config/secrets.go` — move to provider-keyed secrets; `GetProviderSecrets` and `DeleteProviderSecrets` read from `providers` map directly
- `internal/api/contracts/config.go` — `Model` struct gains context window, cost, reasoning, release date, IsDefault, IsUserDefined fields
- `internal/dashboard/` — new user-model CRUD endpoints with validation
- `internal/daemon/` — trigger non-blocking fetch on startup, daily refresh via ticker with context-based shutdown
- `assets/dashboard/src/routes/config/` — user-defined model editor, context window in model catalog
- `assets/dashboard/src/routes/SessionDetailPage.tsx` — remove branch/status, add context window/pricing
- `docs/api.md` — document new endpoints and model struct changes (same PR)
- `cmd/gen-types` output — regenerate TypeScript types

### Doesn't Change

- Spawn flow — still resolves model → runner → command
- WebSocket/terminal — unrelated (except new `catalog_updated` event on existing `/ws/dashboard`)
