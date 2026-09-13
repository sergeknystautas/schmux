package chat

import (
	"os"
	"path/filepath"
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

func TestPersistAttachment(t *testing.T) {
	dir := t.TempDir()
	path, err := PersistAttachment(dir, Image{MediaType: "image/png", Data: "aGVsbG8="})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("path %q not in %s", path, dir)
	}
	if base := filepath.Base(path); !strings.HasPrefix(base, "schmux-chat-") || !strings.HasSuffix(base, ".png") {
		t.Fatalf("name %q", base)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	b, _ := os.ReadFile(path)
	if string(b) != "hello" {
		t.Fatalf("content %q", b)
	}
}

func TestPersistAttachment_MediaTypes(t *testing.T) {
	for media, ext := range map[string]string{
		"image/png":  "png",
		"image/jpeg": "jpg",
		"image/gif":  "gif",
		"image/webp": "webp",
		"image/avif": "png", // unknown → png
	} {
		path, err := PersistAttachment(t.TempDir(), Image{MediaType: media, Data: "aGVsbG8="})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(path, "."+ext) {
			t.Fatalf("%s → %q, want .%s", media, path, ext)
		}
	}
}

func TestPersistAttachment_Failures(t *testing.T) {
	if _, err := PersistAttachment(t.TempDir(), Image{MediaType: "image/png", Data: "!!!"}); err == nil {
		t.Fatal("invalid base64 must fail")
	}
	// A file where the dir should be makes MkdirAll fail.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PersistAttachment(blocker, Image{MediaType: "image/png", Data: "aGVsbG8="}); err == nil {
		t.Fatal("unwritable dir must fail")
	}
}
