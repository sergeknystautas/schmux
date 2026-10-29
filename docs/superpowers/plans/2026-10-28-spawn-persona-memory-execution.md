# Execution report: Spawn Page Persona Memory

Plan: `docs/superpowers/plans/2026-10-28-spawn-persona-memory.md`
Spec: `docs/superpowers/specs/2026-10-28-spawn-persona-memory-design.md`
Branch: `schmux/flight/fl-f83a71e4`
Date: 2026-10-28

---

## What was built

The spawn page's Persona dropdown is now remembered exactly the way the model selection and the
target counts already were.

**Layer 2 — the per-tab draft.** `SpawnDraft` gained an optional `personaId` field
(`assets/dashboard/src/lib/spawn-draft.ts:20`). The persist effect writes the user's raw pick into
the draft on every change (`assets/dashboard/src/routes/SpawnPage.tsx:601-603`), so a draft holds the
persona, and a draft holding an explicit `''` restores "None" instead of falling through to
last-used.

**Layer 3 — long-term last-used memory.** A new `schmux:spawn-last-persona-id` localStorage key,
with `loadLastPersonaId`/`saveLastPersonaId` helpers matching the existing `loadLast*`/`saveLast*`
shape (`assets/dashboard/src/routes/SpawnPage.tsx:141-161`). Every spawn path that sends a persona
writes back the persona it actually sent: Engage (`:943`), `/resume` (`:778`), and the command
target (`:835`). `/quick` sends no persona and writes none.

**The waterfall.** The init effect resolves the persona once, outside the mode branches
(`assets/dashboard/src/routes/SpawnPage.tsx:427-430`):

```
setSelectedPersonaId(draft?.personaId ?? loadLastPersonaId() ?? '');
```

so it behaves identically in `fresh` and `workspace` mode.

**Only the shown persona is sent.** A derived `effectivePersonaId`
(`assets/dashboard/src/routes/SpawnPage.tsx:496-504`) is the value the page shows and sends. It is
`selectedPersonaId` only when the Persona dropdown renders and the loaded persona list contains that
id; otherwise it is `''`. All three spawn paths send `persona_id: effectivePersonaId || undefined`,
all three `<select data-testid="persona-select">` use `value={effectivePersonaId}`
(`:1221`, `:1374`, `:1627`), the draft keeps the raw `selectedPersonaId`, and last-used records
`effectivePersonaId`. A persona that was deleted, or a persona list that never loaded, therefore
shows "None" and sends no `persona_id` — the spawn cannot fail with `persona not found`.

**Docs.** `docs/sessions.md` records the persona field in Layer 2, the new Layer 3 key (plus the
previously missing `schmux:spawn-last-chat-enabled`), a `personaId` row in each of the three
field-initialization tables, the rule that only a shown persona is sent, and the persona write-back
under "On Successful Spawn".

**Review artifact.** `review/spawn-persona-memory.html` — one self-contained page listing each
scenario, its starting storage state, the user action, the expected dropdown value and
`persona_id` sent, the expected last-used key, and the test that proves it.

Style (`selectedStyleId`) was left untouched, as the spec required.

---

## Files touched

```
$ git status --short
 M assets/dashboard/src/lib/spawn-draft.test.ts
 M assets/dashboard/src/lib/spawn-draft.ts
 M assets/dashboard/src/routes/SpawnPage.tsx
 M docs/sessions.md
?? assets/dashboard/src/routes/SpawnPage.persona.test.tsx
?? docs/superpowers/plans/2026-10-28-spawn-persona-memory.md
?? docs/superpowers/specs/2026-10-28-spawn-persona-memory-design.md
?? review/spawn-persona-memory.html
```

The last two untracked files are the plan and spec themselves; they were already untracked when the
flight started.

---

## Deviations from the plan (rulings)

### Task 1 — the RED signal was the typecheck, not the test run

The plan's Task 1 Step 2 predicted this: "Vitest type-strips, so if the run passes anyway, note that
and continue." `./test.sh --quick` passed at runtime (5404 passed) with the new test in place,
because Vitest erases types rather than checking them. The failing signal was
`tsc --noEmit`:

```
src/lib/spawn-draft.test.ts(36,40): error TS2353: Object literal may only specify known properties, and 'personaId' does not exist in type 'SpawnDraft'.
src/lib/spawn-draft.test.ts(37,36): error TS2339: Property 'personaId' does not exist on type 'SpawnDraft'.
src/lib/spawn-draft.test.ts(38,38): error TS2353: Object literal may only specify known properties, and 'personaId' does not exist in type 'SpawnDraft'.
src/lib/spawn-draft.test.ts(39,34): error TS2339: Property 'personaId' does not exist on type 'SpawnDraft'.
```

Cost if wrong: the runtime assertion alone would not have proven the field exists; the typecheck
does, and the full `./test.sh` gate re-checks it.

### Task 2 — `./test.sh` does not typecheck in this repo

The plan's Step 7 says "`./test.sh` (the full suite, including typecheck)". It does not: the runner
exposes only the frontend, backend, E2E, and scenario suites
(`tools/test-runner/src/main.ts:123-125`), and typecheck lives in `./badcode.sh`.

`./badcode.sh` cannot run in this workspace — it exits 127 with "missing local deadcode / staticcheck
/ govulncheck / tsc" because `./scripts/bootstrap-badcode.sh` was never provisioned here. That is a
pre-existing workspace condition, not something this change caused.

**Decided:** run `./test.sh` exactly as the plan says, and close the typecheck gate with the same
invocation `badcode.sh` uses — `tsc --noEmit` in `assets/dashboard` (where every changed file
lives) and `tools/test-runner`:

```
assets/dashboard: ok
tools/test-runner: ok
tools/dev-runner: SKIPPED (no local tsc — not provisioned in this workspace)
test/scenarios/generated: SKIPPED (no local tsc — not provisioned in this workspace)
```

**Cost if wrong:** the two unprovisioned trees were not typechecked. Neither contains a file this
change touches.

### Task 2 — the workspace-mode test needs three personas, not two

The plan's fixture gives `spawn-draft-fresh` the persona `builder`, which is also the last-used
value. With that fixture the second phase ("no workspace draft") cannot distinguish _restored from
last-used_ from _leaked from the fresh draft_ — and pinning exactly that is what the plan's Review
Focus requires.

**Decided:** add a third persona (`auditor`) so the workspace draft (`reviewer`), the fresh draft
(`auditor`) and last-used (`builder`) are each distinguishable, and add the reverse-leak phase the
Review Focus names — a workspace draft must not leak into a fresh render either. The test now
asserts `reviewer` → `builder` → `builder` across the three renders.

**Cost if wrong:** none behaviorally — the assertions are the plan's, only the fixture is stronger.

### Task 2 — a draft-seeded test must not re-select the agent

The single-mode agent dropdown toggles rather than sets (`SpawnPage.tsx:609-618`), so calling
`selectAgent('claude')` after seeding a draft with `targetCounts: {claude: 1}` deselects the only
target and `validateForm` rejects the spawn.

**Decided:** in the tests that seed a draft with a target, enter only the branch. Same reason the
existing chat test re-enters only the branch on a re-render.

**Cost if wrong:** none.

### Task 2 — no scenario file under `test/scenarios/`

`docs/testing.md` ("Adding Tests", item 5) asks for a scenario file for user-facing features. The
plan defines the deliverables and the spec's acceptance criteria list only dashboard Vitest tests;
`test/scenarios/check-coverage.sh` nudges and does not block.

**Decided:** follow the plan; the gap is recorded here rather than silently filled.

**Cost if wrong:** the persona-memory flow has no Playwright-level regression. The dashboard Vitest
suite covers it at the unit level.

### Task 3 — no reviewer dispatched

The flight brief forbids subagents, and the plan's author checks the work against the plan. The
skill's final whole-branch review was therefore performed as a self-review against the package diff:
every changed file was read against the spec's Goals 1–5 and Anti-goals. No Critical or Important
finding; no deferred minors. A self-review by the author is weaker than a fresh reviewer.

### Commits

No commits were made — the flight brief assigns committing to a later phase. The plan's commit
steps, the reviewer dispatch, and `superpowers:finishing-a-development-branch` were all skipped as
instructed.

---

## Tests and checks run

| Check             | Command                                                               | Result                                                                                                                                                                                                                               |
| ----------------- | --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Task 1 RED        | `assets/dashboard/node_modules/.bin/tsc --noEmit`                     | 4 errors — `personaId` does not exist on `SpawnDraft`                                                                                                                                                                                |
| Task 1 GREEN      | `./test.sh --quick`                                                   | 5404 passed (Frontend 2309, Backend 3095), "All tests passed!"                                                                                                                                                                       |
| Task 1 typecheck  | `tsc --noEmit` (assets/dashboard)                                     | clean, 0 errors                                                                                                                                                                                                                      |
| Task 2 RED        | `./test.sh --quick`                                                   | 2314 passed, **7 failed** — the new restore, write-back and stale/hidden tests, each failing on the missing feature                                                                                                                  |
| Task 2 GREEN      | `./test.sh --quick`                                                   | 2321 passed, 0 failed                                                                                                                                                                                                                |
| Task 2 full suite | `./test.sh`                                                           | **5708 passed**, exit 0                                                                                                                                                                                                              |
| Typecheck         | `tsc --noEmit` in `assets/dashboard` and `tools/test-runner`          | both `ok`                                                                                                                                                                                                                            |
| Static analysis   | `./badcode.sh`                                                        | **could not run** — tooling not provisioned in this workspace (exit 127, "Run ./scripts/bootstrap-badcode.sh to provision badcode")                                                                                                  |
| Test rubric       | `.agents/skills/test-rules-review/scan.sh` on both changed test files | 25 `exception/framework-auto-wait` candidates (all compliant — RTL `findBy*`/`waitFor` per rubric rule 5) and 1 `polling` false positive (`waitForSession: vi.fn().mockResolvedValue(true)`, a mocked context value). No violations. |
| Formatting        | `npx prettier --write` on every changed file                          | all formatted; `docs/sessions.md` reported "unchanged" (the new table rows were already correctly aligned)                                                                                                                           |

### The full `./test.sh` output

```
$ ./test.sh

  ┌───────────┬────────┬──────────┬─────────────┐
  │ Suite     │ Status │ Duration │ Tests       │
  ├───────────┼────────┼──────────┼─────────────┤
  │ Frontend  │   ✓    │    24.0s │ 2321 passed │
  │ Backend   │   ✓    │     2.1s │ 3095 passed │
  │ E2e       │   ✓    │    25.4s │ 55 passed   │
  │ Scenarios │   ✓    │   1m 41s │ 237 passed  │
  ├───────────┼────────┼──────────┼─────────────┤
  │ Total     │        │   2m 32s │ 5708 passed │
  └───────────┴────────┴──────────┴─────────────┘

 All tests passed!
```

---

## Spec acceptance criteria

| #   | Criterion                                                                                                          | Satisfied by                                                                                                                                                                                                                                        |
| --- | ------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Draft restore: pick P, unmount, re-render in the same tab → `persona-select` shows P, engage sends `persona_id: P` | `restores the persona from the tab draft`                                                                                                                                                                                                           |
| 2   | Draft "None" wins: with last-used = P and `personaId: ''` in the draft → "None" restored, no `persona_id` sent     | `an explicit None in the draft beats last-used`                                                                                                                                                                                                     |
| 3   | Last-used after spawn: engage with P → `schmux:spawn-last-persona-id === P`; a new-tab render shows P              | `remembers the spawned persona for a new tab`                                                                                                                                                                                                       |
| 4   | Last-used "None": engage with None after P → the key becomes `''`, the next fresh render shows "None"              | `spawning with None records None`                                                                                                                                                                                                                   |
| 5   | Resume and command paths write back                                                                                | `/resume writes the last-used persona`, `command target writes the last-used persona`                                                                                                                                                               |
| 6   | Workspace mode: the persona is restored from that workspace's draft, and from last-used when no draft exists       | `workspace mode restores from its own draft, else last-used`                                                                                                                                                                                        |
| 7   | Stale ID not sent: last-used = `gone`, persona list lacks it → "None" shown, no `persona_id` sent                  | `a remembered persona that no longer exists shows None and is not sent`                                                                                                                                                                             |
| 8   | Personas off: `personas_enabled: false` → no dropdown, no `persona_id` sent                                        | `personas disabled: no dropdown, no persona sent`                                                                                                                                                                                                   |
| 9   | Fetch failure keeps the draft; and a failed fetch sends no persona                                                 | `a failed persona fetch keeps the draft pick`, `a failed persona fetch sends no persona`                                                                                                                                                            |
| 10  | Draft round-trip includes `personaId`                                                                              | `spawn-draft.test.ts` → `round-trips personaId, including an explicit empty "None"`                                                                                                                                                                 |
| 11  | Docs match code                                                                                                    | `docs/sessions.md` — Layer 2 fields line, Layer 3 key list (with `schmux:spawn-last-persona-id` and `schmux:spawn-last-chat-enabled`), a `personaId` row in each of the three field-initialization tables, and the "On Successful Spawn" write-back |
| 12  | The suite passes from the repository root, including typecheck                                                     | `./test.sh` → 5708 passed, exit 0; `tsc --noEmit` clean in `assets/dashboard` and `tools/test-runner`                                                                                                                                               |

Two extra guards beyond the criteria list, both from the plan's Review Focus: `a failed spawn does
not record last-used` (a rejected spawn must leave the key `null`) and the workspace test's
cross-mode leak phases.

---

## Deferred / unresolved

- **`./badcode.sh` did not run.** Its tooling (`deadcode`, `staticcheck`, `govulncheck`, `tsc` in
  `tools/dev-runner` and `test/scenarios/generated`) is not provisioned in this workspace. The
  typecheck half was covered manually with the same command; the Go half (deadcode, staticcheck,
  govulncheck) and `knip`/`npm audit` were not run. `./scripts/bootstrap-badcode.sh` followed by
  `./badcode.sh` closes it. **No Go file was changed**, so the Go analyzers have nothing new to
  inspect.
- **No scenario test** for the persona-memory flow (see the ruling above).
- **No commits.** The changed files are uncommitted in the worktree, as instructed.
- **Not verified against a real backend.** The persona list in these tests comes from a mocked
  `getPersonas`. The end-to-end behavior against a live daemon (a persona created, used, deleted,
  then spawned again) was not exercised — the Vitest suite covers the request payload and the
  dropdown state, not the daemon's `persona not found` rejection.
