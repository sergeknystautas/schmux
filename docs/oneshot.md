# Oneshot Architecture

Oneshot is schmux's prompt-in/result-out execution mode for AI agents. It's used internally — not user-facing — for tasks where schmux needs a structured answer from an LLM.

## Consumers

| Consumer         | Package                    | Schema             | What it does                                                      |
| ---------------- | -------------------------- | ------------------ | ----------------------------------------------------------------- |
| NudgeNik         | `internal/nudgenik`        | `nudgenik`         | Classifies agent terminal state (stuck, waiting, completed, etc.) |
| Branch Suggest   | `internal/branchsuggest`   | `branch-suggest`   | Generates a git branch name and nickname from a user prompt       |
| Conflict Resolve | `internal/conflictresolve` | `conflict-resolve` | Resolves git rebase conflicts via LLM, reports actions per file   |

## Execution Flow

```
Caller (e.g. nudgenik.AskForExtracted)
  │
  ├─ passes schema label (e.g. oneshot.SchemaNudgeNik)
  │
  ▼
oneshot.ExecuteTarget(ctx, cfg, targetName, prompt, schemaLabel, timeout, dir)
  │
  ├─ resolveTarget: looks up config target, resolves model/secrets/env
  ├─ user-defined targets → ExecuteCommand (no schema support)
  ├─ detected tools → Execute
  │
  ▼
oneshot.Execute(ctx, agentName, agentCommand, prompt, schemaLabel, env, dir)
  │
  ├─ resolveSchema(label) → file path (~/.schmux/schemas/<label>.json)
  │   ├─ Claude: reads file, passes content inline (--json-schema <json>)
  │   └─ Codex: passes file path directly (--output-schema <path>)
  │
  ├─ detect.BuildCommandParts: builds CLI args for the agent
  ├─ exec.CommandContext: runs the agent process
  │
  ▼
parseResponse(agentName, rawOutput)
  ├─ Claude: JSON envelope → extracts "structured_output" field
  └─ Codex: JSONL stream → extracts last "item.completed" agent_message
```

### Schema Files

Schemas are written to `~/.schmux/schemas/` as JSON files. `WriteAllSchemas()` runs on daemon startup to ensure they're current. At execution time, `resolveSchema` returns a file path — the caller reads the file for Claude (inline arg) or passes the path for Codex.

This keeps the interface consistent: every agent gets its schema from a file.

### CLI Arguments by Agent

| Agent  | Oneshot flag              | Schema flag       | Schema value                 |
| ------ | ------------------------- | ----------------- | ---------------------------- |
| Claude | `-p --output-format json` | `--json-schema`   | Inline JSON (read from file) |
| Codex  | `exec --json`             | `--output-schema` | File path                    |

## Schema Registry

All schemas live in `oneshot.go` in a central registry:

```go
const (
    SchemaConflictResolve = "conflict-resolve"
    SchemaNudgeNik        = "nudgenik"
    SchemaBranchSuggest   = "branch-suggest"
)

var schemaRegistry = map[string]string{
    SchemaConflictResolve: `{...}`,
    SchemaNudgeNik:        `{...}`,
    SchemaBranchSuggest:   `{...}`,
}
```

Callers reference labels, never raw JSON.

### Adding a New Schema

1. Add a label constant and JSON string to `schemaRegistry` in `oneshot.go`
2. `TestSchemaRegistry` will automatically validate it (required fields, OpenAI structured output constraints)
3. Use the label in your consumer's `ExecuteTarget` call

### Validation

`TestSchemaRegistry` walks every registered schema and checks:

- All `required` keys exist in `properties`
- When `additionalProperties` is a schema object, `properties` is explicitly defined (OpenAI requirement)
- Recursive validation of nested objects

## Integration Testing

NudgeNik has a manifest-driven integration test that runs real oneshot calls against captured terminal output. This is the pattern to follow for testing other consumers.

### Test Corpus Structure

```
internal/tmux/testdata/
├── manifest.yaml          # Source of truth: capture file → expected state
├── claude-01.txt          # Raw terminal capture (input)
├── claude-01.want.txt     # Expected extraction output (golden file)
├── codex-01.txt
├── codex-01.want.txt
└── ...
```

The manifest defines each case:

```yaml
cases:
  - id: claude-01
    capture: claude-01.txt
    want_state: Needs Feature Clarification
    notes: After claude compacts things
```

### Capturing New Test Data

1. Find the tmux session:

   ```bash
   tmux ls
   ```

2. Capture terminal output:

   ```bash
   tmux capture-pane -e -p -S -100 -t "session name" > internal/tmux/testdata/claude-16.txt
   ```

3. Add the case to `manifest.yaml` with the expected state and notes.

4. Generate the `.want.txt` golden file:

   ```bash
   UPDATE_GOLDEN=1 go test -v -run TestUpdateGoldenFiles ./internal/tmux/...
   ```

5. Review the generated `.want.txt` to ensure the extraction is correct.

### Running Integration Tests

Basic run (uses `nudgenik_target` from `~/.schmux/config.json`):

```bash
go test -tags=integration ./internal/nudgenik
```

Verbose output with summary table:

```bash
go test -tags=integration -v ./internal/nudgenik
```

Control concurrency:

```bash
go test -tags=integration -parallel 4 ./internal/nudgenik
```

Override pass^k runs (default 3):

```bash
NUDGENIK_PASS_K=5 go test -tags=integration ./internal/nudgenik
```

### Testing Against Multiple Agents

Change `nudgenik_target` in `~/.schmux/config.json` to point at different models, then run the same suite:

```bash
# Test with Claude Sonnet
# config.json: "nudgenik_target": "claude-sonnet"
go test -tags=integration -v ./internal/nudgenik

# Test with Claude Haiku
# config.json: "nudgenik_target": "claude-haiku"
go test -tags=integration -v ./internal/nudgenik

# Test with Codex
# config.json: "nudgenik_target": "codex"
go test -tags=integration -v ./internal/nudgenik
```

The summary table makes it easy to compare accuracy across agents:

```
Nudgenik classification summary:
FILE              WANT                        GOT                           STATUS
claude-01.txt     Needs Feature Clarification Needs Feature Clarification x3 PASS
claude-05.txt     Completed                   Completed x3                   PASS
codex-01.txt      Needs User Testing          Needs User Testing x2, Completed x1  FAIL
```

### Pass^k Testing

Integration tests use pass^k (default k=3) to reduce variance from nondeterministic LLM responses. A case passes only if all k runs return the expected state. This catches flaky classifications that might pass on a single run.

See: [Demystifying Evals for AI Agents](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents)

## Extending to Other Consumers

### Branch Suggest

Straightforward to test with this pattern. Input is a user prompt string, output is JSON with `branch` and `nickname`. A manifest would map prompts to expected branch names/patterns.

### Conflict Resolve

Harder — requires a real git repo with actual merge conflicts as fixtures. The test would need to:

1. Set up a repo with known conflicts
2. Run the oneshot call with `dir` set to the repo
3. Verify the LLM resolved the files correctly and returned accurate JSON

This likely needs purpose-built fixtures rather than terminal captures.
