package lore

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/internal/actions"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// ActionCuratorResponse is the expected JSON output from the action curator LLM.
type ActionCuratorResponse struct {
	ProposedActions  []ProposedAction  `json:"proposed_actions"`
	EntriesDiscarded map[string]string `json:"entries_discarded"`
}

// ProposedAction represents a single action proposal from the curator.
type ProposedAction struct {
	Name            string                              `json:"name"`
	Template        string                              `json:"template"`
	Parameters      []contracts.ActionParameter         `json:"parameters,omitempty"`
	LearnedDefaults map[string]contracts.LearnedDefault `json:"learned_defaults,omitempty"`
	EvidenceKeys    []string                            `json:"evidence_keys"`
}

// BuildActionCuratorPrompt constructs the LLM prompt for curating intent signals
// into proposed actions.
func BuildActionCuratorPrompt(existingActions []contracts.Action, signals []actions.IntentSignal) string {
	var sb strings.Builder
	sb.WriteString(`You are an action curator for a multi-agent development environment.

You observe what users repeatedly ask their AI agents to do and propose
reusable actions that can be triggered with one click.

Rules:
- SYNTHESIZE: Group similar intents into parameterized templates
  (e.g., "fix lint errors in src/" + "fix lint errors in lib/" → "Fix lint errors in {{path}}")
- DEDUPLICATE: Don't propose actions that already exist
- FILTER: Discard one-off intents that don't indicate repeated patterns (count=1 is usually noise)
- NAME: Give actions short, imperative names ("Fix lint errors", "Run tests")
- TEMPLATE: Use {{param}} syntax for variable parts
- PARAMETERS: Define parameters with sensible defaults from the most common usage
- LEARNED DEFAULTS: If a signal has a clear target or persona, include as learned_defaults
- Output ONLY valid JSON matching the schema below, no markdown fencing

Output schema:
{
  "proposed_actions": [
    {
      "name": "Short imperative name",
      "template": "Full prompt with {{param}} placeholders",
      "parameters": [{"name": "param", "default": "most_common_value"}],
      "learned_defaults": {
        "target": {"value": "sonnet", "confidence": 0.8},
        "persona": {"value": "code-engineer", "confidence": 0.6}
      },
      "evidence_keys": ["exact intent text used as evidence"]
    }
  ],
  "entries_discarded": {"intent text": "reason for discarding"}
}

`)

	// List existing actions to avoid duplicates.
	if len(existingActions) > 0 {
		sb.WriteString("CURRENT ACTIONS:\n")
		for _, a := range existingActions {
			state := string(a.State)
			if a.Template != "" {
				fmt.Fprintf(&sb, "- [%s] %s: %q\n", state, a.Name, a.Template)
			} else if a.Command != "" {
				fmt.Fprintf(&sb, "- [%s] %s: command=%q\n", state, a.Name, a.Command)
			} else {
				fmt.Fprintf(&sb, "- [%s] %s\n", state, a.Name)
			}
		}
		sb.WriteString("\n")
	}

	// List intent signals.
	sb.WriteString("INTENT SIGNALS (what users have been asking):\n")
	for _, sig := range signals {
		fmt.Fprintf(&sb, "- %q (×%d", sig.Text, sig.Count)
		if sig.Target != "" {
			fmt.Fprintf(&sb, ", target: %s", sig.Target)
		}
		if sig.Persona != "" {
			fmt.Fprintf(&sb, ", persona: %s", sig.Persona)
		}
		sb.WriteString(")\n")
	}

	return sb.String()
}

// ParseActionCuratorResponse parses the LLM JSON response into an ActionCuratorResponse.
func ParseActionCuratorResponse(response string) (*ActionCuratorResponse, error) {
	// Strip markdown fencing if present.
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "```") {
		firstNewline := strings.Index(response, "\n")
		if firstNewline >= 0 {
			response = response[firstNewline+1:]
		}
		lastFence := strings.LastIndex(response, "\n```")
		if lastFence >= 0 {
			response = response[:lastFence]
		}
	}

	var result ActionCuratorResponse
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, fmt.Errorf("invalid action curator JSON: %w", err)
	}
	return &result, nil
}

// ConvertProposedActions converts curator-proposed actions into contracts.Action
// values ready for AddProposed.
func ConvertProposedActions(proposed []ProposedAction) []contracts.Action {
	result := make([]contracts.Action, 0, len(proposed))
	for _, p := range proposed {
		a := contracts.Action{
			Name:       p.Name,
			Type:       contracts.ActionTypeAgent,
			Scope:      "repo",
			Template:   p.Template,
			Parameters: p.Parameters,
			Confidence: 0.5,
		}

		// Apply learned defaults for target and persona.
		if ld, ok := p.LearnedDefaults["target"]; ok {
			a.LearnedTarget = &ld
			a.Target = ld.Value
		}
		if ld, ok := p.LearnedDefaults["persona"]; ok {
			a.LearnedPersona = &ld
			a.Persona = ld.Value
		}

		a.EvidenceCount = len(p.EvidenceKeys)
		result = append(result, a)
	}
	return result
}
