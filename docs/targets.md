# Targets (Multi-Agent Coordination)

**Problem:** The agentic coding landscape is fragmented—Claude, Codex, Gemini, and more. Each has strengths. Locking into one vendor limits your options, and switching between tools manually is friction that slows you down.

---

## Run Targets

A Run Target is what you can execute—any AI coding tool or command.

### Three Types of Run Targets

#### 1. Detected Tools
Officially supported and auto-detected tools with built-in knowledge:

- **Claude** (`claude`) — Anthropic's coding agent
- **Codex** (`codex`) — OpenAI's coding agent
- **Gemini** (`gemini`) — Google's coding agent

Each detected tool has two command modes:
- **Interactive**: Spawns an interactive shell (e.g., `claude`)
- **Oneshot**: Prompt-in, immediate output (e.g., `claude -p`)

Detected tools are always **promptable** and support **variants**.

#### 2. User Promptable Commands
User-supplied command lines that accept a prompt as their final argument:

```json
{
  "name": "glm-4.7",
  "type": "promptable",
  "command": "~/bin/glm-4.7"
}
```

#### 3. User Commands
User-supplied command lines that do not accept prompts (shell scripts, tools):

```json
{
  "name": "zsh",
  "type": "command",
  "command": "zsh"
}
```

---

## Variants

Variants are **profiles over detected tools** that redirect to alternative providers or models via environment variables.

### Supported Variants

| Name | Provider | Base Tool |
|------|----------|-----------|
| `kimi-thinking` | Moonshot AI | claude |
| `glm-4.7` | Z.AI | claude |
| `minimax` | MiniMax | claude |

All variants apply environment variables like `ANTHROPIC_BASE_URL` and `ANTHROPIC_AUTH_TOKEN` to redirect requests.

### Configuration

Variants are **not** configured in `config.json`. The registry is fixed and tied to detected tools. Users only provide API secrets.

Create `~/.schmux/secrets.json` to store variant API keys:

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
- Created automatically when you first configure a variant
- Never logged or displayed in the UI
- Read-only to the daemon

### Context Compatibility

Variants are available anywhere their base detected tool is allowed:
- Internal use (NudgeNik)
- Spawn wizard
- Quick launch presets

Variants do **not** apply to user-supplied run targets.

---

## Built-in Commands

schmux includes a library of pre-defined command templates for common AI coding tasks:

- **code review - local**: Review local changes
- **code review - branch**: Review current branch
- **git commit**: Create a thorough git commit
- **merge in main**: Merge main into current branch

Built-in commands:
- Appear in both the spawn dropdown and spawn wizard
- Are merged with user-defined commands (built-ins take precedence on duplicate names)
- Work in production (installed binary) and development

---

## User-Defined Run Targets

Define your own commands in `~/.schmux/config.json`:

```json
{
  "run_targets": [
    {
      "name": "my-custom-agent",
      "type": "promptable",
      "command": "/path/to/my-agent"
    },
    {
      "name": "shell",
      "type": "command",
      "command": "zsh"
    }
  ]
}
```

**Rules:**
- `type = "promptable"` requires the target accepts the prompt as the final argument
- `type = "command"` means no prompt is allowed
- Detected tools do **not** appear in `run_targets` (they're built-in)

---

## Quick Launch Presets

Quick Launch saves combinations of target + prompt for one-click execution:

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

**Rules:**
- Prompt must be set if target is promptable
- Only command targets may use `null` for prompt
- Resolve order: variant → detected tool → user target

---

## Contexts (Where Targets Are Used)

### Internal Use
- Used by schmux itself (e.g., NudgeNik)
- **Restricted to detected tools only** (and their variants)
- Uses **oneshot** mode

### Wizard
- Interactive flow for spawning sessions
- Can use **any run target**
- For detected tools, uses **interactive** mode

### Quick Launch
- User-configured presets
- Can use **any run target**
- Must include a prompt if target is promptable
- For detected tools, uses **interactive** mode

---

## Configuration Structure

```json
{
  "workspace_path": "~/schmux-workspaces",
  "repos": [
    {"name": "myproject", "url": "git@github.com:user/myproject.git"}
  ],
  "run_targets": [
    {"name": "glm-4.7", "type": "promptable", "command": "/path/to/glm-4.7"}
  ],
  "quick_launch": [
    {"name": "Review: Kimi", "target": "kimi-thinking", "prompt": "..."}
  ]
}
```

**Secrets** (optional): `~/.schmux/secrets.json` for variant API keys.
