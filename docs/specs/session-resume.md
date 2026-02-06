# Session Resume

Design notes for adding agent-level resume support when spawning sessions.

**Status**: Exploratory / Not yet implemented

## Current State

Schmux has two spawn modes, toggled via slash command in the prompt textarea:

1. **Promptable** (default) — user writes a prompt, it gets passed to the agent CLI (e.g. `claude 'the prompt'`)
2. **Command** (`/command`) — user writes a raw shell command that runs in tmux directly

There is no way to resume an existing agent conversation. The closest thing is the "Recent Branches" flow on the home page, which calls `POST /api/prepare-branch-spawn` to synthesize a context-reconstruction prompt ("review the branch, summarize findings, ask what to work on next"). This starts a **new** conversation every time.

No conversation or session identity is persisted in `state.json` — the `Session` struct stores `ID`, `WorkspaceID`, `Target`, `TmuxSession`, etc., but nothing about the agent's conversation state.

## Proposed Design

Add a third spawn mode: **Resume** (`/resume`).

The user enters `/resume` in the prompt textarea (same pattern as `/command`). The form reshapes to just target/model selection — no prompt textarea needed. On spawn, the backend builds an agent-specific resume command instead of a prompt-based command.

### Spawn Modes After Change

| Mode | Trigger | Form Fields | Command Built |
|------|---------|-------------|---------------|
| Promptable | (default) | target + prompt | `claude 'the prompt'` |
| Command | `/command` | raw command | user's literal command |
| Resume | `/resume` | target only | `claude --resume` |

### Resume Command Per Agent

| Agent | Resume Command | Notes |
|-------|---------------|-------|
| Claude Code | `claude --resume` | Resumes last conversation in the working directory |
| Codex | N/A | No known resume flag |
| Gemini CLI | N/A | No known resume flag |

For agents without native resume, two options:
- **(a)** Fall back to the synthesized resume prompt (what `prepare-branch-spawn` does today)
- **(b)** Disable/hide those agents in resume mode

Option (a) is more useful — `/resume` means "continue where I left off" regardless of how the agent achieves it.

### Workspace Selection

Resume implies working in an existing workspace. The current spawn form defaults to creating new workspaces (repo + branch). In resume mode, the form needs a workspace picker instead — the user selects *where* to resume.

If entered from a "Recent Branch" on the home page, the workspace context is already implied and can be pre-filled.

### Backend Changes

`buildCommand()` in `internal/session/manager.go` currently has two paths (promptable vs. command). Add a third:

```
switch mode {
case "promptable":
    // existing: build command with prompt arg
case "command":
    // existing: use raw command string
case "resume":
    // new: build agent-specific resume command (e.g. "claude --resume")
    // for agents without native resume, fall back to synthesized prompt
}
```

The `ResolvedTarget` or `Model` struct would need a new field (e.g. `ResumeFlag string`) so the command builder knows what flag to use. Only Claude Code would have this set initially.

### What This Does NOT Include

- Persisting conversation IDs or agent session state
- Resuming a *specific* past conversation (only "most recent in this directory")
- Any changes to agent process lifecycle or tmux session management

## Open Questions

1. Should `/resume` be available from the CLI (`schmux spawn --resume`) or dashboard-only initially?
2. When Claude Code's `--resume` finds no prior conversation in the directory, it starts fresh — should we warn the user?
3. Should the home page "Recent Branches" flow default to `/resume` mode instead of the synthesized prompt?
