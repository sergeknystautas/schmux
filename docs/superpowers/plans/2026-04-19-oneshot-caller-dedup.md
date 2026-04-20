# Oneshot Caller Deduplication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Collapse ~150 lines of duplicated wrapper code across 4 oneshot LLM-call features by centralizing errors, JSON parsing, and an `ExecuteTarget + decode` helper inside `internal/oneshot`.

**Architecture:** Add two generics-based helpers to `internal/oneshot` (`ParseJSON[T]`, `ExecuteTargetJSON[T]`) and two new sentinel errors (`ErrDisabled`, `ErrInvalidResponse`). Feature packages (`branchsuggest`, `nudgenik`, `conflictresolve`, `dashboard/commit`) delete their per-package parsing, normalization, and error-sentinel code and call the central helpers. External callers that `errors.Is` against feature-level sentinels migrate to the `oneshot.*` equivalents.

**Tech Stack:** Go 1.22+ (generics), `encoding/json`, `github.com/sergeknystautas/schmux/internal/oneshot`, `github.com/sergeknystautas/schmux/internal/schema`, standard `testing` package.

**Source spec:** `docs/superpowers/specs/2026-04-19-oneshot-caller-dedup-design.md`

**Working-agreement constraint:** The user's `~/.claude/CLAUDE.md` says agents must NEVER `git commit`. This plan does not include per-task commits. The user creates a single commit at the end after review.

---

## File Structure

**New files:**

- `internal/oneshot/oneshot_json.go` — new errors (`ErrDisabled`, `ErrInvalidResponse`) + `ParseJSON[T]` + `ExecuteTargetJSON[T]`.
- `internal/oneshot/oneshot_json_test.go` — unit tests for the two new helpers.

**Modified files:**

- `internal/oneshot/oneshot.go` — `resolveTarget` (and thereby `ExecuteTarget`/`ExecuteTargetJSON`) returns `ErrDisabled` when `targetName == ""` instead of `ErrTargetNotFound`.
- `internal/branchsuggest/branchsuggest.go` — delete `ParseResult`, `normalizeJSONPayload`, `ErrDisabled`, `ErrTargetNotFound`, `ErrInvalidResponse`; `AskForPrompt` uses `oneshot.ExecuteTargetJSON[Result]`.
- `internal/branchsuggest/branchsuggest_test.go` — delete `TestParseResult` (coverage moves to `oneshot_json_test.go`).
- `internal/nudgenik/nudgenik.go` — delete `ParseResult`, `normalizeJSONPayload`, `truncateForError`, `ErrDisabled`, `ErrTargetNotFound`, `ErrInvalidResponse`; keep `ErrNoResponse`, `ErrTargetNoSecrets`, `IsEnabled`, `ExtractLatestFromCapture`.
- `internal/nudgenik/helpers_test.go` — delete `TestParseResult` (coverage moves to `oneshot_json_test.go`); keep capture-extraction tests.
- `internal/conflictresolve/conflictresolve.go` — delete `findJSON`, `extractFromEnvelope`, `ErrDisabled`, `ErrTargetNotFound`, `ErrInvalidResponse`; `Execute` keeps `executorFunc` indirection and pipes the response through `oneshot.ParseJSON[OneshotResult]`; `ParseResult` becomes a one-line delegate to preserve the existing test suite, then call-sites are updated to call `oneshot.ParseJSON[OneshotResult]` directly where reasonable (the test file drives this decision).
- `internal/conflictresolve/conflictresolve_test.go` — adjust `TestParseResult` to handle the new `oneshot.ErrInvalidResponse` error-chain (uses `errors.Is`, not string matching).
- `internal/dashboard/commit.go` — replace inline `json.Unmarshal` + manual error handling with `oneshot.ExecuteTargetJSON[commitmessage.Result]`; map errors to HTTP statuses via `errors.Is(err, oneshot.Err*)`.
- `internal/dashboard/handlers_spawn.go:529-535` — switch `errors.Is` checks from `branchsuggest.ErrTargetNotFound` / `branchsuggest.ErrDisabled` / `branchsuggest.ErrInvalidResponse` to `oneshot.*` equivalents. Keep `branchsuggest.ErrNoPrompt` / `branchsuggest.ErrInvalidBranch` checks (feature-specific semantics).
- `internal/dashboard/handlers_sessions.go:482-491, 517-529` — switch `errors.Is` checks from `nudgenik.ErrDisabled` / `nudgenik.ErrTargetNotFound` to `oneshot.*`. Keep `nudgenik.ErrNoResponse` / `nudgenik.ErrTargetNoSecrets` checks (feature-specific). Migrate `parseNudgeSummary` to call `oneshot.ParseJSON[nudgenik.Result]` instead of `nudgenik.ParseResult`.
- `internal/daemon/daemon.go:1801-1807` — same migration: `nudgenik.ErrDisabled` / `nudgenik.ErrTargetNotFound` → `oneshot.*`; keep `nudgenik.ErrNoResponse` / `nudgenik.ErrTargetNoSecrets`.

**Untouched (confirmed out of scope):**

- `internal/subreddit/subreddit.go` — passes `schemaLabel=""`, parses with feature-specific parsers. No JSON-decoding boilerplate to consolidate.
- `internal/daemon/daemon.go:1176, 1416, 2136` — thin passthrough closures or plain-string calls.
- `internal/dashboard/handlers_autolearn.go:1250` — executor-factory closure.
- `internal/dashboard/curation_helpers.go:34` — `ResolveTargetCommand` run-script generation, not a oneshot call path.

---

## Task 1: Add central errors and `ParseJSON[T]` in `internal/oneshot`

**Files:**

- Create: `internal/oneshot/oneshot_json.go`
- Create: `internal/oneshot/oneshot_json_test.go`

### Context for the implementer

`internal/oneshot/oneshot.go` already defines `ErrTargetNotFound` (line ~31) and the `NormalizeJSONPayload` helper (line ~452) that fixes common LLM JSON encoding issues (fancy quotes, extra whitespace, tabs). You are adding two new exported errors in the same package, and a generic helper that performs the JSON-extraction pipeline currently duplicated across `branchsuggest`, `nudgenik`, and `conflictresolve`.

The duplicated pipeline (see `internal/branchsuggest/branchsuggest.go:112-152` and `internal/nudgenik/nudgenik.go:148-179`):

1. `strings.TrimSpace`
2. If prefix is ` ``` `: strip ` ```json ` / ` ``` ` fences
3. Find first `{` and last `}`; slice between them
4. `json.Unmarshal` into the target struct
5. On error: apply `oneshot.NormalizeJSONPayload`, retry unmarshal
6. On second error: return wrapped `ErrInvalidResponse`

- [ ] **Step 1: Write the failing tests for `ParseJSON`**

Create `internal/oneshot/oneshot_json_test.go` with the following content:

````go
package oneshot

import (
	"errors"
	"strings"
	"testing"
)

type testResult struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestParseJSON_PlainObject(t *testing.T) {
	got, err := ParseJSON[testResult](`{"name":"foo","value":7}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "foo" || got.Value != 7 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseJSON_StripsCodeFence(t *testing.T) {
	raw := "```json\n{\"name\":\"bar\",\"value\":1}\n```"
	got, err := ParseJSON[testResult](raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "bar" || got.Value != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseJSON_HandlesBannerBeforeAndAfter(t *testing.T) {
	raw := "blah blah {\"name\":\"baz\",\"value\":42} trailing"
	got, err := ParseJSON[testResult](raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "baz" || got.Value != 42 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseJSON_CurlyQuotesRecoveredByNormalize(t *testing.T) {
	// Curly double-quotes would break json.Unmarshal; NormalizeJSONPayload
	// replaces them with ASCII quotes on retry.
	raw := "{\u201cname\u201d:\u201chello\u201d,\u201cvalue\u201d:3}"
	got, err := ParseJSON[testResult](raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "hello" || got.Value != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseJSON_EmptyInput(t *testing.T) {
	_, err := ParseJSON[testResult]("")
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("want ErrInvalidResponse, got %v", err)
	}
}

func TestParseJSON_NoBraces(t *testing.T) {
	_, err := ParseJSON[testResult]("nothing json-like here")
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("want ErrInvalidResponse, got %v", err)
	}
}

func TestParseJSON_MalformedJSONBeyondRecovery(t *testing.T) {
	_, err := ParseJSON[testResult](`{"name": "unterminated`)
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("want ErrInvalidResponse, got %v", err)
	}
	if !strings.Contains(err.Error(), "unexpected end") && !strings.Contains(err.Error(), "unexpected EOF") && !strings.Contains(err.Error(), "no JSON") {
		// Accept either unmarshal error leak-through or the no-JSON message,
		// but the point is the error chain contains useful context.
		t.Logf("note: underlying error is %q", err.Error())
	}
}
````

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/oneshot/ -run TestParseJSON -v`

Expected: All tests fail because `ParseJSON` and `ErrInvalidResponse` do not yet exist. Error should be a compile error: `undefined: ParseJSON` and `undefined: ErrInvalidResponse`.

- [ ] **Step 3: Create the implementation file**

Create `internal/oneshot/oneshot_json.go`:

````go
package oneshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
)

// ErrDisabled is returned when a oneshot helper is invoked with an empty target
// name. Callers use it to distinguish "feature turned off" from
// ErrTargetNotFound ("configured target does not exist").
var ErrDisabled = errors.New("oneshot target is disabled")

// ErrInvalidResponse is returned when an LLM response cannot be decoded into
// the expected struct, even after fence-stripping and NormalizeJSONPayload
// recovery. It wraps the underlying json.Unmarshal error so callers can
// inspect it with errors.Unwrap.
var ErrInvalidResponse = errors.New("invalid oneshot response")

// ParseJSON strips ```json code fences, locates the JSON object in raw, and
// decodes it into T. On unmarshal failure it retries once with
// NormalizeJSONPayload applied. Returns ErrInvalidResponse (wrapping the
// underlying cause) on failure.
func ParseJSON[T any](raw string) (T, error) {
	var zero T
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return zero, fmt.Errorf("%w: empty response", ErrInvalidResponse)
	}

	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "```json"))
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "```"))
	}

	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start == -1 || end == -1 || end <= start {
		return zero, fmt.Errorf("%w: no JSON object found", ErrInvalidResponse)
	}

	payload := trimmed[start : end+1]
	var result T
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		normalized := NormalizeJSONPayload(payload)
		if normalized == "" {
			return zero, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
		}
		if err2 := json.Unmarshal([]byte(normalized), &result); err2 != nil {
			return zero, fmt.Errorf("%w: %v", ErrInvalidResponse, err2)
		}
	}
	return result, nil
}

// ExecuteTargetJSON resolves a target, runs ExecuteTarget, and decodes the
// response into T. Returns:
//   - ErrDisabled        when targetName is empty
//   - ErrTargetNotFound  when the target is not in config
//   - ErrInvalidResponse (wrapped) when the response cannot be parsed as T
//
// The second return value is the raw LLM response — empty on success and on
// pre-parse errors, populated on parse failures so callers can surface it
// (e.g. conflictresolve → linear_sync).
func ExecuteTargetJSON[T any](
	ctx context.Context,
	cfg *config.Config,
	targetName, prompt, schemaLabel string,
	timeout time.Duration,
	dir string,
) (T, string, error) {
	var zero T
	response, err := ExecuteTarget(ctx, cfg, targetName, prompt, schemaLabel, timeout, dir)
	if err != nil {
		return zero, "", err
	}
	parsed, err := ParseJSON[T](response)
	if err != nil {
		return zero, response, err
	}
	return parsed, "", nil
}
````

- [ ] **Step 4: Run the `ParseJSON` tests to verify they pass**

Run: `go test ./internal/oneshot/ -run TestParseJSON -v`

Expected: All 7 tests pass.

- [ ] **Step 5: Add and run failing tests for the `ErrDisabled` behavior of `ExecuteTarget`**

Append to `internal/oneshot/oneshot_json_test.go`:

```go
func TestExecuteTarget_EmptyTargetReturnsErrDisabled(t *testing.T) {
	_, err := ExecuteTarget(nil, nil, "", "some prompt", "", 0, "")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("want ErrDisabled, got %v", err)
	}
}

func TestExecuteTargetJSON_EmptyTargetReturnsErrDisabled(t *testing.T) {
	_, raw, err := ExecuteTargetJSON[testResult](nil, nil, "", "some prompt", "", 0, "")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("want ErrDisabled, got %v", err)
	}
	if raw != "" {
		t.Fatalf("raw should be empty on pre-parse error, got %q", raw)
	}
}
```

Run: `go test ./internal/oneshot/ -run TestExecuteTarget -v`

Expected: Both tests fail — `ExecuteTarget("", ...)` currently returns `ErrTargetNotFound`, not `ErrDisabled`.

- [ ] **Step 6: Modify `ExecuteTarget` to return `ErrDisabled` for empty `targetName`**

In `internal/oneshot/oneshot.go`, locate `ExecuteTarget` (around line 202). Before the existing `prompt == ""` validation, add:

```go
if targetName == "" {
	return "", ErrDisabled
}
```

The validated block near the top of `ExecuteTarget` should now read:

```go
func ExecuteTarget(ctx context.Context, cfg *config.Config, targetName, prompt, schemaLabel string, timeout time.Duration, dir string) (string, error) {
	if targetName == "" {
		return "", ErrDisabled
	}
	if prompt == "" {
		return "", fmt.Errorf("prompt cannot be empty")
	}

	target, err := resolveTarget(cfg, targetName)
	// ... existing code ...
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/oneshot/ -v`

Expected: All new tests pass; pre-existing oneshot tests still pass.

- [ ] **Step 8: Run the whole oneshot package suite**

Run: `go test ./internal/oneshot/...`

Expected: PASS (no regressions).

---

## Task 2: Migrate `internal/branchsuggest` to the central helpers

**Files:**

- Modify: `internal/branchsuggest/branchsuggest.go`
- Modify: `internal/branchsuggest/branchsuggest_test.go`
- Modify: `internal/dashboard/handlers_spawn.go:529-535`

### Context for the implementer

`branchsuggest.AskForPrompt` currently:

1. Trim-checks the user prompt (`ErrNoPrompt`).
2. Looks up the configured target (returns `ErrDisabled` for empty).
3. Substitutes into `Prompt`.
4. Calls `oneshot.ExecuteTarget`, translates `oneshot.ErrTargetNotFound` → `branchsuggest.ErrTargetNotFound`.
5. Calls `ParseResult` to unmarshal into `Result` and validate with `workspace.ValidateBranchName` (returns `ErrInvalidBranch`).

`branchsuggest.ErrDisabled`, `.ErrTargetNotFound`, and `.ErrInvalidResponse` are checked externally in `internal/dashboard/handlers_spawn.go` at lines 529-535. Those checks must be migrated to the new `oneshot.*` sentinels.

After refactor, the branchsuggest package keeps: `Prompt`, `Result`, `init()`, `branchSuggestTimeout`, `ErrNoPrompt`, `ErrInvalidBranch`, `IsEnabled`, `AskForPrompt`. It loses: `ErrDisabled`, `ErrTargetNotFound`, `ErrInvalidResponse`, `ParseResult`, `normalizeJSONPayload`.

`branchsuggest_test.go` currently has only `TestParseResult` (see file lines 1-55). That coverage is replaced by `oneshot.TestParseJSON_*` plus one new feature-level test for the `ValidateBranchName` post-check.

- [ ] **Step 1: Rewrite `branchsuggest.go`**

Replace the contents of `internal/branchsuggest/branchsuggest.go` with:

```go
package branchsuggest

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schema"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func init() {
	// Register the Result type for JSON schema generation.
	schema.Register(schema.LabelBranchSuggest, Result{})
}

const (
	// Prompt is the branch suggestion prompt.
	Prompt = `
You are generating a git branch name from a coding task prompt.

Generate a branch name following git conventions (kebab-case, lowercase, concise).

Rules:
- 3-6 words max, use prefixes like "feature/", "fix/", "refactor/" when appropriate
- Must be kebab-case (lowercase, hyphens only, no spaces)
- Avoid the words "add", "implement" - focus on what it IS, not what you're DOING
- If the prompt mentions a specific component/feature, include that in the branch name

Examples:
- Prompt: "Add dark mode to the settings panel"
  Branch: "feature/dark-mode-settings"

- Prompt: "Fix the login bug where users can't reset password"
  Branch: "fix/password-reset"

- Prompt: "Refactor the auth flow to use JWT tokens"
  Branch: "refactor/auth-jwt"

Here is the user's prompt:
<<<
{{USER_PROMPT}}
>>>
`

	branchSuggestTimeout = 30 * time.Second
)

var (
	ErrNoPrompt      = errors.New("empty prompt provided")
	ErrInvalidBranch = errors.New("invalid branch name")
)

// IsEnabled returns true if branch suggestion is enabled (has a configured target).
func IsEnabled(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return cfg.GetBranchSuggestTarget() != ""
}

// Result is the parsed branch suggestion response.
// Struct tags control JSON schema generation via swaggest/jsonschema-go.
type Result struct {
	Branch string   `json:"branch" required:"true"`
	_      struct{} `additionalProperties:"false"`
}

// AskForPrompt generates a branch name from a user prompt.
// Errors surfaced:
//   - ErrNoPrompt                  (empty user prompt)
//   - oneshot.ErrDisabled          (no target configured)
//   - oneshot.ErrTargetNotFound    (configured target missing)
//   - oneshot.ErrInvalidResponse   (LLM output not parseable)
//   - ErrInvalidBranch             (LLM returned an invalid branch name)
func AskForPrompt(ctx context.Context, cfg *config.Config, userPrompt string) (Result, error) {
	userPrompt = strings.TrimSpace(userPrompt)
	if userPrompt == "" {
		return Result{}, ErrNoPrompt
	}

	targetName := ""
	if cfg != nil {
		targetName = cfg.GetBranchSuggestTarget()
	}

	input := strings.ReplaceAll(Prompt, "{{USER_PROMPT}}", userPrompt)

	result, _, err := oneshot.ExecuteTargetJSON[Result](ctx, cfg, targetName, input, schema.LabelBranchSuggest, branchSuggestTimeout, "")
	if err != nil {
		return Result{}, err
	}

	branch := strings.TrimSpace(result.Branch)
	if branch == "" {
		return Result{}, ErrInvalidBranch
	}
	if err := workspace.ValidateBranchName(branch); err != nil {
		return Result{}, ErrInvalidBranch
	}
	result.Branch = branch
	return result, nil
}
```

- [ ] **Step 2: Replace the branchsuggest test file**

Replace the contents of `internal/branchsuggest/branchsuggest_test.go` with a focused test that exercises only feature-level behavior not covered by `oneshot.TestParseJSON_*`. The LLM-execution path requires a live target, so the error paths we can unit-test without one are: empty prompt and disabled target.

```go
package branchsuggest

import (
	"context"
	"errors"
	"testing"

	"github.com/sergeknystautas/schmux/internal/oneshot"
)

func TestAskForPrompt_EmptyPrompt(t *testing.T) {
	_, err := AskForPrompt(context.Background(), nil, "   ")
	if !errors.Is(err, ErrNoPrompt) {
		t.Fatalf("want ErrNoPrompt, got %v", err)
	}
}

func TestAskForPrompt_NilConfigReturnsErrDisabled(t *testing.T) {
	_, err := AskForPrompt(context.Background(), nil, "add dark mode")
	if !errors.Is(err, oneshot.ErrDisabled) {
		t.Fatalf("want oneshot.ErrDisabled, got %v", err)
	}
}
```

- [ ] **Step 3: Migrate external error-sentinel checks in `handlers_spawn.go`**

Open `internal/dashboard/handlers_spawn.go`. Locate the block around lines 529-535 that currently reads:

```go
case errors.Is(err, branchsuggest.ErrNoPrompt):
	// ...
case errors.Is(err, branchsuggest.ErrTargetNotFound):
	// ...
case errors.Is(err, branchsuggest.ErrDisabled):
	// ...
case errors.Is(err, branchsuggest.ErrInvalidBranch), errors.Is(err, branchsuggest.ErrInvalidResponse):
	// ...
```

Replace the three feature-level sentinels with their `oneshot.*` counterparts, keeping `ErrNoPrompt` and `ErrInvalidBranch` (feature-specific):

```go
case errors.Is(err, branchsuggest.ErrNoPrompt):
	// ...
case errors.Is(err, oneshot.ErrTargetNotFound):
	// ...
case errors.Is(err, oneshot.ErrDisabled):
	// ...
case errors.Is(err, branchsuggest.ErrInvalidBranch), errors.Is(err, oneshot.ErrInvalidResponse):
	// ...
```

Ensure `import "github.com/sergeknystautas/schmux/internal/oneshot"` is present in `handlers_spawn.go`'s import block. If not, add it.

- [ ] **Step 4: Build and run branchsuggest tests**

Run: `go build ./... && go test ./internal/branchsuggest/ ./internal/dashboard/ -v`

Expected: All tests pass. Specifically, no compile errors in `handlers_spawn.go` from the removed error sentinels.

- [ ] **Step 5: Run the whole backend test suite**

Run: `go test ./...`

Expected: PASS, no regressions. If anything fails, the most likely culprit is another import I missed — search with: `grep -rn "branchsuggest.Err" --include="*.go" .` and make sure every remaining reference resolves to a sentinel that still exists (`ErrNoPrompt`, `ErrInvalidBranch`).

---

## Task 3: Migrate `internal/nudgenik` to the central helpers

**Files:**

- Modify: `internal/nudgenik/nudgenik.go`
- Modify: `internal/nudgenik/helpers_test.go`
- Modify: `internal/dashboard/handlers_sessions.go:482-491, 517-529`
- Modify: `internal/daemon/daemon.go:1801-1807`

### Context for the implementer

`nudgenik` follows the same shape as branchsuggest but has two extra wrinkles:

1. `nudgenik.ParseResult` is called externally by `parseNudgeSummary` (handlers*sessions.go:517-529) to parse a \_stored* nudge string — this is not a fresh LLM call, it's post-hoc parsing of persisted JSON. After `ParseResult` is deleted, that caller switches to `oneshot.ParseJSON[nudgenik.Result]`.
2. `nudgenik.ErrTargetNoSecrets` is declared in nudgenik.go but is **never returned** anywhere in the codebase. External `errors.Is` checks exist (daemon.go:1807, handlers_sessions.go:491) but always fall through. Leave this error declared (out of scope) — callers keep their (dead) checks.

After refactor, the nudgenik package keeps: `Prompt`, `Result`, `init()`, `nudgenikTimeout`, `ErrNoResponse`, `ErrTargetNoSecrets`, `IsEnabled`, `AskForCapture`, `AskForExtracted`, `ExtractLatestFromCapture`. It loses: `ErrDisabled`, `ErrTargetNotFound`, `ErrInvalidResponse`, `ParseResult`, `normalizeJSONPayload`, `truncateForError`.

- [ ] **Step 1: Rewrite `nudgenik.go`**

Replace the contents of `internal/nudgenik/nudgenik.go` with:

```go
package nudgenik

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schema"
	"github.com/sergeknystautas/schmux/internal/tmux"
)

func init() {
	// Register the Result type for JSON schema generation.
	// The "source" field is excluded as it's set by code, not the LLM.
	schema.Register(schema.LabelNudgeNik, Result{}, "source")
}

const (
	// Prompt is the NudgeNik prompt prefix.
	Prompt = `
You are analyzing the last response from a coding agent.

Your task is to determine the agent’s current operational state based ONLY on that response.

Do NOT:
- continue development
- suggest next steps
- ask clarifying questions

Choose exactly ONE state from the list below:
- Needs Input
- Needs Feature Clarification
- Needs Attention
- Completed

If multiple states appear applicable, choose the primary blocking or terminal state.

Compacted results should be considered Needs Feature Clarification.

When to choose "Needs Authorization" (must follow these):
- Any response that includes a menu, numbered choices, or a confirmation prompt (e.g., "Do you want to proceed?", "Proceed?", "Choose an option", "What do you want to do?").
- Any response that indicates a rate limit with options to wait/upgrade.

Stylistic rules for "summary":
- Do NOT use the words "agent", "model", "system", or "it"
- Do NOT anthropomorphize
- Begin directly with the situation or state (e.g., "Implementation is complete…" not "The agent has completed…")

Here is the agent’s last response:
<<<
{{AGENT_LAST_RESPONSE}}
>>>
`

	nudgenikTimeout = 15 * time.Second
)

var (
	ErrNoResponse      = errors.New("no response extracted")
	ErrTargetNoSecrets = errors.New("nudgenik target missing required secrets")
)

// IsEnabled returns true if nudgenik is enabled (has a configured target).
func IsEnabled(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return cfg.GetNudgenikTarget() != ""
}

// Result is the parsed NudgeNik response.
// Struct tags control JSON schema generation via swaggest/jsonschema-go.
// Note: Source is internal (not in schema), set by code after parsing.
type Result struct {
	State      string   `json:"state" required:"true"`
	Confidence string   `json:"confidence,omitempty" required:"true"`
	Evidence   []string `json:"evidence,omitempty" required:"true" nullable:"false"`
	Summary    string   `json:"summary" required:"true"`
	Source     string   `json:"source,omitempty"`
	_          struct{} `additionalProperties:"false"`
}

// AskForCapture extracts the latest response from a raw tmux capture and asks NudgeNik for feedback.
func AskForCapture(ctx context.Context, cfg *config.Config, capture string) (Result, error) {
	extracted, err := ExtractLatestFromCapture(capture)
	if err != nil {
		return Result{}, err
	}
	return AskForExtracted(ctx, cfg, extracted)
}

// AskForExtracted asks NudgeNik using a pre-extracted agent response.
// Errors surfaced:
//   - ErrNoResponse                (empty extracted text)
//   - oneshot.ErrDisabled          (no target configured)
//   - oneshot.ErrTargetNotFound    (configured target missing)
//   - oneshot.ErrInvalidResponse   (LLM output not parseable)
func AskForExtracted(ctx context.Context, cfg *config.Config, extracted string) (Result, error) {
	if strings.TrimSpace(extracted) == "" {
		return Result{}, ErrNoResponse
	}

	targetName := ""
	if cfg != nil {
		targetName = cfg.GetNudgenikTarget()
	}

	input := strings.Replace(Prompt, "{{AGENT_LAST_RESPONSE}}", extracted, 1)

	timeoutCtx, cancel := context.WithTimeout(ctx, nudgenikTimeout)
	defer cancel()

	result, _, err := oneshot.ExecuteTargetJSON[Result](timeoutCtx, cfg, targetName, input, schema.LabelNudgeNik, nudgenikTimeout, "")
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

// ExtractLatestFromCapture extracts the latest agent response from a raw tmux capture.
func ExtractLatestFromCapture(capture string) (string, error) {
	lines := strings.Split(capture, "\n")
	extracted := tmux.ExtractLatestResponse(lines)
	if strings.TrimSpace(extracted) == "" {
		return "", ErrNoResponse
	}
	return extracted, nil
}
```

- [ ] **Step 2: Replace the nudgenik test file**

The old `helpers_test.go` contains capture-extraction tests (`TestExtractLatestFromCapture_*`) plus `TestParseResult`. Keep the former; delete the latter (its coverage moves to `oneshot.TestParseJSON_*`).

First inspect the existing file to see which tests are present:

Run: `grep -n "^func Test" internal/nudgenik/helpers_test.go`

Expected output: Lists of test functions. The `TestParseResult` function (currently lines 71-150 in that file) needs to be removed. Other `TestExtract*` or similar tests stay.

Open `internal/nudgenik/helpers_test.go` and delete the `TestParseResult` function (from its `func TestParseResult(t *testing.T) {` through its matching closing `}`). Leave all other test functions intact. If the file has unused imports after deletion, remove them (common offenders: `"strings"`, `"testing"`'s sibling imports used only by the deleted test).

- [ ] **Step 3: Migrate `parseNudgeSummary` in `handlers_sessions.go`**

Open `internal/dashboard/handlers_sessions.go` and locate `parseNudgeSummary` at lines 517-529. Current code:

```go
func parseNudgeSummary(nudge string) (string, string) {
	trimmed := strings.TrimSpace(nudge)
	if trimmed == "" {
		return "", ""
	}

	result, err := nudgenik.ParseResult(trimmed)
	if err != nil {
		return "", ""
	}

	return strings.TrimSpace(result.State), strings.TrimSpace(result.Summary)
}
```

Replace with:

```go
func parseNudgeSummary(nudge string) (string, string) {
	trimmed := strings.TrimSpace(nudge)
	if trimmed == "" {
		return "", ""
	}

	result, err := oneshot.ParseJSON[nudgenik.Result](trimmed)
	if err != nil {
		return "", ""
	}

	return strings.TrimSpace(result.State), strings.TrimSpace(result.Summary)
}
```

Ensure `import "github.com/sergeknystautas/schmux/internal/oneshot"` is in the import block.

- [ ] **Step 4: Migrate error sentinels in `handlers_sessions.go`**

Locate the error-switch block at lines 482-491. Current:

```go
case errors.Is(err, nudgenik.ErrDisabled):
	// ...
case errors.Is(err, nudgenik.ErrNoResponse):
	// ...
case errors.Is(err, nudgenik.ErrTargetNotFound):
	// ...
case errors.Is(err, nudgenik.ErrTargetNoSecrets):
	// ...
```

Replace with:

```go
case errors.Is(err, oneshot.ErrDisabled):
	// ...
case errors.Is(err, nudgenik.ErrNoResponse):
	// ...
case errors.Is(err, oneshot.ErrTargetNotFound):
	// ...
case errors.Is(err, nudgenik.ErrTargetNoSecrets):
	// ...
```

`ErrNoResponse` and `ErrTargetNoSecrets` stay because they are feature-specific (the former is returned from `AskForExtracted` / `ExtractLatestFromCapture`; the latter is a dead-but-declared sentinel that we are not cleaning up in this refactor).

- [ ] **Step 5: Migrate error sentinels in `daemon.go`**

Open `internal/daemon/daemon.go` and locate lines 1801-1807:

```go
case errors.Is(err, nudgenik.ErrDisabled):
	// ...
case errors.Is(err, nudgenik.ErrNoResponse):
	// ...
case errors.Is(err, nudgenik.ErrTargetNotFound):
	// ...
case errors.Is(err, nudgenik.ErrTargetNoSecrets):
	// ...
```

Replace with:

```go
case errors.Is(err, oneshot.ErrDisabled):
	// ...
case errors.Is(err, nudgenik.ErrNoResponse):
	// ...
case errors.Is(err, oneshot.ErrTargetNotFound):
	// ...
case errors.Is(err, nudgenik.ErrTargetNoSecrets):
	// ...
```

Ensure `oneshot` is already in daemon.go's imports (it is — other call-sites use it).

- [ ] **Step 6: Build and run nudgenik + dashboard + daemon tests**

Run: `go build ./... && go test ./internal/nudgenik/ ./internal/dashboard/ ./internal/daemon/ -v`

Expected: PASS.

- [ ] **Step 7: Run the whole backend test suite**

Run: `go test ./...`

Expected: PASS.

---

## Task 4: Migrate `internal/conflictresolve` to `oneshot.ParseJSON`

**Files:**

- Modify: `internal/conflictresolve/conflictresolve.go`
- Modify: `internal/conflictresolve/conflictresolve_test.go`

### Context for the implementer

`conflictresolve` is the most involved consumer: it has an existing test-injection point (`var executorFunc = oneshot.ExecuteTarget`) that tests stub out to avoid hitting a real LLM. The spec preserves this indirection — the refactor only replaces the parsing tail of `Execute`, not its execution scaffolding.

Current `conflictresolve_test.go` (lines 248-249, 344) stubs `executorFunc` with the signature `func(context.Context, *config.Config, string, string, string, time.Duration, string) (string, error)` — the shape of `oneshot.ExecuteTarget`. We keep that shape.

Three error sentinels (`ErrDisabled`, `ErrTargetNotFound`, `ErrInvalidResponse`) are declared in conflictresolve.go. Search for external `errors.Is` checks against them:

Run first: `grep -rn "conflictresolve.Err" --include="*.go" .`

If any references exist (the initial audit found none), the plan becomes: update those callers to check `oneshot.*`. The initial audit showed no `conflictresolve.Err*` references outside the package itself, but re-verify.

`linear_sync.go:734-741` consumes `Execute`'s second return value (the raw LLM response on parse failure). The new `Execute` preserves this: on `oneshot.ParseJSON` failure, return the raw response.

`TestParseResult` in conflictresolve*test.go (starts at line 14, runs through ~169) exercises `ParseResult`. After refactor, `ParseResult` becomes a two-line delegate to `oneshot.ParseJSON[OneshotResult]` + `normalizeSummary`. The tests keep working because they call `ParseResult`, which keeps its signature. However, tests that match on specific error \_messages* (not `errors.Is`) may break — check during the run step.

- [ ] **Step 1: Verify no external references to conflictresolve error sentinels**

Run: `grep -rn "conflictresolve\.Err" --include="*.go" .`

Expected: Zero matches outside `internal/conflictresolve/` itself. If there are external references, list them and update their `errors.Is` calls to `oneshot.*` before proceeding to Step 2.

- [ ] **Step 2: Rewrite `conflictresolve.go`**

Replace the contents of `internal/conflictresolve/conflictresolve.go` with:

```go
package conflictresolve

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schema"
)

func init() {
	// Register the OneshotResult type for JSON schema generation.
	schema.Register(schema.LabelConflictResolve, OneshotResult{})
}

// executorFunc is the function used to run a oneshot target. Package-level var for testability.
var executorFunc = oneshot.ExecuteTarget

// FileAction describes what the LLM did to resolve a single conflicted file.
// Struct tags control JSON schema generation via swaggest/jsonschema-go.
type FileAction struct {
	Action      string   `json:"action" required:"true"`      // "modified" or "deleted"
	Description string   `json:"description" required:"true"` // per-file explanation
	_           struct{} `additionalProperties:"false"`
}

// OneshotResult is the parsed response from a conflict resolution one-shot call.
// Struct tags control JSON schema generation via swaggest/jsonschema-go.
type OneshotResult struct {
	AllResolved bool                  `json:"all_resolved" required:"true"`
	Confidence  string                `json:"confidence" required:"true"`
	Summary     string                `json:"summary" required:"true"`
	Files       map[string]FileAction `json:"files" required:"true" nullable:"false"`
	_           struct{}              `additionalProperties:"false"`
}

// BuildPrompt constructs the prompt for a conflict resolution one-shot call.
// The LLM is expected to read and edit the conflicted files in-place at the
// given workspace path, then report back what it did via JSON.
func BuildPrompt(workspacePath, defaultBranchHash, localCommitHash, localCommitMessage string, conflictedFiles []string) string {
	var b strings.Builder

	b.WriteString("You are resolving a git rebase conflict.\n\n")
	b.WriteString("One commit from the default branch is being rebased. During replay of a local\n")
	b.WriteString("commit, git produced conflicts in the files listed below.\n\n")
	b.WriteString(fmt.Sprintf("Workspace path: %s\n", workspacePath))
	b.WriteString(fmt.Sprintf("Default branch commit: %s\n", defaultBranchHash))
	b.WriteString(fmt.Sprintf("Local commit being replayed: %s %q\n\n", localCommitHash, localCommitMessage))
	b.WriteString("Conflicted files:\n")

	// Sort file paths for deterministic prompt ordering
	sorted := make([]string, len(conflictedFiles))
	copy(sorted, conflictedFiles)
	sort.Strings(sorted)

	for _, path := range sorted {
		b.WriteString(fmt.Sprintf("  - %s\n", path))
	}

	b.WriteString(`
Instructions:
1. Read each conflicted file (they contain <<<<<<< / ======= / >>>>>>> markers).
2. Resolve the conflict so the intent of BOTH sides is preserved.
3. Write the resolved contents back to the file (or delete the file if the
   correct resolution is removal).
4. Return ONLY a JSON object describing what you did.

Expected JSON format:

{
  "all_resolved": true,
  "confidence": "high",
  "summary": "Detailed explanation of what the conflict was, the approach taken to resolve it, and any concerns or trade-offs involved.\nInclude specifics about what each side was trying to do and how you merged them.\nUse \\n newlines to separate paragraphs or logical sections for readability.",
  "files": {
    "path/to/file.go": {"action": "modified", "description": "Merged both changes"},
    "path/to/obsolete.go": {"action": "deleted", "description": "File was removed by incoming commit"}
  }
}

Rules:
- "all_resolved" must be true only if you resolved ALL conflicts in ALL files
- "confidence" must be "high", "medium", or "low"
- "files" must have an entry for every conflicted file listed above
- Each file entry must have "action" set to "modified" or "deleted"
- Each file entry must include "description"
- If "modified", the file on disk must contain the resolved contents with NO conflict markers
- If "deleted", you must have deleted the file from disk
- The "action" field is used to stage changes: "modified" -> git add, "deleted" -> git rm
- The "summary" field should use \n newlines to separate paragraphs or sections for readability
- Do NOT include any text outside the JSON object
- Output MUST be valid JSON only
`)

	return b.String()
}

// Execute runs the conflict resolution one-shot call against the configured target.
// The workspacePath sets the working directory for the oneshot process so the LLM
// agent can read and edit the conflicted files.
// The second return value is the raw LLM response text, returned on parse errors
// so callers can surface it to the user. It is empty when the error is pre-parse
// (e.g. disabled, target not found, execution failure) or on success.
//
// Error sentinels surfaced (all from internal/oneshot):
//   - oneshot.ErrDisabled          (no conflict_resolve target configured)
//   - oneshot.ErrTargetNotFound    (configured target missing)
//   - oneshot.ErrInvalidResponse   (LLM output not parseable as OneshotResult)
func Execute(ctx context.Context, cfg *config.Config, prompt string, workspacePath string) (OneshotResult, string, error) {
	targetName := cfg.GetConflictResolveTarget()
	timeout := time.Duration(cfg.GetConflictResolveTimeoutMs()) * time.Millisecond

	response, err := executorFunc(ctx, cfg, targetName, prompt, schema.LabelConflictResolve, timeout, workspacePath)
	if err != nil {
		return OneshotResult{}, "", err
	}

	result, err := oneshot.ParseJSON[OneshotResult](response)
	if err != nil {
		return OneshotResult{}, response, err
	}

	// Validate required fields beyond JSON structure: confidence must be populated.
	// oneshot.ParseJSON only enforces JSON-decodability; the stricter "confidence
	// was set" check stays here because the previous implementation rejected
	// responses with empty confidence via "JSON does not contain expected fields".
	if result.Confidence == "" {
		return OneshotResult{}, response, fmt.Errorf("%w: response JSON does not contain expected fields", oneshot.ErrInvalidResponse)
	}

	normalizeSummary(&result)
	return result, "", nil
}

// ParseResult is kept for backward compatibility with the existing test suite.
// It is a thin wrapper around oneshot.ParseJSON[OneshotResult] that applies the
// same post-parse normalization as Execute.
func ParseResult(raw string) (OneshotResult, error) {
	result, err := oneshot.ParseJSON[OneshotResult](raw)
	if err != nil {
		return OneshotResult{}, err
	}
	if result.Confidence == "" {
		return OneshotResult{}, fmt.Errorf("%w: response JSON does not contain expected fields", oneshot.ErrInvalidResponse)
	}
	normalizeSummary(&result)
	return result, nil
}

// normalizeSummary replaces literal "\n" text (which LLMs often produce in JSON
// strings instead of actual newlines) with real newlines.
func normalizeSummary(r *OneshotResult) {
	r.Summary = strings.ReplaceAll(r.Summary, `\n`, "\n")
	for k, f := range r.Files {
		f.Description = strings.ReplaceAll(f.Description, `\n`, "\n")
		r.Files[k] = f
	}
}
```

- [ ] **Step 3: Adjust conflictresolve tests**

Open `internal/conflictresolve/conflictresolve_test.go`. The existing `TestParseResult` cases will mostly still pass because `ParseResult` still exists and delegates to `oneshot.ParseJSON`. Inspect each test case that asserts on a specific error message:

Run: `grep -n "ErrInvalidResponse\|ErrDisabled\|ErrTargetNotFound\|errors.Is" internal/conflictresolve/conflictresolve_test.go`

For every `errors.Is(err, ErrInvalidResponse)` or similar — change to `errors.Is(err, oneshot.ErrInvalidResponse)`. Add `"github.com/sergeknystautas/schmux/internal/oneshot"` to the test file's imports if not present.

For any test that matches on a specific error _substring_ (e.g. `strings.Contains(err.Error(), "no JSON object found")`), verify the new message: `oneshot.ParseJSON` uses `"no JSON object found"` and `"empty response"` as prefixes after `ErrInvalidResponse`. If a test expects the old nudgenik-style `"no JSON object found in: %s"` with the raw payload, loosen it to match the new shorter message.

- [ ] **Step 4: Build and run conflictresolve tests**

Run: `go test ./internal/conflictresolve/ -v`

Expected: PASS. If a test fails on an error-message mismatch, fix the expected string in the test (the new messages are listed above).

- [ ] **Step 5: Run the whole backend test suite**

Run: `go test ./...`

Expected: PASS, including `linear_sync` tests that consume `conflictresolve.Execute`'s raw return.

---

## Task 5: Migrate `internal/dashboard/commit.go` to `ExecuteTargetJSON`

**Files:**

- Modify: `internal/dashboard/commit.go:80-146`
- Possibly modify: `internal/dashboard/commit_test.go`

### Context for the implementer

`commit.go`'s `handleCommitGenerate` handler (approximately lines 80-146) currently:

1. Reads a diff and numstat from git.
2. Builds a prompt.
3. Looks up the target name.
4. Calls `oneshot.ExecuteTarget(...)` with `schema.LabelCommitMessage`.
5. Calls `json.Unmarshal` on the result into `commitmessage.Result`.
6. Writes the HTTP response.

The inline `json.Unmarshal` path on line ~131 duplicates a subset of `ParseJSON`'s logic — without the fence-stripping, find-JSON, or `NormalizeJSONPayload` retry that other features enjoy. Migrating to `ExecuteTargetJSON[commitmessage.Result]` gains that robustness for free.

Error-to-HTTP mapping: previously, any oneshot error → 500. After refactor, distinguish:

- `oneshot.ErrDisabled` → 400 (user-actionable: "configure a target")
- `oneshot.ErrTargetNotFound` → 400 (user-actionable: "target does not exist")
- `oneshot.ErrInvalidResponse` → 500 (LLM produced garbage — server issue)
- Any other error (context timeout, network, etc.) → 500

- [ ] **Step 1: Inspect and modify `handleCommitGenerate`**

Open `internal/dashboard/commit.go`. Locate the block from approximately line 111 to line 145 (the target-name check through the `json.Unmarshal`). Current code:

```go
	// Check if commit message target is configured
	targetName := h.config.GetCommitMessageTarget()
	if targetName == "" {
		h.logger.Info("commit-generate: not configured", "workspace", req.WorkspaceID)
		writeJSONError(w, "No commit_message target configured. Select a model in Settings > Code Review.", http.StatusBadRequest)
		return
	}

	h.logger.Info("commit-generate: asking target", "workspace", req.WorkspaceID, "target", targetName)
	start := time.Now()

	timeout := 60 * time.Second
	rawResult, err := oneshot.ExecuteTarget(ctx, h.config, targetName, prompt, schema.LabelCommitMessage, timeout, ws.Path)
	if err != nil {
		h.logger.Error("commit-generate: failed", "workspace", req.WorkspaceID, "err", err)
		writeJSONError(w, fmt.Sprintf("oneshot failed: %v", err), http.StatusInternalServerError)
		return
	}

	var result commitmessage.Result
	if err := json.Unmarshal([]byte(rawResult), &result); err != nil {
		h.logger.Error("commit-generate: failed to parse response", "workspace", req.WorkspaceID, "err", err)
		writeJSONError(w, fmt.Sprintf("failed to parse response: %v", err), http.StatusInternalServerError)
		return
	}
```

Replace with:

```go
	h.logger.Info("commit-generate: asking target", "workspace", req.WorkspaceID, "target", h.config.GetCommitMessageTarget())
	start := time.Now()

	timeout := 60 * time.Second
	result, raw, err := oneshot.ExecuteTargetJSON[commitmessage.Result](
		ctx,
		h.config,
		h.config.GetCommitMessageTarget(),
		prompt,
		schema.LabelCommitMessage,
		timeout,
		ws.Path,
	)
	if err != nil {
		switch {
		case errors.Is(err, oneshot.ErrDisabled):
			h.logger.Info("commit-generate: not configured", "workspace", req.WorkspaceID)
			writeJSONError(w, "No commit_message target configured. Select a model in Settings > Code Review.", http.StatusBadRequest)
		case errors.Is(err, oneshot.ErrTargetNotFound):
			h.logger.Warn("commit-generate: target missing", "workspace", req.WorkspaceID, "err", err)
			writeJSONError(w, fmt.Sprintf("commit_message target not found: %v", err), http.StatusBadRequest)
		case errors.Is(err, oneshot.ErrInvalidResponse):
			h.logger.Error("commit-generate: failed to parse response", "workspace", req.WorkspaceID, "err", err, "raw", raw)
			writeJSONError(w, fmt.Sprintf("failed to parse response: %v", err), http.StatusInternalServerError)
		default:
			h.logger.Error("commit-generate: failed", "workspace", req.WorkspaceID, "err", err)
			writeJSONError(w, fmt.Sprintf("oneshot failed: %v", err), http.StatusInternalServerError)
		}
		return
	}
```

Notes:

- The pre-check `if targetName == ""` block is gone — `ExecuteTargetJSON` returns `ErrDisabled` which the switch handles.
- `raw` replaces the old `rawResult` variable only for error logging; it is empty on success.
- The logger call that previously read `h.config.GetCommitMessageTarget()` now runs before the call regardless (keeps the existing info log). If you want to avoid calling the getter twice, hoist it:

```go
targetName := h.config.GetCommitMessageTarget()
h.logger.Info("commit-generate: asking target", "workspace", req.WorkspaceID, "target", targetName)
// ... and pass targetName to ExecuteTargetJSON
```

Use the hoisted variant — it's cleaner and matches the pre-existing variable naming.

- [ ] **Step 2: Ensure `errors` and `oneshot` are imported**

Check the import block at the top of `internal/dashboard/commit.go`. If `"errors"` is not present, add it. `"github.com/sergeknystautas/schmux/internal/oneshot"` should already be there. Remove `"encoding/json"` if it is no longer used (the migration removes the only `json.Unmarshal` in this file — but other functions may use it; check before deleting).

- [ ] **Step 3: Run commit handler tests**

Run: `go test ./internal/dashboard/ -run TestCommit -v`

Expected: Existing `commit_test.go` tests pass. If a test depended on the specific error format `"oneshot failed: ..."` for a disabled-target case, update its assertion to match the new error response.

- [ ] **Step 4: Run whole backend test suite**

Run: `go test ./...`

Expected: PASS.

---

## Task 6: Full verification

**Files:** (verification only — no edits)

- [ ] **Step 1: Run `./test.sh`**

Run: `./test.sh`

Expected: PASS. This includes Go unit tests + frontend vitest. Per `CLAUDE.md`, this (not `--quick`) is the definition-of-done check.

If any test fails that was not specifically addressed in Tasks 1-5, investigate before claiming done — do not assume an existing failure. Follow the Ground-Shift Rule in the user's global CLAUDE.md.

- [ ] **Step 2: Run static analysis**

Run: `./badcode.sh`

Expected: PASS (`deadcode`, `staticcheck`, `govulncheck`, `knip`, `npm audit`, `tsc` all clean).

Likely deadcode candidates worth checking:

- `nudgenik.ErrTargetNoSecrets` (already dead before this refactor; still flagged) — leave, out of scope.
- Anything else new should be investigated: if this refactor introduced dead code, reverse it.

- [ ] **Step 3: Confirm line-count expectations**

Run:

```
wc -l internal/branchsuggest/branchsuggest.go internal/nudgenik/nudgenik.go internal/conflictresolve/conflictresolve.go internal/oneshot/oneshot_json.go
```

Expected (approximate):

- `branchsuggest.go`: ~80 lines (was 157)
- `nudgenik.go`: ~115 lines (was 192)
- `conflictresolve.go`: ~155 lines (was 225)
- `oneshot_json.go`: ~70 lines (new)

Net change: ~−154 lines in callers, ~+70 lines in `oneshot` → net savings ~84 lines of code, plus elimination of drift between the three JSON-parse implementations.

- [ ] **Step 4: Summarize changes**

Write a short summary to standard output describing what changed. This is the hand-off material for the user's eventual single commit. Per the user's working agreement, this plan does NOT commit — the user reviews and commits manually.

---

## Appendix: Task dependency graph

```
Task 1 (oneshot helpers + errors)
   │
   ├─ Task 2 (branchsuggest)
   ├─ Task 3 (nudgenik)
   ├─ Task 4 (conflictresolve)
   └─ Task 5 (dashboard/commit)
           │
           ▼
        Task 6 (verification)
```

Tasks 2-5 are independent and can be executed in any order once Task 1 is complete. Task 6 runs last.
