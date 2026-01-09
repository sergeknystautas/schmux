# schmux

## What is schmux?

**Smart Cognitive Hub on tmux**

Orchestrate multiple AI coding agents across tmux sessions with a web dashboard for monitoring and management.

schmux lets you spin up multiple AI coding agents (Claude, Codex, and others) working on the same task in parallel. Each agent runs in its own tmux session on a managed clone of your git repository. A web dashboard lets you spawn sessions, monitor terminal output, and manage workspaces.

## Features

- **Multi-agent orchestration** - Run Claude, Codex, and friends simultaneously
- **Multi-agent per directory** - Spawn reviewers or subagents on existing workspaces
- **Workspace management** - Auto git clone/checkout/pull for clean working directories
- **tmux integration** - Each agent in its own session, attach anytime
- **Web dashboard** - Watch your agents work (or panic) in real-time
- **Session persistence** - Survives agent completion for review and resume

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
2. **tmux** - [Homepage](https://github.com/tmux/tmux) or `brew install tmux`
3. **git** - Usually pre-installed, or `brew install git`
4. **AI agent CLIs** - At least one of:
   - [Claude Code](https://claude.ai/code) (Anthropic's official CLI)
   - [Codex](https://github.com/xyz) (or your preferred agent)
   - Any CLI that takes a prompt as an argument

### Installation

```bash
# Clone the repository
git clone https://github.com/yourusername/schmux.git
cd schmux

# Build the binary
go build ./cmd/schmux

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
   - AI agents you want to use

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
  "agents": [
    {
      "name": "claude",
      "command": "claude"
    },
    {
      "name": "codex",
      "command": "codex"
    }
  ],
  "terminal": {
    "width": 120,
    "height": 40,
    "seed_lines": 100
  }
}
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

**Problem**: `agent command is required for X`
- **Solution**: Make sure each agent in your config has both `name` and `command` fields

**Problem**: Dashboard shows "Disconnected"
- **Solution**: Check if daemon is running with `./schmux status`

## Status

**v0.5** - Mostly working, occasionally on fire

## Documentation

- [SPEC.md](SPEC.md) - Full feature specification
- [WEB-UX.md](WEB-UX.md) - Web UI/UX architecture and component standards
- [CONTRIBUTING.md](CONTRIBUTING.md) - Guide to contribute to this codebase

## Development

Cross-platform dashboard build helper (runs npm build, with optional Go test/build):

```bash
go run ./cmd/build-dashboard
go run ./cmd/build-dashboard -test -build
go run ./cmd/build-dashboard -skip-install
```

## License

Apache License 2.0 - see [LICENSE](LICENSE)
