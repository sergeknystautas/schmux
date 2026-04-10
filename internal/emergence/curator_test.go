package emergence

import (
	"strings"
	"testing"
)

func TestBuildEmergencePrompt_ContainsKeySections(t *testing.T) {
	signals := []IntentSignal{
		{Text: "review this PR", Count: 5},
		{Text: "fix the tests", Count: 3},
		{Text: "deploy to staging", Count: 2},
	}
	prompt := BuildEmergencePrompt(signals, []string{"commit"}, "my-repo")

	// Verify key sections exist
	if !strings.Contains(prompt, "skill distiller") {
		t.Error("prompt should contain role description")
	}
	if !strings.Contains(prompt, "review this PR") {
		t.Error("prompt should contain intent signals")
	}
	if !strings.Contains(prompt, "fix the tests") {
		t.Error("prompt should contain all signals")
	}
	if !strings.Contains(prompt, "commit") {
		t.Error("prompt should reference existing skills")
	}
	if !strings.Contains(prompt, "new_skills") {
		t.Error("prompt should contain output schema")
	}
	if !strings.Contains(prompt, "my-repo") {
		t.Error("prompt should reference repo name")
	}
}

func TestBuildEmergencePrompt_EmptySignals(t *testing.T) {
	prompt := BuildEmergencePrompt(nil, nil, "test-repo")
	if prompt == "" {
		t.Error("prompt should not be empty even with no signals")
	}
	if !strings.Contains(prompt, "INTENT SIGNALS") {
		t.Error("prompt should still have signals section header")
	}
}

func TestBuildEmergencePrompt_IncludesExistingSkills(t *testing.T) {
	existing := []string{"deploy-staging", "run-tests"}
	prompt := BuildEmergencePrompt(nil, existing, "test-repo")
	if !strings.Contains(prompt, "EXISTING SKILLS") {
		t.Error("prompt should contain existing skills section")
	}
	if !strings.Contains(prompt, "deploy-staging") {
		t.Error("prompt should list existing skill names")
	}
	if !strings.Contains(prompt, "run-tests") {
		t.Error("prompt should list all existing skill names")
	}
}

func TestBuildEmergencePrompt_ShowsNoneWhenNoSkills(t *testing.T) {
	prompt := BuildEmergencePrompt(nil, nil, "test-repo")
	if !strings.Contains(prompt, "(none)") {
		t.Error("prompt should show (none) when no existing skills")
	}
}

func TestParseEmergenceResponse_ValidJSON(t *testing.T) {
	input := `{
		"new_skills": [
			{
				"name": "code-review",
				"description": "Review PRs",
				"triggers": ["review PR"],
				"procedure": "1. Read diff",
				"quality_criteria": "No bugs missed",
				"evidence": ["review PR (5x)"],
				"confidence": 0.85,
				"is_update": false
			}
		],
		"updated_skills": [],
		"discarded_signals": {"one-off": "only seen once"}
	}`
	resp, err := ParseEmergenceResponse(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.NewSkills) != 1 {
		t.Fatalf("expected 1 new skill, got %d", len(resp.NewSkills))
	}
	if resp.NewSkills[0].Name != "code-review" {
		t.Errorf("expected name 'code-review', got %q", resp.NewSkills[0].Name)
	}
	if resp.NewSkills[0].Confidence != 0.85 {
		t.Errorf("expected confidence 0.85, got %f", resp.NewSkills[0].Confidence)
	}
	if len(resp.DiscardedSignals) != 1 {
		t.Errorf("expected 1 discarded signal, got %d", len(resp.DiscardedSignals))
	}
}

func TestParseEmergenceResponse_CodeFenced(t *testing.T) {
	input := "Here are the results:\n```json\n{\"new_skills\": [], \"updated_skills\": [], \"discarded_signals\": {}}\n```\n"
	resp, err := ParseEmergenceResponse(input)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestParseEmergenceResponse_Malformed(t *testing.T) {
	_, err := ParseEmergenceResponse("this is not json")
	if err == nil {
		t.Error("expected error for malformed input")
	}
}
