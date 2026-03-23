package vcs

import (
	"strings"
	"testing"
)

func TestGitCurrentBranch(t *testing.T) {
	cb := NewCommandBuilder("git")
	cmd := cb.CurrentBranch()
	if cmd == "" {
		t.Error("CurrentBranch() returned empty string")
	}
	if cmd != "git branch --show-current" {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestSaplingCurrentBranch(t *testing.T) {
	cb := NewCommandBuilder("sapling")
	cmd := cb.CurrentBranch()
	if cmd == "" {
		t.Error("CurrentBranch() returned empty string")
	}
}

func TestGitStatusPorcelain(t *testing.T) {
	cb := NewCommandBuilder("git")
	cmd := cb.StatusPorcelain()
	if cmd != "git status --porcelain" {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestSaplingStatusPorcelain(t *testing.T) {
	cb := NewCommandBuilder("sapling")
	cmd := cb.StatusPorcelain()
	if cmd == "" {
		t.Error("StatusPorcelain() returned empty string")
	}
}

func TestGitRemoteBranchExists(t *testing.T) {
	cb := NewCommandBuilder("git")
	cmd := cb.RemoteBranchExists("feature/foo")
	if cmd == "" {
		t.Error("RemoteBranchExists() returned empty string")
	}
}

func TestSaplingRemoteBranchExists(t *testing.T) {
	cb := NewCommandBuilder("sapling")
	cmd := cb.RemoteBranchExists("feature/foo")
	if cmd == "" {
		t.Error("RemoteBranchExists() returned empty string")
	}
}

func TestGitAddFiles(t *testing.T) {
	cb := NewCommandBuilder("git")
	cmd := cb.AddFiles([]string{"file1.go", "file2.go"})
	if cmd == "" {
		t.Error("AddFiles() returned empty string")
	}
	if !strings.Contains(cmd, "git add") {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestSaplingAddFiles(t *testing.T) {
	cb := NewCommandBuilder("sapling")
	cmd := cb.AddFiles([]string{"file1.go"})
	if cmd == "" {
		t.Error("AddFiles() returned empty string")
	}
	if !strings.Contains(cmd, "sl add") {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestGitCommitAmendNoEdit(t *testing.T) {
	cb := NewCommandBuilder("git")
	cmd := cb.CommitAmendNoEdit()
	if cmd != "git commit --amend --no-edit" {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestSaplingCommitAmendNoEdit(t *testing.T) {
	cb := NewCommandBuilder("sapling")
	cmd := cb.CommitAmendNoEdit()
	if cmd != "sl amend" {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestGitUncommit(t *testing.T) {
	cb := NewCommandBuilder("git")
	cmd := cb.Uncommit()
	if cmd != "git reset HEAD~1" {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestSaplingUncommit(t *testing.T) {
	cb := NewCommandBuilder("sapling")
	cmd := cb.Uncommit()
	if cmd != "sl uncommit" {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestGitDiscardFile(t *testing.T) {
	cb := NewCommandBuilder("git")
	cmd := cb.DiscardFile("main.go")
	if cmd == "" {
		t.Error("DiscardFile() returned empty string")
	}
}

func TestSaplingDiscardFile(t *testing.T) {
	cb := NewCommandBuilder("sapling")
	cmd := cb.DiscardFile("main.go")
	if cmd == "" {
		t.Error("DiscardFile() returned empty string")
	}
	if !strings.Contains(cmd, "sl revert") {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestGitDiffUnified(t *testing.T) {
	cb := NewCommandBuilder("git")
	cmd := cb.DiffUnified()
	if cmd != "git diff HEAD" {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestSaplingDiffUnified(t *testing.T) {
	cb := NewCommandBuilder("sapling")
	cmd := cb.DiffUnified()
	if cmd != "sl diff" {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestGitDiscardAllTracked(t *testing.T) {
	cb := NewCommandBuilder("git")
	cmd := cb.DiscardAllTracked()
	if cmd != "git checkout -- ." {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestSaplingDiscardAllTracked(t *testing.T) {
	cb := NewCommandBuilder("sapling")
	cmd := cb.DiscardAllTracked()
	if cmd != "sl revert --all" {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestGitCleanUntrackedFile(t *testing.T) {
	cb := NewCommandBuilder("git")
	cmd := cb.CleanUntrackedFile("tmp.log")
	if !strings.Contains(cmd, "git clean") {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestGitCleanAllUntracked(t *testing.T) {
	cb := NewCommandBuilder("git")
	cmd := cb.CleanAllUntracked()
	if cmd != "git clean -fd" {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestSaplingCleanAllUntracked(t *testing.T) {
	cb := NewCommandBuilder("sapling")
	cmd := cb.CleanAllUntracked()
	if cmd != "sl purge --all" {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestGitUnstageNewFile(t *testing.T) {
	cb := NewCommandBuilder("git")
	cmd := cb.UnstageNewFile("new.go")
	if !strings.Contains(cmd, "git rm") && !strings.Contains(cmd, "--cached") {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestSaplingUnstageNewFile(t *testing.T) {
	cb := NewCommandBuilder("sapling")
	cmd := cb.UnstageNewFile("new.go")
	if !strings.Contains(cmd, "sl forget") {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestGitCheckIgnore(t *testing.T) {
	cb := NewCommandBuilder("git")
	cmd := cb.CheckIgnore("build/output.o")
	if !strings.Contains(cmd, "git check-ignore") {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestSaplingCheckIgnore(t *testing.T) {
	cb := NewCommandBuilder("sapling")
	cmd := cb.CheckIgnore("build/output.o")
	if cmd == "" {
		t.Error("CheckIgnore() returned empty string")
	}
}

func TestGitAddFiles_QuotesSpaces(t *testing.T) {
	cb := NewCommandBuilder("git")
	cmd := cb.AddFiles([]string{"path with spaces/file.go"})
	if !strings.Contains(cmd, "git add") {
		t.Errorf("unexpected command: %s", cmd)
	}
	// Verify the path is quoted (should contain single quotes from shellutil.Quote)
	if !strings.Contains(cmd, "'path with spaces/file.go'") {
		t.Errorf("expected quoted path in command: %s", cmd)
	}
}

func TestGitDiscardFile_QuotesPath(t *testing.T) {
	cb := NewCommandBuilder("git")
	cmd := cb.DiscardFile("dir/file name.go")
	if !strings.Contains(cmd, "'dir/file name.go'") {
		t.Errorf("expected quoted path in command: %s", cmd)
	}
}
