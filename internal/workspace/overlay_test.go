package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOverlayDir(t *testing.T) {
	// Use a temp directory as HOME to avoid touching real ~/.schmux/
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	tests := []struct {
		name     string
		repoName string
		want     string
	}{
		{
			name:     "simple repo name",
			repoName: "myproject",
			want:     filepath.Join(fakeHome, ".schmux", "overlays", "myproject"),
		},
		{
			name:     "repo with hyphens",
			repoName: "my-project",
			want:     filepath.Join(fakeHome, ".schmux", "overlays", "my-project"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := OverlayDir(tt.repoName)
			if err != nil {
				t.Fatalf("OverlayDir() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("OverlayDir() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestListOverlayFiles(t *testing.T) {
	// Use a temp directory as HOME to avoid touching real ~/.schmux/
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Create a mock overlay directory structure
	repoName := "test-repo"
	overlayDir := filepath.Join(fakeHome, ".schmux", "overlays", repoName)

	// Create test overlay directory
	if err := os.MkdirAll(overlayDir, 0755); err != nil {
		t.Fatalf("failed to create overlay dir: %v", err)
	}

	// Create test files
	testFiles := []string{
		".env",
		"config/local.json",
		"credentials/service.json",
	}
	for _, file := range testFiles {
		fullPath := filepath.Join(overlayDir, file)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("failed to create parent dir: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte("test content"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	tests := []struct {
		name     string
		repoName string
		want     []string
		wantErr  bool
	}{
		{
			name:     "existing overlay with files",
			repoName: repoName,
			want:     testFiles,
			wantErr:  false,
		},
		{
			name:     "non-existent overlay",
			repoName: "nonexistent",
			want:     []string{},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ListOverlayFiles(tt.repoName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListOverlayFiles() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("ListOverlayFiles() returned %d files, want %d", len(got), len(tt.want))
				return
			}
			// Check that all expected files are present
			gotMap := make(map[string]bool)
			for _, f := range got {
				gotMap[f] = true
			}
			for _, wantFile := range tt.want {
				if !gotMap[wantFile] {
					t.Errorf("ListOverlayFiles() missing file: %s", wantFile)
				}
			}
		})
	}
}

func TestCopyFile(t *testing.T) {
	tempDir := t.TempDir()

	// Create a source file with some content
	srcFile := filepath.Join(tempDir, "source.txt")
	content := "hello world\nthis is a test file\nwith multiple lines\n"
	if err := os.WriteFile(srcFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	// Test copying to destination
	dstFile := filepath.Join(tempDir, "dest.txt")
	if err := copyFile(srcFile, dstFile, 0644); err != nil {
		t.Fatalf("copyFile() error = %v", err)
	}

	// Verify content was copied correctly
	gotContent, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}
	if string(gotContent) != content {
		t.Errorf("copyFile() content mismatch\ngot:  %q\nwant: %q", string(gotContent), content)
	}

	// Verify file permissions
	info, err := os.Stat(dstFile)
	if err != nil {
		t.Fatalf("failed to stat destination file: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("copyFile() permissions = %v, want %v", info.Mode().Perm(), 0644)
	}
}

func TestIsIgnoredByGit(t *testing.T) {
	// This test requires a git repository, so we'll create a temporary one
	tempDir := t.TempDir()

	// Initialize git repo
	ctx := context.Background()
	if err := runGitCommand(ctx, tempDir, "init"); err != nil {
		t.Skipf("git not available: %v", err)
		return
	}

	// Create a .gitignore file
	gitignoreContent := "*.env\nconfig/secrets/\n"
	if err := os.WriteFile(filepath.Join(tempDir, ".gitignore"), []byte(gitignoreContent), 0644); err != nil {
		t.Fatalf("failed to create .gitignore: %v", err)
	}

	// Create some test files (but don't actually create them - we just test the gitignore check)
	tests := []struct {
		name     string
		filePath string
		want     bool
	}{
		{
			name:     "file matching .gitignore pattern",
			filePath: ".env",
			want:     true,
		},
		{
			name:     "file in ignored directory",
			filePath: "config/secrets/key.txt",
			want:     true,
		},
		{
			name:     "file not matching any pattern",
			filePath: "README.md",
			want:     false,
		},
		{
			name:     "Go file (typically not ignored)",
			filePath: "main.go",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := isIgnoredByGit(ctx, tempDir, tt.filePath)
			if err != nil {
				t.Errorf("isIgnoredByGit() unexpected error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("isIgnoredByGit() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper function to run git commands in tests
func runGitCommand(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	return cmd.Run()
}

func TestCopyOverlayReturnsManifest(t *testing.T) {
	ctx := context.Background()

	// Create temp overlay source directory with test files
	overlayDir := t.TempDir()
	testFiles := map[string]string{
		".env":                 "SECRET=abc123\n",
		"config/local.json":    `{"key": "value"}`,
		"data/credentials.txt": "user:pass\n",
	}
	for relPath, content := range testFiles {
		fullPath := filepath.Join(overlayDir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("failed to create parent dir for %s: %v", relPath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write overlay file %s: %v", relPath, err)
		}
	}

	// Create temp workspace directory initialized as a git repo
	workspaceDir := t.TempDir()
	if err := runGitCommand(ctx, workspaceDir, "init"); err != nil {
		t.Skipf("git not available: %v", err)
		return
	}

	// Create .gitignore that covers the overlay files
	gitignoreContent := ".env\nconfig/\ndata/\n"
	if err := os.WriteFile(filepath.Join(workspaceDir, ".gitignore"), []byte(gitignoreContent), 0644); err != nil {
		t.Fatalf("failed to create .gitignore: %v", err)
	}

	// Call CopyOverlay
	manifest, err := CopyOverlay(ctx, overlayDir, workspaceDir)
	if err != nil {
		t.Fatalf("CopyOverlay() error = %v", err)
	}

	// Verify the manifest has entries for each copied file
	if len(manifest) != len(testFiles) {
		t.Errorf("manifest has %d entries, want %d", len(manifest), len(testFiles))
	}

	for relPath := range testFiles {
		hash, ok := manifest[relPath]
		if !ok {
			t.Errorf("manifest missing entry for %s", relPath)
			continue
		}
		// SHA-256 hex digest should be exactly 64 characters
		if len(hash) != 64 {
			t.Errorf("hash for %s has length %d, want 64", relPath, len(hash))
		}
		// Verify it's valid hex
		for _, c := range hash {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("hash for %s contains non-hex character: %c", relPath, c)
				break
			}
		}
	}

	// Verify the files were actually copied
	for relPath, content := range testFiles {
		destPath := filepath.Join(workspaceDir, relPath)
		gotContent, err := os.ReadFile(destPath)
		if err != nil {
			t.Errorf("failed to read copied file %s: %v", relPath, err)
			continue
		}
		if string(gotContent) != content {
			t.Errorf("copied file %s content mismatch: got %q, want %q", relPath, string(gotContent), content)
		}
	}
}
