# Spec: Keyboard Shortcuts

## Overview

Add a keyboard shortcut system to the web dashboard for rapid navigation and session management. Users are primarily in terminal sessions, so shortcuts must be context-aware and not conflict with terminal/shell keybindings.

## Constraints

1. **Terminal-primary**: Users spend most time in xterm/tmux sessions. Shortcuts should work when the web UI has focus, not when typing in terminal.
2. **Mac-first**: Use `Cmd` conventions, not Windows-centric `Ctrl` patterns.
3. **Browser-compatible**: Avoid conflicts with browser/system shortcuts (`Cmd+N`, `Cmd+T`, `Cmd+W`, `Cmd+L`, `Cmd+R`, `Cmd+[1-9]`, `Cmd+/-`, `Cmd+F`, `Cmd+S`, `Cmd+P`, clipboard/undo).
4. **No `Ctrl` combos**: Terminal users have muscle memory for `Ctrl+C`, `Ctrl+Z`, `Ctrl+D`, etc.

## Pattern: Prefix Key Mode

Emacs/tmux-style prefix key system. `Cmd+K` enters a keyboard mode where the next keystroke triggers an action.

### Entering Keyboard Mode

`Cmd+K` — Enters keyboard mode

**Behavior:**
- Saves current focus element
- Shows visual indicator (toast or corner pill)
- Waits for next keystroke
- If unrecognized key pressed: exit mode, restore focus
- If browser loses focus: exit mode, restore focus

### Keyboard Mode Actions

| Key | Action | Context |
|-----|--------|---------|
| `N` | Spawn new session (context-aware) | If in workspace: local spawn. Otherwise: general spawn. |
| `Shift+N` | Spawn new session (general) | Always goes to general `/spawn` page, regardless of context. |
| `0-9` | Jump to session | Within a workspace, goes to session N by index. |
| `W` | Dispose session | On session detail page only. Shows confirmation modal. |
| `D` | Go to diff page | Within a workspace only. |
| `G` | Go to git graph | Within a workspace only. |
| `H` | Go to home | Always goes to `/`. |
| `?` | Show help modal | Displays all available shortcuts with descriptions. |

### Exiting Keyboard Mode

Keyboard mode exits (and restores original focus) when:
- Recognized action is executed
- Unrecognized key is pressed
- Browser window loses focus
- `Esc` is pressed

## Visual Indicator

When keyboard mode is active, show a visual indicator. Options:

1. **Center toast**: "Keyboard mode active - press a key or Esc to cancel" (auto-dismiss after 2-3s)
2. **Corner pill/badge**: Small indicator in top-right or bottom-right (like connection status)

To be determined.

## Patterns to Avoid

| Avoid | Reason |
|-------|--------|
| `Ctrl+[anything]` | Terminal/shell muscle memory conflict |
| `Cmd+[letter]` | Mostly reserved by browser (new tab, save, print, etc.) |
| `Cmd+Shift+[letter]` | Browser conflicts (new tab, close tab, etc.) |
| `F-keys` | Some browsers block, different keyboard layouts |
| `Alt+Tab` style | OS-level window switching |

## Success Criteria

- [ ] `Cmd+K` enters keyboard mode on all pages
- [ ] Visual indicator shows when in keyboard mode
- [ ] Keyboard mode actions execute as specified
- [ ] Unrecognized keys exit mode and restore focus
- [ ] Browser blur exits mode and restores focus
- [ ] No conflicts with browser, OS, or terminal shortcuts
- [ ] Help modal (`?`) documents all shortcuts
