# schmux Consolidated Architecture Review

**Date:** 2026-04-03
**Reviewers:** Claude (Opus 4.6), Codex, Gemini CLI
**Scope:** Full codebase audit — security, concurrency, persistence, architecture, API design, testing
**Method:** Three independent reviews consolidated by theme, with overlapping findings merged

---

## How to Read This

Findings are grouped by domain, not by reviewer. Where multiple reviewers flagged the same issue, that convergence is noted — independent discovery by separate reviewers is a strong signal. Findings unique to one reviewer are still included when substantive.

Severity tiers:

- **TIER 1** — Fix before these hurt you. Active risk in production use.
- **TIER 2** — Structural problems that compound with every feature added.
- **TIER 3** — Hardening and quality gaps.

---

## TIER 1: Fix These Before They Bite You

### 1. Network exposure + command spawn = unauthenticated RCE

**Flagged by:** Claude, Codex, Gemini (all three — strongest consensus finding)

When `network_access: true`, the dashboard binds to `0.0.0.0:7337` with auth disabled by default. The `/api/sessions/spawn` endpoint accepts a `command` field that flows through `SpawnCommand()` to `tmux new-session` for shell execution. Anyone on the LAN can POST a spawn request and execute arbitrary commands as the developer's user.

The auth default is `false` (`internal/dashboard/auth.go:33`). The spawn path is: `handlers_spawn.go:226` → `manager.go:877-951` → `tmux.go:198-222`.

Gemini additionally notes that preview proxies (`internal/preview/manager.go`) bind to `0.0.0.0` in network-access mode without authentication, exposing dev servers running arbitrary AI-generated code to the LAN.

**Recommendation:** Refuse to enable `network_access` unless auth is also enabled — make this a hard invariant in config validation. Consider removing raw shell command execution from the HTTP API entirely, or gate it behind an explicit `unsafe_http_commands` kill switch. Enforce auth on preview proxies when bound to non-loopback interfaces.

---

### 2. No panic recovery anywhere in production code

**Flagged by:** Claude (primary), Codex (mentioned in runtime robustness roadmap)

Zero `recover()` calls exist in the entire Go codebase. The Go `net/http` server recovers panics in its own goroutines, but every `go func()` launched from handlers is unprotected. There are ~15 background goroutines in `daemon.Run()`, plus per-session tracker goroutines, plus per-WebSocket goroutines. One nil pointer dereference in any background goroutine kills the entire daemon — all sessions orphaned until manual restart.

Unprotected launches at: `daemon.go` lines 496, 499, 765, 783, 1265, 1324, 1331, 1352, 1385; `tracker.go:321`; `localsource.go:141`; broadcast/preview/cleanup goroutines in `server.go`.

**Recommendation:** Add a `safeGo(fn func())` utility that wraps every goroutine launch with `recover()`, logs the panic with stack trace, and increments a metric.

---

### 3. Corrupted state file = bricked daemon

**Flagged by:** Claude, Codex, Gemini (all three)

`state.Load()` returns a hard error on invalid JSON, and `daemon.Run()` treats this as fatal. The atomic write pattern (temp file + rename) protects against process crash, but:

- No `fsync()` before rename — OS crash can lose data on Linux/ext4
- No backup of the previous state file before overwriting
- No fallback to empty state or last-known-good state
- Production has no backup mechanism (dev mode has `.tar.gz` backups)

Codex goes further: the single JSON blob has consistency leaks where related state mutations are not atomic. Workspace manager saves the workspace, then updates the overlay manifest afterward. Preview manager saves preview state, then adds the corresponding tab afterward. Persisted state can temporarily diverge from in-memory state.

Gemini adds: high-frequency updates during rapid agent operations will saturate disk I/O, and constant serialization of large structures creates GC pressure.

**Recommendation (near-term):** Keep a `state.json.bak` (rename old file before writing new). On load failure, try the backup. Log a warning but start with degraded state rather than refusing to start. Add retry-with-backoff to the batched save path.

**Recommendation (long-term):** Replace JSON blob state with transactional storage (SQLite via `modernc.org/sqlite`). Separate durable state from ephemeral/cache state. Make related updates transactional.

---

### 4. Secrets visible in process command lines

**Flagged by:** Claude, Codex

`buildEnvPrefix()` (`internal/session/manager.go:1126-1138`) constructs `KEY='value'` strings that appear in the tmux session's command. These are visible via `tmux list-sessions` and `/proc/<pid>/cmdline` to other local users on shared dev machines.

**Recommendation:** Pass secrets via environment variables on the `exec.Cmd` struct (the `Env` field), not as shell-interpolated command prefixes. If a tool truly requires file-based injection, use short-lived private files with strict permissions.

---

### 5. Core API response types bypass the type generation pipeline

**Flagged by:** Claude, Codex

The two most important API response shapes — `SessionResponseItem` and `WorkspaceResponseItem` — are defined in `internal/dashboard/handlers_sessions.go:20-88`, NOT in `internal/api/contracts/`. They are not processed by `cmd/gen-types`. The corresponding TypeScript types in `assets/dashboard/src/lib/types.ts:1-58` are manually maintained.

Every other contract type flows through the generation pipeline, creating a false sense of safety. A developer adds a field to `WorkspaceResponseItem` in Go, the TypeScript builds fine, the dashboard silently ignores the new data. `state.ResolveConflict` is exposed directly in the API response without going through the contracts package.

WebSocket message shapes are also not in the type generation pipeline — changes require manual updates on both Go and TypeScript sides.

**Recommendation:** Move `SessionResponseItem` and `WorkspaceResponseItem` into `internal/api/contracts/`. Include them in the type generation pipeline. Do the same for WebSocket message payloads.

---

## TIER 2: Structural Problems That Compound Over Time

### 6. The codebase is dominated by god files and god objects

**Flagged by:** Claude, Codex

Representative sizes:

| File                     | LOC  | Role                                  |
| ------------------------ | ---- | ------------------------------------- |
| `daemon/daemon.go`       | 1880 | Initialization + all subsystem wiring |
| `dashboard/server.go`    | 1812 | 40+ field god struct                  |
| `dashboard/websocket.go` | 1478 | Terminal WS handler monolith          |
| `session/manager.go`     | 1605 | Session lifecycle                     |
| `workspace/manager.go`   | 1559 | Workspace lifecycle                   |
| `state/state.go`         | 1308 | All mutable state                     |
| `terminalStream.ts`      | 1818 | Frontend terminal logic               |
| `api.ts`                 | 1387 | Frontend API layer                    |

`daemon.Run()` is a 1400-line initialization function. Everything is wired together via closures that capture local variables. The lore curation callback alone is 150 lines of inline business logic embedded in daemon wiring. The `Server` struct has 40+ fields covering HTTP, WebSocket, auth, CSRF, rate limiting, models, personas, lore, emergence, curation, remote, tunnel, preview, repofeed, dev mode diagnostics, and more.

**Recommendation:** Extract subsystem initialization into a builder or registry pattern. Decompose `Server` into sub-servers or handler groups. Extract terminal WebSocket into a state machine with injectable dependencies. Separate workspace orchestration from VCS operations from persistence concerns.

---

### 7. tmux isolation and integration model

**Flagged by:** Codex (primary — tmux socket isolation), Gemini (primary — control mode split-brain)

**Shared tmux universe (Codex):** schmux uses the default tmux server rather than an isolated socket. It shares namespace with unrelated user sessions, relies on global tmux process state, and mutates shared tmux server environment via `CleanTmuxServerEnv()`. This is insufficient for a multi-session orchestration daemon.

**Split-brain operation (Gemini):** schmux uses `tmux -CC` (control mode) for streaming in `localsource.go` and `remotesource.go`, but simultaneously shells out via `exec.CommandContext("tmux", ...)` for `CaptureLastLines`, `KillSession`, `GetPanePID`, and `SendKeys`. Active control mode connections and manual CLI calls compete for the same buffers and state. Spawning new OS processes for every state check or bootstrap capture is inefficient under high load.

**Recommendation:** Run schmux on its own tmux socket via `-L` or `-S`. Transition the `internal/tmux` package to maintain persistent control mode client connections and issue commands through the existing socket rather than spawning CLI processes.

---

### 8. Terminal output hot path contamination

**Flagged by:** Codex

`internal/session/tracker.go` calls the output callback inline on every output event. That callback is wired to `internal/dashboard/preview_autodetect.go`, which can do: URL scanning, PID-tree discovery, port ownership lookup, HTTP probes, and preview creation. This is side work hanging directly off the terminal delivery path.

**Recommendation:** Make the output callback asynchronous. Queue preview autodetect work behind a bounded worker. Dropping or delaying preview detection is acceptable — terminal delivery should not pay for it.

---

### 9. Event handler data race

**Flagged by:** Claude

`session.Manager.eventHandlers` (`internal/session/manager.go:48`) is a `map[string][]events.EventHandler` that can be written by floor manager toggle (from HTTP handlers via `daemon.go:649-694`) and read by tracker goroutines during event processing. No mutex protects this field. The `fmMu` in `daemon.go:655` only serializes toggle operations, not concurrent reads from event processing goroutines.

**Recommendation:** Protect with `sync.RWMutex` or use `atomic.Pointer[map[string][]events.EventHandler]`.

---

### 10. WebSocket terminal handler leaks goroutines

**Flagged by:** Claude

When a browser tab closes while the session is alive, `handleTerminalWebSocket` exits but leaves two polling goroutines running: session liveness check (500ms ticker) and control mode health monitor (1s ticker). These tick indefinitely until the session dies. In a multi-tab scenario, this leaks 4 goroutines per closed tab.

**Recommendation:** Add a `done` channel that the handler closes on return. All spawned goroutines select on both `<-sessionDead` and `<-done`.

---

### 11. WebSocket runtime lacks production hardening

**Flagged by:** Codex

No production use of: read deadlines, write deadlines, ping/pong keepalive handling, or a formalized protocol contract. The terminal WebSocket uses a mix of binary frames (sequenced terminal data), text/JSON frames (control messages), and client-to-server messages with ad-hoc type definitions.

**Recommendation:** Add deadlines and keepalive behavior. Version the WebSocket protocol. Make protocol types first-class generated contracts. Break the handler into explicit phases: bootstrap, input, replay, sync, diagnostics, shutdown.

---

### 12. Global mutable state and package-level side effects

**Flagged by:** Codex

Package-level tmux binary/checker state, package-level loggers, adapter registry via `init()`, mutable event handler map wiring, callback-heavy daemon composition. At the current codebase size this taxes testability, hot reconfiguration, and reasoning.

**Recommendation:** Reduce package globals. Use explicit constructor-time dependency wiring. Prefer immutable snapshots or atomic replacement over shared mutable maps.

---

### 13. PID polling and process identity

**Flagged by:** Gemini

`internal/session/manager.go` relies on `os.FindProcess(sess.Pid)` and Signal 0 to determine if a session is running. On long-running systems, PIDs are recycled. schmux may incorrectly report a session as "Healthy" if a different process occupies the recycled PID of a crashed agent.

**Recommendation:** Incorporate process start-time verification (e.g., checking `btime` in Linux `/proc`). Alternatively, rely entirely on the tmux internal session registry as the single source of truth for "running" status.

---

## TIER 3: Quality & Hardening Gaps

### 14. API error responses are inconsistent

**Flagged by:** Claude

Some handlers use `writeJSONError()` (JSON), others use `http.Error()` (plain text). The frontend can't reliably parse error responses. Examples: `handleAskNudgenik`, `handleTerminalWebSocket`, `handleProvisionWebSocket`.

**Recommendation:** Standardize all error responses to JSON format via a single error-writing utility.

---

### 15. No input validation on state-changing endpoints

**Flagged by:** Claude

Nicknames accept any string with no length or character restriction. Session IDs are only checked for emptiness, not format. Body size limits not applied uniformly. No validation that repo URLs, branch names, or target names reference valid config entries.

**Recommendation:** Define a validation middleware or utility. Enforce length limits and character restrictions on all user-supplied identifiers.

---

### 16. Source-level event drops are invisible

**Flagged by:** Claude

`LocalSource.emit()` drops events when the 1000-element channel buffer is full, with no counter and no log message. The tracker has `FanOutDrops` counters for subscriber-level drops, but source-level drops are silent.

**Recommendation:** Add an atomic counter for source-level drops. Include it in diagnostic output.

---

### 17. Additional concurrency and lifecycle issues

**Flagged by:** Claude

- **Lock held across process spawn:** `Connect()` and `Reconnect()` in `internal/remote/connection.go` hold `c.mu.Lock()` while calling `pty.StartWithSize()`, a potentially blocking syscall.
- **NudgeNik full-struct replace:** `checkInactiveSessionsForNudge` uses `UpdateSession()` (full struct replace) instead of `UpdateSessionNudge()` (atomic field update), risking data loss from concurrent field updates.
- **Gap replay unbounded:** `buildGapReplayFrames` replays all entries from a sequence number with no rate limiting or chunking — large gaps stall the WebSocket.
- **Stuck disposal not context-aware:** The retry goroutine at `daemon.go:783` does not respect `d.shutdownCtx`, delaying clean process exit by up to 60 seconds.
- **No reverse tmux orphan cleanup:** Startup validates state against tmux, but never discovers tmux sessions that exist without corresponding state entries.
- **No retry on transient state save failures:** Failed batched saves are logged but never retried; pending changes are silently lost until the next mutation triggers a new save. Several `state.Save()` return values are discarded entirely (`daemon.go:101, 461, 481`).

---

### 18. Stale documentation

**Flagged by:** Codex

`docs/react.md` still talks about polling in places that don't reflect the WebSocket-first architecture. `docs/architecture.md` doesn't fully match current package reality. Stale architecture docs create false confidence.

**Recommendation:** Make architecture docs part of change discipline. Prune stale statements aggressively. Document actual runtime invariants, not aspirational ones.

---

### 19. Dev tooling brittleness

**Flagged by:** Gemini

The `pause-watch` plugin in `assets/dashboard/vite.config.js` monkey-patches Vite's internal `server.restart` to prevent reloads during backend Git operations. This relies on Vite's internal implementation details. The project also relies on documentation mandates to prevent running standard Go tests directly, requiring Docker for E2E.

**Recommendation:** Improve frontend resilience to temporary WebSocket disconnects rather than suppressing tool-level restarts. Consider `testcontainers-go` for programmatic E2E lifecycle management.

---

### 20. Startup and lifecycle signaling

**Flagged by:** Codex

`Start()` in the daemon path waits a short fixed delay instead of verifying a real readiness handshake. This becomes a problem as startup gets slower or users depend on automation around daemon lifecycle.

**Recommendation:** Add a real readiness probe or startup handshake. Distinguish "process launched" from "daemon ready."

---

### 21. Naming conventions

**Flagged by:** Codex

Names like `nudgenik`, `floormanager`, and `emergence` hide responsibility behind brand instead of function. They increase ramp-up cost in a growing codebase.

**Recommendation:** Prefer domain-explicit names for new subsystems. Keep creative names out of foundational interfaces and docs.

---

## Testing Gaps

### Critical untested code

| Path                                   | Risk       | Why                                                                                               |
| -------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------- |
| `internal/assets/download.go`          | **HIGH**   | Tar extraction with path-traversal guard — handles external input from GitHub, guard never tested |
| `handleTerminalWebSocket`              | **HIGH**   | Most complex handler (800+ lines), zero unit tests                                                |
| `internal/workspace/origin_queries.go` | **MEDIUM** | ~500 lines of git operations with locking, zero unit tests                                        |
| `internal/github/discovery.go`         | **MEDIUM** | Concurrent poll/stop lifecycle, zero unit tests                                                   |
| `internal/session/tmux_health.go`      | **MEDIUM** | Ring buffer stats with percentile computation, zero unit tests                                    |
| `handlers_sync.go`                     | **MEDIUM** | Git rebase/merge on live worktrees, zero unit tests                                               |
| `keyclassify.go`                       | **MEDIUM** | Key classification for remote input, only bench test, no correctness test                         |

### Missing test categories (flagged by Claude, Codex)

- **Zero** fuzz tests (strong candidates: tar extraction, WebSocket message parsing, control mode protocol, config JSON parsing, ANSI/control-sequence handling, path handling)
- **Zero** property-based tests (strong candidates: ANSI parser, key classifier, git DAG layout)
- **Zero** chaos/fault-injection tests
- **Zero** tests for daemon crash recovery scenarios
- **Zero** tests for concurrent spawning to the same branch

### Weak tests providing false confidence

- `TestSourceEventConstruction` — only accesses struct fields, a compilation test
- `TestBroadcastToSession` — uses `recover()` to catch panics from nil internals, tests registry not broadcast
- `TestStatus_NoPidFile` — skips itself when a daemon is running (may always skip in CI)
- `handleClearNudgeOnInput` tests — use `time.Sleep(50ms)`, flaky under CI contention

---

## What's Actually Good

All three reviewers independently noted real engineering discipline:

- **Atomic writes** for config and state files (temp+rename pattern)
- **Deep-copy on read** — workspaces returned from state are copies, preventing shared state mutation
- **Fine-grained locking** — workspace manager has six independent mutexes, no deadlock risk from lock ordering
- **Non-blocking channel sends** with drop counters in subscriber fan-out
- **Batched saves** with debounce — prevents I/O saturation during rapid state changes
- **`wsConn` wrapper** — correct mutex discipline for gorilla/websocket concurrent writes
- **CSRF protection** with constant-time comparison, origin checking, rate limiting
- **Path traversal protection** with `isPathWithinDir()`, extension allowlists, `.gitignore` checks
- **Shell quoting** via `shellutil.Quote()` consistently applied
- **E2E test isolation** — each test gets its own HOME, tmux socket, ephemeral port
- **`shutdownOnce`/`devRestartOnce`** — prevent double-close panics
- **Config-vs-state separation** — clean conceptual boundary between user intent and runtime truth
- **Interface-based decoupling** — `StateStore` and `WorkspaceManager` interfaces prevent concrete dependencies
- **Broad automated test coverage** (2433+ tests passing)

---

## Recommended Roadmap

### Phase 1 — Safety and containment

1. Require auth for any non-loopback exposure (hard invariant in config validation)
2. Isolate schmux on its own tmux socket
3. Remove or hard-gate raw HTTP command spawn
4. Stop placing secrets in command-line prefixes
5. Add panic recovery to all goroutines

### Phase 2 — Durability

1. Add state file backup + graceful degradation on corruption
2. Add retry-with-backoff to batched save path
3. Make related state mutations atomic
4. Classify durable vs ephemeral state explicitly
5. Long-term: replace JSON blob with transactional storage (SQLite)

### Phase 3 — Runtime robustness

1. Fix event handler data race
2. Add `done` channel to WebSocket terminal handlers (goroutine leak)
3. Remove slow side work from terminal hot paths
4. Add WebSocket deadlines and keepalives
5. Add real startup/readiness signaling
6. Cap gap replay at configurable maximum

### Phase 4 — Architectural decomposition

1. Split daemon lifecycle wiring from subsystem business logic
2. Break up dashboard server into smaller services
3. Turn terminal WebSocket into a protocol/state-machine package
4. Untangle workspace orchestration from VCS operations from persistence
5. Consolidate tmux integration on persistent control mode connections

### Phase 5 — Contract, testing, and doc discipline

1. Move core response types into `internal/api/contracts/`
2. Generate WebSocket protocol types
3. Add fuzz tests for parsers and external input handlers
4. Add unit tests for terminal WebSocket state machine (after extraction)
5. Bring docs back into alignment with reality

---

## Final Verdict

All three reviewers converge on the same assessment: schmux has real engineering quality and strong product instincts, but it has outgrown its original architectural tolerances.

The security boundary (network access without auth), the persistence model (single JSON blob), and the implementation shape (god files, monolithic handlers) were acceptable choices for a power-user tool. They are not acceptable for a multi-session orchestration platform.

The correct next move is **containment, durability, and decomposition** — not more surface area.
