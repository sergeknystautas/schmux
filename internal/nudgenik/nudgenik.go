package nudgenik

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sergek/schmux/internal/config"
	"github.com/sergek/schmux/internal/detect"
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
	ErrNoResponse      = errors.New("no response extracted")
	ErrTargetNotFound  = errors.New("nudgenik target not found")
	ErrTargetNoSecrets = errors.New("nudgenik target missing required secrets")
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

	targetName := "claude"
	if cfg != nil {
		if configured := cfg.GetNudgenikTarget(); configured != "" {
			targetName = configured
		}
	}

	resolved, err := resolveNudgenikTarget(cfg, targetName)
	if err != nil {
		return "", err
	}
	if !resolved.Promptable {
		return "", fmt.Errorf("nudgenik target %s must be promptable", resolved.Name)
	}

	timeoutCtx, cancel = context.WithTimeout(ctx, defaultOneshotTimeout)
	defer cancel()

	var response string
	if resolved.Kind == targetKindUser {
		response, err = oneshot.ExecuteCommand(timeoutCtx, resolved.Command, input, resolved.Env)
	} else {
		response, err = oneshot.Execute(timeoutCtx, resolved.ToolName, resolved.Command, input, resolved.Env)
	}
	if err != nil {
		return "", fmt.Errorf("oneshot execute: %w", err)
	}

	return response, nil
}

type nudgenikTarget struct {
	Name       string
	Kind       string
	ToolName   string
	Command    string
	Promptable bool
	Env        map[string]string
}

const (
	targetKindDetected = "detected"
	targetKindVariant  = "variant"
	targetKindUser     = "user"
)

func resolveNudgenikTarget(cfg *config.Config, targetName string) (nudgenikTarget, error) {
	if cfg == nil {
		return nudgenikTarget{}, fmt.Errorf("%w: %s", ErrTargetNotFound, targetName)
	}

	for _, variant := range cfg.GetMergedVariants() {
		if variant.Name != targetName {
			continue
		}
		baseTarget, found := cfg.GetDetectedRunTarget(variant.BaseTool)
		if !found {
			return nudgenikTarget{}, fmt.Errorf("%w: %s", ErrTargetNotFound, targetName)
		}
		secrets, err := config.GetVariantSecrets(variant.Name)
		if err != nil {
			return nudgenikTarget{}, fmt.Errorf("failed to load secrets for variant %s: %w", variant.Name, err)
		}
		if err := ensureVariantSecrets(variant, secrets); err != nil {
			return nudgenikTarget{}, err
		}
		return nudgenikTarget{
			Name:       variant.Name,
			Kind:       targetKindVariant,
			ToolName:   variant.BaseTool,
			Command:    baseTarget.Command,
			Promptable: true,
			Env:        mergeEnvMaps(variant.Env, secrets),
		}, nil
	}

	if target, found := cfg.GetRunTarget(targetName); found {
		kind := targetKindUser
		toolName := ""
		if target.Source == config.RunTargetSourceDetected {
			kind = targetKindDetected
			toolName = target.Name
		}
		return nudgenikTarget{
			Name:       target.Name,
			Kind:       kind,
			ToolName:   toolName,
			Command:    target.Command,
			Promptable: target.Type == config.RunTargetTypePromptable,
		}, nil
	}

	return nudgenikTarget{}, fmt.Errorf("%w: %s", ErrTargetNotFound, targetName)
}

func mergeEnvMaps(base, overrides map[string]string) map[string]string {
	if base == nil && overrides == nil {
		return nil
	}
	out := make(map[string]string, len(base)+len(overrides))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}

func ensureVariantSecrets(variant detect.Variant, secrets map[string]string) error {
	for _, key := range variant.RequiredSecrets {
		val := strings.TrimSpace(secrets[key])
		if val == "" {
			return fmt.Errorf("%w: %s", ErrTargetNoSecrets, variant.Name)
		}
	}
	return nil
}
