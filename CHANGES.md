# Changelog

This file tracks high-level changes between releases of schmux.

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
