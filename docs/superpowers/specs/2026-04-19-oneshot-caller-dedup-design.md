# Unify oneshot caller boilerplate

**Status:** design
**Date:** 2026-04-19

## Problem

Every package that runs a oneshot LLM call re-implements the same ~30–80 lines of wrapping around `oneshot.ExecuteTarget`:

- `init()` shape for `schema.Register`
- Local sentinel errors (`ErrDisabled`, `ErrTargetNotFound`, `ErrInvalidResponse`) that duplicate each other
- `IsEnabled(cfg)` helper that just checks `targetName != ""`
- Target lookup + empty check + `ExecuteTarget` call + translation of `oneshot.ErrTargetNotFound` to a local error
- A `ParseResult` function: trim → strip ` ```json ` fences → locate first `{` / last `}` → unmarshal → retry with `NormalizeJSONPayload` on failure
- A one-line `normalizeJSONPayload` wrapper around `oneshot.NormalizeJSONPayload`

Current footprints:

| File                                                   | Total lines | Genuine feature content                            |
| ------------------------------------------------------ | ----------- | -------------------------------------------------- |
| `internal/branchsuggest/branchsuggest.go`              | 157         | ~60 (Prompt + struct + `ValidateBranchName`)       |
| `internal/nudgenik/nudgenik.go`                        | 192         | ~90 (Prompt + struct + `ExtractLatestFromCapture`) |
| `internal/conflictresolve/conflictresolve.go`          | 225         | ~120 (`BuildPrompt` + struct + `normalizeSummary`) |
| `internal/dashboard/commit.go` commit-generate handler | ~70         | ~55                                                |

The duplicates have drifted: `conflictresolve.extractFromEnvelope` reimplements envelope unwrapping that `oneshot.parseClaudeStructuredOutput` already performs before returning to the caller. `nudgenik.ParseResult` returns a more detailed error than `branchsuggest.ParseResult` for the same failure mode. Adding a new oneshot feature currently means copying a 40-line template and remembering to keep it in sync.

## Goal

Each feature package contributes only its `Result` struct, `Prompt`, `init()` schema register, and one thin public function. All transport, empty-target handling, error translation, JSON parsing, and fallback normalization live in `internal/oneshot`.

## Changes

### 1. Centralize sentinel errors in `internal/oneshot`

Add alongside the existing `ErrTargetNotFound`:

```go
var (
    ErrDisabled        = errors.New("oneshot target is disabled")
    ErrInvalidResponse = errors.New("invalid oneshot response")
)
```

`oneshot.ExecuteTarget` (and `ExecuteTargetJSON`) return `ErrDisabled` when `targetName == ""` instead of the current `ErrTargetNotFound`. This is a strict refinement: callers that today check `errors.Is(err, oneshot.ErrTargetNotFound)` after passing an empty target will need to also/instead check `ErrDisabled`. In practice every caller currently guards the empty case _before_ calling, so this only affects one site (`daemon.getOrSummarizeIntent`, line 2136, which passes `""` deliberately — caller fall-back behavior stays identical since any error triggers the fallback).

Per-package `ErrDisabled` / `ErrTargetNotFound` / `ErrInvalidResponse` are deleted. Callers test against `oneshot.*` sentinels.

### 2. New helpers in `internal/oneshot`

Two helpers, split so packages that inject a fake executor for testing can keep doing so:

````go
// ParseJSON strips ```json fences, locates the JSON object in s, and decodes
// into T. On unmarshal failure it retries once with NormalizeJSONPayload.
// Returns ErrInvalidResponse (wrapped with the underlying cause) on failure.
func ParseJSON[T any](raw string) (T, error)

// ExecuteTargetJSON composes ExecuteTarget + ParseJSON. Returns:
//   - ErrDisabled       when targetName is empty
//   - ErrTargetNotFound when the target is not in config
//   - ErrInvalidResponse (wrapped) when the response cannot be parsed as T
// The second return value is the raw LLM response — empty on success and on
// pre-parse errors, populated on parse failures so callers can surface it.
func ExecuteTargetJSON[T any](
    ctx context.Context,
    cfg *config.Config,
    targetName, prompt, schemaLabel string,
    timeout time.Duration,
    dir string,
) (T, string, error)
````

`ParseJSON` pipeline:

1. Trim whitespace.
2. Strip ` ```json ` / ` ``` ` code fences.
3. Locate first `{` and last `}` to tolerate trailing banner text.
4. `json.Unmarshal` into `T`.
5. On unmarshal error, retry with `NormalizeJSONPayload` applied.
6. On second failure, return `fmt.Errorf("%w: %v", ErrInvalidResponse, err)`.

`oneshot.Execute` already unwraps the Claude `structured_output` / `result` envelope before returning to the caller, so neither helper needs a second envelope-extraction pass.

### 3. Refactor callers

**`internal/branchsuggest/branchsuggest.go`** — after refactor:

- Delete `ErrDisabled`, `ErrTargetNotFound`, `ErrInvalidResponse`, `ParseResult`, `normalizeJSONPayload`.
- Keep `IsEnabled`: it is called from `internal/dashboard/handlers_spawn.go:515`. Implementation stays a one-liner.
- `AskForPrompt` becomes:

```go
func AskForPrompt(ctx context.Context, cfg *config.Config, userPrompt string) (Result, error) {
    userPrompt = strings.TrimSpace(userPrompt)
    if userPrompt == "" {
        return Result{}, ErrNoPrompt
    }
    input := strings.ReplaceAll(Prompt, "{{USER_PROMPT}}", userPrompt)
    target := ""
    if cfg != nil {
        target = cfg.GetBranchSuggestTarget()
    }
    result, _, err := oneshot.ExecuteTargetJSON[Result](ctx, cfg, target, input, schema.LabelBranchSuggest, branchSuggestTimeout, "")
    if err != nil {
        return Result{}, err
    }
    if err := workspace.ValidateBranchName(strings.TrimSpace(result.Branch)); err != nil {
        return Result{}, ErrInvalidBranch
    }
    return result, nil
}
```

Keep `ErrNoPrompt` and `ErrInvalidBranch` since they are branchsuggest-specific semantics.

**`internal/nudgenik/nudgenik.go`** — same shape. Keep `ExtractLatestFromCapture`, `ErrNoResponse`, and `IsEnabled` (called from `internal/dashboard/handlers_sessions.go:510`). Delete `ParseResult`, `normalizeJSONPayload`, `truncateForError`, and the three generic error sentinels.

**`internal/conflictresolve/conflictresolve.go`** — `Execute` keeps its package-level `executorFunc = oneshot.ExecuteTarget` indirection for test stubs, then pipes the response through `oneshot.ParseJSON[OneshotResult]`. Delete `findJSON`, `extractFromEnvelope`, and `ParseResult`'s envelope and retry logic; keep `normalizeSummary` as a post-parse step. Existing tests that inject a fake `ExecuteTarget` continue to work unchanged.

**`internal/dashboard/commit.go`** — replace inline `json.Unmarshal` + hand-rolled error with one call to `ExecuteTargetJSON[commitmessage.Result]`. Error-to-HTTP mapping uses `errors.Is` against the oneshot sentinels (disabled → 400, not-found → 400, invalid response → 500).

### 4. Out of scope

These are plain-string oneshot calls with no JSON decoding and only a single line of boilerplate — nothing to deduplicate:

- `internal/subreddit/subreddit.go` (schemaLabel=""; parses with feature-specific `ParseBootstrapResult` / `ParseIncrementalResult`; the `FullConfig → *config.Config` assertion is a separate concern).
- `internal/daemon/daemon.go:2136` repofeed intent summarization (returns plain string to cache).
- `internal/daemon/daemon.go:1176`, `1416` and `internal/dashboard/handlers_autolearn.go:1250` executor-factory closures that just forward arguments.
- `internal/dashboard/curation_helpers.go` `ResolveTargetCommand` for run-script generation (not a oneshot call path).

## Testing

- `conflictresolve_test.go` already injects `executorFunc` (test file lines 248–249, 344) and uses the `oneshot.ExecuteTarget` signature `(string, error)`. Since the spec keeps that signature, these tests work unchanged.
- `branchsuggest_test.go` and `nudgenik_test.go` currently exercise `ParseResult` directly (no executor injection). After `ParseResult` is deleted, these tests move: JSON-parsing cases (fences, curly quotes, malformed payloads) relocate to `internal/oneshot/oneshot_json_test.go` against `ParseJSON[T]`. Feature-specific assertions (e.g. branchsuggest's `ValidateBranchName` rejection, nudgenik's empty-capture handling) stay in their home packages and test the public entry point by giving it inputs that reach the validator without needing to hit the LLM (e.g. empty prompt → `ErrNoPrompt` before any network call).
- New `internal/oneshot/oneshot_json_test.go` covers: empty target → `ErrDisabled`, unknown target → `ErrTargetNotFound`, malformed JSON → `ErrInvalidResponse` with raw response surfaced, fenced JSON succeeds, curly-quote JSON succeeds via `NormalizeJSONPayload` fallback.
- `./test.sh` passes; `./badcode.sh` clean.

## Expected size change

- branchsuggest.go: 157 → ~70 lines
- nudgenik.go: 192 → ~100 lines
- conflictresolve.go: 225 → ~130 lines
- dashboard/commit.go commit-generate handler: −10 lines (minor)
- oneshot package: +~70 lines for `ExecuteTargetJSON` and its tests

Net savings: ~150 lines, and adding the next oneshot feature drops from a 40-line boilerplate template to a ~15-line wrapper.
