package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// linearSyncResponse is the error/status response used by both sync-from-main and sync-to-main handlers.
type linearSyncResponse struct {
	Success              bool   `json:"success"`
	Message              string `json:"message"`
	InProgress           bool   `json:"in_progress,omitempty"`
	IsPreCommitHookError bool   `json:"is_pre_commit_hook_error"`
	PreCommitErrorDetail string `json:"pre_commit_error_detail,omitempty"`
	Hash                 string `json:"hash,omitempty"`
	ActualHash           string `json:"actual_hash,omitempty"`
}

// handleLinearSyncFromMain handles POST requests to sync commits from origin/main into branch.
// POST /api/workspaces/{id}/linear-sync-from-main
//
// This performs an iterative rebase that brings commits FROM main INTO the current branch
// one at a time, preserving local changes. Supports diverged branches.
func (s *Server) handleLinearSyncFromMain(w http.ResponseWriter, r *http.Request) {
	// Extract workspace ID from chi URL param
	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	var req struct {
		Hash string `json:"hash"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req.Hash = strings.TrimSpace(req.Hash)
	workspaceLog := logging.Sub(s.logger, "workspace")
	if req.Hash == "" {
		workspaceLog.Warn("linear-sync-from-main validation error: hash missing", "workspace_id", workspaceID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, linearSyncResponse{
			Success: false,
			Message: "hash is required",
		})
		return
	}

	// Get workspace from state
	_, found := s.state.GetWorkspace(workspaceID)
	if !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, linearSyncResponse{
			Success: false,
			Message: fmt.Sprintf("workspace %s not found", workspaceID),
		})
		return
	}

	workspaceLog.Info("linear-sync-from-main", "workspace_id", workspaceID)

	// Validate required hash precondition before running sync.
	graph, err := s.workspace.GetGitGraph(r.Context(), workspaceID, 1, 1)
	if err != nil {
		workspaceLog.Error("linear-sync-from-main validation error: get-graph failed", "workspace_id", workspaceID, "err", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, linearSyncResponse{
			Success: false,
			Message: fmt.Sprintf("failed to validate hash precondition: %v", err),
		})
		return
	}
	actualHash := strings.TrimSpace(graph.MainAheadNextHash)
	if actualHash == "" {
		workspaceLog.Warn("linear-sync-from-main hash mismatch: no next hash", "workspace_id", workspaceID, "requested", req.Hash)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, linearSyncResponse{
			Success:    false,
			Message:    "hash mismatch: no next hash is available",
			Hash:       req.Hash,
			ActualHash: actualHash,
		})
		return
	}
	if actualHash != req.Hash {
		workspaceLog.Warn("linear-sync-from-main hash mismatch", "workspace_id", workspaceID, "requested", req.Hash, "actual", actualHash)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, linearSyncResponse{
			Success:    false,
			Message:    fmt.Sprintf("hash mismatch: requested %s but next is %s", req.Hash, actualHash),
			Hash:       req.Hash,
			ActualHash: actualHash,
		})
		return
	}

	// Pause Vite file watching during rebase to prevent transform errors
	// from transient conflict markers in source files.
	if s.workspace.IsWorkspaceLocked(workspaceID) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, linearSyncResponse{
			Success: false,
			Message: "workspace is locked by another sync operation",
		})
		return
	}

	go s.runLinearSyncFromMain(workspaceID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(linearSyncResponse{
		Success:    true,
		Message:    "sync started",
		InProgress: true,
	}); err != nil {
		s.logger.Error("failed to encode response", "handler", "linear-sync", "err", err)
	}
}

func (s *Server) runLinearSyncFromMain(workspaceID string) {
	s.pauseViteWatch()
	defer s.resumeViteWatch()

	workspaceLog := logging.Sub(s.logger, "workspace")

	// Perform the sync from main
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitCloneTimeoutMs())*time.Millisecond)
	defer cancel()

	result, err := s.workspace.LinearSyncFromDefault(ctx, workspaceID)
	if err != nil {
		workspaceLog.Error("linear-sync-from-main failed", "workspace_id", workspaceID, "err", err)
		s.BroadcastWorkspaceUnlockedWithSyncResult(workspaceID, nil, err)
		return
	}

	// Update git status after sync (best-effort, don't block response)
	if _, err := s.workspace.UpdateVCSStatus(ctx, workspaceID); err != nil {
		if !errors.Is(err, workspace.ErrWorkspaceLocked) {
			workspaceLog.Warn("linear-sync-from-main: failed to update git status", "err", err)
		}
	}

	// Update ConflictOnBranch based on result
	if result.ConflictingHash != "" {
		// Re-fetch workspace to avoid overwriting concurrent changes
		ws, found := s.state.GetWorkspace(workspaceID)
		if !found {
			return
		}
		// Conflict detected - set ConflictOnBranch to current branch
		branch := ws.Branch
		ws.ConflictOnBranch = &branch
		if err := s.state.UpdateWorkspace(ws); err != nil {
			workspaceLog.Warn("linear-sync-from-main: failed to update conflict state", "err", err)
		} else {
			if err := s.state.Save(); err != nil {
				workspaceLog.Warn("linear-sync-from-main: failed to save state", "err", err)
			}
			go s.BroadcastSessions()
		}
	}

	successMsg := "success"
	if result.Success {
		successMsg = fmt.Sprintf("success: synced %d commits from %s", result.SuccessCount, result.Branch)
	} else if result.ConflictingHash != "" {
		successMsg = fmt.Sprintf("conflict at %s after %d commits", result.ConflictingHash, result.SuccessCount)
	}
	workspaceLog.Info("linear-sync-from-main", "workspace_id", workspaceID, "result", successMsg)
	s.BroadcastWorkspaceUnlockedWithSyncResult(workspaceID, result, nil)
	go s.BroadcastSessions()
}

// handleLinearSyncToMain handles POST requests to sync commits from branch to origin/main.
// POST /api/workspaces/{id}/linear-sync-to-main
//
// This pushes the current branch's commits directly to main without a merge commit.
func (s *Server) handleLinearSyncToMain(w http.ResponseWriter, r *http.Request) {
	// Extract workspace ID from chi URL param
	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	// Get workspace from state
	_, found := s.state.GetWorkspace(workspaceID)
	if !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, linearSyncResponse{
			Success: false,
			Message: fmt.Sprintf("workspace %s not found", workspaceID),
		})
		return
	}

	workspaceLog := logging.Sub(s.logger, "workspace")
	workspaceLog.Info("linear-sync-to-main", "workspace_id", workspaceID)

	// Perform the sync to main
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitCloneTimeoutMs())*time.Millisecond)
	defer cancel()

	result, err := s.workspace.LinearSyncToDefault(ctx, workspaceID)
	if err != nil {
		workspaceLog.Error("linear-sync-to-main failed", "workspace_id", workspaceID, "err", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, linearSyncResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to sync to main: %v", err),
		})
		return
	}

	// Update git status after sync (best-effort, don't block response)
	if _, err := s.workspace.UpdateVCSStatus(ctx, workspaceID); err != nil {
		if !errors.Is(err, workspace.ErrWorkspaceLocked) {
			workspaceLog.Warn("linear-sync-to-main: failed to update git status", "err", err)
		}
	}

	successMsg := "success"
	if result.Success {
		successMsg = fmt.Sprintf("success: pushed %d commits to %s", result.SuccessCount, result.Branch)
	} else {
		successMsg = "failed"
	}
	workspaceLog.Info("linear-sync-to-main", "workspace_id", workspaceID, "result", successMsg)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		s.logger.Error("failed to encode response", "handler", "linear-sync-to-main", "err", err)
	}
}

// handlePushToBranch handles POST requests to push commits to origin/branch.
// POST /api/workspaces/{id}/push-to-branch
//
// Request body: {"confirm": true|false}
// If branches have diverged and confirm=false, returns needs_confirm=true with divergent commits.
func (s *Server) handlePushToBranch(w http.ResponseWriter, r *http.Request) {
	// Extract workspace ID from chi URL param
	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	// Parse request body
	var req struct {
		Confirm bool `json:"confirm"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get workspace from state
	ws, found := s.state.GetWorkspace(workspaceID)
	if !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("workspace %s not found", workspaceID),
		})
		return
	}

	workspaceLog := logging.Sub(s.logger, "workspace")
	workspaceLog.Info("push-to-branch", "workspace_id", workspaceID, "confirm", req.Confirm)

	// Perform the push to branch
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitCloneTimeoutMs())*time.Millisecond)
	defer cancel()

	result, err := s.workspace.PushToBranch(ctx, workspaceID, req.Confirm)
	if err != nil {
		workspaceLog.Error("push-to-branch failed", "workspace_id", workspaceID, "err", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("Failed to push to branch: %v", err),
		})
		return
	}

	// Update git status after push (best effort)
	if result.Success {
		if _, err := s.workspace.UpdateVCSStatus(ctx, workspaceID); err != nil {
			if !errors.Is(err, workspace.ErrWorkspaceLocked) {
				workspaceLog.Warn("push-to-branch: failed to update git status", "err", err)
			}
		}
	}

	successMsg := "success"
	if result.Success {
		successMsg = fmt.Sprintf("success: pushed to origin/%s", ws.Branch)
	} else if result.NeedsConfirm {
		successMsg = "needs confirmation"
	} else {
		successMsg = "failed"
	}
	workspaceLog.Info("push-to-branch", "workspace_id", workspaceID, "result", successMsg)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		s.logger.Error("failed to encode response", "handler", "push-to-branch", "err", err)
	}
}

// handleLinearSyncResolveConflict handles POST requests to kick off conflict resolution.
// Returns 202 immediately; progress is streamed via the /ws/dashboard WebSocket.
// POST /api/workspaces/{id}/linear-sync-resolve-conflict
func (s *Server) handleLinearSyncResolveConflict(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	// 404 if workspace not found
	if _, found := s.state.GetWorkspace(workspaceID); !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]interface{}{
			"started": false, "message": fmt.Sprintf("workspace %s not found", workspaceID),
		})
		return
	}

	// 409 if already in progress (auto-clear completed/failed states)
	existing := s.getLinearSyncResolveConflictState(workspaceID)
	if existing != nil {
		if existing.Status == "in_progress" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]interface{}{
				"started": false, "message": "operation already in progress",
			})
			return
		}
		// Auto-clear completed/failed state and its tab
		s.deleteLinearSyncResolveConflictState(workspaceID)
		s.removeResolveConflictTab(workspaceID)
	}

	// Create the server-managed tab for this conflict resolution session.
	tabID := uuid.NewString()
	crTab := state.Tab{
		ID:        tabID,
		Kind:      "resolve-conflict",
		Label:     "Conflict Resolution",
		Route:     fmt.Sprintf("/resolve-conflict/%s/%s", workspaceID, tabID),
		Closable:  false,
		Meta:      map[string]string{"status": "in_progress"},
		CreatedAt: time.Now(),
	}
	if err := s.state.AddTab(workspaceID, crTab); err != nil {
		logging.Sub(s.logger, "workspace").Warn("linear-sync-resolve-conflict: failed to add tab", "err", err)
	}

	// Create state and insert before launching goroutine
	crState := &LinearSyncResolveConflictState{
		Type:        "linear_sync_resolve_conflict",
		WorkspaceID: workspaceID,
		Status:      "in_progress",
		StartedAt:   time.Now().Format(time.RFC3339),
		Steps:       []LinearSyncResolveConflictStep{},
	}
	s.setLinearSyncResolveConflictState(workspaceID, crState)
	go s.BroadcastSessions()

	workspaceLog := logging.Sub(s.logger, "workspace")
	workspaceLog.Info("linear-sync-resolve-conflict started", "workspace_id", workspaceID)

	// Launch background goroutine
	go func() {
		// Pause Vite file watching during conflict resolution to prevent
		// transform errors from transient conflict markers in source files.
		s.pauseViteWatch()
		defer s.resumeViteWatch()

		// Panic recovery — never leave state stuck at in_progress
		defer func() {
			if r := recover(); r != nil {
				logging.Sub(s.logger, "workspace").Error("linear-sync-resolve-conflict PANIC", "err", r)
				s.cleanupCRTrackers(crState)
				crState.Finish("failed", fmt.Sprintf("Internal error: %v", r), nil)
				s.finalizeResolveConflictTab(workspaceID, tabID, "", "failed")
				go s.BroadcastSessions()
			}
		}()

		// Wire the step callback to state mutations + broadcasts
		onStep := func(step workspace.ResolveConflictStep) {
			// Handle llm_session steps: create/destroy ephemeral tracker
			if step.Action == "llm_session" && step.TmuxSession != "" {
				if step.Status == "in_progress" {
					crState.SetTmuxSession(step.TmuxSession)
					tracker := session.NewSessionTracker(
						step.TmuxSession,
						step.TmuxSession,
						nil, // no state store
						"",  // no event file
						nil, // no event handlers
						nil, // no output callback
						nil, // no logger
					)
					tracker.Start()
					s.setCRTracker(step.TmuxSession, tracker)
				} else {
					crState.ClearTmuxSession()
					if tracker := s.getCRTracker(step.TmuxSession); tracker != nil {
						tracker.Stop()
						s.deleteCRTracker(step.TmuxSession)
					}
				}
			}

			if step.Hash != "" {
				crState.SetHash(step.Hash, step.HashMessage)
				// Update tab label to include the short hash for user context
				shortHash := step.Hash
				if len(shortHash) > 7 {
					shortHash = shortHash[:7]
				}
				s.updateResolveConflictTab(workspaceID, tabID, "Conflict "+shortHash, step.Hash, "in_progress")
			}
			stepPayload := LinearSyncResolveConflictStep{
				Action:             step.Action,
				Status:             step.Status,
				Message:            step.Message,
				LocalCommit:        step.LocalCommit,
				LocalCommitMessage: step.LocalCommitMessage,
				Files:              step.Files,
				ConflictDiffs:      step.ConflictDiffs,
				Confidence:         step.Confidence,
				Summary:            step.Summary,
				Created:            step.Created,
				TmuxSession:        step.TmuxSession,
			}
			if step.Status != "in_progress" {
				if crState.UpdateLastMatchingStep(step.Action, step.LocalCommit, func(existing *LinearSyncResolveConflictStep) {
					existing.Status = stepPayload.Status
					existing.Message = stepPayload.Message
					existing.LocalCommitMessage = stepPayload.LocalCommitMessage
					existing.Files = stepPayload.Files
					existing.ConflictDiffs = stepPayload.ConflictDiffs
					existing.Confidence = stepPayload.Confidence
					existing.Summary = stepPayload.Summary
					existing.Created = stepPayload.Created
					existing.At = time.Now().Format(time.RFC3339)
				}) {
					go s.BroadcastSessions()
					return
				}
			}
			crState.AddStep(stepPayload)
			go s.BroadcastSessions()
		}

		ctx := s.shutdownCtx
		result, err := s.workspace.LinearSyncResolveConflict(ctx, workspaceID, onStep)

		crLog := logging.Sub(s.logger, "workspace")

		// Update git status (best-effort; do not block final state)
		if _, err := s.workspace.UpdateVCSStatus(s.shutdownCtx, workspaceID); err != nil {
			if !errors.Is(err, workspace.ErrWorkspaceLocked) {
				crLog.Warn("linear-sync-resolve-conflict: failed to update git status", "err", err)
			}
		}

		s.cleanupCRTrackers(crState)

		if err != nil {
			crLog.Error("linear-sync-resolve-conflict failed", "workspace_id", workspaceID, "err", err)
			crState.Finish("failed", fmt.Sprintf("Failed to resolve conflict: %v", err), nil)
			s.finalizeResolveConflictTab(workspaceID, tabID, crState.Hash, "failed")
		} else if result.Success {
			var resolutions []LinearSyncResolveConflictResolution
			for _, r := range result.Resolutions {
				resolutions = append(resolutions, LinearSyncResolveConflictResolution{
					LocalCommit:        r.LocalCommit,
					LocalCommitMessage: r.LocalCommitMessage,
					AllResolved:        r.AllResolved,
					Confidence:         r.Confidence,
					Summary:            r.Summary,
					Files:              r.Files,
				})
			}
			crState.Hash = result.Hash
			crState.Finish("done", result.Message, resolutions)
			s.finalizeResolveConflictTab(workspaceID, tabID, result.Hash, "done")

			// Clear ConflictOnBranch on successful resolution
			if ws, found := s.state.GetWorkspace(workspaceID); found {
				ws.ConflictOnBranch = nil
				if err := s.state.UpdateWorkspace(ws); err != nil {
					crLog.Warn("linear-sync-resolve-conflict: failed to clear conflict state", "err", err)
				} else {
					if err := s.state.Save(); err != nil {
						crLog.Warn("linear-sync-resolve-conflict: failed to save state", "err", err)
					}
				}
			}
		} else {
			var resolutions []LinearSyncResolveConflictResolution
			for _, r := range result.Resolutions {
				resolutions = append(resolutions, LinearSyncResolveConflictResolution{
					LocalCommit:        r.LocalCommit,
					LocalCommitMessage: r.LocalCommitMessage,
					AllResolved:        r.AllResolved,
					Confidence:         r.Confidence,
					Summary:            r.Summary,
					Files:              r.Files,
				})
			}
			crState.Hash = result.Hash
			crState.Finish("failed", result.Message, resolutions)
			s.finalizeResolveConflictTab(workspaceID, tabID, result.Hash, "failed")
		}

		crLog.Info("linear-sync-resolve-conflict done", "workspace_id", workspaceID, "status", crState.Status)
		go s.BroadcastSessions()
	}()

	// Return 202 immediately
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"started":      true,
		"workspace_id": workspaceID,
		"tab_id":       tabID,
	}); err != nil {
		s.logger.Error("failed to encode response", "handler", "resolve-conflict", "err", err)
	}
}

// handleDeleteLinearSyncResolveConflictState handles DELETE requests to dismiss a completed resolve conflict state.
// DELETE /api/workspaces/{id}/linear-sync-resolve-conflict-state
func (s *Server) handleDeleteLinearSyncResolveConflictState(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	existing := s.getLinearSyncResolveConflictState(workspaceID)
	if existing == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if existing.Status == "in_progress" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]string{"message": "operation still in progress"})
		return
	}

	s.deleteLinearSyncResolveConflictState(workspaceID)
	s.removeResolveConflictTab(workspaceID)
	go s.BroadcastSessions()

	w.WriteHeader(http.StatusOK)
}

// updateResolveConflictTab updates the label and meta of the resolve-conflict tab.
func (s *Server) updateResolveConflictTab(workspaceID, tabID, label, hash, status string) {
	tabs := s.state.GetWorkspaceTabs(workspaceID)
	for _, t := range tabs {
		if t.ID == tabID {
			t.Label = label
			t.Meta = map[string]string{"hash": hash, "status": status}
			if err := s.state.UpdateTab(workspaceID, t); err != nil {
				logging.Sub(s.logger, "workspace").Warn("linear-sync-resolve-conflict: failed to update tab", "err", err)
			}
			return
		}
	}
}

// finalizeResolveConflictTab makes the resolve-conflict tab closable and updates its status.
func (s *Server) finalizeResolveConflictTab(workspaceID, tabID, hash, status string) {
	tabs := s.state.GetWorkspaceTabs(workspaceID)
	for _, t := range tabs {
		if t.ID == tabID {
			// Update label to include short hash if available
			if hash != "" {
				shortHash := hash
				if len(shortHash) > 7 {
					shortHash = shortHash[:7]
				}
				t.Label = "Conflict " + shortHash
			}
			t.Closable = true
			t.Meta = map[string]string{"hash": hash, "status": status}
			if err := s.state.UpdateTab(workspaceID, t); err != nil {
				logging.Sub(s.logger, "workspace").Warn("linear-sync-resolve-conflict: failed to finalize tab", "err", err)
			}
			s.state.Save() //nolint:errcheck
			return
		}
	}
}

// removeResolveConflictTab removes all resolve-conflict tabs from the workspace.
func (s *Server) removeResolveConflictTab(workspaceID string) {
	tabs := s.state.GetWorkspaceTabs(workspaceID)
	for _, t := range tabs {
		if t.Kind == "resolve-conflict" {
			if err := s.state.RemoveTab(workspaceID, t.ID); err != nil {
				logging.Sub(s.logger, "workspace").Warn("linear-sync-resolve-conflict: failed to remove tab", "err", err)
			}
		}
	}
	s.state.Save() //nolint:errcheck
}
