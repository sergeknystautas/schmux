package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeInjectSkill(t *testing.T) {
	loadDescriptorAdapter(t, "claude")
	adapter := GetAdapter("claude")
	if adapter == nil {
		t.Fatal("claude adapter not registered")
	}
	dir := t.TempDir()
	skill := SkillModule{
		Name:    "code-review",
		Content: "---\nname: code-review\n---\n\n## Procedure\n1. Read the PR\n",
	}
	if err := adapter.InjectSkill(dir, skill); err != nil {
		t.Fatal(err)
	}
	// Verify file exists at .claude/skills/schmux-code-review/SKILL.md
	path := filepath.Join(dir, ".claude", "skills", "schmux-code-review", "SKILL.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("skill file not created: %v", err)
	}
	if string(content) != skill.Content {
		t.Errorf("content mismatch: got %q", string(content))
	}
}

func TestClaudeRemoveSkill(t *testing.T) {
	loadDescriptorAdapter(t, "claude")
	adapter := GetAdapter("claude")
	if adapter == nil {
		t.Fatal("claude adapter not registered")
	}
	dir := t.TempDir()
	skill := SkillModule{Name: "code-review", Content: "test"}
	adapter.InjectSkill(dir, skill)
	if err := adapter.RemoveSkill(dir, "code-review"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".claude", "skills", "schmux-code-review", "SKILL.md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("skill file should be removed")
	}
}

func TestClaudeRemoveSkill_NonExistent(t *testing.T) {
	loadDescriptorAdapter(t, "claude")
	adapter := GetAdapter("claude")
	if adapter == nil {
		t.Fatal("claude adapter not registered")
	}
	dir := t.TempDir()
	// Removing a non-existent skill should not error
	if err := adapter.RemoveSkill(dir, "nonexistent"); err != nil {
		t.Errorf("expected no error for non-existent skill, got %v", err)
	}
}
