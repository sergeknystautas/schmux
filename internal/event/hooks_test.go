package event

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureGlobalHookScripts(t *testing.T) {
	t.Run("creates hooks directory and writes all scripts", func(t *testing.T) {
		homeDir := t.TempDir()

		hooksDir, err := EnsureGlobalHookScripts(homeDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedDir := filepath.Join(homeDir, ".schmux", "hooks")
		if hooksDir != expectedDir {
			t.Errorf("returned path = %q, want %q", hooksDir, expectedDir)
		}

		// Verify directory exists
		info, err := os.Stat(hooksDir)
		if err != nil {
			t.Fatalf("hooks directory does not exist: %v", err)
		}
		if !info.IsDir() {
			t.Fatal("hooks path is not a directory")
		}

		// Verify all three scripts exist and are executable
		scripts := []string{
			"capture-failure.sh",
			"stop-status-check.sh",
			"stop-lore-check.sh",
		}
		for _, name := range scripts {
			path := filepath.Join(hooksDir, name)
			info, err := os.Stat(path)
			if err != nil {
				t.Errorf("script %q does not exist: %v", name, err)
				continue
			}

			// Check file is not empty
			if info.Size() == 0 {
				t.Errorf("script %q is empty", name)
			}

			// Check file is executable (owner execute bit)
			mode := info.Mode()
			if mode&0100 == 0 {
				t.Errorf("script %q is not executable: mode=%v", name, mode)
			}
		}
	})

	t.Run("files are executable with 0755 permissions", func(t *testing.T) {
		homeDir := t.TempDir()

		hooksDir, err := EnsureGlobalHookScripts(homeDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		scripts := []string{
			"capture-failure.sh",
			"stop-status-check.sh",
			"stop-lore-check.sh",
		}
		for _, name := range scripts {
			path := filepath.Join(hooksDir, name)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("script %q does not exist: %v", name, err)
			}

			// On Unix, verify the permission bits
			mode := info.Mode().Perm()
			if mode != 0755 {
				t.Errorf("script %q has permissions %o, want 0755", name, mode)
			}
		}
	})

	t.Run("scripts start with shebang", func(t *testing.T) {
		homeDir := t.TempDir()

		hooksDir, err := EnsureGlobalHookScripts(homeDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		scripts := []string{
			"capture-failure.sh",
			"stop-status-check.sh",
			"stop-lore-check.sh",
		}
		for _, name := range scripts {
			path := filepath.Join(hooksDir, name)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read %q: %v", name, err)
			}
			if len(content) < 2 || string(content[:2]) != "#!" {
				t.Errorf("script %q does not start with shebang (#!), starts with: %q", name, string(content[:min(10, len(content))]))
			}
		}
	})

	t.Run("idempotent - second call succeeds", func(t *testing.T) {
		homeDir := t.TempDir()

		hooksDir1, err := EnsureGlobalHookScripts(homeDir)
		if err != nil {
			t.Fatalf("first call: unexpected error: %v", err)
		}

		hooksDir2, err := EnsureGlobalHookScripts(homeDir)
		if err != nil {
			t.Fatalf("second call: unexpected error: %v", err)
		}

		if hooksDir1 != hooksDir2 {
			t.Errorf("first call returned %q, second call returned %q", hooksDir1, hooksDir2)
		}

		// Verify scripts still exist and are valid after second call
		scripts := []string{
			"capture-failure.sh",
			"stop-status-check.sh",
			"stop-lore-check.sh",
		}
		for _, name := range scripts {
			path := filepath.Join(hooksDir2, name)
			info, err := os.Stat(path)
			if err != nil {
				t.Errorf("script %q does not exist after second call: %v", name, err)
				continue
			}
			if info.Size() == 0 {
				t.Errorf("script %q is empty after second call", name)
			}
		}
	})

	t.Run("returns correct hooks directory path", func(t *testing.T) {
		homeDir := t.TempDir()

		hooksDir, err := EnsureGlobalHookScripts(homeDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// The path should be <homeDir>/.schmux/hooks
		expected := filepath.Join(homeDir, ".schmux", "hooks")
		if hooksDir != expected {
			t.Errorf("hooksDir = %q, want %q", hooksDir, expected)
		}
	})
}
