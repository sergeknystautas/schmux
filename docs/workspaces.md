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
- **Branch name**: Current branch (e.g., `main`, `feature/x`)
- **Ahead/Behind**: Commits ahead or behind origin

---

## Diff Viewer

View what changed in a workspace with the built-in diff viewer:

- Side-by-side git diffs
- See what agents changed across multiple workspaces
- Access via dashboard or `schmux diff` commands

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

### Worktree Strategy

schmux uses git worktrees for efficient workspace management:

1. **First workspace for a repo**: Creates a bare clone in `~/.schmux/repos/<repo>.git`
2. **Additional workspaces**: Uses `git worktree add` from the bare clone (instant, no network)

**Worktree constraint**: Git only allows one worktree per branch. You can't have two worktrees both checked out to `main`.

### Full Clone Fallback

When you spawn a workspace on a branch that's already checked out in another worktree, schmux automatically falls back to a full clone:

```
Spawn to "main" → worktree for main already exists at schmux-001
                → create schmux-002 as full clone instead
```

This means you can have multiple workspaces on the same branch:
- `schmux-001`: worktree on `main`
- `schmux-002`: full clone on `main`
- `schmux-003`: full clone on `main`

The fallback is transparent — all workspaces work identically regardless of whether they're worktrees or full clones.

### New Workspaces

- First workspace on a branch: worktree (fast, shared objects)
- Additional workspaces on same branch: full clone (independent)
- Workspaces on different branches: worktrees (fast, shared objects)

### Existing Workspaces

- Skips git operations (safe for concurrent agents)
- Reuse directory for additional sessions

### Disposal

- Blocked if workspace has uncommitted or unpushed changes
- Uses `git worktree remove` for worktrees, `rm -rf` for full clones
- No automatic git reset — you're in control

### Why Worktrees?

- Disk efficient: git objects shared across all workspaces for a repo
- Fast creation: no network clone for additional workspaces
- Tool compatible: VS Code, git CLI, and agents work normally
