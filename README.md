# schmux

## Who is this for?

Have you mastered vibe coding?  Did you read and enjoy [Clerky's tweets](https://x.com/bcherny/status/2007179832300581177) about how the author of Claude Code develops?  Are you trying to run multiple agents but aren't satisfied with [Gaslight town](https://steve-yegge.medium.com/welcome-to-gas-town-4f25ee16dd04), [Ralph Wiggum](https://medium.com/@joe.njenga/ralph-wiggum-claude-code-new-way-to-run-autonomously-for-hours-without-drama-095f47fbd467), or other agent automation frameworks?  

Then it's time for you to work with schmux!

**schmux is built by schmux.**

We know no agent framework is perfect, but we're living through the biggest developer force multiplier since the rollout of the TCP/IP driver. We need lots of approaches.


## What is this?

**Smart Cognitive Hub on tmux**

Orchestrate multiple AI coding agents across tmux sessions with a web dashboard for monitoring and management.

schmux lets you spin up multiple AI coding agents (Codex, Claude Code with various LLM backends) working on the same task in parallel. Each agent runs in its own tmux session on a managed clone of your git repository. A web dashboard lets you spawn sessions, monitor terminal output, and manage workspaces.

## Status

**v0.5** - Mostly working, occasionally on fire

## Features

- **Multi-agent orchestration** - Run Claude, Codex, and friends simultaneously
- **Multi-agent per directory** - Spawn reviewers or subagents on existing workspaces
- **Workspace management** - Auto git clone/checkout/pull for clean working directories
- **tmux integration** - Each agent in its own session, attach anytime
- **Web dashboard** - Watch your agents work (or panic) in real-time
- **Session persistence** - Survives agent completion for review and resume

## Requirements

- Go 1.21+
- tmux
- git
- AI agent CLIs (codex, claude, etc.)

## Documentation

See [SPEC.md](SPEC.md) for the full specification.

## More Docs

- [WEB-UX.md](WEB-UX.md) - Web UI/UX architecture and component standards
- [CONTRIBUTING.md](CONTRIBUTING.md) - Guide to contribute to this codebase

## License

Apache License 2.0 - see [LICENSE](LICENSE)
