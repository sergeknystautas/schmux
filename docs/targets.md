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

Detected tools are always **promptable** and support **models**.

#### 2. User Promptable Commands

User-supplied command lines that accept a prompt as their final argument:

```json
{
  "name": "my-custom-agent",
  "type": "promptable",
  "command": "~/bin/my-agent"
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

## Models

Models are the AI models you can use for spawning sessions. They include native Claude models and third-party providers.

### Native Claude Models

Native models require no configuration—just select them when spawning.

| ID              | Display Name      |
| --------------- | ----------------- |
| `claude-opus`   | claude opus 4.5   |
| `claude-sonnet` | claude sonnet 4.5 |
| `claude-haiku`  | claude haiku 4.5  |

You can also use short aliases: `opus`, `sonnet`, `haiku`.

### Third-Party Models

Third-party models require API secrets to be configured.

| ID                 | Display Name      | Provider    |
| ------------------ | ----------------- | ----------- |
| `kimi-thinking`    | kimi k2 thinking  | Moonshot AI |
| `kimi-k2.5`        | kimi k2.5         | Moonshot AI |
| `glm-4.7`          | glm 4.7           | Z.AI        |
| `minimax`          | minimax m2.1      | MiniMax     |
| `qwen3-coder-plus` | qwen 3 coder plus | DashScope   |

> **Note:** For backward compatibility, `minimax-m2.1` is also accepted as an alias for the `minimax` model.

### Configuration

Third-party models require API secrets. Create `~/.schmux/secrets.json`:

```json
{
  "models": {
    "kimi-thinking": {
      "ANTHROPIC_AUTH_TOKEN": "sk-..."
    },
    "glm-4.7": {
      "ANTHROPIC_AUTH_TOKEN": "..."
    },
    "minimax": {
      "ANTHROPIC_AUTH_TOKEN": "..."
    },
    "qwen3-coder-plus": {
      "ANTHROPIC_AUTH_TOKEN": "..."
    }
  }
}
```

> **See integration docs:**
>
> - [Qwen3 Coder](https://qwen.ai/blog?id=qwen3-coder)
> - [Kimi (Moonshot)](https://platform.moonshot.ai/docs/guide/agent-support)
> - [GLM (Z.AI)](https://docs.z.ai/scenario-example/develop-tools/claude)
> - [MiniMax](https://platform.minimax.io/docs/api-reference/text-anthropic-api)

Provider-scoped secrets are shared across models for a given provider. For example, adding Moonshot secrets once unlocks both Kimi models.

This file is:

- Created automatically when you first configure a model
- Never logged or displayed in the UI
- Read-only to the daemon

### Context Compatibility

Models are available anywhere their base detected tool is allowed:

- Internal use (NudgeNik)
- Spawn wizard
- Quick launch presets

Models do **not** apply to user-supplied run targets.

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

Quick Launch provides one-click execution of shell commands or AI agents with prompts.

### Schema

| Field     | Type   | Description                                        |
| --------- | ------ | -------------------------------------------------- |
| `name`    | string | Display name (required)                            |
| `command` | string | Shell command to run directly                      |
| `target`  | string | Run target (claude, codex, model, or user-defined) |
| `prompt`  | string | Prompt to send to the target                       |

### Rules

- **Shell command**: Set `command` to run a shell command directly
- **AI agent**: Set `target` and `prompt` to spawn an agent with a prompt
- **Either/or**: Use `command` OR `target`+`prompt`, not both

### Examples

```json
{
  "quick_launch": [
    {
      "name": "Run Tests",
      "command": "npm test"
    },
    {
      "name": "Review Changes",
      "target": "claude-sonnet",
      "prompt": "Review these changes for bugs and style issues"
    },
    {
      "name": "Review: Kimi",
      "target": "kimi-thinking",
      "prompt": "Please review these changes."
    }
  ]
}
```

### Global vs Workspace

- **Global**: Define in `~/.schmux/config.json` — available for all repos
- **Workspace**: Define in `<workspace>/.schmux/config.json` — repo-specific presets

Workspace presets are merged with global presets (workspace takes precedence on name conflicts). See [workspaces.md](workspaces.md#workspace-configuration) for details.

---

## Contexts (Where Targets Are Used)

### Internal Use

- Used by schmux itself (e.g., NudgeNik)
- **Restricted to detected tools only** (and their models)
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
  "repos": [{ "name": "myproject", "url": "git@github.com:user/myproject.git" }],
  "run_targets": [
    { "name": "my-custom-agent", "type": "promptable", "command": "/path/to/my-agent" }
  ],
  "quick_launch": [{ "name": "Review: Kimi", "target": "kimi-thinking", "prompt": "..." }]
}
```

**Secrets** (optional): `~/.schmux/secrets.json` for third-party model API keys.
