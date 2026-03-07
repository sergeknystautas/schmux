# Documentation

Reference documentation for the schmux codebase. Each file is a subsystem guide that serves as a starting point for agents and developers working in that area.

## How to use these docs

When modifying a subsystem, read its guide first. The guide tells you which files matter, why things are built the way they are, and what pitfalls to avoid. Then read the actual source files it points to.

## Index

### Project

| File                                       | Description                         |
| ------------------------------------------ | ----------------------------------- |
| [PHILOSOPHY.md](PHILOSOPHY.md)             | Product principles and design goals |
| [contributing.md](contributing.md)         | Development setup and workflow      |
| [release-strategy.md](release-strategy.md) | Release process                     |

### Reference (API, CLI, UX)

| File             | Description                 |
| ---------------- | --------------------------- |
| [api.md](api.md) | HTTP/WebSocket API contract |
| [cli.md](cli.md) | CLI command reference       |
| [web.md](web.md) | Web dashboard UX patterns   |

### Backend subsystems

| File                                 | Description                                                      |
| ------------------------------------ | ---------------------------------------------------------------- |
| [architecture.md](architecture.md)   | Backend architecture overview, router, JSON schema               |
| [models.md](models.md)               | Model catalog, RunnerSpec, model config editor                   |
| [tool-adapters.md](tool-adapters.md) | ToolAdapter interface, per-agent adapters                        |
| [sessions.md](sessions.md)           | Session lifecycle, spawn modes, resume                           |
| [workspaces.md](workspaces.md)       | Workspace management, locking, ensure, preview proxy             |
| [remote.md](remote.md)               | Remote access (Cloudflare tunnel) and remote sessions (SSH/tmux) |
| [https.md](https.md)                 | HTTPS via dashboard.sx, ACME, TLS config UI                      |
| [git-features.md](git-features.md)   | Git graph, status watcher, commit detail, PR discovery           |
| [floor-manager.md](floor-manager.md) | Floor manager agency, CLI tools, VCS abstraction                 |
| [overlays.md](overlays.md)           | Overlay compounding, file propagation, manifests                 |
| [personas.md](personas.md)           | Persona YAML files, built-ins, prompt delivery                   |
| [telemetry.md](telemetry.md)         | PostHog telemetry, IO workspace telemetry                        |
| [preview.md](preview.md)             | Preview proxy lifecycle, auto-detection, port allocation         |
| [subreddit.md](subreddit.md)         | Subreddit digest generation                                      |

### Frontend

| File                                       | Description                                                                   |
| ------------------------------------------ | ----------------------------------------------------------------------------- |
| [react.md](react.md)                       | React architecture, component patterns                                        |
| [dashboard-ui.md](dashboard-ui.md)         | Sidebar, tools section, sort toggle, action dropdown, event monitor, spawn UX |
| [demo.md](demo.md)                         | Interactive demo, mock transport, tour system                                 |
| [tooltip-examples.md](tooltip-examples.md) | Tooltip component usage examples                                              |

### Testing

| File                     | Description                                               |
| ------------------------ | --------------------------------------------------------- |
| [testing.md](testing.md) | Test infrastructure, scenario testing, definition of done |
| [e2e.md](e2e.md)         | E2E testing in Docker                                     |

### Other

| File                                                     | Description                                 |
| -------------------------------------------------------- | ------------------------------------------- |
| [agent-signaling.md](agent-signaling.md)                 | Agent signaling protocol                    |
| [claude-code-hooks.md](claude-code-hooks.md)             | Claude Code hooks integration               |
| [nudgenik.md](nudgenik.md)                               | NudgeNik feature                            |
| [targets.md](targets.md)                                 | Targets (multi-agent coordination)          |
| [terminal-pipeline.md](terminal-pipeline.md)             | Terminal streaming pipeline and diagnostics |
| [websocket-sessions-spec.md](websocket-sessions-spec.md) | WebSocket dashboard protocol                |
| [schmux-directories.md](schmux-directories.md)           | Directory layout (`~/.schmux/`)             |
| [oneshot.md](oneshot.md)                                 | Oneshot mode architecture                   |
| [git-graph-parameters.md](git-graph-parameters.md)       | Git graph parameter semantics               |
| [worktree-migration.md](worktree-migration.md)           | Git worktree migration                      |

## Subdirectories

| Directory        | Purpose                                                                |
| ---------------- | ---------------------------------------------------------------------- |
| [specs/](specs/) | Design specs for features not yet fully implemented                    |
| [plans/](plans/) | Step-by-step implementation plans (temporary, deleted after execution) |
