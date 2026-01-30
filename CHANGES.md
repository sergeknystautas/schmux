# Changelog

This file tracks high-level changes between releases of schmux.

## Version 1.1.0 (2026-01-30)

**Major features:**
- Model selection overhaul with native Claude models (Opus, Sonnet, Haiku) and more third-party options (Kimi 2.5)
  **Note: Your config.json will be automatically migrated. No manual intervention required.**
- Home dashboard with recent branches from all repos and one-click resume work flow
- Filesystem-based git status watcher for faster dashboard updates
- Spawn form persistence across page refreshes and browser restarts
- Spawn gets easier model selection modes: single model, multiple models, or advanced (0-10 per model)

**Improvements:**
- Clickable URLs in terminal
- Auto-resolve worktree branch conflicts with unique suffix
- Linear sync captures untracked files
- Improved diff viewer readability
- Repo and branch promoted to primary visual position in workspace header

**Tech debt tackled:**
- Split workspace manager into focused modules (git, git_watcher, worktree, origin_queries, overlay, linear_sync)

**Bug fixes:**
- Fixed concurrent write safety in WebSocket connections
- Fixed multiline prompt handling with proper shell quoting
- Fixed tooltip positioning on scroll

## Version 1.0.1 (2026-01-25)

**Bug fixes:**
- Fixed tar extraction error when dashboard assets have "./" prefixed entries

## Version 1.0.0 (2026-01-25)

**Major features:**
- Git worktree support for efficient workspace management without full clones
- External diff tool integration to run diffs in your preferred editor
- Spawn wizard overhaul - faster, more prompt-centric UX
- Session details page redesign with new workspace header and tabbed interface
- Diff tab to view file diffs directly in the dashboard with resizable file list
- Collapsible sidebar navigation with tree view of workspaces and sessions
- Bidirectional git sync - push workspace changes to main, or fast-forward rebase from main
- Spawn form draft persistence to resume incomplete spawn sessions
- Config edit mode overhaul with sticky header, global save button, and edit modals

**Improvements:**
- Line change tracking for workspaces
- Structured logging with component prefixes
- Clickable branch links in workspace table
- Better workspace disposal reliability
- Fixed spawn workspace conflicts when multiple agents use same branch

## Version 0.9.5 (2026-01-22)

**Major features:**
- GitHub OAuth authentication to secure web dashboard access
- Workspace directories cleaned up when creation fails

**Improvements:**
- Support VSCode editor invocation when code is an alias
- In-browser self-update with version notifications
- Improved installer output formatting and user feedback
- Nudgenik now optional and disabled by default
- Fixed button padding in configuration wizard

**Tech debt tackled:**
- E2E tests enforced to run in Docker
- E2E tests added to pre-commit requirements
- Config versioning for forward compatibility
- Go upgraded to 1.24

**Documentation:**
- Added CHANGES.md
- Cleaned up conventions for inline shell command comments
- Cleaned up terminology: renamed "quick launch presets" to "cookbooks"

## Version 0.9.4

- Initial changelog entry
- See git history for detailed changes
