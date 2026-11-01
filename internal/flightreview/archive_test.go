package flightreview

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func newTestService(inspector VCSInspector) *Service {
	return NewService(Options{Workspaces: inspector, Now: func() time.Time { return t0.Add(time.Hour) }, Version: "test"})
}

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestImportLegacyCapturesCurrentAndPriorTranscripts(t *testing.T) {
	isolate(t)
	row := startedRow("fl-1", flights.StateExecuting)
	assign(&row, flights.RolePlanner, "s-new", "s-old")
	assign(&row, flights.RoleExecutor, "s-exec")
	newPath := writeConversation(t, "ws-fl-1", "s-new", "current planner")
	oldPath := writeConversation(t, "ws-fl-1", "s-old", "replaced planner")
	// s-exec has no conversation log.
	inspector := &fakeInspector{states: map[string]workspace.VCSArchiveState{
		"ws-fl-1": {VCS: "git", Branch: "feature/fl-1", HeadSHA: "abc123", FilesChanged: 2},
	}}

	if err := newTestService(inspector).ImportLegacy(context.Background(), testManager(t, row)); err != nil {
		t.Fatalf("ImportLegacy: %v", err)
	}

	dir := archiveDir(t, "fl-1")
	for name, want := range map[string]string{
		"planner-s-new.jsonl": "current planner",
		"planner-s-old.jsonl": "replaced planner",
	} {
		data, err := os.ReadFile(filepath.Join(dir, "transcripts", name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !contains(string(data), want) {
			t.Errorf("%s = %q, want it to contain %q", name, data, want)
		}
		if mode := modeOf(t, filepath.Join(dir, "transcripts", name)); mode != 0o600 {
			t.Errorf("%s mode = %v, want 0600", name, mode)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "transcripts", "executor-s-exec.jsonl")); !os.IsNotExist(err) {
		t.Errorf("a transcript was invented for a session with no log: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "final-flight.json")); !os.IsNotExist(err) {
		t.Errorf("legacy import wrote final-flight.json: %v", err)
	}

	m := readManifest(t, "fl-1")
	if got := m.Transcripts["planner-s-new.jsonl"]; len(got) != 1 || got[0].Source != newPath {
		t.Errorf("planner-s-new versions = %+v, want source %s", got, newPath)
	}
	if got := m.Transcripts["planner-s-old.jsonl"]; len(got) != 1 || got[0].Source != oldPath {
		t.Errorf("planner-s-old versions = %+v, want source %s", got, oldPath)
	}
	if len(m.MissingSources) != 1 || m.MissingSources[0].Kind != "transcript" || m.MissingSources[0].ID != "executor-s-exec" {
		t.Errorf("missing sources = %+v, want the executor transcript", m.MissingSources)
	}
	git, err := os.ReadFile(filepath.Join(dir, "git-state.json"))
	if err != nil || !contains(string(git), `"head_sha":"abc123"`) || !contains(string(git), `"branch":"feature/fl-1"`) {
		t.Errorf("git-state.json = %q (err %v)", git, err)
	}
	if mode := modeOf(t, filepath.Join(dir, "git-state.json")); mode != 0o600 {
		t.Errorf("git-state.json mode = %v, want 0600", mode)
	}
	if m.SchmuxVersion != "test" {
		t.Errorf("schmux version = %q", m.SchmuxVersion)
	}
}

func TestImportLegacyRecordsMissingWorkspaceGitCollectionError(t *testing.T) {
	isolate(t)
	row := startedRow("fl-2", flights.StatePlanning)
	assign(&row, flights.RolePlanner, "s-1")
	writeConversation(t, "ws-fl-2", "s-1", "plan it")
	inspector := &fakeInspector{
		states: map[string]workspace.VCSArchiveState{"ws-fl-2": {VCS: "git", Branch: "feature/fl-2", Error: "workspace directory /ws/fl-2 is not available"}},
		errs:   map[string]error{"ws-fl-2": errors.New("workspace directory /ws/fl-2 is not available")},
	}

	if err := newTestService(inspector).ImportLegacy(context.Background(), testManager(t, row)); err != nil {
		t.Fatalf("a git collection failure must not fail the import: %v", err)
	}

	dir := archiveDir(t, "fl-2")
	if _, err := os.Stat(filepath.Join(dir, "transcripts", "planner-s-1.jsonl")); err != nil {
		t.Errorf("transcript was not archived despite the git failure: %v", err)
	}
	git, err := os.ReadFile(filepath.Join(dir, "git-state.json"))
	if err != nil || !contains(string(git), `"error":"workspace directory /ws/fl-2 is not available"`) || !contains(string(git), `"branch":"feature/fl-2"`) {
		t.Errorf("git-state.json = %q (err %v), want identity plus error", git, err)
	}
	m := readManifest(t, "fl-2")
	var found bool
	for _, missing := range m.MissingSources {
		if missing.Kind == "workspace" && missing.ID == "ws-fl-2" && missing.Error != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("manifest does not record the missing workspace: %+v", m.MissingSources)
	}
}

func TestImportLegacyBacklogFlightHasNoTranscriptOrGitFile(t *testing.T) {
	isolate(t)
	inspector := &fakeInspector{}

	if err := newTestService(inspector).ImportLegacy(context.Background(), testManager(t, backlogRow("fl-3"))); err != nil {
		t.Fatalf("ImportLegacy: %v", err)
	}

	dir := archiveDir(t, "fl-3")
	if _, err := os.Stat(filepath.Join(dir, "flight.json")); err != nil {
		t.Errorf("flight.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Errorf("manifest.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "git-state.json")); !os.IsNotExist(err) {
		t.Errorf("git-state.json exists for a backlog flight: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "transcripts"))
	if err != nil || len(entries) != 0 {
		t.Errorf("transcripts = %v (err %v), want none", entries, err)
	}
	if len(inspector.calls) != 0 {
		t.Errorf("inspected workspaces for a backlog flight: %v", inspector.calls)
	}
}

func TestCaptureFinalRetryDoesNotDuplicateTranscript(t *testing.T) {
	isolate(t)
	row := startedRow("fl-4", flights.StateExecuting)
	assign(&row, flights.RoleExecutor, "s-1")
	writeConversation(t, "ws-fl-4", "s-1", "executing")
	svc := newTestService(&fakeInspector{states: map[string]workspace.VCSArchiveState{"ws-fl-4": {VCS: "git"}}})

	for i := 0; i < 2; i++ {
		if err := svc.Capture(context.Background(), row, true); err != nil {
			t.Fatalf("Capture #%d: %v", i+1, err)
		}
	}

	m := readManifest(t, "fl-4")
	if !m.Final {
		t.Error("manifest is not final after a final capture")
	}
	if got := m.Transcripts["executor-s-1.jsonl"]; len(got) != 1 {
		t.Errorf("transcript versions after retry = %d, want 1", len(got))
	}
	if _, err := os.Stat(filepath.Join(archiveDir(t, "fl-4"), "final-flight.json")); err != nil {
		t.Errorf("final-flight.json missing: %v", err)
	}
}

func TestCaptureFinalRowUpdatesRowWithoutRereadingDestroyedResources(t *testing.T) {
	isolate(t)
	row := startedRow("fl-final-row", flights.StateExecuting)
	assign(&row, flights.RoleExecutor, "s-final")
	writeConversation(t, "ws-fl-final-row", "s-final", "executing")
	inspector := &fakeInspector{states: map[string]workspace.VCSArchiveState{
		"ws-fl-final-row": {VCS: "git", RepoURL: row.Repo, Branch: "feature/fl-final-row", HeadSHA: "abc"},
	}}
	svc := newTestService(inspector)
	if err := svc.Capture(context.Background(), row, false); err != nil {
		t.Fatal(err)
	}
	row.Operations = append(row.Operations, flights.Operation{
		ID: "op-dispose", Action: flights.ActionDispose, Status: flights.StatusSucceeded,
	})

	if err := svc.CaptureFinalRow(row); err != nil {
		t.Fatal(err)
	}

	var archived flights.Flight
	data, err := os.ReadFile(filepath.Join(archiveDir(t, "fl-final-row"), "final-flight.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &archived); err != nil {
		t.Fatal(err)
	}
	if len(archived.Operations) != 1 || archived.Operations[0].Status != flights.StatusSucceeded {
		t.Fatalf("final row operations = %+v, want one succeeded dispose", archived.Operations)
	}
	m := readManifest(t, "fl-final-row")
	if len(m.Transcripts["executor-s-final.jsonl"]) != 1 {
		t.Errorf("final row capture lost transcript evidence: %+v", m.Transcripts)
	}
	if len(m.GitVersions) != 1 {
		t.Errorf("final row capture lost git evidence: %+v", m.GitVersions)
	}
	if len(inspector.calls) != 1 {
		t.Errorf("final row capture reread the workspace %d times", len(inspector.calls)-1)
	}
}

func TestCapturePassesCancellationToEvidenceCollection(t *testing.T) {
	isolate(t)
	row := startedRow("fl-cancel", flights.StateExecuting)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := newTestService(&fakeInspector{})

	err := svc.Capture(ctx, row, false)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Capture error = %v, want context.Canceled", err)
	}
}

func TestCaptureRejectsUnconfinedFlightID(t *testing.T) {
	isolate(t)
	row := backlogRow("../escape")

	if err := newTestService(&fakeInspector{}).Capture(context.Background(), row, false); err == nil {
		t.Fatal("Capture accepted a flight ID that escapes the history directory")
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
