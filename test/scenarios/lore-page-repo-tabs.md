# View autolearn page as a flat card wall

A user navigates to the autolearn page to review agent learnings. The page
shows a flat list of cards aggregated across all configured repos —
there are no repo tabs or sub-tabs.

Each card shows the learning text. The page heading reads "Autolearn" with
the subtitle "Schmux continual learning system".

When no pending proposals exist, an empty state message is shown.

## Preconditions

- The daemon is running
- At least one repository is configured

## Verifications

- The sidebar shows an "Autolearn" link
- Clicking "Autolearn" navigates to /autolearn
- The page shows an "Autolearn" heading
- The page shows the subtitle "Schmux continual learning system"
- There is no repo tab bar (no elements with data-testid="repo-tab")
- GET /api/autolearn/{repoName}/proposals responds for each configured repo
- When no pending proposals exist, the page shows "Nothing to review"
