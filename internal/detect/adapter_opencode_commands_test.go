package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpencodeSetupCommands(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	adapter := &OpencodeAdapter{}
	err := adapter.SetupCommands(dir)
	if err != nil {
		t.Fatalf("SetupCommands error: %v", err)
	}

	commitPath := filepath.Join(dir, ".opencode", "commands", "commit.md")
	content, err := os.ReadFile(commitPath)
	if err != nil {
		t.Fatalf("failed to read commit command: %v", err)
	}

	// Verify YAML frontmatter
	if !strings.HasPrefix(string(content), "---\n") {
		t.Error("commit command should start with YAML frontmatter")
	}

	// Verify key content
	if !strings.Contains(string(content), "Definition of Done") {
		t.Error("commit command should contain Definition of Done")
	}
	if !strings.Contains(string(content), "./test.sh") {
		t.Error("commit command should reference test.sh")
	}
	if !strings.Contains(string(content), "go vet") {
		t.Error("commit command should reference go vet")
	}
}

func TestOpencodeSetupCommandsIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	adapter := &OpencodeAdapter{}
	_ = adapter.SetupCommands(dir)
	_ = adapter.SetupCommands(dir)

	commitPath := filepath.Join(dir, ".opencode", "commands", "commit.md")
	content, err := os.ReadFile(commitPath)
	if err != nil {
		t.Fatalf("failed to read commit command: %v", err)
	}
	if !strings.Contains(string(content), "Definition of Done") {
		t.Error("commit command should still be valid after second run")
	}
}
