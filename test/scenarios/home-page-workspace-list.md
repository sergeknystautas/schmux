# View active workspaces on the home page

A user wants to see all their active workspaces and their running sessions
at a glance.

They navigate to the home page. The workspace list shows each workspace
with its repository name, branch, and git divergence stats (behind/ahead counts).
Workspaces with running sessions show a badge indicating the count.

## Preconditions

- The daemon is running with at least two workspaces, each with at least one session

## Verifications

- The home page loads and shows the workspace list
- Each workspace row shows the branch name and git stats (behind | ahead)
- The workspace list updates in real-time when a new session is spawned
  (via WebSocket, without page reload)
- Clicking a workspace row navigates to the first session in that workspace
