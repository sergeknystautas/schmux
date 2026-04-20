# Sapling workspace spawn with optional Label

A user wants to spawn an agent against a sapling repository. Sapling has no
named branch concept, so the spawn page hides the branch input entirely. In
its place, the page offers an optional **Label** input — a free-form
human-friendly name for the workspace. The placeholder shows the
prospective on-disk workspace ID (e.g. `test-sapling-label-001`), so the user
sees what name will be used if they leave the field empty.

When spawned without a label, the sidebar shows the workspace ID as the
display name. When spawned with a typed label (e.g. `Login bug fix`), the
sidebar shows the label instead. In both cases the workspace's persisted
`branch` stays empty — sapling repos never carry a branch in state.

## Preconditions

- The daemon is running
- Sapling (`sl`) is installed in the test environment
- A configured sapling repo (with `vcs: sapling`)
- No existing sapling workspaces for the test repo

## Verifications

- On the spawn page, selecting a sapling repo hides the branch input and
  reveals a Label input with the prospective workspace ID as its placeholder
  (this UI assertion is skipped if the test environment has no detected
  models — the spawn-page selectors are gated on the model catalog)
- The placeholder format is `{repoName}-{NNN}` (zero-padded 3-digit counter)
- A spawn with no label produces a workspace whose sidebar entry shows the
  workspace ID (e.g. `test-sapling-label-001`)
- A spawn with the label `Login bug fix` produces a workspace whose sidebar
  entry shows `Login bug fix` (and does NOT show the workspace ID)
- The `/api/sessions` response for the labelled workspace has
  `branch === ""`, `label === "Login bug fix"`, and `vcs === "sapling"`
