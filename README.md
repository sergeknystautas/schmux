# schmux

## What is schmux?

**Multi-Agent Orchestration on tmux** (Smart Cognitive Hub)

Orchestrate multiple run targets across tmux sessions with a web dashboard for monitoring and management.

schmux lets you spin up multiple run targets (detected tools like Claude, Codex, Gemini, plus user-defined commands) working on the same task in parallel. Each target runs in its own tmux session on a managed clone of your git repository. A web dashboard lets you spawn sessions, monitor terminal output, and manage workspaces.

## Who is this for?

Have you mastered vibe coding? Did you read and enjoy [Clerky's tweets](https://x.com/bcherny/status/2007179832300581177) about how the author of Claude Code develops? Are you trying to run multiple agents but aren't satisfied with [Gaslight town](https://steve-yegge.medium.com/welcome-to-gas-town-4f25ee16dd04), [Ralph Wiggum](https://medium.com/@joe.njenga/ralph-wiggum-claude-code-new-way-to-run-autonomously-for-hours-without-drama-095f47fbd467), or other agent automation frameworks?

Then it's time for you to work with schmux!

**schmux is built by schmux.** We know no agent framework is perfect, but we're living through the biggest developer force multiplier since the rollout of the TCP/IP driver. We need lots of approaches.

---

## Quick Start

Get up and running with schmux in 5 minutes. Works on Mac, Linux, or Windows (WSL only).

### Prerequisites

You'll need:

1. [**tmux**](https://github.com/tmux/tmux) - `brew install tmux`, `apt install tmux`, or whatever your package manager.
2. [**git**](https://git-scm.com/) - usually pre-installed or available with your favorite package manager.
   - Note: schmux runs git commands locally in your workspaces, so it will work with whatever authentication you have configured (SSH keys, HTTPS tokens, credential helpers, etc.)
3. **Run target CLIs** - At least one of:
   - [Claude Code](https://claude.ai/code)
   - [Codex](https://openai.com/codex/)
   - [Gemini CLI](https://github.com/google-gemini/gemini-cli)
   - Or any CLI you want to add as a run target

### Installation

```bash
# One-line install (downloads binary + dashboard assets)
curl -fsSL https://raw.githubusercontent.com/sergeknystautas/schmux/main/install.sh | bash
```

This installs to `~/.local/bin/schmux`. Make sure it's in your PATH, then run `schmux update` anytime to get the latest version.

### Next Steps

1. **Verify installation**: `schmux --version`
2. **Start the daemon**: `schmux start`
3. **Open the dashboard**: http://localhost:7337

To build from source instead, see [Contributing](CONTRIBUTING.md).

### First-Time Setup

When you run `schmux start` for the first time, if `~/.schmux/config.json` doesn't exist, the daemon will guide you through creating a configuration:

1. **Start schmux**:

   ```bash
   schmux start
   ```

2. **Follow the prompts** to configure:
   - Where to store workspace directories (default: `~/schmux-workspaces`)
   - Git repositories you want to work with
   - Run targets and quick launch presets

3. **Open the dashboard** at `http://localhost:7337`

4. **Spawn your first session** via the web UI

### Common Issues

**Problem**: `tmux is not installed or not accessible`

- **Solution**: Install tmux (`brew install tmux` on macOS)

**Problem**: `config file not found: ~/.schmux/config.json`

- **Solution**: Run `schmux start` - it will offer to create a config for you

**Problem**: `run target command is required for X`

- **Solution**: Make sure each run target has `name`, `type`, and `command`

**Problem**: Dashboard shows "Disconnected"

- **Solution**: Check if daemon is running with `schmux status`

**Problem**: I want local config files in each workspace

- **Solution**: Use workspace overlays - see `docs/workspaces.md` for details

## Features

- **Multi-agent orchestration** - Run Claude, Codex, and friends simultaneously
- **Multi-agent per directory** - Spawn reviewers or subtargets on existing workspaces
- **Workspace management** - Auto git clone/checkout/pull for clean working directories
- **Workspace overlays** - Auto-copy local-only files (`.env`, config) to new workspaces
- **tmux integration** - Each target in its own session, attach anytime
- **Web dashboard** - Watch agents work in real-time
- **Full CLI capabilities** - Spawn and manage sessions and workspaces from your terminal
- **Session multitasking** - See when an agent is done, usually with a summary on what it needs.

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│ Daemon (internal/daemon/daemon.go)                      │
├─────────────────────────────────────────────────────────┤
│  Dashboard Server (:7337)                               │
│  - HTTP API (internal/dashboard/handlers.go)            │
│  - WebSocket terminal streaming                         │
│  - Serves static assets from assets/dashboard/          │
│                                                         │
│  Session Manager (internal/session/manager.go)          │
│  - Spawn/dispose tmux sessions                          │
│  - Track PIDs, status, terminal output                  │
│                                                         │
│  Workspace Manager (internal/workspace/manager.go)      │
│  - Clone/checkout git repos                             │
│  - Track workspace directories                          │
│                                                         │
│  tmux Package (internal/tmux/tmux.go)                   │
│  - tmux CLI wrapper (create, capture, list, kill)       │
│                                                         │
│  Config/State (internal/config/, internal/state/)       │
│  - ~/.schmux/config.json  (repos, agents, workspace)    │
│  - ~/.schmux/state.json    (workspaces, sessions)       │
└─────────────────────────────────────────────────────────┘
```

## Status

**v1.1.1** - Stable for daily use

See [CHANGES.md](CHANGES.md) for what's new in each release.

### Known Limitations

- Windows support via WSL only (no native Windows support)
- Dashboard runs locally only (no remote access by default)
- See [docs/PHILOSOPHY.md](docs/PHILOSOPHY.md) for non-goals

### Roadmap

- v1.1: Enhanced NudgeNik capabilities
- Future: Browser UI automation tests

## Documentation

- [docs/PHILOSOPHY.md](docs/PHILOSOPHY.md) - Product philosophy (source of truth)
- [docs/workspaces.md](docs/workspaces.md) - Workspace management
- [docs/targets.md](docs/targets.md) - Run targets and multi-agent coordination
- [docs/sessions.md](docs/sessions.md) - Session lifecycle and management
- [docs/web.md](docs/web.md) - Web dashboard
- [docs/cli.md](docs/cli.md) - CLI command reference
- [docs/api.md](docs/api.md) - Daemon HTTP API contract (client-agnostic)
- [docs/nudgenik.md](docs/nudgenik.md) - NudgeNik feature
- [CONTRIBUTING.md](CONTRIBUTING.md) - Contributor guide
- [docs/dev/README.md](docs/dev/README.md) - Detailed development documentation

## Getting Help

- **Issues**: Bug reports and feature requests at [github.com/sergeknystautas/schmux/issues](https://github.com/sergeknystautas/schmux/issues)
- **Discussions**: Questions and general discussion at [github.com/sergeknystautas/schmux/discussions](https://github.com/sergeknystautas/schmux/discussions)
- **Documentation**: See the docs/ directory for complete guides and API reference

## License

Apache License 2.0 - see [LICENSE](LICENSE)

Copyright 2025 Serge Knystautas

Licensed under the Apache License, Version 2.0
