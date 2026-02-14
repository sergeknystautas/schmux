package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sergeknystautas/schmux/internal/lore"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// handleLoreRouter dispatches lore API requests based on the URL path.
// Routes:
//   - GET  /api/lore/{repo}/proposals          — list proposals
//   - GET  /api/lore/{repo}/proposals/{id}     — get single proposal
//   - POST /api/lore/{repo}/proposals/{id}/apply   — apply a proposal
//   - POST /api/lore/{repo}/proposals/{id}/dismiss — dismiss a proposal
//   - GET  /api/lore/{repo}/entries            — list lore entries
//   - POST /api/lore/{repo}/curate             — trigger manual curation
func (s *Server) handleLoreRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/lore/")
	parts := strings.Split(path, "/")

	switch {
	case len(parts) >= 4 && parts[1] == "proposals" && parts[3] == "apply":
		s.handleLoreApply(w, r)
	case len(parts) >= 4 && parts[1] == "proposals" && parts[3] == "dismiss":
		s.handleLoreDismiss(w, r)
	case len(parts) >= 3 && parts[1] == "proposals":
		s.handleLoreProposalGet(w, r)
	case len(parts) >= 2 && parts[1] == "proposals":
		s.handleLoreProposals(w, r)
	case len(parts) >= 2 && parts[1] == "entries":
		s.handleLoreEntries(w, r)
	case len(parts) >= 2 && parts[1] == "curate":
		s.handleLoreCurate(w, r)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// handleLoreProposals lists all proposals for a repo.
func (s *Server) handleLoreProposals(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/lore/"), "/")
	if len(parts) < 2 || parts[0] == "" {
		http.Error(w, "missing repo name", http.StatusBadRequest)
		return
	}
	repoName := parts[0]

	if s.loreStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	proposals, err := s.loreStore.List(repoName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"proposals": proposals,
	})
}

// handleLoreProposalGet returns a single proposal by ID.
func (s *Server) handleLoreProposalGet(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/lore/"), "/")
	if len(parts) < 3 {
		http.Error(w, "missing repo name or proposal id", http.StatusBadRequest)
		return
	}
	repoName, proposalID := parts[0], parts[2]

	if s.loreStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	proposal, err := s.loreStore.Get(repoName, proposalID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(proposal)
}

// handleLoreApply applies a proposal: creates a worktree, commits changes, pushes the branch,
// and optionally creates a PR when auto_pr is enabled.
func (s *Server) handleLoreApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/lore/"), "/")
	if len(parts) < 4 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	repoName, proposalID := parts[0], parts[2]

	if s.loreStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	proposal, err := s.loreStore.Get(repoName, proposalID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Check for overrides in request body
	var body struct {
		Overrides map[string]string `json:"overrides"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&body)
		for k, v := range body.Overrides {
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
		body := proposal.DiffSummary
		if body == "" {
			body = fmt.Sprintf("Automated lore update — %d file(s) changed.", len(proposal.ProposedFiles))
		}
		url, err := lore.CreatePR(r.Context(), bareDir, result.Branch, title, body)
		if err != nil {
			// Log but don't fail — the commit and push already succeeded
			fmt.Fprintf(os.Stderr, "schmux: auto-PR creation failed (branch %s pushed): %v\n", result.Branch, err)
		} else {
			prURL = url
		}
	}

	// Update proposal status
	s.loreStore.UpdateStatus(repoName, proposalID, lore.ProposalApplied)

	// Mark source entries as "applied" in the lore JSONL
	overlayDir, err := workspace.OverlayDir(repoName)
	if err == nil {
		lorePath := filepath.Join(overlayDir, ".claude", "lore.jsonl")
		if err := lore.MarkEntriesByText(lorePath, "applied", proposal.EntriesUsed, proposalID); err != nil {
			fmt.Printf("[lore] warning: failed to mark entries as applied: %v\n", err)
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
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/lore/"), "/")
	if len(parts) < 4 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	repoName, proposalID := parts[0], parts[2]

	if s.loreStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	// Load the proposal first to get EntriesUsed for marking
	proposal, err := s.loreStore.Get(repoName, proposalID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := s.loreStore.UpdateStatus(repoName, proposalID, lore.ProposalDismissed); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Mark source entries as "dismissed" in the lore JSONL
	overlayDir, err := workspace.OverlayDir(repoName)
	if err == nil {
		lorePath := filepath.Join(overlayDir, ".claude", "lore.jsonl")
		if err := lore.MarkEntriesByText(lorePath, "dismissed", proposal.EntriesUsed, proposalID); err != nil {
			fmt.Printf("[lore] warning: failed to mark entries as dismissed: %v\n", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "dismissed"})
}

// handleLoreEntries returns the lore JSONL entries for a repo from its overlay directory.
// Supports query parameters: state, agent, type, limit.
func (s *Server) handleLoreEntries(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/lore/"), "/")
	if len(parts) < 2 {
		http.Error(w, "missing repo name", http.StatusBadRequest)
		return
	}
	repoName := parts[0]

	overlayDir, err := workspace.OverlayDir(repoName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	lorePath := filepath.Join(overlayDir, ".claude", "lore.jsonl")

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

	entries, err := lore.ReadEntries(lorePath, filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": entries,
	})
}

// handleLoreCurate handles manual curation requests.
func (s *Server) handleLoreCurate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/lore/"), "/")
	if len(parts) < 2 || parts[0] == "" {
		http.Error(w, "missing repo name", http.StatusBadRequest)
		return
	}
	repoName := parts[0]

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

	overlayDir, err := workspace.OverlayDir(repoName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	lorePath := filepath.Join(overlayDir, ".claude", "lore.jsonl")

	proposal, err := s.loreCurator.Curate(r.Context(), repoName, bareDir, lorePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("curation failed: %v", err), http.StatusInternalServerError)
		return
	}
	if proposal == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "no_raw_entries"})
		return
	}

	if err := s.loreStore.Save(proposal); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Mark source entries as "proposed" in the lore JSONL
	if err := lore.MarkEntriesByText(lorePath, "proposed", proposal.EntriesUsed, proposal.ID); err != nil {
		fmt.Printf("[lore] warning: failed to mark entries as proposed: %v\n", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "curated",
		"proposal_id": proposal.ID,
	})
}
