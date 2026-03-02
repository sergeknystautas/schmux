package lore

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/actions"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

func TestBuildActionCuratorPrompt_Empty(t *testing.T) {
	prompt := BuildActionCuratorPrompt(nil, nil)
	if prompt == "" {
		t.Error("expected non-empty prompt even with no inputs")
	}
	if !contains(prompt, "INTENT SIGNALS") {
		t.Error("prompt should contain INTENT SIGNALS header")
	}
}

func TestBuildActionCuratorPrompt_WithSignals(t *testing.T) {
	signals := []actions.IntentSignal{
		{Text: "fix lint errors in src/", Count: 3, Target: "sonnet", Persona: "code-engineer"},
		{Text: "add tests for auth", Count: 2},
	}
	prompt := BuildActionCuratorPrompt(nil, signals)

	if !contains(prompt, `"fix lint errors in src/"`) {
		t.Error("prompt should contain signal text")
	}
	if !contains(prompt, "×3") {
		t.Error("prompt should contain signal count")
	}
	if !contains(prompt, "target: sonnet") {
		t.Error("prompt should contain target")
	}
	if !contains(prompt, "persona: code-engineer") {
		t.Error("prompt should contain persona")
	}
}

func TestBuildActionCuratorPrompt_WithExistingActions(t *testing.T) {
	existing := []contracts.Action{
		{Name: "Fix lint", State: contracts.ActionStatePinned, Template: "Fix lint errors in {{path}}"},
		{Name: "Deploy", State: contracts.ActionStatePinned, Command: "make deploy"},
	}
	signals := []actions.IntentSignal{
		{Text: "run tests", Count: 2},
	}
	prompt := BuildActionCuratorPrompt(existing, signals)

	if !contains(prompt, "CURRENT ACTIONS") {
		t.Error("prompt should contain CURRENT ACTIONS header")
	}
	if !contains(prompt, "[pinned] Fix lint") {
		t.Error("prompt should list existing actions with state")
	}
	if !contains(prompt, "command=") {
		t.Error("prompt should show command for command actions")
	}
}

func TestParseActionCuratorResponse_Valid(t *testing.T) {
	response := `{
		"proposed_actions": [
			{
				"name": "Fix lint errors",
				"template": "Fix all lint errors in {{path}}",
				"parameters": [{"name": "path", "default": "src/"}],
				"learned_defaults": {
					"target": {"value": "sonnet", "confidence": 0.8}
				},
				"evidence_keys": ["fix lint errors in src/", "fix lint errors in lib/"]
			}
		],
		"entries_discarded": {"one-off task": "only appeared once"}
	}`

	result, err := ParseActionCuratorResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ProposedActions) != 1 {
		t.Fatalf("expected 1 proposed action, got %d", len(result.ProposedActions))
	}
	if result.ProposedActions[0].Name != "Fix lint errors" {
		t.Errorf("name = %q", result.ProposedActions[0].Name)
	}
	if result.ProposedActions[0].Template != "Fix all lint errors in {{path}}" {
		t.Errorf("template = %q", result.ProposedActions[0].Template)
	}
	if len(result.ProposedActions[0].Parameters) != 1 {
		t.Errorf("expected 1 parameter, got %d", len(result.ProposedActions[0].Parameters))
	}
	if len(result.ProposedActions[0].EvidenceKeys) != 2 {
		t.Errorf("expected 2 evidence keys, got %d", len(result.ProposedActions[0].EvidenceKeys))
	}
	if len(result.EntriesDiscarded) != 1 {
		t.Errorf("expected 1 discarded entry, got %d", len(result.EntriesDiscarded))
	}
}

func TestParseActionCuratorResponse_WithFencing(t *testing.T) {
	response := "```json\n" + `{"proposed_actions": [], "entries_discarded": {}}` + "\n```"
	result, err := ParseActionCuratorResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ProposedActions) != 0 {
		t.Errorf("expected 0 proposed actions, got %d", len(result.ProposedActions))
	}
}

func TestParseActionCuratorResponse_Invalid(t *testing.T) {
	_, err := ParseActionCuratorResponse("not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestConvertProposedActions(t *testing.T) {
	proposed := []ProposedAction{
		{
			Name:     "Fix lint",
			Template: "Fix lint errors in {{path}}",
			Parameters: []contracts.ActionParameter{
				{Name: "path", Default: "src/"},
			},
			LearnedDefaults: map[string]contracts.LearnedDefault{
				"target":  {Value: "sonnet", Confidence: 0.8},
				"persona": {Value: "code-engineer", Confidence: 0.6},
			},
			EvidenceKeys: []string{"fix lint in src/", "fix lint in lib/"},
		},
	}

	result := ConvertProposedActions(proposed)
	if len(result) != 1 {
		t.Fatalf("expected 1 action, got %d", len(result))
	}

	a := result[0]
	if a.Name != "Fix lint" {
		t.Errorf("name = %q", a.Name)
	}
	if a.Type != contracts.ActionTypeAgent {
		t.Errorf("type = %q", a.Type)
	}
	if a.Template != "Fix lint errors in {{path}}" {
		t.Errorf("template = %q", a.Template)
	}
	if a.Confidence != 0.5 {
		t.Errorf("confidence = %f, want 0.5", a.Confidence)
	}
	if a.Target != "sonnet" {
		t.Errorf("target = %q, want sonnet", a.Target)
	}
	if a.LearnedTarget == nil || a.LearnedTarget.Value != "sonnet" {
		t.Error("expected learned target")
	}
	if a.LearnedPersona == nil || a.LearnedPersona.Value != "code-engineer" {
		t.Error("expected learned persona")
	}
	if a.EvidenceCount != 2 {
		t.Errorf("evidence_count = %d, want 2", a.EvidenceCount)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
