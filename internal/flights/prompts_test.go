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
		"structured decision": "Do not copy notification IDs",
		"restated contract":   "Every message schmux sends you restates this contract",
		"no intervention":     `{"guidance":null}`,
		"orchestrator":        "You are the flight's orchestrator.",
		"acts when prompted":  "whatever prompted the turn: a notification, a launch notice, or the user",
		"talks to the user":   "A final response that is not a JSON object is you talking to the user",
		"decision recipient":  `"to":"<role>"`,
		"recipient required":  "`to` names who receives the guidance and is required with guidance:",
		"any started session": "`planner`, `executor`, `reviewer`, or `commit`, whichever sessions the flight has started so far; every started session stays addressable in every later phase",
		"next reviewer form":  `"next_reviewer":"<what the previous reviewer changed and what to re-check>"`,
		"stop form":           `"stop":"<why this flight has gone off the rails>"`,
		"direct delivery":     "delivers guidance directly to that session's chat",
		"verbatim prompt":     promptBlock("do the thing"),
		"decides itself":      "Schmux tells you what happened and carries out your decisions; it does not decide for you.",
		"ends its turn":       "End your turn after each message, this one included, so schmux can read your decision.",
		"manual native":       "Native permission and question",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("pilot prompt missing %s (%q) in:\n%s", name, want, got)
		}
	}
}

func TestExecutorPrompt(t *testing.T) {
	got := ExecutorPrompt(awkwardPrompt, "/ws/docs/specs/thing.md", "/ws/docs/plans/thing.md", "/ws/docs/plans/thing-execution.md")

	for name, want := range map[string]string{
		"implement instruction": "Implement the plan",
		"execution skill":       "superpowers:executing-plans",
		"run the skill":         "Run the skill; do not describe or simulate it",
		"no subagents":          "Use no subagents",
		"no worktrees":          "create no worktrees",
		"no commits":            "make no commits",
		"no finishing skill":    "superpowers:finishing-a-development-branch",
		"report path":           "Report: /ws/docs/plans/thing-execution.md",
		"pilot attribution":     "[Pilot]",
		"verbatim prompt":       promptBlock(awkwardPrompt),
		"spec path":             "Spec: /ws/docs/specs/thing.md",
		"plan path":             "Plan: /ws/docs/plans/thing.md",
		"read before editing":   "Read both files before editing",
		"how to work":           executionDiscipline,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("executor prompt missing %s (%q) in:\n%s", name, want, got)
		}
	}
	// Without a verified report the executor is told to find the documents.
	none := ExecutorPrompt(awkwardPrompt, "", "", "/ws/review/fl-1-execution.md")
	for _, want := range []string{"did not report a verified spec and plan, so find them in the workspace", "Spec: (none recorded; the flight has no verified spec path)", "Plan: (none recorded; the flight has no verified plan path)"} {
		if !strings.Contains(none, want) {
			t.Errorf("executor prompt without a report lacks %q in:\n%s", want, none)
		}
	}
	// Order: role, rules, how to work, report, spec and plan paths, then the original prompt.
	order := []string{"You are the executor", "Use no subagents", "How to work:", "Report: /ws/docs/plans/thing-execution.md", "Spec: /ws/docs/specs/thing.md", beginPrompt}
	last := -1
	for _, s := range order {
		i := strings.Index(got, s)
		if i <= last {
			t.Errorf("%q appears out of order in:\n%s", s, got)
		}
		last = i
	}
}

func TestExecutionNotice(t *testing.T) {
	f := Flight{
		ID: "fl-1", SpecPath: "/ws/docs/specs/thing.md", PlanPath: "/ws/docs/plans/thing.md",
		ReportTarget: "/ws/docs/plans/thing-execution.md",
	}
	got := ExecutionNotice(f, "sess-3")

	for name, want := range map[string]string{
		"launch":           "An executor has now been launched for flight fl-1",
		"executor session": "sess-3",
		"report path":      "/ws/docs/plans/thing-execution.md",
		"recipients":       `guidance can go to it with "to": "executor"`,
		"no decision":      "needs no decision",
		"contract":         "No notification ID is needed or read",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("execution notice missing %s (%q) in:\n%s", name, want, got)
		}
	}
	// The planner does not check executed work: the notice sends nothing to
	// it and asks nothing of it. (The planner stays addressable, so the
	// restated decision contract may still list it as a recipient.)
	if strings.Contains(got, "to the planner") || strings.Contains(got, "planner checks") {
		t.Errorf("execution notice gives the planner a job while executing:\n%s", got)
	}
}

func TestPromptsReproduceThePromptByteForByte(t *testing.T) {
	for name, build := range map[string]func(string) string{
		"planner":  PlannerPrompt,
		"executor": func(p string) string { return ExecutorPrompt(p, "/p/spec.md", "/p/plan.md", "/p/plan-execution.md") },
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

func TestPlanningStepsIncludeAcceptanceCriteria(t *testing.T) {
	criteria := strings.Index(planningSteps, "Acceptance criteria")
	coherence := strings.Index(planningSteps, "spec-coherence-check")
	if criteria < 0 {
		t.Fatalf("planning steps have no acceptance-criteria step:\n%s", planningSteps)
	}
	if coherence < 0 {
		t.Fatalf("planning steps have no coherence check:\n%s", planningSteps)
	}
	if criteria > coherence {
		t.Errorf("the acceptance-criteria step must come before the coherence check:\n%s", planningSteps)
	}
	for name, want := range map[string]string{
		"checkable conditions": "a condition the change must satisfy to be accepted",
		"artifact paths":       "the exact path of each under review/",
		"proof commands":       "the repository's functional tests and checks that prove the change",
		"phase ownership":      "later flight phases own them",
		"plan boundary":        "The plan ends with implementation and the execution report",
	} {
		if !strings.Contains(planningSteps, want) {
			t.Errorf("planning steps missing %s (%q):\n%s", name, want, planningSteps)
		}
	}
	for _, forbidden := range []string{
		"Do not make workflow-owned steps—test-rules-review, review summaries, commit, finalize, or rebase—acceptance criteria",
		"omit test-rules-review, review-summary, commit, finalize, and rebase steps",
	} {
		if !strings.Contains(planningSteps, forbidden) {
			t.Errorf("planning steps missing workflow boundary %q:\n%s", forbidden, planningSteps)
		}
	}
	// The existing planning_artifacts classifier and the coherence step stay.
	if !strings.Contains(planningSteps, "run the spec-coherence-check skill on the spec") {
		t.Errorf("planning steps lost the coherence check:\n%s", planningSteps)
	}
}

func TestReviewerPrompt(t *testing.T) {
	f := Flight{
		ID: "fl-1", Repo: testRepo, Prompt: "do the thing",
		PlannerModel: "planner", ExecutorModel: "executor",
		WorkflowState: StateReviewing,
		Workspace:     &WorkspaceAssignment{WorkspaceID: "repo-1", Path: "/ws/fl-1"},
		SpecPath:      "/ws/fl-1/docs/specs/thing.md",
		PlanPath:      "/ws/fl-1/docs/plans/thing.md",
		ReportPath:    "/ws/fl-1/docs/plans/thing-execution.md",
		CreatedAt:     t0, UpdatedAt: t0,
	}
	got := ReviewerPrompt(f, "")

	for name, want := range map[string]string{
		"reviewer role":      "You are the reviewer",
		"as the author":      "as the plan's author would",
		"fix in place":       "fix, yourself and in place, every problem you find",
		"deviations":         "every deviation from the plan and say which and why",
		"do not start over":  "Do not start over",
		"reuse":              "reuse of existing code instead of copies",
		"repository pattern": "the repository's patterns and its style or review skills",
		"matching docs":      "docs that match the code",
		"behavioral tests":   "tests that test behavior",
		"acceptance":         "Verify every acceptance criterion in the spec",
		"own tests":          "Run the repository's tests and checks as the repository's own instructions define them",
		"rerun":              "After your last change, run them again",
		"actual results":     "Report each command with its actual result",
		"skill":              "the `test-rules-review` skill on the changed tests",
		"skill runs":         "Run the skill; do not describe or simulate it",
		"artifacts":          "Put every artifact the acceptance criteria name at its path under review/",
		"summary":            "/ws/fl-1/review/thing-review.html",
		"summary contents":   "each acceptance criterion with its evidence",
		"end changed":        "saying whether you changed anything",
		"no commit":          "Do not commit, push, rebase, or finalize",
		"no phase change":    "do not change the flight phase",
		"discipline":         executionDiscipline,
		"pilot attribution":  "[Pilot]",
		"spec path":          "/ws/fl-1/docs/specs/thing.md",
		"plan path":          "/ws/fl-1/docs/plans/thing.md",
		"report path":        "/ws/fl-1/docs/plans/thing-execution.md",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reviewer prompt missing %s (%q) in:\n%s", name, want, got)
		}
	}
	if strings.Contains(got, "no verified execution report exists") {
		t.Errorf("reviewer prompt claims no report while the flight has a verified one:\n%s", got)
	}
	// A later reviewer carries the pilot's paragraph; the first does not.
	if strings.Contains(got, "The previous reviewer") {
		t.Errorf("the first reviewer carries a next-reviewer paragraph:\n%s", got)
	}
}

// Without a verified execution report the reviewer is told so, in words,
// instead of being pointed at a path that may not exist.
func TestReviewerPromptWithoutVerifiedReport(t *testing.T) {
	f := Flight{
		ID: "fl-1", WorkflowState: StateReviewing,
		Workspace:  &WorkspaceAssignment{WorkspaceID: "repo-1", Path: "/ws/fl-1"},
		SpecPath:   "/ws/fl-1/docs/specs/thing.md",
		PlanPath:   "/ws/fl-1/docs/plans/thing.md",
		ReportPath: "",
	}
	got := ReviewerPrompt(f, "")

	if !strings.Contains(got, "no verified execution report exists") {
		t.Errorf("reviewer prompt does not say the report is unverified:\n%s", got)
	}
	if strings.Contains(got, "thing-execution.md") {
		t.Errorf("reviewer prompt names an execution report path that was never verified:\n%s", got)
	}
}

// A later reviewer gets the same prompt plus the pilot's paragraph.
func TestReviewerPromptCarriesTheNextReviewerContext(t *testing.T) {
	f := Flight{
		ID: "fl-1", WorkflowState: StateReviewing,
		Workspace: &WorkspaceAssignment{WorkspaceID: "repo-1", Path: "/ws/fl-1"},
		SpecPath:  "/ws/fl-1/docs/specs/thing.md",
		PlanPath:  "/ws/fl-1/docs/plans/thing.md",
	}
	got := ReviewerPrompt(f, "The previous reviewer changed the parser; re-check the parser and its tests.")

	if !strings.Contains(got, "The previous reviewer changed the parser; re-check the parser and its tests.") {
		t.Errorf("reviewer prompt missing the pilot's paragraph:\n%s", got)
	}
	if !strings.Contains(got, "Review the whole change again") {
		t.Errorf("reviewer prompt does not tell the later reviewer to re-review the whole change:\n%s", got)
	}
}

func TestReviewNotice(t *testing.T) {
	f := Flight{
		ID: "fl-1", WorkflowState: StateReviewing,
		Workspace: &WorkspaceAssignment{WorkspaceID: "repo-1", Path: "/ws/fl-1"},
	}
	got := ReviewNotice(f, "sess-4")

	for name, want := range map[string]string{
		"launch":           "A reviewer has now been launched for flight fl-1",
		"reviewer session": "sess-4",
		"recipient":        `"to": "reviewer"`,
		"no decision":      "needs no decision",
		"contract":         "No notification ID is needed or read",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("review notice missing %s (%q) in:\n%s", name, want, got)
		}
	}
}

func TestCommitterPrompt(t *testing.T) {
	f := Flight{
		ID: "fl-1", WorkflowState: StateCompleting,
		Workspace: &WorkspaceAssignment{WorkspaceID: "repo-1", Path: "/ws/fl-1"},
		SpecPath:  "/ws/fl-1/docs/specs/2026-10-19-thing-design.md",
		PlanPath:  "/ws/fl-1/docs/superpowers/plans/2026-10-19-thing.md",
	}
	got := CommitterPrompt(f)

	for name, want := range map[string]string{
		"role":              "You are the committer for this work.",
		"approved":          "The user has reviewed and approved this work.",
		"stage all":         "Stage every change except",
		"review excluded":   "review/",
		"spec path":         "/ws/fl-1/docs/specs/2026-10-19-thing-design.md",
		"plan path":         "/ws/fl-1/docs/superpowers/plans/2026-10-19-thing.md",
		"commit skill":      "commit",
		"gates once":        "Run its gates once per commit attempt.",
		"gate failure":      "If a gate fails, stop and report the actual output",
		"change nothing":    "change nothing",
		"no rerun":          "do not rerun the gates",
		"no revert":         "do not revert anything",
		"no suite restart":  "do not start or kill a running suite",
		"cleanup after":     "Only after the work commit succeeds, remove review/",
		"finalize":          "finalize",
		"explicit mode":     "--amend",
		"ignore unrelated":  "Ignore unrelated specs and plans.",
		"no asking":         "Make the standard consolidation choices without asking.",
		"amend":             "Amend the work commit",
		"commit hash":       "Report the final commit hash and the files it contains.",
		"no rebase":         "Do not rebase or push.",
		"pilot attribution": "[Pilot]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("committer prompt missing %s (%q) in:\n%s", name, want, got)
		}
	}
	// The cleanup order is load-bearing: review/ is removed only after the
	// work commit succeeds.
	commit := strings.Index(got, "the work commit succeeds")
	remove := strings.Index(got, "remove review/")
	if commit < 0 || remove < 0 || remove < commit {
		t.Errorf("the prompt does not remove review/ after the work commit succeeds:\n%s", got)
	}
	// The finalize step names the exact documents.
	if !strings.Contains(got, "--spec /ws/fl-1/docs/specs/2026-10-19-thing-design.md") ||
		!strings.Contains(got, "--plan /ws/fl-1/docs/superpowers/plans/2026-10-19-thing.md") {
		t.Errorf("the finalize step does not name the exact documents:\n%s", got)
	}
}
