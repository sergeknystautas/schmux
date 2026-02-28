# Floor Manager Design

## Overview

The floor manager is a singleton agent session that acts as the conversational counterpart to the entire schmux system. It monitors all agents via the unified event pipeline, relays status to the human operator, and executes orchestration commands on their behalf — all through natural language dialogue in a terminal.

It is not a workspace session. It runs as a **peer to the session manager** — managing its own tmux session directly via the `tmux` package, with no workspace, no event hooks, and no presence in the session list. The daemon holds a reference to it and feeds it events by injecting text into its terminal.

## Core Principles

- **Agent-agnostic.** Any configured run target (Claude, Codex, Gemini, custom). Users choose based on cost/capability tradeoff.
- **No new infrastructure.** Reuses the existing event pipeline, tmux package, and WebSocket terminal streaming. The FM registers as an event handler alongside `DashboardHandler`.
- **Mobile-first.** The home page reorganizes around the FM terminal as the primary interface. On mobile, workspace status becomes a tab strip and the terminal fills the viewport.
- **Event-driven.** The FM reacts to `StatusEvent` objects from the unified pipeline, not polling or signal files.
- **Durable.** Survives crashes through external memory (`memory.md`) and a restart fallback chain (resume → fresh spawn → retry).
- **Least privilege.** Destructive commands (`dispose`, `stop`) are not pre-approved. The agent must request operator permission through Claude Code's tool approval system, regardless of context compaction.

## Architecture and Data Flow

The floor manager consists of two components in `internal/floormanager/`:

**Manager** — Owns the tmux session lifecycle. Creates `~/.schmux/floor-manager/`, writes instruction files (`CLAUDE.md`, `memory.md`), spawns tmux sessions via `tmux.CreateSession()`, monitors liveness (polling every 15s), and handles restart/rotation. Singleton — only one FM can exist at a time.

**Injector** — An `events.EventHandler` registered in the daemon's event pipeline alongside `DashboardHandler`. Receives `StatusEvent` objects, applies filtering rules, batches within a debounce window, formats as text, and injects into the FM's terminal via `tmux.SendKeys("C-u")` (clear partial input) + `tmux.SendLiteral()` + `tmux.SendKeys("Enter")`. The Ctrl+U clear-before-inject pattern prevents collisions with operator typing — without it, if the operator is mid-keystroke when a signal fires, the signal text gets appended to their partial input, garbling both messages.

```
  Agent Session (e.g. claude-1)           Floor Manager Session
  ─────────────────────────               ─────────────────────
         │                                         ▲
         │ writes JSONL event                      │ [SIGNAL] text
         │ to .schmux/events/{id}.jsonl            │ via tmux.SendLiteral
         ▼                                         │
  ┌──────────────────┐                    ┌────────┴──────────┐
  │  EventWatcher    │                    │  tmux terminal    │
  │  (fsnotify)      │                    │  (floor-manager)  │
  └────────┬─────────┘                    └────────▲──────────┘
           │                                       │
           ▼                                       │
  ┌────────────────────────┐              ┌────────┴──────────┐
  │  Event Handlers        │              │    Injector        │
  │                        │              │                    │
  │  1. DashboardHandler   │              │  1. Filter         │
  │     → WebSocket        │              │  2. Format         │
  │                        │              │  3. Debounce       │
  │  2. Injector           │──────────────│  4. tmux.Send      │
  │     → Floor Manager    │              │  5. Count          │
  └────────────────────────┘              └───────────────────┘
```

The FM also receives direct human input via `/ws/terminal/{tmuxSession}` — the same WebSocket terminal streaming used by all tmux sessions. The `[SIGNAL]` prefix distinguishes system events from human messages. Two input sources, one terminal.

### Input Collision Prevention

Both the operator (via WebSocket) and the Injector (via `tmux send-keys`) write to the same terminal PTY. Without coordination, signal injection interleaves with operator keystrokes, garbling both messages.

The **clear-before-inject** pattern prevents this: before every injection, the Injector sends `Ctrl+U` (unix-line-discard), which clears any partial input on the current line in readline-based shells and many TUI inputs. The operator may need to re-type their draft, but this prevents the far worse experience of garbled signal+typing going to the agent.

This pattern is applied consistently in three places:

- `Injector.flush()` — signal injection
- `handlers_tell.go` — FM→agent tell messages
- `Manager.handleShiftRotation()` — `[SHIFT]` rotation messages

## Event Injection Rules and Format

The Injector filters `StatusEvent` objects to avoid flooding the FM with noise. It tracks previous state per session to evaluate transitions.

### Filtering Rules

| Transition                             | Action                      |
| -------------------------------------- | --------------------------- |
| Any → `error`                          | Always inject               |
| Any → `needs_input`                    | Always inject               |
| Any → `needs_testing`                  | Always inject               |
| Any → `completed`                      | Always inject               |
| `working` → `working`                  | Skip (agent still chugging) |
| Any → `working`                        | Skip                        |
| Multiple events within debounce window | Batch into single injection |

### Format

Compact single line, empty fields omitted. Session nickname used when available, falling back to session ID. Previous state included to help the FM understand transitions without running commands.

Minimal (no intent/blockers):

```
[SIGNAL] claude-1: working -> completed "Auth module finished"
```

Full (all fields present):

```
[SIGNAL] claude-1: working -> needs_input "Need clarification on auth token format" intent="Implementing OAuth2 token refresh" blocked="Unknown token expiry requirement"
```

### Debouncing

Events within the `debounce_ms` window (default 2000ms) are batched and sent as a single newline-joined block. This handles bursts like multiple agents transitioning simultaneously.

### Injection Count

After each flush, the Injector increments a counter on the Manager. When this reaches `rotation_threshold`, the rotation flow triggers.

## Rotation and Shift Mechanism

Rotation is entirely daemon-driven. The FM does not self-request rotation — the daemon decides based on injection count.

### Flow

1. Injection count reaches `rotation_threshold` (default 150)
2. Injector sends `[SHIFT]` message (with Ctrl+U clear-before-inject) via `tmux.SendLiteral`:
   ```
   [SHIFT] Forced rotation imminent. Save your summary to memory.md, then run `/path/to/schmux end-shift`. Do not acknowledge this message to the operator.
   ```
   The `[SHIFT]` message uses the absolute path to the currently running binary (resolved via `os.Executable()` at Manager construction time) rather than bare `schmux`, ensuring the FM calls the same binary that is currently running — critical in dev mode where the PATH binary may be out of sync.
3. FM saves `memory.md` and runs `schmux end-shift`
4. `schmux end-shift` hits `POST /api/floor-manager/end-shift`, daemon rotates immediately
5. If `schmux end-shift` is not received within 30s, daemon force-rotates anyway

### `schmux end-shift` Command

New CLI subcommand that calls `POST /api/floor-manager/end-shift` on the daemon. Pre-approved in the FM's `.claude/settings.json`. This is the FM's only communication channel back to the daemon — it has no event file, no hooks, no signal mechanism.

### Rotation Execution (`Manager.HandleRotation`)

1. Dispose current tmux session (`tmux.KillSession`)
2. Wait `restartDelay` (3s)
3. Spawn fresh session with `"Begin."` prompt
4. Reset injection count
5. New session reads `memory.md` on startup per its `CLAUDE.md` instructions

A mutex guard prevents concurrent rotations from racing. The `[SHIFT]` send is best-effort — if tmux send fails, force rotation still proceeds after timeout.

### Restart Fallback Chain (for crashes, not rotation)

```
  Session exits
       │
       ▼
  ┌────────────────────────────────┐
  │  1. Try resume                 │
  │     (agent's --resume flag)    │──── Success ──► Running
  │     preserves conversation     │                 (full context
  │                                │                  preserved)
  └───────────┬────────────────────┘
              │ Failure
              ▼
  ┌────────────────────────────────┐
  │  2. Fresh spawn                │
  │     Prompt: "Begin."           │──── Success ──► Running
  │     Agent reads memory.md on   │                 (reads memory.md
  │     startup per CLAUDE.md      │                  for continuity)
  └───────────┬────────────────────┘
              │ Failure
              ▼
         Log error,
         retry on next
         monitor tick (15s)
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

| Field                | Type   | Default | Description                           |
| -------------------- | ------ | ------- | ------------------------------------- |
| `enabled`            | bool   | false   | Spawn FM on daemon start              |
| `target`             | string | —       | Any valid run target name             |
| `rotation_threshold` | int    | 150     | Max injections before forced rotation |
| `debounce_ms`        | int    | 2000    | Batch window for rapid events         |

Toggling `enabled` takes effect immediately on config save — the daemon starts or stops the FM without restarting.

## Context Durability

### Working Directory

`~/.schmux/floor-manager/`

At spawn time, the Manager writes:

| File                    | Purpose                                                   |
| ----------------------- | --------------------------------------------------------- |
| `CLAUDE.md`             | Role instructions, CLI reference, behavior rules          |
| `AGENTS.md`             | Same content (for non-Claude agents)                      |
| `.claude/settings.json` | Pre-approves non-destructive commands only (see Security) |
| `memory.md`             | Persistent memory maintained by the agent itself          |

`CLAUDE.md` and `AGENTS.md` are regenerated on every spawn to pick up changes. `memory.md` is never overwritten — it is the agent's long-term memory across rotations and restarts.

The spawn prompt is `"Begin."` — all substantive instructions are in `CLAUDE.md`, which tells the agent to read `memory.md`, run `schmux status`, and proactively summarize when the operator connects.

Generated by `floormanager.GenerateInstructions(schmuxBin)` and `floormanager.GenerateSettings(schmuxBin)`, which accept the resolved binary path so all CLI references in instructions and pre-approved permissions use the currently running binary — not the potentially stale PATH version.

### Pre-Approved Permissions (`.claude/settings.json`)

Least privilege — only non-destructive commands:

- `schmux status` — read-only
- `schmux list` — read-only
- `schmux spawn *` — additive
- `schmux end-shift` — rotation signal
- `cat memory.md` — read memory
- `echo * > memory.md` — write memory

**Not pre-approved** (require operator confirmation via Claude Code's tool approval):

- `schmux dispose` — destructive
- `schmux stop` — destructive

This ensures safety survives context compaction. Even if the agent loses its behavioral instructions after compaction, the tool approval layer prevents autonomous destructive actions.

## Security

### Terminal Escape Injection

The Injector formats `StatusEvent` data (including agent-written messages, intents, blockers) and sends it via `tmux.SendLiteral()`. Event content is sanitized before injection: ANSI escape sequences, control characters, and newlines are stripped from event fields.

### `[SIGNAL]`/`[SHIFT]` Spoofing

An agent's event message could contain `[SIGNAL]` or `[SHIFT]` literal text to trick the FM. Content fields are quoted/delimited in the injection format so they cannot be confused with protocol prefixes.

### XSS in Dashboard

React auto-escapes by default. No `dangerouslySetInnerHTML` with event data. No special action needed.

### Privilege Enforcement

Destructive commands are not pre-approved in `.claude/settings.json`. This is the primary safety mechanism — it operates at the tool approval layer, independent of agent instructions or context state. The agent must request operator permission for destructive operations regardless of whether it remembers being told to do so.

## Dashboard UI

### Home Page — FM Enabled, Desktop

- Hero banner (full width, top, unchanged)
- Below: two columns
  - Left (~60-65%): FM terminal, fills the column height
  - Right (~35-40%): Recent branches, PRs, workspaces, subreddit (same content as current layout, stacked)

The FM terminal uses the shared `useTerminalStream` hook (same as the session detail page) and connects via `/ws/terminal/{fmTmuxSession}`. The `terminal-xterm` CSS class ensures consistent terminal styling (padding, overflow, dimensions) across both the FM terminal and session terminals. The shared `TerminalStream` class handles resize observation, falling back to `parentElement` when the `.session-detail` ancestor is absent (as it is for the FM terminal).

### Home Page — FM Enabled, Mobile

- Hero banner (compact)
- Workspace tab strip: horizontal row of compact pills showing workspace name + running session count. Tapping a tab navigates to that workspace.
- FM terminal fills remaining viewport height

No page scroll — the terminal is the scroll container. Branches, PRs, and subreddit are not rendered on mobile. The operator can ask the FM for that information conversationally.

### Layout Commitment

The layout commits immediately when `enabled` is true, regardless of `running` state. The FM terminal area takes its full space in all three visual states:

1. **Starting** (`enabled && !running`) — FM area shows centered spinner + "Starting floor manager..."
2. **Running** (`enabled && running`) — Live terminal connected
3. **Restarting** (crash recovery) — FM area shows "Reconnecting..." with spinner

No layout jump when the terminal connects. Layout is driven by `enabled`; terminal content is driven by `running` + tmux session availability.

### Home Page — FM Disabled

Current layout unchanged. No two-column shift, no workspace tabs.

### FM State Delivery

FM state changes (starting, running, restarting, stopped) are broadcast over the existing `/ws/dashboard` WebSocket. The `useFloorManager` hook consumes these broadcasts alongside the initial state from `GET /api/floor-manager`.

### Config Page

New "Floor Manager" section with:

- Enable/disable toggle (takes effect immediately on save)
- Target selector dropdown (populated from detected/configured run targets)
- Rotation threshold input

### API Endpoints

`GET /api/floor-manager` returns FM runtime status:

```json
{
  "enabled": true,
  "tmux_session": "floor-manager",
  "running": true,
  "injection_count": 42,
  "rotation_threshold": 150
}
```

`POST /api/floor-manager/end-shift` triggers immediate rotation (called by the FM agent via `schmux end-shift`).

## Daemon Wiring

```
daemon.Run()
     │
     ├── Create Config, State, Session Manager, EventWatcher
     │
     ├── Build event handlers:
     │     handlers["status"] = [
     │       DashboardHandler,    // existing — broadcasts to WebSocket
     │       fmInjector,          // new — forwards to FM terminal
     │     ]
     │
     ├── If config.floor_manager.enabled:
     │     fm = floormanager.NewManager(cfg, homeDir, logger)
     │     fmInjector = floormanager.NewInjector(fm, debounceMs)
     │     register fmInjector in event handlers
     │     fm.Start(ctx)  // spawn tmux + start monitor goroutine
     │
     ├── Wire config toggle callback:
     │     onConfigSave: if floor_manager.enabled changed:
     │       enabled → startFloorManager()
     │       disabled → fm.Stop() + remove injector from handlers
     │
     ├── Register API routes:
     │     GET  /api/floor-manager          → fm status
     │     POST /api/floor-manager/end-shift → trigger rotation
     │
     ├── Broadcast FM state changes over /ws/dashboard
     │     (starting, running, restarting, stopped)
     │
     └── On shutdown:
           fmInjector.Stop()
           fm.Stop()
```

A `fmMu` mutex protects the `fm` and `fmInjector` pointers so the config toggle callback can safely start/stop at runtime.

## Package Structure

```
internal/floormanager/
├── manager.go           # Manager: lifecycle, spawn, monitor, rotation
├── injector.go          # Injector: event handler, filtering, debounce, tmux injection
├── prompt.go            # GenerateInstructions(), GenerateSettings()
├── sanitize.go          # StripControlChars(), QuoteContentField()
├── manager_test.go      # Manager unit tests
├── injector_test.go     # Injector tests (filtering, formatting, debounce)
├── prompt_test.go       # Prompt generation tests
└── sanitize_test.go     # Sanitization tests

internal/events/types.go        # No changes needed — StatusEvent already
                                 # has State, Message, Intent, Blockers

internal/config/config.go       # Add FloorManagerConfig struct,
                                 # getters, contract types

internal/daemon/daemon.go       # Wire FM as event handler, toggle
                                 # callback, API routes, shutdown

internal/dashboard/
├── handlers_floormanager.go    # GET /api/floor-manager
│                                # POST /api/floor-manager/end-shift
├── server.go                   # SetFloorManager(), new fields
└── websocket.go                # Broadcast FM state changes

cmd/schmux/main.go              # Add "end-shift" subcommand
                                 # (calls POST /api/floor-manager/end-shift)

assets/dashboard/src/
├── hooks/useFloorManager.ts    # FM state from /ws/dashboard broadcasts
├── hooks/useTerminalStream.ts  # Shared terminal lifecycle hook (FM + sessions)
├── routes/HomePage.tsx         # Two-column layout, mobile workspace tabs,
│                                # FM terminal area with loading states
└── routes/ConfigPage.tsx       # Floor Manager settings section
```

No changes needed to `internal/session/`, `internal/workspace/`, `internal/state/`, or `internal/events/`. The floor manager is additive — it doesn't modify existing packages.
