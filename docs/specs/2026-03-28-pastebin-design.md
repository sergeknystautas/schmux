# Pastebin — Saved Text Clips for Terminal Input

## Problem

Users frequently paste the same commands or text blocks into terminal sessions. Currently they must switch to a clipboard manager or re-type common commands. A built-in quick-paste feature eliminates this friction.

## Design

A button next to the spawn "+" button in the session tab bar opens a dropdown listing saved text entries. Clicking an entry pastes its content into the active session's terminal via WebSocket and closes the dropdown.

### Data Model

```go
// internal/config/config.go
Pastebin []string `json:"pastebin"`
```

Each entry is a plain string — the text content to paste. No name field, no struct wrapper. Entries are sorted alphabetically in the dropdown (display shows first line, truncated).

Stored in `~/.schmux/config.json` under the `"pastebin"` key.

### UI — Button & Dropdown

**Location**: `SessionTabs.tsx`, immediately after the spawn "+" button. Same visual treatment (icon-only, same dimensions).

- **Enabled**: When an active session exists (selected session ID is set)
- **Disabled**: No active session — button is dimmed, no click handler

**Icon**: The Pastebin logo SVG from https://www.svgrepo.com/svg/473743/pastebin (CC0 license).

**Dropdown**: Follows `ActionDropdown` exactly — same styles, same portal-to-body pattern, same structure:

- Section header: **PASTEBIN** (same styling as "QUICK LAUNCH" / "EMERGED" in spawn dropdown)
- Entries listed alphabetically, each showing the first line of content (truncated)
- Clicking an entry calls `sendInput(entry.content)` on the active session's terminal stream and closes the dropdown
- Empty state: "No pastes yet" in italics
- **Manage** link at the bottom (same pattern as spawn dropdown), navigates to the Pastebin config tab at `/config` with the pastebin tab selected
- Close on click-outside only — no Escape key handler

`PastebinDropdown` is a standalone component reusing the same CSS classes as `ActionDropdown`.

### Config Tab

New "Pastebin" tab in the config settings page:

- `PastebinTab.tsx` in `assets/dashboard/src/routes/config/`
- Tab label: "Pastebin", slug: `"pastebin"`
- Add `'Pastebin'` to `TABS` array and `'pastebin'` to `TAB_SLUGS` in `ConfigPage.tsx`, bump `TOTAL_STEPS` to 10
- Content: List of text entries with multi-line textareas. Add/remove entries. No reorder.
- `useConfigForm.ts` reducer gets a new `pastebin: string[]` field
- On save, the full array is sent to the API

### Go Backend Changes

Four files:

- `internal/config/config.go` — Add `Pastebin []string` field on Config struct with getter
- `internal/api/contracts/config.go` — Add `Pastebin *[]string` to `ConfigResponse` and `ConfigUpdateRequest`
- `internal/dashboard/handlers_config.go` — Handle pastebin in GET (include in response) and UPDATE (apply if present)
- `docs/api.md` — Document the new field

### Data Flow

1. User configures pastes in Settings → Pastebin tab → saved to `~/.schmux/config.json`
2. Config loaded by frontend on page load via `GET /api/config`
3. User clicks pastebin button → `PastebinDropdown` renders entries from config
4. User clicks entry → `sendInput(content)` on active session's `TerminalStream` → WebSocket sends UTF-8 encoded text → tmux receives as terminal input
5. Dropdown closes
