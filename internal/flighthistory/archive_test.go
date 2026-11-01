package flighthistory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

var archiveT0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func archiveEnv(t *testing.T) (opts ArchiveOptions, tick func(time.Duration)) {
	t.Helper()
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	now := archiveT0
	return ArchiveOptions{SchmuxVersion: "test", Now: func() time.Time { return now }},
		func(d time.Duration) { now = now.Add(d) }
}

func writeSource(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func readManifest(t *testing.T, flightID string) Manifest {
	t.Helper()
	dir, err := FlightDir(flightID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestArchiveFlightWritesVersionedEvidenceWithMode0600(t *testing.T) {
	opts, _ := archiveEnv(t)
	src := writeSource(t, "{\"a\":1}\n")
	flightJSON := []byte(`{"id":"fl-1"}`)
	gitJSON := []byte(`{"branch":"b"}`)

	err := ArchiveFlight("fl-1", flightJSON, []TranscriptInput{{Role: "planner", SessionID: "s-1", Source: src}}, gitJSON, opts)
	if err != nil {
		t.Fatalf("ArchiveFlight: %v", err)
	}

	dir, _ := FlightDir("fl-1")
	for name, want := range map[string]string{
		"flight.json":                   string(flightJSON),
		"git-state.json":                string(gitJSON),
		"transcripts/planner-s-1.jsonl": "{\"a\":1}\n",
	} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q (err %v), want %q", name, got, err, want)
		}
		if mode := modeOf(t, filepath.Join(dir, name)); mode != 0o600 {
			t.Errorf("%s mode = %v, want 0600", name, mode)
		}
	}
	if mode := modeOf(t, filepath.Join(dir, "manifest.json")); mode != 0o600 {
		t.Errorf("manifest mode = %v, want 0600", mode)
	}
	for _, d := range []string{dir, filepath.Join(dir, "transcripts")} {
		if mode := modeOf(t, d); mode != 0o700 {
			t.Errorf("%s mode = %v, want 0700", d, mode)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "final-flight.json")); !os.IsNotExist(err) {
		t.Errorf("final-flight.json exists for a non-final archive: %v", err)
	}

	m := readManifest(t, "fl-1")
	if m.SchemaVersion != 1 || m.SchmuxVersion != "test" || m.Final {
		t.Errorf("manifest header = %+v", m)
	}
	versions := m.Transcripts["planner-s-1.jsonl"]
	if len(versions) != 1 || versions[0].SHA256 != sha("{\"a\":1}\n") || versions[0].Bytes != 8 || versions[0].Source != src {
		t.Errorf("transcript versions = %+v", versions)
	}
	if v, ok := m.FlightVersions[sha(string(flightJSON))]; !ok || v.Bytes != int64(len(flightJSON)) {
		t.Errorf("flight versions = %+v", m.FlightVersions)
	}
	if _, ok := m.GitVersions[sha(string(gitJSON))]; !ok {
		t.Errorf("git versions = %+v", m.GitVersions)
	}
}

func TestArchiveFlightIsIdempotentForIdenticalTranscript(t *testing.T) {
	opts, tick := archiveEnv(t)
	src := writeSource(t, "{\"a\":1}\n")
	in := []TranscriptInput{{Role: "planner", SessionID: "s-1", Source: src}}
	if err := ArchiveFlight("fl-1", []byte(`{"id":"fl-1"}`), in, nil, opts); err != nil {
		t.Fatal(err)
	}
	dir, _ := FlightDir("fl-1")
	dest := filepath.Join(dir, "transcripts", "planner-s-1.jsonl")
	before, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	manifestBefore, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))

	tick(time.Hour)
	if err := ArchiveFlight("fl-1", []byte(`{"id":"fl-1"}`), in, nil, opts); err != nil {
		t.Fatal(err)
	}

	after, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Errorf("identical transcript was rewritten: before %v/%d after %v/%d", before.ModTime(), before.Size(), after.ModTime(), after.Size())
	}
	manifestAfter, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if string(manifestAfter) != string(manifestBefore) {
		t.Errorf("manifest changed on an identical retry:\n%s\n%s", manifestBefore, manifestAfter)
	}
	if got := len(readManifest(t, "fl-1").Transcripts["planner-s-1.jsonl"]); got != 1 {
		t.Errorf("transcript versions = %d, want 1", got)
	}
}

func TestArchiveFlightRecordsChangedTranscriptVersion(t *testing.T) {
	opts, tick := archiveEnv(t)
	src := writeSource(t, "one\n")
	in := []TranscriptInput{{Role: "executor", SessionID: "s-2", Source: src}}
	if err := ArchiveFlight("fl-1", []byte(`{}`), in, nil, opts); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tick(time.Minute)

	if err := ArchiveFlight("fl-1", []byte(`{}`), in, nil, opts); err != nil {
		t.Fatal(err)
	}

	versions := readManifest(t, "fl-1").Transcripts["executor-s-2.jsonl"]
	if len(versions) != 2 {
		t.Fatalf("versions = %+v, want 2", versions)
	}
	if versions[0].SHA256 != sha("one\n") || versions[1].SHA256 != sha("one\ntwo\n") {
		t.Errorf("version digests = %s, %s", versions[0].SHA256, versions[1].SHA256)
	}
	if !versions[1].ObservedAt.After(versions[0].ObservedAt) {
		t.Errorf("versions are not ordered by observation: %+v", versions)
	}
	dir, _ := FlightDir("fl-1")
	got, _ := os.ReadFile(filepath.Join(dir, "transcripts", "executor-s-2.jsonl"))
	if string(got) != "one\ntwo\n" {
		t.Errorf("current transcript = %q", got)
	}
}

func TestArchiveFlightRecordsMissingTranscriptWithoutInventingEmptyFile(t *testing.T) {
	opts, _ := archiveEnv(t)
	missing := filepath.Join(t.TempDir(), "gone", "conversation.jsonl")

	err := ArchiveFlight("fl-1", []byte(`{}`), []TranscriptInput{{Role: "reviewer", SessionID: "s-3", Source: missing}}, nil, opts)
	if err != nil {
		t.Fatalf("a missing transcript must not fail the archive: %v", err)
	}

	dir, _ := FlightDir("fl-1")
	if _, err := os.Stat(filepath.Join(dir, "transcripts", "reviewer-s-3.jsonl")); !os.IsNotExist(err) {
		t.Errorf("destination transcript exists for a missing source: %v", err)
	}
	m := readManifest(t, "fl-1")
	if len(m.MissingSources) != 1 {
		t.Fatalf("missing sources = %+v", m.MissingSources)
	}
	got := m.MissingSources[0]
	if got.Kind != "transcript" || got.ID != "reviewer-s-3" || got.Source != missing || got.Error == "" {
		t.Errorf("missing source = %+v", got)
	}

	// A retry does not duplicate the record.
	if err := ArchiveFlight("fl-1", []byte(`{}`), []TranscriptInput{{Role: "reviewer", SessionID: "s-3", Source: missing}}, nil, opts); err != nil {
		t.Fatal(err)
	}
	if got := len(readManifest(t, "fl-1").MissingSources); got != 1 {
		t.Errorf("missing sources after retry = %d, want 1", got)
	}
}

func TestArchiveFlightFinalWritesFinalFlightOnce(t *testing.T) {
	opts, tick := archiveEnv(t)
	final := []byte(`{"id":"fl-1","state":"final"}`)
	opts.Final = true
	if err := ArchiveFlight("fl-1", final, nil, nil, opts); err != nil {
		t.Fatal(err)
	}
	dir, _ := FlightDir("fl-1")
	finalPath := filepath.Join(dir, "final-flight.json")
	got, err := os.ReadFile(finalPath)
	if err != nil || string(got) != string(final) {
		t.Fatalf("final-flight.json = %q (err %v)", got, err)
	}
	if mode := modeOf(t, finalPath); mode != 0o600 {
		t.Errorf("final-flight.json mode = %v", mode)
	}
	before, _ := os.Stat(finalPath)
	m := readManifest(t, "fl-1")
	if !m.Final || m.FinalSnapshotSHA256 != sha(string(final)) {
		t.Errorf("manifest final = %v/%s", m.Final, m.FinalSnapshotSHA256)
	}

	tick(time.Hour)
	if err := ArchiveFlight("fl-1", final, nil, nil, opts); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(finalPath)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("identical final flight was rewritten")
	}
	if _, err := os.Stat(filepath.Join(dir, "git-state.json")); !os.IsNotExist(err) {
		t.Errorf("git-state.json written with no git evidence: %v", err)
	}
}

func TestArchiveFlightRejectsUnconfinedIdentifiers(t *testing.T) {
	opts, _ := archiveEnv(t)
	src := writeSource(t, "x\n")

	if err := ArchiveFlight("../escape", []byte(`{}`), nil, nil, opts); err == nil {
		t.Error("ArchiveFlight accepted a traversing flight ID")
	}
	if err := ArchiveFlight("fl-1", []byte(`{}`), []TranscriptInput{{Role: "planner", SessionID: "../../x", Source: src}}, nil, opts); err == nil {
		t.Error("ArchiveFlight accepted a traversing session ID")
	}
	if err := ArchiveFlight("fl-1", []byte(`{}`), []TranscriptInput{{Role: "a/b", SessionID: "s", Source: src}}, nil, opts); err == nil {
		t.Error("ArchiveFlight accepted a role with a separator")
	}
	if _, err := os.Stat(filepath.Join(Root(), "..", "escape")); !os.IsNotExist(err) {
		t.Errorf("a traversing flight ID created a directory outside the root: %v", err)
	}
}

func TestArchiveFlightRecordsCallerSuppliedMissingSourceOnce(t *testing.T) {
	opts, _ := archiveEnv(t)
	opts.Missing = []MissingSource{{Kind: "workspace", ID: "repo-001", Source: "/ws/gone", ObservedAt: archiveT0, Error: "directory is not available"}}

	for i := 0; i < 2; i++ {
		if err := ArchiveFlight("fl-1", []byte(`{}`), nil, []byte(`{"error":"x"}`), opts); err != nil {
			t.Fatal(err)
		}
	}

	m := readManifest(t, "fl-1")
	if len(m.MissingSources) != 1 || m.MissingSources[0].Kind != "workspace" || m.MissingSources[0].ID != "repo-001" {
		t.Fatalf("missing sources = %+v", m.MissingSources)
	}
}
