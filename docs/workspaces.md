# Workspaces

**Problem:** Running multiple agents in parallel means managing multiple copies of your codebase. Creating git clones is tedious, keeping them organized is error-prone, and it's easy to lose track of uncommitted work or forget which workspace has what changes.

---

## Git as the Primary Organizing Format

Workspaces are git working directories on your filesystem, not containers or virtualized environments.

- Each repository gets sequential workspace directories: `myproject-001`, `myproject-002`, etc.
- Multiple agents can work in the same workspace simultaneously
- Workspaces are created on-demand when you spawn sessions
- Uses git worktrees for efficiency (shared object store, instant creation)

---

## Filesystem-Based, Not Containerized

schmux uses your actual filesystem rather than Docker or other abstracted isolation mechanisms.

- Workspace directories live in `~/.schmux-workspaces/` by default
- Full access to your real files, tools, and environment
- No container startup overhead or complexity

---

## Workspace Overlays

Local-only files (`.env`, configs, secrets) that shouldn't be in git can be automatically copied into each workspace via the overlay system.

### Storage

Overlay files are stored in `~/.schmux/overlays/<repo-name>/` where `<repo-name>` matches the name from your repos config.

Example structure:

```
~/.schmux/overlays/
├── myproject/
│   ├── .env                 # Copied to workspace root
│   └── config/
│       └── local.json      # Copied to workspace/config/local.json
```

### Behavior

- Files are copied after workspace creation, preserving directory structure
- Each file must be covered by `.gitignore` (enforced for safety)
- Use `schmux refresh-overlay <workspace-id>` to reapply overlay files to existing workspaces
- Overlay files overwrite existing workspace files

### Safety Check

The overlay system enforces that files are truly local-only by checking `.gitignore` coverage:

```bash
git check-ignore -q <path>
```

If a file is NOT matched by `.gitignore`, the copy is skipped with a warning. This prevents accidentally shadowing tracked repository files.

---

## Git Status Visualization

The dashboard shows workspace git status at a glance:

- **Dirty indicator**: Uncommitted changes present
- **Branch name**: Current branch (e.g., `main`, `feature/x`) — clickable link to remote when available
- **Ahead/Behind**: Commits ahead or behind origin
- **Line changes**: Color-coded indicators showing uncommitted line additions (+N in green) and deletions (-M in red)

### Clickable Branch Links

When a branch has a remote tracking branch, the branch name in the workspace table appears as a clickable link that opens the branch in the web UI (GitHub, GitLab, Bitbucket, or generic git hosts). Supports both SSH (`git@host:user/repo`) and HTTPS URL formats, with proper URL encoding for special characters.

### Line Change Tracking

The workspace table displays uncommitted line additions and deletions calculated via `git diff --numstat HEAD`, covering both staged and unstaged modifications:

- **+N** (green): Lines added
- **-M** (red): Lines removed

---

## Diff Viewer

### Built-in Diff Viewer

View what changed in a workspace with the built-in diff viewer:

- Side-by-side git diffs
- See what agents changed across multiple workspaces
- Access via dashboard or `schmux diff` commands

### External Diff Tool Integration

Launch workspace changes in your preferred diff tool (VS Code, Kaleidoscope, or any custom tool) directly from the web dashboard.

Configure named commands in `~/.schmux/config.json` under `external_diff_commands` using template placeholders:

```json
{
  "external_diff_commands": {
    "VS Code": "code --diff {old_file} {new_file}",
    "Kaleidoscope": "ksdiff {old_file} {new_file}"
  }
}
```

Available placeholders:

- `{old_file}`: Original file version
- `{new_file}`: Modified file version
- `{file}`: Current file (for single-file tools)

The dashboard displays a DiffDropdown UI component on workspace rows with your configured commands. Temp files are automatically cleaned up via scheduled sweeping.

---

## VS Code Integration

Launch a VS Code window directly in any workspace:

- Dashboard: "Open in VS Code" button on workspace
- CLI: `schmux code <workspace-id>`

---

## Safety Checks

schmux prevents accidental data loss:

- Cannot dispose workspaces with uncommitted changes
- Cannot dispose workspaces with unpushed commits
- Explicit confirmation required for disposal

---

## Git Behavior

### Branch Names

schmux supports standard git branch naming conventions:

**Valid characters:**

- Alphanumeric characters (a-z, A-Z, 0-9)
- Hyphens (-), underscores (\_), periods (.), and forward slashes (/) for hierarchical names
- Examples: `feature-branch`, `feature/subfeature`, `bugfix_123`, `release.v1.0`

**Constraints:**

- Cannot begin or end with a separator (/ - . \_)
- Cannot contain consecutive separators (//, --, \_\_, .., etc.)
- Maximum length follows git conventions (typically 256 characters)

**Automatic handling:**

- If you request a branch name that's already in use, schmux appends a unique suffix (e.g., `feature-x7k`)
- Branch names with invalid characters are rejected with a helpful error message

### Source Code Management

schmux supports two modes for creating workspace directories, configurable in **Settings > Workspace > Source Code Management**:

| Mode                       | Description                               | Branch Handling                               |
| -------------------------- | ----------------------------------------- | --------------------------------------------- |
| **Git Worktree** (default) | Efficient disk usage, shares repo history | Each branch can only be used by one workspace |
| **Git (Full Clone)**       | Independent clones                        | Multiple workspaces can use the same branch   |

### Git Worktree Mode

When using worktrees (the default):

1. **First workspace for a repo**: Creates a bare clone in `~/.schmux/repos/<repo>.git`
2. **Additional workspaces**: Uses `git worktree add` from the bare clone (instant, no network)

**Worktree constraint**: Git only allows one worktree per branch. If you request a branch that's already checked out by another worktree, schmux will automatically create a unique branch name by appending a 3-character suffix (e.g., `feature/foo-x7k`) and create it from the requested branch's tip.

```
Requested "feature/foo" is already in use by workspace "myrepo-001".
Using "feature/foo-x7k" for this workspace.
```

**Why Worktrees?**

- Disk efficient: git objects shared across all workspaces for a repo
- Fast creation: no network clone for additional workspaces
- Tool compatible: VS Code, git CLI, and agents work normally

### Git (Full Clone) Mode

When using full clones:

- Each workspace is a complete, independent git clone
- Multiple workspaces can work on the same branch
- No branch conflict restrictions
- Uses more disk space (no shared objects)

### Existing Workspaces

Regardless of mode, spawning into an existing workspace:

- Skips git operations (safe for concurrent agents)
- Reuses the directory for additional sessions

### Disposal

- Blocked if workspace has uncommitted or unpushed changes
- Uses `git worktree remove` for worktrees, `rm -rf` for full clones
- No automatic git reset — you're in control

---

## Git Workflow Sync

schmux provides bidirectional linear synchronization for clean git history without merge commits.

### Sync from Main

Brings commits from `origin/main` into your current branch via iterative cherry-pick:

- **Handles both behind and diverged states**: Works whether your branch is behind or has diverged from main
- **Conflict detection**: Aborts if conflicts are detected during cherry-pick
- **Preserves local changes**: Creates a temporary WIP commit before syncing, resets after success or abort
- **Access**: Dropdown menu on the git status indicator (behind | ahead) in workspace header
- **Disabled when**: Already caught up to main

This replaces the previous "rebase ff main" action.

### Sync to Main

Pushes your branch commits directly to main via fast-forward:

- **Validation**: Requires clean workspace state (no uncommitted changes, not behind main)
- **Fast-forward only**: No merge commits, maintains linear history
- **Two workflow styles**:
  - **On-main workflow**: Push directly when workspace is already on main branch
  - **Feature branch workflow**: Set upstream to main, sync locally after push
- **Access**: Dropdown menu on git status indicator in workspace header
- **Disabled when**: Workspace has uncommitted changes or is behind main

Both actions are available from the dashboard workspace header git status dropdown.

---

## Workspace Configuration

Workspaces can have their own configuration files that extend the global config with workspace-specific settings.

### Location

Place a `.schmux/config.json` file inside any workspace directory:

```
~/schmux-workspaces/myproject-001/
├── .schmux/
│   └── config.json    # Workspace-specific config
├── src/
└── ...
```

### Supported Settings

Currently, workspace configs support:

| Setting        | Description                                          |
| -------------- | ---------------------------------------------------- |
| `quick_launch` | Quick launch presets specific to this workspace/repo |

### Quick Launch

Define quick launch presets that only appear for this repository:

```json
{
  "quick_launch": [
    {
      "name": "Run Tests",
      "command": "npm test"
    },
    {
      "name": "Fix Tests",
      "target": "claude",
      "prompt": "Run the test suite and fix any failures"
    }
  ]
}
```

#### Schema

| Field     | Type   | Description                                        |
| --------- | ------ | -------------------------------------------------- |
| `name`    | string | Display name (required)                            |
| `command` | string | Shell command to run directly                      |
| `target`  | string | Run target (claude, codex, model, or user-defined) |
| `prompt`  | string | Prompt to send to the target                       |

#### Rules

- **Shell command**: Set `command` to run a shell command directly
- **AI agent**: Set `target` and `prompt` to spawn an agent with a prompt
- **Either/or**: Use `command` OR `target`+`prompt`, not both

#### Merge Behavior

Workspace quick launch items are merged with global quick launch items:

- Items with the same name: workspace version takes precedence
- Items with unique names: both appear in the spawn dropdown

### Config File Watching

The daemon monitors workspace config files and reloads them automatically:

- **On startup**: All workspace configs are loaded
- **On change**: Config is reloaded when the file's modification time changes
- **Parse errors**: Logged once per change (not spammed on every poll cycle)
- **Success**: Logged when config is successfully loaded after a change

Example log output:

```
[workspace] loaded config from /path/to/workspace/.schmux/config.json
[workspace] warning: failed to parse /path/to/workspace/.schmux/config.json: invalid character...
```

### Use Cases

- **Project-specific prompts**: "Run tests", "Build docs", "Deploy to staging"
- **Team presets**: Check workspace config into git so the whole team gets the same quick launch options
- **Repo-specific targets**: Different repos may use different agents or workflows
