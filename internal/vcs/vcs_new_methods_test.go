package vcs

import "testing"

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
