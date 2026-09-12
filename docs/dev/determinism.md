# Finding non-deterministic tests

The goal is to detect backend tests whose result changes while the code remains
unchanged, and to identify which sampled runtime condition exposed the change.

Test-authoring rules live in [`docs/testing.md`](../testing.md); this guide covers the
sampling harness only and deliberately restates no rules.

For an ordinary repeat of the same test configuration, use the existing test
runner first:

```bash
./test.sh --backend --repeat 10
./test.sh --frontend --repeat 10
```

Frontend repeats run as N independent `vitest run` processes; per-test
results are aggregated from the JSON reporter output.

When a backend failure is hard to reproduce, use the determinism harness to run
fresh test processes under several runtime configurations:

```bash
./scripts/determinism.sh
```

The harness covers the untagged backend packages. It does not run the frontend,
E2E tests, scenarios, or the separate vendor-locked backend invocation.

## Configurations

Each sample is a separate `go test` process. That gives every shuffle sample a
new seed and prevents process-global state from carrying between samples.

| Configuration | Change from the base run                        |
| ------------- | ----------------------------------------------- |
| `base`        | No additional runtime flag                      |
| `cpu1`        | `-cpu=1`                                        |
| `cpu8`        | `-cpu=8`                                        |
| `shuffle`     | `-shuffle=on`, with a new seed for every sample |
| `race`        | `-race`; sampled once because of its cost       |
| `minpath`     | `PATH=/usr/bin:/bin:/usr/sbin:/sbin`            |

These knobs can expose order, scheduling, race, or host-dependency problems.
They do not control machine load, filesystem latency, or wall-clock timing, so a
clean result is evidence from the sampled runs rather than proof that the suite
contains no flakes.

## Verdicts

| Verdict            | Meaning                                                                                                                      |
| ------------------ | ---------------------------------------------------------------------------------------------------------------------------- |
| `FLAKY`            | The test both passed and failed in the same configuration.                                                                   |
| `CONFIG-SENSITIVE` | Each sampled configuration was internally consistent, but the test passed under some configurations and failed under others. |
| `ALWAYS-FAIL`      | Every observed non-skipped result failed.                                                                                    |
| `HOST-GATED`       | The test ran under some configurations and explicitly skipped under others.                                                  |

Build failures, setup failures, invalid package patterns, and unexplained
non-zero `go test` exits are execution errors. They are reported separately and
never converted into a clean result.

## Focused runs

Narrow the run to the package under investigation and raise the sample count:

```bash
./scripts/determinism.sh --pkg ./internal/dashboard/... --runs 25
./scripts/determinism.sh --pkg ./internal/config/... --runs 50 --configs base,shuffle
```

`--pkg` accepts one or more space-separated Go package patterns. `--runs`
applies to every selected configuration except `race`, which runs once.

## Output and exit status

Raw enriched `go test -json` streams, preserved stderr, and `verdict.tsv` are
written to `.schmux/determinism/` by default. Use `--out` to choose another
directory.

| Exit  | Meaning                                           |
| ----- | ------------------------------------------------- |
| `0`   | No result variation was observed.                 |
| `1`   | At least one test needs attention.                |
| `2`   | A requested run could not execute or be analyzed. |
| `130` | The harness was interrupted.                      |

If interrupted, the completed raw streams remain available. The active `go
test` process group is recorded in `RUNNING.pgid` while a sample is running.

## Detector contract (`--verify-detector`)

Both harnesses self-test their classification against synthetic fixtures that
never run in a normal gate:

```bash
./scripts/determinism.sh --verify-detector   # backend: exact verdict.tsv rows
./test.sh --verify-detector                  # frontend: real vitest repeats
```

Fixtures live under `test/detector-fixtures/` — the Go package inside
`testdata/` (invisible to `./...`) and a vitest file outside the dashboard's
discovery root. Outcomes are controlled by environment variables, never
randomness:

| Variable                | Meaning                               |
| ----------------------- | ------------------------------------- |
| `DETECTOR_SAMPLE_INDEX` | 1-based sample number within a config |
| `DETECTOR_CONFIG`       | configuration being sampled           |

The detector must exit 1 (variation found) and produce exactly the contract
verdicts — FLAKY for the alternating fixture, CONFIG-SENSITIVE for the
cpu1-bound fixture, nothing for the stable control. Exit 0, exit 2, missing
samples, or any other verdict fails verification.

## Scheduled sampling

`.github/workflows/determinism.yml` runs nightly (`17 6 * * *` UTC) and on
demand (Actions → Determinism Sampling → Run workflow; `runs` sets samples
per configuration, `suite` narrows to one scope). It samples backend
configurations, race, frontend repeats, and serial E2E/scenario repeats.
Every producer uploads raw evidence unconditionally; the `summary` job
classifies each producer as clean, findings, or incomplete execution —
findings and incomplete execution both fail the run. Reproduce any shuffled
backend order locally with the `repro.txt` lines in the backend artifacts.

### Workflow shape

Configurations stay serial inside one backend invocation so cross-configuration
classification is computed locally (the harness already does this). Race is
isolated because of its cost. E2E and scenarios share one runner with serial
steps and `if: always()` between them — Docker evidence from both suites is
cheap to collect sequentially, and a scenario step running with `if: always()`
after an E2E infrastructure failure keeps scenario evidence from being lost.
Fully parallel per-suite jobs would spend more runners for little signal; a
single all-suite job would be unnecessarily long and one early infrastructure
failure would suppress every later scope's evidence.

### Producer-job contract

Every producer job (everything except `summary`) follows the same shape:

- Multi-step jobs (`detector-contract`, `frontend`, `docker-suites`) run each
  suite step with `if: always()` — an earlier step's failure must not prevent
  later steps from collecting evidence.
- Capture the suite's exit code into the artifact directory before exiting
  (`echo "$rc" > .schmux/.../<producer>.exit-code.txt`), so the summary job
  can distinguish exit 1 (findings) from exit 2 (incomplete execution) after
  the fact.
- Upload artifacts `if: always()` with 7-day retention, one artifact per job.
  Detector-contract raw streams upload separately from real corpus results.
- Write a `$GITHUB_STEP_SUMMARY` section: verdict tables or flaky counts,
  sample counts, configuration/seed metadata, tool versions, and repro
  commands.

The `summary` job has `needs: [...]` with `if: always()`, downloads every
artifact, and classifies each producer as **clean**, **findings**, or
**incomplete execution** using the captured exit codes plus `verdict.tsv` /
`<suite>-repeat.json` contents. It writes one consolidated summary table
and exits 1 when any producer needs attention — findings and incomplete
execution both fail the workflow, but the summary names which. This is the
single place a nightly failure is read from.

> **Note on `--vitest-shuffle-seed`:** Vitest 4.1.8 advertises
> `--sequence.shuffle --sequence.seed=N` for reproducible order. The seed is
> honored only when vitest discovers files via explicit positional args; the
> default full-tree discovery feeds the shuffle a non-deterministic input
> order, so different runs with the same seed still produce different file
> orders. The flag is still accepted (and the seed recorded in
> `<suite>-repeat.json`) for runs that pass an explicit file list, but the
> scheduled workflow does not rely on reproducibility for the frontend
> shuffled step — that step is intentionally omitted from the workflow
> until vitest's discovery becomes stable.
