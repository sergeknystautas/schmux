# Spawn applies default communication style from config

A user has configured a default communication style for an agent type in the
config page. When they spawn a session for that agent type without specifying
a style, the session should automatically use the configured default. When
they spawn with an explicit style, that overrides the default. When they spawn
with `style_id: "none"`, no style is applied even if a default exists.

## Preconditions

- The daemon is running
- At least one repository is configured
- At least one promptable agent is configured
- At least one communication style exists (built-ins are available)

## Verifications

- Setting a comm_styles default via POST /api/config succeeds
- Spawning a session without specifying style_id uses the configured default
- GET /api/sessions returns the session with the correct style_id matching the default
- Spawning a session with an explicit style_id overrides the default
- GET /api/sessions returns the session with the explicit style_id, not the default
- Spawning a session with style_id "none" suppresses the default
- GET /api/sessions returns the session with no style_id
