# Terminal Capture Test Data

Raw terminal captures from coding agents (Claude, Codex) used to test the `ExtractLatestResponse` function.

## File Structure

- `manifest.yaml` - Source of truth for test cases (capture file, expected NudgeNik state, notes)
- `*.txt` - Raw terminal captures (input)
- `*.want.txt` - Expected extraction output (golden files)

## Capturing New Test Data

Figure out which terminal session you need:

```bash
tmux ls
```

Capture the session to a file:

```bash
tmux capture-pane -e -p -S -100 -t "session name" > internal/tmux/testdata/claude-01.txt
```

## Naming Convention

Files are named by agent type followed by an incremental number:

- `claude-01.txt`, `claude-02.txt`, ...
- `codex-01.txt`, `codex-02.txt`, ...

## Adding New Test Cases

1. Capture terminal output (see above)

2. Add the case to `manifest.yaml` (capture file, expected NudgeNik state, notes).

3. Generate the `.want.txt` file:

   ```bash
   UPDATE_GOLDEN=1 go test -v -run TestUpdateGoldenFiles ./internal/tmux/...
   ```

4. Review the generated `.want.txt` to ensure the extraction is correct.

## Regenerating Golden Files

If the extractor logic changes, regenerate all golden files:

```bash
UPDATE_GOLDEN=1 go test -v -run TestUpdateGoldenFiles ./internal/tmux/...
```

Then review the diffs to ensure changes are expected.

## Test Fixtures

See `manifest.yaml` for the full list of captures, expected states, and notes.

## How Extraction Works

The `ExtractLatestResponse` function:

1. Finds the last prompt line (`❯` or `›`)
2. Collects non-empty content lines going backwards (up to 80 lines)
3. Appends any choice menu lines after the prompt

The extracted content is sent to NudgeNik for agent state classification.

## NudgeNik Integration Tests

The integration test (`internal/nudgenik/integration_test.go`) reads cases from `manifest.yaml`.

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
