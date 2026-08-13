# Finding non-deterministic tests

The goal is to detect backend tests whose result changes while the code remains
unchanged, and to identify which sampled runtime condition exposed the change.

For an ordinary repeat of the same test configuration, use the existing test
runner first:

```bash
./test.sh --backend --repeat 10
./test.sh --frontend --repeat 10
```

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
