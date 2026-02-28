# Model Configuration Editor

## Problem

The Sessions tab in config mixes four unrelated concerns: detected run targets (read-only), models (passive list), promptable targets (user-defined agents), and command targets (shell shortcuts). The models section shows what the system auto-detected but gives the user no control over which models are active or which tool runs each one. The `enabled_models` config map and multi-runner backend are fully wired but have no UI.

## Goal

Replace the first three sections of the Sessions tab (Detected Run Targets, Models, Promptable Targets) with a single model configuration editor. The user explicitly picks which models are enabled and which detected tool runs each one. Command Targets stays unchanged. The spawn wizard is completely untouched.

## Design

### Data Model

The data model is 1:1 — each enabled model maps to exactly one tool:

```json
{
  "models": {
    "enabled": {
      "claude-opus-4-6": "claude",
      "claude-sonnet-4-6": "opencode",
      "kimi-thinking": "opencode",
      "gpt-5.3-codex": "codex"
    }
  }
}
```

This is the existing `enabled_models` config (`map[string]string` of modelID → tool name), already wired through the backend, API contracts, and config persistence. No backend data model changes needed.

### Layout

Models are grouped by provider. Each provider is a collapsible section. Within each provider, models are listed with an enable/disable toggle and a runner picker.

```
┌─────────────────────────────────────────────────────────┐
│ Models                                                  │
│ Enable models and choose which tool runs each one.      │
│                                                         │
│ ▾ Anthropic                                             │
│ ┌─────────────────────────────────────────────────────┐ │
│ │ [x] Claude Opus 4.6          [claude | opencode]    │ │
│ │ [ ] Claude Opus 4            [claude | opencode]    │ │
│ │ [x] Claude Sonnet 4.6        [claude | opencode]    │ │
│ │ [ ] Claude Sonnet 4.5        [claude | opencode]    │ │
│ │ [ ] Claude Sonnet 4          [claude | opencode]    │ │
│ │ [x] Claude Haiku 4.5         [claude | opencode]    │ │
│ └─────────────────────────────────────────────────────┘ │
│                                                         │
│ ▾ OpenAI                                                │
│ ┌─────────────────────────────────────────────────────┐ │
│ │ [x] GPT 5.3 Codex            [codex]                │ │
│ │ [ ] GPT 5.2 Codex            [codex]                │ │
│ │ [ ] GPT 5.1 Codex Max        [codex]                │ │
│ │ [ ] GPT 5.1 Codex Mini       [codex]                │ │
│ └─────────────────────────────────────────────────────┘ │
│                                                         │
│ ▸ Moonshot  (requires secrets)                          │
│ ▸ Z.AI  (requires secrets)                              │
│ ▸ MiniMax  (requires secrets)                           │
│ ▸ Dashscope  (requires secrets)                         │
│                                                         │
│ ▾ OpenCode                                              │
│ ┌─────────────────────────────────────────────────────┐ │
│ │ [x] OpenCode Zen (free)       [opencode]            │ │
│ └─────────────────────────────────────────────────────┘ │
│                                                         │
│ ▸ Google  ░░░ no tools detected ░░░                     │
│                                                         │
├─────────────────────────────────────────────────────────┤
│ Command Targets (unchanged)                             │
│ ...                                                     │
└─────────────────────────────────────────────────────────┘
```

### Behaviors

#### Provider Groups

- Each provider is a collapsible section with a chevron header.
- Providers with detected tools start expanded. Providers needing secrets or with no detected tools start collapsed.
- A provider with NO detected tools for any of its models is shown greyed out with a "no tools detected" hint. The user can see what's possible but can't interact. This covers the future case of showing install prompts.
- A provider whose models all require secrets shows "(requires secrets)" next to the provider name when collapsed.

#### Enable Toggle

- Checkbox per model row. Checked = model appears in the spawn wizard.
- Checking a model adds it to `enabled_models` with the currently selected runner tool.
- Unchecking removes it from `enabled_models`.
- When `enabled_models` is empty (fresh install / no user choices yet), all models with a detected runner are available in spawn (backward compat, existing behavior). Once the user enables anything explicitly, only enabled models appear.

#### Runner Segmented Control

- Only shows runners whose tools are actually detected on the system. Undetected tools do not appear.
- When a model has exactly one detected runner, the segmented control is a static label (no choice to make).
- When a model has 2+ detected runners (e.g., claude-opus-4-6 can run via `claude` or `opencode`), it's a clickable segmented control. The selected segment is the tool stored in `enabled_models`.
- Changing the runner updates `enabled_models` for that model.

#### Secrets

- Third-party providers (Moonshot, Z.AI, MiniMax, Dashscope) require secrets to use via the claude tool's proxy API.
- When a provider section is expanded and its models need secrets, the existing secrets modal flow (Add/Update/Remove) appears — same pattern as the current Models section, just in the new grouped layout.
- Through opencode, many of these models need no secrets (opencode manages its own auth). The secrets requirement is per-runner, already modeled in `RunnerSpec.RequiredSecrets`.

#### Backward Compatibility

- If `enabled_models` is empty, the system behaves as today: all models with a detected runner appear in spawn.
- First explicit user toggle transitions to explicit mode.

### What Gets Deleted

1. **"Detected Run Targets" section** — the read-only tool list. Runner availability is now shown implicitly via the per-model segmented controls.
2. **"Models" section** — the passive model list with secrets buttons. Replaced by the editor.
3. **"Promptable Targets" section** — models ARE the promptable targets for the spawn wizard. User-defined custom promptable commands are out of scope for this iteration (they can be re-added later if needed, possibly under Command Targets).

### What Stays

- **Command Targets section** — unchanged, stays at the bottom of the tab.

### What Does NOT Change

- **Spawn wizard** — completely untouched. It reads whatever the backend resolves from `enabled_models`.
- **Backend data model** — `enabled_models map[string]string` already exists and is wired.
- **API contracts** — `Model` with `runners`, `preferred_tool`, `configured` already exist.
- **Model resolution** — `ResolveTarget` already respects `PreferredTool` from config.

## Catalog Expansion

The hardcoded `builtinModels` in `internal/detect/models.go` needs more model versions. Current catalog has 16 models. Expansion:

### Anthropic (add older versions)

- `claude-opus-4` — Claude Opus 4
- `claude-sonnet-4-5` — Claude Sonnet 4.5
- `claude-sonnet-4` — Claude Sonnet 4
- `claude-sonnet-3-5` — Claude Sonnet 3.5
- `claude-haiku-3-5` — Claude Haiku 3.5

All Anthropic models get runners for both `claude` and `opencode`.

### Google (new provider group)

- `gemini-2.5-pro` — Gemini 2.5 Pro
- `gemini-2.5-flash` — Gemini 2.5 Flash
- `gemini-2.0-flash` — Gemini 2.0 Flash

Google models get a runner for `gemini` (and possibly `opencode` if it supports Google providers).

### OpenAI (verify completeness)

Confirm current codex model list is complete. Add any missing versions.

## Implementation Scope

### Frontend Changes

- **Replace**: `SessionsTab.tsx` internals — remove 3 sections, add model editor
- **New component**: `ModelCatalog.tsx` (or inline in SessionsTab) — provider-grouped model list with toggles and segmented controls
- **Update**: `useConfigForm.ts` — track `enabledModels` state, dispatch enable/disable/runner-change actions
- **Update**: `ConfigPage.tsx` — adjust props passed to SessionsTab (remove promptable/detected target props, simplify)
- **CSS**: Styles for provider groups, model rows, segmented controls, disabled states

### Backend Changes

- **Expand**: `builtinModels` in `models.go` — add older Anthropic versions, Google models
- **No other backend changes** — the `enabled_models` config, API contracts, and model resolution are already wired.

### Tests

- Update `SessionsTab.test.tsx` — new test cases for the editor
- Update `useConfigForm.test.ts` — enabled models state management
- Update `models_test.go` — verify expanded catalog
