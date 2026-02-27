package dashboard

import (
	"strings"
	"testing"
)

func TestBuildOneshotCommitPrompt(t *testing.T) {
	diff := "diff --git a/main.go"
	got := BuildOneshotCommitPrompt(diff)
	if got == "" {
		t.Fatal("BuildOneshotCommitPrompt returned empty string")
	}
	if !strings.Contains(got, diff) {
		t.Error("prompt should contain the diff output")
	}
	if !strings.Contains(got, "commit message") {
		t.Error("prompt should mention commit message")
	}
	if !strings.Contains(got, "no preamble") {
		t.Error("prompt should instruct no preamble for oneshot mode")
	}
}

func TestCommitPrompt(t *testing.T) {
	got := CommitPrompt()
	if got == "" {
		t.Fatal("CommitPrompt returned empty string")
	}
	if !strings.Contains(got, "commit message") {
		t.Error("prompt should mention commit message")
	}
}
