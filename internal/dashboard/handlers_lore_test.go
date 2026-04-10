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

func TestHandleLorePendingMerge_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	statePath := filepath.Join(tmpDir, "state.json")
	st := state.New(statePath, nil)
	logger := log.NewWithOptions(io.Discard, log.Options{})
	wm := workspace.New(cfg, st, statePath, logger)
	sm := session.New(cfg, st, statePath, wm, nil, logger)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
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
	sm := session.New(cfg, st, statePath, wm, nil, logger)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
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
	sm := session.New(cfg, st, statePath, wm, nil, logger)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
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
	sm := session.New(cfg, st, statePath, wm, nil, logger)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
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

func TestValidateLoreRepo(t *testing.T) {
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := validateLoreRepo(dummy)

	tests := []struct {
		name     string
		repo     string
		wantCode int
	}{
		{"valid repo name", "myrepo", http.StatusOK},
		{"empty repo name", "", http.StatusBadRequest},
		{"repo with slash", "my/repo", http.StatusBadRequest},
		{"repo with backslash", "my\\repo", http.StatusBadRequest},
		{"repo with dot", "my.repo", http.StatusBadRequest},
		{"repo with null byte", "my\x00repo", http.StatusBadRequest},
		{"repo exceeding 128 chars", strings.Repeat("a", 129), http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/lore/test/status", nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("repo", tc.repo)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != tc.wantCode {
				t.Errorf("repo=%q: expected %d, got %d", tc.repo, tc.wantCode, rr.Code)
			}
		})
	}
}

func TestHandleLoreStatus(t *testing.T) {
	t.Run("default config no executor", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		cfg := config.CreateDefault(configPath)
		statePath := filepath.Join(tmpDir, "state.json")
		st := state.New(statePath, nil)
		logger := log.NewWithOptions(io.Discard, log.Options{})
		wm := workspace.New(cfg, st, statePath, logger)
		sm := session.New(cfg, st, statePath, wm, nil, logger)
		shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
		defer shutdownCancel()
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
		server.SetModelManager(models.New(cfg, nil, "", logger))
		defer server.CloseForTest()

		req := httptest.NewRequest(http.MethodGet, "/api/lore/status", nil)
		rr := httptest.NewRecorder()
		server.handleLoreStatus(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["enabled"] != true {
			t.Errorf("expected enabled=true, got %v", resp["enabled"])
		}
		if resp["curator_configured"] != false {
			t.Errorf("expected curator_configured=false, got %v", resp["curator_configured"])
		}
		issues, ok := resp["issues"].([]interface{})
		if !ok || len(issues) == 0 {
			t.Errorf("expected non-empty issues, got %v", resp["issues"])
		} else {
			found := false
			for _, issue := range issues {
				if s, ok := issue.(string); ok && strings.Contains(s, "No LLM target") {
					found = true
				}
			}
			if !found {
				t.Errorf("expected 'No LLM target' issue, got %v", issues)
			}
		}
	})

	t.Run("with executor set", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		cfg := config.CreateDefault(configPath)
		statePath := filepath.Join(tmpDir, "state.json")
		st := state.New(statePath, nil)
		logger := log.NewWithOptions(io.Discard, log.Options{})
		wm := workspace.New(cfg, st, statePath, logger)
		sm := session.New(cfg, st, statePath, wm, nil, logger)
		shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
		defer shutdownCancel()
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
		server.SetModelManager(models.New(cfg, nil, "", logger))
		server.SetLoreExecutor(func(ctx context.Context, prompt string, timeout time.Duration) (string, error) {
			return "", nil
		})
		defer server.CloseForTest()

		req := httptest.NewRequest(http.MethodGet, "/api/lore/status", nil)
		rr := httptest.NewRecorder()
		server.handleLoreStatus(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["curator_configured"] != true {
			t.Errorf("expected curator_configured=true, got %v", resp["curator_configured"])
		}
		issues, ok := resp["issues"].([]interface{})
		if ok && len(issues) > 0 {
			t.Errorf("expected no issues with executor set, got %v", issues)
		}
	})
}

func TestHandleLoreProposals(t *testing.T) {
	t.Run("no lore store", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		cfg := config.CreateDefault(configPath)
		statePath := filepath.Join(tmpDir, "state.json")
		st := state.New(statePath, nil)
		logger := log.NewWithOptions(io.Discard, log.Options{})
		wm := workspace.New(cfg, st, statePath, logger)
		sm := session.New(cfg, st, statePath, wm, nil, logger)
		shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
		defer shutdownCancel()
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
		server.SetModelManager(models.New(cfg, nil, "", logger))
		defer server.CloseForTest()

		req := httptest.NewRequest(http.MethodGet, "/api/lore/testrepo/proposals", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("repo", "testrepo")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		server.handleLoreProposals(rr, req)

		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", rr.Code)
		}
	})

	t.Run("empty repo no proposals", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		cfg := config.CreateDefault(configPath)
		statePath := filepath.Join(tmpDir, "state.json")
		st := state.New(statePath, nil)
		logger := log.NewWithOptions(io.Discard, log.Options{})
		wm := workspace.New(cfg, st, statePath, logger)
		sm := session.New(cfg, st, statePath, wm, nil, logger)
		shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
		defer shutdownCancel()
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
		server.SetModelManager(models.New(cfg, nil, "", logger))
		defer server.CloseForTest()

		loreDir := filepath.Join(tmpDir, "lore-proposals")
		proposalStore := lore.NewProposalStore(loreDir, logger)
		server.SetLoreStore(proposalStore)

		req := httptest.NewRequest(http.MethodGet, "/api/lore/testrepo/proposals", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("repo", "testrepo")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		server.handleLoreProposals(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		// When no proposals exist, the handler returns null or an empty array.
		if proposals, ok := resp["proposals"].([]interface{}); ok && len(proposals) != 0 {
			t.Errorf("expected empty/null proposals, got %v", resp["proposals"])
		}
	})

	t.Run("with proposals", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		cfg := config.CreateDefault(configPath)
		statePath := filepath.Join(tmpDir, "state.json")
		st := state.New(statePath, nil)
		logger := log.NewWithOptions(io.Discard, log.Options{})
		wm := workspace.New(cfg, st, statePath, logger)
		sm := session.New(cfg, st, statePath, wm, nil, logger)
		shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
		defer shutdownCancel()
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
		server.SetModelManager(models.New(cfg, nil, "", logger))
		defer server.CloseForTest()

		loreDir := filepath.Join(tmpDir, "lore-proposals")
		proposalStore := lore.NewProposalStore(loreDir, logger)
		server.SetLoreStore(proposalStore)

		proposalStore.Save(&lore.Proposal{
			ID: "prop-001", Repo: "testrepo", Status: lore.ProposalPending,
			Rules: []lore.Rule{{ID: "r1", Text: "test rule", Status: lore.RulePending}},
		})

		req := httptest.NewRequest(http.MethodGet, "/api/lore/testrepo/proposals", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("repo", "testrepo")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		server.handleLoreProposals(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		proposals, ok := resp["proposals"].([]interface{})
		if !ok || len(proposals) != 1 {
			t.Errorf("expected 1 proposal, got %v", resp["proposals"])
		}
	})
}

func TestHandleLoreProposalGet(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		cfg := config.CreateDefault(configPath)
		statePath := filepath.Join(tmpDir, "state.json")
		st := state.New(statePath, nil)
		logger := log.NewWithOptions(io.Discard, log.Options{})
		wm := workspace.New(cfg, st, statePath, logger)
		sm := session.New(cfg, st, statePath, wm, nil, logger)
		shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
		defer shutdownCancel()
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
		server.SetModelManager(models.New(cfg, nil, "", logger))
		defer server.CloseForTest()

		loreDir := filepath.Join(tmpDir, "lore-proposals")
		proposalStore := lore.NewProposalStore(loreDir, logger)
		server.SetLoreStore(proposalStore)

		req := httptest.NewRequest(http.MethodGet, "/api/lore/testrepo/proposals/prop-nonexistent", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("repo", "testrepo")
		rctx.URLParams.Add("proposalID", "prop-nonexistent")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		server.handleLoreProposalGet(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("valid proposal", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		cfg := config.CreateDefault(configPath)
		statePath := filepath.Join(tmpDir, "state.json")
		st := state.New(statePath, nil)
		logger := log.NewWithOptions(io.Discard, log.Options{})
		wm := workspace.New(cfg, st, statePath, logger)
		sm := session.New(cfg, st, statePath, wm, nil, logger)
		shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
		defer shutdownCancel()
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
		server.SetModelManager(models.New(cfg, nil, "", logger))
		defer server.CloseForTest()

		loreDir := filepath.Join(tmpDir, "lore-proposals")
		proposalStore := lore.NewProposalStore(loreDir, logger)
		server.SetLoreStore(proposalStore)

		proposalStore.Save(&lore.Proposal{
			ID: "prop-001", Repo: "testrepo", Status: lore.ProposalPending,
			Rules: []lore.Rule{{ID: "r1", Text: "test rule", Status: lore.RulePending}},
		})

		req := httptest.NewRequest(http.MethodGet, "/api/lore/testrepo/proposals/prop-001", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("repo", "testrepo")
		rctx.URLParams.Add("proposalID", "prop-001")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		server.handleLoreProposalGet(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["id"] != "prop-001" {
			t.Errorf("expected id=prop-001, got %v", resp["id"])
		}
		if resp["repo"] != "testrepo" {
			t.Errorf("expected repo=testrepo, got %v", resp["repo"])
		}
	})
}

func TestHandleLoreRuleUpdate(t *testing.T) {
	setupServer := func(t *testing.T) (*Server, *lore.ProposalStore) {
		t.Helper()
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		cfg := config.CreateDefault(configPath)
		statePath := filepath.Join(tmpDir, "state.json")
		st := state.New(statePath, nil)
		logger := log.NewWithOptions(io.Discard, log.Options{})
		wm := workspace.New(cfg, st, statePath, logger)
		sm := session.New(cfg, st, statePath, wm, nil, logger)
		shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
		t.Cleanup(shutdownCancel)
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
		server.SetModelManager(models.New(cfg, nil, "", logger))
		t.Cleanup(func() { server.CloseForTest() })

		loreDir := filepath.Join(tmpDir, "lore-proposals")
		proposalStore := lore.NewProposalStore(loreDir, logger)
		server.SetLoreStore(proposalStore)

		proposalStore.Save(&lore.Proposal{
			ID: "prop-001", Repo: "testrepo", Status: lore.ProposalPending,
			Rules: []lore.Rule{
				{ID: "r1", Text: "original rule text", Status: lore.RulePending, SuggestedLayer: lore.LayerRepoPublic},
			},
		})

		return server, proposalStore
	}

	makeReq := func(body string) *http.Request {
		req := httptest.NewRequest(http.MethodPatch, "/api/lore/testrepo/proposals/prop-001/rules/r1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("repo", "testrepo")
		rctx.URLParams.Add("proposalID", "prop-001")
		rctx.URLParams.Add("ruleID", "r1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		return req
	}

	t.Run("approve a rule", func(t *testing.T) {
		server, proposalStore := setupServer(t)
		rr := httptest.NewRecorder()
		server.handleLoreRuleUpdate(rr, makeReq(`{"status":"approved"}`))

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		proposal, _ := proposalStore.Get("testrepo", "prop-001")
		if proposal.Rules[0].Status != lore.RuleApproved {
			t.Errorf("expected rule status=approved, got %s", proposal.Rules[0].Status)
		}
	})

	t.Run("dismiss a rule", func(t *testing.T) {
		server, proposalStore := setupServer(t)
		rr := httptest.NewRecorder()
		server.handleLoreRuleUpdate(rr, makeReq(`{"status":"dismissed"}`))

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		proposal, _ := proposalStore.Get("testrepo", "prop-001")
		if proposal.Rules[0].Status != lore.RuleDismissed {
			t.Errorf("expected rule status=dismissed, got %s", proposal.Rules[0].Status)
		}
	})

	t.Run("edit rule text", func(t *testing.T) {
		server, proposalStore := setupServer(t)
		rr := httptest.NewRecorder()
		server.handleLoreRuleUpdate(rr, makeReq(`{"text":"updated text"}`))

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		proposal, _ := proposalStore.Get("testrepo", "prop-001")
		if proposal.Rules[0].Text != "updated text" {
			t.Errorf("expected rule text='updated text', got %q", proposal.Rules[0].Text)
		}
	})

	t.Run("change layer", func(t *testing.T) {
		server, proposalStore := setupServer(t)
		rr := httptest.NewRecorder()
		server.handleLoreRuleUpdate(rr, makeReq(`{"chosen_layer":"repo_public"}`))

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		proposal, _ := proposalStore.Get("testrepo", "prop-001")
		if proposal.Rules[0].ChosenLayer == nil || *proposal.Rules[0].ChosenLayer != lore.LayerRepoPublic {
			t.Errorf("expected chosen_layer=repo_public, got %v", proposal.Rules[0].ChosenLayer)
		}
	})

	t.Run("invalid status", func(t *testing.T) {
		server, _ := setupServer(t)
		rr := httptest.NewRecorder()
		server.handleLoreRuleUpdate(rr, makeReq(`{"status":"invalid"}`))

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("invalid layer", func(t *testing.T) {
		server, _ := setupServer(t)
		rr := httptest.NewRecorder()
		server.handleLoreRuleUpdate(rr, makeReq(`{"chosen_layer":"bad"}`))

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("missing path params", func(t *testing.T) {
		server, _ := setupServer(t)
		req := httptest.NewRequest(http.MethodPatch, "/api/lore/testrepo/proposals/prop-001/rules/r1", strings.NewReader(`{"status":"approved"}`))
		req.Header.Set("Content-Type", "application/json")
		rctx := chi.NewRouteContext()
		// Intentionally omit all params
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		server.handleLoreRuleUpdate(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("no lore store", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		cfg := config.CreateDefault(configPath)
		statePath := filepath.Join(tmpDir, "state.json")
		st := state.New(statePath, nil)
		logger := log.NewWithOptions(io.Discard, log.Options{})
		wm := workspace.New(cfg, st, statePath, logger)
		sm := session.New(cfg, st, statePath, wm, nil, logger)
		shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
		defer shutdownCancel()
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
		server.SetModelManager(models.New(cfg, nil, "", logger))
		defer server.CloseForTest()

		rr := httptest.NewRecorder()
		server.handleLoreRuleUpdate(rr, makeReq(`{"status":"approved"}`))

		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("nonexistent rule", func(t *testing.T) {
		server, _ := setupServer(t)
		req := httptest.NewRequest(http.MethodPatch, "/api/lore/testrepo/proposals/prop-001/rules/r-nonexistent", strings.NewReader(`{"status":"approved"}`))
		req.Header.Set("Content-Type", "application/json")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("repo", "testrepo")
		rctx.URLParams.Add("proposalID", "prop-001")
		rctx.URLParams.Add("ruleID", "r-nonexistent")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		server.handleLoreRuleUpdate(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}

func TestHandleLoreDismiss(t *testing.T) {
	makeReq := func(repo, proposalID string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/lore/"+repo+"/proposals/"+proposalID+"/dismiss", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("repo", repo)
		rctx.URLParams.Add("proposalID", proposalID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		return req
	}

	t.Run("no lore store", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		cfg := config.CreateDefault(configPath)
		statePath := filepath.Join(tmpDir, "state.json")
		st := state.New(statePath, nil)
		logger := log.NewWithOptions(io.Discard, log.Options{})
		wm := workspace.New(cfg, st, statePath, logger)
		sm := session.New(cfg, st, statePath, wm, nil, logger)
		shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
		defer shutdownCancel()
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
		server.SetModelManager(models.New(cfg, nil, "", logger))
		defer server.CloseForTest()

		rr := httptest.NewRecorder()
		server.handleLoreDismiss(rr, makeReq("testrepo", "prop-001"))

		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", rr.Code)
		}
	})

	t.Run("nonexistent proposal", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		cfg := config.CreateDefault(configPath)
		statePath := filepath.Join(tmpDir, "state.json")
		st := state.New(statePath, nil)
		logger := log.NewWithOptions(io.Discard, log.Options{})
		wm := workspace.New(cfg, st, statePath, logger)
		sm := session.New(cfg, st, statePath, wm, nil, logger)
		shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
		defer shutdownCancel()
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
		server.SetModelManager(models.New(cfg, nil, "", logger))
		defer server.CloseForTest()

		loreDir := filepath.Join(tmpDir, "lore-proposals")
		proposalStore := lore.NewProposalStore(loreDir, logger)
		server.SetLoreStore(proposalStore)

		rr := httptest.NewRecorder()
		server.handleLoreDismiss(rr, makeReq("testrepo", "prop-nonexistent"))

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("already applied", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		cfg := config.CreateDefault(configPath)
		statePath := filepath.Join(tmpDir, "state.json")
		st := state.New(statePath, nil)
		logger := log.NewWithOptions(io.Discard, log.Options{})
		wm := workspace.New(cfg, st, statePath, logger)
		sm := session.New(cfg, st, statePath, wm, nil, logger)
		shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
		defer shutdownCancel()
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
		server.SetModelManager(models.New(cfg, nil, "", logger))
		defer server.CloseForTest()

		loreDir := filepath.Join(tmpDir, "lore-proposals")
		proposalStore := lore.NewProposalStore(loreDir, logger)
		server.SetLoreStore(proposalStore)

		proposalStore.Save(&lore.Proposal{
			ID: "prop-001", Repo: "testrepo", Status: lore.ProposalApplied,
			Rules: []lore.Rule{{ID: "r1", Text: "applied rule", Status: lore.RuleApproved}},
		})

		rr := httptest.NewRecorder()
		server.handleLoreDismiss(rr, makeReq("testrepo", "prop-001"))

		if rr.Code != http.StatusConflict {
			t.Errorf("expected 409, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("successful dismiss", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		cfg := config.CreateDefault(configPath)
		statePath := filepath.Join(tmpDir, "state.json")
		st := state.New(statePath, nil)
		logger := log.NewWithOptions(io.Discard, log.Options{})
		wm := workspace.New(cfg, st, statePath, logger)
		sm := session.New(cfg, st, statePath, wm, nil, logger)
		shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
		defer shutdownCancel()
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
		server.SetModelManager(models.New(cfg, nil, "", logger))
		defer server.CloseForTest()

		loreDir := filepath.Join(tmpDir, "lore-proposals")
		proposalStore := lore.NewProposalStore(loreDir, logger)
		server.SetLoreStore(proposalStore)

		proposalStore.Save(&lore.Proposal{
			ID: "prop-001", Repo: "testrepo", Status: lore.ProposalPending,
			Rules: []lore.Rule{{ID: "r1", Text: "pending rule", Status: lore.RulePending}},
		})

		rr := httptest.NewRecorder()
		server.handleLoreDismiss(rr, makeReq("testrepo", "prop-001"))

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]string
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["status"] != "dismissed" {
			t.Errorf("expected status=dismissed, got %q", resp["status"])
		}

		proposal, err := proposalStore.Get("testrepo", "prop-001")
		if err != nil {
			t.Fatalf("failed to reload proposal: %v", err)
		}
		if proposal.Status != lore.ProposalDismissed {
			t.Errorf("expected proposal status=dismissed, got %s", proposal.Status)
		}
	})
}
