# Toggle debug UI from settings

A user wants to enable diagnostic panels (Event Monitor, Tmux stats, Typing
Performance, Lore Curation) without running schmux in dev mode. They navigate
to the Settings page, open the Advanced tab, and toggle the "Enable debug UI"
checkbox. The config round-trips through the API correctly.

Note: the scenario test daemon runs with --dev-mode, which always enables
debug panels. Panel visibility cannot be asserted here — that behavior is
covered by unit tests. This scenario validates the config UI toggle itself.

## Preconditions

- The daemon is running
- At least one repository is configured

## Verifications

- Navigate to /config?tab=advanced — the Advanced tab loads
- The Advanced tab has an "Enable debug UI" checkbox that is initially unchecked
- Check the "Enable debug UI" checkbox and save the config
- GET /api/config after save shows debug_ui=true
- Navigate away and back to /config?tab=advanced — the checkbox is still checked (persisted)
- Uncheck the "Enable debug UI" checkbox and save the config
- GET /api/config after save shows debug_ui is absent or false
- Navigate away and back to /config?tab=advanced — the checkbox is unchecked (persisted)
