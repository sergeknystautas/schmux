# Spawn Form In-Flight Lock

**Date:** 2026-08-28
**Status:** Approved design, not yet implemented

## Problem

When the user presses Engage on the spawn form, only the Engage button, the
fence toggle, and the create-branch checkbox disable. Every other control —
prompt, repo, branch, agents, persona, style, remote host, share-intent —
stays editable while the client runs the spawn sequence (optional
`POST /api/suggest-branch`, then `POST /api/spawn`, then waiting for the
session to appear over WebSocket). Edits during this window change nothing
about the in-flight request, so the live form gives a false impression of
control.

Worse, the in-progress state lives in component-local state
(`engagePhase` in `SpawnPage.tsx`). Navigating away and back remounts the
form as idle, so the user can submit a duplicate spawn while the first
request is still running.

## Goals

1. While a spawn is in flight, disable every editable control on that spawn
   form.
2. Persist the in-flight state at app level, so leaving the spawn page and
   returning shows the same locked, in-progress form.
3. Leave room for a future Cancel control; do not build it now.

## Non-Goals

- **Cancellation.** No Cancel button and no backend cancel API in this
  change. The design reserves a place for both (see Future Work).
- **Surviving page reload.** A full reload drops the browser's in-flight
  response, so the client can never learn the request's outcome. The lock
  resets on reload. Reload-survival requires backend spawn jobs (Future
  Work).
- **Global locking.** A spawn in workspace A must not block spawning into
  workspace B or into a fresh workspace. The lock is per form.

## Design

### 1. In-flight store (`assets/dashboard/src/lib/spawn-inflight.ts`)

A module-level store, read from React via `useSyncExternalStore`, holding a
map keyed by **form key** — `workspaceId || 'fresh'`, the same key scheme
`SpawnDraft` uses. Each entry:

```ts
interface SpawnInflightEntry {
  phase: 'naming' | 'spawning' | 'waiting';
  error?: string; // set on failure; consumed once by the form
  failedPhase?: 'naming' | 'spawning'; // which step failed, so the form
  // reacts correctly even after a remount (naming → reveal branch input)
  navTarget?: { type: 'session' | 'workspace'; id: string }; // set on success
  spawnedSessionIds?: string[]; // session ids returned by POST /api/spawn;
  // the waiting phase clears when one of these appears in dashboard data
}
```

No entry for a key means that form is idle. The store exposes:

- `useSpawnInflight(formKey)` — subscribe to one entry.
- `startSpawn(formKey, …)` — run the spawn sequence (section 2).
- `consumeError(formKey)` — return and clear a stored error, resetting the
  entry to idle.
- `clearIfLanded(formKey, sessions)` — clear a `waiting` entry once one of
  its `spawnedSessionIds` exists in dashboard data (section 3).

### 2. Spawn execution moves into the store

`startSpawn` owns the sequence that `handleEngage`, the slash-command
handler, and the quick-launch handler currently run inline:

1. Set phase `naming` and call `POST /api/suggest-branch` when the flow
   needs a branch name; otherwise skip to step 2.
2. Set phase `spawning` and call `POST /api/spawn` with the prepared
   request.
3. On success: clear the sessionStorage draft for this form key, persist
   the last-used repo/targets/mode, set `pendingNavigation` in
   `SessionsContext`, expand the target workspaces in the sidebar, record
   `navTarget` and `spawnedSessionIds` from the `SpawnResult[]` response,
   and set phase `waiting`.

   `pendingNavigation` remains a single slot: when spawns from different
   form keys are concurrently in flight, the last-completed spawn wins
   navigation. That is today's behavior and this design keeps it; lock
   correctness never depends on it, because each form's lock clears off
   its own `spawnedSessionIds` (section 3). Improving navigation
   arbitration is a `SessionsContext` concern, out of scope here.

4. On failure: store the error message and `failedPhase` in the entry. A
   mounted form surfaces it immediately; an unmounted form surfaces it on
   remount (section 5). Consuming the error resets the entry to idle. The
   draft survives, so the user can retry.

Because the store outlives any component, the sequence completes and
navigation fires even when the user has left the spawn page —
`pendingNavigation` already lives in `SessionsContext`, which is app-level.

`SpawnPage` handlers shrink to: validate, build the request, call
`startSpawn`. The reaction to a branch-suggestion failure stays in the
component: `startSpawn` records the failure with `failedPhase: 'naming'`,
and the form — whether it stayed mounted or is remounting — reveals the
branch input alongside the error, as it does today.

The draft helpers (`loadSpawnDraft`, `saveSpawnDraft`, `clearSpawnDraft`)
move from `SpawnPage.tsx` into a shared module (e.g.
`assets/dashboard/src/lib/spawn-draft.ts`) so that draft persistence has
one owner: the form saves and loads through it, and `startSpawn` clears
through it on success.

`SessionsContext` and `usePendingNavigation` are unchanged.

### 3. Ending the `waiting` phase

A `waiting` entry clears when one of its `spawnedSessionIds` appears in
`SessionsContext` data. Keying off the ids returned by `POST /api/spawn` —
rather than "the target workspace has sessions" — matters when spawning
into a workspace that already has sessions: pre-existing sessions must not
clear the lock early. Normally the user has already been navigated to the
new session by then. The
`useSpawnInflight` hook performs this check against `SessionsContext` data,
so no store→context coupling is added. No timeout in v1: a spawn that
errors takes the error path, and a daemon that never delivers the session
is a daemon bug, not a form-lock bug.

### 4. Full form disable

`SpawnPage` derives one flag:

```ts
const formDisabled = inflight !== undefined; // any phase
```

and threads `disabled={formDisabled}` to every control that edits spawn
request state:

- Prompt: `PromptTextarea` gains a `disabled` prop that disables the
  textarea, autocomplete, slash commands, and image attachment.
- Repo select and the new-repo name input.
- Branch input and sapling workspace-label input. (Nickname is component
  state with no input control today; nothing to disable.)
- Agent selection in all three modes: the single-mode selector, the
  multiple-mode toggle grid, the advanced-mode +/− counters, and the
  buttons that switch between modes.
- Persona and style selects (both the single-mode and multi-mode variants).
- `RemoteHostSelector` (already accepts `disabled`).
- All three option checkboxes: create-branch and fence (already wired),
  share-intent (currently missing).
- Spawn-entry and quick-launch items that fill or submit the form.
- The Engage button (already wired), which keeps its existing per-phase
  spinner labels: "Naming branch…", "Spawning…", "Downloading session…".

Navigation stays enabled: session tabs, the sidebar, and links all work.
The user may leave; they just cannot edit this form.

Visual treatment uses the design system's native disabled styling for
`.input`/`.select`/`.btn` plus the existing Engage spinner — no new visual
language. Custom non-form controls (agent grid buttons, remote host cards,
spawn-entry items) follow the style guide's disabled treatment.

### 5. Returning mid-spawn

On mount, `SpawnPage` reads the store for its form key:

- Phase present → render the form disabled with the matching Engage
  spinner label.
- Stored error present → surface it once through the existing paths (tmux
  banner for tmux errors, modal alert otherwise) via `consumeError`, then
  render the form idle with the draft intact.

### 6. Cancel (future, designed-for)

The entry is the natural home for cancellation state: an `AbortController`
for the client-side fetch today, a backend job id later. The Engage button
area is where a Cancel control will sit. Neither ships in this change.

## Future Work

- **Backend spawn jobs**: `POST /api/spawn` returns a job id immediately;
  progress streams over the dashboard WebSocket; a cancel endpoint aborts
  the job and cleans up partially created workspaces/sessions. This
  unlocks reload-survival, per-target progress, and real cancellation.
- **Cancel button** on the spawn form, once jobs exist.

## Testing

Vitest + React Testing Library, run via `./test.sh`:

- **Store unit tests**: phase transitions for success and failure paths;
  per-key isolation (an entry for workspace A never affects key B);
  `consumeError` returns the error once and resets to idle;
  `clearIfLanded` clears only when a spawned session id appears — and not
  when the target workspace merely has pre-existing sessions.
- **SpawnPage tests**: every editable control carries `disabled` during
  each phase; remounting with an in-flight entry renders the locked form
  with the right spinner label; remounting with a stored error surfaces it
  once and unlocks with the draft intact; a second form key renders
  enabled while the first is in flight; slash-command and quick-launch
  handlers refuse to start while locked.
