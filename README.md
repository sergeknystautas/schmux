# schmux

**Smart Cognitive Hub on tmux**

Orchestrate multiple AI coding agents across tmux sessions with a web dashboard for monitoring and management.

## What is this?

schmux lets you spin up multiple AI coding agents (Codex, Claude Code with various LLM backends) working on the same task in parallel. Each agent runs in its own tmux session on a managed clone of your git repository. A web dashboard lets you spawn sessions, monitor terminal output, and manage workspaces.

## Status

🚧 **Early Development** - v0.5 in progress

## Features (Planned)

- **Multi-agent orchestration** - Run multiple AI agents simultaneously with different LLM backends
- **Multi-agent per directory** - Spawn reviewers or subagents on existing workspaces
- **Workspace management** - Automatic git clone/checkout/pull for clean working directories
- **tmux integration** - Each agent in its own session, attach anytime to interact
- **Web dashboard** - Spawn sessions, view real-time output, manage workspaces
- **Session persistence** - Sessions survive agent completion for review and resume

## Requirements

- Go 1.21+
- tmux
- git
- AI agent CLIs (codex, claude, etc.)

## Documentation

See [SPEC.md](SPEC.md) for the full specification.

## More Docs

- [SPEC.md](SPEC.md) - Product requirements and overall spec
- [WEB-UX.md](WEB-UX.md) - Web UI/UX architecture and component standards

## License

Apache License 2.0 - see [LICENSE](LICENSE)
