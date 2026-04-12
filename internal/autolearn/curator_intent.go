//go:build !noautolearn

package autolearn

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/internal/schema"
)

func init() {
	schema.Register(schema.LabelAutolearnIntent, IntentCuratorResponse{})
}

// IntentCuratorResponse is the expected JSON output from the intent curator LLM.
type IntentCuratorResponse struct {
	NewLearnings     []Learning        `json:"new_learnings"`
	UpdatedLearnings []Learning        `json:"updated_learnings"`
	DiscardedSignals map[string]string `json:"discarded_signals"`
}

// BuildIntentPrompt constructs the LLM prompt for distilling intent signals into learnings.
func BuildIntentPrompt(signals []IntentSignal, existingTitles []string, dismissedTitles []string, repoName string) string {
	var sb strings.Builder

	sb.WriteString(`You are a learning distiller for a multi-agent software development environment.

You will receive intent signals (user prompts from agent sessions) and lists of existing/dismissed learnings.
Your job is to identify recurring patterns and distill them into reusable learnings.

Repository: `)
	sb.WriteString(repoName)
	sb.WriteString(`

Rules for distillation:
- CLUSTER: Group semantically similar intent signals together
- DISTILL: Turn each meaningful cluster into a learning with kind, title, category, and suggested_layer
- UPDATE: For existing learnings, propose updates only if the signals reveal meaningfully different behavior
- DISCARD: One-off signals that don't form patterns should be discarded with a reason
- MINIMUM THRESHOLD: Require at least 3 signals per cluster to propose a new learning
- Each learning must be self-contained — understandable without the original signal context
- You may output rules if you notice users repeatedly working around a footgun
- You may output skills if you notice recurring multi-step workflows
- Use lowercase-hyphenated titles (e.g., "code-review", "deploy-staging")
- For skills: include triggers, procedure, quality_criteria, confidence
- For rules: include a concise rule description in the title

Output ONLY valid JSON matching the schema below, no markdown fencing:

{
  "new_learnings": [
    {
      "kind": "skill",
      "title": "code-review",
      "category": "development",
      "suggested_layer": "repo_public",
      "skill": {
        "triggers": ["review this PR", "check this code"],
        "procedure": "1. Read the diff\n2. Check for bugs\n3. Leave comments",
        "quality_criteria": "All critical issues flagged, no false positives",
        "confidence": 0.85
      }
    },
    {
      "kind": "rule",
      "title": "always-run-tests-before-push",
      "category": "workflow",
      "suggested_layer": "repo_public"
    }
  ],
  "updated_learnings": [
    {
      "kind": "skill",
      "title": "existing-skill",
      "category": "development",
      "suggested_layer": "repo_public",
      "skill": {
        "triggers": ["updated trigger"],
        "procedure": "Updated procedure",
        "quality_criteria": "Updated criteria",
        "confidence": 0.9,
        "is_update": true,
        "changes": "Added step 3 based on new usage pattern"
      }
    }
  ],
  "discarded_signals": {
    "one-off signal text": "reason for discarding"
  }
}

`)

	sb.WriteString("INTENT SIGNALS:\n")
	if len(signals) == 0 {
		sb.WriteString("(none)\n")
	} else {
		for _, s := range signals {
			fmt.Fprintf(&sb, "- [%dx] [%s] %s\n", s.Count, s.Workspace, s.Text)
		}
	}

	sb.WriteString("\nEXISTING LEARNINGS (do NOT re-propose these):\n")
	if len(existingTitles) == 0 {
		sb.WriteString("(none)\n")
	} else {
		for _, title := range existingTitles {
			fmt.Fprintf(&sb, "- %s\n", title)
		}
	}

	sb.WriteString("\nDISMISSED LEARNINGS (do NOT re-propose these):\n")
	if len(dismissedTitles) == 0 {
		sb.WriteString("(none)\n")
	} else {
		for _, title := range dismissedTitles {
			fmt.Fprintf(&sb, "- %s\n", title)
		}
	}

	return sb.String()
}

// ParseIntentResponse parses the LLM JSON response into an IntentCuratorResponse.
func ParseIntentResponse(response string) (*IntentCuratorResponse, error) {
	response = strings.TrimSpace(response)
	var result IntentCuratorResponse
	if err := json.Unmarshal([]byte(response), &result); err == nil {
		return &result, nil
	}
	// Fallback: strip markdown code fences and retry.
	stripped := stripCodeFences(response)
	if err := json.Unmarshal([]byte(stripped), &result); err == nil {
		return &result, nil
	}
	// Fallback: extract outermost JSON object from prose-wrapped response.
	if start := strings.Index(response, "{"); start >= 0 {
		if end := strings.LastIndex(response, "}"); end > start {
			if err := json.Unmarshal([]byte(response[start:end+1]), &result); err == nil {
				return &result, nil
			}
		}
	}
	return nil, fmt.Errorf("invalid intent curator JSON: no valid JSON object found in response")
}

// stripCodeFences removes markdown code fences from an LLM response.
func stripCodeFences(response string) string {
	response = strings.TrimSpace(response)
	fenceStart := strings.Index(response, "```")
	if fenceStart >= 0 {
		afterFence := response[fenceStart:]
		firstNewline := strings.Index(afterFence, "\n")
		if firstNewline >= 0 {
			afterFence = afterFence[firstNewline+1:]
		}
		lastFence := strings.LastIndex(afterFence, "\n```")
		if lastFence >= 0 {
			response = afterFence[:lastFence]
		} else {
			response = afterFence
		}
	}
	return response
}
