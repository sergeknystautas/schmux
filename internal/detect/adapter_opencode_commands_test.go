package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpencodeInjectSkill(t *testing.T) {
	dir := t.TempDir()
	loadDescriptorAdapter(t, "opencode")
	adapter := GetAdapter("opencode")
	if adapter == nil {
		t.Fatal("opencode adapter not registered")
	}
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
	loadDescriptorAdapter(t, "opencode")
	adapter := GetAdapter("opencode")
	if adapter == nil {
		t.Fatal("opencode adapter not registered")
	}
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
	loadDescriptorAdapter(t, "opencode")
	adapter := GetAdapter("opencode")
	if adapter == nil {
		t.Fatal("opencode adapter not registered")
	}
	if err := adapter.RemoveSkill(dir, "nonexistent"); err != nil {
		t.Errorf("expected no error for non-existent skill, got %v", err)
	}
}
