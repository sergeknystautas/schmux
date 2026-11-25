//go:build !nobuildmonitor

package buildmonitor

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// A "Launch workspace" remediation must land on the row, or the page keeps
// offering another launch while the session works, and on the ledger.
func TestRecordManualLaunch(t *testing.T) {
	s := &UnitState{Workflows: []WorkflowState{
		{WorkflowID: 2, Name: "Performance", RunID: 14, Status: "completed", Conclusion: "failure", HeadSHA: "78971fb0", FirstFailureRunID: 14, SessionID: "disposed-session"},
		{WorkflowID: 1, Name: "CI", RunID: 30, Status: "completed", Conclusion: "success"},
	}}

	RecordManualLaunch(s, s.Workflows[0], "bach-godot-003-b501189e", "2026-10-07T09:01:45Z")
	if got := s.Workflows[0].SessionID; got != "bach-godot-003-b501189e" {
		t.Errorf("Performance row session = %q, want the launched session", got)
	}
	if got := s.Workflows[1].SessionID; got != "" {
		t.Errorf("CI row session = %q, want untouched", got)
	}
	want := []RemediationRecord{{
		WorkflowID: 2, WorkflowName: "Performance", RunID: 14, HeadSHA: "78971fb0",
		FirstObservedAt: "2026-10-07T09:01:45Z", Status: RemediationLaunched,
		SessionID: "bach-godot-003-b501189e",
	}}
	if !reflect.DeepEqual(s.RecentRemediations, want) {
		t.Fatalf("ledger = %+v, want %+v", s.RecentRemediations, want)
	}

	// Launching the same run again replaces the session on row and ledger.
	RecordManualLaunch(s, s.Workflows[0], "bach-godot-009-aaaa", "2026-10-07T10:00:00Z")
	if got := s.Workflows[0].SessionID; got != "bach-godot-009-aaaa" {
		t.Errorf("row session after relaunch = %q", got)
	}
	if len(s.RecentRemediations) != 1 || s.RecentRemediations[0].SessionID != "bach-godot-009-aaaa" {
		t.Errorf("ledger after relaunch = %+v, want one record for the new session", s.RecentRemediations)
	}
}

// A manual launch records the agent even when the workflow is no longer
// failing: the agent keeps working in its dedicated workspace, so the row
// must keep pointing at it.
func TestRecordManualLaunch_SetsSessionUnconditionally(t *testing.T) {
	s := &UnitState{Workflows: []WorkflowState{
		{WorkflowID: 1, Name: "CI", RunID: 11, Status: "completed", Conclusion: "success"},
	}}

	RecordManualLaunch(s, s.Workflows[0], "s1", "2026-10-07T09:01:45Z")

	if got := s.Workflows[0].SessionID; got != "s1" {
		t.Errorf("row session = %q, want the launched session", got)
	}
	if len(s.RecentRemediations) != 1 || s.RecentRemediations[0].SessionID != "s1" {
		t.Errorf("ledger = %+v, want one launched record for s1", s.RecentRemediations)
	}
}

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

func TestClaimRemediation_DeduplicatesAndBoundsHistory(t *testing.T) {
	st := &UnitState{}
	w := twfFailing(1, 11, 11)
	w.Name = "CI"
	w.HeadSHA = "abc"
	if !ClaimRemediation(st, w, "2026-08-13T08:00:00Z") {
		t.Fatal("first claim should succeed")
	}
	if ClaimRemediation(st, w, "2026-08-13T08:05:00Z") {
		t.Fatal("same workflow run must not be claimed twice")
	}
	if got := st.RecentRemediations[0]; got.Status != RemediationClaimed || got.WorkflowName != "CI" || got.HeadSHA != "abc" {
		t.Fatalf("record = %+v", got)
	}

	for runID := int64(12); runID < 12+maxRecentRemediations; runID++ {
		w.RunID = runID
		w.FirstFailureRunID = runID
		if !ClaimRemediation(st, w, "later") {
			t.Fatalf("claim %d was refused", runID)
		}
	}
	if len(st.RecentRemediations) != maxRecentRemediations {
		t.Fatalf("history length = %d, want %d", len(st.RecentRemediations), maxRecentRemediations)
	}
	if st.RecentRemediations[0].RunID != 12 {
		t.Fatalf("oldest retained run = %d, want 12", st.RecentRemediations[0].RunID)
	}
}

func TestRemediationLedgerSurvivesRecovery(t *testing.T) {
	prev := &UnitState{Workflows: []WorkflowState{twfFailing(1, 11, 11)}}
	if !ClaimRemediation(prev, prev.Workflows[0], "now") {
		t.Fatal("claim failed")
	}

	recovered := &UnitState{Workflows: []WorkflowState{twf(1, 12, "success")}}
	ApplyTransitions(prev, recovered)
	if len(recovered.RecentRemediations) != 1 {
		t.Fatalf("history lost on recovery: %+v", recovered.RecentRemediations)
	}

	reappeared := &UnitState{Workflows: []WorkflowState{twf(1, 11, "failure")}}
	ApplyTransitions(recovered, reappeared)
	reappeared.Workflows[0].FirstFailureRunID = 11
	if ClaimRemediation(reappeared, reappeared.Workflows[0], "later") {
		t.Fatal("previously claimed run was claimed again after recovery")
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
	t.Run("sets the session when the episode moved on", func(t *testing.T) {
		st := failing()
		st.Workflows[0].FirstFailureRunID = 99
		if !StampLaunch(st, 1, 11, "sess-1", "") {
			t.Fatal("expected the session to be stamped")
		}
		if st.Workflows[0].SessionID != "sess-1" {
			t.Fatalf("SessionID = %q", st.Workflows[0].SessionID)
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
	t.Run("a launch error keeps the row's agent", func(t *testing.T) {
		st := failing()
		st.Workflows[0].SessionID = "working"
		if !StampLaunch(st, 1, 11, "", "boom") {
			t.Fatal("expected stamp")
		}
		if w := st.Workflows[0]; w.SessionID != "working" || w.LaunchError != "boom" {
			t.Fatalf("row = %+v, want the working agent and the error", w)
		}
	})
	t.Run("stamps durable record after current workflow disappears", func(t *testing.T) {
		st := &UnitState{RecentRemediations: []RemediationRecord{{WorkflowID: 1, RunID: 11, Status: RemediationClaimed}}}
		if !StampLaunch(st, 1, 11, "sess-1", "") {
			t.Fatal("expected ledger stamp")
		}
		if got := st.RecentRemediations[0]; got.Status != RemediationLaunched || got.SessionID != "sess-1" {
			t.Fatalf("record = %+v", got)
		}
	})
	t.Run("stamps durable record after current workflow recovers", func(t *testing.T) {
		st := &UnitState{
			Workflows:          []WorkflowState{twf(1, 12, "success")},
			RecentRemediations: []RemediationRecord{{WorkflowID: 1, RunID: 11, Status: RemediationClaimed}},
		}
		if !StampLaunch(st, 1, 11, "sess-1", "") {
			t.Fatal("expected ledger stamp")
		}
		if got := st.RecentRemediations[0]; got.Status != RemediationLaunched || got.SessionID != "sess-1" {
			t.Fatalf("record = %+v", got)
		}
	})
}

// A launch that raced the workflow's recovery still belongs to the row: the
// agent keeps working in its dedicated workspace, so the row must point at it.
func TestStampLaunch_SetsSessionWhenRecovered(t *testing.T) {
	st := &UnitState{
		Workflows:          []WorkflowState{twf(1, 12, "success")},
		RecentRemediations: []RemediationRecord{{WorkflowID: 1, RunID: 11, Status: RemediationClaimed}},
	}

	if !StampLaunch(st, 1, 11, "s2", "") {
		t.Fatal("expected the stamp to be accepted")
	}
	if got := st.Workflows[0].SessionID; got != "s2" {
		t.Errorf("row session = %q, want the launched session", got)
	}
	if got := st.RecentRemediations[0]; got.Status != RemediationLaunched || got.SessionID != "s2" {
		t.Errorf("ledger record = %+v, want a launched record for s2", got)
	}
}

func TestStampJoined(t *testing.T) {
	st := &UnitState{RecentRemediations: []RemediationRecord{{WorkflowID: 1, RunID: 11, Status: RemediationClaimed}}}
	if !StampJoined(st, 1, 11, "s1") {
		t.Fatal("expected the stamp to be accepted")
	}
	if got := st.RecentRemediations[0]; got.Status != RemediationJoined || got.SessionID != "s1" {
		t.Errorf("ledger record = %+v, want a joined record for s1", got)
	}
	if StampJoined(st, 1, 11, "s1") {
		t.Error("an unchanged stamp reported a change")
	}
	if StampJoined(st, 1, 99, "s1") {
		t.Error("an unknown run reported a change")
	}
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

// The dedicated workspace name is the identity of a monitored workflow's
// workspace. The "/" separators keep repo and workflow apart, so
// repo "bach-godot" + workflow "ci-image" and repo "bach-godot-ci" +
// workflow "image" never collide.
func TestWorkspaceName(t *testing.T) {
	cases := []struct {
		repoSlug, workflowName, want string
	}{
		{"bach-godot", "CI", "build-monitor/bach-godot/ci"},
		{"bach-godot", "ci-image", "build-monitor/bach-godot/ci-image"},
		{"bach-godot-ci", "image", "build-monitor/bach-godot-ci/image"},
		{"r", "!!!", "build-monitor/r/build"},
	}
	for _, tc := range cases {
		if got := WorkspaceName(tc.repoSlug, tc.workflowName); got != tc.want {
			t.Errorf("WorkspaceName(%q, %q) = %q, want %q", tc.repoSlug, tc.workflowName, got, tc.want)
		}
	}
	if WorkspaceName("bach-godot", "ci-image") == WorkspaceName("bach-godot-ci", "image") {
		t.Error("repo and workflow segments must not collide")
	}
}

// The label is the name with its separators flattened: the dashboard shows it
// wherever a workspace is named.
func TestWorkspaceLabel(t *testing.T) {
	if got := WorkspaceLabel("bach-godot", "ci-image"); got != "build-monitor-bach-godot-ci-image" {
		t.Errorf("WorkspaceLabel = %q, want build-monitor-bach-godot-ci-image", got)
	}
}

// Every workflow gets a stable branch, so its dedicated workspace survives
// the failure episode that created it.
func TestWorkspaceBranch(t *testing.T) {
	if got := WorkspaceBranch("CI Image"); got != "fix/ci-image" {
		t.Errorf("WorkspaceBranch = %q, want fix/ci-image", got)
	}
	if got := WorkspaceBranch("!!!"); got != "fix/build" {
		t.Errorf("degenerate name: WorkspaceBranch = %q, want fix/build", got)
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

func TestFailureFiles(t *testing.T) {
	info := testFailureInfo()
	info.Workflow.FailedJobs = append(info.Workflow.FailedJobs, FailedJob{ID: 100, Name: "lint"})
	logs := map[int64][]byte{99: []byte("log body")}
	logErrors := map[int64]string{100: "download failed"}
	files, err := FailureFiles(info, logs, logErrors)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Name != "failure.json" || files[1].Name != "99.log" {
		t.Fatalf("files = %+v, want failure.json then 99.log", files)
	}
	var fc map[string]any
	if err := json.Unmarshal(files[0].Data, &fc); err != nil {
		t.Fatal(err)
	}
	if fc["head_sha"] != "abc1234def5678" || fc["workflow"] != "E2E Tests" {
		t.Fatalf("failure.json = %v", fc)
	}
	if fc["log_download_errors"].(map[string]any)["100"] != "download failed" {
		t.Fatalf("log_download_errors = %v", fc["log_download_errors"])
	}
	if string(files[1].Data) != "log body" {
		t.Fatalf("log = %q", files[1].Data)
	}
}

func TestBuildPrompt(t *testing.T) {
	info := testFailureInfo()
	prompt := BuildPrompt(info)
	for _, want := range []string{
		"git reset --hard abc1234def5678",
		"Before changing any code",
		"Issue: explain the observed failure",
		"Cause: explain what caused the failure to start happening",
		"Reproduction: explain how you reproduced the failure",
		"Proposed fix: explain the specific change",
		"After sending the investigation update",
		"E2E Tests",
		"run #7",
		"https://github.com/owner/repo-a/actions/runs/11",
		"e2e",
		"attached failure.json",
		"attached <job-id>.log files",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
	beforeChange := strings.Index(prompt, "Before changing any code")
	afterUpdate := strings.Index(prompt, "After sending the investigation update")
	if beforeChange == -1 || afterUpdate == -1 || beforeChange >= afterUpdate {
		t.Errorf("prompt does not require the investigation update before implementation:\n%s", prompt)
	}
	// The move-to-commit instruction must be reset --hard (stays on the
	// branch) — a launched agent obeyed a literal `git checkout <sha>`
	// instruction and ended up in detached HEAD.
	if strings.Contains(prompt, "git checkout "+info.Workflow.HeadSHA) {
		t.Errorf("prompt instructs git checkout <sha> (detaches HEAD):\n%s", prompt)
	}
}

// Each workflow has a workspace of its own, so no other remediation session
// shares it.
func TestBuildPrompt_NoSharedWorkspaceLine(t *testing.T) {
	prompt := BuildPrompt(testFailureInfo())
	if strings.Contains(prompt, "may share this workspace") {
		t.Errorf("prompt still tells the agent its workspace is shared:\n%s", prompt)
	}
}
