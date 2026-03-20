# Model Subsystem

## What it does

Manages the catalog of AI models, resolves which CLI tool runs each model, and provides the dashboard with model metadata, availability, and user enablement preferences.

## Key files

| File                                                  | Purpose                                                                                                                                                                             |
| ----------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/detect/models.go`                           | Model and RunnerSpec structs, default\_\* models, legacy ID migration map                                                                                                           |
| `internal/models/manager.go`                          | `Manager` owns resolution logic: `ResolveModel()` picks tool + env, `GetCatalog()` builds the API response, `IsModel()` determines promptability                                    |
| `internal/api/contracts/config.go`                    | API-facing `Model` struct (slim: id, display_name, provider, configured, runners as `[]string`, required_secrets), `RunnerInfo` (available, capabilities), `ConfigResponse.Runners` |
| `internal/detect/adapter.go`                          | `ToolAdapter` interface including `BuildRunnerEnv(RunnerSpec)`, `ModelFlag()`, `Capabilities()`                                                                                     |
| `internal/detect/adapter_claude.go`                   | Claude adapter: `BuildRunnerEnv` sets proxy env vars (`ANTHROPIC_BASE_URL`, `ANTHROPIC_MODEL`, tier overrides) when `spec.Endpoint` is non-empty                                    |
| `internal/detect/adapter_codex.go`                    | Codex adapter: returns empty env from `BuildRunnerEnv`, uses `-m` as model flag                                                                                                     |
| `internal/detect/adapter_gemini.go`                   | Gemini adapter: interactive-only capabilities, `--model` flag                                                                                                                       |
| `internal/detect/adapter_opencode.go`                 | OpenCode adapter: universal runner for 75+ providers via `provider/model` syntax, returns empty env from `BuildRunnerEnv`                                                           |
| `internal/detect/commands.go`                         | `BuildCommandParts()` dispatcher -- delegates to adapter's `InteractiveArgs`/`OneshotArgs`/`StreamingArgs` by mode                                                                  |
| `internal/detect/tools.go`                            | `IsBuiltinToolName()`, `AgentInstructionConfig`, `GetInstructionPath()`                                                                                                             |
| `internal/config/run_targets.go`                      | Validates command-only `RunTarget` entries (name + command); model validation happens at runtime, not config load                                                                   |
| `assets/dashboard/src/routes/config/ModelCatalog.tsx` | Provider-grouped model editor with enable toggles and runner segmented controls                                                                                                     |
| `assets/dashboard/src/routes/config/TargetSelect.tsx` | Dropdown that takes `Model[]` and renders options -- no filtering logic, callers pre-filter                                                                                         |
| `assets/dashboard/src/routes/config/useConfigForm.ts` | Derives `modelCatalog` (raw API), `models` (enabled-filtered), and `oneshotModels` (capability-filtered)                                                                            |
| `assets/dashboard/src/lib/types.generated.ts`         | Auto-generated TypeScript types from Go contracts; never edit directly                                                                                                              |

## Architecture decisions

- **Models are decoupled from runners.** A `Model` has a `Runners map[string]RunnerSpec` listing which tools can execute it and how. The old 1:1 `BaseTool` binding is gone. This allows the same model (e.g., `claude-sonnet-4-6`) to run via either `claude` or `opencode`.
- **Adapters own their flags and env vars.** `ModelFlag()` returns the adapter's CLI flag (`--model`, `-m`). `BuildRunnerEnv(spec)` constructs env vars (only Claude does anything non-trivial here -- proxy endpoint vars). This replaced `Model.BuildEnv()` and `Model.ModelFlag` fields.
- **Config stores `enabled_models` as `map[string]string` (model ID to preferred tool).** When empty, all models with a detected runner appear in the spawn wizard (backward compat). Once a user explicitly enables any model, only enabled models appear.
- **API response keeps models slim.** The `contracts.Model` struct has `runners` as `[]string` (just tool names), not the full `RunnerSpec`. Per-runner details (availability, capabilities) live in a top-level `runners` map on `ConfigResponse`, defined once per tool.
- **Secrets are per-runner, not per-model.** `RunnerSpec.RequiredSecrets` means the same model may need secrets via one tool (claude proxy) but not another (opencode native auth). The API response surfaces `required_secrets` at model level using the first runner's requirements as a simplification.
- **Legacy model IDs are migrated at lookup time.** `legacyIDMigrations` maps old aliases (`"opus"`, `"claude-sonnet"`) to current vendor IDs (`"claude-opus-4-6"`, `"claude-sonnet-4-6"`). `FindModel()` applies migration transparently.
- **`IsModel()` determines promptability at runtime, not config time.** A target is "promptable" if it is a model ID or a builtin tool name. Command targets (user-defined `RunTarget` entries with name + command) are not promptable. The old bridge that converted models into fake `RunTarget{type:"promptable"}` entries is deleted.
- **`run_targets` is a command-only concept.** The `RunTarget` struct has only `name` and `command`. The `Type`, `Source`, and `RunTargetTypePromptable` constants were removed in the kill-bridge cleanup. Models travel through `models` and `enabled_models` in the API, not through `run_targets`.
- **Catalog is registry-driven.** Models come from the models.dev registry (cached locally, refreshed daily). User-defined models override registry entries. Default models (`default_*`) are generated in code. The old `builtinModels` list has been deleted.
- **Frontend derives three model lists from one source.** `modelCatalog` (raw from API, used by ModelCatalog editor), `models` (filtered to enabled, used by SpawnPage), `oneshotModels` (further filtered to tools with oneshot capability, used by config tabs that select targets for oneshot features).

## Gotchas

- **`IsModel()` still has a `GetRunTarget` fallback at `manager.go:290`.** The dead-code cleanup spec flagged this for removal (IsModel should only check model IDs and builtin tool names, not command targets). It still exists. Removing it requires verifying callers that depend on the `(false, true)` return for command targets.
- **`FirstRunnerRequiredSecrets()` returns secrets from the first sorted runner, which may not be the user's preferred runner.** The API response uses this as a simplification. The ModelCatalog UI shows the correct secrets requirements based on the actual runner.
- **`opencode-zen` model has `ModelValue: ""`** (empty string). This means opencode uses its default Zen free-tier model. The empty value is intentional -- do not add a model value.
- **Third-party models use the same `ANTHROPIC_AUTH_TOKEN` secret** across providers (Moonshot, Z.AI, MiniMax, Dashscope) when running via the claude proxy. This is the Anthropic model routing endpoint's auth token, not the provider's own API key.
- **`Capabilities()` is defined on the adapter, not the model.** All models running via a given tool share that tool's capabilities. There is no per-model capability override.
- **Never edit `types.generated.ts` directly.** Edit Go structs in `internal/api/contracts/`, then run `go run ./cmd/gen-types`.

## Common modification patterns

- **To add a new model:** Models appear automatically from the registry when a provider profile exists in `internal/models/profiles.go`. No code changes needed for individual models.
- **To add a new provider:** Add a `ProviderProfile` entry in `internal/models/profiles.go`. Models from that provider will appear automatically from the registry.
- **To change how a tool executes models (env vars, flags):** Edit the tool's `BuildRunnerEnv()` method in `adapter_<tool>.go`. The `ModelFlag()` method controls which CLI flag is used.
- **To change model availability logic:** Edit `Manager.GetCatalog()` in `internal/models/manager.go`. This builds the `contracts.Model` list and determines the `Configured` flag.
- **To change which models appear in the spawn wizard:** The flow is `GetCatalog()` (backend) -> `ConfigResponse.Models` (API) -> `useConfigForm.models` (filtered by enablement) -> SpawnPage's `availableModels` memo.
- **To change model resolution at spawn time:** Edit `Manager.ResolveModel()` or `Manager.ResolveToolForModel()` in `internal/models/manager.go`.
- **To rename or deprecate a model ID:** Add the old ID to `legacyIDMigrations` in `internal/detect/models.go`. Add a config migration if the old ID appears in `enabled_models`, `quick_launch` targets, or other config fields.
- **To modify the API shape of `Model` or `RunnerInfo`:** Edit structs in `internal/api/contracts/config.go`, update `GetCatalog()` in `manager.go` to populate new fields, then run `go run ./cmd/gen-types` to regenerate TypeScript types.
- **To add a capability (e.g., a new tool mode):** Add the string to the adapter's `Capabilities()` return value. The `oneshotModels` derivation in `useConfigForm.ts` checks for `"oneshot"` in the preferred runner's capabilities.
- **To filter TargetSelect dropdowns differently:** The filtering happens in `useConfigForm.ts` (`models` and `oneshotModels` memos), not in `TargetSelect.tsx` itself. Pass the appropriate pre-filtered list.
