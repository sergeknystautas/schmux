package flights

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecutingPlansEvidence(t *testing.T) {
	turn := func(session string, skills ...SkillUse) MonitoredTurn {
		return MonitoredTurn{Turn: PlannerTurn{SessionID: session, Skills: skills}}
	}
	claude := SkillUse{Skill: "superpowers:executing-plans", Seq: 21, Evidence: "invoked"}
	codex := SkillUse{Skill: "executing-plans", Seq: 54, Evidence: "read"}
	other := SkillUse{Skill: "superpowers:brainstorming", Seq: 3, Evidence: "invoked"}
	for _, tc := range []struct {
		name  string
		turns []MonitoredTurn
		want  string
	}{
		{"claude invocation", []MonitoredTurn{turn("exec", claude)}, "executing-plans invoked at record 21"},
		{"codex file read", []MonitoredTurn{turn("exec", codex)}, "executing-plans skill file read at record 54"},
		{"other skill only", []MonitoredTurn{turn("exec", other)}, "executing-plans not found"},
		{"no skills", []MonitoredTurn{turn("exec")}, "executing-plans not found"},
		{"session outside the executor", []MonitoredTurn{turn("planner", claude)}, "executing-plans not found"},
		{"earliest wins", []MonitoredTurn{
			turn("exec", SkillUse{Skill: "superpowers:executing-plans", Seq: 90, Evidence: "invoked"}),
			turn("exec", SkillUse{Skill: "superpowers:executing-plans", Seq: 40, Evidence: "invoked"}),
		}, "executing-plans invoked at record 40"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := executingPlansEvidence(tc.turns, map[string]bool{"exec": true}); got != tc.want {
				t.Errorf("evidence = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestVerifyExecutionReport(t *testing.T) {
	started := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	ws := &WorkspaceAssignment{Path: root}
	write := func(path, content string, mtime time.Time) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		writePlan(t, path, content)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, tc := range []struct {
		name string
		path string
		want string // "" means verified
	}{
		{"missing", filepath.Join(root, "plans", "missing-execution.md"), "not written yet"},
		{"empty", write(filepath.Join(root, "plans", "empty-execution.md"), "", started.Add(time.Hour)), "is empty"},
		{"outside the workspace", write(filepath.Join(t.TempDir(), "outside-execution.md"), "# report\n", started.Add(time.Hour)), "is outside the workspace"},
		{"older than the execution start", write(filepath.Join(root, "plans", "stale-execution.md"), "# old\n", started.Add(-time.Hour)), "not modified since execution started"},
		{"fresh", write(filepath.Join(root, "plans", "fresh-execution.md"), "# report\n", started.Add(time.Hour)), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyExecutionReport(ws, tc.path, started)
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("error = %v, want verified", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Errorf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
	if err := verifyExecutionReport(ws, filepath.Join(root, "plans", "missing-execution.md"), started); err == nil || err.Error() != "not written yet" {
		t.Errorf("missing report error = %v, want exactly %q", err, "not written yet")
	}
}

func TestRecordExecutionCheckSetsReportPathOnce(t *testing.T) {
	e := newEnv(t)
	f := execute(t, e)
	executor := f.sessionFor(RoleExecutor).SessionID
	skill := SkillUse{Skill: "superpowers:executing-plans", Seq: 21, Evidence: "invoked"}

	first, err := e.m.recordPlannerTurn(f.ID, PlannerTurn{SessionID: executor, StartSeq: 20, EndSeq: 30, Text: "Working.", Outcome: "completed", Skills: []SkillUse{skill}})
	if err != nil {
		t.Fatal(err)
	}
	retained, _ := e.m.findMonitoredTurn(f.ID, first)
	e.m.recordExecutionCheck(bg, f.ID, retained)
	got, _ := e.m.findMonitoredTurn(f.ID, first)
	want := ExecutionCheck{Skill: "executing-plans invoked at record 21", ReportPath: f.ReportTarget, ReportError: "not written yet"}
	if got.Execution == nil || *got.Execution != want {
		t.Fatalf("first check = %+v, want %+v", got.Execution, want)
	}
	if current, _ := e.m.Get(f.ID); current.ReportPath != "" {
		t.Fatalf("report path = %q before the report exists", current.ReportPath)
	}

	writePlan(t, f.ReportTarget, "# Execution report\n")
	second, err := e.m.recordPlannerTurn(f.ID, PlannerTurn{SessionID: executor, StartSeq: 31, EndSeq: 40, Text: "Done.", Outcome: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	retained, _ = e.m.findMonitoredTurn(f.ID, second)
	e.m.recordExecutionCheck(bg, f.ID, retained)
	got, _ = e.m.findMonitoredTurn(f.ID, second)
	if got.Execution == nil || !got.Execution.ReportVerified || got.Execution.ReportError != "" || got.Execution.ReportPath != f.ReportTarget {
		t.Fatalf("second check = %+v, want a verified report at %s", got.Execution, f.ReportTarget)
	}
	if current, _ := e.m.Get(f.ID); current.ReportPath != f.ReportTarget {
		t.Fatalf("report path = %q, want %q", current.ReportPath, f.ReportTarget)
	}
}

// The report target is fixed at Enter execution. A planner that reports a new
// plan while the executor works moves the plan path, never the report target.
func TestReportTargetIgnoresLaterPlannerReport(t *testing.T) {
	e := newEnv(t)
	f := execute(t, e)
	other := filepath.Join(filepath.Dir(f.PlanPath), "other-plan.md")
	writePlan(t, other, "# other\n")

	reported := e.recordReport(t, f, "Revised.\nSpec: "+f.SpecPath+"\nPlan: "+other)

	if reported.PlanPath != other {
		t.Fatalf("setup: plan path = %q, want the revised %q", reported.PlanPath, other)
	}
	if reported.ReportTarget != f.ReportTarget {
		t.Errorf("report target moved from %q to %q", f.ReportTarget, reported.ReportTarget)
	}
}
