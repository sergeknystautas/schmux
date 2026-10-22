package flights

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseArtifactReport(t *testing.T) {
	for _, tt := range []struct {
		name, text, spec, plan string
		found                  bool
	}{
		{"none", "Still brainstorming.", "", "", false},
		{"both", "Done.\nSpec: /w/s.md\nPlan: /w/p.md\n", "/w/s.md", "/w/p.md", true},
		{"last report wins", "Spec: /old.md\nPlan: /old-p.md\nRevised.\nSpec: /new.md\nPlan: /new-p.md", "/new.md", "/new-p.md", true},
		{"plan only", "Plan: /w/p.md", "", "/w/p.md", true},
		{"mid-line mention is not a report", "The Spec: line comes later.", "", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			spec, plan, found := parseArtifactReport(tt.text)
			if spec != tt.spec || plan != tt.plan || found != tt.found {
				t.Errorf("parse = %q, %q, %v; want %q, %q, %v", spec, plan, found, tt.spec, tt.plan, tt.found)
			}
		})
	}
}

func TestVerifyArtifacts(t *testing.T) {
	ws := t.TempDir()
	gitInit(t, ws)
	write := func(rel, content string) string {
		p := filepath.Join(ws, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		writePlan(t, p, content)
		return p
	}
	spec := write("docs/superpowers/specs/s.md", "# spec\n")
	plan := write("docs/superpowers/plans/p.md", "# plan\n")
	writePlan(t, filepath.Join(ws, ".gitignore"), ".schmux/\n")
	ignored := write(".schmux/flights/fl-1/plan.md", "# plan\n")
	empty := write("docs/plans/empty.md", "")
	outside := filepath.Join(t.TempDir(), "elsewhere.md")
	writePlan(t, outside, "# elsewhere\n")
	// Workspaces are often reached through a symlinked root.
	link := filepath.Join(t.TempDir(), "ws-link")
	if err := os.Symlink(ws, link); err != nil {
		t.Fatal(err)
	}
	assignment := &WorkspaceAssignment{Path: ws, VCS: "git"}

	for _, tt := range []struct {
		name       string
		ws         *WorkspaceAssignment
		spec, plan string
		want       []string // every violation that must be reported; none means pass
	}{
		{"skill defaults pass", assignment, spec, plan, nil},
		{"through a symlinked workspace root", &WorkspaceAssignment{Path: link, VCS: "git"}, filepath.Join(link, "docs/superpowers/specs/s.md"), plan, nil},
		{"spec not reported", assignment, "", plan, []string{"no spec reported"}},
		{"relative path", assignment, "docs/superpowers/specs/s.md", plan, []string{"not an absolute path"}},
		{"git-ignored plan", assignment, spec, ignored, []string{"ignored by git"}},
		{"outside the workspace", assignment, outside, plan, []string{"outside the workspace"}},
		{"empty plan", assignment, spec, empty, []string{"is empty"}},
		{"missing file", assignment, filepath.Join(ws, "docs/nope.md"), plan, []string{"not readable"}},
		{"same file twice", assignment, spec, spec, []string{"same file"}},
		{"both bad", assignment, "", "", []string{"no spec reported", "no plan reported"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := verifyArtifacts(bg, tt.ws, tt.spec, tt.plan)
			if len(tt.want) == 0 && got != "" {
				t.Fatalf("verifyArtifacts = %q, want pass", got)
			}
			if len(tt.want) > 0 && got == "" {
				t.Fatalf("verifyArtifacts passed, want %q", tt.want)
			}
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("verifyArtifacts = %q, want it to contain %q", got, w)
				}
			}
		})
	}
}

func TestRecordArtifactReport(t *testing.T) {
	e := newEnv(t)
	verified := e.reportPlanning(t, takeoff(t, e))
	if verified.SpecPath == "" || verified.PlanPath == "" {
		t.Fatalf("verified report not adopted: %+v", verified.Monitor.Turns)
	}
	if r := verified.Monitor.Turns[0].Artifacts; r == nil || r.Error != "" || r.SpecPath != verified.SpecPath {
		t.Fatalf("turn report = %+v", r)
	}

	// A later violating report is retained on its turn but does not replace
	// the last verified paths.
	misplaced := filepath.Join(e.fk.workspaces.path, ".schmux", "plan.md")
	writePlan(t, filepath.Join(e.fk.workspaces.path, ".gitignore"), ".schmux/\n")
	if err := os.MkdirAll(filepath.Dir(misplaced), 0o755); err != nil {
		t.Fatal(err)
	}
	writePlan(t, misplaced, "# plan\n")
	after := e.recordReport(t, verified, "Moved it.\nSpec: "+verified.SpecPath+"\nPlan: "+misplaced)
	last := after.Monitor.Turns[len(after.Monitor.Turns)-1].Artifacts
	if last == nil || !strings.Contains(last.Error, "ignored by git") || last.PlanPath != misplaced {
		t.Fatalf("violating report = %+v", last)
	}
	if after.SpecPath != verified.SpecPath || after.PlanPath != verified.PlanPath {
		t.Errorf("flight paths = %q/%q, want the last verified %q/%q", after.SpecPath, after.PlanPath, verified.SpecPath, verified.PlanPath)
	}

	// A turn with no report lines records nothing.
	quiet := e.recordReport(t, after, "Still thinking.")
	if r := quiet.Monitor.Turns[len(quiet.Monitor.Turns)-1].Artifacts; r != nil {
		t.Errorf("turn without report lines recorded %+v", r)
	}
}

// A placement violation always reaches the pilot, even when the classifier
// rules the response routine; a verified report does not by itself.
func TestNotificationForPlacementViolation(t *testing.T) {
	cfg := twoClassifierConfig()
	high := 0.95
	routine := &Classification{Answers: []MonitorAnswer{{ID: "a", Choice: "y", Confidence: &high}, {ID: "b", Choice: "y", Confidence: &high}}}
	f := Flight{ID: "fl-1", Workspace: &WorkspaceAssignment{WorkspaceID: "ws-1", Path: "/ws"}}
	turn := func(report *ArtifactReport) MonitoredTurn {
		return MonitoredTurn{
			ID:        "turn-1",
			Turn:      PlannerTurn{SessionID: "planner", StartSeq: 1, EndSeq: 9, Outcome: "completed", Text: "Spec: /ws/s.md\nPlan: /ws/.schmux/p.md"},
			Attempts:  []MonitorAttempt{{Config: &cfg, Classification: routine}},
			Artifacts: report,
		}
	}
	m := &Manager{}

	violating := m.notificationForTurn(f, turn(&ArtifactReport{SpecPath: "/ws/s.md", PlanPath: "/ws/.schmux/p.md", Error: "plan /ws/.schmux/p.md is ignored by git, so it cannot be committed"}), "pilot", RolePlanner)
	if violating == nil {
		t.Fatal("violation produced no pilot notification")
	}
	for _, want := range []string{"Reported plan: /ws/.schmux/p.md", "Spec/plan check FAILED: plan /ws/.schmux/p.md is ignored by git"} {
		if !strings.Contains(violating.Text, want) {
			t.Errorf("notification lacks %q:\n%s", want, violating.Text)
		}
	}
	if got := m.notificationForTurn(f, turn(&ArtifactReport{SpecPath: "/ws/s.md", PlanPath: "/ws/p.md"}), "pilot", RolePlanner); got != nil {
		t.Errorf("verified report with routine rulings notified the pilot:\n%s", got.Text)
	}
}
