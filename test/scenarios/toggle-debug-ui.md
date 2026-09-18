# Toggle feature diagnostics from settings

A user wants Event Monitor and Tmux Diagnostics to turn on only with their own
sidebar feature switches. They open Settings, use the Advanced tab's Sidebar
Panels section, and enable or disable each diagnostic independently. Changes
auto-save and round-trip through the API.

## Preconditions

- The daemon is running
- At least one repository is configured

## Verifications

- Navigate to `/config?tab=advanced`; the Advanced tab loads
- The Event Monitor and Tmux Diagnostics checkboxes are initially unchecked
- Check Event Monitor; after auto-save, `GET /api/config` reports `ui.panels.eventMonitor === true`
- Check Tmux Diagnostics; after auto-save, `GET /api/config` reports `ui.panels.tmuxDiagnostic === true`
- Navigate away and back; both checkboxes remain checked
- Uncheck Event Monitor; after auto-save, `GET /api/config` reports `ui.panels.eventMonitor === false`
- Uncheck Tmux Diagnostics; after auto-save, `GET /api/config` reports `ui.panels.tmuxDiagnostic === false`
- Navigate away and back; both checkboxes remain unchecked
