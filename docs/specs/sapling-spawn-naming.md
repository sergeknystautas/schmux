# Sapling Workspace Naming on the Spawn Page

**Status:** v3 — revised after second adversarial review
(`sapling-spawn-naming-review-2.md`).

## Problem

The spawn page asks the user for a **branch name** when starting a fresh
workspace. For git repos this doubles as a human-meaningful label — the
sidebar shows the branch next to the auto-generated workspace ID, so a user
can scan `fix/spawn` or `feature/login` and know what each workspace is for.

Sapling repos have no equivalent concept. The branch input is meaningless,
and without it the workspace list collapses to opaque sequential IDs
(`myrepo-007`, `myrepo-008`, …) with nothing to distinguish one from another.

## Goals

- Hide branch-related controls when the selected repo's `vcs === "sapling"`.
- Give the user an **optional** way to label the workspace.
- When no label is provided, **fall back to the workspace ID** (the on-disk
  name), not to a substituted branch placeholder.
- Keep the change cosmetic: the label is for display only. It does **not**
  become a sapling bookmark, commit message, or PR title.

## Non-goals

- Auto-suggesting a label via LLM. Optional naming with a sensible fallback
  is good enough; LLM cost is not justified.
- Adding the optional label to git workspaces. The branch already serves
  that role for git. (The new field will be schema-available, so this stays
  cheap to add later if desired.)
- Changing workspace identity. The on-disk workspace ID remains
  `{repoName}-{NNN}`. The label is purely a display alias.
- Touching the existing **session** `Nickname` feature. Sessions retain
  their own per-spawn `nickname` field with sanitization rules and tmux
  name derivation (see `docs/api.md`). The new workspace label is a
  **separate concept** at a different level of the hierarchy.
- Supporting workspace-label entry from the slash-command spawn paths
  (`/resume`, `/quick`, command targets). Those paths spawn from a single
  textbox with no inline label affordance — supporting it would clutter the
  command palette. Workspaces created via slash commands get the
  auto-generated ID and can be renamed later if a "rename workspace"
  follow-up ships.

## Vocabulary

This spec consistently uses **"label"** (Go: `Label`, JSON: `label`) for
the new workspace-level field. We avoid `nickname`, `name`, and
`display_name` to prevent collision with:

- `state.Session.Nickname` (`internal/state/state.go:300`) — already
  serialized as `nickname` and used end-to-end through the spawn pipeline,
  with sanitization, uniqueness suffixes, and tmux-name derivation.
- `SpawnRequest.Nickname` (`internal/api/contracts/spawn_request.go:8`) —
  already on the same JSON shape we'd be modifying.

## Design

### Schema

Add one field to `state.Workspace` (`internal/state/state.go:125`):

```go
Label string `json:"label,omitempty"` // Optional human-friendly display label
```

`omitempty` keeps backward compatibility — existing persisted workspaces
deserialize with `Label == ""` and render as today.

Mirror the field on the workspace API response in
`internal/api/contracts/sessions.go` (the `WorkspaceResponseItem` struct,
not `SessionResponseItem`).

Add a new `WorkspaceLabel` field to `SpawnRequest`
(`internal/api/contracts/spawn_request.go`) — distinct from the existing
`Nickname` (which remains the session label):

```go
WorkspaceLabel string `json:"workspace_label,omitempty"` // Optional workspace display label (sapling-only today)
```

Regenerate TypeScript via `go run ./cmd/gen-types`.

### Backend: branch handling for sapling

Two enforcement points currently reject `branch: ""` and must be relaxed.
**Crucially, `state.Workspace.Branch` is left empty (`""`) for sapling
workspaces** — we do **not** substitute `"main"` into persisted state. The
substitution happens only at the sapling backend boundary where a non-empty
template variable is needed.

1. `internal/dashboard/handlers_spawn.go:134-137` — replace the
   unconditional `if req.Branch == ""` rejection with an exemption when the
   resolved repo's `VCS == "sapling"`. Look up the repo via the existing
   config helper (`config.FindRepoByURL`) before the check.

2. `internal/workspace/manager.go::GetOrCreate` (line 416) — call
   `findRepoByURL` first; if `repo.VCS == "sapling"` and `branch == ""`,
   skip `ValidateBranchName` entirely and pass `""` through to `create()`.
   Persist `state.Workspace.Branch = ""`.

3. `internal/workspace/manager.go::create()` — when `repoConfig.VCS ==
"sapling"` and `branch == ""`, substitute `"main"` **only for the
   sapling backend call** (`backend.CreateWorkspace`'s template variable).
   Do not write the substituted value back to `branch` or to
   `state.Workspace.Branch`.

This keeps the workspace's persisted `Branch` field semantically accurate
("no branch — sapling workspace") and lets the frontend's display helper
fall through to the workspace ID naturally without VCS-aware logic in the
view layer.

`Manager.create()` and `CreateLocalRepo()` accept the new `label` argument
and persist it on the `state.Workspace` they construct. The dashboard spawn
handler passes `req.WorkspaceLabel` through.

### Workspace-mode `workspace_label` semantics

`req.WorkspaceLabel` arrives on every spawn request, but workspace-mode
spawns reuse an existing workspace and never reach `Manager.create()`. The
handler **silently ignores** `workspace_label` when `WorkspaceID != ""`.

Rationale: workspace-mode spawn means "add another session to this
existing workspace." Renaming a workspace at session-spawn time is a
surprising side effect; if rename is wanted, it belongs in a dedicated
endpoint (out of scope here, candidate follow-up).

A compliant client (this spec's UI) only sends `workspace_label` for
fresh sapling spawns, so the silent-ignore behavior is invisible. Third-
party CLI callers that send it in workspace mode get no error and no
effect — documented as such in `docs/api.md` when this ships.

### `__new__` (add new repo) flow

Sapling repos cannot be added via the inline `__new__` URL detection
flow — `handlers_spawn.go:182-198` registers new repos as git when the URL
is git-shaped, and there is no inline UI for "add sapling repo." Sapling
repos are only added via the config UI's repo editor, which sets `VCS =
"sapling"` explicitly. Therefore by construction, when `req.Repo` resolves
to a sapling repo, it always corresponds to an existing config entry — the
`__new__` branch never produces a sapling repo and needs no special
handling here.

### Spawn page UI (`assets/dashboard/src/routes/SpawnPage.tsx`)

Detect sapling via the existing `repos` data:

```ts
const selectedRepo = repos.find((r) => r.url === repo);
const isSapling = selectedRepo?.vcs === 'sapling';
```

Memoize once and use everywhere below. Six branch-touching code paths need
sapling-aware handling:

| #   | Location                                                                           | Today                                                                                           | Sapling change                                                                              |
| --- | ---------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| 1   | Single-agent branch input (`SpawnPage.tsx:1226-1238`)                              | Shown when `showBranchInput`                                                                    | Hidden when `isSapling`; replaced by label input in same slot                               |
| 2   | Multi/advanced branch input (`SpawnPage.tsx:1584-1599`)                            | Shown when `showBranchInput && modelSelectionMode !== 'single'`                                 | Hidden when `isSapling`; label input rendered above (or in a comparable spot) instead       |
| 3   | `showBranchInput` auto-set effect (`SpawnPage.tsx:256-260`)                        | Forces show when `branch_suggest.target` unset                                                  | Skip the auto-set when `isSapling`                                                          |
| 4   | "Create new branch from here" checkbox (workspace mode, `SpawnPage.tsx:1632-1653`) | Shown when `currentWorkspace` exists                                                            | Hidden when `currentWorkspace.vcs === 'sapling'` (read VCS off the workspace, not the repo) |
| 5   | `validateForm` branch check (`SpawnPage.tsx:584-587`)                              | Errors when `mode === 'fresh' && !branchSuggestTarget && !branch.trim()`                        | Add `&& !isSapling` to the gating clause                                                    |
| 6   | LLM branch suggester in fresh-mode `handleEngage` (`SpawnPage.tsx:835`)            | Calls `generateBranchName(prompt)` for fresh spawns when `prompt.trim() && branchSuggestTarget` | Short-circuit when `isSapling` — never invoke the suggester                                 |

Plus one path that is **defensively safe but worth noting**:

- Workspace-mode LLM suggester (`SpawnPage.tsx:861-872`) is gated on
  `createBranch`, which element 4 hides for sapling workspaces. So in
  practice it never fires for sapling. Add an explicit `isSapling` short-
  circuit anyway as a defensive guard against future code changes that
  decouple the checkbox from the conditional.

And the alternate spawn paths:

- `/resume` (`SpawnPage.tsx:678-727`) and command-target slash commands
  (`SpawnPage.tsx:756-779`) compute `actualBranch = branch.trim() ||
getDefaultBranch(actualRepo)`. For sapling fresh spawns through these
  paths, this would send `branch: "main"` instead of `branch: ""`. Change
  both call sites to send `branch: ""` when `isSapling` (matches the
  primary path's semantics: empty branch + backend handles substitution).
  Per the non-goals, these paths do not surface a label input, so they
  always send `workspace_label: ""`.

### Spawn page UI: the label input

The label input replaces the branch input visually:

```
[Agent ▾] [Persona ▾] [Style ▾] [Repo ▾]  [Label: __________________]
                                          placeholder: "myrepo-008"
```

The placeholder shows a prospective workspace ID computed client-side from
the repos snapshot. **It is best-effort and may be off by one or two** in
the presence of:

- Concurrent spawns from other tabs/clients
- `findNextWorkspaceNumber`'s gap-filling behavior
  (`internal/workspace/manager.go:990-1008` — returns the lowest unused N,
  not `count+1`)
- In-flight `provisioning` workspaces that haven't broadcast yet

This is acceptable because the placeholder is purely a hint; the daemon
arbitrates the real ID at spawn time and the user sees the actual workspace
ID in the sidebar immediately after spawn. We deliberately do not add a
new endpoint just to make the placeholder authoritative.

### Display

Six render sites need the label-aware fallback. Three of them combine the
fallback with **remote-aware logic** (substitutes hostname when `branch ===
repo` for remote workspaces). To keep both behaviors composable, the
helper signature accepts a precomputed display branch:

```ts
// assets/dashboard/src/lib/workspace-display.ts
export function workspaceDisplayLabel(
  ws: WorkspaceResponseItem,
  computedBranch?: string // already remote-aware; defaults to ws.branch
): string {
  return ws.label?.trim() || computedBranch || ws.branch || ws.id;
}
```

For sapling workspaces with no label, `branch` is `""` (per the backend
change), `computedBranch` falls back to `ws.branch` which is `""`, and the
helper returns `ws.id` — satisfying the original requirement.

Sites:

| #   | File                                                                   | Notes                                                                                          |
| --- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| 1   | `assets/dashboard/src/components/WorkspaceHeader.tsx:177-188, 210-214` | Has remote-aware `displayBranch`. Pass it as `computedBranch`.                                 |
| 2   | `assets/dashboard/src/components/AppShell.tsx:736-784`                 | Sidebar `nav-workspace__name`. Has remote-aware logic — pass through.                          |
| 3   | `assets/dashboard/src/routes/HomePage.tsx:656`                         | Active workspace cards.                                                                        |
| 4   | `assets/dashboard/src/routes/HomePage.tsx:1205`                        | Backburner workspace list.                                                                     |
| 5   | `assets/dashboard/src/routes/RepofeedPage.tsx:90`                      | `{isShared && summary ? summary : ws.branch}` — wrap the `ws.branch` fallback with the helper. |
| 6   | `assets/dashboard/src/routes/OverlayPage.tsx:586`                      | Workspace selector dropdown option.                                                            |

When both label and branch exist for git workspaces, the helper still
prefers `label` first — but since git workspaces will not set `label` in
this iteration, the existing branch-display behavior is preserved
unchanged.

## Backward compatibility

- **Persisted state.** Existing workspaces deserialize with `Label == ""`;
  the helper falls back to `branch` (or for git: the existing remote-aware
  computed branch) and rendering is unchanged.
- **Spawn request.** New optional `workspace_label` field. Old clients omit
  it and behave as today. The existing `nickname` field on `SpawnRequest`
  retains its session-label semantics — we add a _new_ field rather than
  overloading the existing one.
- **Workspace response.** New optional `label` field. Old frontends ignore
  it; new frontends use it via the helper.
- **Sapling spawns today.** Currently the only working path passes
  `branch: "main"` (e.g., `vcs_sapling_test.go:328`). After this change,
  passing `branch: ""` also works for sapling, and the backend substitutes
  `"main"` at the sapling backend call only — `state.Workspace.Branch`
  stays `""`. Existing callers that pass `"main"` keep working but
  `state.Workspace.Branch` will then record `"main"` for those legacy
  spawns. This is harmless but means sapling workspaces created via the
  old path will continue to render "main" in their branch slot. New
  sapling spawns made through the updated UI render the workspace ID
  instead.

## Out of scope

- Editing label after creation (potential follow-up: sidebar context menu
  "Rename workspace" → updates `Label` only; reuse the same field).
- Surfacing the label control for git workspaces.
- Sapling bookmark integration.
- LLM auto-suggestion for sapling.
- Consolidating workspace label and session nickname into a unified
  concept.
- Supporting `workspace_label` in `/resume` / `/quick` / command-target
  slash command spawn flows.

## Testing

### Go unit tests

- `internal/state/state_test.go` — `Workspace` round-trips `Label` through
  marshal/unmarshal; empty `Label` is omitted from JSON.
- `internal/workspace/manager_test.go` —
  - `GetOrCreate` for sapling repo with empty branch skips
    `ValidateBranchName` and persists `Workspace.Branch == ""`.
  - `create()` substitutes `"main"` into the sapling backend call but does
    not write it back to `Workspace.Branch`.
- `internal/workspace/vcs_sapling_test.go` — extend the existing test:
  spawning a sapling workspace with `branch: ""` succeeds; `Label` is
  persisted when supplied.
- `internal/dashboard/handlers_spawn_test.go` —
  - Sapling repo spawn with `branch: ""` succeeds; git repo spawn with
    `branch: ""` still rejects.
  - `workspace_label` for fresh sapling spawn lands on the workspace.
  - `workspace_label` for workspace-mode spawn is silently ignored
    (existing workspace's `Label` is unchanged).

### Frontend unit tests (Vitest)

- `assets/dashboard/src/routes/SpawnPage.test.tsx` —
  - Selecting a sapling repo hides all five branch-related elements (1–5
    in the table) and shows the label input.
  - The label input's placeholder is the prospective workspace ID.
  - Submitting with empty label spawns successfully (no validation error).
  - The LLM branch suggester is **not** invoked when `isSapling` (mock
    `suggestBranch` and assert no calls) — covers both element 6 and the
    workspace-mode defensive guard.
  - `/resume` and command-target paths send `branch: ""` (not `"main"`)
    when the selected repo is sapling.
- New `assets/dashboard/src/lib/workspace-display.test.ts` —
  - Returns `label` when set.
  - Returns `computedBranch` when label is empty and branch present.
  - Returns `id` when label and branch are both empty (the sapling
    fallback case — the most important assertion).

### Scenario test (Playwright)

- `test/scenarios/` — spawn flow on a sapling repo:
  - Branch input is gone.
  - Spawn without a label → sidebar shows the workspace ID (not "main").
  - Spawn with a label → sidebar shows the label.

## Implementation order

Suggested sequence to keep each step shippable:

1. **Schema.** Add `Label` to `state.Workspace`, regen TypeScript types,
   add new `WorkspaceLabel` to `SpawnRequest`. No behavior change.
2. **Backend.** Relax branch-required check for sapling at both gates;
   substitute `"main"` only at the sapling backend call; persist
   `Workspace.Branch == ""` for sapling. Persist `Label`. Silently ignore
   `workspace_label` in workspace mode.
3. **Display helper + rendering.** Add `workspaceDisplayLabel(ws,
computedBranch?)` and swap all 6 render sites. Until the spawn page
   ships, no workspace has `Label` set and behavior is identical to today,
   except for sapling workspaces (which now render workspace ID instead of
   "main") — but this is also exactly what we want.
4. **Spawn page.** Add `isSapling`, hide branch elements 1–5, short-
   circuit LLM call sites 6 + the workspace-mode defensive guard, render
   the label input, fix `/resume` and command-target paths to send empty
   branch for sapling.
5. **Tests at each step.**

Note: step 4 without step 3 leaves a semi-functional in-between state — the
user can type a label and spawn successfully, but display sites still show
the branch (or workspace ID for sapling). Steps 3 and 4 should land
together when possible, or at least in the same release window.

## Changes from v2

- **Backend strategy switched.** v2 substituted `"main"` into
  `state.Workspace.Branch` for sapling, which (per Round 2 review) caused
  label-less sapling workspaces to render "main" everywhere — violating
  the original requirement. v3 substitutes `"main"` only at the sapling
  backend boundary and leaves `Workspace.Branch == ""` in persisted state,
  letting the display helper naturally fall through to the workspace ID.
- **Display sites: 4 → 6.** Added `RepofeedPage.tsx:90` and
  `OverlayPage.tsx:586`.
- **Helper signature** now accepts an optional `computedBranch` arg so
  callers with remote-aware fallback logic compose cleanly.
- **Workspace-mode `workspace_label` semantics defined**: silently
  ignored. Documented rationale.
- **`/resume` and command-target paths addressed**: send `branch: ""` for
  sapling; do not surface label entry (per non-goals).
- **Defensive guard for line 864 (workspace-mode LLM suggester)** added
  with explicit reasoning.
- **`__new__` flow correctness documented**.
- **Implementation-order note** about step 4 depending on step 3 for full
  user-visible value.
