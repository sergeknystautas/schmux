package flights

import (
	"strings"
	"testing"
)

const awkwardPrompt = "  Fix the <<<login>>> bug.\n\n```go\nfmt.Println(\"x\")\n```\n  trailing spaces  \n"

func TestPlannerPrompt(t *testing.T) {
	got := PlannerPrompt(awkwardPrompt, "/ws/.schmux/flights/fl-1/plan.md")

	for name, want := range map[string]string{
		"planner role":          "planner",
		"no implementation":     "do not implement",
		"brainstorming skill":   "superpowers:brainstorming",
		"verbatim prompt":       promptBlock(awkwardPrompt),
		"absolute artifact":     "/ws/.schmux/flights/fl-1/plan.md",
		"write the plan prompt": "Write the final implementation plan to",
	} {
		if !strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
			t.Errorf("planner prompt missing %s (%q) in:\n%s", name, want, got)
		}
	}
	// Spec order: role, skill, original prompt, then the artifact path.
	order := []string{"planner", "superpowers:brainstorming", beginPrompt, "/ws/.schmux/flights/fl-1/plan.md"}
	last := -1
	for _, s := range order {
		i := strings.Index(got, s)
		if i < last {
			t.Errorf("%q appears out of order in:\n%s", s, got)
		}
		last = i
	}
}

func TestExecutorPrompt(t *testing.T) {
	got := ExecutorPrompt(awkwardPrompt, "/ws/.schmux/flights/fl-1/plan.md")

	for name, want := range map[string]string{
		"implement instruction": "Implement the plan",
		"verbatim prompt":       promptBlock(awkwardPrompt),
		"absolute artifact":     "/ws/.schmux/flights/fl-1/plan.md",
		"read before editing":   "before editing",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("executor prompt missing %s (%q) in:\n%s", name, want, got)
		}
	}
	// Spec order: implement instruction, original prompt, then the artifact path.
	implement := strings.Index(got, "Implement the plan")
	prompt := strings.Index(got, beginPrompt)
	artifact := strings.LastIndex(got, "/ws/.schmux/flights/fl-1/plan.md")
	if !(implement < prompt && prompt < artifact) {
		t.Errorf("sections out of order (implement=%d prompt=%d artifact=%d) in:\n%s", implement, prompt, artifact, got)
	}
}

func TestPromptsReproduceThePromptByteForByte(t *testing.T) {
	for name, build := range map[string]func(string, string) string{"planner": PlannerPrompt, "executor": ExecutorPrompt} {
		t.Run(name, func(t *testing.T) {
			got := build(awkwardPrompt, "/p/plan.md")
			start := strings.Index(got, beginPrompt) + len(beginPrompt) + 1
			end := strings.Index(got, endPrompt)
			if start <= len(beginPrompt) || end < start {
				t.Fatalf("delimiters not found in:\n%s", got)
			}
			if body := got[start:end]; body != awkwardPrompt+"\n" {
				t.Errorf("delimited body = %q, want prompt followed by one newline %q", body, awkwardPrompt+"\n")
			}
		})
	}
}
