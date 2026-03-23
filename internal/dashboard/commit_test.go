package dashboard

import (
	"strings"
	"testing"
)

func TestBuildOneshotCommitPrompt(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new"
	got := BuildOneshotCommitPrompt(diff)
	if got == "" {
		t.Fatal("BuildOneshotCommitPrompt returned empty string")
	}
	// The diff must appear verbatim in the prompt
	if !strings.Contains(got, diff) {
		t.Error("prompt should contain the full diff verbatim")
	}
	// Must include the commit message instruction from CommitPrompt()
	if !strings.Contains(got, "commit message") {
		t.Error("prompt should contain commit message instruction")
	}
	// Oneshot mode requires no preamble instruction
	if !strings.Contains(got, "no preamble") {
		t.Error("prompt should instruct no preamble for oneshot mode")
	}
	// Verify the prompt has the base prompt prepended (not just the diff)
	basePrompt := CommitPrompt()
	if !strings.HasPrefix(got, basePrompt) {
		t.Error("oneshot prompt should start with the base CommitPrompt()")
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

// Note: TestHandleCommitGenerate_RejectsNonGitWorkspace was removed — the commit
// handler is now VCS-agnostic via CommandBuilder and works with any VCS type.
