package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/lore"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// makeLoreApplyRequest creates an HTTP request with chi route context for the lore apply-merge endpoint.
func makeLoreApplyRequest(t *testing.T, repoName, proposalID string, body []byte) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/lore/"+repoName+"/proposals/"+proposalID+"/apply-merge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("repo", repoName)
	rctx.URLParams.Add("proposalID", proposalID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return req
}

func TestHandleLoreApplyMerge_RepoPublic_WorkspaceBased(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()

	// Create a remote git repo with initial commit
	remoteDir := filepath.Join(tmpDir, "remote")
	os.MkdirAll(remoteDir, 0755)
	runGitHelper(t, remoteDir, "init", "-b", "main")
	runGitHelper(t, remoteDir, "config", "user.email", "test@test.com")
	runGitHelper(t, remoteDir, "config", "user.name", "test")
	os.WriteFile(filepath.Join(remoteDir, "CLAUDE.md"), []byte("# Project\n"), 0644)
	runGitHelper(t, remoteDir, "add", ".")
	runGitHelper(t, remoteDir, "commit", "-m", "initial")

	// Set up server with real workspace manager
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	cfg.Repos = []config.Repo{
		{Name: "testrepo", URL: remoteDir, BarePath: "testrepo-workspace.git"},
	}
	cfg.Save()

	statePath := filepath.Join(tmpDir, "state.json")
	st := state.New(statePath, nil)
	logger := log.NewWithOptions(io.Discard, log.Options{})
	wm := workspace.New(cfg, st, statePath, logger)
	sm := session.New(cfg, st, statePath, wm, logger)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, ServerOptions{
		ShutdownCtx: shutdownCtx,
	})
	server.SetModelManager(models.New(cfg, nil, "", log.NewWithOptions(io.Discard, log.Options{})))

	// Set up lore store with a proposal
	loreDir := filepath.Join(tmpDir, "lore")
	proposalStore := lore.NewProposalStore(loreDir, logger)
	server.SetLoreStore(proposalStore)

	// Create a proposal
	proposal := &lore.Proposal{
		ID:     "prop-test-001",
		Repo:   "testrepo",
		Status: lore.ProposalPending,
		Rules: []lore.Rule{
			{Text: "Always use semicolons", Status: lore.RuleApproved, SuggestedLayer: lore.LayerRepoPublic},
		},
		MergePreviews: []lore.MergePreview{
			{Layer: lore.LayerRepoPublic, MergedContent: "# Project\n\n- Always use semicolons\n"},
		},
	}
	proposalStore.Save(proposal)

	t.Cleanup(server.CloseForTest)
	t.Cleanup(shutdownCancel)
	t.Cleanup(func() {
		for _, sess := range st.GetSessions() {
			sm.Dispose(context.Background(), sess.ID)
		}
	})

	// Apply the merge
	body, _ := json.Marshal(map[string]interface{}{
		"merges": []map[string]string{
			{"layer": "repo_public", "content": "# Project\n\n- Always use semicolons\n"},
		},
	})
	req := makeLoreApplyRequest(t, "testrepo", "prop-test-001", body)
	rr := httptest.NewRecorder()
	server.handleLoreApplyMerge(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Status  string              `json:"status"`
		Results []map[string]string `json:"results"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0]["workspace_id"] == "" {
		t.Error("expected workspace_id in result")
	}
	if resp.Results[0]["status"] != "applied" {
		t.Errorf("expected status=applied, got %q", resp.Results[0]["status"])
	}

	// Verify the file was written as unstaged change
	wsID := resp.Results[0]["workspace_id"]
	ws, found := st.GetWorkspace(wsID)
	if !found {
		t.Fatalf("workspace %s not found in state", wsID)
	}
	content, err := os.ReadFile(filepath.Join(ws.Path, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}
	if string(content) != "# Project\n\n- Always use semicolons\n" {
		t.Errorf("unexpected CLAUDE.md content: %q", string(content))
	}

	// Verify the change is unstaged (git status --porcelain shows modification)
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = ws.Path
	statusOut, err := cmd.Output()
	if err != nil {
		t.Fatalf("git status failed: %v", err)
	}
	if len(statusOut) == 0 {
		t.Error("expected unstaged changes in workspace")
	}
}

func TestHandleLoreApplyMerge_RepoPublic_ConflictWhenDirty(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()

	// Create a remote git repo
	remoteDir := filepath.Join(tmpDir, "remote")
	os.MkdirAll(remoteDir, 0755)
	runGitHelper(t, remoteDir, "init", "-b", "main")
	runGitHelper(t, remoteDir, "config", "user.email", "test@test.com")
	runGitHelper(t, remoteDir, "config", "user.name", "test")
	os.WriteFile(filepath.Join(remoteDir, "CLAUDE.md"), []byte("# Project\n"), 0644)
	runGitHelper(t, remoteDir, "add", ".")
	runGitHelper(t, remoteDir, "commit", "-m", "initial")

	// Set up server
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	cfg.Repos = []config.Repo{
		{Name: "testrepo", URL: remoteDir, BarePath: "testrepo-conflict.git"},
	}
	cfg.Save()

	statePath := filepath.Join(tmpDir, "state.json")
	st := state.New(statePath, nil)
	logger := log.NewWithOptions(io.Discard, log.Options{})
	wm := workspace.New(cfg, st, statePath, logger)
	sm := session.New(cfg, st, statePath, wm, logger)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, ServerOptions{
		ShutdownCtx: shutdownCtx,
	})
	server.SetModelManager(models.New(cfg, nil, "", log.NewWithOptions(io.Discard, log.Options{})))

	loreDir := filepath.Join(tmpDir, "lore")
	proposalStore := lore.NewProposalStore(loreDir, logger)
	server.SetLoreStore(proposalStore)

	proposal := &lore.Proposal{
		ID:     "prop-test-002",
		Repo:   "testrepo",
		Status: lore.ProposalPending,
		Rules: []lore.Rule{
			{Text: "Always use semicolons", Status: lore.RuleApproved, SuggestedLayer: lore.LayerRepoPublic},
		},
	}
	proposalStore.Save(proposal)

	t.Cleanup(server.CloseForTest)
	t.Cleanup(shutdownCancel)

	// Pre-create the schmux/lore workspace and make it dirty
	ws, err := wm.GetOrCreate(context.Background(), remoteDir, "schmux/lore")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}
	// Write a dirty file
	os.WriteFile(filepath.Join(ws.Path, "dirty.txt"), []byte("dirty"), 0644)

	// Try to apply — should get 409 Conflict
	body, _ := json.Marshal(map[string]interface{}{
		"merges": []map[string]string{
			{"layer": "repo_public", "content": "# Project\n\n- New rule\n"},
		},
	})
	req := makeLoreApplyRequest(t, "testrepo", "prop-test-002", body)
	rr := httptest.NewRecorder()
	server.handleLoreApplyMerge(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleLoreApplyMergeAutoCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()

	// Create a remote git repo with initial commit (bare-like: allow push to checked-out branch)
	remoteDir := filepath.Join(tmpDir, "remote")
	os.MkdirAll(remoteDir, 0755)
	runGitHelper(t, remoteDir, "init", "-b", "main")
	runGitHelper(t, remoteDir, "config", "user.email", "test@test.com")
	runGitHelper(t, remoteDir, "config", "user.name", "test")
	runGitHelper(t, remoteDir, "config", "receive.denyCurrentBranch", "ignore")
	runGitHelper(t, remoteDir, "config", "gc.auto", "0")
	runGitHelper(t, remoteDir, "config", "gc.autoDetach", "false")
	os.WriteFile(filepath.Join(remoteDir, "CLAUDE.md"), []byte("# Project\n"), 0644)
	runGitHelper(t, remoteDir, "add", ".")
	runGitHelper(t, remoteDir, "commit", "-m", "initial")

	// Set up server with real workspace manager
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	cfg.Repos = []config.Repo{
		{Name: "testrepo", URL: remoteDir, BarePath: "testrepo-autocommit.git"},
	}
	cfg.Save()

	statePath := filepath.Join(tmpDir, "state.json")
	st := state.New(statePath, nil)
	logger := log.NewWithOptions(io.Discard, log.Options{})
	wm := workspace.New(cfg, st, statePath, logger)
	sm := session.New(cfg, st, statePath, wm, logger)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, ServerOptions{
		ShutdownCtx: shutdownCtx,
	})
	server.SetModelManager(models.New(cfg, nil, "", log.NewWithOptions(io.Discard, log.Options{})))

	// Set up lore store with a proposal containing 2 approved rules
	loreDir := filepath.Join(tmpDir, "lore")
	proposalStore := lore.NewProposalStore(loreDir, logger)
	server.SetLoreStore(proposalStore)

	proposal := &lore.Proposal{
		ID:     "prop-auto-001",
		Repo:   "testrepo",
		Status: lore.ProposalPending,
		Rules: []lore.Rule{
			{Text: "Always use semicolons", Status: lore.RuleApproved, SuggestedLayer: lore.LayerRepoPublic},
			{Text: "Prefer const over let", Status: lore.RuleApproved, SuggestedLayer: lore.LayerRepoPublic},
		},
		MergePreviews: []lore.MergePreview{
			{Layer: lore.LayerRepoPublic, MergedContent: "# Project\n\n- Always use semicolons\n- Prefer const over let\n"},
		},
	}
	proposalStore.Save(proposal)

	t.Cleanup(server.CloseForTest)
	t.Cleanup(shutdownCancel)
	t.Cleanup(func() {
		for _, sess := range st.GetSessions() {
			sm.Dispose(context.Background(), sess.ID)
		}
	})

	// Apply the merge with auto_commit=true
	body, _ := json.Marshal(map[string]interface{}{
		"merges": []map[string]string{
			{"layer": "repo_public", "content": "# Project\n\n- Always use semicolons\n- Prefer const over let\n"},
		},
		"auto_commit": true,
	})
	req := makeLoreApplyRequest(t, "testrepo", "prop-auto-001", body)
	rr := httptest.NewRecorder()
	server.handleLoreApplyMerge(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Status  string              `json:"status"`
		Results []map[string]string `json:"results"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0]["status"] != "applied" {
		t.Errorf("expected status=applied, got %q", resp.Results[0]["status"])
	}
	if resp.Results[0]["commit_sha"] == "" {
		t.Error("expected commit_sha in result when auto_commit=true")
	}

	// Verify the commit was pushed to the remote.
	// NOTE: We verify against the remote repo, not the workspace, because the
	// handler disposes the workspace asynchronously after a successful push.
	wsID := resp.Results[0]["workspace_id"]
	remoteLogCmd := exec.Command("git", "log", "-1", "--format=%H", "main")
	remoteLogCmd.Dir = remoteDir
	remoteLogOut, err := remoteLogCmd.Output()
	if err != nil {
		t.Fatalf("git log on remote failed: %v", err)
	}
	remoteSHA := strings.TrimSpace(string(remoteLogOut))
	if remoteSHA != resp.Results[0]["commit_sha"] {
		t.Errorf("remote HEAD %s does not match commit_sha %s", remoteSHA, resp.Results[0]["commit_sha"])
	}

	// Verify the commit message mentions the rule count (check on remote)
	msgCmd := exec.Command("git", "log", "-1", "--format=%s", "main")
	msgCmd.Dir = remoteDir
	msgOut, err := msgCmd.Output()
	if err != nil {
		t.Fatalf("git log message on remote failed: %v", err)
	}
	commitMsg := strings.TrimSpace(string(msgOut))
	if commitMsg != "lore: add 2 rules from agent learnings" {
		t.Errorf("unexpected commit message: %q", commitMsg)
	}

	// Verify no shell session was spawned (auto_commit skips shell)
	for _, sess := range st.GetSessions() {
		if sess.WorkspaceID == wsID {
			t.Error("expected no shell session when auto_commit=true")
			break
		}
	}
}

func runGitHelper(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
