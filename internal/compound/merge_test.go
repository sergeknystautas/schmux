package compound

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- helpers ---

// writeFile creates a file with content in the given directory.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	return path
}

// hashOf returns the SHA-256 hex digest of the given content.
func hashOf(t *testing.T, content string) string {
	t.Helper()
	return HashBytes([]byte(content))
}

// mockExecutor returns an LLMExecutor that returns a canned response.
func mockExecutor(response string) LLMExecutor {
	return func(ctx context.Context, prompt string, timeout time.Duration) (string, error) {
		return response, nil
	}
}

// failingExecutor returns an LLMExecutor that always fails.
func failingExecutor(msg string) LLMExecutor {
	return func(ctx context.Context, prompt string, timeout time.Duration) (string, error) {
		return "", errors.New(msg)
	}
}

// emptyExecutor returns an LLMExecutor that returns an empty string.
func emptyExecutor() LLMExecutor {
	return func(ctx context.Context, prompt string, timeout time.Duration) (string, error) {
		return "", nil
	}
}

// --- DetermineMergeAction tests ---

func TestDetermineMergeAction_Skip_UnchangedFromManifest(t *testing.T) {
	dir := t.TempDir()
	content := "unchanged content"
	wsPath := writeFile(t, dir, "ws.txt", content)
	overlayPath := writeFile(t, dir, "overlay.txt", "overlay content")
	manifestHash := hashOf(t, content)

	action, err := DetermineMergeAction(wsPath, overlayPath, manifestHash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != MergeActionSkip {
		t.Errorf("expected MergeActionSkip, got %d", action)
	}
}

func TestDetermineMergeAction_Skip_IdenticalToOverlay(t *testing.T) {
	dir := t.TempDir()
	content := "same in both"
	wsPath := writeFile(t, dir, "ws.txt", content)
	overlayPath := writeFile(t, dir, "overlay.txt", content)
	// Manifest hash is different from both
	manifestHash := hashOf(t, "original content")

	action, err := DetermineMergeAction(wsPath, overlayPath, manifestHash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != MergeActionSkip {
		t.Errorf("expected MergeActionSkip (identical to overlay), got %d", action)
	}
}

func TestDetermineMergeAction_FastPath_OverlayUnchanged(t *testing.T) {
	dir := t.TempDir()
	original := "original content"
	wsPath := writeFile(t, dir, "ws.txt", "modified by workspace")
	overlayPath := writeFile(t, dir, "overlay.txt", original)
	manifestHash := hashOf(t, original)

	action, err := DetermineMergeAction(wsPath, overlayPath, manifestHash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != MergeActionFastPath {
		t.Errorf("expected MergeActionFastPath, got %d", action)
	}
}

func TestDetermineMergeAction_FastPath_OverlayMissing(t *testing.T) {
	dir := t.TempDir()
	wsPath := writeFile(t, dir, "ws.txt", "workspace content")
	overlayPath := filepath.Join(dir, "nonexistent_overlay.txt")
	manifestHash := hashOf(t, "original content")

	action, err := DetermineMergeAction(wsPath, overlayPath, manifestHash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != MergeActionFastPath {
		t.Errorf("expected MergeActionFastPath (overlay missing), got %d", action)
	}
}

func TestDetermineMergeAction_LLMMerge_BothDiverged(t *testing.T) {
	dir := t.TempDir()
	wsPath := writeFile(t, dir, "ws.txt", "workspace version")
	overlayPath := writeFile(t, dir, "overlay.txt", "overlay version")
	manifestHash := hashOf(t, "original version")

	action, err := DetermineMergeAction(wsPath, overlayPath, manifestHash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != MergeActionLLMMerge {
		t.Errorf("expected MergeActionLLMMerge, got %d", action)
	}
}

func TestDetermineMergeAction_Error_WorkspaceFileMissing(t *testing.T) {
	dir := t.TempDir()
	wsPath := filepath.Join(dir, "nonexistent.txt")
	overlayPath := writeFile(t, dir, "overlay.txt", "overlay")

	_, err := DetermineMergeAction(wsPath, overlayPath, "somehash")
	if err == nil {
		t.Fatalf("expected error for missing workspace file, got nil")
	}
	if !strings.Contains(err.Error(), "failed to hash workspace file") {
		t.Errorf("expected 'failed to hash workspace file' in error, got: %v", err)
	}
}

// --- ExecuteMerge tests ---

func TestExecuteMerge_Skip(t *testing.T) {
	result, err := ExecuteMerge(context.Background(), MergeActionSkip, "", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for skip, got %q", result)
	}
}

func TestExecuteMerge_FastPath(t *testing.T) {
	dir := t.TempDir()
	wsContent := "workspace content for fast path"
	wsPath := writeFile(t, dir, "ws.txt", wsContent)
	overlayPath := filepath.Join(dir, "overlay.txt")

	result, err := ExecuteMerge(context.Background(), MergeActionFastPath, wsPath, overlayPath, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != wsContent {
		t.Errorf("expected result %q, got %q", wsContent, result)
	}

	// Verify the overlay file was written
	got, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatalf("failed to read overlay: %v", err)
	}
	if string(got) != wsContent {
		t.Errorf("overlay content = %q, want %q", got, wsContent)
	}
}

func TestExecuteMerge_FastPath_ReadError(t *testing.T) {
	dir := t.TempDir()
	wsPath := filepath.Join(dir, "nonexistent.txt")
	overlayPath := filepath.Join(dir, "overlay.txt")

	_, err := ExecuteMerge(context.Background(), MergeActionFastPath, wsPath, overlayPath, nil)
	if err == nil {
		t.Fatal("expected error for missing workspace file")
	}
	if !strings.Contains(err.Error(), "failed to read workspace file") {
		t.Errorf("expected 'failed to read workspace file' in error, got: %v", err)
	}
}

func TestExecuteMerge_LLMMerge_Success(t *testing.T) {
	dir := t.TempDir()
	wsPath := writeFile(t, dir, "ws.txt", "workspace version")
	overlayPath := writeFile(t, dir, "overlay.txt", "overlay version")

	mergedContent := "merged result from LLM"
	executor := mockExecutor(mergedContent)

	result, err := ExecuteMerge(context.Background(), MergeActionLLMMerge, wsPath, overlayPath, executor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != mergedContent {
		t.Errorf("expected result %q, got %q", mergedContent, result)
	}

	// Verify overlay was updated with merged content
	got, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatalf("failed to read overlay: %v", err)
	}
	if string(got) != mergedContent {
		t.Errorf("overlay = %q, want %q", got, mergedContent)
	}
}

func TestExecuteMerge_LLMMerge_ExecutorFails_FallbackToLWW(t *testing.T) {
	dir := t.TempDir()
	wsContent := "workspace wins"
	wsPath := writeFile(t, dir, "ws.txt", wsContent)
	overlayPath := writeFile(t, dir, "overlay.txt", "overlay version")

	executor := failingExecutor("LLM unavailable")

	result, err := ExecuteMerge(context.Background(), MergeActionLLMMerge, wsPath, overlayPath, executor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != wsContent {
		t.Errorf("expected LWW fallback result %q, got %q", wsContent, result)
	}

	// Verify overlay was overwritten with workspace content (LWW)
	got, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatalf("failed to read overlay: %v", err)
	}
	if string(got) != wsContent {
		t.Errorf("overlay = %q, want %q (LWW)", got, wsContent)
	}
}

func TestExecuteMerge_LLMMerge_EmptyResponse_FallbackToLWW(t *testing.T) {
	dir := t.TempDir()
	wsContent := "workspace content"
	wsPath := writeFile(t, dir, "ws.txt", wsContent)
	overlayPath := writeFile(t, dir, "overlay.txt", "overlay content")

	executor := emptyExecutor()

	result, err := ExecuteMerge(context.Background(), MergeActionLLMMerge, wsPath, overlayPath, executor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != wsContent {
		t.Errorf("expected LWW fallback result %q, got %q", wsContent, result)
	}
}

func TestExecuteMerge_LLMMerge_NilExecutor_FallbackToLWW(t *testing.T) {
	dir := t.TempDir()
	wsContent := "workspace content"
	wsPath := writeFile(t, dir, "ws.txt", wsContent)
	overlayPath := writeFile(t, dir, "overlay.txt", "overlay content")

	result, err := ExecuteMerge(context.Background(), MergeActionLLMMerge, wsPath, overlayPath, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != wsContent {
		t.Errorf("expected LWW fallback result %q, got %q", wsContent, result)
	}
}

func TestExecuteMerge_LLMMerge_BinaryFile_LWW(t *testing.T) {
	dir := t.TempDir()
	// Create a binary file with null bytes
	binaryContent := []byte{0x89, 0x50, 0x4E, 0x47, 0x00, 0x00, 0x00, 0x01}
	wsPath := filepath.Join(dir, "image.png")
	if err := os.WriteFile(wsPath, binaryContent, 0644); err != nil {
		t.Fatal(err)
	}
	overlayPath := writeFile(t, dir, "overlay.png", "old overlay")

	// Executor should NOT be called for binary files
	executor := func(ctx context.Context, prompt string, timeout time.Duration) (string, error) {
		t.Error("executor should not be called for binary files")
		return "", nil
	}

	result, err := ExecuteMerge(context.Background(), MergeActionLLMMerge, wsPath, overlayPath, executor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != string(binaryContent) {
		t.Errorf("expected binary LWW result, got different content")
	}
}

func TestExecuteMerge_UnknownAction(t *testing.T) {
	_, err := ExecuteMerge(context.Background(), MergeAction(99), "", "", nil)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "unknown merge action") {
		t.Errorf("expected 'unknown merge action' error, got: %v", err)
	}
}

// --- BuildMergePrompt tests ---

func TestBuildMergePrompt_ContainsBothVersions(t *testing.T) {
	overlay := "overlay content here"
	workspace := "workspace content here"

	prompt := BuildMergePrompt(overlay, workspace)

	if !strings.Contains(prompt, overlay) {
		t.Error("prompt should contain overlay content")
	}
	if !strings.Contains(prompt, workspace) {
		t.Error("prompt should contain workspace content")
	}
	if !strings.Contains(prompt, "VERSION A") {
		t.Error("prompt should reference VERSION A")
	}
	if !strings.Contains(prompt, "VERSION B") {
		t.Error("prompt should reference VERSION B")
	}
	if !strings.Contains(prompt, "prefer VERSION B") {
		t.Error("prompt should indicate preference for VERSION B")
	}
}

func TestBuildMergePrompt_ContainsMergeRules(t *testing.T) {
	prompt := BuildMergePrompt("a", "b")

	expectedPhrases := []string{
		"union the arrays",
		"Never remove entries",
		"no explanation or markdown fencing",
	}
	for _, phrase := range expectedPhrases {
		if !strings.Contains(prompt, phrase) {
			t.Errorf("prompt should contain %q", phrase)
		}
	}
}

// --- JSONL line-union merge ---

func TestExecuteMerge_JSONLLineUnion(t *testing.T) {
	dir := t.TempDir()
	wsDir := filepath.Join(dir, "ws", ".claude")
	overlayDir := filepath.Join(dir, "overlay", ".claude")
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(overlayDir, 0755); err != nil {
		t.Fatal(err)
	}

	wsPath := filepath.Join(wsDir, "lore.jsonl")
	overlayPath := filepath.Join(overlayDir, "lore.jsonl")

	// Workspace has lines A and C
	if err := os.WriteFile(wsPath, []byte("{\"ts\":\"1\",\"text\":\"A\"}\n{\"ts\":\"3\",\"text\":\"C\"}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Overlay has lines A and B
	if err := os.WriteFile(overlayPath, []byte("{\"ts\":\"1\",\"text\":\"A\"}\n{\"ts\":\"2\",\"text\":\"B\"}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Both diverged from manifest, so this is LLMMerge action.
	// But for .jsonl, it should use line-union instead of LLM.
	content, err := ExecuteMerge(context.Background(), MergeActionLLMMerge, wsPath, overlayPath, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := string(content)
	if !strings.Contains(result, `"text":"A"`) {
		t.Error("merged content should contain A")
	}
	if !strings.Contains(result, `"text":"B"`) {
		t.Error("merged content should contain B")
	}
	if !strings.Contains(result, `"text":"C"`) {
		t.Error("merged content should contain C")
	}

	// Verify A appears only once
	if strings.Count(result, `"text":"A"`) != 1 {
		t.Error("A should appear exactly once (deduped)")
	}
}

// --- DetermineMergeAction table-driven ---

func TestDetermineMergeAction_TableDriven(t *testing.T) {
	original := "original content"
	modified := "modified content"
	overlayModified := "overlay modified"

	tests := []struct {
		name         string
		wsContent    string
		overlaySet   bool
		overlayText  string
		manifestBase string
		want         MergeAction
	}{
		{
			name:         "skip: ws matches manifest",
			wsContent:    original,
			overlaySet:   true,
			overlayText:  "anything",
			manifestBase: original,
			want:         MergeActionSkip,
		},
		{
			name:         "skip: ws and overlay identical, both differ from manifest",
			wsContent:    modified,
			overlaySet:   true,
			overlayText:  modified,
			manifestBase: original,
			want:         MergeActionSkip,
		},
		{
			name:         "fast path: overlay matches manifest, ws differs",
			wsContent:    modified,
			overlaySet:   true,
			overlayText:  original,
			manifestBase: original,
			want:         MergeActionFastPath,
		},
		{
			name:         "fast path: overlay missing",
			wsContent:    modified,
			overlaySet:   false,
			manifestBase: original,
			want:         MergeActionFastPath,
		},
		{
			name:         "llm merge: all three differ",
			wsContent:    modified,
			overlaySet:   true,
			overlayText:  overlayModified,
			manifestBase: original,
			want:         MergeActionLLMMerge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			wsPath := writeFile(t, dir, "ws.txt", tt.wsContent)

			overlayPath := filepath.Join(dir, "overlay.txt")
			if tt.overlaySet {
				writeFile(t, dir, "overlay.txt", tt.overlayText)
			}

			manifestHash := hashOf(t, tt.manifestBase)

			got, err := DetermineMergeAction(wsPath, overlayPath, manifestHash)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("DetermineMergeAction() = %d, want %d", got, tt.want)
			}
		})
	}
}
