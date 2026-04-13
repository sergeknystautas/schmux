# Autolearn page shows configuration warning

A user navigates to the autolearn page to review proposals. The autolearn system
is enabled but no LLM target has been configured, so the curator cannot
run. The page should show a yellow warning banner explaining the issue
and linking to the config page.

The user can also check the autolearn status API directly to see the system
health.

## Preconditions

- The daemon is running
- At least one repository is configured
- Autolearn is enabled (default) but no llm_target is set

## Verifications

- GET /api/autolearn/status returns enabled=true, curator_configured=false
- GET /api/autolearn/status returns a non-empty issues array
- The autolearn page loads and shows the "Autolearn" heading
- A warning banner is visible on the autolearn page
- The warning banner contains text about LLM target not being configured
- The warning banner contains a link to the config page Advanced tab
