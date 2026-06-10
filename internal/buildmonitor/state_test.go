package buildmonitor

import (
	"path/filepath"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "my-repo.json")
	want := &UnitState{RepoName: "My Repo", Repo: "o/r", Branch: "main", Workflows: []WorkflowState{
		{Name: "CI", Path: ".github/workflows/ci.yml", Conclusion: "failure"},
	}}
	if err := WriteState(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadState(path)
	if err != nil || got == nil || len(got.Workflows) != 1 || got.Workflows[0].Conclusion != "failure" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	missing, err := ReadState(filepath.Join(dir, "absent.json"))
	if err != nil || missing != nil {
		t.Fatalf("absent must be nil,nil; got %+v err=%v", missing, err)
	}
}
