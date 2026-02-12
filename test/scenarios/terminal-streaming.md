# View live terminal output

A user wants to watch an AI agent's terminal output in real-time as it works.

They navigate to a running session's detail page. The terminal viewport
should show live output streaming from the agent process. As the agent
produces more output, it should appear in the terminal without the user
needing to refresh.

## Preconditions

- The daemon is running with a session that continuously produces output
  (e.g., an agent running `sh -c 'while true; do echo tick; sleep 1; done'`)

## Verifications

- The session detail page shows the terminal viewport
- The terminal displays initial output from the agent
- New output appears automatically without page refresh
- The terminal auto-scrolls to follow new output (tail mode)
- The WebSocket connection indicator shows "Live" (connected state)
