# Environment Init Hooks for Workspace Sessions

**Status**: Speculative / Stubbed
**Author**: Aaron (farra)
**Context**: Project-specific toolchain initialization before agent sessions

## Problem

When schmux spawns an agent session, it opens a tmux window in the workspace directory and runs the agent command directly. The shell environment is whatever the host (or container) provides. There is no mechanism to initialize a project-specific environment — activate a Nix devShell, enter a devcontainer, source a `.envrc`, or run any other setup — before the agent starts.

This matters because real projects have complex toolchains. For example:

- **Godot game** (Jam & Tea's "scone"): Needs Godot 4.x with .NET, dotnet SDK, Python 3.12, uv, pulumi, awscli2, and a proprietary CLI installed via pip. Currently managed by `nix develop` via a cautomaton-develops `deps.toml` + `flake.nix`.
- **Rust service**: Needs a specific Rust toolchain version, cargo plugins, and system libraries.
- **Node.js app**: Needs a specific Node version, pnpm, and env vars from `.env`.

Without environment init, agents running on remote hosts or shared VMs lack the tools they need. The current workarounds are:

1. **Bake everything into the host/container image** — works but inflexible (one image per project, huge images for multi-project VMs)
2. **Wrap the agent command in the run target** — e.g., `"command": "nix develop --command claude"` — works but couples environment setup to the agent target definition, can't be per-workspace, and is fragile
3. **Pre-provision manually** — SSH in, set things up, hope it persists

### What Exists Today

- **Run Targets** (`~/.schmux/config.json`): Define what command to run (`claude`, `codex`, custom). The `command` field is the full command string. No pre/post hooks.
- **`provision_command`** (remote flavors only): Runs once per remote host connection. Intended for `git clone`, not per-session environment setup. Template vars: `{{.Repo}}`, `{{.Branch}}`, `{{.WorkspacePath}}`.
- **Overlays** (`~/.schmux/overlays/<repo>/`): Copy files into workspace before agent runs. Could drop a `.envrc` but doesn't execute anything.
- **Schmux provision hooks** (`internal/provision/`): Write signal/hooks files (`.claude/settings.local.json`, `.schmux/signaling.md`) into workspace before agent launch. Agent-specific, not user-extensible.
- **`buildCommand()` in session manager**: Assembles the final tmux command string. Prepends env vars, appends prompt. No hook point for arbitrary init.

### Spawn Flow (Current)

```
ResolveTarget("claude") → command = "claude"
workspace.GetOrCreate() → git worktree at ~/workspace/scone-feature-x
provision.EnsureClaudeHooks(workspace.Path)  ← writes .claude/settings.local.json
inject env vars (SCHMUX_ENABLED, SCHMUX_SESSION_ID, ...)
buildCommand() → "SCHMUX_ENABLED='1' ... claude 'review the auth module'"
tmux new-session -d -s <name> -c <workspace-path> <command>
```

There is no step between "workspace is ready" and "agent command runs" where user-defined initialization can happen.

## Design Principles

1. **Schmux is environment-agnostic** — Schmux should not know about Nix, devcontainers, direnv, or any specific tool. It provides a hook; the user fills it in.
2. **Generic shell command** — The init mechanism is just a shell command (or script) that runs before the agent. If it succeeds (exit 0), the agent starts. If it fails, the session fails with a clear error.
3. **Multiple granularities** — Init can be configured globally, per-workspace, or per-repo. More specific wins.
4. **Works locally and remotely** — The same init mechanism applies to local tmux sessions and remote control-mode sessions.
5. **Observable** — Init output should be visible in the terminal (it runs in the same tmux pane), so the user can debug failures.

## Proposed Design

### Configuration: `shell_init`

A new optional field at three levels of specificity:

**Per run target** (in `~/.schmux/config.json`):

```json
{
  "run_targets": [
    {
      "name": "scone-claude",
      "type": "promptable",
      "command": "claude",
      "shell_init": "nix develop ~/scone --command bash"
    }
  ]
}
```

**Per workspace** (in `state.json`, set via API/CLI at workspace creation or later):

```json
{
  "workspaces": [
    {
      "id": "scone-feature-x",
      "repo": "git@github.com:jamandtea/scone_game.git",
      "branch": "feature-x",
      "shell_init": "nix develop --command bash"
    }
  ]
}
```

**Global default** (in `~/.schmux/config.json`):

```json
{
  "shell_init": "eval \"$(direnv export bash 2>/dev/null)\"",
  "run_targets": [...]
}
```

**Precedence**: workspace `shell_init` > run target `shell_init` > global `shell_init`. If none is set, behavior is unchanged from today.

### Auto-Detection (Optional, Future)

Schmux could optionally detect environment files in the workspace and suggest or auto-apply init:

| File | Suggested `shell_init` |
|------|----------------------|
| `.envrc` | `eval "$(direnv export bash 2>/dev/null)"` |
| `flake.nix` | `nix develop --command bash` |
| `deps.toml` + `flake.nix` | `nix develop --command bash` |
| `.devcontainer/devcontainer.json` | (more complex, see below) |
| `shell.nix` | `nix-shell --run bash` |
| `.tool-versions` | `mise install && eval "$(mise activate bash)"` |

This is a convenience layer, not a requirement. Users can always set `shell_init` explicitly. Auto-detection should be opt-in via a config flag like `"auto_detect_shell_init": true`.

### Command Construction

The init command wraps the agent command. Instead of:

```bash
tmux new-session -d -s <name> -c <workspace-path> "SCHMUX_ENABLED='1' ... claude 'prompt'"
```

With `shell_init = "nix develop --command bash"`, the command becomes:

```bash
tmux new-session -d -s <name> -c <workspace-path> "nix develop --command bash -c 'SCHMUX_ENABLED=1 ... claude \"prompt\"'"
```

Or, for cleaner implementation, use a wrapper approach:

```bash
# Write a temporary init script
cat > /tmp/schmux-init-<session-id>.sh << 'SCRIPT'
#!/usr/bin/env bash
set -e

# User's shell_init (may change the shell environment, enter a container, etc.)
eval "nix develop --command bash"

# If shell_init replaces the process (exec/enter), the agent command
# must be passed through. See "Shell Init Modes" below.
SCRIPT
```

### Shell Init Modes

There's a fundamental distinction in how init commands work:

**Mode 1: Environment modification** — The init command sets up the environment and returns. The original shell continues with the modified environment.
```
shell_init: "source .envrc"
shell_init: "eval \"$(direnv export bash)\""
shell_init: "export PATH=\"$HOME/.local/bin:$PATH\""
```

**Mode 2: Shell replacement** — The init command replaces the shell with a new one that has the right environment. The agent command must be passed as an argument.
```
shell_init: "nix develop --command bash"
shell_init: "distrobox enter gamedev --"
shell_init: "devcontainer exec --workspace-folder . bash"
```

For Mode 2, `--command bash` (or equivalent) is critical — the init command must accept a subcommand. Schmux passes the agent command as that subcommand.

**Implementation approach**: Use Mode 2 semantics universally. The `shell_init` command receives the agent command as `$@` or via `--command`:

```go
// In buildCommand(), when shell_init is set:
func buildCommandWithInit(shellInit string, agentCommand string) string {
    // The shell_init command wraps the agent command
    return fmt.Sprintf("%s -c %s", shellInit, shellQuote(agentCommand))
}
```

For Mode 1 init commands, users wrap them appropriately:
```json
"shell_init": "bash -c 'source .envrc && exec \"$@\"' --"
```

Or schmux provides a simpler convention: if `shell_init` starts with `source` or `export` or `eval`, wrap it automatically in `bash -c '... && exec "$@"' --`. This is a convenience, not a requirement.

### Remote Sessions

For remote sessions (via `tmux -CC` control mode), the same mechanism applies. The `CreateWindow` call in `controlmode/client.go` already accepts a command string:

```go
// Current:
CreateWindow(ctx, name, workdir, agentCommand)
// → new-window -n name -c workdir -P -F '...' agentCommand

// With init:
CreateWindow(ctx, name, workdir, buildCommandWithInit(shellInit, agentCommand))
```

For remote `provision_command` (one-time host setup), the semantics are different and unchanged. `shell_init` runs per-session; `provision_command` runs per-host-connection.

### CLI

```bash
# Set shell_init on a workspace
schmux workspace set-init <workspace-id> "nix develop --command bash"

# Clear shell_init on a workspace
schmux workspace set-init <workspace-id> ""

# Show current init for a workspace
schmux workspace info <workspace-id>
```

### Dashboard UI

**Workspace settings panel** (or modal):
- Text field for `shell_init` command
- Helper text: "Shell command that runs before the agent. Must accept the agent command via -c flag."
- Show detected environment files as suggestions (if auto-detection is enabled)

**Spawn wizard**:
- If workspace has `shell_init`, show it as a read-only info line: "Environment: `nix develop --command bash`"
- No override at spawn time (use workspace settings to change)

**Session terminal**:
- Init output is visible in the terminal since it runs in the same tmux pane
- If init fails (non-zero exit), the pane shows the error output and schmux marks the session as `failed`

### API Changes

**POST /api/spawn** — no changes needed (init is resolved from workspace/target/global config)

**GET /api/sessions** — extend workspace response:

```json
{
  "workspaces": [
    {
      "id": "scone-feature-x",
      "shell_init": "nix develop --command bash",
      "shell_init_source": "workspace"
    }
  ]
}
```

`shell_init_source` indicates where the active init came from: `"workspace"`, `"run_target"`, `"global"`, or `null`.

**PUT /api/workspaces/{id}** — allow setting `shell_init`:

```json
{
  "shell_init": "nix develop --command bash"
}
```

## Examples

### Godot Game (scone) on Shared VM

The VM has Nix installed but not Godot/dotNet. The scone repo has a `flake.nix` that provides everything.

```json
{
  "workspaces": [
    {
      "id": "scone-refactor",
      "repo": "git@github.com:jamandtea/scone_game.git",
      "branch": "sk-refactor",
      "shell_init": "nix develop --command bash"
    }
  ]
}
```

Spawn flow: tmux window opens in `~/workspace/scone-refactor` → `nix develop --command bash -c 'SCHMUX_ENABLED=1 claude "review the combat system"'` → Nix builds/activates the devShell (Godot, dotNet, Python, etc.) → Claude starts with all tools available.

### Distrobox on Same VM

The VM has a `gamedev` distrobox with all tools pre-installed.

```json
{
  "shell_init": "distrobox enter gamedev --"
}
```

The `--` tells distrobox to pass the remaining args as a command inside the container.

### direnv for All Projects

```json
{
  "shell_init": "bash -c 'eval \"$(direnv export bash 2>/dev/null)\" && exec \"$@\"' --"
}
```

Set as global default. Every session gets direnv-activated if the workspace has a `.envrc`.

### No Init (Default)

If no `shell_init` is set anywhere, the agent runs directly in the workspace directory exactly as it does today. Zero behavior change for existing users.

## What This Does NOT Cover

- **Container lifecycle management** — Schmux doesn't start/stop containers. If `shell_init` is `distrobox enter gamedev --`, the distrobox must already exist. Provisioning containers is the user's responsibility (or the `provision_command`'s job for remote hosts).
- **Devcontainer spec parsing** — Schmux doesn't read `devcontainer.json`. If you want devcontainer support, your `shell_init` calls `devcontainer exec` or equivalent. A future spec could add first-class devcontainer support, but this hook enables it without schmux knowing about the spec.
- **Environment caching** — `nix develop` can take 30+ seconds on first run. Schmux doesn't cache or pre-warm environments. The user sees the init output in the terminal and waits. Tools like `direnv` + `nix-direnv` solve this at the Nix layer.
- **Init dependencies between sessions** — Each session runs its own init independently. No ordering guarantees.
- **Secrets injection** — Init commands can `source ~/.secrets` or similar, but schmux doesn't manage secrets. The `provision_command` or overlay system can place secret files; `shell_init` can source them.

## Implementation Notes

The spawn flow in `internal/session/manager.go` already builds agent commands via `buildCommand()` and passes them to `tmux.CreateSession()`. The wrapping insertion point is straightforward — between command construction and tmux session creation:

```go
// Existing: build the agent command
agentCmd := buildCommand(resolved, prompt, model, resume, remoteMode)

// New: wrap with shell_init if configured
shellInit := resolveShellInit(workspace, target, globalConfig)
if shellInit != "" {
    agentCmd = buildCommandWithInit(shellInit, agentCmd)
}

// Existing: create tmux session with the command
tmux.CreateSession(ctx, tmuxSession, w.Path, agentCmd)
```

This mirrors the existing `RemoteFlavor` template rendering pattern already used for `connect_command` and `provision_command`. For remote sessions, the same wrapping applies to the command passed to `controlmode.Client.CreateWindow()`.

**Design choice: explicit over auto-detection.** An earlier internal design explored auto-detecting `.envrc`/`flake.nix`/`Dockerfile` and choosing an environment tier automatically. This was rejected in favor of explicit config because: (1) auto-detection adds complexity for users who don't need it (local single-project workflows), (2) `direnv exec .` already handles the `.envrc` → nix → flake chain internally, and (3) explicit config is easier to debug. Auto-detection can layer on top later as a default when `shell_init` is empty.

## Implementation Scope

### Backend Changes

- Add `ShellInit string` field to `state.Workspace` struct
- Add `ShellInit string` field to `config.RunTarget` struct
- Add `ShellInit string` field to root config
- Resolution function: workspace > run target > global > empty
- Modify `buildCommand()` in `internal/session/manager.go` to wrap agent command with init
- For remote sessions: modify `CreateWindow` call to use wrapped command
- CLI: `workspace set-init` command
- API: extend workspace GET/PUT

### Frontend Changes

- Workspace settings: text field for `shell_init`
- Spawn wizard: show active init as info line
- TypeScript type update (regenerate from Go types)

### Estimated Complexity

- Backend: ~150-250 lines of Go across 3-4 files
- Frontend: ~50-100 lines of React
- The hard part is getting the shell quoting right for nested commands (init wrapping agent wrapping prompt). Good test coverage of `buildCommandWithInit()` is essential.

## Relationship to Other Specs

- **Multi-user sessions** (`multi-user-sessions-farra.md`): Complementary. Init hooks solve "what tools are available"; multi-user solves "who is using them". Together they enable the full Jam & Tea shared workflow.
- **Multi-worker architecture** (`multi-worker-architecture.md`): Workers would inherit the init hook mechanism. A worker on a different machine could have different `shell_init` defaults.
- **Workspace dependencies** (`workspace-dependencies.md`): Orthogonal. Dependencies are about ordering/blocking between workspaces; init hooks are about environment setup within a workspace.
- **Overlays** (`overlay-bootstrap.md`, `overlay-compounding.md`): Complementary. Overlays place files (`.envrc`, config files); init hooks execute commands (activate the environment those files define).

## Open Questions

1. **Should `shell_init` failure block the session?** Proposed: yes, mark session as `failed` with init output captured. Alternative: warn but start the agent anyway (risky — agent runs without tools).

2. **Should there be a timeout on init?** Proposed: configurable, default 120 seconds. `nix develop` on a cold cache can take minutes; but hanging forever is also bad.

3. **Per-repo vs per-workspace init?** The current design is per-workspace (each git worktree can have different init). A per-repo default (all workspaces for `scone_game.git` get the same init) could reduce repetition. Could be added later via a `repo_defaults` config section.

4. **Interactive init?** Some init commands prompt for input (e.g., `nix develop` asking to accept a flake). Since init runs in the tmux pane, the user can interact via the dashboard terminal. But this conflicts with the agent starting afterward. Proposed: require non-interactive init (`--accept-flake-config` for Nix, `--yes` flags, etc.).

## Future Directions: First-Class Environment Providers

The `shell_init` hook is intentionally generic — a raw shell command. But it opens the door to schmux understanding specific environment management tools at a higher level. Rather than requiring users to hand-write shell commands, schmux could offer direct (optional) support for common patterns:

- **direnv** — Auto-detect `.envrc` in workspace, run `direnv allow` + `direnv export` before agent launch. Minimal integration surface (direnv is already designed to be embedded). Could be as simple as a config toggle: `"direnv": true`.

- **Nix subshells** — Detect `flake.nix`, `shell.nix`, `devbox.json`, or `devenv.nix` and run the appropriate activation command (`nix develop`, `devbox shell`, `devenv shell`). These all follow the same pattern: enter a subshell with project-specific packages on PATH.

- **Distrobox** — Each workspace (or repo) could be associated with a named distrobox. Schmux would run the agent session inside that distrobox via `distrobox enter <name> --`. Could extend to managing distrobox lifecycle (create from image if not exists, using a configured image reference).

- **Containers** — More ambitious: schmux manages a container per workspace. This could mean devcontainer spec support (read `devcontainer.json`, build/pull image, `exec` into it) or a simpler schmux-native container config. The key challenge is container lifecycle — who starts/stops it, how does it persist across sessions, how do workspace files get mounted.

These would layer on top of `shell_init`, not replace it. Each provider would essentially be a structured way to generate the right `shell_init` command, with schmux handling the details (detection, lifecycle, caching, error messages). The raw `shell_init` escape hatch remains for anything schmux doesn't natively support.

This is not yet designed — noted here to capture the trajectory. A separate spec would be needed for each provider, addressing detection heuristics, configuration schema, lifecycle management, and interaction with remote sessions.
