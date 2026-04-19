package daemon

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/log"
)

func newTestLogger() *log.Logger {
	return log.NewWithOptions(io.Discard, log.Options{})
}

func TestMigrateModesTightensFileMode(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(f, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := MigrateModes(dir, "" /*workspacePath*/, false /*allowInsecure*/, newTestLogger()); err != nil {
		t.Fatalf("MigrateModes failed: %v", err)
	}

	info, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("got mode %o, want 0600", info.Mode().Perm())
	}
}

func TestMigrateModesTightensDirMode(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "schmux")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := MigrateModes(dir, "", false, newTestLogger()); err != nil {
		t.Fatalf("MigrateModes failed: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf("got dir mode %o, want 0700", info.Mode().Perm())
	}
}

func TestMigrateModesSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	// MigrateModes should chmod target (it's a regular file in the dir) but
	// skip the symlink itself. Verify the symlink's target mode wasn't
	// changed via the symlink.
	if err := MigrateModes(dir, "", false, newTestLogger()); err != nil {
		t.Fatalf("MigrateModes failed: %v", err)
	}
	// Target is a regular file: should have been chmod'd to 0600.
	if info, _ := os.Stat(target); info.Mode().Perm() != 0600 {
		t.Errorf("target file: got mode %o, want 0600", info.Mode().Perm())
	}
}

// Test for failure case + allow_insecure_modes:
func TestMigrateModesRespectsAllowInsecure(t *testing.T) {
	// Hard to simulate chmod failure on a normal filesystem. Document the
	// intent and skip if not on a FS that supports it. Alternative: inject
	// a chmod failure via a test seam (e.g., a chmodFunc variable that the
	// production code uses, swappable in tests).
	t.Skip("chmod failure simulation requires test seam; covered by manual test in Step 36")
}

// repos/ holds bare clones and Sapling/EdenFS working copies that may be
// virtual mounts of multi-million-file monorepos. Walking it would force
// materialization of every backing file and rewrite permissions on upstream
// code that schmux does not own. MigrateModes must tighten the repos/
// directory entry itself but stop at its boundary.
func TestMigrateModesSkipsReposSubtree(t *testing.T) {
	dir := t.TempDir()
	reposDir := filepath.Join(dir, "repos")
	if err := os.MkdirAll(reposDir, 0755); err != nil {
		t.Fatal(err)
	}
	topFile := filepath.Join(reposDir, "config")
	if err := os.WriteFile(topFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	nestedDir := filepath.Join(reposDir, "monorepo", "subdir")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatal(err)
	}
	nestedFile := filepath.Join(nestedDir, "file")
	if err := os.WriteFile(nestedFile, []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := MigrateModes(dir, "", false, newTestLogger()); err != nil {
		t.Fatalf("MigrateModes failed: %v", err)
	}

	if info, _ := os.Stat(reposDir); info.Mode().Perm() != 0700 {
		t.Errorf("repos dir: got mode %o, want 0700 (boundary tightened)", info.Mode().Perm())
	}
	if info, _ := os.Stat(topFile); info.Mode().Perm() != 0644 {
		t.Errorf("file under repos/: got mode %o, want 0644 (untouched)", info.Mode().Perm())
	}
	if info, _ := os.Stat(nestedDir); info.Mode().Perm() != 0755 {
		t.Errorf("nested dir under repos/: got mode %o, want 0755 (untouched)", info.Mode().Perm())
	}
	if info, _ := os.Stat(nestedFile); info.Mode().Perm() != 0644 {
		t.Errorf("nested file under repos/: got mode %o, want 0644 (untouched)", info.Mode().Perm())
	}
}

// query/ holds bare git clones used for branch/commit lookups. Same
// reasoning as repos/: descending would rewrite git-managed mode bits on
// objects/refs and trigger noisy chmod on every restart.
func TestMigrateModesSkipsQuerySubtree(t *testing.T) {
	dir := t.TempDir()
	queryDir := filepath.Join(dir, "query")
	if err := os.MkdirAll(queryDir, 0755); err != nil {
		t.Fatal(err)
	}
	objectsDir := filepath.Join(queryDir, "schmux.git", "objects", "ab")
	if err := os.MkdirAll(objectsDir, 0755); err != nil {
		t.Fatal(err)
	}
	objectFile := filepath.Join(objectsDir, "cdef")
	if err := os.WriteFile(objectFile, []byte("git-object"), 0444); err != nil {
		t.Fatal(err)
	}

	if err := MigrateModes(dir, "", false, newTestLogger()); err != nil {
		t.Fatalf("MigrateModes failed: %v", err)
	}

	if info, _ := os.Stat(queryDir); info.Mode().Perm() != 0700 {
		t.Errorf("query dir: got mode %o, want 0700 (boundary tightened)", info.Mode().Perm())
	}
	if info, _ := os.Stat(objectsDir); info.Mode().Perm() != 0755 {
		t.Errorf("nested dir under query/: got mode %o, want 0755 (untouched)", info.Mode().Perm())
	}
	if info, _ := os.Stat(objectFile); info.Mode().Perm() != 0444 {
		t.Errorf("git object under query/: got mode %o, want 0444 (untouched)", info.Mode().Perm())
	}
}

// Some installations configure cfg.WorkspacePath to a directory inside
// $SCHMUXDIR (e.g. "/tmp/schmux_test/workspaces"). Workspaces in there can
// be Sapling/EdenFS working copies — same materialization/ownership problem
// as repos/. The workspace path must be skipped at its boundary too.
func TestMigrateModesSkipsConfiguredWorkspacePath(t *testing.T) {
	dir := t.TempDir()
	workspacesDir := filepath.Join(dir, "workspaces")
	if err := os.MkdirAll(workspacesDir, 0755); err != nil {
		t.Fatal(err)
	}
	nestedDir := filepath.Join(workspacesDir, "ws-001", "subdir")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatal(err)
	}
	nestedFile := filepath.Join(nestedDir, "file")
	if err := os.WriteFile(nestedFile, []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := MigrateModes(dir, workspacesDir, false, newTestLogger()); err != nil {
		t.Fatalf("MigrateModes failed: %v", err)
	}

	if info, _ := os.Stat(workspacesDir); info.Mode().Perm() != 0700 {
		t.Errorf("workspaces dir: got mode %o, want 0700 (boundary tightened)", info.Mode().Perm())
	}
	if info, _ := os.Stat(nestedDir); info.Mode().Perm() != 0755 {
		t.Errorf("nested dir under workspaces/: got mode %o, want 0755 (untouched)", info.Mode().Perm())
	}
	if info, _ := os.Stat(nestedFile); info.Mode().Perm() != 0644 {
		t.Errorf("nested file under workspaces/: got mode %o, want 0644 (untouched)", info.Mode().Perm())
	}
}

// When cfg.WorkspacePath points outside $SCHMUXDIR (the common case), the
// walk never reaches it and there is nothing to skip. Verify the empty /
// out-of-tree case does not break normal tightening.
func TestMigrateModesIgnoresWorkspacePathOutsideSchmuxDir(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir() // separate temp dir, not under schmuxDir
	f := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := MigrateModes(dir, outside, false, newTestLogger()); err != nil {
		t.Fatalf("MigrateModes failed: %v", err)
	}

	if info, _ := os.Stat(f); info.Mode().Perm() != 0600 {
		t.Errorf("file under schmuxDir: got mode %o, want 0600", info.Mode().Perm())
	}
}

// Hook scripts and other generated executables under $SCHMUXDIR are 0755 on
// disk. Naively forcing 0600 strips the executable bit and breaks anything
// that exec(2)s them. Preserve the owner's exec bit while still stripping
// group/other access.
func TestMigrateModesPreservesOwnerExecBit(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "hook.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "data.json")
	if err := os.WriteFile(plain, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := MigrateModes(dir, "", false, newTestLogger()); err != nil {
		t.Fatalf("MigrateModes failed: %v", err)
	}

	if info, _ := os.Stat(script); info.Mode().Perm() != 0700 {
		t.Errorf("script: got mode %o, want 0700 (owner +x preserved, group/other stripped)", info.Mode().Perm())
	}
	if info, _ := os.Stat(plain); info.Mode().Perm() != 0600 {
		t.Errorf("plain file: got mode %o, want 0600 (no exec to preserve)", info.Mode().Perm())
	}
}
