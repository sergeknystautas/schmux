# Documentation

Reference documentation for the schmux codebase. Each file is a subsystem guide that serves as a starting point for agents and developers working in that area.

## How to use these docs

When modifying a subsystem, read its guide first. The guide tells you which files matter, why things are built the way they are, and what pitfalls to avoid. Then read the actual source files it points to.

## Index

### Project

| File                                                  | Description                         |
| ----------------------------------------------------- | ----------------------------------- |
| [PHILOSOPHY.md](PHILOSOPHY.md)                        | Product principles and design goals |
| [contributing.md](contributing.md)                    | Development setup and workflow      |
| [release-strategy.md](../release/release-strategy.md) | Release process                     |

### Reference (API, CLI, UX)

| File             | Description                 |
| ---------------- | --------------------------- |
| [api.md](api.md) | HTTP/WebSocket API contract |
| [cli.md](cli.md) | CLI command reference       |
| [web.md](web.md) | Web dashboard UX patterns   |

### Backend subsystems

| File                                           | Description                                                                                                                                |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| [architecture.md](architecture.md)             | Backend architecture overview, router, JSON schema                                                                                         |
| [models.md](models.md)                         | Model catalog, registry, provider profiles                                                                                                 |
| [tool-adapters.md](tool-adapters.md)           | ToolAdapter interface, per-agent adapters                                                                                                  |
| [sessions.md](sessions.md)                     | Session lifecycle, ControlSource, spawn modes, resume                                                                                      |
| [chat-sessions.md](chat-sessions.md)           | Chat-kind sessions: conversation view, per-agent wire protocols                                                                            |
| [chat-context-usage.md](chat-context-usage.md) | Tokens-in-context reported by the chat harnesses, shown in the sidebar                                                                     |
| [plan-usage.md](plan-usage.md)                 | Plan quota snapshots from the chat harnesses, compared against the elapsed window                                                          |
| [fenced-sessions.md](fenced-sessions.md)       | Per-spawn Fence sandboxing, monitor logs, network allowlists                                                                               |
| [workspaces.md](workspaces.md)                 | Workspace management, VCS abstraction, workspace disposal                                                                                  |
| [repo-names.md](repo-names.md)                 | Repo name sanitizing: the character set, where names enter config, why                                                                     |
| [flights.md](flights.md)                       | Flights: durable manually-driven workflow over a workspace + planner/executor/commit sessions; shared disposal and spawn-launcher services |
| [build-monitor.md](build-monitor.md)           | Build monitor: GitHub Actions workflow watching, auto-launched remediation sessions                                                        |
| [remote.md](remote.md)                         | Remote access (Cloudflare tunnel)                                                                                                          |
| [https.md](https.md)                           | HTTPS via dashboard.sx, ACME, TLS config UI, status alerts                                                                                 |
| [timelapse.md](timelapse.md)                   | Session recording, time-compressed replay, asciicast export                                                                                |
| [git-features.md](git-features.md)             | Git graph, status watcher, commit detail, PR discovery                                                                                     |
| [floor-manager.md](floor-manager.md)           | Floor manager agency, CLI tools                                                                                                            |
| [overlays.md](overlays.md)                     | Overlay compounding, file propagation, manifests                                                                                           |
| [personas.md](personas.md)                     | Persona YAML files, built-ins, prompt delivery                                                                                             |
| [comm-styles.md](comm-styles.md)               | Communication styles, per-agent defaults, prompt composition                                                                               |
| [nudgenik.md](nudgenik.md)                     | NudgeNik session status classification                                                                                                     |
| [repofeed.md](repofeed.md)                     | Cross-developer activity feed                                                                                                              |
| [telemetry.md](telemetry.md)                   | PostHog telemetry, IO workspace telemetry                                                                                                  |
| [preview.md](preview.md)                       | Preview proxy lifecycle, auto-detection, port allocation                                                                                   |
| [clipboard.md](clipboard.md)                   | Clipboard bridge: OSC 52 interception, approve/reject banner                                                                               |
| [subreddit.md](subreddit.md)                   | Subreddit news feed (per-repo posts, upvotes)                                                                                              |
| [autolearn.md](autolearn.md)                   | Autolearn system (continual learning, curation, merge)                                                                                     |
| [onboarding.md](onboarding.md)                 | Onboarding: startup detection, smart defaults, lazy-config first session                                                                   |
| [security.md](security.md)                     | Security posture: argv command templates, file modes, HTTP perimeter validation                                                            |

### Frontend

| File                                                 | Description                                                                   |
| ---------------------------------------------------- | ----------------------------------------------------------------------------- |
| [react.md](react.md)                                 | React architecture, component patterns                                        |
| [dashboard-style-guide.md](dashboard-style-guide.md) | Design system: tokens, primitives, page templates, compliance rubric          |
| [dashboard-ui.md](dashboard-ui.md)                   | Sidebar, tools section, sort toggle, action dropdown, event monitor, spawn UX |
| [client-performance.md](client-performance.md)       | Browser main-thread recording for diagnosing a slow dashboard                 |
| [typing-performance.md](typing-performance.md)       | Keystroke latency diagnostic panel                                            |
| [settings.md](settings.md)                           | The `/config` settings page: tab structure, form state, auto-save             |
| [demo.md](demo.md)                                   | Interactive demo, mock transport, tour system                                 |
| [markdown-editor.md](markdown-editor.md)             | Autosaving Markdown editor, server-side merge with agent writes               |
| [tooltip-examples.md](tooltip-examples.md)           | Tooltip component usage examples                                              |

### Infrastructure

| File                                           | Description                                 |
| ---------------------------------------------- | ------------------------------------------- |
| [terminal-pipeline.md](terminal-pipeline.md)   | Terminal streaming pipeline and diagnostics |
| [dev-mode.md](dev-mode.md)                     | Hot-reload development, workspace switching |
| [testing.md](testing.md)                       | Test infrastructure, scenario testing       |
| [e2e.md](e2e.md)                               | E2E testing in Docker                       |
| [schmux-directories.md](schmux-directories.md) | Directory layout (`~/.schmux/`)             |
| [dependencies.md](dependencies.md)             | Optional dependencies and install tracks    |

### Other

| File                                               | Description                         |
| -------------------------------------------------- | ----------------------------------- |
| [agent-signaling.md](agent-signaling.md)           | Agent signaling protocol            |
| [claude-code-hooks.md](claude-code-hooks.md)       | Claude Code hooks integration       |
| [targets.md](targets.md)                           | Run targets (command-only)          |
| [oneshot.md](oneshot.md)                           | Oneshot mode architecture           |
| [git-graph-parameters.md](git-graph-parameters.md) | Git graph parameter semantics       |
| [tabs.md](tabs.md)                                 | Tab system (sessions + accessories) |

## Subdirectories

| Directory        | Purpose                                                                |
| ---------------- | ---------------------------------------------------------------------- |
| [specs/](specs/) | Design specs for features not yet fully implemented                    |
| [plans/](plans/) | Step-by-step implementation plans (temporary, deleted after execution) |
