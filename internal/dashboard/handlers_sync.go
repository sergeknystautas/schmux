package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/workspace"
)

// handleLinearSync handles POST requests for workspace linear sync operations.
// Dispatches to specific handlers based on URL suffix:
// - GET /api/workspaces/{id}/git-graph - get commit graph
// - POST /api/workspaces/{id}/linear-sync-from-main - sync commits from main into branch
// - POST /api/workspaces/{id}/linear-sync-to-main - sync commits from branch to main
// - POST /api/workspaces/{id}/git-commit-stage - stage files for commit
// - POST /api/workspaces/{id}/git-amend - amend last commit
// - POST /api/workspaces/{id}/git-discard - discard all changes
func (s *Server) handleLinearSync(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// GET routes
	if strings.HasSuffix(path, "/git-graph") {
		s.handleWorkspaceGitGraph(w, r)
		return
	}

	// DELETE routes
	if r.Method == http.MethodDelete {
		if strings.HasSuffix(path, "/linear-sync-resolve-conflict-state") {
			s.handleDeleteLinearSyncResolveConflictState(w, r)
		} else {
			http.NotFound(w, r)
		}
		return
	}

	// All other routes require POST
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Route based on URL suffix
	if strings.HasSuffix(path, "/linear-sync-from-main") {
		s.handleLinearSyncFromMain(w, r)
	} else if strings.HasSuffix(path, "/linear-sync-to-main") {
		s.handleLinearSyncToMain(w, r)
	} else if strings.HasSuffix(path, "/linear-sync-resolve-conflict") {
		s.handleLinearSyncResolveConflict(w, r)
	} else if strings.HasSuffix(path, "/git-commit-stage") {
		s.handleGitCommitStage(w, r)
	} else if strings.HasSuffix(path, "/git-amend") {
		s.handleGitAmend(w, r)
	} else if strings.HasSuffix(path, "/git-discard") {
		s.handleGitDiscard(w, r)
	} else if strings.HasSuffix(path, "/dispose") {
		s.handleDisposeWorkspace(w, r)
	} else if strings.HasSuffix(path, "/dispose-all") {
		s.handleDisposeWorkspaceAll(w, r)
	} else {
		http.NotFound(w, r)
	}
}

// handleLinearSyncFromMain handles POST requests to sync commits from origin/main into branch.
// POST /api/workspaces/{id}/linear-sync-from-main
//
// This performs an iterative rebase that brings commits FROM main INTO the current branch
// one at a time, preserving local changes. Supports diverged branches.
func (s *Server) handleLinearSyncFromMain(w http.ResponseWriter, r *http.Request) {
	// Extract workspace ID from URL: /api/workspaces/{id}/linear-sync-from-main
	path := strings.TrimPrefix(r.URL.Path, "/api/workspaces/")
	workspaceID := strings.TrimSuffix(path, "/linear-sync-from-main")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	type LinearSyncResponse struct {
		Success              bool   `json:"success"`
		Message              string `json:"message"`
		IsPreCommitHookError bool   `json:"is_pre_commit_hook_error"`
		PreCommitErrorDetail string `json:"pre_commit_error_detail,omitempty"`
	}

	// Get workspace from state
	ws, found := s.state.GetWorkspace(workspaceID)
	if !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(LinearSyncResponse{
			Success: false,
			Message: fmt.Sprintf("workspace %s not found", workspaceID),
		})
		return
	}

	fmt.Printf("[workspace] linear-sync-from-main: workspace_id=%s\n", workspaceID)

	// Pause Vite file watching during rebase to prevent transform errors
	// from transient conflict markers in source files.
	s.pauseViteWatch()
	defer s.resumeViteWatch()

	// Perform the sync from main
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitCloneTimeoutMs())*time.Millisecond)
	defer cancel()

	result, err := s.workspace.LinearSyncFromDefault(ctx, workspaceID)
	if err != nil {
		fmt.Printf("[workspace] linear-sync-from-main error: workspace_id=%s error=%v\n", workspaceID, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)

		// Check if it's a pre-commit hook error
		var preCommitErr *workspace.PreCommitHookError
		isPreCommitHookError := errors.As(err, &preCommitErr)

		resp := LinearSyncResponse{
			Success:              false,
			Message:              "Failed to sync from main",
			IsPreCommitHookError: isPreCommitHookError,
		}

		// Extract the raw error detail for pre-commit hook failures
		if isPreCommitHookError && preCommitErr.Unwrap() != nil {
			resp.PreCommitErrorDetail = preCommitErr.Unwrap().Error()
		}

		json.NewEncoder(w).Encode(resp)
		return
	}

	// Update git status after sync (best-effort, don't block response)
	if _, err := s.workspace.UpdateGitStatus(ctx, workspaceID); err != nil {
		if !errors.Is(err, workspace.ErrWorkspaceLocked) {
			fmt.Printf("[workspace] linear-sync-from-main warning: failed to update git status: %v\n", err)
		}
	}

	// Update ConflictOnBranch based on result
	if result.ConflictingHash != "" {
		// Re-fetch workspace to avoid overwriting concurrent changes
		ws, found = s.state.GetWorkspace(workspaceID)
		if !found {
			http.Error(w, "Workspace not found after sync", http.StatusNotFound)
			return
		}
		// Conflict detected - set ConflictOnBranch to current branch
		branch := ws.Branch
		ws.ConflictOnBranch = &branch
		if err := s.state.UpdateWorkspace(ws); err != nil {
			fmt.Printf("[workspace] linear-sync-from-main warning: failed to update conflict state: %v\n", err)
		} else {
			s.state.Save()
			go s.BroadcastSessions()
		}
	}

	successMsg := "success"
	if result.Success {
		successMsg = fmt.Sprintf("success: synced %d commits from %s", result.SuccessCount, result.Branch)
	} else if result.ConflictingHash != "" {
		successMsg = fmt.Sprintf("conflict at %s after %d commits", result.ConflictingHash, result.SuccessCount)
	}
	fmt.Printf("[workspace] linear-sync-from-main: workspace_id=%s %s\n", workspaceID, successMsg)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleLinearSyncToMain handles POST requests to sync commits from branch to origin/main.
// POST /api/workspaces/{id}/linear-sync-to-main
//
// This pushes the current branch's commits directly to main without a merge commit.
func (s *Server) handleLinearSyncToMain(w http.ResponseWriter, r *http.Request) {
	// Extract workspace ID from URL: /api/workspaces/{id}/linear-sync-to-main
	path := strings.TrimPrefix(r.URL.Path, "/api/workspaces/")
	workspaceID := strings.TrimSuffix(path, "/linear-sync-to-main")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	type LinearSyncResponse struct {
		Success              bool   `json:"success"`
		Message              string `json:"message"`
		IsPreCommitHookError bool   `json:"is_pre_commit_hook_error"`
		PreCommitErrorDetail string `json:"pre_commit_error_detail,omitempty"`
	}

	// Get workspace from state
	_, found := s.state.GetWorkspace(workspaceID)
	if !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(LinearSyncResponse{
			Success: false,
			Message: fmt.Sprintf("workspace %s not found", workspaceID),
		})
		return
	}

	fmt.Printf("[workspace] linear-sync-to-main: workspace_id=%s\n", workspaceID)

	// Perform the sync to main
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitCloneTimeoutMs())*time.Millisecond)
	defer cancel()

	result, err := s.workspace.LinearSyncToDefault(ctx, workspaceID)
	if err != nil {
		fmt.Printf("[workspace] linear-sync-to-main error: workspace_id=%s error=%v\n", workspaceID, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(LinearSyncResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to sync to main: %v", err),
		})
		return
	}

	// Update git status after sync (best-effort, don't block response)
	if _, err := s.workspace.UpdateGitStatus(ctx, workspaceID); err != nil {
		if !errors.Is(err, workspace.ErrWorkspaceLocked) {
			fmt.Printf("[workspace] linear-sync-to-main warning: failed to update git status: %v\n", err)
		}
	}

	successMsg := "success"
	if result.Success {
		successMsg = fmt.Sprintf("success: pushed %d commits to %s", result.SuccessCount, result.Branch)
	} else {
		successMsg = "failed"
	}
	fmt.Printf("[workspace] linear-sync-to-main: workspace_id=%s %s\n", workspaceID, successMsg)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleLinearSyncResolveConflict handles POST requests to kick off conflict resolution.
// Returns 202 immediately; progress is streamed via the /ws/dashboard WebSocket.
// POST /api/workspaces/{id}/linear-sync-resolve-conflict
func (s *Server) handleLinearSyncResolveConflict(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/workspaces/")
	workspaceID := strings.TrimSuffix(path, "/linear-sync-resolve-conflict")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	// 404 if workspace not found
	if _, found := s.state.GetWorkspace(workspaceID); !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
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
			json.NewEncoder(w).Encode(map[string]interface{}{
				"started": false, "message": "operation already in progress",
			})
			return
		}
		// Auto-clear completed/failed state
		s.deleteLinearSyncResolveConflictState(workspaceID)
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

	fmt.Printf("[workspace] linear-sync-resolve-conflict: started workspace_id=%s\n", workspaceID)

	// Launch background goroutine
	go func() {
		// Pause Vite file watching during conflict resolution to prevent
		// transform errors from transient conflict markers in source files.
		s.pauseViteWatch()
		defer s.resumeViteWatch()

		// Panic recovery — never leave state stuck at in_progress
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[workspace] linear-sync-resolve-conflict: PANIC: %v\n", r)
				crState.Finish("failed", fmt.Sprintf("Internal error: %v", r), nil)
				go s.BroadcastSessions()
			}
		}()

		// Wire the step callback to state mutations + broadcasts
		onStep := func(step workspace.ResolveConflictStep) {
			if step.Hash != "" {
				crState.SetHash(step.Hash)
			}
			stepPayload := LinearSyncResolveConflictStep{
				Action:             step.Action,
				Status:             step.Status,
				Message:            step.Message,
				LocalCommit:        step.LocalCommit,
				LocalCommitMessage: step.LocalCommitMessage,
				Files:              step.Files,
				Confidence:         step.Confidence,
				Summary:            step.Summary,
				Created:            step.Created,
			}
			if step.Status != "in_progress" {
				if crState.UpdateLastMatchingStep(step.Action, step.LocalCommit, func(existing *LinearSyncResolveConflictStep) {
					existing.Status = stepPayload.Status
					existing.Message = stepPayload.Message
					existing.LocalCommitMessage = stepPayload.LocalCommitMessage
					existing.Files = stepPayload.Files
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

		// Update git status (best-effort; do not block final state)
		if _, err := s.workspace.UpdateGitStatus(s.shutdownCtx, workspaceID); err != nil {
			if !errors.Is(err, workspace.ErrWorkspaceLocked) {
				fmt.Printf("[workspace] linear-sync-resolve-conflict warning: failed to update git status: %v\n", err)
			}
		}

		if err != nil {
			fmt.Printf("[workspace] linear-sync-resolve-conflict error: workspace_id=%s error=%v\n", workspaceID, err)
			crState.Finish("failed", fmt.Sprintf("Failed to resolve conflict: %v", err), nil)
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

			// Clear ConflictOnBranch on successful resolution
			if ws, found := s.state.GetWorkspace(workspaceID); found {
				ws.ConflictOnBranch = nil
				if err := s.state.UpdateWorkspace(ws); err != nil {
					fmt.Printf("[workspace] linear-sync-resolve-conflict warning: failed to clear conflict state: %v\n", err)
				} else {
					s.state.Save()
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
		}

		fmt.Printf("[workspace] linear-sync-resolve-conflict done: workspace_id=%s status=%s\n", workspaceID, crState.Status)
		go s.BroadcastSessions()
	}()

	// Return 202 immediately
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"started":      true,
		"workspace_id": workspaceID,
	})
}

// handleDeleteLinearSyncResolveConflictState handles DELETE requests to dismiss a completed resolve conflict state.
// DELETE /api/workspaces/{id}/linear-sync-resolve-conflict-state
func (s *Server) handleDeleteLinearSyncResolveConflictState(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/workspaces/")
	workspaceID := strings.TrimSuffix(path, "/linear-sync-resolve-conflict-state")
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
		json.NewEncoder(w).Encode(map[string]string{"message": "operation still in progress"})
		return
	}

	s.deleteLinearSyncResolveConflictState(workspaceID)
	go s.BroadcastSessions()

	w.WriteHeader(http.StatusOK)
}
