# Record client performance and send it from the sidebar

A developer enables Client Performance through the ordinary sidebar panel preferences in dev mode, launches its agent, records browser activity, and sends the recording from the sidebar.

## Preconditions

- Client performance is enabled with a schmux repo and chat target.
- The panel preference ui.panels.clientPerformance is enabled.
- Health, dev status, workspace launch, recording delivery, and dashboard/chat sockets are controlled fixtures.

## Verifications

- Record is unavailable until Launch creates the dedicated workspace and its agent appears through dashboard state.
- Record binds the capture to the running agent session.
- Stop sends the recording without a destruction warning and opens the agent.
- The sidebar links to the agent chat and keeps that link after reload without another launch.
- Ordinary chat has no recording attachment checkbox.
- Stop posts the JSON file directly to the recording endpoint for that agent. The file includes build, environment, and timeline.
- The sidebar shows Recording sent after daemon acceptance.
