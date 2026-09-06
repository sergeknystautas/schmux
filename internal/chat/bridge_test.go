package chat

import (
	"os"
	"strings"
	"testing"
)

func TestPathsFor_EnsureAndPipeline(t *testing.T) {
	p := PathsFor(t.TempDir())
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{p.Input, p.Output, p.Errors} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
	}
	if err := os.WriteFile(p.Input, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p.Input); string(b) != "keep\n" {
		t.Fatal("Ensure truncated an existing file")
	}
	cmd := PipelineCommand("claude -p --input-format stream-json", p)
	for _, want := range []string{
		"tail -n +1 -f " + p.Input,
		"echo $! > " + p.TailPID,
		"| claude -p --input-format stream-json >> " + p.Output + " 2>> " + p.Errors,
		"kill $(cat " + p.TailPID + ")",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("pipeline %q lacks %q", cmd, want)
		}
	}
	if err := AppendInput(p, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p.Input); string(b) != "keep\n{\"a\":1}\n" {
		t.Fatalf("input: %q", b)
	}
}
