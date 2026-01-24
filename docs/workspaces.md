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

**New workspaces:**
- First workspace for a repo creates a bare clone in `~/.schmux/repos/`
- Additional workspaces use `git worktree add` (instant, no network)
- If same branch is already checked out elsewhere, falls back to full clone

**Existing workspaces:**
- Skips git operations (safe for concurrent agents)
- Reuse directory for additional sessions

**Disposal:**
- Blocked if workspace has uncommitted or unpushed changes
- Uses `git worktree remove` for worktrees, `rm -rf` for legacy clones
- No automatic git reset — you're in control

**Why worktrees?**
- Disk efficient: git objects shared across all workspaces for a repo
- Fast creation: no network clone for additional workspaces
- Tool compatible: VS Code, git CLI, and agents work normally
