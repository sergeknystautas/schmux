package provision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureAgentInstructions_CreatesNewFile(t *testing.T) {
	tmpDir := t.TempDir()

	err := EnsureAgentInstructions(tmpDir, "claude")
	if err != nil {
		t.Fatalf("EnsureAgentInstructions failed: %v", err)
	}

	// Check that the file was created
	instructionPath := filepath.Join(tmpDir, ".claude", "CLAUDE.md")
	content, err := os.ReadFile(instructionPath)
	if err != nil {
		t.Fatalf("Failed to read instruction file: %v", err)
	}

	// Check that it contains the schmux markers
	if !strings.Contains(string(content), schmuxMarkerStart) {
		t.Error("File should contain SCHMUX:BEGIN marker")
	}
	if !strings.Contains(string(content), schmuxMarkerEnd) {
		t.Error("File should contain SCHMUX:END marker")
	}
	if !strings.Contains(string(content), "--<[schmux:") {
		t.Error("File should contain signaling instructions")
	}
}

func TestEnsureAgentInstructions_AppendsToExisting(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing instruction file
	instructionDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(instructionDir, 0755); err != nil {
		t.Fatal(err)
	}
	instructionPath := filepath.Join(instructionDir, "CLAUDE.md")
	existingContent := "# My Project\n\nExisting instructions here.\n"
	if err := os.WriteFile(instructionPath, []byte(existingContent), 0644); err != nil {
		t.Fatal(err)
	}

	err := EnsureAgentInstructions(tmpDir, "claude")
	if err != nil {
		t.Fatalf("EnsureAgentInstructions failed: %v", err)
	}

	content, err := os.ReadFile(instructionPath)
	if err != nil {
		t.Fatal(err)
	}

	// Check that original content is preserved
	if !strings.Contains(string(content), "My Project") {
		t.Error("Original content should be preserved")
	}
	if !strings.Contains(string(content), "Existing instructions here") {
		t.Error("Original content should be preserved")
	}

	// Check that schmux block was appended
	if !strings.Contains(string(content), schmuxMarkerStart) {
		t.Error("File should contain SCHMUX:BEGIN marker")
	}
}

func TestEnsureAgentInstructions_UpdatesExisting(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing instruction file with old schmux block
	instructionDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(instructionDir, 0755); err != nil {
		t.Fatal(err)
	}
	instructionPath := filepath.Join(instructionDir, "CLAUDE.md")
	existingContent := "# My Project\n\n" + schmuxMarkerStart + "\nOld content\n" + schmuxMarkerEnd + "\n"
	if err := os.WriteFile(instructionPath, []byte(existingContent), 0644); err != nil {
		t.Fatal(err)
	}

	err := EnsureAgentInstructions(tmpDir, "claude")
	if err != nil {
		t.Fatalf("EnsureAgentInstructions failed: %v", err)
	}

	content, err := os.ReadFile(instructionPath)
	if err != nil {
		t.Fatal(err)
	}

	// Check that old content was replaced
	if strings.Contains(string(content), "Old content") {
		t.Error("Old schmux content should be replaced")
	}

	// Check that new content is present
	if !strings.Contains(string(content), "--<[schmux:") {
		t.Error("New signaling instructions should be present")
	}

	// Should only have one set of markers
	if strings.Count(string(content), schmuxMarkerStart) != 1 {
		t.Error("Should have exactly one SCHMUX:BEGIN marker")
	}
}

func TestEnsureAgentInstructions_DifferentAgents(t *testing.T) {
	tests := []struct {
		target       string
		expectedDir  string
		expectedFile string
	}{
		{"claude", ".claude", "CLAUDE.md"},
		{"codex", ".codex", "AGENTS.md"},
		{"gemini", ".gemini", "GEMINI.md"},
		{"claude-opus", ".claude", "CLAUDE.md"},   // Model should use base tool
		{"claude-sonnet", ".claude", "CLAUDE.md"}, // Model should use base tool
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			tmpDir := t.TempDir()

			err := EnsureAgentInstructions(tmpDir, tt.target)
			if err != nil {
				t.Fatalf("EnsureAgentInstructions failed: %v", err)
			}

			instructionPath := filepath.Join(tmpDir, tt.expectedDir, tt.expectedFile)
			if _, err := os.Stat(instructionPath); os.IsNotExist(err) {
				t.Errorf("Expected instruction file at %s", instructionPath)
			}
		})
	}
}

func TestEnsureAgentInstructions_UnknownTarget(t *testing.T) {
	tmpDir := t.TempDir()

	// Unknown target should not create any files
	err := EnsureAgentInstructions(tmpDir, "unknown-agent")
	if err != nil {
		t.Fatalf("EnsureAgentInstructions should not error for unknown target: %v", err)
	}

	// No files should be created
	entries, _ := os.ReadDir(tmpDir)
	if len(entries) != 0 {
		t.Error("No files should be created for unknown target")
	}
}

func TestRemoveAgentInstructions(t *testing.T) {
	tmpDir := t.TempDir()

	// First ensure instructions exist
	if err := EnsureAgentInstructions(tmpDir, "claude"); err != nil {
		t.Fatal(err)
	}

	// Verify they exist
	if !HasSignalingInstructions(tmpDir, "claude") {
		t.Fatal("Instructions should exist after EnsureAgentInstructions")
	}

	// Remove them
	if err := RemoveAgentInstructions(tmpDir, "claude"); err != nil {
		t.Fatalf("RemoveAgentInstructions failed: %v", err)
	}

	// Verify they're gone (file should be removed since it was only the schmux block)
	instructionPath := filepath.Join(tmpDir, ".claude", "CLAUDE.md")
	if _, err := os.Stat(instructionPath); !os.IsNotExist(err) {
		t.Error("Instruction file should be removed when it only contained schmux block")
	}
}

func TestRemoveAgentInstructions_PreservesOtherContent(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file with both user content and schmux block
	instructionDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(instructionDir, 0755); err != nil {
		t.Fatal(err)
	}
	instructionPath := filepath.Join(instructionDir, "CLAUDE.md")
	content := "# My Project\n\nUser content here.\n\n" + buildSchmuxBlock()
	if err := os.WriteFile(instructionPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Remove schmux block
	if err := RemoveAgentInstructions(tmpDir, "claude"); err != nil {
		t.Fatal(err)
	}

	// File should still exist with user content
	newContent, err := os.ReadFile(instructionPath)
	if err != nil {
		t.Fatal("File should still exist after removing schmux block")
	}

	if !strings.Contains(string(newContent), "User content here") {
		t.Error("User content should be preserved")
	}
	if strings.Contains(string(newContent), schmuxMarkerStart) {
		t.Error("Schmux block should be removed")
	}
}

func TestHasSignalingInstructions(t *testing.T) {
	tmpDir := t.TempDir()

	// Should be false initially
	if HasSignalingInstructions(tmpDir, "claude") {
		t.Error("Should be false before adding instructions")
	}

	// Add instructions
	if err := EnsureAgentInstructions(tmpDir, "claude"); err != nil {
		t.Fatal(err)
	}

	// Should be true now
	if !HasSignalingInstructions(tmpDir, "claude") {
		t.Error("Should be true after adding instructions")
	}
}
