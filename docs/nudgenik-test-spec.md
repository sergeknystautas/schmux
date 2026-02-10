# NudgeNik Tests (Current)

## Source of Truth

Test cases live in:

- `internal/tmux/testdata/manifest.yaml`

Each case includes:

- `capture`: raw terminal capture filename
- `want_state`: expected NudgeNik state
- `notes`: commentary for review

Both the extractor tests and the NudgeNik integration test read from this manifest.

## Files Involved

- `internal/tmux/tmux.go` - ExtractLatestResponse
- `internal/tmux/tmux_test.go` - Extraction tests (driven by manifest)
- `internal/tmux/testdata/*.txt` - Raw terminal captures
- `internal/tmux/testdata/*.want.txt` - Expected extraction output
- `internal/nudgenik/integration_test.go` - NudgeNik integration test (driven by manifest)

## Adding a New Case

1. Capture terminal output:
   ```bash
   tmux capture-pane -e -p -S -100 -t "session name" > internal/tmux/testdata/claude-01.txt
   ```
2. Add the case to `internal/tmux/testdata/manifest.yaml` (capture, expected state, notes).
3. Generate the `.want.txt`:
   ```bash
   UPDATE_GOLDEN=1 go test -v -run TestUpdateGoldenFiles ./internal/tmux/...
   ```
4. Review diffs for correctness.

## Running Tests

Extractor tests:

```bash
go test ./internal/tmux/...
```

NudgeNik integration tests:

```bash
go test -tags=integration ./internal/nudgenik
```

### Configuration

- `NUDGENIK_PASS_K` (default: 3): number of repeated runs per capture (pass^k).

### Parallel execution

Fixtures run in parallel. Use `-parallel N` to cap concurrency:

```bash
go test -tags=integration -parallel 8 ./internal/nudgenik
```

### Output summary

The integration test prints a summary table at the end:

```
Nudgenik classification summary:
FILE         WANT                       GOT                                      STATUS
claude-01.txt  Needs Feature Clarification Needs User Testing x2, Completed x1    FAIL
```
