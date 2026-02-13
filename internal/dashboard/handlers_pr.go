package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	gh "github.com/sergeknystautas/schmux/internal/github"
)

// handlePRs handles GET /api/prs - returns cached PRs.
func (s *Server) handlePRs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	prs, lastFetched, lastErr := s.prDiscovery.GetPRs()
	if prs == nil {
		prs = []contracts.PullRequest{}
	}

	resp := contracts.PRsResponse{
		PullRequests:  prs,
		LastFetchedAt: lastFetched,
		Error:         lastErr,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handlePRRefresh handles POST /api/prs/refresh - re-runs PR discovery.
func (s *Server) handlePRRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	prs, retryAfter, err := s.prDiscovery.Refresh(s.config.GetRepos())
	if err != nil {
		cached, _, _ := s.prDiscovery.GetPRs()
		if cached == nil {
			cached = []contracts.PullRequest{}
		}
		resp := contracts.PRRefreshResponse{
			PullRequests:  cached,
			FetchedCount:  len(cached),
			RetryAfterSec: retryAfter,
			Error:         err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}
	if prs == nil {
		prs = []contracts.PullRequest{}
	}

	resp := contracts.PRRefreshResponse{
		PullRequests:  prs,
		FetchedCount:  len(prs),
		RetryAfterSec: retryAfter,
	}

	// Persist to state
	s.state.SetPullRequests(prs)
	s.state.SetPublicRepos(s.prDiscovery.GetPublicRepos())
	s.state.Save()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handlePRCheckout handles POST /api/prs/checkout - creates workspace from PR, launches session.
func (s *Server) handlePRCheckout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req contracts.PRCheckoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}
	if req.RepoURL == "" || req.PRNumber <= 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "repo_url and pr_number are required"})
		return
	}

	// Look up PR from discovery cache
	pr, found := s.prDiscovery.FindPR(req.RepoURL, req.PRNumber)
	if !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("PR #%d not found for %s", req.PRNumber, req.RepoURL)})
		return
	}

	// Determine target for session (explicit config required)
	target := s.config.GetPrReviewTarget()
	if target == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "No pr_review target configured"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	// Create workspace from PR ref
	ws, err := s.workspace.CheckoutPR(ctx, pr)
	if err != nil {
		fmt.Printf("[pr] checkout failed: %v\n", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to checkout PR: %v", err)})
		return
	}

	// Build review prompt with workspace context
	prompt := gh.BuildReviewPrompt(pr, ws.Path, gh.PRBranchName(pr))

	// Launch session
	nickname := fmt.Sprintf("PR #%d: %s", pr.Number, pr.Title)
	sess, err := s.session.Spawn(ctx, pr.RepoURL, gh.PRBranchName(pr), target, prompt, nickname, ws.ID, false, "")
	if err != nil {
		fmt.Printf("[pr] session launch failed: %v\n", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Workspace created but session launch failed: %v", err)})
		return
	}

	// Broadcast workspace update
	go s.BroadcastSessions()

	resp := contracts.PRCheckoutResponse{
		WorkspaceID: ws.ID,
		SessionID:   sess.ID,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
