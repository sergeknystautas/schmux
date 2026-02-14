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

// handleLoreApply applies a proposal: creates a worktree, commits changes, and optionally pushes.
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

	// Push if configured
	if s.config.GetLoreAutoPR() {
		if err := lore.PushBranch(r.Context(), bareDir, result.Branch); err != nil {
			http.Error(w, fmt.Sprintf("commit succeeded but push failed: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Update proposal status
	s.loreStore.UpdateStatus(repoName, proposalID, lore.ProposalApplied)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "applied",
		"branch": result.Branch,
	})
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

	if err := s.loreStore.UpdateStatus(repoName, proposalID, lore.ProposalDismissed); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "dismissed"})
}

// handleLoreEntries returns the lore JSONL entries for a repo from its overlay directory.
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

	entries, err := lore.ReadEntries(lorePath, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": entries,
	})
}

// handleLoreCurate handles manual curation requests (not yet implemented).
func (s *Server) handleLoreCurate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.Error(w, "manual curation not yet implemented", http.StatusNotImplemented)
}
