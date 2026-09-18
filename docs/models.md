# Model Subsystem

## What it does

Manages the catalog of AI models by merging a remote registry, user-defined models, and synthetic defaults, then resolves which CLI tool runs each model and provides the dashboard with model metadata, availability, and user enablement preferences.

## Key files

| File                                                  | Purpose                                                                                                                                                                                                                     |
| ----------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/detect/models.go`                           | `Model` and `RunnerSpec` structs, default models (`claude`, `codex`, `gemini`, `opencode`, `antigravity`), `legacyIDMigrations` map                                                                                         |
| `internal/models/manager.go`                          | `Manager` owns the merged catalog behind a `sync.RWMutex`. `ResolveModel()` picks tool + env, `GetCatalog()` builds the API response, `FindModel()` resolves IDs with legacy fallback, `IsModel()` determines promptability |
| `internal/models/registry.go`                         | Fetches and parses `models.dev/api.json`, filters by tool_call/text/recency/provider, deduplicates alias/dated variants, manages the local cache at `~/.schmux/cache/models-dev.json`                                       |
| `internal/models/registry_disabled.go`                | No-op stubs under `//go:build nomodelregistry` for builds that exclude the registry                                                                                                                                         |
| `internal/models/profiles.go`                         | `ProviderProfile` entries mapping models.dev providers to schmux runners, endpoints, secrets, opencode prefixes, UI category, optional static runner env (`Env`), and `ExtraRunners` (data-driven additional runners)       |
| `internal/models/userdefined.go`                      | `UserModel` struct, load/save from `~/.schmux/user-models.json`, validation rules, conversion to `detect.Model`                                                                                                             |
| `internal/models/antigravity.go`                      | Runtime discovery for `agy` (Antigravity): runs `agy models`, parses into catalog entries, refreshes on a 15-min loop — agy's auth-gated model list is absent from models.dev                                               |
| `internal/api/contracts/config.go`                    | API-facing `Model` struct (id, display_name, provider, configured, runners, required_secrets, context_window, cost, reasoning, release_date), `RunnerInfo`, `ConfigResponse.Runners`                                        |
| `internal/detect/adapter.go`                          | `ToolAdapter` interface: `BuildRunnerEnv(RunnerSpec)`, `ModelFlag()`, `Capabilities()`, `BuildRunnerArgs()`                                                                                                                 |
| `internal/detect/adapter_generic.go`                  | `GenericAdapter`: one implementation of `ToolAdapter` driven by a YAML descriptor; `BuildRunnerEnv` expands the descriptor's `runner_env.when_endpoint` block and overlays `RunnerSpec.Env`                                 |
| `internal/detect/descriptors/*.yaml`                  | Per-tool descriptors (claude, codex, gemini, opencode, ...): detection, flags, capabilities, `runner_env`, `runner_args`, fence domains                                                                                     |
| `internal/models/codex_catalog.go`                    | Codex catalog transformer (derived state from `models.dev` feed + `codex debug models` dump); `WithoutCatalogArgs` (remote arg stripping); regenerated on every registry re-download                                        |
| `internal/detect/commands.go`                         | `BuildCommandParts()` dispatcher -- delegates to adapter's `InteractiveArgs`/`OneshotArgs`/`StreamingArgs` by mode                                                                                                          |
| `internal/detect/tools.go`                            | `IsBuiltinToolName()`, `AgentInstructionConfig`, `GetInstructionPath()`                                                                                                                                                     |
| `internal/config/run_targets.go`                      | Validates command-only `RunTarget` entries (name + command); model validation happens at runtime                                                                                                                            |
| `assets/dashboard/src/routes/config/ModelCatalog.tsx` | Provider-grouped model editor with enable toggles and runner segmented controls                                                                                                                                             |
| `assets/dashboard/src/routes/config/TargetSelect.tsx` | Dropdown that takes `Model[]` and renders options -- no filtering logic, callers pre-filter                                                                                                                                 |
| `assets/dashboard/src/routes/config/useConfigForm.ts` | Derives `modelCatalog` (raw API), `models` (enabled-filtered), and `oneshotModels` (capability-filtered)                                                                                                                    |

## Architecture decisions

### Registry-driven catalog (no hardcoded model list)

Models come from the `models.dev` remote registry, not a hardcoded list. The old `builtinModels` variable (~400 lines, 35 models) has been deleted entirely. Every new model required a code change and release, and third-party models routed through `ANTHROPIC_BASE_URL` were especially painful to maintain.

On first startup with no cache and a failed fetch, only the five default models are available. A warning is logged and the dashboard updates automatically once the fetch succeeds via `catalog_updated` WebSocket event.

**Rejected alternative: keep builtins as fallback.** The initial implementation kept `builtinModels` as a third layer underneath the registry. This was removed because deprecated models persisted, the registry could never be the sole source of truth, and the hardcoded list still needed manual maintenance -- defeating the purpose.

### Four-layer catalog merge

`rebuildCatalog()` in `manager.go` merges four sources. On ID collision, later layers win:

1. **Registry models** (lowest priority) -- from `models.dev/api.json`
2. **User-defined models** -- from `~/.schmux/user-models.json`
3. **Antigravity-discovered models** -- runtime `agy models` output; IDs are `antigravity-`-prefixed so they never collide with other sources
4. **Default models** (highest priority) -- synthetic `claude`, `codex`, `gemini`, `opencode`, `antigravity` entries

The merge is a flat map keyed by model ID. No inheritance or partial override -- a collision means the higher-priority entry completely replaces the lower one.

### Antigravity models discovered at runtime

Antigravity (`agy`) is a multi-model harness whose model list is defined by the tool itself, auth-gated, and absent from models.dev — so unlike every other source, schmux discovers its models by running `agy models` at runtime. `internal/models/antigravity.go` parses the plain-text output (one model per line; no `--json` form exists) into catalog entries: a deterministic `antigravity-`-prefixed ID, the exact display string as both `DisplayName` and runner `ModelValue`, a cosmetic provider derived from the name prefix (`Gemini`→google, `Claude`→anthropic, `GPT`→openai), and a single `antigravity` runner with no secrets or endpoint (agy owns auth).

`StartAntigravityDiscovery` runs the parse once at daemon start (only when agy is among the detected tools) and every 15 minutes, firing `catalog_updated` on change. Signing into agy after the daemon starts surfaces models on the next tick without a restart. A failing run (signed out, timeout, transient error) **keeps the previously discovered list** rather than wiping it — mirroring the registry source, which retains stale models on fetch error — so a single transient failure doesn't yank every antigravity model out of the picker mid-session; the layer only empties when a _successful_ run returns no models. This mirrors the registry source's shape (async fetch → parse → set a catalog layer → rebuild → broadcast) but is local and session-scoped, with no disk cache.

### Provider profiles instead of per-model configuration

Each `ProviderProfile` in `profiles.go` maps a models.dev provider key to a runner, endpoint, secrets, opencode prefix, UI category, and optional static runner env (~10 lines per provider). When a new model appears from an existing provider, it works automatically. Only a new _provider_ requires a code change.

Every registry model gets two runner entries: one for its provider's primary runner using the models.dev ID as `ModelValue`, and one for opencode using `{opencode_prefix}/{model_id}`.

### `ExtraRunners` extends runners without new code paths

A profile can declare additional runners beyond the native and opencode defaults via `ExtraRunners map[string]ExtraRunnerSpec` — the zai provider declares a `codex` extra runner that routes Z.ai GLM models through CodeX's TOML config overrides (`model_provider=zai`, `model_providers.zai.base_url=https://api.z.ai/api/v1`, etc.). `BuildDetectModels` emits each extra runner alongside the defaults. A future provider (kimi, MiniMax) gains a codex runner by adding profile data, not new code paths — the routing is data-driven.

### Resolve once, append everywhere

`ResolvedModel.Args` carries precomputed provider-routing CLI args from `Manager.ResolveModel`; every spawn site (`session.buildCommand` interactive + resume, `session.buildChatCommand`, `floormanager.buildFMCommand`/`buildFMResumeCommand`, `oneshot.ResolveTargetCommand`/`Execute`) appends them, and none re-derives. `models.WithoutCatalogArgs` strips the daemon-local `model_catalog_json=` arg (and its `-c`) for remote spawns so path-free routing/auth args survive but the catalog path does not ride the SSH line.

### Kimi comes from the subscription provider, not the metered one

models.dev exposes Kimi several ways: `moonshotai` (pay-as-you-go, `https://api.moonshot.ai/v1`, priced models like `kimi-k3`) and the subscription plan under two keys — `kimi-code-plan-cn` (`https://api.kimi.com/coding/v1`) and `kimi-code-plan-global` (`https://api.kimi.ai/coding/v1`) — both `cost: 0` with the same model IDs `k3`, `k3-256k`, `kimi-for-coding`, `kimi-for-coding-highspeed`. schmux registers only `kimi-code-plan-cn` (the key was `kimi-for-coding` before models.dev renamed it); registering both plan keys would put duplicate model IDs under the one `moonshot` provider. This mirrors Z.AI, where only `zai-coding-plan` has a profile.

The two are not interchangeable by URL alone — the model IDs differ, so pointing the metered profile at the subscription host would send IDs the endpoint does not serve. The profile's `SchmuxProvider` stays `moonshot` so existing `secrets.json` entries under `providers.moonshot` keep working; one Kimi credential slot, not two. `LegacyModelIDMigrations` maps the three metered IDs that have a like-for-like subscription model (`kimi-k3`→`k3`, `kimi-k2.7-code`→`kimi-for-coding`, `kimi-k2.7-code-highspeed`→`kimi-for-coding-highspeed`); the thinking models have no equivalent and resolve to nothing, rendering as "(unavailable)" in the target picker.

To go back to metered Kimi, restore the `moonshotai` profile entry.

### Default models pass no --model flag

The default models have `ModelValue: ""`. No `--model` flag is passed when spawning. The harness uses its own default, so when a harness promotes a new default, schmux picks it up without knowing the model ID.

### Models decoupled from runners

A `Model` has a `Runners map[string]RunnerSpec` listing which tools can execute it and how. The old 1:1 `BaseTool` binding is gone. This allows the same model to run via either its native runner or opencode.

### Adapters own their flags and env vars

`ModelFlag()` returns the adapter's CLI flag (`--model`, `-m`). `BuildRunnerEnv(spec)` constructs env vars from two sources, later wins: the descriptor's `runner_env.when_endpoint` block (only Claude's descriptor has one; it sets the `ANTHROPIC_*` routing vars when `spec.Endpoint` is non-empty), then `RunnerSpec.Env` (static per-provider vars copied from `ProviderProfile.Env`; today only `zai-coding-plan` sets it, with the Claude Code settings z.ai documents). `Manager.ResolveModel` layers secrets on top, so the full precedence is `when_endpoint < profile Env < secrets`. This replaced the old `Model.BuildEnv()` and `Model.ModelFlag` fields.

### Provider-scoped secrets (not model-scoped)

`secrets.json` has a `providers` map keyed by provider: `{"providers": {"moonshot": {"ANTHROPIC_AUTH_TOKEN": "sk-..."}}}`. A legacy `models` map (keyed by model ID) is migrated to provider-keyed format at load time. With dynamic models, scanning the catalog to infer provider from model ID is fragile. Provider-keyed storage is deterministic regardless of catalog state.

### Registry deduplication

models.dev returns multiple IDs for the same model (alias/dated pairs, `-chat-latest` suffixes, `(latest)` display names). `deduplicateModels()` applies four rules: skip IDs matching provider `SkipIDPatterns`, skip `(latest)` display names, skip `-latest` ID suffixes when a dated variant of the same base exists, and skip dated variants when a shorter alias exists (but only when the suffix is 8+ digits, so `claude-opus-4-1` is NOT deduped by `claude-opus-4`).

### Legacy ID migration

`legacyIDMigrations` maps old schmux IDs to current models.dev IDs. `MigrateModelID()` resolves chains transitively (up to depth 10). Migrations run at config load time for `enabledModels`, `secrets.json`, and session state references.

### Config stores `enabled_models` as `map[string]string`

Maps model ID to preferred tool. If the `models` section is absent, default models for detected tools are implicitly enabled for first-run convenience. Once the dashboard stores a selection, that selection is authoritative: omitted defaults stay disabled, and an explicit empty selection remains empty across reloads.

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

### CodeX catalog is derived, not vendored

CodeX's bundled catalog lacks third-party providers, so without a generated catalog a Z.ai GLM session sees a "fallback metadata" line and CodeX mis-sizes the 1M-token context window for truncation. `internal/models/codex_catalog.go` regenerates `<schmux_dir>/cache/codex-models-<provider>.json` from the fresh models.dev registry feed on every successful `fetchAndUpdate` — the same cycle that re-downloads `models-dev.json`. The transformer (`BuildCodexCatalog`, `ExtractBaseInstructions`) lifts `base_instructions` from `codex debug models` (which runs without auth) so the agent system prompt stays in sync with the installed CodeX. Models without a context window are skipped (a wrong window mis-sizes truncation, which is the failure the catalog exists to prevent). Regeneration is best-effort: a failing dump or generation logs a warning and leaves the previous file in place — sessions degrade to fallback metadata rather than failing to launch. The startup cache-load path does not regenerate; it reuses the previous cycle's file until the startup fetch completes.

## Gotchas

- **Hot-swap concurrency.** The merged catalog is swapped behind a `sync.RWMutex`. `rebuildCatalog()` is called under the _caller's_ lock -- it does not acquire the mutex. Most call sites hold the write lock, but `LoadUserModels()` and `SaveUserModels()` release it before calling `rebuildCatalog()` -- a known gap.
- **`FirstRunnerRequiredSecrets()` returns secrets from the first _sorted_ runner**, which may not be the user's preferred runner. The API response uses this as a simplification.
- **Third-party models all use `ANTHROPIC_AUTH_TOKEN`** when running via the claude proxy. This is the Anthropic model routing endpoint's auth token, not the provider's own API key.
- **`Capabilities()` is defined on the adapter, not the model.** All models running via a given tool share that tool's capabilities. No per-model capability override.
- **models.dev uses mixed-case IDs** (e.g., `MiniMax-M2.5`). Case-sensitive comparison everywhere.
- **12-month recency filter.** Models older than 12 months are silently dropped from registry results. No grace period or warning.
- **Cache staleness.** If models.dev is unreachable and the cache is missing or has a wrong `schema_version`, only default models are available. The `schemaVersion` constant in `registry.go` must be bumped when the cache format changes.
- **Never edit `types.generated.ts` directly.** Edit Go structs in `internal/api/contracts/`, then run `go run ./cmd/gen-types`.
- **Default runner resolution sort is alphabetical.** `ResolveToolForModel` honors `PreferredTool`, else picks the first sorted runner key — so GLM models keep defaulting to claude (`claude < codex < opencode`) unless the user explicitly picks codex. Adding a codex `ExtraRunner` does not change the default for any existing user.
- **User-defined codex models get `runner_args` for free.** A user model with `runner: "codex"` and an endpoint builds the right `RunnerSpec` through the standard path; the descriptor's `when_endpoint` template and pair-drop rule apply via the adapter, with `{provider}` from the entry's `provider` field (defaulting to `"custom"`). A user codex model with an endpoint but no required secrets emits no `env_key` pair — the drop happens before the arg reaches the shell.
- **CodeX chat readiness keys on `requiresOpenaiAuth`.** A `account/read` response of `{"account":null,"requiresOpenaiAuth":false}` means CodeX is ready and needs no OpenAI auth — a provider-routed session (Z.ai GLM via codex) is `Addressable` once the thread id exists. `account:null` without the field, or `requiresOpenaiAuth:true`, stays in today's logged-out reading (first-party behavior unchanged). An unparseable result is treated as no answer, never as logged in.
- **CodeX `model_catalog_json=` is path-statted twice.** `BuildRunnerArgs` drops the override (and its `-c`) when the daemon-local file is absent or `os.Stat` fails — covers undetected CodeX, failed catalog generation, and missing cache. `models.WithoutCatalogArgs` then strips the same arg for remote spawns at the session-manager call site, so the daemon-local path never rides the SSH line. Routing and auth args (path-free) travel unchanged.

## Common modification patterns

- **To add a new model:** Nothing to do. Models appear automatically from the registry when a provider profile exists.
- **To add a new provider:** Add a `ProviderProfile` entry in `internal/models/profiles.go`. Set `Env` if the provider documents extra Claude Code settings (timeouts, compaction window).
- **To route a provider's models through CodeX:** Add an `ExtraRunners["codex"]` entry to the provider's `ProviderProfile` with `ModelTemplate`, `Endpoint`, and `RequiredSecrets` (use the env name the provider's secret already lives under so `IsConfigured` reuses the existing entry). The codex descriptor already carries the `runner_args.when_endpoint` block; `BuildDetectModels` emits the runner; `ResolveModel` precomputes the args. No new code paths. If the provider needs its own catalog, add a `codex` extra runner to its profile and the next registry refresh regenerates `<schmux_dir>/cache/codex-models-<provider>.json`.
- **To change the codex catalog transformer:** Edit `BuildCodexCatalog` / `codexCatalogEntry` in `internal/models/codex_catalog.go`. New required fields show up as loud CodeX launch failures ("missing field X") rather than silent degradation — adjust the struct and the test in `codex_catalog_test.go`. `base_instructions` lifts from the installed CodeX, so agent-prompt drift is absorbed automatically.
- **To add a user-defined model at runtime:** `PUT /api/user-models` with the full models list.
- **To change how a tool executes models:** Edit the tool's descriptor in `internal/detect/descriptors/<tool>.yaml` (`runner_env.when_endpoint` for env, `model_flag` for the CLI flag). Provider-specific env goes in `ProviderProfile.Env`, not the descriptor.
- **To change model availability logic:** Edit `Manager.GetCatalog()` in `internal/models/manager.go`.
- **To change which models appear in the spawn wizard:** Flow is `GetCatalog()` -> `ConfigResponse.Models` -> `useConfigForm.models` (filtered) -> SpawnPage.
- **To change model resolution at spawn time:** Edit `Manager.ResolveModel()` or `Manager.ResolveToolForModel()`.
- **To rename or deprecate a model ID:** Add the old ID to `legacyIDMigrations` in `internal/detect/models.go`. Add config migration if the old ID appears in `enabled_models`, `quick_launch` targets, etc.
- **To change registry filtering:** Edit `ParseRegistry()` in `internal/models/registry.go`. The `recencyMonths` constant controls the age cutoff.
- **To change deduplication rules:** Edit `deduplicateModels()` in `internal/models/registry.go`.
- **To modify the API shape:** Edit structs in `internal/api/contracts/config.go`, update `GetCatalog()`, then run `go run ./cmd/gen-types`.
- **To build without registry support:** `go build -tags nomodelregistry ./cmd/schmux`.
