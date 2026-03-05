package ensure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/lore"
)

func TestAgentInstructions_CreatesNewFile(t *testing.T) {
	tmpDir := t.TempDir()

	err := AgentInstructions(tmpDir, "claude", "")
	if err != nil {
		t.Fatalf("AgentInstructions failed: %v", err)
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
	if !strings.Contains(string(content), "$SCHMUX_EVENTS_FILE") {
		t.Error("File should contain signaling instructions")
	}
}

func TestAgentInstructions_AppendsToExisting(t *testing.T) {
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

	err := AgentInstructions(tmpDir, "claude", "")
	if err != nil {
		t.Fatalf("AgentInstructions failed: %v", err)
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

func TestAgentInstructions_UpdatesExisting(t *testing.T) {
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

	err := AgentInstructions(tmpDir, "claude", "")
	if err != nil {
		t.Fatalf("AgentInstructions failed: %v", err)
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
	if !strings.Contains(string(content), "$SCHMUX_EVENTS_FILE") {
		t.Error("New signaling instructions should be present")
	}

	// Should only have one set of markers
	if strings.Count(string(content), schmuxMarkerStart) != 1 {
		t.Error("Should have exactly one SCHMUX:BEGIN marker")
	}
}

func TestAgentInstructions_DifferentAgents(t *testing.T) {
	tests := []struct {
		target       string
		expectedDir  string
		expectedFile string
	}{
		{"claude", ".claude", "CLAUDE.md"},
		{"codex", ".codex", "AGENTS.md"},
		{"gemini", ".gemini", "GEMINI.md"},
		{"claude-opus-4-6", ".claude", "CLAUDE.md"},   // Model should use base tool
		{"claude-sonnet-4-6", ".claude", "CLAUDE.md"}, // Model should use base tool
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			tmpDir := t.TempDir()

			err := AgentInstructions(tmpDir, tt.target, "")
			if err != nil {
				t.Fatalf("AgentInstructions failed: %v", err)
			}

			instructionPath := filepath.Join(tmpDir, tt.expectedDir, tt.expectedFile)
			if _, err := os.Stat(instructionPath); os.IsNotExist(err) {
				t.Errorf("Expected instruction file at %s", instructionPath)
			}
		})
	}
}

func TestAgentInstructions_UnknownTarget(t *testing.T) {
	tmpDir := t.TempDir()

	// Unknown target should not create any files
	err := AgentInstructions(tmpDir, "unknown-agent", "")
	if err != nil {
		t.Fatalf("AgentInstructions should not error for unknown target: %v", err)
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
	if err := AgentInstructions(tmpDir, "claude", ""); err != nil {
		t.Fatal(err)
	}

	// Verify they exist
	if !HasSignalingInstructions(tmpDir, "claude") {
		t.Fatal("Instructions should exist after AgentInstructions")
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
	if err := AgentInstructions(tmpDir, "claude", ""); err != nil {
		t.Fatal(err)
	}

	// Should be true now
	if !HasSignalingInstructions(tmpDir, "claude") {
		t.Error("Should be true after adding instructions")
	}
}

func TestSignalingInstructionsFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := SignalingInstructionsFile(); err != nil {
		t.Fatalf("SignalingInstructionsFile failed: %v", err)
	}

	path := filepath.Join(tmpHome, ".schmux", "signaling.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read signaling file: %v", err)
	}

	if string(content) != SignalingInstructions {
		t.Fatal("signaling file content mismatch")
	}
}

func TestEnsureExcludeEntries_CreatesNewFile(t *testing.T) {
	tmpDir := t.TempDir()
	excludePath := filepath.Join(tmpDir, "info", "exclude")

	if err := ensureExcludeEntries(excludePath); err != nil {
		t.Fatalf("ensureExcludeEntries failed: %v", err)
	}

	content, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("failed to read exclude file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, excludeMarkerStart) {
		t.Error("should contain SCHMUX:BEGIN marker")
	}
	if !strings.Contains(contentStr, excludeMarkerEnd) {
		t.Error("should contain SCHMUX:END marker")
	}
	for _, pattern := range excludePatterns {
		if !strings.Contains(contentStr, pattern) {
			t.Errorf("should contain pattern %q", pattern)
		}
	}
}

func TestEnsureExcludeEntries_AppendsToExisting(t *testing.T) {
	tmpDir := t.TempDir()
	infoDir := filepath.Join(tmpDir, "info")
	if err := os.MkdirAll(infoDir, 0755); err != nil {
		t.Fatal(err)
	}
	excludePath := filepath.Join(infoDir, "exclude")
	userContent := "# user patterns\n*.log\n"
	if err := os.WriteFile(excludePath, []byte(userContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ensureExcludeEntries(excludePath); err != nil {
		t.Fatalf("ensureExcludeEntries failed: %v", err)
	}

	content, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatal(err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "*.log") {
		t.Error("user content should be preserved")
	}
	if !strings.Contains(contentStr, excludeMarkerStart) {
		t.Error("schmux block should be appended")
	}
}

func TestEnsureExcludeEntries_ReplacesStaleBlock(t *testing.T) {
	tmpDir := t.TempDir()
	infoDir := filepath.Join(tmpDir, "info")
	if err := os.MkdirAll(infoDir, 0755); err != nil {
		t.Fatal(err)
	}
	excludePath := filepath.Join(infoDir, "exclude")
	staleContent := "# user patterns\n*.log\n\n" + excludeMarkerStart + "\nold-stale-pattern\n" + excludeMarkerEnd + "\n"
	if err := os.WriteFile(excludePath, []byte(staleContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ensureExcludeEntries(excludePath); err != nil {
		t.Fatalf("ensureExcludeEntries failed: %v", err)
	}

	content, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatal(err)
	}

	contentStr := string(content)
	if strings.Contains(contentStr, "old-stale-pattern") {
		t.Error("stale content should be replaced")
	}
	if !strings.Contains(contentStr, ".schmux/events/") {
		t.Error("new patterns should be present")
	}
	if strings.Count(contentStr, excludeMarkerStart) != 1 {
		t.Error("should have exactly one SCHMUX:BEGIN marker")
	}
}

func TestEnsureExcludeEntries_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	excludePath := filepath.Join(tmpDir, "info", "exclude")

	if err := ensureExcludeEntries(excludePath); err != nil {
		t.Fatal(err)
	}
	content1, _ := os.ReadFile(excludePath)

	if err := ensureExcludeEntries(excludePath); err != nil {
		t.Fatal(err)
	}
	content2, _ := os.ReadFile(excludePath)

	if string(content1) != string(content2) {
		t.Error("ensureExcludeEntries should be idempotent")
	}
}

func TestEnsureExcludeEntries_PreservesUserEntries(t *testing.T) {
	tmpDir := t.TempDir()
	infoDir := filepath.Join(tmpDir, "info")
	if err := os.MkdirAll(infoDir, 0755); err != nil {
		t.Fatal(err)
	}
	excludePath := filepath.Join(infoDir, "exclude")

	// User entries before and after the schmux block
	before := "# before\n*.tmp\n"
	after := "# after\n*.bak\n"
	existing := before + "\n" + buildExcludeBlock() + after
	if err := os.WriteFile(excludePath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ensureExcludeEntries(excludePath); err != nil {
		t.Fatal(err)
	}

	content, _ := os.ReadFile(excludePath)
	contentStr := string(content)

	if !strings.Contains(contentStr, "*.tmp") {
		t.Error("user entries before block should be preserved")
	}
	if !strings.Contains(contentStr, "*.bak") {
		t.Error("user entries after block should be preserved")
	}
}

func TestEnsureExcludeEntries_NoTrailingNewline(t *testing.T) {
	tmpDir := t.TempDir()
	infoDir := filepath.Join(tmpDir, "info")
	if err := os.MkdirAll(infoDir, 0755); err != nil {
		t.Fatal(err)
	}
	excludePath := filepath.Join(infoDir, "exclude")
	// File without trailing newline
	if err := os.WriteFile(excludePath, []byte("*.log"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ensureExcludeEntries(excludePath); err != nil {
		t.Fatal(err)
	}

	content, _ := os.ReadFile(excludePath)
	contentStr := string(content)

	// Should not mangle the user entry
	if !strings.Contains(contentStr, "*.log") {
		t.Error("user entry should be preserved")
	}
	// The schmux block should be on its own lines (not joined to *.log)
	if strings.Contains(contentStr, "*.log"+excludeMarkerStart) {
		t.Error("block should be separated from existing content")
	}
}

func TestAgentInstructions_InjectsLoreInstructions(t *testing.T) {
	tmpDir := t.TempDir()
	loreDir := t.TempDir()

	// Setup instruction store with global + repo-private content
	store := lore.NewInstructionStore(loreDir)
	store.Write(lore.LayerUserGlobal, "", "# Global\n- Prefer table-driven tests")
	store.Write(lore.LayerRepoPrivate, "myrepo", "# Private\n- Use internal API v2")

	// Set the package-level instruction store
	oldStore := instrStore
	SetInstructionStore(store)
	defer func() { instrStore = oldStore }()

	err := AgentInstructions(tmpDir, "claude", "myrepo")
	if err != nil {
		t.Fatalf("AgentInstructions failed: %v", err)
	}

	instructionPath := filepath.Join(tmpDir, ".claude", "CLAUDE.md")
	content, err := os.ReadFile(instructionPath)
	if err != nil {
		t.Fatalf("Failed to read instruction file: %v", err)
	}

	contentStr := string(content)

	// Should contain signaling instructions
	if !strings.Contains(contentStr, "$SCHMUX_EVENTS_FILE") {
		t.Error("File should contain signaling instructions")
	}

	// Should contain lore global instructions
	if !strings.Contains(contentStr, "Prefer table-driven tests") {
		t.Error("File should contain global lore instructions")
	}

	// Should contain lore repo-private instructions
	if !strings.Contains(contentStr, "Use internal API v2") {
		t.Error("File should contain repo-private lore instructions")
	}

	// All lore content should be within the schmux markers
	startIdx := strings.Index(contentStr, schmuxMarkerStart)
	endIdx := strings.Index(contentStr, schmuxMarkerEnd)
	if startIdx == -1 || endIdx == -1 {
		t.Fatal("Schmux markers not found")
	}
	schmuxSection := contentStr[startIdx:endIdx]
	if !strings.Contains(schmuxSection, "Prefer table-driven tests") {
		t.Error("Global instructions should be within schmux markers")
	}
	if !strings.Contains(schmuxSection, "Use internal API v2") {
		t.Error("Repo-private instructions should be within schmux markers")
	}
}

func TestAgentInstructions_NoLoreWithoutStore(t *testing.T) {
	tmpDir := t.TempDir()

	// Ensure no instruction store is set
	oldStore := instrStore
	instrStore = nil
	defer func() { instrStore = oldStore }()

	err := AgentInstructions(tmpDir, "claude", "myrepo")
	if err != nil {
		t.Fatalf("AgentInstructions failed: %v", err)
	}

	instructionPath := filepath.Join(tmpDir, ".claude", "CLAUDE.md")
	content, err := os.ReadFile(instructionPath)
	if err != nil {
		t.Fatalf("Failed to read instruction file: %v", err)
	}

	// Should have signaling but no "Project Instructions" section
	if !strings.Contains(string(content), "$SCHMUX_EVENTS_FILE") {
		t.Error("File should contain signaling instructions")
	}
	if strings.Contains(string(content), "Project Instructions") {
		t.Error("File should not contain Project Instructions when no store is set")
	}
}
