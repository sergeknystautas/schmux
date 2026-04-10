package emergence

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/schema"
)

func init() {
	schema.Register(schema.LabelEmergenceCurator, EmergenceCuratorResponse{})
}

// IntentSignal represents a single user intent captured from event logs.
type IntentSignal struct {
	Text      string    `json:"text"`
	Timestamp time.Time `json:"ts"`
	Target    string    `json:"target,omitempty"`
	Persona   string    `json:"persona,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
	Session   string    `json:"session,omitempty"`
	Count     int       `json:"count"`
}

// EmergenceCuratorResponse is the expected JSON output from the emergence curator LLM.
type EmergenceCuratorResponse struct {
	NewSkills        []contracts.SkillProposal `json:"new_skills"`
	UpdatedSkills    []contracts.SkillProposal `json:"updated_skills"`
	DiscardedSignals map[string]string         `json:"discarded_signals"`
}

// BuildEmergencePrompt constructs the LLM prompt for distilling intent signals into skills.
func BuildEmergencePrompt(signals []IntentSignal, existingSkillNames []string, repoName string) string {
	var sb strings.Builder

	sb.WriteString(`You are a skill distiller for a multi-agent software development environment.

You will receive intent signals (user prompts from agent sessions) and a list of existing skills.
Your job is to identify recurring patterns and distill them into reusable skills.

Repository: `)
	sb.WriteString(repoName)
	sb.WriteString(`

Rules for distillation:
- CLUSTER: Group semantically similar intent signals together
- DISTILL: Turn each meaningful cluster into a skill with procedure, quality criteria, and triggers
- UPDATE: For existing skills, propose updates only if the signals reveal meaningfully different behavior
- DISCARD: One-off signals that don't form patterns should be discarded with a reason
- MINIMUM THRESHOLD: Require at least 3 signals per cluster to propose a new skill
- Each skill must be self-contained — understandable without the original signal context
- Name skills with lowercase-hyphenated names (e.g., "code-review", "deploy-staging")
- Triggers should be short phrases that match user intent

Output ONLY valid JSON matching the schema below, no markdown fencing:

{
  "new_skills": [
    {
      "name": "code-review",
      "description": "Review a pull request for code quality and correctness",
      "triggers": ["review this PR", "check this code"],
      "procedure": "1. Read the diff\n2. Check for bugs\n3. Leave comments",
      "quality_criteria": "All critical issues flagged, no false positives",
      "evidence": ["review this PR (5x)", "check code quality (3x)"],
      "confidence": 0.85,
      "is_update": false
    }
  ],
  "updated_skills": [
    {
      "name": "existing-skill",
      "description": "Updated description",
      "triggers": ["updated trigger"],
      "procedure": "Updated procedure",
      "quality_criteria": "Updated criteria",
      "evidence": ["new signal (2x)"],
      "confidence": 0.9,
      "is_update": true,
      "changes": "Added step 3 based on new usage pattern"
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

	sb.WriteString("\nEXISTING SKILLS (do NOT re-propose these):\n")
	if len(existingSkillNames) == 0 {
		sb.WriteString("(none)\n")
	} else {
		for _, name := range existingSkillNames {
			fmt.Fprintf(&sb, "- %s\n", name)
		}
	}

	return sb.String()
}

// ParseEmergenceResponse parses the LLM JSON response into an EmergenceCuratorResponse.
func ParseEmergenceResponse(response string) (*EmergenceCuratorResponse, error) {
	response = strings.TrimSpace(response)
	var result EmergenceCuratorResponse
	if err := json.Unmarshal([]byte(response), &result); err == nil {
		return &result, nil
	}
	// Fallback: strip markdown code fences and retry.
	stripped := stripCodeFences(response)
	if err := json.Unmarshal([]byte(stripped), &result); err != nil {
		return nil, fmt.Errorf("invalid emergence curator JSON: %w", err)
	}
	return &result, nil
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
