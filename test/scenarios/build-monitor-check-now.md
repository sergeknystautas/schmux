# Build Monitor Check Now

A user opens the Build Monitor page and clicks Check Now to fetch the
latest GitHub Actions status for all configured units. The results show
passing, failing, and error states with appropriate links.

## Preconditions

- The daemon is running
- Dashboard auth is enabled
- GitHub OAuth app is configured
- At least one GitHub identity has been authorized for build access
- Build Monitor is enabled in the Experimental tab
- Three repos are enabled with different outcomes:
  - Repo A: one active workflow whose latest run is passing
  - Repo B: one active workflow whose latest run is failing with two failed jobs
  - Repo C: the GitHub API returns a 401 (token lacks access)
- The GitHub Actions API base URL is overridable for test stubbing

## Verifications

- GET /api/build-monitor returns the last persisted check (or empty if first time)
- The Build Monitor page shows a Check Now button
- Clicking Check Now disables the button while the fetch is in flight
- After Check Now completes:
  - Repo A's workflow row shows "Passing" with a run link and checked-at time
  - Repo B's workflow row shows "Failing" with the run link and two failed-job links
  - Repo C shows an unauthorized error with a re-authorize hint
- POST /api/build-monitor/check returns the same response shape as GET
- Reloading /build-monitor shows the same persisted results without re-fetching
- The state files exist at ~/.schmux/build-monitor/<slug>.json for each unit
