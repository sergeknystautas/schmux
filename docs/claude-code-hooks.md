# Claude Code Hooks

Hooks are user-defined shell commands or LLM prompts that execute automatically
at specific points in Claude Code's lifecycle. They provide deterministic control
over agent behavior — enforcing rules, automating side effects, injecting
context, and gating progress.

This document covers what hooks can do, how to configure them, and common
patterns for harnessing agent interactions.

## Lifecycle Events

Every hook is attached to an event. Events fire at specific points during a
Claude Code session:

| Event                | When it fires                                   | Can block? |
| -------------------- | ----------------------------------------------- | ---------- |
| `SessionStart`       | Session begins or resumes                       | No         |
| `UserPromptSubmit`   | Prompt submitted, before the agent processes it | Yes        |
| `PreToolUse`         | Before a tool call executes                     | Yes        |
| `PermissionRequest`  | When a permission dialog would appear           | Yes        |
| `PostToolUse`        | After a tool call succeeds                      | No\*       |
| `PostToolUseFailure` | After a tool call fails                         | No         |
| `Notification`       | When Claude Code sends a notification           | No         |
| `SubagentStart`      | When a subagent is spawned                      | No         |
| `SubagentStop`       | When a subagent finishes                        | Yes        |
| `Stop`               | When the agent finishes responding              | Yes        |
| `TeammateIdle`       | When a teammate is about to go idle             | Yes        |
| `TaskCompleted`      | When a task is being marked completed           | Yes        |
| `PreCompact`         | Before context compaction                       | No         |
| `SessionEnd`         | When the session terminates                     | No         |

\*`PostToolUse` can feed feedback to the agent, but the tool already ran.

`Stop` does not fire on user interrupts — only on normal response completion.

## Configuration

### Structure

Hook configuration has three levels:

1. **Event** — which lifecycle point to respond to
2. **Matcher group** — a regex filter for when the hook fires
3. **Hook handlers** — one or more commands, prompts, or agents to run

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": ".claude/hooks/validate-bash.sh"
          }
        ]
      }
    ]
  }
}
```

### Where to define hooks

| Location                      | Scope                         | Shareable |
| ----------------------------- | ----------------------------- | --------- |
| `~/.claude/settings.json`     | All projects                  | No        |
| `.claude/settings.json`       | Single project                | Yes       |
| `.claude/settings.local.json` | Single project (gitignored)   | No        |
| Plugin `hooks/hooks.json`     | When plugin is enabled        | Yes       |
| Skill/agent YAML frontmatter  | While the component is active | Yes       |
| Managed policy settings       | Organization-wide             | Yes       |

Hooks are snapshotted at session startup. Mid-session edits to config files
don't take effect until the next session or until reviewed in the `/hooks` menu.

### Matchers

The `matcher` field is a regex that filters when a hook fires. Omit it, or use
`"*"` or `""`, to match everything. What the matcher filters depends on the event:

| Event                                                                  | Matches on        | Example values                                  |
| ---------------------------------------------------------------------- | ----------------- | ----------------------------------------------- |
| `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `PermissionRequest` | tool name         | `Bash`, `Edit\|Write`, `mcp__.*`                |
| `SessionStart`                                                         | session source    | `startup`, `resume`, `clear`, `compact`         |
| `SessionEnd`                                                           | exit reason       | `clear`, `logout`, `prompt_input_exit`, `other` |
| `Notification`                                                         | notification type | `permission_prompt`, `idle_prompt`              |
| `SubagentStart`, `SubagentStop`                                        | agent type        | `Bash`, `Explore`, `Plan`, custom names         |
| `PreCompact`                                                           | trigger           | `manual`, `auto`                                |
| `UserPromptSubmit`, `Stop`, `TeammateIdle`, `TaskCompleted`            | (none)            | Always fires; matcher is ignored                |

Matchers are regex: `Edit|Write` matches either tool, `mcp__github__.*` matches
all tools from the GitHub MCP server. MCP tools follow the naming pattern
`mcp__<server>__<tool>`.

### Hook handler types

Each handler in the `hooks` array is one of three types:

| Type      | What it does                                                               | Default timeout |
| --------- | -------------------------------------------------------------------------- | --------------- |
| `command` | Runs a shell command. Receives JSON on stdin.                              | 600s            |
| `prompt`  | Sends a prompt to a Claude model for single-turn yes/no evaluation         | 30s             |
| `agent`   | Spawns a subagent with tool access (Read, Grep, Glob) to verify conditions | 60s             |

Common fields for all types:

| Field           | Required | Description                             |
| --------------- | -------- | --------------------------------------- |
| `type`          | Yes      | `"command"`, `"prompt"`, or `"agent"`   |
| `timeout`       | No       | Seconds before canceling                |
| `statusMessage` | No       | Custom spinner text while the hook runs |

Command-specific: `command` (the shell command), `async` (run in background).
Prompt/agent-specific: `prompt` (text to send; use `$ARGUMENTS` for hook input),
`model` (defaults to a fast model).

## Input and Output

### Input (stdin)

All hooks receive JSON on stdin with these common fields:

| Field             | Description                     |
| ----------------- | ------------------------------- |
| `session_id`      | Current session identifier      |
| `transcript_path` | Path to conversation transcript |
| `cwd`             | Current working directory       |
| `permission_mode` | Current permission mode         |
| `hook_event_name` | Name of the event that fired    |

Tool-related events add `tool_name` and `tool_input`. `PostToolUse` adds
`tool_response`. `Stop` adds `stop_hook_active`. Each event's specific fields
are documented below in [Event Reference](#event-reference).

### Output (exit codes)

| Exit code | Effect                                                          |
| --------- | --------------------------------------------------------------- |
| 0         | Success. Stdout is parsed for JSON.                             |
| 2         | Blocking error. Stderr is fed to the agent as an error message. |
| Other     | Non-blocking error. Stderr visible in verbose mode only.        |

Exit code 2 is the primary mechanism for blocking. The agent receives the stderr
text and treats it as an instruction to follow.

### Output (JSON on stdout)

On exit 0, a hook can print a JSON object to stdout for finer-grained control.
**Do not mix approaches** — either use exit code 2 for blocking, or exit 0 with
JSON. Claude Code ignores JSON when exit code is 2.

Universal fields (work on all events):

| Field            | Description                                      |
| ---------------- | ------------------------------------------------ |
| `continue`       | `false` stops the agent entirely                 |
| `stopReason`     | Message shown to user when `continue` is `false` |
| `suppressOutput` | `true` hides stdout from verbose mode            |
| `systemMessage`  | Warning message shown to the user                |

Event-specific decision patterns vary. See [Decision Control](#decision-control).

### Decision control

Different events use different JSON shapes for decisions:

| Events                                                                          | Pattern              | Key fields                                                                                             |
| ------------------------------------------------------------------------------- | -------------------- | ------------------------------------------------------------------------------------------------------ |
| `UserPromptSubmit`, `PostToolUse`, `PostToolUseFailure`, `Stop`, `SubagentStop` | Top-level `decision` | `decision: "block"`, `reason`                                                                          |
| `PreToolUse`                                                                    | `hookSpecificOutput` | `permissionDecision` (allow/deny/ask), `permissionDecisionReason`, `updatedInput`, `additionalContext` |
| `PermissionRequest`                                                             | `hookSpecificOutput` | `decision.behavior` (allow/deny), `updatedInput`, `updatedPermissions`                                 |
| `TeammateIdle`, `TaskCompleted`                                                 | Exit code only       | Exit 2 + stderr                                                                                        |

## Event Reference

### SessionStart

Fires when a session begins or resumes. Useful for loading dynamic context or
setting environment variables.

Matcher values: `startup`, `resume`, `clear`, `compact`.

**Context injection:** Anything written to stdout (plain text or JSON
`additionalContext`) is added to the agent's context. This is the primary way to
feed the agent information at session start.

**Environment variables:** `SessionStart` hooks have access to `CLAUDE_ENV_FILE`.
Write `export` statements to this file to persist environment variables for all
subsequent Bash commands in the session:

```bash
#!/bin/bash
if [ -n "$CLAUDE_ENV_FILE" ]; then
  echo 'export NODE_ENV=production' >> "$CLAUDE_ENV_FILE"
fi
exit 0
```

**Re-injecting context after compaction:** Use `"matcher": "compact"` to fire
only after context compaction, which summarizes the conversation to free space.
This lets you re-inject critical context (project conventions, current goals)
that might be lost during summarization.

### UserPromptSubmit

Fires when a prompt is submitted, before the agent processes it. Can validate,
block, or augment prompts.

Input includes `prompt` (the submitted text). No matcher support.

To block: `exit 2` with reason on stderr, or exit 0 with
`{ "decision": "block", "reason": "..." }`.

To add context: print text to stdout (plain or JSON `additionalContext`).

### PreToolUse

Fires before a tool call executes. The primary mechanism for controlling what
the agent is allowed to do.

Matches on tool name. Input includes `tool_name`, `tool_input`, `tool_use_id`.

Built-in tool names: `Bash`, `Edit`, `Write`, `Read`, `Glob`, `Grep`, `Task`,
`WebFetch`, `WebSearch`. MCP tools: `mcp__<server>__<tool>`.

Decision output (inside `hookSpecificOutput`):

| Field                      | Description                                            |
| -------------------------- | ------------------------------------------------------ |
| `permissionDecision`       | `"allow"`, `"deny"`, or `"ask"`                        |
| `permissionDecisionReason` | For allow/ask: shown to user. For deny: shown to agent |
| `updatedInput`             | Modify tool input before execution                     |
| `additionalContext`        | Inject context before the tool runs                    |

Example — block destructive commands:

```bash
#!/bin/bash
COMMAND=$(jq -r '.tool_input.command')
if echo "$COMMAND" | grep -q 'rm -rf'; then
  jq -n '{
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: "deny",
      permissionDecisionReason: "Destructive command blocked"
    }
  }'
else
  exit 0
fi
```

Example — rewrite tool input:

```json
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "allow",
    "updatedInput": { "command": "npm run lint -- --fix" }
  }
}
```

### PermissionRequest

Fires when a permission dialog is about to be shown. Allows automated approval
or denial of permissions.

**Does NOT fire in non-interactive/headless mode (`-p`).** Use `PreToolUse`
instead for automated permission decisions in headless environments.

Input includes `tool_name`, `tool_input`, and `permission_suggestions` (the
"always allow" options the user would normally see).

Decision output (inside `hookSpecificOutput`):

| Field                         | Description                         |
| ----------------------------- | ----------------------------------- |
| `decision.behavior`           | `"allow"` or `"deny"`               |
| `decision.updatedInput`       | Modify tool input (allow only)      |
| `decision.updatedPermissions` | Apply permission rules (allow only) |
| `decision.message`            | Tell agent why denied (deny only)   |

### PostToolUse

Fires after a tool call succeeds. Cannot undo the action, but can feed the
agent context or flag issues for it to address.

Matches on tool name. Input includes `tool_input` and `tool_response`.

Output: `decision: "block"` with `reason` feeds feedback to the agent.
`additionalContext` injects extra context. For MCP tools, `updatedMCPToolOutput`
can replace the tool's output.

### PostToolUseFailure

Fires after a tool call fails. Input includes `tool_input`, `error` (string),
and `is_interrupt` (boolean). Output: `additionalContext` to add context
alongside the error.

### Stop

Fires when the agent finishes responding (not on user interrupts). The main
mechanism for enforcing completion criteria.

Input includes `stop_hook_active` — `true` when the agent is already continuing
due to a previous stop hook. **Check this flag to prevent infinite loops.**

To block stopping: `exit 2` with reason on stderr, or exit 0 with
`{ "decision": "block", "reason": "..." }`. The reason is fed to the agent as
its next instruction.

Example — require status update before stopping:

```bash
#!/bin/bash
INPUT=$(cat)
ACTIVE=$(echo "$INPUT" | jq -r '.stop_hook_active // false')
[ "$ACTIVE" = "true" ] && exit 0  # prevent infinite loop

if [ ! -f "$STATUS_FILE" ]; then
  echo "Write a status update before finishing." >&2
  exit 2
fi
exit 0
```

### SubagentStart / SubagentStop

`SubagentStart` fires when a subagent is spawned via the Task tool. Cannot block
creation, but can inject context via `additionalContext`.

`SubagentStop` fires when a subagent finishes. Uses the same decision format as
`Stop`. Input includes `agent_id`, `agent_type`, and `agent_transcript_path`.

Both match on agent type: `Bash`, `Explore`, `Plan`, or custom agent names.

### TeammateIdle / TaskCompleted

Both use exit codes only (no JSON decision control).

`TeammateIdle` fires when an agent team teammate is about to go idle. Exit 2
with stderr feedback to keep the teammate working.

`TaskCompleted` fires when a task is being marked completed. Exit 2 with stderr
to prevent completion (e.g., "tests must pass first"). Input includes `task_id`,
`task_subject`, and optionally `task_description`.

### Notification

Fires when Claude Code sends a notification. Matches on notification type:
`permission_prompt`, `idle_prompt`, `auth_success`, `elicitation_dialog`.

Cannot block notifications. Useful for routing alerts (desktop notifications,
Slack messages, etc.).

### PreCompact

Fires before context compaction. Matches on trigger: `manual` or `auto`.
Input includes `custom_instructions` (what the user passed to `/compact`).
Cannot block compaction.

### SessionEnd

Fires when the session terminates. Matches on reason: `clear`, `logout`,
`prompt_input_exit`, `bypass_permissions_disabled`, `other`. Cannot block
termination. Useful for cleanup tasks and logging.

## Prompt and Agent Hooks

For decisions requiring judgment rather than deterministic rules, use `prompt`
or `agent` type hooks instead of shell commands.

**Prompt hooks** send the hook input and your prompt to a Claude model (Haiku by
default). The model returns `{ "ok": true }` to allow or
`{ "ok": false, "reason": "..." }` to block. Use `$ARGUMENTS` in the prompt
to interpolate hook input.

**Agent hooks** are like prompt hooks but spawn a subagent that can use tools
(Read, Grep, Glob) to inspect the codebase before deciding. Up to 50 turns.
Longer default timeout (60s vs 30s).

Use prompt hooks when the hook input alone is enough to decide. Use agent hooks
when verification requires inspecting files or running commands.

Example — prompt-based Stop hook:

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "prompt",
            "prompt": "Check if all tasks are complete: $ARGUMENTS. Return {\"ok\": false, \"reason\": \"what remains\"} if not."
          }
        ]
      }
    ]
  }
}
```

## Async Hooks

Set `"async": true` on a command hook to run it in the background. The agent
continues working immediately. When the hook finishes, its output is delivered
on the next conversation turn via `systemMessage` or `additionalContext`.

Async hooks **cannot block or return decisions** — the triggering action has
already proceeded. Only `type: "command"` supports async.

Example — run tests in background after file edits:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "command",
            "command": ".claude/hooks/run-tests-async.sh",
            "async": true,
            "timeout": 300
          }
        ]
      }
    ]
  }
}
```

## Common Patterns

### Auto-format after edits

Run a formatter on every file the agent writes or edits:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          {
            "type": "command",
            "command": "jq -r '.tool_input.file_path' | xargs npx prettier --write"
          }
        ]
      }
    ]
  }
}
```

### Block edits to protected files

Prevent modification of `.env`, lock files, `.git/`:

```bash
#!/bin/bash
INPUT=$(cat)
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty')
PROTECTED=(".env" "package-lock.json" ".git/")
for pattern in "${PROTECTED[@]}"; do
  if [[ "$FILE_PATH" == *"$pattern"* ]]; then
    echo "Blocked: $FILE_PATH matches protected pattern '$pattern'" >&2
    exit 2
  fi
done
exit 0
```

### Gate stopping on a condition

Require the agent to do something before it can finish (e.g., update a status
file, pass tests, write a summary). Use `stop_hook_active` to avoid loops.

### Desktop notifications

Route `Notification` events to OS-native alerts:

```json
{
  "hooks": {
    "Notification": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "osascript -e 'display notification \"Claude Code needs attention\" with title \"Claude Code\"'"
          }
        ]
      }
    ]
  }
}
```

### Re-inject context after compaction

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "compact",
        "hooks": [
          {
            "type": "command",
            "command": "echo 'Reminder: use Bun, not npm. Run tests before committing.'"
          }
        ]
      }
    ]
  }
}
```

## Script Paths

Use `$CLAUDE_PROJECT_DIR` to reference scripts relative to the project root.
Use `${CLAUDE_PLUGIN_ROOT}` for scripts bundled with a plugin. Always quote
paths to handle spaces:

```json
{
  "type": "command",
  "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/my-hook.sh"
}
```

## Debugging

- Run `claude --debug` for full hook execution details
- Toggle verbose mode with `Ctrl+O` to see hook output in the transcript
- Test hooks manually: `echo '{"tool_name":"Bash","tool_input":{"command":"ls"}}' | ./my-hook.sh; echo $?`
- If JSON parsing fails, check that your shell profile (`~/.zshrc`, `~/.bashrc`)
  doesn't print text unconditionally — wrap echo statements in
  `if [[ $- == *i* ]]; then ... fi`

## Gotchas

- **Hooks are snapshotted at startup.** Editing config mid-session has no effect
  until restart or `/hooks` review.
- **All matching hooks run in parallel.** Identical commands are deduplicated.
- **Hooks run with full user permissions.** They can access anything the user can.
- **`PermissionRequest` does not fire in headless mode** (`-p`). Use `PreToolUse`.
- **Stop hooks fire on every response completion,** not just at task boundaries.
  Guard with `stop_hook_active` to avoid infinite loops.
- **Stdout must be clean JSON** (or empty). Shell profile noise breaks parsing.
- **`PostToolUse` cannot undo.** The tool already ran.
- **Hook timeout defaults to 10 minutes** for commands, configurable per hook.
