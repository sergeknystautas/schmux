//go:build !nobuildmonitor

package buildmonitor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanLaunches(t *testing.T) {
	events := []TransitionEvent{
		{WorkflowID: 1, Kind: TransitionEnteredFailure, RunID: 11},
		{WorkflowID: 2, Kind: TransitionEnteredFailure, FromUnknown: true, RunID: 22},
		{WorkflowID: 3, Kind: TransitionRecovered, RunID: 33},
		{WorkflowID: 4, Kind: TransitionEnteredFailure, RunID: 44},
	}
	got := PlanLaunches(events)
	if len(got) != 2 || got[0] != 1 || got[1] != 4 {
		t.Fatalf("got %v, want [1 4]", got)
	}
}

func TestStampWorkspace_FirstWins(t *testing.T) {
	st := &UnitState{}
	if !StampWorkspace(st, "ws-1", "abc") {
		t.Fatal("first stamp should succeed")
	}
	if StampWorkspace(st, "ws-2", "def") {
		t.Fatal("second stamp should be refused")
	}
	if st.RemediationWorkspaceID != "ws-1" || st.RemediationSHA != "abc" {
		t.Fatalf("st = %+v", st)
	}
}

func TestStampLaunch(t *testing.T) {
	failing := func() *UnitState {
		return &UnitState{Workflows: []WorkflowState{twfFailing(1, 11, 11)}}
	}
	t.Run("stamps a matching failing workflow", func(t *testing.T) {
		st := failing()
		if !StampLaunch(st, 1, 11, "sess-1", "") {
			t.Fatal("expected stamp")
		}
		if st.Workflows[0].SessionID != "sess-1" {
			t.Fatalf("SessionID = %q", st.Workflows[0].SessionID)
		}
	})
	t.Run("refuses when the episode moved on", func(t *testing.T) {
		st := failing()
		st.Workflows[0].FirstFailureRunID = 99
		if StampLaunch(st, 1, 11, "sess-1", "") {
			t.Fatal("expected refusal on episode mismatch")
		}
	})
	t.Run("refuses when the workflow recovered", func(t *testing.T) {
		st := &UnitState{Workflows: []WorkflowState{twf(1, 12, "success")}}
		if StampLaunch(st, 1, 11, "sess-1", "") {
			t.Fatal("expected refusal on recovered workflow")
		}
	})
	t.Run("refuses unknown workflow", func(t *testing.T) {
		st := failing()
		if StampLaunch(st, 42, 11, "sess-1", "") {
			t.Fatal("expected refusal on unknown workflow")
		}
	})
	t.Run("stamps a launch error", func(t *testing.T) {
		st := failing()
		if !StampLaunch(st, 1, 11, "", "boom") {
			t.Fatal("expected stamp")
		}
		if st.Workflows[0].LaunchError != "boom" {
			t.Fatalf("LaunchError = %q", st.Workflows[0].LaunchError)
		}
	})
}

func TestWorkflowSlug(t *testing.T) {
	cases := map[string]string{
		"E2E Tests":      "e2e-tests",
		"Deploy Website": "deploy-website",
		"  CI / lint  ":  "ci-lint",
		"Release":        "release",
	}
	for in, want := range cases {
		if got := WorkflowSlug(in); got != want {
			t.Errorf("WorkflowSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShortSHA(t *testing.T) {
	if got := ShortSHA("abc1234def5678"); got != "abc1234d" {
		t.Errorf("got %q", got)
	}
	if got := ShortSHA("ab"); got != "ab" {
		t.Errorf("got %q", got)
	}
}

func TestFixBranch(t *testing.T) {
	if got := FixBranch("Canary", "809bec697a4f487d"); got != "fix/canary-809bec69" {
		t.Errorf("got %q", got)
	}
	if got := FixBranch("E2E Tests", "abc1234def"); got != "fix/e2e-tests-abc1234d" {
		t.Errorf("got %q", got)
	}
	if got := FixBranch("***", "abc1234def"); got != "fix/build-abc1234d" {
		t.Errorf("degenerate name: got %q", got)
	}
}

func testFailureInfo() FailureInfo {
	return FailureInfo{
		RepoName: "Repo A",
		Repo:     "owner/repo-a",
		Workflow: WorkflowState{
			Name: "E2E Tests", Path: ".github/workflows/e2e.yml", WorkflowID: 1,
			RunID: 11, RunNumber: 7, Status: "completed", Conclusion: "failure",
			HTMLURL:    "https://github.com/owner/repo-a/actions/runs/11",
			HeadSHA:    "abc1234def5678",
			FailedJobs: []FailedJob{{ID: 99, Name: "e2e", HTMLURL: "https://x/j/99"}},
		},
	}
}

func TestWriteWorkspaceContext(t *testing.T) {
	dir := t.TempDir()
	logs := map[int64][]byte{99: []byte("log body")}
	logErrors := map[int64]string{100: "download failed"}
	ctxDir, err := WriteWorkspaceContext(dir, testFailureInfo(), logs, logErrors)
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(dir, ".schmux", "build-monitor", "e2e-tests")
	if ctxDir != wantDir {
		t.Fatalf("ctxDir = %q, want %q", ctxDir, wantDir)
	}
	data, err := os.ReadFile(filepath.Join(wantDir, "failure.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fc map[string]any
	if err := json.Unmarshal(data, &fc); err != nil {
		t.Fatal(err)
	}
	if fc["head_sha"] != "abc1234def5678" || fc["workflow"] != "E2E Tests" {
		t.Fatalf("failure.json = %v", fc)
	}
	if fc["log_download_errors"].(map[string]any)["100"] != "download failed" {
		t.Fatalf("log_download_errors = %v", fc["log_download_errors"])
	}
	logData, err := os.ReadFile(filepath.Join(wantDir, "logs", "99.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(logData) != "log body" {
		t.Fatalf("log = %q", logData)
	}
}

func TestBuildPrompt(t *testing.T) {
	info := testFailureInfo()
	prompt := BuildPrompt(info, ContextDir(info.Workflow.Name))
	for _, want := range []string{
		"git reset --hard abc1234def5678",
		"E2E Tests",
		"run #7",
		"https://github.com/owner/repo-a/actions/runs/11",
		"e2e",
		".schmux/build-monitor/e2e-tests",
		"share this workspace",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
	// The move-to-commit instruction must be reset --hard (stays on the
	// branch) — a launched agent obeyed a literal `git checkout <sha>`
	// instruction and ended up in detached HEAD.
	if strings.Contains(prompt, "git checkout "+info.Workflow.HeadSHA) {
		t.Errorf("prompt instructs git checkout <sha> (detaches HEAD):\n%s", prompt)
	}
}
