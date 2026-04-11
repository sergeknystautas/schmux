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
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/vcs"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// handleWorkspaceCommitGraph handles GET /api/workspaces/{id}/commit-graph.
func (s *Server) handleWorkspaceCommitGraph(w http.ResponseWriter, r *http.Request) {
	ws, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}

	if !workspace.HasVCSSupport(ws.VCS) {
		writeJSONError(w, "commit graph not available for this VCS type", http.StatusBadRequest)
		return
	}

	// Parse query params
	// max_total: Maximum total commits to display (applied after category limits)
	maxTotal := 200
	if mt := r.URL.Query().Get("max_total"); mt != "" {
		if parsed, err := strconv.Atoi(mt); err == nil && parsed > 0 {
			maxTotal = parsed
		}
	}
	// For backward compatibility, also accept max_commits
	if mt := r.URL.Query().Get("max_commits"); mt != "" && maxTotal == 200 {
		if parsed, err := strconv.Atoi(mt); err == nil && parsed > 0 {
			maxTotal = parsed
		}
	}
	if maxTotal > 5000 {
		maxTotal = 5000
	}

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

	// Delegate to remote handler if this is a remote workspace
	if ws.RemoteHostID != "" {
		// Cap remote to minimize SSH round-trips on large repos
		if maxTotal > 10 {
			maxTotal = 10
		}
		s.handleRemoteCommitGraph(w, r, ws, maxTotal, mainContext)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := s.workspace.GetGitGraph(ctx, ws.ID, maxTotal, mainContext)
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
		s.logger.Error("failed to encode response", "handler", "commit-graph", "err", err)
	}
}

// handleRemoteCommitGraph handles git graph requests for remote workspaces.
// Uses a single batched RunCommand to resolve HEAD and fetch the log in one
// tmux window — minimizes control mode channel contention.
func (s *Server) handleRemoteCommitGraph(w http.ResponseWriter, r *http.Request, ws state.Workspace, maxTotal int, mainContext int) {
	if s.remoteManager == nil {
		writeJSONError(w, "remote manager not available", http.StatusServiceUnavailable)
		return
	}

	conn := s.remoteManager.GetConnection(ws.RemoteHostID)
	if conn == nil || !conn.IsConnected() {
		writeJSONError(w, "remote host not connected", http.StatusServiceUnavailable)
		return
	}

	cb := vcs.NewCommandBuilder(s.vcsTypeForWorkspace(ws))

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	workdir := ws.RemotePath
	localBranch := ws.Branch
	// If the "branch" is a raw commit hash (Sapling on fbsource has no
	// bookmarks), use the short hash as a label instead of the 40-char blob.
	if isValidVCSHash(localBranch) && len(localBranch) >= 12 {
		localBranch = localBranch[:12]
	}

	// Batch resolve-HEAD + detect default branch + log into a single RunCommand
	// (1 tmux window, 1 poll loop). We skip resolving the default branch HEAD
	// to save a round-trip — the branch label still appears for context.
	const delim = "__SCHMUX_GRAPH_DELIM__"
	batchCmd := fmt.Sprintf("%s; echo %s; %s; echo %s; %s",
		cb.ResolveRef("HEAD"), delim,
		cb.DetectDefaultBranch(), delim,
		cb.Log([]string{"HEAD"}, maxTotal))

	out, err := conn.RunCommand(ctx, workdir, batchCmd)
	if err != nil {
		s.logger.Error("remote commit graph failed", "err", err)
		writeJSONError(w, fmt.Sprintf("commit graph failed: %v", err), http.StatusInternalServerError)
		return
	}

	sections := strings.SplitN(out, delim, 3)

	// Section 0: HEAD hash
	localHead := strings.TrimSpace(sections[0])
	if !isValidVCSHash(localHead) {
		writeJSONError(w, fmt.Sprintf("HEAD resolved to invalid hash: %q", localHead), http.StatusInternalServerError)
		return
	}

	// Section 1: default branch name (e.g., "main" or "master")
	var defaultBranch string
	if len(sections) > 1 {
		defaultBranch = strings.TrimSpace(sections[1])
	}
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	defaultBranchRef := cb.DefaultBranchRef(defaultBranch) // e.g., "remote/main"

	// Section 2: log output
	var logOutput string
	if len(sections) > 2 {
		logOutput = strings.TrimSpace(sections[2])
	}

	if s.logger != nil {
		lines := strings.Split(logOutput, "\n")
		firstLine := ""
		if len(lines) > 0 {
			firstLine = lines[0]
		}
		s.logger.Debug("remote commit graph: raw output",
			"head", localHead,
			"log_len", len(logOutput),
			"line_count", len(lines),
			"first_line_len", len(firstLine),
			"first_line", fmt.Sprintf("%q", firstLine),
		)
	}

	rawNodes := workspace.ParseGitLogOutput(logOutput)

	// Build workspace ID mapping for annotations.
	// Truncate hash-only branch names to match the localBranch truncation above.
	branchWorkspaces := make(map[string][]string)
	for _, ws2 := range s.state.GetWorkspaces() {
		if ws2.Repo == ws.Repo {
			bName := ws2.Branch
			if isValidVCSHash(bName) && len(bName) >= 12 {
				bName = bName[:12]
			}
			branchWorkspaces[bName] = append(branchWorkspaces[bName], ws2.ID)
		}
	}

	// Build response with default branch reference (e.g., "remote/main").
	// originMainHead is empty — we don't resolve it here to save a round trip.
	// The default branch label still appears in the branches map for context.
	resp := workspace.BuildGraphResponse(rawNodes, localBranch, defaultBranchRef, localHead, "", "", branchWorkspaces, ws.Repo, maxTotal, 0)

	// Populate dirty state from workspace VCS stats (same as local handler)
	if ws.FilesChanged > 0 {
		resp.DirtyState = &contracts.CommitGraphDirtyState{
			FilesChanged: ws.FilesChanged,
			LinesAdded:   ws.LinesAdded,
			LinesRemoved: ws.LinesRemoved,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode response", "handler", "remote-commit-graph", "err", err)
	}
}

// isValidVCSHash checks if a string looks like a valid VCS hash (40+ hex characters).
func isValidVCSHash(s string) bool {
	if len(s) < 40 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// handleWorkspaceCommitDetail handles GET /api/workspaces/{id}/commit-detail/{hash}.
func (s *Server) handleWorkspaceCommitDetail(w http.ResponseWriter, r *http.Request) {
	// Extract workspace ID and commit hash from chi URL params
	workspaceID := chi.URLParam(r, "workspaceID")
	commitHash := chi.URLParam(r, "hash")
	if workspaceID == "" || commitHash == "" {
		writeJSONError(w, "invalid path: expected /api/workspaces/{id}/commit-detail/{hash}", http.StatusBadRequest)
		return
	}

	// Verify workspace exists
	ws, ok := s.state.GetWorkspace(workspaceID)
	if !ok {
		writeJSONError(w, "workspace not found: "+workspaceID, http.StatusNotFound)
		return
	}

	if !workspace.HasVCSSupport(ws.VCS) {
		writeJSONError(w, "commit detail not available for this VCS type", http.StatusBadRequest)
		return
	}

	// TODO: Remote workspace support
	if ws.RemoteHostID != "" {
		writeJSONError(w, "commit detail not yet supported for remote workspaces", http.StatusNotImplemented)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := s.workspace.GetCommitDetail(ctx, workspaceID, commitHash)
	if err != nil {
		// Determine appropriate status code based on error
		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "invalid commit hash") {
			statusCode = http.StatusBadRequest
		} else if strings.Contains(err.Error(), "commit not found") || strings.Contains(err.Error(), "workspace not found") {
			statusCode = http.StatusNotFound
		}
		writeJSONError(w, err.Error(), statusCode)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode response", "handler", "commit-detail", "err", err)
	}
}

// handleStage handles POST /api/workspaces/{id}/stage.
// Stages the specified files for commit.
func (s *Server) handleStage(w http.ResponseWriter, r *http.Request) {
	ws, ok := s.requireWorkspace(w, r)
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

	cb := vcs.NewCommandBuilder(s.vcsTypeForWorkspace(ws))
	ctx := r.Context()
	run := localShellRun(ctx, ws.Path)

	if _, err := run(cb.AddFiles(req.Files)); err != nil {
		writeJSONError(w, fmt.Sprintf("stage failed: %s", err), http.StatusInternalServerError)
		return
	}

	if _, err := s.workspace.UpdateVCSStatus(ctx, ws.ID); err != nil {
		s.logger.Warn("failed to update VCS status after stage", "err", err)
	}
	s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Files staged"}); err != nil {
		s.logger.Error("failed to encode response", "handler", "stage", "err", err)
	}
}

// handleAmend handles POST /api/workspaces/{id}/amend.
// Stages the specified files and amends the last commit.
func (s *Server) handleAmend(w http.ResponseWriter, r *http.Request) {
	ws, ok := s.requireWorkspace(w, r)
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

	cb := vcs.NewCommandBuilder(s.vcsTypeForWorkspace(ws))
	ctx := r.Context()
	run := localShellRun(ctx, ws.Path)

	if _, err := run(cb.AddFiles(req.Files)); err != nil {
		writeJSONError(w, fmt.Sprintf("stage failed: %s", err), http.StatusInternalServerError)
		return
	}

	if _, err := run(cb.CommitAmendNoEdit()); err != nil {
		writeJSONError(w, fmt.Sprintf("amend failed: %s", err), http.StatusInternalServerError)
		return
	}

	if _, err := s.workspace.UpdateVCSStatus(ctx, ws.ID); err != nil {
		s.logger.Warn("failed to update VCS status after amend", "err", err)
	}
	s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Commit amended"}); err != nil {
		s.logger.Error("failed to encode response", "handler", "amend", "err", err)
	}
}

// handleDiscard handles POST /api/workspaces/{id}/discard.
// Discards local changes. If files are specified, only those files are discarded.
func (s *Server) handleDiscard(w http.ResponseWriter, r *http.Request) {
	ws, ok := s.requireWorkspace(w, r)
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

	cb := vcs.NewCommandBuilder(s.vcsTypeForWorkspace(ws))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	run := localShellRun(ctx, ws.Path)

	s.logger.Info("discard", "workspace", ws.ID, "path", ws.Path, "files", req.Files)

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
					s.logger.Debug("discard tracked failed", "file", file, "err", err)
				} else {
					s.logger.Debug("restored from HEAD", "file", file)
				}
			} else {
				// Untracked or staged-new: unstage then remove
				// Try unstage first (handles staged-new files in git)
				_, _ = run(cb.UnstageNewFile(file))
				// Remove the working tree copy
				if err := os.Remove(filePath); err != nil {
					// Fallback: use VCS clean command
					if _, err2 := run(cb.CleanUntrackedFile(file)); err2 != nil {
						s.logger.Debug("clean failed", "file", file, "err", err2)
					} else {
						s.logger.Debug("cleaned untracked file", "file", file)
					}
				} else {
					s.logger.Debug("removed untracked file", "file", file)
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

	if _, err := s.workspace.UpdateVCSStatus(ctx, ws.ID); err != nil {
		s.logger.Warn("failed to update VCS status after discard", "err", err)
	}
	s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Changes discarded"}); err != nil {
		s.logger.Error("failed to encode response", "handler", "discard", "err", err)
	}
}

// handleUncommit handles POST /api/workspaces/{id}/uncommit.
// Resets the HEAD commit, keeping changes as unstaged.
// Requires hash parameter to verify we're uncommitting the expected commit.
func (s *Server) handleUncommit(w http.ResponseWriter, r *http.Request) {
	ws, ok := s.requireWorkspace(w, r)
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

	cb := vcs.NewCommandBuilder(s.vcsTypeForWorkspace(ws))
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

	if _, err := s.workspace.UpdateVCSStatus(ctx, ws.ID); err != nil {
		s.logger.Warn("failed to update VCS status after uncommit", "err", err)
	}
	s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Commit undone, changes are now unstaged"}); err != nil {
		s.logger.Error("failed to encode response", "handler", "uncommit", "err", err)
	}
}
