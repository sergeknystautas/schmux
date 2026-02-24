# Dev Config Backup Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Automatically backup config.json, secrets.json, and state.json to a tar.gz archive when the daemon starts in dev mode.

**Architecture:** Add a `createDevConfigBackup()` helper function to daemon.go that creates a timestamped tar.gz backup and cleans up old backups. Called at the start of `Run()` when `devMode=true`.

**Tech Stack:** Go standard library (`archive/tar`, `compress/gzip`, `os`, `filepath`, `time`)

---

### Task 1: Write unit tests for backup function

**Files:**

- Create: `internal/daemon/backup_test.go`

**Step 1: Create test file with test cases**

```go
package daemon

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateDevConfigBackup(t *testing.T) {
	// Create temp directory to act as schmuxDir
	tmpDir := t.TempDir()
	backupDir := filepath.Join(tmpDir, "backups")

	// Create test config files
	configContent := []byte(`{"repos": []}`)
	secretsContent := []byte(`{"api_keys": {}}`)
	stateContent := []byte(`{"workspaces": [], "sessions": []}`)

	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), configContent, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "secrets.json"), secretsContent, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "state.json"), stateContent, 0644); err != nil {
		t.Fatal(err)
	}

	// Run backup
	err := createDevConfigBackup(tmpDir)
	if err != nil {
		t.Fatalf("createDevConfigBackup failed: %v", err)
	}

	// Verify backup directory was created
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		t.Fatal("backups directory was not created")
	}

	// Find the backup file
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 backup file, got %d", len(entries))
	}

	// Verify filename format: config-YYYY-MM-DDTHH-MM-SS_<dir>.tar.gz
	backupFile := entries[0]
	if backupFile.IsDir() {
		t.Fatal("backup should be a file, not a directory")
	}
	name := backupFile.Name()
	if len(name) < 30 || !filepath.Ext(name, ".tar.gz") {
		t.Fatalf("backup filename has wrong format: %s", name)
	}

	// Verify archive contents
	f, err := os.Open(filepath.Join(backupDir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	foundFiles := make(map[string]bool)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		foundFiles[header.Name] = true

		// Read and verify content
		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		switch header.Name {
		case "config.json":
			if string(content) != string(configContent) {
				t.Errorf("config.json content mismatch")
			}
		case "secrets.json":
			if string(content) != string(secretsContent) {
				t.Errorf("secrets.json content mismatch")
			}
		case "state.json":
			if string(content) != string(stateContent) {
				t.Errorf("state.json content mismatch")
			}
		}
	}

	// Verify all three files are in archive
	for _, expected := range []string{"config.json", "secrets.json", "state.json"} {
		if !foundFiles[expected] {
			t.Errorf("missing %s in backup archive", expected)
		}
	}
}

func TestCreateDevConfigBackupSkipsMissingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Only create config.json, not secrets.json or state.json
	configContent := []byte(`{"repos": []}`)
	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), configContent, 0644); err != nil {
		t.Fatal(err)
	}

	// Run backup - should not error
	err := createDevConfigBackup(tmpDir)
	if err != nil {
		t.Fatalf("createDevConfigBackup failed: %v", err)
	}

	// Verify backup was still created
	backupDir := filepath.Join(tmpDir, "backups")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 backup file, got %d", len(entries))
	}

	// Verify only config.json is in archive
	f, err := os.Open(filepath.Join(backupDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	foundFiles := make(map[string]bool)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		foundFiles[header.Name] = true
	}

	if !foundFiles["config.json"] {
		t.Error("config.json should be in backup")
	}
	if foundFiles["secrets.json"] {
		t.Error("secrets.json should not be in backup (doesn't exist)")
	}
	if foundFiles["state.json"] {
		t.Error("state.json should not be in backup (doesn't exist)")
	}
}

func TestCleanupOldBackups(t *testing.T) {
	tmpDir := t.TempDir()
	backupDir := filepath.Join(tmpDir, "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create old backup file (4 days old)
	oldFile := filepath.Join(backupDir, "config-2026-02-20T10-00-00_old.tar.gz")
	if err := os.WriteFile(oldFile, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	// Set modification time to 4 days ago
	oldTime := time.Now().Add(-4 * 24 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Create recent backup file (1 day old)
	recentFile := filepath.Join(backupDir, "config-2026-02-23T10-00-00_recent.tar.gz")
	if err := os.WriteFile(recentFile, []byte("recent"), 0644); err != nil {
		t.Fatal(err)
	}
	recentTime := time.Now().Add(-1 * 24 * time.Hour)
	if err := os.Chtimes(recentFile, recentTime, recentTime); err != nil {
		t.Fatal(err)
	}

	// Run cleanup
	cleanupOldBackups(backupDir, 3*24*time.Hour)

	// Verify old file was deleted
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("old backup should have been deleted")
	}

	// Verify recent file still exists
	if _, err := os.Stat(recentFile); os.IsNotExist(err) {
		t.Error("recent backup should still exist")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/daemon/... -run "TestCreateDevConfigBackup|TestCleanupOldBackups" -v`
Expected: FAIL - undefined functions

---

### Task 2: Implement backup creation function

**Files:**

- Modify: `internal/daemon/daemon.go`

**Step 1: Add imports at top of daemon.go**

Add to existing imports:

```go
import (
	"archive/tar"
	"compress/gzip"
	// ... existing imports ...
)
```

**Step 2: Add createDevConfigBackup function at end of daemon.go**

```go
// createDevConfigBackup creates a tar.gz backup of config.json, secrets.json,
// and state.json in ~/.schmux/backups/. Only runs in dev mode.
func createDevConfigBackup(schmuxDir string) error {
	backupDir := filepath.Join(schmuxDir, "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backups directory: %w", err)
	}

	// Get current working directory name for filename
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	dirName := filepath.Base(cwd)

	// Generate filename: config-2026-02-23T19-33-00_schmux-002.tar.gz
	timestamp := time.Now().UTC().Format("2006-01-02T15-04-05")
	filename := fmt.Sprintf("config-%s_%s.tar.gz", timestamp, dirName)
	backupPath := filepath.Join(backupDir, filename)

	// Files to backup
	files := []string{
		"config.json",
		"secrets.json",
		"state.json",
	}

	// Create the tar.gz file
	f, err := os.Create(backupPath)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer f.Close()

	gzw := gzip.NewWriter(f)
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	for _, name := range files {
		srcPath := filepath.Join(schmuxDir, name)
		content, err := os.ReadFile(srcPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Skip missing files silently
			}
			return fmt.Errorf("failed to read %s: %w", name, err)
		}

		header := &tar.Header{
			Name: name,
			Mode: 0600,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write tar header for %s: %w", name, err)
		}
		if _, err := tw.Write(content); err != nil {
			return fmt.Errorf("failed to write %s to tar: %w", name, err)
		}
	}

	// Clean up old backups (older than 3 days)
	cleanupOldBackups(backupDir, 3*24*time.Hour)

	return nil
}

// cleanupOldBackups removes backup files older than maxAge.
func cleanupOldBackups(backupDir string, maxAge time.Duration) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Only clean up config backup files
		if len(name) < 7 || name[:7] != "config-" || filepath.Ext(name) != ".gz" {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			filePath := filepath.Join(backupDir, name)
			os.Remove(filePath)
			// Silent cleanup - don't log from this low-level function
		}
	}
}
```

**Step 3: Add call site in Run() function**

In `Run()`, after the `schmuxDir` creation and before the PID file write (around line 318), add:

```go
	// Create dev config backup if in dev mode
	if devMode {
		if err := createDevConfigBackup(schmuxDir); err != nil {
			logger.Warn("failed to create dev config backup", "err", err)
			// Don't fail daemon startup for backup failure
		}
	}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/daemon/... -run "TestCreateDevConfigBackup|TestCleanupOldBackups" -v`
Expected: PASS

---

### Task 3: Manual integration test

**Files:**

- None (manual verification)

**Step 1: Build and run in dev mode**

Run: `./dev.sh`
Wait for daemon to start, then Ctrl+C to stop.

**Step 2: Verify backup was created**

Run: `ls -la ~/.schmux/backups/`
Expected: See file like `config-2026-02-24T10-30-00_schmux-002.tar.gz`

**Step 3: Verify archive contents**

Run: `tar -tzf ~/.schmux/backups/config-*.tar.gz`
Expected: Lists config.json, secrets.json, state.json

---

### Task 4: Commit

**Step 1: Commit changes**

```bash
git add internal/daemon/daemon.go internal/daemon/backup_test.go docs/specs/2026-02-23-dev-config-backup-design.md docs/plans/2026-02-24-dev-config-backup.md
git commit -m "$(cat <<'EOF'
feat(daemon): add dev config backup on daemon start

When running in dev mode, automatically create a tar.gz backup of
config.json, secrets.json, and state.json to ~/.schmux/backups/.
Backups older than 3 days are automatically cleaned up.

Filename format: config-<timestamp>_<cwd-basename>.tar.gz

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```
