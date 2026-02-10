# Session Resume

Design notes for adding agent-level resume support when spawning sessions.

**Status**: Implemented (v1.1.2)

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

| Mode       | Trigger    | Form Fields     | Command Built          |
| ---------- | ---------- | --------------- | ---------------------- |
| Promptable | (default)  | target + prompt | `claude 'the prompt'`  |
| Command    | `/command` | raw command     | user's literal command |
| Resume     | `/resume`  | target only     | `claude --continue`    |

### Resume Command Per Agent

| Agent       | Resume Command        | Notes                                              |
| ----------- | --------------------- | -------------------------------------------------- |
| Claude Code | `claude --continue`   | Resumes last conversation in the working directory |
| Codex       | `codex resume --last` | Resumes last conversation in the working directory |
| Gemini CLI  | `gemini -r latest`    | Resumes last conversation in the working directory |

### Workspace Selection

Resume can work with either an existing workspace or a new workspace:

- **Existing workspace**: When accessed from `/spawn?workspace_id=X`, spawns into that workspace
- **New workspace**: When accessed from `/spawn`, creates a new workspace using repo + default branch

The conversation state is stored by the agent (e.g., Claude Code stores it in its own data directory), not in the workspace itself.

### Backend Changes

`buildCommand()` in `internal/session/manager.go` currently has two paths (promptable vs. command). Add a third:

```
switch mode {
case "promptable":
    // existing: build command with prompt arg
case "command":
    // existing: use raw command string
case "resume":
    // new: build agent-specific resume command (e.g. "claude --continue")
    // for agents without native resume, fall back to synthesized prompt
}
```

Add a new tool mode `ToolModeResume` in `internal/detect/commands.go`. Each tool returns its resume command parts (e.g. `["claude", "--continue"]`, `["codex", "resume", "--last"]`, `["gemini", "-r", "latest"]`) in `BuildCommandParts()`.

### What This Does NOT Include

- Persisting conversation IDs or agent session state
- Resuming a _specific_ past conversation (only "most recent in this directory")
- Any changes to agent process lifecycle or tmux session management

### Documentation Updates

- `docs/api.md`: document the new `resume` boolean field on `POST /api/spawn`
- `docs/sessions.md`: document the `/resume` spawn mode and slash command

### Workspace Picker Integration

When user types `/resume`, the form enters resume spawn mode (parallel to promptable and command modes):

| Spawn Mode | Prompt Visible | Workspace Selection                                        |
| ---------- | -------------- | ---------------------------------------------------------- |
| Promptable | Yes            | Fresh: repo/branch input; Workspace/prefilled: pre-filled  |
| Command    | No             | Fresh: repo/branch input; Workspace/prefilled: pre-filled  |
| **Resume** | **No**         | **Workspace picker only** (must select existing workspace) |

In resume mode with fresh entry (no `workspace_id`), show a dropdown of existing workspaces. User must select where to resume — cannot create new workspaces in resume mode.

### Testing Requirements

- Unit test: `buildCommand()` with resume mode
- Unit test: `BuildCommandParts()` with `ToolModeResume` for each tool (claude, codex, gemini)
- Unit test: API validation — `resume: true` without `workspace_id` should return 400
- E2E test: full resume flow from dashboard slash command to session spawn

## Open Questions

1. Should `/resume` be available from the CLI (`schmux spawn --resume`) or dashboard-only initially?
   -> dashboard only
2. When Claude Code's `--continue` finds no prior conversation in the directory, it starts fresh — should we warn the user?
   -> no, they can do it in the interactive session
3. Should the home page "Recent Branches" flow default to `/resume` mode instead of the synthesized prompt?
   -> hmmm, could be in the future
