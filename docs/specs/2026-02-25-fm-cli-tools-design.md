# CLI Tools for Floor Manager Agency

## Overview

Five new CLI commands that give the floor manager deep visibility into sessions and workspaces, the ability to communicate with agents, and a complete view of version control state across the factory. These commands are general-purpose — any user or script can run them — but they're designed primarily for the FM agent to use autonomously. All VCS operations use the existing `vcs.CommandBuilder` abstraction, so they work identically with git and sapling.

All commands hit the daemon's HTTP API (same pattern as existing CLI commands like `spawn`, `list`, `dispose`). The daemon does the actual work since it has access to session state, tmux sessions, workspaces, and event files. The CLI is a thin client.

### Commands at a Glance

| Command                                | Verb        | Target    | Purpose                       |
| -------------------------------------- | ----------- | --------- | ----------------------------- |
| `schmux tell <session> -m "..."`       | Communicate | Session   | Send a message to an agent    |
| `schmux events <session> [flags]`      | Observe     | Session   | Read event history            |
| `schmux capture <session> [--lines N]` | Observe     | Session   | Read recent terminal output   |
| `schmux inspect <workspace>`           | Understand  | Workspace | Full VCS state report         |
| `schmux branches`                      | Understand  | All       | Bird's-eye workspace overview |

Supporting changes: spawn prompt written to events file, `UserPromptSubmit` hook removes truncation and tags FM-sourced prompts, FM instructions updated with new commands.

---

## `schmux tell`

**Syntax:**

```
schmux tell <session-id> -m "message"
```

**Behavior:**

1. CLI sends `POST /api/sessions/{id}/tell` with `{"message": "..."}` body
2. Daemon looks up the session's tmux session name
3. Daemon injects `[from FM] <message>` via `tmux.SendLiteral()` + `tmux.SendKeys("Enter")`
4. Returns success/failure to CLI

The `[from FM]` prefix is added server-side, not by the caller. This prevents spoofing — even if something other than the FM calls `tell`, the prefix is always applied. The agent and any human observer see the attribution.

**Hook integration:** The agent's existing `UserPromptSubmit` hook fires when the injected text is processed. The hook detects the `[from FM]` prefix and writes the status event with `"source":"floor-manager"` instead of the default.

**Output:**

```
Message sent to session schmux-001-abc12345.
```

**Errors:**

- Session not found -> exit 1
- Session not running (tmux session dead) -> exit 1
- tmux send failure -> exit 1

**Pre-approved** in FM's `.claude/settings.json`: `Bash(schmux tell*)`

---

## `schmux events`

**Syntax:**

```
schmux events <session-id> [--type status|failure|reflection|friction] [--last N]
```

**Behavior:**

1. CLI sends `GET /api/sessions/{id}/events?type=...&last=...`
2. Daemon reads the session's `.schmux/events/<session-id>.jsonl` file
3. Parses each line, applies type filter if specified
4. Returns the last N events (default: all) in chronological order

**Output (human-readable, default):**

```
schmux-001-abc12345 events:

  12:04:01  status   working     "Implement OAuth2 token refresh"
  12:04:01  status   working     [from FM] "Focus on the auth module first"
  12:07:23  failure  build       tool=Bash error="exit status 1" category=build_failure
  12:09:45  status   needs_input "Need clarification on token expiry format"
  12:14:02  status   working     "Adding unit tests for token refresh"
  12:18:30  status   completed   "Auth module finished"
```

The first event is always the spawn prompt (written at spawn time). `[from FM]` tagged events are visually distinct. Timestamps are local time, short format.

**`--type` filter examples:**

```bash
schmux events abc123 --type status        # only status transitions
schmux events abc123 --type failure       # only tool failures
schmux events abc123 --last 5             # last 5 events of any type
schmux events abc123 --type status --last 3  # last 3 status events
```

**Errors:**

- Session not found -> exit 1
- No events file (session never started) -> empty output, exit 0

**Pre-approved:** `Bash(schmux events*)`

---

## `schmux capture`

**Syntax:**

```
schmux capture <session-id> [--lines N]
```

**Behavior:**

1. CLI sends `GET /api/sessions/{id}/capture?lines=...`
2. Daemon calls `tmux capture-pane -t <tmux-session> -p -S -N` to grab the last N lines of terminal output (default: 50)
3. Returns the raw text

This is the terminal's scrollback buffer — what you'd see if you attached to the tmux session and scrolled up. It includes agent output, tool results, status lines, everything visible in the terminal.

**Output:**

```
$ schmux capture schmux-001-abc12345 --lines 20

  * Read src/auth/token.go
  I see the token refresh logic. The current implementation doesn't handle
  expired refresh tokens. Let me fix that.

  * Edit src/auth/token.go
  Added expiry check before refresh attempt.

  * Bash: go test ./src/auth/...
  ok  src/auth  0.342s

  All tests pass. The token refresh now properly handles expired refresh
  tokens by requesting a new auth code flow.
```

**Default lines:** 50. This is enough to see recent activity without dumping the entire scrollback. The FM can request more with `--lines 200` if it needs deeper context.

**tmux command:**

```bash
tmux capture-pane -t <session> -p -S -<N>
```

- `-t <session>` — target the session's pane
- `-p` — output to stdout (instead of a tmux paste buffer)
- `-S -<N>` — start capture N lines before the current cursor position (negative = scrollback)

**Edge cases:**

- If the session has fewer than N lines of history, tmux returns whatever exists — no error
- If the pane has active alternate screen (e.g., agent is in a full-screen editor), `capture-pane` captures the alternate screen content. This is a known tmux behavior — rare for coding agents.
- Empty output (fresh session, nothing rendered yet) -> return empty string, exit 0

**Errors:**

- Session not found -> exit 1
- Session not running (no tmux session) -> exit 1, message: "Session is not running"
- tmux capture failure -> exit 1

**Pre-approved:** `Bash(schmux capture*)`

---

## `schmux inspect`

**Syntax:**

```
schmux inspect <workspace-id>
```

**Behavior:**

1. CLI sends `GET /api/workspaces/{id}/inspect`
2. Daemon resolves the workspace path, determines the VCS type (git or sapling), and runs VCS commands via `vcs.CommandBuilder`:
   - `CurrentBranch()` — current branch
   - `RevListCount()` — ahead/behind main
   - `RevListCount()` — ahead/behind origin branch (if pushed)
   - `Log()` / `LogRange()` — commits ahead of main
   - `StatusPorcelain()` — uncommitted changes
   - `RemoteBranchExists(branch)` — whether branch exists on origin
3. Assembles into a structured report

**Output:**

```
schmux-001 (schmux)

  Branch:  feature/oauth-refresh
  Pushed:  yes (origin/feature/oauth-refresh)
  vs main: +5 commits, -0 behind

  Commits (not in main):
    a1b2c3d  feat: add token expiry check
    e4f5g6h  test: add refresh token tests
    i7j8k9l  fix: handle expired refresh tokens
    m0n1o2p  refactor: extract token validator
    q3r4s5t  feat: initial OAuth2 refresh flow

  Uncommitted:
    M src/auth/token.go
    M src/auth/token_test.go
    A src/auth/validator.go
```

If the branch hasn't been pushed:

```
  Pushed:  no
  vs main: +3 commits, -0 behind
```

If behind main:

```
  vs main: +5 commits, -2 behind
```

**Errors:**

- Workspace not found -> exit 1
- VCS errors (no repo, no remote) -> partial output with warnings

**Pre-approved:** `Bash(schmux inspect*)`

---

## `schmux branches`

**Syntax:**

```
schmux branches
```

**Behavior:**

1. CLI sends `GET /api/branches`
2. Daemon iterates all workspaces, determines VCS type per workspace, and runs lightweight queries via `vcs.CommandBuilder`:
   - `CurrentBranch()` — current branch
   - `RevListCount()` — ahead/behind main
   - `RemoteBranchExists()` — whether branch exists on origin
   - `StatusPorcelain()` — whether working tree is dirty
   - Count of running sessions and their states from session manager
3. Returns a compact table

**Output:**

```
Workspace          Branch                    Main     Origin       Dirty  Sessions
schmux-001         feature/oauth-refresh     +5 -0    pushed       yes    2 (working, needs_input)
schmux-002         fix/login-bug             +1 -0    pushed       no     1 (idle)
myproject-001      main                       — —     up-to-date   no     0
myproject-002      feature/dark-mode         +8 -2    not pushed   yes    3 (working, working, error)
```

Session states are listed in parentheses after the count. This lets the FM instantly spot problems — an `error` or `needs_input` in the list jumps out without needing to drill into each workspace.

**Sorting:** By workspace name (alphabetical).

**Empty state:** If no workspaces exist:

```
No workspaces.
```

**Errors:**

- Daemon not running -> exit 1
- Individual VCS errors per workspace -> show workspace with `(error)` instead of crashing

**Pre-approved:** `Bash(schmux branches*)`

---

## Supporting Changes

### 1. Write spawn prompt to events file

In `internal/session/manager.go`, after the events file is created and before the tmux session is spawned, write the initial event:

```json
{
  "type": "status",
  "state": "working",
  "message": "Session spawned",
  "intent": "<full spawn prompt>",
  "ts": "..."
}
```

This uses the existing `events.AppendEvent()` function. The spawn prompt is not truncated — it gets the full text. This is the canonical record of what the agent was asked to do.

For non-promptable targets (like `zsh`), the intent field is omitted.

### 2. `UserPromptSubmit` hook: remove truncation, fix JSON escaping, detect FM source

Three changes to `statusEventWithContextCommand()` in `internal/workspace/ensure/manager.go`:

- **Remove truncation:** Remove `cut -c1-100` from the pipeline. Prompts are captured in full.
- **Fix JSON construction:** Replace `printf` with string interpolation with `jq -n --arg` for safe JSON output. This handles quotes, newlines, and special characters in prompts of any length.
- **Detect FM source:** After extracting the prompt, check if it starts with `[from FM] `. If so, add `"source":"floor-manager"` to the event JSON.

### 3. Update FM instructions and permissions

In `internal/floormanager/prompt.go`:

`GenerateInstructions()` — add to the "Available Commands" section:

```
- schmux tell <session-id> -m "message" — send a message to an agent
- schmux events <session-id> [--type T] [--last N] — read session event history
- schmux capture <session-id> [--lines N] — read recent terminal output
- schmux inspect <workspace-id> — full VCS state report
- schmux branches — bird's-eye view of all workspaces
```

`GenerateSettings()` — add to the pre-approved list:

```
Bash(schmux tell*)
Bash(schmux events*)
Bash(schmux capture*)
Bash(schmux inspect*)
Bash(schmux branches*)
```

### 4. Extend VCS CommandBuilder for inspect/branches

The existing `vcs.CommandBuilder` interface (`internal/vcs/vcs.go`) covers most of what `inspect` and `branches` need — `DetectDefaultBranch()`, `RevListCount()`, `Log()`, `ResolveRef()`, `MergeBase()`, `DiffNumstat()`, `UntrackedFiles()`. Three new methods are needed:

| New method                                 | Git                                     | Sapling                                  |
| ------------------------------------------ | --------------------------------------- | ---------------------------------------- |
| `CurrentBranch() string`                   | `git branch --show-current`             | `sl whereami`                            |
| `StatusPorcelain() string`                 | `git status --porcelain`                | `sl status`                              |
| `RemoteBranchExists(branch string) string` | `git ls-remote --heads origin <branch>` | `sl log -r "remote(<branch>)" --limit 1` |

Add these to the `CommandBuilder` interface and implement on both `GitCommandBuilder` and `SaplingCommandBuilder`. The `inspect` and `branches` handlers use `vcs.NewCommandBuilder(vcsType)` to get the right builder, same pattern as `handlers_git.go` and `handlers_diff.go`.

This ensures `inspect` and `branches` work identically for git and sapling workspaces — local or remote.

---

## Security Model

**Principle: the FM can see everything and talk to anyone, but cannot destroy anything.**

Two independent safety layers protect the system:

| Layer               | What it controls           | Mechanism                                 |
| ------------------- | -------------------------- | ----------------------------------------- |
| FM tool approval    | What the FM itself can run | `.claude/settings.json` pre-approved list |
| Agent tool approval | What each agent can run    | Each agent's own `.claude/settings.json`  |

**FM pre-approved commands (full updated list):**

- `schmux status`, `schmux list`, `schmux spawn`, `schmux attach`, `schmux end-shift` (existing)
- `schmux tell`, `schmux events`, `schmux capture`, `schmux inspect`, `schmux branches` (new)
- `cat memory.md`, `echo * > memory.md`, `printf * > memory.md` (memory operations)

**FM commands requiring operator approval:**

- `schmux dispose` — destructive
- `schmux stop` — destructive

**`schmux tell` does not create a new trust boundary.** The same access that allows calling `tell` already allows connecting to any agent's terminal via the dashboard WebSocket. The `[from FM]` prefix is an attribution marker for observability, not an authentication mechanism.

**`schmux tell` threat model:**

The FM can inject arbitrary text into any agent's terminal. This is an influence vector, not a direct execution vector. The worst case: FM context gets corrupted via prompt injection in event data, corrupted FM uses `tell` to instruct agents to do harmful things.

**Mitigations:**

- Each agent has its own tool approval layer — destructive shell commands, file deletions, force pushes all require operator permission at the agent level, regardless of who asked
- The `[from FM]` prefix creates an audit trail — `schmux events` shows every instruction the FM sent to every agent
- Event field sanitization (existing `StripControlChars`, `QuoteContentField`) prevents terminal escape injection in signals flowing into the FM
- The FM cannot escalate its own permissions — its `settings.json` is regenerated on every spawn

**Data exposure:**

`schmux capture` and `schmux events` may expose secrets visible in terminal output or prompts. This is inherent to the FM's role and equivalent to what the dashboard terminal view already shows. No new attack surface.

---

## API Endpoints

Five new endpoints on the daemon HTTP API, following existing patterns in `internal/dashboard/`:

| Method | Path                           | Handler                  | Purpose                              |
| ------ | ------------------------------ | ------------------------ | ------------------------------------ |
| `POST` | `/api/sessions/{id}/tell`      | `handleTellSession`      | Inject message into session terminal |
| `GET`  | `/api/sessions/{id}/events`    | `handleGetSessionEvents` | Read session event history           |
| `GET`  | `/api/sessions/{id}/capture`   | `handleCaptureSession`   | Read terminal scrollback             |
| `GET`  | `/api/workspaces/{id}/inspect` | `handleInspectWorkspace` | Full VCS state report                |
| `GET`  | `/api/branches`                | `handleGetBranches`      | All workspaces overview              |

**Routing:** `GET` endpoints go in the read-only route group. `POST /tell` goes in the CSRF-protected group (same as `dispose`, `spawn`).

**Response format:** All endpoints return JSON. The CLI commands format JSON into human-readable output. A `--json` flag on each command passes the raw JSON through for scripting.

---

## Remote Session Support

All five commands work with both local and remote sessions. No new remote infrastructure is needed — the remote manager already provides every primitive required.

### Primitive Mapping

| Command           | Local                                    | Remote (already exists)                                          |
| ----------------- | ---------------------------------------- | ---------------------------------------------------------------- |
| `schmux tell`     | `tmux.SendLiteral()` + `tmux.SendKeys()` | `conn.SendKeys(ctx, paneID, text)`                               |
| `schmux capture`  | `tmux capture-pane -p -S -N`             | `conn.CapturePaneLines(ctx, paneID, lines)`                      |
| `schmux events`   | Read local `.schmux/events/<id>.jsonl`   | `conn.RunCommand(ctx, workdir, "cat .schmux/events/<id>.jsonl")` |
| `schmux inspect`  | Run VCS commands locally                 | `conn.RunCommand(ctx, workdir, cb.CurrentBranch())`              |
| `schmux branches` | Iterate local workspaces                 | Mix: local iteration + `conn.RunCommand()` per remote workspace  |

### Handler Pattern

Each handler checks whether the session/workspace is remote and branches accordingly. This follows the established pattern used by `handlers_diff.go` and `handlers_git.go`:

```go
if ws.RemoteHostID != "" {
    conn := s.remoteManager.GetConnection(ws.RemoteHostID)
    if conn == nil || !conn.IsConnected() {
        writeJSONError(w, "remote host not connected", http.StatusServiceUnavailable)
        return
    }
    cb := vcs.NewCommandBuilder(vcsType)
    output, err := conn.RunCommand(ctx, ws.RemotePath, cb.StatusPorcelain())
    // ... parse output
} else {
    // local: run VCS commands directly against ws.Path
}
```

### Remote-Specific Details

**`schmux tell` (remote):** Remote sessions use pane IDs (`%5`) instead of tmux session names. The handler looks up the session's `RemotePaneID` from state and calls `conn.SendKeys(ctx, paneID, "[from FM] " + message + "\n")`. The remote `SendKeys` already handles literal text and Enter key splitting.

**`schmux events` (remote):** The events file path on the remote host is `<workspace-path>/.schmux/events/<session-id>.jsonl`. The handler runs `cat` via `conn.RunCommand()`, then parses the JSONL output the same way it would parse a local file. For large event files, `tail -n <last>` can be used when the `--last` flag is specified.

**`schmux capture` (remote):** `conn.CapturePaneLines(ctx, paneID, lines)` already exists and wraps `capture-pane -e -t %5 -p -S -N`. Returns the scrollback as a string. Identical semantics to local capture.

**`schmux inspect` (remote):** Runs the same VCS commands via `conn.RunCommand()` — same approach as the existing git graph handler. Uses `vcs.CommandBuilder` to generate commands, so git and sapling workspaces work identically.

**`schmux branches` (remote):** Iterates all workspaces. For each remote workspace, runs lightweight VCS queries via `conn.RunCommand()`. If the remote host is disconnected, the workspace row shows `(disconnected)` instead of VCS state. Multiple workspaces on the same remote host reuse the same connection.

### Error Handling

- Remote host not connected -> `503 Service Unavailable` with message "remote host not connected"
- `RunCommand` timeout or failure -> `502 Bad Gateway` with the error detail
- Partial failures in `branches` (some hosts connected, some not) -> return available data, mark disconnected workspaces

---

## Implementation: File Changes

**New files:**

| File                                      | Purpose                                                             |
| ----------------------------------------- | ------------------------------------------------------------------- |
| `cmd/schmux/tell.go`                      | CLI command: parse flags, call API                                  |
| `cmd/schmux/events.go`                    | CLI command: parse flags, call API, format output                   |
| `cmd/schmux/capture.go`                   | CLI command: parse flags, call API                                  |
| `cmd/schmux/inspect.go`                   | CLI command: parse flags, call API, format output                   |
| `cmd/schmux/branches.go`                  | CLI command: call API, format table                                 |
| `internal/dashboard/handlers_tell.go`     | `POST /api/sessions/{id}/tell` handler                              |
| `internal/dashboard/handlers_capture.go`  | `GET /api/sessions/{id}/capture` handler                            |
| `internal/dashboard/handlers_branches.go` | `GET /api/branches` and `GET /api/workspaces/{id}/inspect` handlers |

**Modified files:**

| File                                    | Change                                                                                                    |
| --------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `cmd/schmux/main.go`                    | Add 5 new cases to the command switch, update `printUsage()`                                              |
| `internal/dashboard/server.go`          | Register 5 new routes                                                                                     |
| `internal/dashboard/handlers_events.go` | Add per-session `GET /api/sessions/{id}/events` (currently only has global `GET /api/events/history`)     |
| `internal/session/manager.go`           | Write spawn prompt to events file before creating tmux session                                            |
| `internal/workspace/ensure/manager.go`  | Remove `cut -c1-100` truncation, replace `printf` with `jq -n` for JSON safety, detect `[from FM]` prefix |
| `internal/floormanager/prompt.go`       | Add new commands to `GenerateInstructions()` and `GenerateSettings()`                                     |
| `docs/cli.md`                           | Document the 5 new commands                                                                               |
| `docs/api.md`                           | Document the 5 new endpoints                                                                              |

**No changes needed to:**

- `internal/session/tracker.go`, `internal/events/` — event infrastructure is already sufficient
- `internal/config/` — no new config fields, these commands work with existing state

**Additions to existing packages:**

- `internal/vcs/vcs.go` — add `CurrentBranch()`, `StatusPorcelain()`, `RemoteBranchExists(branch)` to the `CommandBuilder` interface
- `internal/vcs/git.go` — implement the three new methods for git
- `internal/vcs/sapling.go` — implement the three new methods for sapling
- `internal/tmux/tmux.go` — add `CapturePane()` wrapper if it doesn't already exist
