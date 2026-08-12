package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// handleGitHubConnectStatus handles GET /api/workspaces/{id}/github-connect.
// Returns the detected connect state, the step plan, gh availability, and
// prefill values for the dashboard dialog.
func (h *GitHandlers) handleGitHubConnectStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceID")
	ctx := r.Context()

	det, err := h.workspace.DetectGitHubConnect(ctx, workspaceID)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	gh := github.CheckAuth(ctx)
	resp := contracts.GitHubConnectStatus{
		Eligible:         det.Eligible,
		GH:               gh,
		OriginURL:        det.OriginURL,
		RemoteReachable:  det.RemoteReachable,
		RemoteHasRefs:    det.RemoteHasRefs,
		ConfigURLIsLocal: det.ConfigURLIsLocal,
		StateRepoIsLocal: det.StateRepoIsLocal,
		Plan:             workspace.BuildConnectPlan(det),
		Name:             det.RepoName,
		DefaultBranch:    "main",
	}
	if gh.Available {
		if owners, err := github.NewCLI().ListOwners(ctx); err == nil {
			resp.Owners = owners
		} else {
			logging.Sub(h.logger, "workspace").Warn("github-connect: listing owners failed", "err", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", "handler", "github-connect-status", "err", err)
	}
}

// handleGitHubConnect handles POST /api/workspaces/{id}/github-connect.
// Runs the connect pipeline; per-step results in the response body.
func (h *GitHandlers) handleGitHubConnect(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceID")

	var req contracts.GitHubConnectRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(h.config.GetGitCloneTimeoutMs())*time.Millisecond)
	defer cancel()

	// Repo creation needs gh; reject early with a clear message instead of a
	// mid-pipeline failure.
	det, err := h.workspace.DetectGitHubConnect(ctx, workspaceID)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusNotFound)
		return
	}
	if det.Eligible && !det.RemoteReachable && !github.CheckAuth(ctx).Available {
		writeJSONError(w, "creating the GitHub repository requires the gh CLI to be installed and authenticated", http.StatusBadRequest)
		return
	}

	workspaceLog := logging.Sub(h.logger, "workspace")
	workspaceLog.Info("github-connect", "workspace_id", workspaceID, "owner", req.Owner, "name", req.Name, "visibility", req.Visibility)

	result, err := h.workspace.RunGitHubConnect(ctx, workspaceID, req, github.NewCLI())
	switch {
	case errors.Is(err, workspace.ErrNotConnectEligible), errors.Is(err, workspace.ErrConnectMissingTarget):
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, workspace.ErrWorkspaceLocked):
		writeJSONError(w, "workspace is busy with another operation", http.StatusConflict)
		return
	case err != nil:
		workspaceLog.Error("github-connect failed", "workspace_id", workspaceID, "err", err)
		writeJSONError(w, fmt.Sprintf("Failed to connect repository: %v", err), http.StatusInternalServerError)
		return
	}

	if result.Success {
		// State/config changed — refresh all dashboard clients.
		go h.broadcastSessions()
	}

	workspaceLog.Info("github-connect done", "workspace_id", workspaceID, "success", result.Success, "repo_url", result.RepoURL)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		h.logger.Error("failed to encode response", "handler", "github-connect", "err", err)
	}
}
