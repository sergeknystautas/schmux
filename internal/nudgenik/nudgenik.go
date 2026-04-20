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

Your task is to determine the agent's current operational state based ONLY on that response.

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

Here is the agent's last response:
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
