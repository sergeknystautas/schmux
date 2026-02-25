package floormanager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagerWritesInstructionFiles(t *testing.T) {
	tmpDir := t.TempDir()
	m := &Manager{
		workDir: tmpDir,
	}

	if err := m.writeInstructionFiles(); err != nil {
		t.Fatal(err)
	}

	// Check CLAUDE.md exists and has content
	claudeMd, err := os.ReadFile(filepath.Join(tmpDir, "CLAUDE.md"))
	if err != nil {
		t.Fatal("CLAUDE.md not written:", err)
	}
	if len(claudeMd) == 0 {
		t.Error("CLAUDE.md is empty")
	}

	// Check AGENTS.md is identical
	agentsMd, err := os.ReadFile(filepath.Join(tmpDir, "AGENTS.md"))
	if err != nil {
		t.Fatal("AGENTS.md not written:", err)
	}
	if string(claudeMd) != string(agentsMd) {
		t.Error("CLAUDE.md and AGENTS.md should have identical content")
	}

	// Check .claude/settings.json exists
	settings, err := os.ReadFile(filepath.Join(tmpDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatal("settings.json not written:", err)
	}
	if len(settings) == 0 {
		t.Error("settings.json is empty")
	}

	// Check memory.md is NOT overwritten if it exists
	memPath := filepath.Join(tmpDir, "memory.md")
	if err := os.WriteFile(memPath, []byte("existing memory"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := m.writeInstructionFiles(); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(memPath)
	if string(content) != "existing memory" {
		t.Error("memory.md was overwritten")
	}
}

func TestManagerInjectionCount(t *testing.T) {
	m := &Manager{}

	m.IncrementInjectionCount(5)
	if m.InjectionCount() != 5 {
		t.Errorf("expected 5, got %d", m.InjectionCount())
	}

	m.IncrementInjectionCount(3)
	if m.InjectionCount() != 8 {
		t.Errorf("expected 8, got %d", m.InjectionCount())
	}

	m.ResetInjectionCount()
	if m.InjectionCount() != 0 {
		t.Errorf("expected 0 after reset, got %d", m.InjectionCount())
	}
}
