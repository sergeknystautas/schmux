# Floor Manager

## What it does

The floor manager is a singleton agent session that acts as the conversational counterpart to the entire schmux system. It monitors all agents via the event pipeline, relays status to the human operator, and executes orchestration commands on their behalf through natural language dialogue in a terminal.

## Key files

| File                                            | Purpose                                                                                     |
| ----------------------------------------------- | ------------------------------------------------------------------------------------------- |
| `internal/floormanager/manager.go`              | `Manager` struct: lifecycle, spawn, monitor loop (15s), rotation, restart fallback chain    |
| `internal/floormanager/injector.go`             | `Injector` struct: `events.EventHandler` impl, filtering, debounce, tmux injection          |
| `internal/floormanager/prompt.go`               | `GenerateInstructions()` and `GenerateSettings()` for CLAUDE.md and `.claude/settings.json` |
| `internal/floormanager/sanitize.go`             | `StripControlChars()` and `QuoteContentField()` for terminal escape safety                  |
| `internal/dashboard/handlers_floormanager.go`   | `GET /api/floor-manager` and `POST /api/floor-manager/end-shift`                            |
| `internal/dashboard/handlers_tell.go`           | `POST /api/sessions/{id}/tell` -- message injection with `[from FM]` prefix                 |
| `internal/dashboard/handlers_capture.go`        | `GET /api/sessions/{id}/capture` -- terminal scrollback retrieval                           |
| `internal/dashboard/handlers_branches.go`       | `GET /api/branches`                                                                         |
| `internal/dashboard/handlers_inspect.go`        | `GET /api/workspaces/{id}/inspect`                                                          |
| `cmd/schmux/tell.go`                            | CLI: `schmux tell <session> -m "..."`                                                       |
| `cmd/schmux/events.go`                          | CLI: `schmux events <session> [--type T] [--last N]`                                        |
| `cmd/schmux/capture.go`                         | CLI: `schmux capture <session> [--lines N]`                                                 |
| `cmd/schmux/inspect.go`                         | CLI: `schmux inspect <workspace>`                                                           |
| `cmd/schmux/branches.go`                        | CLI: `schmux branches`                                                                      |
| `assets/dashboard/src/hooks/useFloorManager.ts` | FM state from `/ws/dashboard` broadcasts                                                    |
| `assets/dashboard/src/routes/HomePage.tsx`      | Two-column layout when FM is enabled; mobile workspace tab strip                            |

## Architecture decisions

- **Peer to the session manager, not a workspace session.** The FM has no workspace, no event hooks, no presence in the session list. It manages its own tmux session directly via the `tmux` package. This avoids circular dependencies where the session manager would need to treat the FM specially.
- **Event-driven, not polling.** The `Injector` is registered as an `events.EventHandler` alongside `DashboardHandler` in the daemon's pipeline. It receives `StatusEvent` objects and filters them by state transition (skips all transitions TO `working`, not just `working -> working`).
- **Clear-before-inject pattern.** Both the operator (via WebSocket) and the Injector (via `tmux send-keys`) write to the same terminal PTY. Before every injection, `Ctrl+U` (unix-line-discard) clears partial input. Applied in three places: `Injector.flush()`, `handlers_tell.go`, and `Manager.handleShiftRotation()`.
- **Least privilege via `.claude/settings.json`.** Destructive commands (`dispose`, `stop`) are never pre-approved. Safety survives context compaction because the tool approval layer is independent of the agent's instructions.
- **Absolute binary path for FM commands.** `GenerateInstructions()` and `GenerateSettings()` use the resolved `os.Executable()` path so the FM calls the same binary that is currently running, not a potentially stale PATH version.
- **CLI tools are general-purpose.** `tell`, `events`, `capture`, `inspect`, and `branches` work for any user or script, but are designed primarily for the FM agent.
- **VCS-agnostic inspection.** `inspect` and `branches` use `vcs.CommandBuilder` so they work identically for git and sapling workspaces, local or remote.

## Gotchas

- The FM session name is the constant `schmux-floor-manager`, not a generated ID. Only one FM can exist at a time; a mutex guard in `handleShiftRotation` prevents concurrent rotations.
- `memory.md` is never overwritten by the daemon. It is the agent's long-term memory across rotations and restarts. `CLAUDE.md` and `AGENTS.md` are regenerated on every spawn.
- The `[SHIFT]` message instructs the FM to save memory and run `schmux end-shift`. If `end-shift` is not received within 30 seconds, the daemon force-rotates anyway.
- `schmux tell` adds the `[from FM]` prefix server-side, not in the CLI. This prevents spoofing.
- The `SessionRuntime` created for the FM passes `nil` for state store, event file, event handlers, output callback, and logger -- it exists only for terminal streaming via WebSocket.
- Rotation resets the injection count and spawns a fresh session with the prompt `"Begin."`. The new session reads `memory.md` on startup per its `CLAUDE.md` instructions.
- On daemon startup, if a leftover FM tmux session already exists, the manager reconnects to it instead of creating a duplicate.
- Remote sessions are supported for all five CLI tools. Each handler checks `ws.RemoteHostID` and branches between local tmux calls and `conn.RunCommand()`/`conn.SendKeys()`.

## Common modification patterns

- **To add a new pre-approved FM command:** Update both `GenerateInstructions()` (add to "Available Commands" list) and `GenerateSettings()` (add `Bash(schmux <cmd>*)` to the allow list) in `internal/floormanager/prompt.go`.
- **To change event filtering rules:** Edit `shouldInject()` in `internal/floormanager/injector.go`. Currently skips transitions to `"working"`.
- **To change signal format:** Edit `FormatSignalMessage()` in `internal/floormanager/injector.go`.
- **To add a new CLI tool for the FM:** Create `cmd/schmux/<cmd>.go` (thin API client), add the handler in `internal/dashboard/handlers_<name>.go`, register the route in `internal/dashboard/server.go`, and add the case to the command switch in `cmd/schmux/main.go`.
- **To change rotation threshold or debounce defaults:** These come from config via `GetFloorManagerRotationThreshold()` and `GetFloorManagerDebounceMs()` in `internal/config/config.go`.
- **To modify the FM home page layout:** Edit `assets/dashboard/src/routes/HomePage.tsx`. The layout commits immediately when `enabled` is true regardless of `running` state -- no layout jump when the terminal connects.
