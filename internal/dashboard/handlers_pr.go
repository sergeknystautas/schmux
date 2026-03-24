//go:build !nogithub

package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	gh "github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/session"
)

// handlePRs handles GET /api/prs - returns cached PRs.
func (s *Server) handlePRs(w http.ResponseWriter, r *http.Request) {
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
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode response", "handler", "prs", "err", err)
	}
}

// handlePRRefresh handles POST /api/prs/refresh - re-runs PR discovery.
func (s *Server) handlePRRefresh(w http.ResponseWriter, r *http.Request) {
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
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			s.logger.Error("failed to encode response", "handler", "pr-refresh", "err", err)
		}
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
	if err := s.state.Save(); err != nil {
		s.logger.Error("failed to save state", "err", err)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode response", "handler", "pr-refresh", "err", err)
	}
}

// handlePRCheckout handles POST /api/prs/checkout - creates workspace from PR, launches session.
func (s *Server) handlePRCheckout(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req contracts.PRCheckoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.RepoURL == "" || req.PRNumber <= 0 {
		writeJSONError(w, "repo_url and pr_number are required", http.StatusBadRequest)
		return
	}

	// Look up PR from discovery cache
	pr, found := s.prDiscovery.FindPR(req.RepoURL, req.PRNumber)
	if !found {
		writeJSONError(w, fmt.Sprintf("PR #%d not found for %s", req.PRNumber, req.RepoURL), http.StatusNotFound)
		return
	}

	// Determine target for session (explicit config required)
	target := s.config.GetPrReviewTarget()
	if target == "" {
		writeJSONError(w, "No pr_review target configured", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	// Create workspace from PR ref
	ws, err := s.workspace.CheckoutPR(ctx, pr)
	if err != nil {
		s.logger.Error("checkout failed", "err", err)
		writeJSONError(w, fmt.Sprintf("Failed to checkout PR: %v", err), http.StatusInternalServerError)
		return
	}

	// Build review prompt with workspace context
	prompt := gh.BuildReviewPrompt(pr, ws.Path, gh.PRBranchName(pr))

	// Launch session
	nickname := fmt.Sprintf("PR #%d: %s", pr.Number, pr.Title)
	sess, err := s.session.Spawn(ctx, session.SpawnOptions{
		RepoURL:     pr.RepoURL,
		Branch:      gh.PRBranchName(pr),
		TargetName:  target,
		Prompt:      prompt,
		Nickname:    nickname,
		WorkspaceID: ws.ID,
	})
	if err != nil {
		s.logger.Error("session launch failed", "err", err)
		writeJSONError(w, fmt.Sprintf("Workspace created but session launch failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Broadcast workspace update
	go s.BroadcastSessions()

	resp := contracts.PRCheckoutResponse{
		WorkspaceID: ws.ID,
		SessionID:   sess.ID,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode response", "handler", "pr-checkout", "err", err)
	}
}

// handleGetGitHubStatus handles GET /api/github/status - returns gh CLI auth status.
func (s *Server) handleGetGitHubStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.githubStatus)
}
