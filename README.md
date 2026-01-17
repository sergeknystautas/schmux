# schmux

## What is schmux?

**Smart Cognitive Hub on tmux**

Orchestrate multiple run targets across tmux sessions with a web dashboard for monitoring and management.

schmux lets you spin up multiple run targets (detected tools like Claude, Codex, Gemini, plus user-defined commands) working on the same task in parallel. Each target runs in its own tmux session on a managed clone of your git repository. A web dashboard lets you spawn sessions, monitor terminal output, and manage workspaces.

## Who is this for?

Have you mastered vibe coding? Did you read and enjoy [Clerky's tweets](https://x.com/bcherny/status/2007179832300581177) about how the author of Claude Code develops? Are you trying to run multiple agents but aren't satisfied with [Gaslight town](https://steve-yegge.medium.com/welcome-to-gas-town-4f25ee16dd04), [Ralph Wiggum](https://medium.com/@joe.njenga/ralph-wiggum-claude-code-new-way-to-run-autonomously-for-hours-without-drama-095f47fbd467), or other agent automation frameworks?

Then it's time for you to work with schmux!

**schmux is built by schmux.** We know no agent framework is perfect, but we're living through the biggest developer force multiplier since the rollout of the TCP/IP driver. We need lots of approaches.

---

## Quick Start

Get up and running with schmux in 5 minutes.

### Prerequisites

You'll need:

1. **Go 1.21+** - [Download here](https://go.dev/dl/) or `brew install go`
2. **Node.js 18+ & npm** - [Download here](https://nodejs.org/) or `brew install node` (for building the React dashboard)
3. **tmux** - [Homepage](https://github.com/tmux/tmux) or `brew install tmux`
4. **git** - Usually pre-installed, or `brew install git`
   - Note: schmux runs git commands locally in your workspaces, so it will work with whatever authentication you have configured (SSH keys, HTTPS tokens, credential helpers, etc.)
5. **Detected tool CLIs** - At least one of:
   - [Claude Code](https://claude.ai/code)
   - Codex
   - Gemini
   - Or any CLI you want to add as a run target

### Installation

```bash
# Clone the repository
git clone https://github.com/yourusername/schmux.git
cd schmux

# Build the React dashboard and binary
go run ./cmd/build-dashboard

# The ./schmux binary is now ready to use
# (Optional) Install to your PATH
mv schmux /usr/local/bin/
```

### First-Time Setup

1. **Start schmux** - It will guide you through creating a config file:
   ```bash
   ./schmux start
   ```

2. **Follow the prompts** to configure:
   - Where to store workspace directories (default: `~/schmux-workspaces`)
   - Git repositories you want to work with
   - Run targets and quick launch presets

3. **Open the dashboard** at `http://localhost:7337`

4. **Spawn your first session** via the web UI

### Manual Configuration

If you prefer to configure manually, create `~/.schmux/config.json`:

```json
{
  "workspace_path": "~/schmux-workspaces",
  "repos": [
    {
      "name": "myproject",
      "url": "git@github.com:user/myproject.git"
    }
  ],
  "run_targets": [
    {
      "name": "glm-4.7-cli",
      "type": "promptable",
      "command": "~/bin/glm-4.7"
    },
    {
      "name": "zsh",
      "type": "command",
      "command": "zsh"
    }
  ],
  "quick_launch": [
    {
      "name": "Review: Kimi",
      "target": "kimi-thinking",
      "prompt": "Please review these changes."
    }
  ],
  "nudgenik": {
    "target": "kimi-thinking"
  },
  "terminal": {
    "width": 120,
    "height": 40,
    "seed_lines": 100
  }
}

NudgeNik uses `nudgenik.target` to select a promptable target (detected tool, variant, or user promptable). If omitted, it defaults to the detected `claude` tool.
```

Then start the daemon:
```bash
./schmux start
./schmux status  # Shows dashboard URL
```

### Common Issues

**Problem**: `tmux is not installed or not accessible`
- **Solution**: Install tmux (`brew install tmux` on macOS)

**Problem**: `config file not found: ~/.schmux/config.json`
- **Solution**: Run `./schmux start` - it will offer to create a config for you

**Problem**: `run target command is required for X`
- **Solution**: Make sure each run target has `name`, `type`, and `command`

**Problem**: Dashboard shows "Disconnected"
- **Solution**: Check if daemon is running with `./schmux status`

**Problem**: I want local config files in each workspace
- **Solution**: Use workspace overlays - see `docs/workspaces.md` for details

## Features

- **Multi-target orchestration** - Run Claude, Codex, and friends simultaneously
- **Multi-target per directory** - Spawn reviewers or subtargets on existing workspaces
- **Workspace management** - Auto git clone/checkout/pull for clean working directories
- **Workspace overlays** - Auto-copy local-only files (`.env`, config) to new workspaces
- **tmux integration** - Each target in its own session, attach anytime
- **Web dashboard** - Watch your targets work (or panic) in real-time
- **Session persistence** - Survives target completion for review and resume

## Status

**v0.5** - Mostly working, occasionally on fire

## Documentation

- [docs/PHILOSOPHY.md](docs/PHILOSOPHY.md) - Product philosophy (source of truth)
- [docs/workspaces.md](docs/workspaces.md) - Workspace management
- [docs/targets.md](docs/targets.md) - Run targets and multi-agent coordination
- [docs/sessions.md](docs/sessions.md) - Session lifecycle and management
- [docs/web.md](docs/web.md) - Web dashboard
- [docs/cli.md](docs/cli.md) - CLI command reference
- [docs/nudgenik.md](docs/nudgenik.md) - NudgeNik feature
- [docs/dev/README.md](docs/dev/README.md) - Contributor guide

## Development

Cross-platform dashboard build helper (runs npm build, with optional Go test/build):

```bash
go run ./cmd/build-dashboard
go run ./cmd/build-dashboard -test -build
go run ./cmd/build-dashboard -skip-install
```

## License

Apache License 2.0 - see [LICENSE](LICENSE)
