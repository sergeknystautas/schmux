# Spec: Keyboard Shortcuts

## Overview

Add a keyboard shortcut system to the web dashboard for rapid navigation and session management. Users are primarily in terminal sessions, so shortcuts must be context-aware and not conflict with terminal/shell keybindings.

## Constraints

1. **Terminal-primary**: Users spend most time in xterm/tmux sessions. Shortcuts should work when the web UI has focus, not when typing in terminal.
2. **Mac-first**: Use `Cmd`/`Option` conventions, not Windows-centric `Ctrl` patterns.
3. **Browser-compatible**: Avoid conflicts with browser/system shortcuts (`Cmd+N`, `Cmd+T`, `Cmd+W`, `Cmd+L`, `Cmd+R`, `Cmd+[1-9]`, `Cmd+/-`, `Cmd+F`, `Cmd+S`, `Cmd+P`, clipboard/undo).
4. **No `Ctrl` combos**: Terminal users have muscle memory for `Ctrl+C`, `Ctrl+Z`, `Ctrl+D`, etc.

## Pattern: Three-Tier System

### Tier 1: Command Palette (Primary)

`Cmd+K` — Opens a searchable command palette

This is the primary entry point for all actions. Users type what they want: "spawn", "sessions", "dispose", "focus session 3", "go to tips".

**Why:**
- One shortcut to remember
- Discoverable (type to see available commands)
- Extensible without teaching new shortcuts
- `Cmd+K` is unused in browsers
- Modern standard (Linear, Slack, VS Code, GitHub, Notion)

**Implementation notes:**
- Modal overlay with text input
- Fuzzy search over available commands
- Keyboard navigation within results
- `Esc` to close, `Enter` to execute

### Tier 2: Single-Key Navigation (Context-Aware)

Letters that **only work when no input/terminal has focus**:

| Key | Pattern | Use |
|-----|---------|-----|
| `?` | Help overlay | Show all available shortcuts |
| `0-9` | Jump | Go to session N, or page 0 (spawn) |
| `n` / `p` | Navigate | Next / previous session |
| `s` / `w` | Focus list | Focus sessions list / workspaces list |
| `/` | Quick search | Jump to search/filter input |

**Why:**
- Fast for frequent actions
- Vim-like single-key efficiency
- Won't trigger when typing in terminal (different focus context)
- Gmail/Twitter use this pattern successfully

**Focus gate implementation:**
Shortcuts only trigger when:
- No `<input>` element has focus
- No `<textarea>` element has focus
- Terminal websocket component doesn't have focus

This is how GitHub/Gmail work—try pressing `k` on GitHub, then click in a comment box and press `k` again.

### Tier 3: Option-Modified (Intentional Actions)

`Option+Key` for destructive or less common actions:

| Combo | Pattern |
|-------|---------|
| `Option+D` | Destructive actions (dispose session) |
| `Option+R` | Restart/reload actions |
| `Option+X` | Kill/force-stop |
| `Option+Shift+S` | Spawn new session (two-handed combo = intentionality) |

**Why:**
- `Option` is rarely used in browsers
- Two-handed combos signal intentionality (good for destructive actions)
- Easy to press on Mac (thumb on Option, finger on letter)

## Patterns to Avoid

| Avoid | Reason |
|-------|--------|
| `Ctrl+[anything]` | Terminal/shell muscle memory conflict |
| `Cmd+[letter]` | Mostly reserved by browser (new tab, save, print, etc.) |
| `Cmd+Shift+[letter]` | Browser conflicts (new tab, close tab, etc.) |
| `F-keys` | Some browsers block, different keyboard layouts |
| `Alt+Tab` style | OS-level window switching |

## Implementation Phases

1. **Phase 1**: Command palette (`Cmd+K`)
   - Build command registry system
   - Implement modal overlay
   - Add fuzzy search
   - Migrate existing actions to command registry

2. **Phase 2**: Help overlay (`?`)
   - Static display of all shortcuts
   - Grouped by category
   - Shows as modal/overlay

3. **Phase 3**: Single-key nav
   - Add focus gate logic
   - Implement number-based session jumps
   - Add `n`/`p` for next/prev session

4. **Phase 4**: Option-modified
   - Only for destructive actions
   - Consistent with Phase 1-3 patterns

## Success Criteria

- [ ] `Cmd+K` opens command palette on all pages
- [ ] `?` opens help overlay showing all shortcuts
- [ ] Single-key shortcuts only work when inputs/terminal lack focus
- [ ] `Option+Key` shortcuts work regardless of focus (intentional)
- [ ] No conflicts with browser, OS, or terminal shortcuts
- [ ] Shortcuts documented in help overlay
- [ ] Command palette is extensible (add new commands without code changes)
