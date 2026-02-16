# Persist lore curator model selection

A user configures the lore curator model on the config page and expects
the selection to survive a page reload. They navigate to the Advanced
tab, select an agent as the LLM target, save, reload the browser, and
verify the dropdown still shows the chosen agent.

The same round-trip should work through the API: POST the lore config,
GET it back, and confirm the values match.

## Preconditions

- The daemon is running
- At least one repository is configured
- At least one promptable agent is configured

## Verifications

- POST /api/config with lore.llm_target is accepted
- GET /api/config returns the same llm_target that was POSTed
- On the config page, selecting an LLM target and saving succeeds
- After reloading the page, the Advanced tab still shows the saved LLM target
- After reloading, curate_on_dispose also retains its value
