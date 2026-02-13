package compound

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompounder_EndToEnd(t *testing.T) {
	// Set up overlay directory
	overlayDir := t.TempDir()
	settingsContent := `{"permissions": ["read"]}`
	os.MkdirAll(filepath.Join(overlayDir, ".claude"), 0755)
	os.WriteFile(filepath.Join(overlayDir, ".claude", "settings.json"), []byte(settingsContent), 0644)

	// Set up workspace directory (simulating post-overlay-copy state)
	wsDir := t.TempDir()
	os.MkdirAll(filepath.Join(wsDir, ".claude"), 0755)
	os.WriteFile(filepath.Join(wsDir, ".claude", "settings.json"), []byte(settingsContent), 0644)

	// Compute manifest hash
	hash, _ := FileHash(filepath.Join(overlayDir, ".claude", "settings.json"))
	manifest := map[string]string{
		filepath.Join(".claude", "settings.json"): hash,
	}

	var propagateCount atomic.Int32

	c, err := NewCompounder(100, nil, func(sourceWorkspaceID, repoURL, relPath string, content []byte) {
		propagateCount.Add(1)
	})
	if err != nil {
		t.Fatalf("NewCompounder() error = %v", err)
	}
	defer c.Stop()

	c.AddWorkspace("ws-001", wsDir, overlayDir, "git@github.com:test/repo.git", manifest)
	c.Start()

	// Simulate agent modifying the settings file
	newContent := `{"permissions": ["read", "write"]}`
	os.WriteFile(filepath.Join(wsDir, ".claude", "settings.json"), []byte(newContent), 0644)

	// Wait for debounce + processing
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		overlayContent, _ := os.ReadFile(filepath.Join(overlayDir, ".claude", "settings.json"))
		if string(overlayContent) == newContent {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify overlay was updated
	overlayContent, _ := os.ReadFile(filepath.Join(overlayDir, ".claude", "settings.json"))
	if string(overlayContent) != newContent {
		t.Errorf("overlay content = %q, want %q", string(overlayContent), newContent)
	}
}
