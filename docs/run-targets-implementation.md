# Run Targets Implementation Notes

This document translates the Run Target + Context model into concrete implementation decisions.

## Scope

- Replaces the current agents/commands structure in `config.json`.
- Hard cutover: no backward compatibility or migration.
- Internal use is restricted to detected tools (oneshot only).
- Wizard and Quick Launch are interactive only.

## Config: Run Targets

Run Targets live in `config.json` and define **user-supplied** targets only. Detected tools are not editable in config.

Proposed structure:

```json
{
  "run_targets": [
    {
      "name": "glm-4.7",
      "type": "promptable",
      "command": "~/bin/glm-4.7"
    },
    {
      "name": "zsh",
      "type": "command",
      "command": "zsh"
    }
  ]
}
```

### Rules

- `type = "promptable"` requires that the target accepts the prompt as the final argument.
- `type = "command"` means no prompt is allowed.
- Detected tools do **not** appear in `run_targets`.

## Detected Tools (built-in)

Detected tools are official and hardcoded. They provide commands for each mode:

| Tool | interactiveCmd | oneshotCmd |
|------|-----------------|------------|
| claude | `claude` | `claude -p` |
| codex | `codex` | `codex exec` |
| gemini | `gemini -i` | `gemini` |

Rules:
- Detected tools are always promptable.
- Detected tools cannot be edited by the user.

## Variants

Variants apply only to detected tools. They are not user-configurable except for secrets.

Rules:
- Variant env is fixed and known; only API secrets are user-provided.
- No per-run overrides.
- Variants are valid anywhere their base tool is allowed.

## Secrets

- Stored in `~/.schmux/secrets.json`.
- Keys are variant name → secret map.
- Secrets are required for any variant run.

## Contexts

### Internal

- Allowed targets: detected tools (and their variants) only.
- Mode: oneshot only.

### Wizard

- Allowed targets: detected tools, user promptable, user command.
- Mode: interactive only.
- Prompt: required for promptable; forbidden for command targets.

### Quick Launch

- Allowed targets: detected tools, user promptable, user command.
- Mode: interactive only.
- Prompt: required for promptable targets and **preset** (one-click, no editing at launch).

## Prompt Handling

- For user promptable targets, prompt is appended as the final argument.
- No flags, no stdin, no alternate prompt formats.

## Quick Launch Presets

Quick Launch is a saved run configuration, not a new target type.

Proposed structure:

```json
{
  "quick_launch": [
    {
      "name": "Review: Kimi",
      "target": "kimi-thinking",
      "prompt": "Please review these changes."
    },
    {
      "name": "Shell",
      "target": "zsh",
      "prompt": null
    }
  ]
}
```

Rules:
- Resolve `target` by lookup order: variant → detected tool → user target.
- No prompt editing at launch.
- Prompt must be set if target is promptable; only command targets may use `null`.

## Open Questions (deferred)

- Should quick launch support prompt editing in the future?
- Should user promptable targets support alternate prompt injection methods?
