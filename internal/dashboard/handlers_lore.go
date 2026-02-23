package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/lore"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schema"
)

// validateLoreRepo is a chi middleware that validates the {repo} URL parameter.
// Rejects requests with repo names that contain path separators, dots, or null bytes.
func validateLoreRepo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repo := chi.URLParam(r, "repo")
		if repo == "" || strings.ContainsAny(repo, "/\\.\x00") || len(repo) > 128 {
			http.Error(w, "invalid repo name", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleLoreStatus returns the lore system configuration status.
func (s *Server) handleLoreStatus(w http.ResponseWriter, r *http.Request) {
	enabled := s.config.GetLoreEnabled()
	curateOnDispose := s.config.GetLoreCurateOnDispose()
	llmTarget := s.config.GetLoreTarget()
	curatorConfigured := s.loreCurator != nil && s.loreCurator.Executor != nil

	var issues []string
	if enabled && !curatorConfigured {
		issues = append(issues, "No LLM target configured \u2014 curator cannot run. Set lore.llm_target in config.")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled":            enabled,
		"curator_configured": curatorConfigured,
		"curate_on_dispose":  curateOnDispose,
		"llm_target":         llmTarget,
		"issues":             issues,
	})
}

// getLoreWorkspacePaths returns the .schmux/lore.jsonl paths for all workspaces
// belonging to the given repo name.
func (s *Server) getLoreWorkspacePaths(repoName string) []string {
	// Find the repo URL by name
	repo, found := s.config.FindRepo(repoName)
	if !found {
		return nil
	}

	var paths []string
	for _, ws := range s.state.GetWorkspaces() {
		if ws.Repo == repo.URL {
			paths = append(paths, filepath.Join(ws.Path, ".schmux", "lore.jsonl"))
		}
	}
	return paths
}

// getLoreReadPaths returns all paths to read lore from: workspace JSONL files + central state file.
func (s *Server) getLoreReadPaths(repoName string) []string {
	paths := s.getLoreWorkspacePaths(repoName)
	statePath, err := lore.LoreStatePath(repoName)
	if err == nil {
		paths = append(paths, statePath)
	}
	return paths
}

// handleLoreProposals lists all proposals for a repo.
func (s *Server) handleLoreProposals(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	if repoName == "" {
		http.Error(w, "missing repo name", http.StatusBadRequest)
		return
	}

	if s.loreStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	proposals, err := s.loreStore.List(repoName)
	if err != nil {
		s.logger.Error("list proposals error", "err", err)
		http.Error(w, "failed to list proposals", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"proposals": proposals,
	})
}

// handleLoreProposalGet returns a single proposal by ID.
func (s *Server) handleLoreProposalGet(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	proposalID := chi.URLParam(r, "proposalID")
	if repoName == "" || proposalID == "" {
		http.Error(w, "missing repo name or proposal id", http.StatusBadRequest)
		return
	}

	if s.loreStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	proposal, err := s.loreStore.Get(repoName, proposalID)
	if err != nil {
		http.Error(w, "proposal not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(proposal)
}

// handleLoreApply applies a proposal: creates a worktree, commits changes, pushes the branch,
// and optionally creates a PR when auto_pr is enabled.
func (s *Server) handleLoreApply(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB limit
	repoName := chi.URLParam(r, "repo")
	proposalID := chi.URLParam(r, "proposalID")
	if repoName == "" || proposalID == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	if s.loreStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	proposal, err := s.loreStore.Get(repoName, proposalID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Check that proposal is still pending
	if proposal.Status != "" && proposal.Status != "pending" {
		http.Error(w, fmt.Sprintf("proposal is %s, not pending", proposal.Status), http.StatusConflict)
		return
	}

	// Check for overrides in request body (body may be empty for apply-without-overrides)
	var body struct {
		Overrides map[string]string `json:"overrides"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		for k, v := range body.Overrides {
			if _, exists := proposal.ProposedFiles[k]; !exists {
				http.Error(w, fmt.Sprintf("override key %q not in original proposal", k), http.StatusBadRequest)
				return
			}
			proposal.ProposedFiles[k] = v
		}
	}

	// Find the repo config by name
	var barePath string
	found := false
	for _, repoConfig := range s.config.Repos {
		if repoConfig.Name == repoName {
			barePath = repoConfig.BarePath
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	bareDir := filepath.Join(s.config.GetWorktreeBasePath(), barePath)
	workDir := filepath.Join(os.TempDir(), "schmux-lore-apply")
	os.MkdirAll(workDir, 0755)

	result, err := lore.ApplyProposal(r.Context(), proposal, bareDir, workDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Always push the branch after a successful commit
	if err := lore.PushBranch(r.Context(), bareDir, result.Branch); err != nil {
		http.Error(w, fmt.Sprintf("commit succeeded but push failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Optionally create a PR when auto_pr is enabled
	var prURL string
	if s.config.GetLoreAutoPR() {
		title := "chore: update instruction files with agent lore"
		prBody := proposal.DiffSummary
		if prBody == "" {
			prBody = fmt.Sprintf("Automated lore update — %d file(s) changed.", len(proposal.ProposedFiles))
		}
		url, err := lore.CreatePR(r.Context(), bareDir, result.Branch, title, prBody)
		if err != nil {
			// Log but don't fail — the commit and push already succeeded
			s.logger.Error("auto-PR creation failed", "branch", result.Branch, "err", err)
		} else {
			prURL = url
		}
	}

	// Update proposal status
	if err := s.loreStore.UpdateStatus(repoName, proposalID, lore.ProposalApplied); err != nil {
		s.logger.Error("failed to update proposal status", "err", err)
	}

	// Mark source entries as "applied" in the central state JSONL
	statePath, err := lore.LoreStatePath(repoName)
	if err == nil {
		wsPaths := s.getLoreWorkspacePaths(repoName)
		if err := lore.MarkEntriesByTextMulti(wsPaths, statePath, "applied", proposal.EntriesUsed, proposalID); err != nil {
			s.logger.Warn("failed to mark entries as applied", "err", err)
		}
	}

	resp := map[string]interface{}{
		"status": "applied",
		"branch": result.Branch,
	}
	if prURL != "" {
		resp["pr_url"] = prURL
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleLoreDismiss marks a proposal as dismissed.
func (s *Server) handleLoreDismiss(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	proposalID := chi.URLParam(r, "proposalID")
	if repoName == "" || proposalID == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	if s.loreStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	// Load the proposal first to get EntriesUsed for marking
	proposal, err := s.loreStore.Get(repoName, proposalID)
	if err != nil {
		http.Error(w, "proposal not found", http.StatusNotFound)
		return
	}

	if proposal.Status == "applied" {
		http.Error(w, "proposal is already applied", http.StatusConflict)
		return
	}

	if err := s.loreStore.UpdateStatus(repoName, proposalID, lore.ProposalDismissed); err != nil {
		s.logger.Error("update proposal status error", "err", err)
		http.Error(w, "failed to update proposal status", http.StatusInternalServerError)
		return
	}

	// Mark source entries as "dismissed" in the central state JSONL
	statePath, err := lore.LoreStatePath(repoName)
	if err == nil {
		wsPaths := s.getLoreWorkspacePaths(repoName)
		if err := lore.MarkEntriesByTextMulti(wsPaths, statePath, "dismissed", proposal.EntriesUsed, proposalID); err != nil {
			s.logger.Warn("failed to mark entries as dismissed", "err", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "dismissed"})
}

// handleLoreEntries returns the lore JSONL entries for a repo, aggregated from all workspace directories
// and the central state file. Supports query parameters: state, agent, type, limit.
func (s *Server) handleLoreEntries(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	if repoName == "" {
		http.Error(w, "missing repo name", http.StatusBadRequest)
		return
	}

	readPaths := s.getLoreReadPaths(repoName)

	// Parse query params for filtering
	q := r.URL.Query()
	state := q.Get("state")
	agent := q.Get("agent")
	entryType := q.Get("type")
	var limit int
	if limitStr := q.Get("limit"); limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
	}

	var filter lore.EntryFilter
	if state != "" || agent != "" || entryType != "" || limit > 0 {
		filter = lore.FilterByParams(state, agent, entryType, limit)
	}

	entries, err := lore.ReadEntriesMulti(readPaths, filter)
	if err != nil {
		s.logger.Error("read entries error", "err", err)
		http.Error(w, "failed to read lore entries", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": entries,
	})
}

// handleLoreCurate handles manual curation requests.
func (s *Server) handleLoreCurate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB limit
	repoName := chi.URLParam(r, "repo")
	if repoName == "" {
		http.Error(w, "missing repo name", http.StatusBadRequest)
		return
	}

	if s.loreCurator == nil || s.loreCurator.Executor == nil {
		http.Error(w, "lore curator not configured (no LLM target)", http.StatusServiceUnavailable)
		return
	}
	if s.loreStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	// Find the repo config by name
	var barePath string
	found := false
	for _, repoConfig := range s.config.Repos {
		if repoConfig.Name == repoName {
			barePath = repoConfig.BarePath
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	bareDir := filepath.Join(s.config.GetWorktreeBasePath(), barePath)

	// Read raw entries from all workspace directories + central state
	readPaths := s.getLoreReadPaths(repoName)
	rawEntries, err := lore.ReadEntriesMulti(readPaths, lore.FilterRaw())
	if err != nil {
		s.logger.Error("read entries error", "err", err)
		http.Error(w, "failed to read lore entries", http.StatusInternalServerError)
		return
	}

	if len(rawEntries) == 0 {
		s.logger.Info("curate: no raw entries to process", "repo", repoName)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "no_raw_entries"})
		return
	}

	s.logger.Info("curate: found raw entries, calling LLM", "repo", repoName, "count", len(rawEntries))
	start := time.Now()

	// Use a detached context with its own timeout — the LLM call can take
	// 30-120s and we don't want it cancelled if the browser disconnects.
	curateCtx, curateCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer curateCancel()

	// Use CurateWithEntries so we pass the pre-aggregated entries
	proposal, err := s.loreCurator.CurateWithEntries(curateCtx, repoName, bareDir, rawEntries)
	elapsed := time.Since(start)
	if err != nil {
		s.logger.Error("curation failed", "elapsed", elapsed.Round(time.Millisecond), "err", err)
		http.Error(w, "curation failed", http.StatusInternalServerError)
		return
	}
	if proposal == nil {
		s.logger.Info("curate: LLM returned no proposal", "repo", repoName, "elapsed", elapsed.Round(time.Millisecond))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "no_raw_entries"})
		return
	}

	s.logger.Info("curate: proposal created", "repo", repoName, "proposal", proposal.ID, "files", len(proposal.ProposedFiles), "entries_used", len(proposal.EntriesUsed), "elapsed", elapsed.Round(time.Millisecond))

	if err := s.loreStore.Save(proposal); err != nil {
		s.logger.Error("save proposal error", "err", err)
		http.Error(w, "failed to save proposal", http.StatusInternalServerError)
		return
	}

	// Mark source entries as "proposed" in the central state JSONL
	statePath, err := lore.LoreStatePath(repoName)
	if err == nil {
		wsPaths := s.getLoreWorkspacePaths(repoName)
		if err := lore.MarkEntriesByTextMulti(wsPaths, statePath, "proposed", proposal.EntriesUsed, proposal.ID); err != nil {
			s.logger.Warn("failed to mark entries as proposed", "err", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "curated",
		"proposal_id": proposal.ID,
	})
}

// refreshLoreCurator updates the lore curator's executor based on the current
// config. Called after config save so the runtime curator stays in sync with
// the persisted lore.llm_target value.
func (s *Server) refreshLoreCurator(cfg *config.Config) {
	target := cfg.GetLoreTargetRaw()

	if s.loreCurator == nil {
		// Lore was disabled at startup — create a curator now
		s.loreCurator = &lore.Curator{
			InstructionFiles: cfg.GetLoreInstructionFiles(),
			BareRepo:         true,
		}
	}

	if target != "" {
		s.loreCurator.Executor = func(ctx context.Context, prompt string, timeout time.Duration) (string, error) {
			return oneshot.ExecuteTarget(ctx, cfg, target, prompt, schema.LabelLoreCurator, timeout, "")
		}
	} else {
		s.loreCurator.Executor = nil
	}
}
