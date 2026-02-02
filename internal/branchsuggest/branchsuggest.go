package branchsuggest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

const (
	// Prompt is the branch suggestion prompt.
	Prompt = `
You are generating a git branch name and nickname from a coding task prompt.

Generate:
1. A branch name following git conventions (kebab-case, lowercase, concise)
2. A short nickname (2-4 words, human-readable)

Rules:
- Branch name should be 3-6 words max, use prefixes like "feature/", "fix/", "refactor/" when appropriate
- Branch name must be kebab-case (lowercase, hyphens only, no spaces)
- Nickname should be a brief summary someone would understand at a glance
- Avoid the words "add", "implement" - focus on what it IS, not what you're DOING
- If the prompt mentions a specific component/feature, include that in the branch name

Examples:
- Prompt: "Add dark mode to the settings panel"
  Branch: "feature/dark-mode-settings"
  Nickname: "Dark mode"

- Prompt: "Fix the login bug where users can't reset password"
  Branch: "fix/password-reset"
  Nickname: "Password reset bug"

- Prompt: "Refactor the auth flow to use JWT tokens"
  Branch: "refactor/auth-jwt"
  Nickname: "JWT auth"

Output format (strict JSON only):
{
  "branch": "<branch-name>",
  "nickname": "<nickname>"
}

Output MUST be valid JSON only. No preamble or trailing text.

Here is the user's prompt:
<<<
{{USER_PROMPT}}
>>>
`

	branchSuggestTimeout = 30 * time.Second
)

var (
	ErrDisabled        = errors.New("branch suggestion is disabled")
	ErrNoPrompt        = errors.New("empty prompt provided")
	ErrTargetNotFound  = errors.New("branch suggestion target not found")
	ErrInvalidResponse = errors.New("invalid branch suggestion response")
	ErrInvalidBranch   = errors.New("invalid branch name")
)

// IsEnabled returns true if branch suggestion is enabled (has a configured target).
func IsEnabled(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return cfg.GetBranchSuggestTarget() != ""
}

// Result is the parsed branch suggestion response.
type Result struct {
	Branch   string `json:"branch"`
	Nickname string `json:"nickname"`
}

// AskForPrompt generates a branch name and nickname from a user prompt.
func AskForPrompt(ctx context.Context, cfg *config.Config, userPrompt string) (Result, error) {
	userPrompt = strings.TrimSpace(userPrompt)
	if userPrompt == "" {
		return Result{}, ErrNoPrompt
	}

	// Check if branch suggestion is disabled (empty target)
	targetName := ""
	if cfg != nil {
		targetName = cfg.GetBranchSuggestTarget()
	}
	if targetName == "" {
		return Result{}, ErrDisabled
	}

	input := strings.ReplaceAll(Prompt, "{{USER_PROMPT}}", userPrompt)

	response, err := oneshot.ExecuteTarget(ctx, cfg, targetName, input, branchSuggestTimeout)
	if err != nil {
		if errors.Is(err, oneshot.ErrTargetNotFound) {
			return Result{}, ErrTargetNotFound
		}
		return Result{}, fmt.Errorf("oneshot execute: %w", err)
	}

	result, err := ParseResult(response)
	if err != nil {
		return Result{}, err
	}

	return result, nil
}

// ParseResult extracts the first JSON object from a raw LLM response and parses it.
func ParseResult(raw string) (Result, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Result{}, ErrInvalidResponse
	}

	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "```json"))
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "```"))
	}

	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start == -1 || end == -1 || end <= start {
		return Result{}, ErrInvalidResponse
	}

	payload := trimmed[start : end+1]
	var result Result
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		payload = normalizeJSONPayload(payload)
		if payload == "" {
			return Result{}, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
		}
		if err := json.Unmarshal([]byte(payload), &result); err != nil {
			return Result{}, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
		}
	}

	// Validate the result has non-empty branch
	branch := strings.TrimSpace(result.Branch)
	if branch == "" {
		return Result{}, ErrInvalidResponse
	}
	if err := workspace.ValidateBranchName(branch); err != nil {
		return Result{}, ErrInvalidBranch
	}

	return result, nil
}

func normalizeJSONPayload(payload string) string {
	fixed := strings.TrimSpace(payload)
	if fixed == "" {
		return ""
	}
	fixed = strings.ReplaceAll(fixed, "\u201c", "\"")
	fixed = strings.ReplaceAll(fixed, "\u201d", "\"")
	fixed = strings.ReplaceAll(fixed, "\u2019", "'")
	fixed = strings.ReplaceAll(fixed, "\t", " ")
	for strings.Contains(fixed, "  ") {
		fixed = strings.ReplaceAll(fixed, "  ", " ")
	}
	fixed = strings.TrimSpace(fixed)
	return fixed
}
