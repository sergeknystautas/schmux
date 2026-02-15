package compound

import (
	"context"
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
	}, nil)
	if err != nil {
		t.Fatalf("NewCompounder() error = %v", err)
	}
	defer c.Stop()

	c.AddWorkspace("ws-001", wsDir, overlayDir, "git@github.com:test/repo.git", manifest, nil)
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

func TestCompounder_Reconcile(t *testing.T) {
	// Set up overlay directory with original content
	overlayDir := t.TempDir()
	os.MkdirAll(filepath.Join(overlayDir, ".claude"), 0755)
	originalContent := `{"permissions": ["read"]}`
	os.WriteFile(filepath.Join(overlayDir, ".claude", "settings.json"), []byte(originalContent), 0644)

	// Set up workspace with modified content (diverged from manifest)
	wsDir := t.TempDir()
	os.MkdirAll(filepath.Join(wsDir, ".claude"), 0755)
	modifiedContent := `{"permissions": ["read", "write", "execute"]}`
	os.WriteFile(filepath.Join(wsDir, ".claude", "settings.json"), []byte(modifiedContent), 0644)

	// Manifest hash matches the original overlay (not the modified workspace)
	manifestHash := HashBytes([]byte(originalContent))
	relPath := filepath.Join(".claude", "settings.json")
	manifest := map[string]string{relPath: manifestHash}

	var manifestUpdated atomic.Int32

	c, err := NewCompounder(100, nil, nil, func(workspaceID, rp, hash string) {
		manifestUpdated.Add(1)
	})
	if err != nil {
		t.Fatalf("NewCompounder() error = %v", err)
	}
	defer c.Stop()

	c.AddWorkspace("ws-001", wsDir, overlayDir, "git@github.com:test/repo.git", manifest, nil)

	// Run reconciliation (without starting watcher — we test reconcile directly)
	c.Reconcile(context.Background(), "ws-001")

	// Verify overlay was updated with workspace content (fast path: overlay unchanged)
	got, err := os.ReadFile(filepath.Join(overlayDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("failed to read overlay after reconcile: %v", err)
	}
	if string(got) != modifiedContent {
		t.Errorf("overlay after reconcile = %q, want %q", string(got), modifiedContent)
	}

	// Verify manifest update callback was called
	if manifestUpdated.Load() == 0 {
		t.Error("expected manifestUpdate callback to be called during reconcile")
	}
}

func TestCompounder_Reconcile_RespectsContext(t *testing.T) {
	overlayDir := t.TempDir()
	wsDir := t.TempDir()

	// Create multiple files to reconcile
	os.MkdirAll(filepath.Join(overlayDir, "dir"), 0755)
	os.MkdirAll(filepath.Join(wsDir, "dir"), 0755)
	manifest := make(map[string]string)
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		relPath := filepath.Join("dir", name)
		content := "original-" + name
		os.WriteFile(filepath.Join(overlayDir, relPath), []byte(content), 0644)
		os.WriteFile(filepath.Join(wsDir, relPath), []byte("modified-"+name), 0644)
		manifest[relPath] = HashBytes([]byte(content))
	}

	c, err := NewCompounder(100, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewCompounder() error = %v", err)
	}
	defer c.Stop()

	c.AddWorkspace("ws-001", wsDir, overlayDir, "repo", manifest, nil)

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c.Reconcile(ctx, "ws-001")

	// With cancelled context, not all files should have been processed
	// (at least the reconcile should not hang)
}

func TestValidateRelPath(t *testing.T) {
	tests := []struct {
		name    string
		relPath string
		wantErr bool
	}{
		{name: "valid simple", relPath: "settings.json", wantErr: false},
		{name: "valid nested", relPath: filepath.Join(".claude", "settings.json"), wantErr: false},
		{name: "empty", relPath: "", wantErr: true},
		{name: "absolute path", relPath: "/etc/passwd", wantErr: true},
		{name: "parent traversal", relPath: "../etc/passwd", wantErr: true},
		{name: "deep traversal", relPath: "../../etc/shadow", wantErr: true},
		{name: "mid-path traversal", relPath: filepath.Join("foo", "..", "..", "etc"), wantErr: true},
		{name: "dot only", relPath: ".", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRelPath(tt.relPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRelPath(%q) error = %v, wantErr %v", tt.relPath, err, tt.wantErr)
			}
		})
	}
}

func TestCompounder_DetectsNewFileAtDeclaredPath(t *testing.T) {
	overlayDir := t.TempDir()
	wsDir := t.TempDir()
	os.MkdirAll(filepath.Join(wsDir, ".claude"), 0755)

	manifest := map[string]string{} // empty — no files exist yet
	declaredPaths := []string{filepath.Join(".claude", "settings.local.json")}

	var propagateCount atomic.Int32

	c, err := NewCompounder(100, nil, func(sourceWorkspaceID, repoURL, relPath string, content []byte) {
		propagateCount.Add(1)
	}, nil)
	if err != nil {
		t.Fatalf("NewCompounder() error = %v", err)
	}
	defer c.Stop()

	c.AddWorkspace("ws-001", wsDir, overlayDir, "repo", manifest, declaredPaths)
	c.Start()

	// Agent creates the file
	os.MkdirAll(filepath.Join(overlayDir, ".claude"), 0755)
	newContent := `{"local_setting": true}`
	os.WriteFile(filepath.Join(wsDir, ".claude", "settings.local.json"), []byte(newContent), 0644)

	// Wait for debounce + processing
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		overlayContent, _ := os.ReadFile(filepath.Join(overlayDir, ".claude", "settings.local.json"))
		if string(overlayContent) == newContent {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify overlay was created
	overlayContent, err := os.ReadFile(filepath.Join(overlayDir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("overlay file not created: %v", err)
	}
	if string(overlayContent) != newContent {
		t.Errorf("overlay content = %q, want %q", string(overlayContent), newContent)
	}
	if propagateCount.Load() == 0 {
		t.Error("expected propagation callback to be called for new file at declared path")
	}
}

func TestCompounder_DeclaredPath_FullFlow(t *testing.T) {
	overlayDir := t.TempDir()
	ws1Dir := t.TempDir()
	ws2Dir := t.TempDir()

	// Create parent dirs for declared paths
	os.MkdirAll(filepath.Join(ws1Dir, ".claude"), 0755)
	os.MkdirAll(filepath.Join(ws2Dir, ".claude"), 0755)
	os.MkdirAll(filepath.Join(overlayDir, ".claude"), 0755)

	relPath := filepath.Join(".claude", "settings.local.json")
	manifest := map[string]string{}
	declaredPaths := []string{relPath}

	var ws2Written atomic.Int32

	c, err := NewCompounder(100, nil, func(sourceWorkspaceID, repoURL, rp string, content []byte) {
		// Simulate propagation to ws2
		destPath := filepath.Join(ws2Dir, rp)
		os.MkdirAll(filepath.Dir(destPath), 0755)
		os.WriteFile(destPath, content, 0644)
		ws2Written.Add(1)
	}, nil)
	if err != nil {
		t.Fatalf("NewCompounder() error = %v", err)
	}
	defer c.Stop()

	c.AddWorkspace("ws-001", ws1Dir, overlayDir, "repo", manifest, declaredPaths)
	c.Start()

	// Agent creates the file in ws1
	fileContent := `{"setting": "from_agent"}`
	os.WriteFile(filepath.Join(ws1Dir, relPath), []byte(fileContent), 0644)

	// Wait for propagation
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ws2Written.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify overlay was written
	overlayContent, err := os.ReadFile(filepath.Join(overlayDir, relPath))
	if err != nil {
		t.Fatalf("overlay file not created: %v", err)
	}
	if string(overlayContent) != fileContent {
		t.Errorf("overlay = %q, want %q", string(overlayContent), fileContent)
	}

	// Verify propagation
	if ws2Written.Load() == 0 {
		t.Error("expected propagation to ws2")
	}
	ws2Content, _ := os.ReadFile(filepath.Join(ws2Dir, relPath))
	if string(ws2Content) != fileContent {
		t.Errorf("ws2 = %q, want %q", string(ws2Content), fileContent)
	}
}
