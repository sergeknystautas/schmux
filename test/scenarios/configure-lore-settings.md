# Configure lore settings

A user wants to configure the lore system so that agent learnings are
automatically curated into documentation proposals. They navigate to
the config page, open the Advanced tab, and find the Lore settings
section.

They enable lore, select an LLM target, choose when to curate on
dispose, and save. After saving, the lore status API reflects the
new configuration and the warning banner on the lore page disappears.

## Preconditions

- The daemon is running
- At least one repository is configured
- At least one promptable agent is configured

## Verifications

- The config page loads and the Advanced tab is accessible
- The Advanced tab contains a "Lore" settings section
- The Lore section has an "Enable lore system" checkbox (checked by default)
- The Lore section has an "LLM Target" dropdown
- The Lore section has a "Curate On Dispose" dropdown with options: every session, last session per workspace, never
- Selecting an LLM target and saving succeeds
- POST /api/config with lore fields is accepted
- GET /api/lore/status after saving shows curator_configured=true and empty issues
