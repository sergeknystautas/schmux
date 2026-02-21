# Floor Manager + Lore System: Architecture Review

This document reviews the interaction between the floor manager and lore (continual learning) systems, identifies architectural issues, and proposes improvements.

## System Overview

### Floor Manager

A singleton supervisory agent session that monitors all other sessions via injected `[SIGNAL]` and `[LIFECYCLE]` messages. It orchestrates work via `schmux` CLI commands and maintains persistent memory across restarts via `memory.md`. Runs in `~/.schmux/floor-manager/` with its own generated `CLAUDE.md`.

**Key files:**

- `internal/floormanager/manager.go` — lifecycle (Start, Stop, spawn, monitor, rotation)
- `internal/floormanager/injector.go` — signal filtering, debounce, tmux injection
- `internal/floormanager/prompt.go` — generates CLAUDE.md/AGENTS.md and settings.json

### Lore (Continual Learning)

A three-stage pipeline: agents capture friction (tool failures via `capture-failure.sh`, reflections via `stop-gate.sh`), the backend curates entries via an LLM call on session dispose, and produces proposals to update instruction files for human review.

**Key files:**

- `internal/lore/scratchpad.go` — JSONL parsing, filtering, pruning
- `internal/lore/curator.go` — LLM-based curation of raw entries into proposals
- `internal/lore/proposals.go` — proposal storage and lifecycle
- `internal/workspace/ensure/manager.go` — hook provisioning (signaling + lore)
- `internal/workspace/ensure/hooks/stop-gate.sh` — stop gate (status + reflection)
- `internal/workspace/ensure/hooks/capture-failure.sh` — automatic failure capture

## Issues

### 1. Floor Manager Gets Lore Hooks It Shouldn't Have

**Severity: High**

In `internal/session/manager.go:644-651`, hook provisioning runs unconditionally after workspace resolution — it doesn't check `IsFloorManager`. Since the floor manager uses a Claude Code target, `SupportsHooks("claude")` is true, and both `ClaudeHooks()` and `LoreHookScripts()` run against `~/.schmux/floor-manager/`.

This means the floor manager gets:

1. **`capture-failure.sh`** (PostToolUseFailure) — writes failure entries to `~/.schmux/floor-manager/.schmux/lore.jsonl` for things like running `schmux status` or reading `memory.md`. These are noise, not learning signals.
2. **`stop-gate.sh`** (Stop) — blocks the floor manager from finishing turns until it writes a `reflection` entry. This forces the supervisor to fabricate a reflection like `"none"` on every turn, polluting lore data.

The entries land in `~/.schmux/floor-manager/.schmux/lore.jsonl`, which is never collected by the lore system (it only reads from registered workspace paths), so they're dead writes — but the stop gate still actively blocks the agent.

**Fix:** Guard lore hook provisioning in `session/manager.go`:

```go
if ensure.SupportsHooks(baseTool) {
    if err := ensure.ClaudeHooks(w.Path); err != nil { ... }
    if !opts.IsFloorManager {
        if err := ensure.LoreHookScripts(w.Path); err != nil { ... }
    }
}
```

Note: the signaling hooks (`SessionStart`, `UserPromptSubmit`, `Notification`, etc.) are useful for the floor manager since it uses `$SCHMUX_STATUS_FILE` for self-rotation. Only the lore-specific hooks (`Stop` → `stop-gate.sh`, `PostToolUseFailure` → `capture-failure.sh`) are problematic.

However, the `Stop` hook in `buildClaudeHooksMap()` currently points to `stop-gate.sh` which combines status validation with lore reflection enforcement. Skipping `LoreHookScripts` means the script file won't exist, and the file-existence guard (`[ -f ... ] && ... || true`) will cause the stop hook to silently no-op. This means the floor manager would lose the status-update gate too. See Issue 4 for the clean fix.

### 2. Lore Callback Fires for Floor Manager Dispose

**Severity: Low (harmless but wasteful)**

In `session/manager.go:1290`, the `loreCallback` fires on every session dispose without checking `IsFloorManager`. For the floor manager, `sess.WorkspaceID` is `"floor-manager"` (a synthetic ID), so `m.state.GetWorkspace("floor-manager")` returns false and the callback exits early. This is harmless but inconsistent — the lifecycle callback already skips the floor manager session on line 1302.

**Fix:** Add an `IsFloorManager` guard to match the lifecycle callback pattern:

```go
if m.loreCallback != nil && !sess.IsFloorManager {
```

### 3. `SignalingInstructions` Conflates Two Concerns

**Severity: Medium**

The `SignalingInstructions` constant in `ensure/manager.go:24-81` bundles two unrelated instruction blocks into one string:

1. **Status signaling** (lines 24-68) — how to write to `$SCHMUX_STATUS_FILE`
2. **Friction capture** (lines 70-81) — how to write to `.schmux/lore.jsonl`

These are injected together into instruction files for all non-Claude agents via `AgentInstructions()`. If the floor manager ever used a non-Claude target, it would receive friction capture instructions that don't apply to it.

More generally, the conflation makes it impossible to include signaling instructions without also including lore instructions, or vice versa.

**Fix:** Split into two constants:

```go
const StatusSignalingInstructions = `## Schmux Status Signaling ...`
const FrictionCaptureInstructions = `## Friction Capture ...`
const SignalingInstructions = StatusSignalingInstructions + "\n" + FrictionCaptureInstructions
```

This preserves backward compatibility while allowing selective composition.

### 4. `stop-gate.sh` Combines Status Validation and Lore Reflection

**Severity: Medium**

The `stop-gate.sh` script enforces two unrelated requirements in a single hook:

1. The status file has been updated (schmux signaling concern)
2. A friction reflection entry exists in `.schmux/lore.jsonl` (lore concern)

It blocks the agent if either condition fails. This means:

- You can't disable lore without also losing the status gate
- The floor manager can't use the status gate without also getting the lore gate
- A simpler status-only check (`stopStatusCheckScript` at line 357) already exists but isn't used

**Fix:** Split into two separate hook scripts:

1. `stop-status-check.sh` — gates on status file update only (already exists as an inline constant)
2. `stop-lore-reflection.sh` — gates on lore reflection only (new)

Then configure the `Stop` hook with two matcher groups. The floor manager would only get the status check, while normal coding agents get both. This also enables per-workspace lore opt-out without losing signaling.

### 5. Two Independent Memory Systems With No Integration

**Severity: Low**

The floor manager and lore system both maintain persistent knowledge across session boundaries but are completely unaware of each other:

|                 | Floor Manager (`memory.md`)                            | Lore (`.schmux/lore.jsonl`)                    |
| --------------- | ------------------------------------------------------ | ---------------------------------------------- |
| **Content**     | Operational state: tasks, decisions, operator requests | Institutional knowledge: mistakes, corrections |
| **Format**      | Unstructured markdown                                  | Structured JSONL                               |
| **Written by**  | The floor manager agent                                | Individual coding agents (via hooks)           |
| **Read by**     | The floor manager on startup                           | The lore curator LLM on session dispose        |
| **Persistence** | `~/.schmux/floor-manager/memory.md`                    | Per-workspace + central state JSONL            |

These don't overlap today because `memory.md` is operational ("PR #42 is in review") while lore is institutional ("use `brew install` not `apt-get` on macOS"). But there's a missed opportunity: the floor manager sees all agent friction across all sessions via signals, making it a potential high-quality friction aggregator.

**Recommendation (light-touch):** Inject a `[LIFECYCLE]` message to the floor manager when a lore proposal is created. Something like:

```
[LIFECYCLE] Lore proposal "prop-20260218-..." created for <repo> (3 files, 12 entries)
```

This keeps the floor manager informed without coupling the systems. The floor manager can then escalate to the operator if a proposal looks significant.

**Recommendation (future):** Consider having the floor manager write lore entries based on cross-session pattern observation, replacing the per-agent reflection requirement. The floor manager sees all signals and could produce higher-quality friction entries than individual agents trying to finish turns while being blocked by the stop gate.

### 6. Callback Proliferation on Session Manager

**Severity: Low**

Both systems are wired into the daemon via typed callbacks on the session manager:

```go
sm.SetLoreCallback(...)      // lore system
sm.SetLifecycleCallback(...) // floor manager
sm.SetCompoundCallback(...)  // overlay compounding
```

The session manager has accumulated three callback fields plus signal monitoring, each with different signatures, all reacting to similar events (session create/dispose). The `IsFloorManager` exclusion logic is duplicated at each callback site.

**Recommendation:** Consider a unified event bus where the session manager emits typed events (`SessionCreated`, `SessionDisposed`, `WorkspaceCreated`) and subscribers filter by type. This would:

- Centralize `IsFloorManager` exclusion in one place
- Make event flow explicit and discoverable
- Make it trivial to add the lore→floor-manager notification from Issue 5

This is a medium-term refactor, not urgent.

### 7. `ensure.Workspace()` Has No Session-Role Awareness

**Severity: Low**

The new `ensure.Workspace(path)` function (line 85) unconditionally provisions both Claude hooks and lore scripts. It's called from the overlay refresh path (`internal/workspace/overlay.go:252`), which runs for all workspaces.

Currently the floor manager's directory isn't treated as a workspace by the overlay system (it uses `WorkDir`), so this doesn't cause problems. But `ensure.Workspace()` has no way to know whether a workspace belongs to a floor manager session — it only receives a path. If the architecture changes, this could become an issue.

**Recommendation:** The right place for the `IsFloorManager` guard is at spawn time in `session/manager.go`, not in `ensure.Workspace()`. The ensure layer should remain role-agnostic; the caller decides what to provision.

## Summary

| #   | Issue                                      | Priority   | Effort             | Fix Location                             |
| --- | ------------------------------------------ | ---------- | ------------------ | ---------------------------------------- |
| 1   | Floor manager gets lore hooks              | **High**   | One line           | `session/manager.go:649`                 |
| 2   | Lore callback fires for FM dispose         | **Low**    | One line           | `session/manager.go:1290`                |
| 3   | `SignalingInstructions` conflates concerns | **Medium** | Small              | `ensure/manager.go`                      |
| 4   | `stop-gate.sh` combines status + lore      | **Medium** | Moderate           | `ensure/hooks/`, `ensure/manager.go`     |
| 5   | No lore→floor-manager notification         | **Low**    | Small              | `daemon/daemon.go`                       |
| 6   | Callback proliferation                     | **Low**    | Larger             | `session/manager.go`, `daemon/daemon.go` |
| 7   | `ensure.Workspace()` role-unaware          | **Low**    | None (design note) | N/A                                      |

Issues 1 and 2 are immediate fixes. Issues 3 and 4 are the right structural cleanup to do alongside them. Issues 5-7 are future improvements.
