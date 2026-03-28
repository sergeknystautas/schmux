package session

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteImageAttachments(t *testing.T) {
	tmpDir := t.TempDir()

	imgData := []byte("fake-png-data-for-testing")
	b64 := base64.StdEncoding.EncodeToString(imgData)

	paths, err := writeImageAttachments(tmpDir, []string{b64})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}

	// Verify file exists and has correct content
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if !bytes.Equal(data, imgData) {
		t.Errorf("file content mismatch")
	}

	// Verify file is in .schmux/attachments/
	if !strings.Contains(paths[0], filepath.Join(".schmux", "attachments")) {
		t.Errorf("expected path to contain .schmux/attachments, got %s", paths[0])
	}
}

func TestWriteImageAttachments_InvalidBase64Skipped(t *testing.T) {
	tmpDir := t.TempDir()

	validData := []byte("valid-image")
	validB64 := base64.StdEncoding.EncodeToString(validData)

	paths, err := writeImageAttachments(tmpDir, []string{"!!!invalid!!!", validB64})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 path (invalid skipped), got %d", len(paths))
	}
}

func TestAppendImagePathsToPrompt(t *testing.T) {
	prompt := "Build a login page"
	paths := []string{"/ws/.schmux/attachments/img-abc.png", "/ws/.schmux/attachments/img-def.png"}
	result := appendImagePathsToPrompt(prompt, paths)

	if !strings.HasPrefix(result, "Build a login page") {
		t.Error("original prompt should be preserved")
	}
	if !strings.Contains(result, "Image #1: /ws/.schmux/attachments/img-abc.png") {
		t.Error("missing image #1")
	}
	if !strings.Contains(result, "Image #2: /ws/.schmux/attachments/img-def.png") {
		t.Error("missing image #2")
	}
}

func TestAppendImagePathsToPrompt_Empty(t *testing.T) {
	prompt := "Build a login page"
	result := appendImagePathsToPrompt(prompt, nil)
	if result != prompt {
		t.Errorf("expected unmodified prompt, got %q", result)
	}
}
