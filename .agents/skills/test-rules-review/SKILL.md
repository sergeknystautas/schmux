---
name: test-rules-review
description: Read-only audit of new or changed test files against docs/testing.md, the sole test-authoring rubric. Use after writing or modifying any test, before claiming the work done or committing. Reports file:line findings and an explicit verdict; never edits code.
---

# Test Rules Review

Read-only audit of test changes against the testing rubric. Report findings;
never edit code, docs, or specs. Every fix direction must preserve the
behavioral claim the test was written to prove.

The rubric is `docs/testing.md` — the sole source of test-authoring rules.
Read it fresh on every run and apply whatever it currently says, citing rules
by number. Never restate the rules from memory and never invent rules.

All commands run from the repository root.

## Workflow

1. Read `docs/testing.md` in full.
2. Build the scope:
   - default: `.agents/skills/test-rules-review/scan.sh --net --changed`
   - `file <path>`: the given file, read end-to-end alongside the rubric
   - `repo` (migration mode, initial backlog only):
     `.agents/skills/test-rules-review/scan.sh --net`
3. If the default-mode scope is empty, report `no changed test files` and
   stop. An empty scope is not a compliant result.
4. Scan the scoped files (working-tree content):
   `.agents/skills/test-rules-review/scan.sh <files...>`
5. Snapshot check. For scoped files that differ between the index and the
   working tree (`git diff --name-only -- <files...>`), also run
   `.agents/skills/test-rules-review/scan.sh --index <file>` per file. Tag
   findings `staged:` or `worktree:` (untagged means the snapshots agree).
   A violation found in either snapshot counts toward the verdict: a staged
   sleep with an unstaged fix is a violation until the fix is staged.
6. Judge every candidate (Judgment rules below). A candidate is never a
   violation without a stated reason, and never compliant without a stated
   reason. Every `exception/…` candidate must receive an explicit
   disposition in the report.
7. Apply the non-mechanical checks to every changed file.
8. Attribute findings, then produce the report and verdict.

## Judgment rules

- Deadline backstops are not sleeps. A `time.After`/`setTimeout` raced
  against an awaited event that fails the test on expiry is the required
  failure backstop (rubric rule 2). A timer that resolves into an assertion
  is a sleep.
- Elapsed time as the claim passes. A negative-claim observation window or
  a product timing claim measured over one window whose duration is the
  claim is a real observation (rules 3-4) — provided the reason is stated
  adjacent to the wait. Missing adjacent reason: warning.
- Timing and ordering claims are valid. Distinguish authored product
  behavior (phase, duration, ordering, interruption, completion) from
  runner-performance gates. If a valid timing test uses a sleep or poll to
  arrange its state, report the synchronization mechanism and direct the
  fix toward a controllable clock, a completion event, or a semantic
  boundary (rules 3, 7). Never recommend deleting, weakening, or
  reclassifying the claim.
- Framework auto-waits are judged by what they await. Playwright locator
  assertions, `expect.poll` on debounced API state, and RTL `findBy*`/
  `waitFor` awaiting an eventual UI state are allowed (rule 5). Using them
  to retry a result after the system reports completion violates rule 7.
- Centralized probes pass; duplicated loops do not. `waitForHealthy`
  (daemon health) and `waitForShellPrompt` (shell readiness) are the
  canonical centralized external-process probes (rule 6): one helper,
  deadline, interval, last observation, failure diagnostics. A test
  embedding its own probe loop is a violation even though the helpers are
  not. Until the render-completion API lands, the shared terminal sentinel
  wait is a documented bounded-probe exception — not a pattern to copy.
- Performance code is judged by placement. Perf-shaped assertions are
  violations only when a CI-routed gate executes them (rule 8). Fix
  direction: route the measurement through `./test.sh --bench` or
  `--microbench`; never delete the measurement.
- Docker stale-base-image rebuilds are dependency setup, not assertion
  retries — not violations.
- String-literal hits are not code under test (fixture HTML, embedded JS).
- Retries for external downloads are dependency setup; retrying an
  assertion is the violation.

## Non-mechanical checks

For each changed test file, judge these against the current rubric, stating
the conclusion per file:

- Lowest-capable gate — could a lower gate make the same deterministic
  assertion (rule 10)? Cite the gate table.
- Routed by a gate — which gate executes this file? Cross-check
  `tools/test-runner/src/main.ts` and `.github/workflows/*.yml` against
  the gate table. A changed test file no gate reaches is a finding
  (rule 11). This check consumes gate routing; it does not redefine it.
- Claim preservation — for every finding, does the fix direction preserve
  the behavior the test proves? A proposed fix that removes an assertion
  instead of making it deterministic is itself an invalid review outcome.

## Attribution

Attribute default-mode findings only to lines the branch actually touched
(check `git diff "$(git merge-base HEAD main)" -- <file>`). A new test file
attributes all findings to the branch. Pre-existing drift elsewhere in a
touched file is info, not a branch finding. `repo`-mode findings are all
labeled `migration`, grouped by cause (assertion retries, performance
gates, missing event/clock seam, ambient shared state, missing cleanup,
legitimate documented exceptions).

The verdict counts violations introduced within the reviewed scope:
default mode counts branch-introduced violations; `file` mode counts all
violations in the audited file; `repo` mode counts all findings (labeled
`migration`). `no changed test files` applies to default mode only.

## Report

Terminal report, ranked error, then warning, then info. Each finding cites
`file:line`, the rubric rule number, the behavioral claim preserved, and a
one-line fix direction toward the missing clock/event/fixture/gate.
Divergent-snapshot findings carry their `staged:`/`worktree:` tag so the
user knows which content to fix (for `staged:`, the fix must be re-staged).
Judged candidates — including every `exception/…` disposition — are listed
separately with reasoning. End with an explicit verdict line:

- compliant — no error or warning findings
- violations (N) — N = error + warning count
- no changed test files — empty default-mode scope (not compliant)

On violations (N): do not commit and do not claim the work done. Fix the
tests, or — if a finding is wrong — surface the conflict to the user. Never
silently override a verdict. Do not apply fixes; edits happen only when the
user asks afterward.
