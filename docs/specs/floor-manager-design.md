# Floor Manager Design

## Overview

The floor manager is a singleton agent session that acts as the conversational counterpart to the entire schmux system. It monitors all agents, relays status to the human operator, and executes orchestration commands on their behalf — all through natural language dialogue in a terminal.

It is not a new service or protocol. It is a regular agent session (any configured run target) spawned with a purpose-built prompt and a privileged position in the dashboard UI.

## Core Principles

- **Agent-agnostic.** The floor manager can be any run target schmux supports (Claude, Codex, Gemini, custom). Users choose based on cost/capability tradeoff.
- **No new infrastructure.** Reuses existing session management, terminal streaming, WebSocket feeds, and signal system.
- **Mobile-first.** Designed for remote access from small screens with limited input. Proactive updates reduce the need to type.
- **Event-driven.** The floor manager reacts to system events, not just user input. It stays informed without polling.
- **Durable.** Survives crashes and context degradation through external memory and rotation.

## System Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Daemon (daemon.go)                          │
│                                                                     │
│  ┌─────────────┐   ┌──────────────┐   ┌────────────────────────┐    │
│  │   Config    │   │    State     │   │   Session Manager      │    │
│  │             │   │              │   │                        │    │
│  │ floor_mgr:  │   │ Sessions[]:  │   │  Spawn()               │    │
│  │  enabled    │   │  ID          │   │  Dispose()             │    │
│  │  target     │   │  IsFloorMgr  │   │  IsRunning()           │    │
│  │  rotation_  │   │  TmuxSession │   │  GetSession()          │    │
│  │  threshold  │   │  ...         │   │  SetSignalCallback()   │    │
│  │  debounce_  │   │              │   │                        │    │
│  │  ms         │   │              │   │                        │    │
│  └──────┬──────┘   └──────┬───────┘   └───────────┬────────────┘    │
│         │                 │                       │                 │
│         ▼                 ▼                       ▼                 │
│  ┌────────────────────────────────────────────────────────────┐     │
│  │              Floor Manager (floormanager/)                 │     │
│  │                                                            │     │
│  │  ┌──────────────────┐      ┌──────────────────────────┐    │     │
│  │  │     Manager      │      │       Injector           │    │     │
│  │  │                  │      │                          │    │     │
│  │  │  Start()         │◄────►│  Inject(id, name, sig)   │    │     │
│  │  │  Stop()          │      │  flush() → tmux send     │    │     │
│  │  │  spawn()         │      │  debounce batching       │    │     │
│  │  │  spawnResume()   │      │  rotation threshold      │    │     │
│  │  │  monitor()       │      │  ShouldInjectSignal()    │    │     │
│  │  │  HandleRotation()│      │  FormatSignalMessage()   │    │     │
│  │  └──────────────────┘      └──────────────────────────┘    │     │
│  └────────────────────────────────────────────────────────────┘     │
│         │                                        │                  │
│         ▼                                        ▼                  │
│  ┌──────────────┐   ┌──────────────┐   ┌─────────────────────┐      │
│  │  Dashboard   │   │   tmux       │   │  Signal Watcher     │      │
│  │  Server      │   │              │   │  (session mgr       │      │
│  │              │   │  SendLiteral │   │   callback)         │      │
│  │ /api/floor-  │   │  SendKeys    │   │                     │      │
│  │  manager     │   │              │   │  Watches all agent  │      │
│  │ /ws/terminal │   │              │   │  signal files       │      │
│  └──────────────┘   └──────────────┘   └─────────────────────┘      │
└─────────────────────────────────────────────────────────────────────┘
```

### Data Flow: Signal Injection Pipeline

```
  Agent Session (e.g. claude-1)          Floor Manager Session
  ─────────────────────────              ─────────────────────
         │                                        ▲
         │ writes signal file                     │ [SIGNAL] text
         │ .schmux/signal/{id}                    │ via tmux send
         ▼                                        │
  ┌──────────────────┐                   ┌────────┴─────────┐
  │  Signal File     │                   │   tmux terminal  │
  │                  │                   │   (floor-manager │
  │  Line 1: STATE   │                   │    session)      │
  │  Line 2: intent  │                   └────────▲─────────┘
  │  Line 3: blocker │                            │
  └────────┬─────────┘                            │
           │                                      │
           ▼                                      │
  ┌────────────────────────┐             ┌────────┴──────────┐
  │  Session Manager       │             │    Injector       │
  │  SignalCallback(id,sig)│────────────►│                   │
  └────────────────────────┘             │  1. Filter:       │
                                         │     skip working  │
                                         │     →working      │
                                         │                   │
                                         │  2. Format:       │
                                         │     [SIGNAL] ...  │
                                         │                   │
                                         │  3. Debounce:     │
                                         │     batch within  │
                                         │     window        │
                                         │                   │
                                         │  4. Send:         │
                                         │     tmux.Send     │
                                         │     Literal+Enter │
                                         │                   │
                                         │  5. Count:        │
                                         │     track for     │
                                         │     rotation      │
                                         └───────────────────┘
```

## Session Model

The floor manager is a session with a special flag (`IsFloorManager: true`) in the state model. It uses a dedicated working directory (`~/.schmux/floor-manager/`) instead of a registered workspace.

- **Singleton.** Only one floor manager can exist at a time. Enforced by `State.GetFloorManagerSession()` and a defensive check in `Manager.spawn()`.
- **Auto-spawn.** Created automatically when the daemon starts, if enabled in config.
- **Auto-restart.** If the agent process exits, the monitor goroutine (polling every 15s) detects it and restarts with resume-first, then fresh spawn fallback.
- **Not user-spawnable.** Does not appear in the spawn wizard — the nickname "floor-manager" is rejected by the spawn handler. Hidden from the session list in API responses.
- **Dynamic toggle.** Can be enabled/disabled at runtime via the config page — the daemon starts or stops the floor manager without requiring a restart.

### Lifecycle State Machine

```
                          ┌─────────┐
                          │ Config  │
                          │ enabled │
                          └────┬────┘
                               │
                 ┌─────────────▼──────────────┐
                 │      Manager.Start()       │
                 │  Check singleton in state  │
                 └─────────────┬──────────────┘
                               │
              ┌────────────────▼────────────────┐
              │         Manager.spawn()         │
              │                                 │
              │  1. Create ~/.schmux/floor-mgr/ │
              │  2. Write CLAUDE.md, AGENTS.md  │
              │  3. Write .claude/settings.json │
              │  4. session.Spawn(WorkDir: ...) │
              │  5. Mark IsFloorManager = true  │
              │  6. Reset injection count       │
              └────────────────┬────────────────┘
                               │
              ┌────────────────▼────────────────┐
              │       Manager.monitor()         │
              │   (goroutine, polls every 15s)  │
              └────────────────┬────────────────┘
                               │
                    ┌──────────▼──────────┐
                    │   Is session alive? │
                    └──────────┬──────────┘
                          yes/ \no
                          /     \
                  ┌──────┘       └──────────────────┐
                  │ continue                        │
                  │ monitoring                      ▼
                  │                    ┌─────────────────────────┐
                  │                    │  checkAndRestart()      │
                  │                    │                         │
                  │                    │  1. Try spawnResume()   │
                  │                    │     (--resume flag)     │
                  │                    │                         │
                  │                    │  2. If fails, spawn()   │
                  │                    │     (fresh + memory.md) │
                  │                    └─────────────────────────┘
                  │
                  │         ┌────────────────────────────┐
                  └────────►│  HandleRotation()          │
                            │  (triggered by threshold   │
                            │   or agent "rotate" signal)│
                            │                            │
                            │  Threshold path:           │
                            │  1. Send [SHIFT] warning   │
                            │  2. Wait 30s for agent     │
                            │     to save memory.md      │
                            │  3. Dispose old session    │
                            │  4. Wait restartDelay (3s) │
                            │  5. spawn() fresh          │
                            │                            │
                            │  Self-requested path:      │
                            │  1. Skip finalize wait     │
                            │     (agent already saved)  │
                            │  2. Dispose old session    │
                            │  3. Wait restartDelay (3s) │
                            │  4. spawn() fresh          │
                            └────────────────────────────┘
```

## Configuration

```json
{
  "floor_manager": {
    "enabled": true,
    "target": "claude-sonnet",
    "rotation_threshold": 150,
    "debounce_ms": 2000
  }
}
```

| Field                | Type   | Default | Description                                               |
| -------------------- | ------ | ------- | --------------------------------------------------------- |
| `enabled`            | bool   | false   | Whether to spawn a floor manager on daemon start          |
| `target`             | string | —       | Any valid run target name (detected tool, model, custom)  |
| `rotation_threshold` | int    | 150     | Max signal injections before forced context rotation      |
| `debounce_ms`        | int    | 2000    | Milliseconds to batch rapid signal changes before sending |

The config page exposes `enabled`, `target`, and `rotation_threshold`. Toggling `enabled` takes effect immediately on save — the daemon starts or stops the floor manager without restarting.

## Agent Context Flow

### Enriched Signal Format

Agents report status via signal files at `.schmux/signal/{sessionID}`. The floor manager feature extends the signal file format from single-line to multi-line. On the **write side**, the enriched hook commands in `provision.go` use `printf` with `\n` to write multi-line files (e.g. `printf "%s\nintent: %s\n" "working $MSG" "$MSG"`). On the **read side**, `signal.ParseSignalFile()` was extended to split on newlines — it parses the first line as `STATE message` (unchanged from before), then scans subsequent lines for `intent: ` and `blockers: ` prefixes to populate the new `Signal.Intent` and `Signal.Blockers` fields. The format:

```
STATE optional-message
intent: what the agent is trying to do
blockers: what's preventing progress
```

Example:

```
needs_input Need clarification on auth token format
intent: Implementing OAuth2 token refresh
blockers: Unknown token expiry requirement
```

The `Signal` struct carries these fields:

| Field      | Description                                                   |
| ---------- | ------------------------------------------------------------- |
| `State`    | working, needs_input, needs_testing, completed, error, rotate |
| `Message`  | One-line description of current activity                      |
| `Intent`   | What the agent is trying to achieve                           |
| `Blockers` | What's preventing progress, if any                            |

### Auto-Hook Installation

When `session.Manager.Spawn()` creates a Claude Code session, schmux wraps the launch command to write hook configuration into `.claude/settings.local.json` before starting the agent. The hooks are workspace-scoped so they don't affect non-schmux Claude Code usage.

Two enriched hook variants are used:

- **`signalCommandWithIntent`** — On `working` state, extracts the prompt from the hook's JSON stdin and writes it as the `intent:` line.
- **`signalCommandWithBlockers`** — On `needs_input` state, extracts the message and writes it as the `blockers:` line.

For agents that don't support hooks (Codex, Gemini, custom), the existing nudgenik system infers state from terminal output as a fallback.

### Hook Installation Flow

```
  session.Spawn(target, prompt, ...)
           │
           ▼
  ┌─────────────────────────────────────┐
  │  provision.WrapCommandWithHooks()   │
  │                                     │
  │  Generates hooks JSON:              │
  │  {                                  │
  │    "hooks": {                       │
  │      "PreToolUse": [...],           │
  │      "PostToolUse": [...],          │
  │      "Notification": [...]          │
  │    }                                │
  │  }                                  │
  │                                     │
  │  Wraps command:                     │
  │  mkdir -p .claude &&                │
  │  printf '%s' '<json>'               │
  │    > .claude/settings.local.json && │
  │  <original-command>                 │
  └──────────────┬──────────────────────┘
                 │
                 ▼
  ┌──────────────────────────────────┐
  │  tmux new-session -d -s ...      │
  │  (runs wrapped command)          │
  └──────────────────────────────────┘
                 │
                 ▼
  Agent runs, hooks fire on events
  → write enriched signals to
    .schmux/signal/{sessionID}
```

## Event Injection

The Go daemon's signal callback (wired in `daemon.go`) processes signals from all sessions. When a signal comes from a non-floor-manager session, it is forwarded to the `Injector`.

### Filtering Rules

| Transition                              | Action                         |
| --------------------------------------- | ------------------------------ |
| Any → error                             | Always inject                  |
| Any → needs_input                       | Always inject                  |
| Any → needs_testing                     | Always inject                  |
| Any → completed                         | Always inject                  |
| working → working                       | Skip (agent is still chugging) |
| Any → working                           | Skip                           |
| Multiple signals within debounce window | Batch into single injection    |

### Injection Mechanics

The `Injector` uses `tmux.SendLiteral()` to write the text into the floor manager's terminal pane, followed by `tmux.SendKeys("Enter")` to submit it as input to the agent. Messages are batched during the debounce window and sent as a single newline-joined block.

Example injected message:

```
[SIGNAL] claude-1 (abc123) state: working -> needs_input. Summary: "Need clarification on auth token format." Intent: "Implementing OAuth2 token refresh" Blocked: "Unknown token expiry requirement."
```

### Two Input Sources

```
  ┌─────────────────────┐     ┌─────────────────────────┐
  │   Human Operator    │     │    Schmux Daemon        │
  │                     │     │    (Injector)           │
  │  Types in dashboard │     │                         │
  │  terminal UI        │     │  Signal transitions     │
  └─────────┬───────────┘     └────────────┬────────────┘
            │                              │
            │  /ws/terminal/{id}           │  tmux.SendLiteral
            │  WebSocket                   │  + SendKeys("Enter")
            │                              │
            ▼                              ▼
  ┌─────────────────────────────────────────────────────┐
  │              tmux session (floor-manager)           │
  │                                                     │
  │  Both appear as terminal input to the agent.        │
  │  [SIGNAL] prefix distinguishes system events        │
  │  from human messages.                               │
  └─────────────────────────────────────────────────────┘
```

### Rotation Threshold

After each flush, the injector increments the Manager's injection count. When this count reaches `rotation_threshold`, the injector triggers a **two-phase shift rotation**:

1. **[SHIFT] warning:** The injector sends a `[SHIFT]` message into the floor manager's terminal via `tmux.SendLiteral`. This tells the agent that a forced rotation is imminent and it has 30 seconds to write its final summary to `memory.md`. The `[SHIFT]` prefix is distinct from `[SIGNAL]` — `[SIGNAL]` messages are informational updates about other agents, while `[SHIFT]` is a directive to the floor manager itself.

2. **Shift timeout:** The injector waits `shiftRotationTimeout` (30 seconds), respecting `stopCh` and `ctx.Done()` for clean cancellation during shutdown.

3. **Forced rotation:** After the timeout, the injector calls `HandleRotation(ctx, true)` with `skipFinalizeWait=true` (since the agent already had 30 seconds to save). `HandleRotation` disposes the current session and spawns a fresh one after a 3-second `restartDelay`.

The `[SHIFT]` warning is best-effort: if the tmux send fails, rotation still proceeds (strictly better than the previous behavior of no warning at all). A mutex guard (`m.rotating`) prevents concurrent rotations from racing.

`[SHIFT]` message format:

```
[SHIFT] Forced rotation imminent. You have 30s to write your final summary to memory.md. Do not acknowledge this message to the operator — just write memory and stop.
```

## Context Durability

### Working Directory and Instructions

The floor manager runs in `~/.schmux/floor-manager/`. At spawn time, the Manager writes:

| File                    | Purpose                                                   |
| ----------------------- | --------------------------------------------------------- |
| `CLAUDE.md`             | Static role instructions, CLI reference, behavior rules   |
| `AGENTS.md`             | Same content (for non-Claude agents that read AGENTS.md)  |
| `.claude/settings.json` | Pre-approves `schmux *`, `cat memory.md`, `echo > memory` |
| `memory.md`             | Persistent memory file maintained by the agent itself     |

The spawn prompt is simply `"Begin."` — all substantive instructions are in `CLAUDE.md`, which tells the agent to:

1. Read `memory.md` for context from previous sessions
2. Run `schmux status` to see current system state
3. Proactively summarize when the operator connects

### Rotation Strategy

Context is managed through two mechanisms:

**Agent self-reporting:** The floor manager's instructions tell it to write a `rotate` signal when its context is getting heavy. The daemon's signal callback detects `state == "rotate"` from the floor manager's own session and calls `HandleRotation()`.

**Hard ceiling:** The `Injector` tracks the number of signal injections. When this exceeds `rotation_threshold`, it triggers a shift rotation: a `[SHIFT]` warning is sent to the agent, giving it 30 seconds to write its final memory before the session is disposed and respawned.

### Restart Fallback Chain

```
  Session exits
       │
       ▼
  ┌────────────────────────────────┐
  │  1. Try spawnResume()          │
  │     Spawns with Resume: true   │──── Success ──► Running
  │     (agent's --resume flag)    │                 (full context
  │     preserves conversation     │                  preserved)
  └───────────┬────────────────────┘
              │ Failure
              ▼
  ┌────────────────────────────────┐
  │  2. Try spawn(isRestart=true)  │
  │     Fresh spawn with prompt    │──── Success ──► Running
  │     "Begin."                   │                 (reads memory.md
  │     Agent reads memory.md on   │                  for continuity)
  │     startup per CLAUDE.md      │
  └───────────┬────────────────────┘
              │ Failure
              ▼
         Log error,
         retry on next
         monitor tick (15s)
```

Planned rotation (self-requested) skips the finalize wait since the agent already saved memory when it wrote the `rotate` signal. Threshold-triggered rotation uses the shift warning flow, giving the agent an orderly 30-second window instead of an abrupt kill.

## Floor Manager Prompt (CLAUDE.md)

Generated by `floormanager.GenerateInstructions()`. Includes:

**Role definition:**

- You are the floor manager for this schmux instance
- You orchestrate work across multiple AI coding agents
- You monitor their status, relay information to the human operator, and execute commands on their behalf

**On startup:**

- Read `memory.md` in your working directory for context from previous sessions
- Run `schmux status` to see the current state of all workspaces and sessions
- When the operator connects, proactively summarize what you found

**Available commands:**

- `schmux status` — see all workspaces, sessions, and their states
- `schmux spawn -a <target> -p "<prompt>" [-b <branch>] [-r <repo>]` — create new agent sessions
- `schmux dispose <session-id>` — tear down a session
- `schmux list` — list all sessions with IDs
- `schmux attach <session-id>` — get tmux attach command for a session
- `schmux stop` — stop the daemon

**Signal handling:**

- Format: `[SIGNAL] <session-name> (<session-id>) state: <old> -> <new>. Summary: "..." Intent: "..." Blocked: "..."`
- Evaluate each signal and decide how to respond

**Behavior guidelines:**

- When `[SIGNAL]` messages arrive, evaluate and decide: act autonomously, inform the operator, or note silently
- Confirm before executing destructive actions (dispose, sending input to agents)
- Keep responses concise — the operator may be on a phone
- Answer questions about the system using existing context without running commands when possible

**Memory file:**

- Maintain `memory.md` with key decisions, ongoing tasks, things the operator asked to watch for, and pending actions
- This file persists across session restarts — it is long-term memory

**Context rotation:**

- When context is getting heavy, write a final update to the memory file
- Then write `rotate` to the signal file: `echo "rotate Ready for context rotation" > "$SCHMUX_STATUS_FILE"`
- The system will restart with fresh context and the memory file

**Shift rotation:**

- If a `[SHIFT]` message appears, a forced rotation is imminent (30 seconds)
- Immediately write final summary to `memory.md`
- Do not acknowledge the `[SHIFT]` to the operator — just write memory and stop

**Pre-approved permissions** (`.claude/settings.json`):

- `Bash(schmux *)` — all schmux CLI commands
- `Bash(cat memory.md)` — read memory
- `Bash(echo * > memory.md)` — write memory

## Dashboard UI

### Landing Page Layout (Floor Manager Enabled)

When the floor manager is enabled and has an active session, the home page switches to a two-column layout:

- **Left column:** Floor manager terminal (fills the column)
- **Right column:** Recent branches, PRs, and workspace list (same content as the default home page)

The floor manager terminal uses the existing `TerminalStream` class and connects via `/ws/terminal/{fmSessionId}`. The `useFloorManager` hook derives floor manager state from `SessionsContext` (WebSocket-driven) and `ConfigContext`.

### When Floor Manager is Disabled

If `floor_manager.enabled` is false or there is no active floor manager session, the home page falls back to the standard workspace list layout.

### Config Page

The Settings page includes a Floor Manager section with:

- Enable/disable checkbox (takes effect immediately on save)
- Target selector (any configured run target)
- Rotation threshold input

### API Endpoint

`GET /api/floor-manager` returns floor manager runtime status:

```json
{
  "enabled": true,
  "session_id": "abc123",
  "running": true,
  "injection_count": 42,
  "rotation_threshold": 150
}
```

## Daemon Wiring

The floor manager is wired into the daemon startup in `daemon.go`:

```
  daemon.Run()
       │
       ├── Create Config, State, Session Manager
       │
       ├── Create Dashboard Server
       │
       ├── Wire signal callback:
       │     sm.SetSignalCallback(func(sessionID, sig) {
       │       server.HandleAgentSignal(sessionID, sig)   // broadcast to dashboard
       │       if fm != nil && injector != nil {
       │         if sig is from floor manager itself:
       │           if sig.State == "rotate": go fm.HandleRotation()
       │           return  // don't inject FM's own signals back
       │         injector.Inject(sessionID, name, sig)    // forward to FM
       │       }
       │     })
       │
       ├── Wire toggle callback:
       │     server.SetFloorManagerToggle(func(enabled) {
       │       if enabled: startFloorManager()
       │       else: dispose session + stopFloorManager()
       │     })
       │
       ├── If config.floor_manager.enabled:
       │     startFloorManager()
       │       → fm = floormanager.New(cfg, state, sm, homeDir)
       │       → injector = floormanager.NewInjector(ctx, fm, debounceMs)
       │       → server.SetFloorManager(fm)
       │       → fm.Start(ctx)  // spawn session + start monitor
       │
       ├── ... (rest of daemon startup)
       │
       └── On shutdown:
             stopFloorManager()
               → injector.Stop()
               → fm.Stop()
```

The `fmMu` mutex protects the `fm` and `fmInjector` pointers so the toggle callback (from config updates) can safely start/stop the floor manager at runtime.

## Package Structure

```
internal/floormanager/
├── manager.go           # Manager: lifecycle, spawn, monitor, rotation
├── injector.go          # Injector: signal filtering, debounce, tmux injection
├── prompt.go            # GenerateInstructions(), GenerateSettings()
├── manager_test.go      # Manager unit tests
├── injector_test.go     # Injector unit tests (filtering, formatting, debounce)
├── prompt_test.go       # Prompt generation tests
└── integration_test.go  # Integration test: rotation signal handling

internal/signal/signal.go        # Signal struct with Intent/Blockers fields,
                                 # ParseSignalFile() handles multi-line format,
                                 # "rotate" added to ValidStates

internal/state/state.go          # Session.IsFloorManager flag,
                                 # GetFloorManagerSession(),
                                 # UpdateSessionFloorManager()

internal/workspace/ensure/manager.go  # signalCommandWithIntent(),
                                 # signalCommandWithBlockers()
                                 # (enriched hook variants)

internal/config/config.go        # FloorManagerConfig struct,
                                 # GetFloorManagerEnabled/Target/
                                 # RotationThreshold/DebounceMs()

internal/daemon/daemon.go        # Wiring: signal callback, toggle callback,
                                 # startFloorManager/stopFloorManager closures

internal/dashboard/
├── handlers.go                  # /api/floor-manager endpoint,
│                                # FloorManager in ConfigResponse,
│                                # Hide FM from session list,
│                                # Reject "floor-manager" nickname
├── server.go                    # SetFloorManager(), SetFloorManagerToggle(),
│                                # floorManager + floorManagerToggle fields
└── websocket.go                 # (no floor-manager-specific changes)

assets/dashboard/src/
├── hooks/useFloorManager.ts     # Derives FM state from sessions + config contexts
├── components/AgentStatusStrip.tsx  # Agent pill indicators
├── routes/HomePage.tsx          # Two-column layout with embedded terminal
└── routes/ConfigPage.tsx        # Floor Manager settings section
```
