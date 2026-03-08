package lore

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initBareRepo creates a bare repo with a CLAUDE.md file for testing.
func initBareRepo(t *testing.T) (bareDir string) {
	t.Helper()
	dir := t.TempDir()

	// Create a normal repo first
	normalDir := filepath.Join(dir, "normal")
	os.MkdirAll(normalDir, 0755)
	run(t, normalDir, "git", "init")
	run(t, normalDir, "git", "config", "user.email", "test@test.com")
	run(t, normalDir, "git", "config", "user.name", "test")
	os.WriteFile(filepath.Join(normalDir, "CLAUDE.md"), []byte("# Project\n"), 0644)
	run(t, normalDir, "git", "add", "CLAUDE.md")
	run(t, normalDir, "git", "commit", "-m", "initial")

	// Clone as bare
	bareDir = filepath.Join(dir, "bare.git")
	run(t, dir, "git", "clone", "--bare", normalDir, bareDir)
	return bareDir
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v: %s", name, args, err, string(output))
	}
}

func TestApplyToPrivateLayer(t *testing.T) {
	dir := t.TempDir()
	store := NewInstructionStore(dir)

	err := ApplyToLayer(store, LayerRepoPrivate, "myrepo", "# Private Rules\n- Don't use internal tool X")
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	content, _ := store.Read(LayerRepoPrivate, "myrepo")
	if !strings.Contains(content, "internal tool X") {
		t.Error("private layer should contain applied content")
	}
}

func TestApplyToGlobalLayer(t *testing.T) {
	dir := t.TempDir()
	store := NewInstructionStore(dir)

	err := ApplyToLayer(store, LayerCrossRepoPrivate, "", "# Global\n- Prefer table-driven tests")
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	content, _ := store.Read(LayerCrossRepoPrivate, "")
	if !strings.Contains(content, "table-driven") {
		t.Error("global layer should contain applied content")
	}
}

func TestApplyToLayer_RejectsPublic(t *testing.T) {
	dir := t.TempDir()
	store := NewInstructionStore(dir)

	err := ApplyToLayer(store, LayerRepoPublic, "myrepo", "content")
	if err == nil {
		t.Error("expected error for repo_public layer")
	}
}
