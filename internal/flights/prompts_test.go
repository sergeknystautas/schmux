package flights

import (
	"strings"
	"testing"
)

const awkwardPrompt = "  Fix the <<<login>>> bug.\n\n```go\nfmt.Println(\"x\")\n```\n  trailing spaces  \n"

func TestPlannerPrompt(t *testing.T) {
	got := PlannerPrompt(awkwardPrompt)

	for name, want := range map[string]string{
		"planner role":       "planner",
		"no implementation":  "do not implement",
		"planning steps":     planningSteps,
		"questions to user":  "Work through every open question with the user",
		"spikes throwaway":   "spike code is throwaway",
		"review artifacts":   "Reviewable artifacts go under review/.",
		"coherence check":    "run the spec-coherence-check skill on the spec",
		"check loop":         "run the check again",
		"plan after check":   "Only once the spec is coherent, write the plan with the superpowers:writing-plans skill.",
		"artifact approval":  "the user has approved any generated review artifacts",
		"skills pick places": "The brainstorming and writing-plans skills decide where the spec and plan go; follow them and do not choose a location yourself.",
		"spec report line":   "\nSpec: <absolute path>\n",
		"plan report line":   "\nPlan: <absolute path>\n",
		"verbatim prompt":    promptBlock(awkwardPrompt),
		"pilot attribution":  "[Pilot]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("planner prompt missing %s (%q) in:\n%s", name, want, got)
		}
	}
	// Schmux never dictates where the planning documents go.
	for _, banned := range []string{".schmux", "plan.md", "Write the final implementation plan to"} {
		if strings.Contains(got, banned) {
			t.Errorf("planner prompt names a location (%q) in:\n%s", banned, got)
		}
	}
	// Order: role, brainstorm the spec, coherence check, then the plan, the
	// report format, and finally the original prompt.
	order := []string{"planner", "superpowers:brainstorming", "spec-coherence-check", "superpowers:writing-plans", "Spec: <absolute path>", beginPrompt}
	last := -1
	for _, s := range order {
		i := strings.Index(got, s)
		if i < last {
			t.Errorf("%q appears out of order in:\n%s", s, got)
		}
		last = i
	}
}

func TestPilotPrompt(t *testing.T) {
	f := Flight{
		ID: "fl-1", Repo: testRepo, Prompt: "do the thing",
		PlannerModel: "planner", ExecutorModel: "executor",
		WorkflowState: StatePlanning,
		Workspace:     &WorkspaceAssignment{WorkspaceID: "repo-1", Path: "/ws/fl-1"},
		CreatedAt:     t0, UpdatedAt: t0,
	}
	got := PilotPrompt(f)

	for name, want := range map[string]string{
		"pilot role":          "You are the pilot",
		"flight id":           "Flight ID: fl-1",
		"workspace path":      "Workspace: /ws/fl-1",
		"planner target":      "Planner role target: planner",
		"skill placement":     "where the superpowers:brainstorming and superpowers:writing-plans skills place them",
		"planning steps":      planningSteps,
		"holds the steps":     "Hold the planner to the planning steps.",
		"skipped check":       "a plan written before the spec-coherence-check skill ran on the spec",
		"report lines":        "`Spec:` and `Plan:` lines",
		"do not edit":         "do not edit the spec or plan",
		"no disposal":         "do not dispose the flight",
		"structured decision": `"notification_id"`,
		"no intervention":     `{"guidance":null}`,
		"direct delivery":     "delivers guidance directly to the planner's chat",
		"verbatim prompt":     promptBlock("do the thing"),
		"notification wait":   "Wait for a monitor notification",
		"manual native":       "Native permission and question",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("pilot prompt missing %s (%q) in:\n%s", name, want, got)
		}
	}
}

func TestExecutorPrompt(t *testing.T) {
	got := ExecutorPrompt(awkwardPrompt, "/ws/docs/specs/thing.md", "/ws/docs/plans/thing.md")

	for name, want := range map[string]string{
		"implement instruction": "Implement the plan",
		"verbatim prompt":       promptBlock(awkwardPrompt),
		"spec path":             "Spec: /ws/docs/specs/thing.md",
		"plan path":             "Plan: /ws/docs/plans/thing.md",
		"read before editing":   "Read both files before editing",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("executor prompt missing %s (%q) in:\n%s", name, want, got)
		}
	}
	// Order: implement instruction, original prompt, then the documents.
	implement := strings.Index(got, "Implement the plan")
	prompt := strings.Index(got, beginPrompt)
	plan := strings.LastIndex(got, "/ws/docs/plans/thing.md")
	if !(implement < prompt && prompt < plan) {
		t.Errorf("sections out of order (implement=%d prompt=%d plan=%d) in:\n%s", implement, prompt, plan, got)
	}
}

func TestPromptsReproduceThePromptByteForByte(t *testing.T) {
	for name, build := range map[string]func(string) string{
		"planner":  PlannerPrompt,
		"executor": func(p string) string { return ExecutorPrompt(p, "/p/spec.md", "/p/plan.md") },
	} {
		t.Run(name, func(t *testing.T) {
			got := build(awkwardPrompt)
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
