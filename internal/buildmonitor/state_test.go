package buildmonitor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "my-repo.json")
	want := &UnitState{
		RepoName: "My Repo", Repo: "o/r", Branch: "main",
		Workflows: []WorkflowState{{Name: "CI", Path: ".github/workflows/ci.yml", Conclusion: "failure"}},
		RecentRemediations: []RemediationRecord{{
			WorkflowID: 1, WorkflowName: "CI", RunID: 11, HeadSHA: "abc",
			FirstObservedAt: "2026-08-13T08:00:00Z", Status: RemediationLaunched,
			SessionID: "sess-1",
		}},
	}
	if err := WriteState(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadState(path)
	if err != nil || got == nil || len(got.Workflows) != 1 || got.Workflows[0].Conclusion != "failure" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if len(got.RecentRemediations) != 1 || got.RecentRemediations[0].SessionID != "sess-1" {
		t.Fatalf("remediation ledger did not round-trip: %+v", got.RecentRemediations)
	}
	missing, err := ReadState(filepath.Join(dir, "absent.json"))
	if err != nil || missing != nil {
		t.Fatalf("absent must be nil,nil; got %+v err=%v", missing, err)
	}
}

// A unit file written before the dedicated workspaces carry the episode
// workspace and the ledger's workspace. The reader ignores the unknown keys,
// and the next write drops them.
func TestReadState_LegacyEpisodeFieldsDropped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.json")
	legacy := `{
  "repo_name": "My Repo",
  "repo": "o/r",
  "branch": "main",
  "remediation_workspace_id": "ws-1",
  "remediation_sha": "abc1234",
  "workflows": [
    {"name": "CI", "path": ".github/workflows/ci.yml", "workflow_id": 1, "session_id": "s1"}
  ],
  "recent_remediations": [
    {"workflow_id": 1, "run_id": 11, "status": "launched", "workspace_id": "ws-1", "session_id": "s1"}
  ]
}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	st, err := ReadState(path)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if st == nil || len(st.Workflows) != 1 || st.Workflows[0].SessionID != "s1" {
		t.Fatalf("state = %+v, want the row's session", st)
	}

	if err := WriteState(path, st); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"remediation_workspace_id", "remediation_sha", "workspace_id"} {
		if strings.Contains(string(data), key) {
			t.Errorf("written state still carries %q:\n%s", key, data)
		}
	}
}
