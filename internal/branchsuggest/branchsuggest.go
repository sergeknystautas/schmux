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

	// Empty Branch is treated as ErrInvalidBranch (not ErrInvalidResponse) because
	// the JSON parsed cleanly — the LLM produced a structurally valid Result with a
	// blank value, which is a content failure, not a transport failure. Both errors
	// map to HTTP 400 in handlers_spawn.go, so external behavior is unchanged.
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
