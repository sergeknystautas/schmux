# Changelog

This file tracks high-level changes between releases of schmux.

## Version 1.2.0 (2026-03-31)

### Multiplexed agentic coding

**Agent Signaling** — File-based signal protocol (replacing terminal parsing) with audio notifications, attention states, working spinner, and support for remote sessions and Codex agents. Claude Code hooks integration for signaling instead of system prompt injection.

**Terminal Streaming Pipeline** — Replaced log polling with tmux control mode + binary WebSocket frames for real-time terminal output. Added escape sequence buffer to prevent frame-boundary corruption, periodic sync to auto-correct xterm.js desync, WebGL renderer, and comprehensive diagnostic capture system.

**Web Preview** — See your app's output alongside your agentic coding sessions. Auto-detects dev servers via PID file matching, stable port allocation persists across daemon restarts, reverse proxy with WebSocket upgrade support, browser navigation bar, and auto-navigate to preview page when a server is detected.

**Personas** — Full CRUD management with dedicated routes, session color accents, and 10 built-in personas. Spawn page persona dropdown.

**Model System** — Dynamic model discovery via models.dev registry, multi-runner architecture, tool adapter system (OpenCode support), capability-based filtering in config UI, hook/persona injection for adapters. New models: GPT-5.4, GPT-5.2, GLM-5, MiniMax m2.5, Qwen3 Coder Plus.

**VCS Abstraction** — Sapling support alongside Git. VCS-agnostic diff, stage, discard, amend, and uncommit operations with backend detection guards.

- Subreddit digest with AI-generated cross-repo activity summaries on home page
- Repofeed for cross-developer intent federation via git orphan branch
- Conflict resolution with embedded terminal panel and live LLM streaming
- xterm.js upgraded to v6.0.0

### Learning systems

**Lore System** — Automated learning from agent friction. Hook-based capture of failures and reflections, LLM curator for deduplication and proposal generation, apply-via-PR workflow, and full dashboard UI with proposal cards and inline diffs.

**Floor Manager** — Singleton orchestration agent with tell, events, capture, inspect, and branches CLI commands. Dashboard integration with operator-driven control model.

**Overlay Compounding** — File watcher with debouncing and anti-echo suppression, three-strategy merge engine (skip, fast path, LLM merge), propagation across worktrees, management page with scan/add flow, and CONVENTIONS.md in defaults.

**Emerging Actions** — Automatic action discovery from agent patterns with registry, curation, autocomplete, and consolidated quick launch dropdown.

### Ergonomics

**Keyboard Controls** — Cmd+/ toggles keyboard mode for rapid navigation without a mouse. Cmd+Arrow cycles through workspaces, extensible tab system with unified close and pending navigation.

**Image Attachments** — Paste screenshots and diagrams directly into the spawn prompt. Images are written to the workspace and referenced in the agent's prompt for visual context.

**Pastebin Clips** — Quick-paste snippets into terminal sessions without navigating away from the dashboard.

**Environment Comparison** — Side-by-side view of your shell environment vs the tmux server environment, with one-click sync to fix missing variables that cause agent tool failures.

- Drag-to-reorder session tabs, xterm title reflection in tabs
- Workspace sort toggle (alpha/time)
- Safe force-push with confirmation for post-rebase workflows
- Remote branch tracking with visual ahead/behind indicators
- Bulk pull button to sync all behind workspaces
- Git divergence stats on workspace list
- Commit detail view with keyboard navigation
- Markdown preview in diff viewer

### Security

**Dashboard.sx** — ACME certificate provisioning via Let's Encrypt, HTTPS server, and CLI commands. TLS config UI with certificate validation.

**Remote Access** — Cloudflared tunnels for phone/tablet dashboard access with PIN auth, CSRF protection, rate limiting, mobile-responsive layout, ntfy push notifications, QR code subscription, and extensive security hardening.

- Constant-time OAuth state comparison
- CORS restricted to same-port origins with tunnel active
- HTML-escape to prevent XSS on PIN page
- Input validation hardening, file permission tightening
- Request body size limits, X-Forwarded-For validation
- Token TTL and password policy improvements
- macOS codesign verification on auto-downloaded cloudflared
- Focus trap in modals for keyboard accessibility
- Prevent git credential prompts from hanging daemon processes

### schmux development

**Dev Mode Overhaul** — TypeScript dev runner with Ink/React TUI replacing bash script. Color-coded log levels, toggleable log panel, workspace switching, git pull shortcut, and `--plain` flag for CI.

**Scenario Testing** — Playwright-based end-to-end test infrastructure with 15+ scenario tests covering core user flows. Docker containerization, parallel execution, video recording on failure, and CI workflow.

**Website** — Static marketing site with real product screenshots, interactive guided demo, and 7 feature highlight sections.

- Embedded dashboard assets in binary for single-file distribution
- Build-time module exclusion for deployment customization
- Per-package coverage analysis
- Tmux control mode RTT health probe with dashboard histogram
- Typing performance stats for remote sessions

### Dashboard & UX

- Tips page with prompt engineering tab, git lifecycle docs, and power tools
- chi router migration with middleware and route groups
- TypeScript strict mode
- Dual-stack IPv6 listener
- Custom favicon
- Mobile responsive layout with bottom nav
- Collapsible Tools section replacing More popover
- iTerm2 deep link, VS Code for remote browser clients
- Gray out sessions/workspaces during disposal
- Toast notifications replacing modal dialogs for non-blocking feedback
- Configurable clear-screen stripping and WebGL renderer
- Sidebar badge and overlay activity feed with inline diffs
- Clone button for remote host flavors

### Infrastructure

- Structured logging via charmbracelet/log
- TypeScript test runner replacing bash test.sh
- Nix flake dev environment and justfile
- Parallel E2E test execution with per-test isolation
- Definition-of-done enforcement in /commit command
- go vet in definition of done
- Split Docker images into base + thin layers
- Podman fallback when Docker unavailable
- Dev config backup on daemon start
- Daemon struct refactor replacing package-level globals
- Handler split from monolithic handlers.go into domain files

### Performance

- Parallel git status updates with per-cycle caching
- Deduplicated git fetch across worktrees
- Overlapped concurrent API fetches
- Terminal write coalescing to reduce xterm.js render pressure
- tmux pause-after for reliable control mode delivery
- Workspace test speedup with shared template repo

## Version 1.1.2 (2026-02-06)

**Major features:**

- Compact spawn page controls - Streamlined controls with dropdown menus for faster configuration
- Dynamic terminal resizing - Terminal viewport now adjusts dimensions when browser window is resized
- Keyboard shortcut system for rapid dashboard navigation (press `?` to view shortcuts)
- GitHub PR discovery for workspace creation - Browse and checkout PRs directly from the dashboard

**Improvements:**

- Select-to-click marker positioning for scrolled terminal content
- Session resume (`/resume`) - Continue agent conversations instead of starting fresh

**Bug fixes:**

- Unified modal provider consolidates all dashboard modals (Codex models and more)
- Branch suggestion UX - Failures now prompt for explicit user input instead of silently defaulting
- Binary file detection with memory safety improvements

## Version 1.1.1 (2026-01-31)

**Major features:**

- Git History DAG visualization for workspace branches - Interactive visualization showing commit topology following Sapling ISL patterns
- Multi-line selection in terminal viewer with explicit Copy/Cancel actions

**Improvements:**

- Qwen3 Coder Plus model support - Added new AI model option for enhanced capabilities
- Default branch detection now uses repository's actual default branch instead of hardcoding "main"
- Terminology clarified: distinguished query repos from worktree bases

**UI**

- Reduced diff page font sizes for improved content density
- ConfigPage heading styles consolidated into global CSS

**Bug fixes:**

- Branch review diff now compares against divergence point for more accurate comparisons
- Fixed git rev-list ambiguous argument error in workspace status
- E2E test cleanup improved with dangling Docker image removal after tests
- Go updated to 1.24 with tar command fix in release workflow

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
