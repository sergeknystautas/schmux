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

func TestOpencodeInjectSkill(t *testing.T) {
	dir := t.TempDir()
	adapter := &OpencodeAdapter{}
	skill := SkillModule{
		Name:    "code-review",
		Content: "---\nname: code-review\n---\n\n## Procedure\n1. Read the PR\n",
	}
	if err := adapter.InjectSkill(dir, skill); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".opencode", "commands", "schmux-code-review.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("skill file not created: %v", err)
	}
	if string(content) != skill.Content {
		t.Errorf("content mismatch: got %q", string(content))
	}
}

func TestOpencodeRemoveSkill(t *testing.T) {
	dir := t.TempDir()
	adapter := &OpencodeAdapter{}
	skill := SkillModule{Name: "code-review", Content: "test"}
	adapter.InjectSkill(dir, skill)
	if err := adapter.RemoveSkill(dir, "code-review"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".opencode", "commands", "schmux-code-review.md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("skill file should be removed")
	}
}

func TestOpencodeRemoveSkill_NonExistent(t *testing.T) {
	dir := t.TempDir()
	adapter := &OpencodeAdapter{}
	if err := adapter.RemoveSkill(dir, "nonexistent"); err != nil {
		t.Errorf("expected no error for non-existent skill, got %v", err)
	}
}
