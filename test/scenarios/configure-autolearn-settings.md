# Configure lore settings

A user wants to configure the lore system so that agent learnings are
automatically curated into documentation proposals. They navigate to
the Settings page, open the Experimental tab, and find the Lore
feature card.

They enable lore via the experimental toggle, select an LLM target,
choose when to curate on dispose. Changes auto-save (no Save button).
After the auto-save completes, the lore status API reflects the new
configuration.

## Preconditions

- The daemon is running
- At least one repository is configured
- At least one promptable agent is configured

## Verifications

- The Settings page loads and the Experimental tab is accessible
- The Experimental tab contains a "Lore" feature card with an enable toggle
- When enabled, the Lore config panel shows an "LLM Target" dropdown
- When enabled, the Lore config panel shows a "Curate On Dispose" dropdown with options: every session, last session per workspace, never
- Changing curate-on-dispose auto-saves (wait briefly for auto-save to complete)
- POST /api/config with lore fields is accepted
- GET /api/config after saving shows the lore fields round-tripped correctly
