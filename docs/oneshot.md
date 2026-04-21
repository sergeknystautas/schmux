# Oneshot Architecture

Oneshot is schmux's prompt-in/result-out execution mode for AI agents. It's used internally — not user-facing — for tasks where schmux needs a structured answer from an LLM.

## Consumers

Each schema label is declared in `internal/schema/schema.go` and registered via `schema.Register(label, StructType{})` in the consumer package's `init()`. The authoritative list lives in `schema.go`; the table below mirrors it.

| Consumer                | Package                                  | Schema label            | Response struct           | What it does                                                                        |
| ----------------------- | ---------------------------------------- | ----------------------- | ------------------------- | ----------------------------------------------------------------------------------- |
| NudgeNik                | `internal/nudgenik`                      | `nudgenik`              | `Result`                  | Classifies agent terminal state (stuck, waiting, completed, etc.)                   |
| Branch Suggest          | `internal/branchsuggest`                 | `branch-suggest`        | `Result`                  | Generates a git branch name from a user prompt                                      |
| Commit Message          | `internal/dashboard/commit.go`           | `commit-message`        | `commitmessage.Result`    | Generates commit messages from workspace diffs                                      |
| Conflict Resolve        | `internal/conflictresolve`               | `conflict-resolve`      | `OneshotResult`           | Resolves git rebase conflicts via LLM, reports actions per file                     |
| Subreddit (bootstrap)   | `internal/subreddit`                     | `subreddit-bootstrap`   | `BootstrapResult`         | Generates initial subreddit digest posts from a commit batch                        |
| Subreddit (incremental) | `internal/subreddit`                     | `subreddit-incremental` | `IncrementalResult`       | Updates subreddit digest posts as new commits arrive                                |
| Repofeed intent         | `internal/repofeed` (called from daemon) | `repofeed-intent`       | `IntentSummary`           | Summarizes a workspace's current intent in one sentence                             |
| Autolearn (intent)      | `internal/autolearn`                     | `autolearn-intent`      | `IntentCuratorResponse`   | Classifies session intent for autolearn curation                                    |
| Autolearn (friction)    | `internal/autolearn`                     | `autolearn-friction`    | `FrictionCuratorResponse` | Extracts learnings from friction data in agent sessions                             |
| Autolearn (merge)       | `internal/autolearn`                     | `autolearn-merge`       | `MergeCuratorResponse`    | Merges approved rules into an existing instruction file (e.g. CLAUDE.md)            |
| Compound merge          | `internal/compound`                      | `compound-merge`        | `MergeResult`             | LLM-merges two divergent overlay-file versions (e.g. `.claude/settings.local.json`) |

## Execution Flow

```
Caller (e.g. nudgenik.AskForExtracted)
  │
  ├─ passes schema label (e.g. schema.LabelNudgeNik)
  │
  ▼
oneshot.ExecuteTarget[T any](ctx, cfg, targetName, prompt, schemaLabel, timeout, dir) (T, error)
  │
  ├─ resolveTarget: looks up config target, resolves model/secrets/env
  ├─ user-defined targets → ExecuteCommand (no schema support)
  ├─ detected tools → Execute
  │
  ▼
oneshot.Execute(ctx, agentName, agentCommand, prompt, schemaLabel, env, dir, model)
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

Schemas are generated at runtime from Go struct definitions using `github.com/swaggest/jsonschema-go` via the `internal/schema` package. Each consumer registers its schema via `init()` (e.g., `schema.Register(schema.LabelNudgeNik, Result{}, "source")`). `WriteAllSchemas()` runs on daemon startup to write them to `~/.schmux/schemas/` as JSON files. At execution time, `resolveSchema` returns a file path — the caller reads the file for Claude (inline arg) or passes the path for Codex.

### CLI Arguments by Agent

| Agent  | Oneshot flag              | Schema flag       | Schema value                 |
| ------ | ------------------------- | ----------------- | ---------------------------- |
| Claude | `-p --output-format json` | `--json-schema`   | Inline JSON (read from file) |
| Codex  | `exec --json`             | `--output-schema` | File path                    |

## Schema Registry

Schemas are generated at runtime from Go struct definitions. There is no hand-written JSON anywhere — struct tags drive the shape, and `github.com/swaggest/jsonschema-go` produces OpenAI-compatible JSON schema at first access.

**Labels** are string constants in `internal/schema/schema.go`:

```go
const (
    LabelCommitMessage        = "commit-message"
    LabelConflictResolve      = "conflict-resolve"
    LabelNudgeNik             = "nudgenik"
    LabelBranchSuggest        = "branch-suggest"
    LabelSubredditBootstrap   = "subreddit-bootstrap"
    LabelSubredditIncremental = "subreddit-incremental"
    LabelAutolearnIntent      = "autolearn-intent"
    LabelAutolearnFriction    = "autolearn-friction"
    LabelAutolearnMerge       = "autolearn-merge"
    LabelRepofeedIntent       = "repofeed-intent"
    LabelCompoundMerge        = "compound-merge"
)
```

**Registration** happens in each consumer package's `init()`:

```go
// internal/branchsuggest/branchsuggest.go
func init() {
    schema.Register(schema.LabelBranchSuggest, Result{})
}
```

`schema.Register(label, v, skipFields...)` stores the type; `schema.Get(label)` generates and caches the JSON schema on first call. `WriteAllSchemas()` runs on daemon startup to write every registered schema to `~/.schmux/schemas/<label>.json`.

**Struct tags** control schema shape:

| Tag                            | Effect                                                   |
| ------------------------------ | -------------------------------------------------------- |
| `json:"field_name"`            | JSON property name                                       |
| `required:"true"`              | Marks the field as required in the schema                |
| `nullable:"false"`             | Forbids null for array/map fields                        |
| `additionalProperties:"false"` | On an unnamed `struct{}` field, forbids extra properties |

Example from `internal/conflictresolve/conflictresolve.go`:

```go
type OneshotResult struct {
    AllResolved bool                  `json:"all_resolved" required:"true"`
    Confidence  string                `json:"confidence" required:"true"`
    Summary     string                `json:"summary" required:"true"`
    Files       map[string]FileAction `json:"files" required:"true" nullable:"false"`
    _           struct{}              `additionalProperties:"false"`
}
```

Fields can be excluded from the emitted schema via the variadic `skipFields` parameter (e.g., `schema.Register(LabelNudgeNik, Result{}, "source")` for internal-only fields populated by code, not the LLM).

### Adding a new oneshot consumer

1. Add a `LabelXxx` constant to `internal/schema/schema.go`.
2. In the consumer package, define a response struct with `json` tags + `required:"true"` / `additionalProperties:"false"` as appropriate.
3. Register it in the package's `init()`: `schema.Register(schema.LabelXxx, MyResult{})`.
4. Write the prompt so the LLM emits JSON matching the struct shape.
5. Call `oneshot.ExecuteTarget[MyResult](ctx, cfg, targetName, prompt, schema.LabelXxx, timeout, dir)`.
6. Put any post-decode validation or normalization in a pure function (unit-testable without the LLM).

### Validation

`TestSchemaRegistry` in `internal/oneshot/schema_integration_test.go` walks every registered schema and checks:

- All `required` keys exist in `properties`
- When `additionalProperties` is a schema object, `properties` is explicitly defined (OpenAI structured-output requirement)
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
