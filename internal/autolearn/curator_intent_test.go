//go:build !noautolearn

package autolearn

import (
	"strings"
	"testing"
)

func TestIntentBuildPrompt_ContainsKeySections(t *testing.T) {
	signals := []IntentSignal{
		{Text: "review this PR", Count: 5, Workspace: "ws-1"},
		{Text: "fix the tests", Count: 3, Workspace: "ws-2"},
		{Text: "deploy to staging", Count: 2, Workspace: "ws-1"},
	}
	prompt := BuildIntentPrompt(signals, []string{"commit-helper"}, []string{"old-rule"}, "my-repo")

	checks := map[string]string{
		"learning distiller": "role description",
		"review this PR":     "first signal",
		"fix the tests":      "second signal",
		"commit-helper":      "existing titles",
		"old-rule":           "dismissed titles",
		"new_learnings":      "output schema",
		"my-repo":            "repo name",
	}
	for substr, desc := range checks {
		if !strings.Contains(prompt, substr) {
			t.Errorf("prompt should contain %s (%q)", desc, substr)
		}
	}
}

func TestIntentBuildPrompt_EmptySignals(t *testing.T) {
	prompt := BuildIntentPrompt(nil, nil, nil, "test-repo")
	if prompt == "" {
		t.Error("prompt should not be empty even with no signals")
	}
	if !strings.Contains(prompt, "INTENT SIGNALS") {
		t.Error("prompt should still have signals section header")
	}
}

func TestIntentBuildPrompt_IncludesExistingTitles(t *testing.T) {
	existing := []string{"deploy-staging", "run-tests"}
	prompt := BuildIntentPrompt(nil, existing, nil, "test-repo")
	if !strings.Contains(prompt, "EXISTING LEARNINGS") {
		t.Error("prompt should contain existing learnings section")
	}
	if !strings.Contains(prompt, "deploy-staging") {
		t.Error("prompt should list existing titles")
	}
	if !strings.Contains(prompt, "run-tests") {
		t.Error("prompt should list all existing titles")
	}
}

func TestIntentBuildPrompt_IncludesDismissedTitles(t *testing.T) {
	dismissed := []string{"one-off-thing", "not-useful"}
	prompt := BuildIntentPrompt(nil, nil, dismissed, "test-repo")
	if !strings.Contains(prompt, "DISMISSED LEARNINGS") {
		t.Error("prompt should contain dismissed learnings section")
	}
	if !strings.Contains(prompt, "one-off-thing") {
		t.Error("prompt should list dismissed titles")
	}
	if !strings.Contains(prompt, "not-useful") {
		t.Error("prompt should list all dismissed titles")
	}
}

func TestIntentBuildPrompt_ShowsNoneWhenEmpty(t *testing.T) {
	prompt := BuildIntentPrompt(nil, nil, nil, "test-repo")
	count := strings.Count(prompt, "(none)")
	if count < 3 {
		t.Errorf("prompt should show (none) for all three empty sections, got %d occurrences", count)
	}
}

func TestIntentParseResponse_ValidJSON(t *testing.T) {
	input := `{
		"new_learnings": [
			{
				"kind": "skill",
				"title": "code-review",
				"category": "development",
				"suggested_layer": "repo_public",
				"skill": {
					"triggers": ["review PR"],
					"procedure": "1. Read diff",
					"quality_criteria": "No bugs missed",
					"confidence": 0.85
				}
			}
		],
		"updated_learnings": [],
		"discarded_signals": {"one-off": "only seen once"}
	}`
	resp, err := ParseIntentResponse(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.NewLearnings) != 1 {
		t.Fatalf("expected 1 new learning, got %d", len(resp.NewLearnings))
	}
	if resp.NewLearnings[0].Title != "code-review" {
		t.Errorf("expected title 'code-review', got %q", resp.NewLearnings[0].Title)
	}
	if resp.NewLearnings[0].Kind != KindSkill {
		t.Errorf("expected kind 'skill', got %q", resp.NewLearnings[0].Kind)
	}
	if resp.NewLearnings[0].Skill == nil {
		t.Fatal("expected skill details to be non-nil")
	}
	if resp.NewLearnings[0].Skill.Confidence != 0.85 {
		t.Errorf("expected confidence 0.85, got %f", resp.NewLearnings[0].Skill.Confidence)
	}
	if len(resp.DiscardedSignals) != 1 {
		t.Errorf("expected 1 discarded signal, got %d", len(resp.DiscardedSignals))
	}
}

func TestIntentParseResponse_CodeFenced(t *testing.T) {
	input := "Here are the results:\n```json\n{\"new_learnings\": [], \"updated_learnings\": [], \"discarded_signals\": {}}\n```\n"
	resp, err := ParseIntentResponse(input)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestIntentParseResponse_ProseWrapped(t *testing.T) {
	input := `After analyzing the intent signals, here is my assessment:

{"new_learnings": [], "updated_learnings": [], "discarded_signals": {"deploy to staging": "only seen once"}}

I hope this helps with the distillation process.`
	resp, err := ParseIntentResponse(input)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.DiscardedSignals) != 1 {
		t.Errorf("expected 1 discarded signal, got %d", len(resp.DiscardedSignals))
	}
}

func TestIntentParseResponse_Malformed(t *testing.T) {
	_, err := ParseIntentResponse("this is not json")
	if err == nil {
		t.Error("expected error for malformed input")
	}
}
