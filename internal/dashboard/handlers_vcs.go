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
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/vcs"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// parseCommitGraphLimits reads max_total (and the deprecated max_commits alias)
// from the request, defaulting to 200 and capped at 5000.
func parseCommitGraphLimits(r *http.Request) int {
	maxTotal := 200
	specified := false
	if mt := r.URL.Query().Get("max_total"); mt != "" {
		if parsed, err := strconv.Atoi(mt); err == nil && parsed > 0 {
			maxTotal = parsed
			specified = true
		}
	}
	// For backward compatibility, also accept max_commits
	if mt := r.URL.Query().Get("max_commits"); mt != "" && !specified {
		if parsed, err := strconv.Atoi(mt); err == nil && parsed > 0 {
			maxTotal = parsed
		}
	}
	if maxTotal > 5000 {
		maxTotal = 5000
	}
	return maxTotal
}

// handleWorkspaceCommitGraph handles GET /api/workspaces/{id}/commit-graph.
func (h *GitHandlers) handleWorkspaceCommitGraph(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.requireWorkspace(w, r)
	if !ok {
		return
	}

	if !workspace.HasVCSSupport(ws.VCS) {
		writeJSONError(w, "commit graph not available for this VCS type", http.StatusBadRequest)
		return
	}

	// Parse query params
	maxTotal := parseCommitGraphLimits(r)

	// main_context: Number of commits on main BEFORE fork point (historical context)
	mainContext := 5
	if mc := r.URL.Query().Get("main_context"); mc != "" {
		if parsed, err := strconv.Atoi(mc); err == nil && parsed > 0 {
			mainContext = parsed
		}
	}
	// For backward compatibility, also accept context
	if mc := r.URL.Query().Get("context"); mc != "" && mainContext == 5 {
		if parsed, err := strconv.Atoi(mc); err == nil && parsed > 0 {
			mainContext = parsed
		}
	}
	if mainContext > 500 {
		mainContext = 500
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := h.workspace.GetGitGraph(ctx, ws.ID, maxTotal, mainContext)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Populate dirty state from workspace git stats
	if ws.FilesChanged > 0 {
		resp.DirtyState = &contracts.CommitGraphDirtyState{
			FilesChanged: ws.FilesChanged,
			LinesAdded:   ws.LinesAdded,
			LinesRemoved: ws.LinesRemoved,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", "handler", "commit-graph", "err", err)
	}
}

// handleWorkspaceCommitDetail handles GET /api/workspaces/{id}/commit-detail/{hash}.
func (h *GitHandlers) handleWorkspaceCommitDetail(w http.ResponseWriter, r *http.Request) {
	// Extract workspace ID and commit hash from chi URL params
	workspaceID := chi.URLParam(r, "workspaceID")
	commitHash := chi.URLParam(r, "hash")
	if workspaceID == "" || commitHash == "" {
		writeJSONError(w, "invalid path: expected /api/workspaces/{id}/commit-detail/{hash}", http.StatusBadRequest)
		return
	}

	// Verify workspace exists
	ws, ok := h.state.GetWorkspace(workspaceID)
	if !ok {
		writeJSONError(w, "workspace not found: "+workspaceID, http.StatusNotFound)
		return
	}

	if !workspace.HasVCSSupport(ws.VCS) {
		writeJSONError(w, "commit detail not available for this VCS type", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := h.workspace.GetCommitDetail(ctx, workspaceID, commitHash)
	if err != nil {
		// Determine appropriate status code based on error
		statusCode := http.StatusInternalServerError
		if errors.Is(err, workspace.ErrInvalidCommit) {
			statusCode = http.StatusBadRequest
		} else if errors.Is(err, workspace.ErrCommitNotFound) || errors.Is(err, workspace.ErrNotFound) {
			statusCode = http.StatusNotFound
		}
		writeJSONError(w, err.Error(), statusCode)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", "handler", "commit-detail", "err", err)
	}
}

func filesNeedingStage(files []string, statusOut string) []string {
	stagedDeletions := make(map[string]bool)
	for _, line := range strings.Split(statusOut, "\n") {
		if len(line) >= 4 && line[0] == 'D' && line[1] == ' ' {
			stagedDeletions[strings.TrimSpace(line[3:])] = true
		}
	}
	if len(stagedDeletions) == 0 {
		return files
	}

	filtered := make([]string, 0, len(files))
	for _, file := range files {
		if !stagedDeletions[file] {
			filtered = append(filtered, file)
		}
	}
	return filtered
}

// handleStage handles POST /api/workspaces/{id}/stage.
// Stages the specified files for commit.
func (h *GitHandlers) handleStage(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.requireWorkspace(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		Files []string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if msg := validateGitFilePaths(req.Files); msg != "" {
		writeJSONError(w, msg, http.StatusBadRequest)
		return
	}

	cb := vcs.NewCommandBuilder(h.vcsTypeForWorkspace(ws))
	ctx := r.Context()
	run := localShellRun(ctx, ws.Path)

	filesToAdd := req.Files
	if statusOut, err := run(cb.StatusPorcelain()); err == nil && statusOut != "" {
		// git add rejects a deletion already removed from the index because the
		// path no longer exists there or in the working tree.
		filesToAdd = filesNeedingStage(req.Files, statusOut)
	}

	if len(filesToAdd) > 0 {
		if _, err := run(cb.AddFiles(filesToAdd)); err != nil {
			writeJSONError(w, fmt.Sprintf("stage failed: %s", err), http.StatusInternalServerError)
			return
		}
	}

	if _, err := h.workspace.UpdateVCSStatus(ctx, ws.ID); err != nil {
		h.logger.Warn("failed to update VCS status after stage", "err", err)
	}
	h.broadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Files staged"}); err != nil {
		h.logger.Error("failed to encode response", "handler", "stage", "err", err)
	}
}

// handleAmend handles POST /api/workspaces/{id}/amend.
// Stages the specified files and amends the last commit.
func (h *GitHandlers) handleAmend(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.requireWorkspace(w, r)
	if !ok {
		return
	}

	if ws.Ahead <= 0 {
		writeJSONError(w, "No commits to amend", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		Files []string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Files) == 0 {
		writeJSONError(w, "at least one file is required", http.StatusBadRequest)
		return
	}

	if msg := validateGitFilePaths(req.Files); msg != "" {
		writeJSONError(w, msg, http.StatusBadRequest)
		return
	}

	cb := vcs.NewCommandBuilder(h.vcsTypeForWorkspace(ws))
	ctx := r.Context()
	run := localShellRun(ctx, ws.Path)

	filesToAdd := req.Files
	if statusOut, err := run(cb.StatusPorcelain()); err == nil && statusOut != "" {
		filesToAdd = filesNeedingStage(req.Files, statusOut)
	}
	if len(filesToAdd) > 0 {
		if _, err := run(cb.AddFiles(filesToAdd)); err != nil {
			writeJSONError(w, fmt.Sprintf("stage failed: %s", err), http.StatusInternalServerError)
			return
		}
	}

	if _, err := run(cb.CommitAmendNoEdit()); err != nil {
		writeJSONError(w, fmt.Sprintf("amend failed: %s", err), http.StatusInternalServerError)
		return
	}

	if _, err := h.workspace.UpdateVCSStatus(ctx, ws.ID); err != nil {
		h.logger.Warn("failed to update VCS status after amend", "err", err)
	}
	h.broadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Commit amended"}); err != nil {
		h.logger.Error("failed to encode response", "handler", "amend", "err", err)
	}
}

// handleDiscard handles POST /api/workspaces/{id}/discard.
// Discards local changes. If files are specified, only those files are discarded.
func (h *GitHandlers) handleDiscard(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.requireWorkspace(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		Files []string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Only allow empty/EOF body (means "discard all").
		// Malformed JSON is an error — don't silently discard everything.
		if !errors.Is(err, io.EOF) {
			writeJSONError(w, "invalid request body", http.StatusBadRequest)
			return
		}
	}

	if len(req.Files) > 0 {
		if msg := validateGitFilePaths(req.Files); msg != "" {
			writeJSONError(w, msg, http.StatusBadRequest)
			return
		}
	}

	cb := vcs.NewCommandBuilder(h.vcsTypeForWorkspace(ws))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	run := localShellRun(ctx, ws.Path)

	h.logger.Info("discard", "workspace", ws.ID, "path", ws.Path, "files", req.Files)

	if len(req.Files) > 0 {
		// Discard specific files
		for _, file := range req.Files {
			filePath := filepath.Join(ws.Path, file)

			// Check if file exists in HEAD (tracked vs untracked)
			oldContent, _ := run(cb.ShowFile(file, "HEAD"))
			isTracked := oldContent != ""

			if isTracked {
				// Tracked file: restore from HEAD
				if _, err := run(cb.DiscardFile(file)); err != nil {
					h.logger.Debug("discard tracked failed", "file", file, "err", err)
				} else {
					h.logger.Debug("restored from HEAD", "file", file)
				}
			} else {
				// Untracked or staged-new: unstage then remove
				// Try unstage first (handles staged-new files in git)
				_, _ = run(cb.UnstageNewFile(file))
				// Remove the working tree copy
				if err := os.Remove(filePath); err != nil {
					// Fallback: use VCS clean command
					if _, err2 := run(cb.CleanUntrackedFile(file)); err2 != nil {
						h.logger.Debug("clean failed", "file", file, "err", err2)
					} else {
						h.logger.Debug("cleaned untracked file", "file", file)
					}
				} else {
					h.logger.Debug("removed untracked file", "file", file)
				}
			}
		}
	} else {
		// Discard all changes
		if _, err := run(cb.CleanAllUntracked()); err != nil {
			writeJSONError(w, fmt.Sprintf("clean untracked failed: %s", err), http.StatusInternalServerError)
			return
		}

		if _, err := run(cb.DiscardAllTracked()); err != nil {
			writeJSONError(w, fmt.Sprintf("discard tracked failed: %s", err), http.StatusInternalServerError)
			return
		}
	}

	if _, err := h.workspace.UpdateVCSStatus(ctx, ws.ID); err != nil {
		h.logger.Warn("failed to update VCS status after discard", "err", err)
	}
	h.broadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Changes discarded"}); err != nil {
		h.logger.Error("failed to encode response", "handler", "discard", "err", err)
	}
}

// handleUncommit handles POST /api/workspaces/{id}/uncommit.
// Resets the HEAD commit, keeping changes as unstaged.
// Requires hash parameter to verify we're uncommitting the expected commit.
func (h *GitHandlers) handleUncommit(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.requireWorkspace(w, r)
	if !ok {
		return
	}

	if ws.Ahead <= 0 {
		writeJSONError(w, "No commits to uncommit", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		Hash string `json:"hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Hash == "" {
		writeJSONError(w, "hash is required", http.StatusBadRequest)
		return
	}

	cb := vcs.NewCommandBuilder(h.vcsTypeForWorkspace(ws))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	run := localShellRun(ctx, ws.Path)

	// Verify the current HEAD matches the expected hash
	currentHead, err := run(cb.ResolveRef("HEAD"))
	if err != nil {
		writeJSONError(w, "failed to get current HEAD", http.StatusInternalServerError)
		return
	}

	if currentHead != req.Hash {
		writeJSONError(w, "HEAD has changed, please refresh and try again", http.StatusConflict)
		return
	}

	// Undo the last commit, keeping changes unstaged
	if _, err := run(cb.Uncommit()); err != nil {
		writeJSONError(w, fmt.Sprintf("uncommit failed: %s", err), http.StatusInternalServerError)
		return
	}

	if _, err := h.workspace.UpdateVCSStatus(ctx, ws.ID); err != nil {
		h.logger.Warn("failed to update VCS status after uncommit", "err", err)
	}
	h.broadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Commit undone, changes are now unstaged"}); err != nil {
		h.logger.Error("failed to encode response", "handler", "uncommit", "err", err)
	}
}
