# Refactor: ensure.Workspace as a service

## Problem

`ensure.Workspace()` is a stateless utility function. Callers must pass in
everything it needs — sessions, workspace ID, current target, base tool name.
This pushes decision-making to callers and creates divergent code paths where
Spawn, RefreshOverlay, and daemon startup each wire things differently.

The result: when `GitExclude` was added to `ensure.Workspace()`, it only worked
from RefreshOverlay (the one caller). Spawn never called `ensure.Workspace()` at
all — it called `ClaudeHooks` and `LoreHookScripts` directly. New workspaces
created during a daemon session never got git exclude entries.

## Root cause

The ensure package is treated as a bag of pure functions. Every other server
component (workspace manager, session manager, remote manager) is initialized
with a state store reference and does its own lookups. The ensure package should
work the same way.

## Design

### Give ensure a state reference

```go
// Package ensure — initialized once at daemon startup with state access.

type Ensurer struct {
    state state.StateStore
}

func New(state state.StateStore) *Ensurer {
    return &Ensurer{state: state}
}
```

### ensure.Workspace takes a workspace ID (or path)

Two entry points depending on caller context:

```go
// Called from Spawn — knows the workspace ID and the target being spawned.
// currentTarget is needed because the session doesn't exist in state yet.
func (e *Ensurer) ForSpawn(workspaceID, currentTarget string) error

// Called from RefreshOverlay and daemon startup — no spawn context.
// Sessions are already in state, so no extra info needed.
func (e *Ensurer) ForWorkspace(workspaceID string) error
```

Both call the same internal logic:

```go
func (e *Ensurer) ensureWorkspace(workspacePath string, hasClaude bool) error {
    if hasClaude {
        ClaudeHooks(workspacePath)
        LoreHookScripts(workspacePath)
    }
    GitExclude(workspacePath)
    return nil
}
```

### Claude detection lives inside ensure

```go
// workspaceHasClaude scans sessions in state for the given workspace
// and returns true if any session uses a Claude agent.
func (e *Ensurer) workspaceHasClaude(workspaceID string) bool {
    for _, s := range e.state.GetSessions() {
        if s.WorkspaceID == workspaceID {
            if SupportsHooks(detect.GetBaseToolName(s.Target)) {
                return true
            }
        }
    }
    return false
}
```

`ForSpawn` combines this with the current target:

```go
func (e *Ensurer) ForSpawn(workspaceID, currentTarget string) error {
    w, found := e.state.GetWorkspace(workspaceID)
    if !found {
        return fmt.Errorf("workspace not found: %s", workspaceID)
    }
    hasClaude := e.workspaceHasClaude(workspaceID) ||
        SupportsHooks(detect.GetBaseToolName(currentTarget))
    return e.ensureWorkspace(w.Path, hasClaude)
}
```

`ForWorkspace` just checks existing sessions:

```go
func (e *Ensurer) ForWorkspace(workspaceID string) error {
    w, found := e.state.GetWorkspace(workspaceID)
    if !found {
        return fmt.Errorf("workspace not found: %s", workspaceID)
    }
    return e.ensureWorkspace(w.Path, e.workspaceHasClaude(workspaceID))
}
```

### Callers become trivial

**Daemon startup (daemon.go):**

```go
ensurer := ensure.New(st)
// later...
for _, w := range st.GetWorkspaces() {
    ensurer.ForWorkspace(w.ID)
}
```

**Spawn (session/manager.go):**

```go
ensurer.ForSpawn(w.ID, opts.TargetName)
```

**RefreshOverlay (workspace/overlay.go):**

```go
ensurer.ForWorkspace(workspaceID)
```

### Wiring

The `Ensurer` is created once in daemon startup and passed to the session
manager and workspace manager (same pattern as telemetry, config, etc.).

## What this fixes

1. **bonsai-puzzle-001 bug**: Every spawn calls `ForSpawn` which always runs
   `GitExclude`. No workspace can miss it.

2. **Claude hooks in non-Claude workspaces**: `workspaceHasClaude` gates Claude
   hooks on actual session data. Non-Claude workspaces don't get them.

3. **One path**: Spawn, overlay refresh, and daemon startup all go through the
   same internal `ensureWorkspace` function. Adding a new ensure step means
   adding it in one place.

4. **Callers don't make decisions**: The ensure package owns all logic about
   what a workspace needs. Callers just say "ensure this workspace."

## Agent-specific signaling (not part of ensure)

The agent-specific signaling in Spawn (SignalingInstructionsFile,
AgentInstructions) stays in session/manager.go. These are session-level concerns
(what prompt injection to use for a specific agent), not workspace-level
configuration. Only Claude hooks and lore scripts are workspace-level because
they persist across sessions.
