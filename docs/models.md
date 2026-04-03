# Model Subsystem

## What it does

Manages the catalog of AI models by merging a remote registry, user-defined models, and synthetic defaults, then resolves which CLI tool runs each model and provides the dashboard with model metadata, availability, and user enablement preferences.

## Key files

| File                                                  | Purpose                                                                                                                                                                                                                     |
| ----------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/detect/models.go`                           | `Model` and `RunnerSpec` structs, default models (`claude`, `codex`, `gemini`, `opencode`), `legacyIDMigrations` map                                                                                                        |
| `internal/models/manager.go`                          | `Manager` owns the merged catalog behind a `sync.RWMutex`. `ResolveModel()` picks tool + env, `GetCatalog()` builds the API response, `FindModel()` resolves IDs with legacy fallback, `IsModel()` determines promptability |
| `internal/models/registry.go`                         | Fetches and parses `models.dev/api.json`, filters by tool_call/text/recency/provider, deduplicates alias/dated variants, manages the local cache at `~/.schmux/cache/models-dev.json`                                       |
| `internal/models/registry_disabled.go`                | No-op stubs under `//go:build nomodelregistry` for builds that exclude the registry                                                                                                                                         |
| `internal/models/profiles.go`                         | `ProviderProfile` entries mapping models.dev providers to schmux runners, endpoints, secrets, and opencode prefixes                                                                                                         |
| `internal/models/userdefined.go`                      | `UserModel` struct, load/save from `~/.schmux/user-models.json`, validation rules, conversion to `detect.Model`                                                                                                             |
| `internal/api/contracts/config.go`                    | API-facing `Model` struct (id, display_name, provider, configured, runners, required_secrets, context_window, cost, reasoning, release_date), `RunnerInfo`, `ConfigResponse.Runners`                                        |
| `internal/detect/adapter.go`                          | `ToolAdapter` interface: `BuildRunnerEnv(RunnerSpec)`, `ModelFlag()`, `Capabilities()`                                                                                                                                      |
| `internal/detect/adapter_claude.go`                   | Claude adapter: sets proxy env vars when `spec.Endpoint` is non-empty                                                                                                                                                       |
| `internal/detect/adapter_codex.go`                    | Codex adapter: returns empty env, uses `-m` as model flag                                                                                                                                                                   |
| `internal/detect/adapter_gemini.go`                   | Gemini adapter: interactive-only capabilities, `--model` flag                                                                                                                                                               |
| `internal/detect/adapter_opencode.go`                 | OpenCode adapter: universal runner for 75+ providers via `provider/model` syntax                                                                                                                                            |
| `internal/detect/commands.go`                         | `BuildCommandParts()` dispatcher -- delegates to adapter's `InteractiveArgs`/`OneshotArgs`/`StreamingArgs` by mode                                                                                                          |
| `internal/detect/tools.go`                            | `IsBuiltinToolName()`, `AgentInstructionConfig`, `GetInstructionPath()`                                                                                                                                                     |
| `internal/config/run_targets.go`                      | Validates command-only `RunTarget` entries (name + command); model validation happens at runtime                                                                                                                            |
| `assets/dashboard/src/routes/config/ModelCatalog.tsx` | Provider-grouped model editor with enable toggles and runner segmented controls                                                                                                                                             |
| `assets/dashboard/src/routes/config/TargetSelect.tsx` | Dropdown that takes `Model[]` and renders options -- no filtering logic, callers pre-filter                                                                                                                                 |
| `assets/dashboard/src/routes/config/useConfigForm.ts` | Derives `modelCatalog` (raw API), `models` (enabled-filtered), and `oneshotModels` (capability-filtered)                                                                                                                    |

## Architecture decisions

### Registry-driven catalog (no hardcoded model list)

Models come from the `models.dev` remote registry, not a hardcoded list. The old `builtinModels` variable (~400 lines, 35 models) has been deleted entirely. Every new model required a code change and release, and third-party models routed through `ANTHROPIC_BASE_URL` were especially painful to maintain.

On first startup with no cache and a failed fetch, only the four default models are available. A warning is logged and the dashboard updates automatically once the fetch succeeds via `catalog_updated` WebSocket event.

**Rejected alternative: keep builtins as fallback.** The initial implementation kept `builtinModels` as a third layer underneath the registry. This was removed because deprecated models persisted, the registry could never be the sole source of truth, and the hardcoded list still needed manual maintenance -- defeating the purpose.

### Three-layer catalog merge

`rebuildCatalog()` in `manager.go` merges three sources. On ID collision, later layers win:

1. **Registry models** (lowest priority) -- from `models.dev/api.json`
2. **User-defined models** -- from `~/.schmux/user-models.json`
3. **Default models** (highest priority) -- synthetic `claude`, `codex`, `gemini`, `opencode` entries

The merge is a flat map keyed by model ID. No inheritance or partial override -- a collision means the higher-priority entry completely replaces the lower one.

### Provider profiles instead of per-model configuration

Each `ProviderProfile` in `profiles.go` maps a models.dev provider key to a runner, endpoint, secrets, opencode prefix, and UI category (~10 lines per provider). When a new model appears from an existing provider, it works automatically. Only a new _provider_ requires a code change.

Every registry model gets two runner entries: one for its provider's primary runner using the models.dev ID as `ModelValue`, and one for opencode using `{opencode_prefix}/{model_id}`.

### Default models pass no --model flag

The four default models have `ModelValue: ""`. No `--model` flag is passed when spawning. The harness uses its own default, so when a harness promotes a new default, schmux picks it up without knowing the model ID.

### Models decoupled from runners

A `Model` has a `Runners map[string]RunnerSpec` listing which tools can execute it and how. The old 1:1 `BaseTool` binding is gone. This allows the same model to run via either its native runner or opencode.

### Adapters own their flags and env vars

`ModelFlag()` returns the adapter's CLI flag (`--model`, `-m`). `BuildRunnerEnv(spec)` constructs env vars (only Claude does anything non-trivial -- proxy endpoint vars). This replaced the old `Model.BuildEnv()` and `Model.ModelFlag` fields.

### Provider-scoped secrets (not model-scoped)

`secrets.json` has a `providers` map keyed by provider: `{"providers": {"moonshot": {"ANTHROPIC_AUTH_TOKEN": "sk-..."}}}`. A legacy `models` map (keyed by model ID) is migrated to provider-keyed format at load time. With dynamic models, scanning the catalog to infer provider from model ID is fragile. Provider-keyed storage is deterministic regardless of catalog state.

### Registry deduplication

models.dev returns multiple IDs for the same model (alias/dated pairs, `-chat-latest` suffixes, `(latest)` display names). `deduplicateModels()` applies four rules: skip IDs matching provider `SkipIDPatterns`, skip `(latest)` display names, skip `-latest` ID suffixes when a dated variant of the same base exists, and skip dated variants when a shorter alias exists (but only when the suffix is 8+ digits, so `claude-opus-4-1` is NOT deduped by `claude-opus-4`).

### Legacy ID migration

`legacyIDMigrations` maps old schmux IDs to current models.dev IDs. `MigrateModelID()` resolves chains transitively (up to depth 10). Migrations run at config load time for `enabledModels`, `secrets.json`, and session state references.

### Config stores `enabled_models` as `map[string]string`

Maps model ID to preferred tool. When empty, all models with a detected runner appear in the spawn wizard (backward compat). Once a user explicitly enables any model, only enabled models appear.

### API response keeps models slim

`contracts.Model` has `runners` as `[]string` (just tool names), not the full `RunnerSpec`. Per-runner details live in a top-level `runners` map on `ConfigResponse`. Registry metadata (context window, cost, reasoning, release date) is populated when available.

### `IsModel()` determines promptability at runtime

A target is "promptable" if it is a model ID or a builtin tool name. Command targets are not promptable. The old bridge that converted models into fake `RunTarget{type:"promptable"}` entries is deleted.

### `run_targets` is a command-only concept

The `RunTarget` struct has only `name` and `command`. Models travel through `models` and `enabled_models` in the API, not through `run_targets`.

### Frontend derives three model lists from one source

`modelCatalog` (raw from API, used by ModelCatalog editor), `models` (filtered to enabled, used by SpawnPage), `oneshotModels` (further filtered to tools with oneshot capability).

### Build-tag exclusion

`//go:build nomodelregistry` compiles out all registry functionality. Only default models exist. Used for builds that need to avoid network dependencies.

## Gotchas

- **Hot-swap concurrency.** The merged catalog is swapped behind a `sync.RWMutex`. `rebuildCatalog()` is called under the _caller's_ lock -- it does not acquire the mutex. Most call sites hold the write lock, but `LoadUserModels()` and `SaveUserModels()` release it before calling `rebuildCatalog()` -- a known gap.
- **`FirstRunnerRequiredSecrets()` returns secrets from the first _sorted_ runner**, which may not be the user's preferred runner. The API response uses this as a simplification.
- **Third-party models all use `ANTHROPIC_AUTH_TOKEN`** when running via the claude proxy. This is the Anthropic model routing endpoint's auth token, not the provider's own API key.
- **`Capabilities()` is defined on the adapter, not the model.** All models running via a given tool share that tool's capabilities. No per-model capability override.
- **models.dev uses mixed-case IDs** (e.g., `MiniMax-M2.5`). Case-sensitive comparison everywhere.
- **12-month recency filter.** Models older than 12 months are silently dropped from registry results. No grace period or warning.
- **Cache staleness.** If models.dev is unreachable and the cache is missing or has a wrong `schema_version`, only default models are available. The `schemaVersion` constant in `registry.go` must be bumped when the cache format changes.
- **Never edit `types.generated.ts` directly.** Edit Go structs in `internal/api/contracts/`, then run `go run ./cmd/gen-types`.

## Common modification patterns

- **To add a new model:** Nothing to do. Models appear automatically from the registry when a provider profile exists.
- **To add a new provider:** Add a `ProviderProfile` entry in `internal/models/profiles.go`.
- **To add a user-defined model at runtime:** `PUT /api/user-models` with the full models list.
- **To change how a tool executes models:** Edit `BuildRunnerEnv()` in `adapter_<tool>.go`. `ModelFlag()` controls the CLI flag.
- **To change model availability logic:** Edit `Manager.GetCatalog()` in `internal/models/manager.go`.
- **To change which models appear in the spawn wizard:** Flow is `GetCatalog()` -> `ConfigResponse.Models` -> `useConfigForm.models` (filtered) -> SpawnPage.
- **To change model resolution at spawn time:** Edit `Manager.ResolveModel()` or `Manager.ResolveToolForModel()`.
- **To rename or deprecate a model ID:** Add the old ID to `legacyIDMigrations` in `internal/detect/models.go`. Add config migration if the old ID appears in `enabled_models`, `quick_launch` targets, etc.
- **To change registry filtering:** Edit `ParseRegistry()` in `internal/models/registry.go`. The `recencyMonths` constant controls the age cutoff.
- **To change deduplication rules:** Edit `deduplicateModels()` in `internal/models/registry.go`.
- **To modify the API shape:** Edit structs in `internal/api/contracts/config.go`, update `GetCatalog()`, then run `go run ./cmd/gen-types`.
- **To build without registry support:** `go build -tags nomodelregistry ./cmd/schmux`.
