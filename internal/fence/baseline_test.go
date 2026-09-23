package fence

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

// Wrap writes the embedded baseline beside settings.json and extends it by
// absolute path instead of the builtin "code" template.
func TestWrapWritesBaseline(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sess with 'quote'")
	ws := t.TempDir()
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: ws, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	baselinePath := filepath.Join(dir, baselineFileName)
	got, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("baseline not written: %v", err)
	}
	if !bytes.Equal(got, baselineJSONC) {
		t.Errorf("baseline on disk differs from embedded copy")
	}
	if bytes.Contains(got, []byte("*.sentry.io")) {
		t.Errorf("baseline must not contain *.sentry.io")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.Extends != baselinePath {
		t.Errorf("extends = %q, want %q", s.Extends, baselinePath)
	}
}

// fenceConfigShow runs `fence config show` with the given args and returns
// the merged JSON object it prints after the "Active config chain" preamble.
func fenceConfigShow(t *testing.T, fenceBin string, args ...string) map[string]any {
	t.Helper()
	out, err := exec.Command(fenceBin, append([]string{"config", "show"}, args...)...).Output()
	if err != nil {
		t.Fatalf("fence config show %v: %v", args, err)
	}
	i := bytes.IndexByte(out, '{')
	if i < 0 {
		t.Fatalf("no JSON in fence config show output:\n%s", out)
	}
	var m map[string]any
	if err := json.Unmarshal(out[i:], &m); err != nil {
		t.Fatalf("parse fence config show output: %v\n%s", err, out)
	}
	return m
}

// The embedded baseline must match the installed fence's builtin "code"
// template except for the removed *.sentry.io denial. Any other difference
// means fence changed its template and the copy needs re-syncing.
func TestBaselineMatchesInstalledFence(t *testing.T) {
	fenceBin, err := exec.LookPath("fence")
	if err != nil {
		t.Skip("fence not installed")
	}
	dir := t.TempDir()
	baselinePath := filepath.Join(dir, baselineFileName)
	if err := os.WriteFile(baselinePath, baselineJSONC, 0o600); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"extends": `+strconv.Quote(baselinePath)+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	want := fenceConfigShow(t, fenceBin, "--template", "code")
	got := fenceConfigShow(t, fenceBin, "--settings", settingsPath)

	// Remove the one entry we deliberately dropped, then require equality.
	network, _ := want["network"].(map[string]any)
	denied, _ := network["deniedDomains"].([]any)
	kept := denied[:0:0]
	for _, d := range denied {
		if d != "*.sentry.io" {
			kept = append(kept, d)
		}
	}
	if len(kept) == len(denied) {
		t.Fatalf("installed fence's code template no longer denies *.sentry.io; the baseline copy may be unnecessary")
	}
	network["deniedDomains"] = kept

	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("embedded baseline drifted from installed fence's code template\n--- got (baseline)\n%s\n--- want (code minus *.sentry.io)\n%s", gotJSON, wantJSON)
	}
}
