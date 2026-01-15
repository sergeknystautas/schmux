package nudgenik

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sergek/schmux/internal/config"
	"github.com/sergek/schmux/internal/oneshot"
	"github.com/sergek/schmux/internal/state"
	"github.com/sergek/schmux/internal/tmux"
)

const (
	// Prompt is the NudgeNik prompt prefix.
	// Prompt = "Please tell me the status of this coding agent.  Does they need to test, need permission, need user feedback, need requirements clarified, or are they done?  (direct answer only, no meta commentary, no lists, concise):\n\n"
	Prompt = `
You are analyzing the last response from a coding agent.

Your task is to determine the agent’s current operational state based ONLY on that response.

Do NOT:
- continue development
- suggest next steps
- ask clarifying questions

Choose exactly ONE state from the list below:
- Needs Authorization
- Needs Feature Clarification
- Needs User Testing
- Completed

If multiple states appear applicable, choose the primary blocking or terminal state.

Output format (strict):
{
  "state": "<one of the states above>",
  "confidence": "<low|medium|high>",
  "evidence": ["<direct quotes or behaviors from the response>"],
  "summary": "<1 sentence explanation written WITHOUT referring to the agent, system, or model; start directly with the condition or state>"
}

Stylistic rules for "summary":
- Do NOT use the words "agent", "model", "system", or "it"
- Do NOT anthropomorphize
- Begin directly with the situation or state (e.g., "Implementation is complete…" not "The agent has completed…")

Here is the agent’s last response:
<<<
{{AGENT_LAST_RESPONSE}}
>>>
`

	defaultOneshotTimeout = 30 * time.Second
)

var (
	ErrNoResponse    = errors.New("no response extracted")
	ErrAgentNotFound = errors.New("claude agent not found")
)

// AskForSession captures the latest session output and asks NudgeNik for feedback.
func AskForSession(ctx context.Context, cfg *config.Config, sess state.Session) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, cfg.TmuxOperationTimeout())
	content, err := tmux.CaptureLastLines(timeoutCtx, sess.TmuxSession, 100)
	cancel()
	if err != nil {
		return "", fmt.Errorf("capture tmux session %s: %w", sess.ID, err)
	}

	content = tmux.StripAnsi(content)
	lines := strings.Split(content, "\n")
	extracted := tmux.ExtractLatestResponse(lines)
	if extracted == "" {
		return "", ErrNoResponse
	}

	input := Prompt + extracted

	timeoutCtx, cancel = context.WithTimeout(ctx, defaultOneshotTimeout)
	defer cancel()

	response, err := oneshot.Execute(timeoutCtx, "glm-4.7", "glm-4.7", input)
	if err != nil {
		return "", fmt.Errorf("oneshot execute: %w", err)
	}

	return response, nil
}
