# Quick launch a session from a recent branch

A user wants to quickly spawn a new session on a branch they recently
worked on, without going through the full spawn wizard.

They go to the home page, find a recent branch in the "Recent Branches"
card, and click it. This should prepare the spawn form with the branch
and repository pre-filled, then navigate to the spawn page.

## Preconditions

- The daemon is running with at least one repository configured
- The repository has at least one branch with recent commit activity
- At least one promptable agent is configured

## Verifications

- The home page shows the "Recent Branches" card with at least one branch
- Each branch row shows the branch name, repo, and time
- Clicking a branch navigates to the spawn page
- The spawn page has the repository pre-selected
- The user can type a prompt and submit to spawn on that branch
