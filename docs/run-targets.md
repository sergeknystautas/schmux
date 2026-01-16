# Run Targets & Contexts

This spec defines the execution model for schmuX: what we can run (Run Targets) and how we use them (Contexts). The goal is to support detected tools with variants, user-supplied commands, and quick-launch presets without mixing concerns.

## Goals

- Clearly separate **what** can be executed (Run Targets) from **how** we invoke them (Contexts).
- Allow **detected tools** to have multiple invocation modes and **variants**.
- Support **user promptable commands** (arbitrary shells that accept a prompt).
- Support **user command-only** targets (no prompt).
- Make **quick-launch** a saved preset over a Run Target, not a new kind of target.

## Definitions

### Run Target (what we can run)

A Run Target is the unit of execution. It has a command and optional prompt capability.

Three types:

1) **Detected Tool**
   - Officially supported and auto-detected.
   - Can define multiple **modes** (interactive vs oneshot).
   - Can support **variants** (provider/model overrides).

2) **User Promptable Command**
   - User-supplied command line that **accepts a prompt**.
   - No variants. No internal use.

3) **User Command**
   - User-supplied command line that **does not accept a prompt**.
   - No variants. No internal use.

### Context (how we use a Run Target)

Context = where/how a target is used. Mode = which command form a detected tool uses (interactive vs oneshot).

A Context determines which Run Targets are allowed and which command form to use.

Contexts:

1) **Internal Use**
   - Used by schmuX itself for background or one-shot tasks.
   - **Restricted to Detected Tools only** (and their variants).
   - Uses **oneshot** mode for detected tools.
   - NudgeNik uses `nudgenik.target` (must be promptable); defaults to detected `claude`.

2) **Wizard**
   - Interactive flow for users to start a session.
   - Can use **any Run Target**.
   - For detected tools, uses **interactive** mode.

3) **Quick Launch**
   - User-configured preset (saved run).
   - Can use **any Run Target**.
   - Must include a prompt if target is promptable.
   - For detected tools, uses **interactive** mode.

## Detected Tools

Detected tools are official and have structured command modes. Each tool defines:

- `interactiveCmd` (spawn in interactive shell)
- `oneshotCmd` (prompt-in, immediate output)
- `promptable` (true)
- `variantsAllowed` (true)

Example mapping:

- Claude:
  - interactive: `claude`
  - oneshot: `claude -p`

- Gemini:
  - interactive: `gemini -i`
  - oneshot: `gemini`

## Variants (detected tools only)

A Variant is a preconfigured profile over a detected tool:

- Binds environment variables or flags for provider/model
- Must reference a detected tool
- Must be usable in any context where the base tool is allowed

Variant applies **only** to detected tools. User-supplied commands cannot be variantized.

## Run Target Rules

Prompt capability:

- Detected tools are always promptable.
- User promptable commands must explicitly declare promptable = true.
- User command targets are promptable = false.

Invocation rules:

- If target is promptable, prompt text may be passed.
- If target is not promptable, prompt text is invalid.

## Quick Launch Presets

Quick Launch is a saved *run*, not a new target.

A Quick Launch preset stores:

- `targetRef` (which Run Target)
- `prompt` (required if promptable)

Quick Launch should not mutate or redefine targets; it selects and configures them.

## Examples

### Example 1: Detected Tool + Variant

Target:
- Detected Tool: `claude`
- Variant: `glm-4.7`

Context usage:
- Internal: `claude -p` with variant env
- Wizard: `claude` with variant env
- Quick Launch: `claude` with variant env

### Example 2: User Promptable Command

Target:
- Command: `~/bin/glm-4.7-cli`
- Promptable: true

Context usage:
- Internal: not allowed
- Wizard: allowed
- Quick Launch: allowed with prompt

### Example 3: User Command

Target:
- Command: `zsh`
- Promptable: false

Context usage:
- Internal: not allowed
- Wizard: allowed (command-only)
- Quick Launch: allowed (command-only)

## UI / UX Implications (high level)

- Wizard should clearly show target types and prompt capability.
- Quick Launch setup should require prompts when promptable.
- Internal use should only list detected tools (with variants).

## Open Questions

- Do we need a way to declare user promptable commands as oneshot vs interactive?
- Should Quick Launch allow per-run env overrides beyond variant env?
- Should we allow detected tools to override interactive/oneshot commands per user config?
