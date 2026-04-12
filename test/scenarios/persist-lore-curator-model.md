# Persist lore curator model selection

A user configures the lore curator model on the Settings page and expects
the selection to survive a page reload. They navigate to the Experimental
tab, find the Lore feature card, select an agent as the LLM target via
curate-on-dispose dropdown (since TargetSelect only shows catalog models
unavailable in test). Changes auto-save. After reloading the browser,
the dropdown still shows the chosen value.

The same round-trip should work through the API: POST the lore config,
GET it back, and confirm the values match.

## Preconditions

- The daemon is running
- At least one repository is configured
- At least one promptable agent is configured

## Verifications

- POST /api/config with lore.llm_target is accepted
- GET /api/config returns the same llm_target that was POSTed
- On the Settings page Experimental tab, changing curate-on-dispose auto-saves
- Wait briefly for auto-save to complete
- After reloading the page, the Experimental tab still shows the saved curate_on_dispose value
- After reloading, the API confirms both llm_target and curate_on_dispose retained their values
