package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
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

func TestHandleCommitGenerate_RejectsNonGitWorkspace(t *testing.T) {
	server, _, st := newTestServer(t)

	ws := state.Workspace{
		ID:     "ws-sapling",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   t.TempDir(),
		VCS:    "sapling",
	}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"workspace_id": "ws-sapling"})
	req := httptest.NewRequest(http.MethodPost, "/api/commit/generate", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	server.handleCommitGenerate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "not available") {
		t.Errorf("expected response to contain 'not available', got: %s", rr.Body.String())
	}
}
