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
	"time"

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

func TestHandleLorePendingMerge_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	statePath := filepath.Join(tmpDir, "state.json")
	st := state.New(statePath, nil)
	logger := log.NewWithOptions(io.Discard, log.Options{})
	wm := workspace.New(cfg, st, statePath, logger)
	sm := session.New(cfg, st, statePath, wm, logger)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, ServerOptions{ShutdownCtx: shutdownCtx})
	server.SetModelManager(models.New(cfg, nil, "", logger))
	server.SetLorePendingMergeStore(lore.NewPendingMergeStore(t.TempDir(), nil))
	defer server.CloseForTest()

	req := httptest.NewRequest(http.MethodGet, "/api/lore/testrepo/pending-merge", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("repo", "testrepo")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	server.handleLorePendingMergeGet(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestHandleLorePendingMergePatch(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	statePath := filepath.Join(tmpDir, "state.json")
	st := state.New(statePath, nil)
	logger := log.NewWithOptions(io.Discard, log.Options{})
	wm := workspace.New(cfg, st, statePath, logger)
	sm := session.New(cfg, st, statePath, wm, logger)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, ServerOptions{ShutdownCtx: shutdownCtx})
	server.SetModelManager(models.New(cfg, nil, "", logger))
	defer server.CloseForTest()

	pmStore := lore.NewPendingMergeStore(t.TempDir(), nil)
	server.SetLorePendingMergeStore(pmStore)
	pmStore.Save(&lore.PendingMerge{Repo: "testrepo", Status: lore.PendingMergeStatusReady, MergedContent: "original"})

	body, _ := json.Marshal(map[string]string{"edited_content": "user edit"})
	req := httptest.NewRequest(http.MethodPatch, "/api/lore/testrepo/pending-merge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("repo", "testrepo")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	server.handleLorePendingMergePatch(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	pm, _ := pmStore.Get("testrepo")
	if pm.EditedContent == nil || *pm.EditedContent != "user edit" {
		t.Errorf("expected edited_content='user edit', got %v", pm.EditedContent)
	}
}

func TestHandleLorePendingMergeDelete(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	statePath := filepath.Join(tmpDir, "state.json")
	st := state.New(statePath, nil)
	logger := log.NewWithOptions(io.Discard, log.Options{})
	wm := workspace.New(cfg, st, statePath, logger)
	sm := session.New(cfg, st, statePath, wm, logger)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, ServerOptions{ShutdownCtx: shutdownCtx})
	server.SetModelManager(models.New(cfg, nil, "", logger))
	defer server.CloseForTest()

	pmStore := lore.NewPendingMergeStore(t.TempDir(), nil)
	server.SetLorePendingMergeStore(pmStore)
	pmStore.Save(&lore.PendingMerge{Repo: "testrepo", Status: lore.PendingMergeStatusReady})

	req := httptest.NewRequest(http.MethodDelete, "/api/lore/testrepo/pending-merge", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("repo", "testrepo")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	server.handleLorePendingMergeDelete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if _, err := pmStore.Get("testrepo"); err == nil {
		t.Error("expected PendingMerge to be deleted")
	}
}

func TestHandleLorePush_Success(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	tmpDir := t.TempDir()

	// Create a remote git repo (allow push to checked-out branch)
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

	// Get the initial SHA
	shaCmd := exec.Command("git", "-C", remoteDir, "rev-parse", "HEAD")
	shaOut, _ := shaCmd.Output()
	baseSHA := strings.TrimSpace(string(shaOut))

	// Set up server
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	cfg.Repos = []config.Repo{
		{Name: "testrepo", URL: remoteDir, BarePath: "testrepo-push.git"},
	}
	cfg.Save()

	statePath := filepath.Join(tmpDir, "state.json")
	st := state.New(statePath, nil)
	logger := log.NewWithOptions(io.Discard, log.Options{})
	wm := workspace.New(cfg, st, statePath, logger)
	sm := session.New(cfg, st, statePath, wm, logger)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, ServerOptions{ShutdownCtx: shutdownCtx})
	server.SetModelManager(models.New(cfg, nil, "", logger))
	defer server.CloseForTest()

	// Create bare repo (clone remote)
	bareDir := cfg.ResolveBareRepoDir("testrepo-push.git")
	exec.Command("git", "clone", "--bare", remoteDir, bareDir).Run()

	// Set up PendingMergeStore with a ready merge
	pmStore := lore.NewPendingMergeStore(t.TempDir(), nil)
	server.SetLorePendingMergeStore(pmStore)
	pm := &lore.PendingMerge{
		Repo:           "testrepo",
		Status:         lore.PendingMergeStatusReady,
		BaseSHA:        baseSHA,
		RuleIDs:        []string{"r1"},
		ProposalIDs:    []string{"prop-001"},
		MergedContent:  "# Project\n\n- New rule\n",
		CurrentContent: "# Project\n",
		Summary:        "Added new rule",
		CreatedAt:      time.Now().UTC(),
	}
	pmStore.Save(pm)

	// Set up proposal store with matching approved rule
	loreDir := filepath.Join(tmpDir, "lore-proposals")
	proposalStore := lore.NewProposalStore(loreDir, logger)
	server.SetLoreStore(proposalStore)
	proposalStore.Save(&lore.Proposal{
		ID: "prop-001", Repo: "testrepo", Status: lore.ProposalPending,
		Rules: []lore.Rule{{ID: "r1", Text: "New rule", Status: lore.RuleApproved, SuggestedLayer: lore.LayerRepoPublic}},
	})

	// Call push endpoint
	req := httptest.NewRequest(http.MethodPost, "/api/lore/testrepo/push", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("repo", "testrepo")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	server.handleLorePush(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["commit_sha"] == "" {
		t.Error("expected commit_sha in response")
	}

	// Verify PendingMerge was cleaned up
	if _, err := pmStore.Get("testrepo"); err == nil {
		t.Error("expected PendingMerge to be deleted after push")
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
