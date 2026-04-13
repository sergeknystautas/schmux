package daemon

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func TestCreateDevConfigBackup(t *testing.T) {
	// Setup: create temp schmuxDir with config files
	tmpDir := t.TempDir()
	schmuxDir := filepath.Join(tmpDir, ".schmux")
	schmuxdir.Set(schmuxDir)
	defer schmuxdir.Set("")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		t.Fatalf("failed to create schmux dir: %v", err)
	}

	// Create the three config files
	configFiles := map[string]string{
		"config.json":  `{"repos": []}`,
		"secrets.json": `{"api_keys": {}}`,
		"state.json":   `{"sessions": []}`,
	}
	for name, content := range configFiles {
		path := filepath.Join(schmuxDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	// Execute: create backup
	err := createDevConfigBackup(schmuxDir)
	if err != nil {
		t.Fatalf("createDevConfigBackup failed: %v", err)
	}

	// Verify: backup file exists in backups directory
	backupDir := filepath.Join(schmuxDir, "backups")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("failed to read backup dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 backup file, got %d", len(entries))
	}

	backupFile := entries[0]
	if !strings.HasPrefix(backupFile.Name(), "config-") {
		t.Errorf("expected backup filename to start with 'config-', got %s", backupFile.Name())
	}
	if !strings.HasSuffix(backupFile.Name(), ".tar.gz") {
		t.Errorf("expected backup filename to end with '.tar.gz', got %s", backupFile.Name())
	}

	// Verify: backup contains all three files
	backupPath := filepath.Join(backupDir, backupFile.Name())
	containedFiles, err := listTarGzFiles(backupPath)
	if err != nil {
		t.Fatalf("failed to list tar.gz contents: %v", err)
	}

	expectedFiles := []string{"config.json", "secrets.json", "state.json"}
	for _, expected := range expectedFiles {
		found := false
		for _, f := range containedFiles {
			if f == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected backup to contain %s, but it was not found. Contents: %v", expected, containedFiles)
		}
	}
}

func TestCreateDevConfigBackupSkipsMissingFiles(t *testing.T) {
	// Setup: create temp schmuxDir with only config.json (no secrets.json or state.json)
	tmpDir := t.TempDir()
	schmuxDir := filepath.Join(tmpDir, ".schmux")
	schmuxdir.Set(schmuxDir)
	defer schmuxdir.Set("")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		t.Fatalf("failed to create schmux dir: %v", err)
	}

	// Create only config.json
	configPath := filepath.Join(schmuxDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"repos": []}`), 0644); err != nil {
		t.Fatalf("failed to write config.json: %v", err)
	}

	// Execute: create backup
	err := createDevConfigBackup(schmuxDir)
	if err != nil {
		t.Fatalf("createDevConfigBackup failed: %v", err)
	}

	// Verify: backup file exists
	backupDir := filepath.Join(schmuxDir, "backups")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("failed to read backup dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 backup file, got %d", len(entries))
	}

	// Verify: backup contains only config.json
	backupPath := filepath.Join(backupDir, entries[0].Name())
	containedFiles, err := listTarGzFiles(backupPath)
	if err != nil {
		t.Fatalf("failed to list tar.gz contents: %v", err)
	}

	if len(containedFiles) != 1 {
		t.Errorf("expected backup to contain 1 file, got %d: %v", len(containedFiles), containedFiles)
	}
	if len(containedFiles) > 0 && containedFiles[0] != "config.json" {
		t.Errorf("expected backup to contain only config.json, got %v", containedFiles)
	}
}

func TestCleanupOldBackups(t *testing.T) {
	// Setup: create temp backup directory with old and new backups
	tmpDir := t.TempDir()
	backupDir := filepath.Join(tmpDir, "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatalf("failed to create backup dir: %v", err)
	}

	// Create old backup (5 days old)
	oldBackup := filepath.Join(backupDir, "config-2026-02-19T12-00-00_test.tar.gz")
	if err := os.WriteFile(oldBackup, []byte("old backup"), 0644); err != nil {
		t.Fatalf("failed to create old backup: %v", err)
	}
	// Set modification time to 5 days ago
	oldTime := time.Now().Add(-5 * 24 * time.Hour)
	if err := os.Chtimes(oldBackup, oldTime, oldTime); err != nil {
		t.Fatalf("failed to set old backup time: %v", err)
	}

	// Create new backup (1 day old - should be kept)
	newBackup := filepath.Join(backupDir, "config-2026-02-23T12-00-00_test.tar.gz")
	if err := os.WriteFile(newBackup, []byte("new backup"), 0644); err != nil {
		t.Fatalf("failed to create new backup: %v", err)
	}
	newTime := time.Now().Add(-1 * 24 * time.Hour)
	if err := os.Chtimes(newBackup, newTime, newTime); err != nil {
		t.Fatalf("failed to set new backup time: %v", err)
	}

	// Create non-config file (should be ignored)
	otherFile := filepath.Join(backupDir, "other.txt")
	if err := os.WriteFile(otherFile, []byte("other"), 0644); err != nil {
		t.Fatalf("failed to create other file: %v", err)
	}

	// Execute: cleanup with 3-day threshold
	cleanupOldBackups(backupDir, 3*24*time.Hour)

	// Verify: old backup deleted, new backup kept, other file kept
	if _, err := os.Stat(oldBackup); !os.IsNotExist(err) {
		t.Error("expected old backup to be deleted")
	}
	if _, err := os.Stat(newBackup); os.IsNotExist(err) {
		t.Error("expected new backup to be kept")
	}
	if _, err := os.Stat(otherFile); os.IsNotExist(err) {
		t.Error("expected other file to be kept (not a config backup)")
	}
}

// listTarGzFiles reads a tar.gz file and returns the list of file names it contains.
func listTarGzFiles(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gzReader, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	var files []string
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeReg {
			files = append(files, header.Name)
		}
	}
	return files, nil
}
