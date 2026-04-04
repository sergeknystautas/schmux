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
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/vcs"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// handleWorkspaceGitGraph handles GET /api/workspaces/{id}/git-graph.
func (s *Server) handleWorkspaceGitGraph(w http.ResponseWriter, r *http.Request) {
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
		s.handleRemoteGitGraph(w, r, ws, maxTotal, mainContext)
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
		resp.DirtyState = &contracts.GitGraphDirtyState{
			FilesChanged: ws.FilesChanged,
			LinesAdded:   ws.LinesAdded,
			LinesRemoved: ws.LinesRemoved,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode response", "handler", "git-graph", "err", err)
	}
}

// handleRemoteGitGraph handles git graph requests for remote workspaces.
func (s *Server) handleRemoteGitGraph(w http.ResponseWriter, r *http.Request, ws state.Workspace, maxTotal int, mainContext int) {
	if s.remoteManager == nil {
		writeJSONError(w, "remote manager not available", http.StatusServiceUnavailable)
		return
	}

	conn := s.remoteManager.GetConnection(ws.RemoteHostID)
	if conn == nil || !conn.IsConnected() {
		writeJSONError(w, "remote host not connected", http.StatusServiceUnavailable)
		return
	}

	// Get VCS type from flavor config
	host, _ := s.state.GetRemoteHost(ws.RemoteHostID)
	vcsType := ""
	if host.ProfileID != "" {
		if profile, found := s.config.GetRemoteProfile(host.ProfileID); found {
			if resolved, err := config.ResolveProfileFlavor(profile, host.Flavor); err == nil {
				vcsType = resolved.VCS
			} else {
				vcsType = profile.VCS
			}
		}
	}
	cb := vcs.NewCommandBuilder(vcsType)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	workdir := ws.RemotePath
	localBranch := ws.Branch

	// Detect default branch using VCS-appropriate command
	defaultBranch := "main"
	if out, err := conn.RunCommand(ctx, workdir, cb.DetectDefaultBranch()); err == nil {
		if branch := strings.TrimSpace(out); branch != "" {
			defaultBranch = branch
		}
	}

	defaultBranchRef := cb.DefaultBranchRef(defaultBranch)

	// Resolve HEAD and default branch ref
	localHeadOutput, err := conn.RunCommand(ctx, workdir, cb.ResolveRef("HEAD"))
	if err != nil {
		writeJSONError(w, "cannot resolve HEAD", http.StatusInternalServerError)
		return
	}
	localHead := strings.TrimSpace(localHeadOutput)
	if !isValidVCSHash(localHead) {
		writeJSONError(w, fmt.Sprintf("HEAD resolved to invalid hash: %q", localHead), http.StatusInternalServerError)
		return
	}

	originMainHead := ""
	if out, err := conn.RunCommand(ctx, workdir, cb.ResolveRef(defaultBranchRef)); err == nil {
		trimmed := strings.TrimSpace(out)
		if isValidVCSHash(trimmed) {
			originMainHead = trimmed
		} else {
			s.logger.Debug("ignoring invalid default branch ref output", "output", trimmed)
		}
	}

	// Find fork point
	var forkPoint string
	if originMainHead != "" && localHead != originMainHead {
		if out, err := conn.RunCommand(ctx, workdir, cb.MergeBase("HEAD", defaultBranchRef)); err == nil {
			trimmed := strings.TrimSpace(out)
			if isValidVCSHash(trimmed) {
				forkPoint = trimmed
			}
		}
	}

	// Get main-ahead count (commits on origin/main that aren't on HEAD)
	mainAheadCount := 0
	if originMainHead != "" && localHead != originMainHead {
		if out, err := conn.RunCommand(ctx, workdir, cb.RevListCount("HEAD.."+defaultBranchRef)); err == nil {
			fmt.Sscanf(strings.TrimSpace(out), "%d", &mainAheadCount)
		}
	}

	// Get newest timestamp of commits ahead on main
	var mainAheadNewestTimestamp string
	if mainAheadCount > 0 {
		if out, err := conn.RunCommand(ctx, workdir, cb.NewestTimestamp("HEAD.."+defaultBranchRef)); err == nil {
			mainAheadNewestTimestamp = strings.TrimSpace(out)
		}
	}

	// Build workspace ID mapping for annotations
	branchWorkspaces := make(map[string][]string)
	for _, w := range s.state.GetWorkspaces() {
		if w.Repo == ws.Repo {
			branchWorkspaces[w.Branch] = append(branchWorkspaces[w.Branch], w.ID)
		}
	}

	// Get log output
	var logOutput string
	if originMainHead == "" || localHead == originMainHead {
		out, err := conn.RunCommand(ctx, workdir, cb.Log([]string{"HEAD"}, mainContext+1))
		if err != nil {
			writeJSONError(w, fmt.Sprintf("log failed: %v", err), http.StatusInternalServerError)
			return
		}
		logOutput = out
	} else if forkPoint == "" {
		out, err := conn.RunCommand(ctx, workdir, cb.Log([]string{"HEAD", defaultBranchRef}, maxTotal))
		if err != nil {
			writeJSONError(w, fmt.Sprintf("log failed: %v", err), http.StatusInternalServerError)
			return
		}
		logOutput = out
	} else {
		// Divergence: get local commits + context (no main-ahead data)
		// Get local commits from HEAD
		maxLocal := maxTotal - mainContext
		if maxLocal < 5 {
			maxLocal = 5
		}
		out, err := conn.RunCommand(ctx, workdir, cb.Log([]string{"HEAD"}, maxLocal))
		if err != nil {
			writeJSONError(w, fmt.Sprintf("log failed: %v", err), http.StatusInternalServerError)
			return
		}
		logOutput = out

		// Add context commits below fork point
		if mainContext > 0 {
			ctxOut, ctxErr := conn.RunCommand(ctx, workdir, cb.Log([]string{forkPoint}, mainContext))
			if ctxErr == nil {
				logOutput = logOutput + "\n" + ctxOut
			}
		}
	}

	rawNodes := workspace.ParseGitLogOutput(logOutput)

	// Detect local truncation for the divergence case
	localTruncated := false
	if forkPoint != "" && originMainHead != "" && localHead != originMainHead {
		maxLocal := maxTotal - mainContext
		if maxLocal < 5 {
			maxLocal = 5
		}
		localCount := 0
		for _, n := range rawNodes {
			if n.Hash == forkPoint {
				break
			}
			localCount++
		}
		localTruncated = localCount >= maxLocal
	}

	resp := workspace.BuildGraphResponse(rawNodes, localBranch, defaultBranch, localHead, originMainHead, forkPoint, branchWorkspaces, ws.Repo, maxTotal, mainAheadCount)
	resp.MainAheadNewestTimestamp = mainAheadNewestTimestamp
	resp.LocalTruncated = localTruncated

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode response", "handler", "remote-git-graph", "err", err)
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

// handleWorkspaceGitCommit handles GET /api/workspaces/{id}/git-commit/{hash}.
func (s *Server) handleWorkspaceGitCommit(w http.ResponseWriter, r *http.Request) {
	// Extract workspace ID and commit hash from chi URL params
	workspaceID := chi.URLParam(r, "workspaceID")
	commitHash := chi.URLParam(r, "hash")
	if workspaceID == "" || commitHash == "" {
		writeJSONError(w, "invalid path: expected /api/workspaces/{id}/git-commit/{hash}", http.StatusBadRequest)
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
		s.logger.Error("failed to encode response", "handler", "git-commit", "err", err)
	}
}

// handleGitCommitStage handles POST /api/workspaces/{id}/git-commit-stage.
// Stages the specified files for commit.
func (s *Server) handleGitCommitStage(w http.ResponseWriter, r *http.Request) {
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
		s.logger.Error("failed to encode response", "handler", "git-commit-stage", "err", err)
	}
}

// handleGitAmend handles POST /api/workspaces/{id}/git-amend.
// Stages the specified files and amends the last commit.
func (s *Server) handleGitAmend(w http.ResponseWriter, r *http.Request) {
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
		s.logger.Error("failed to encode response", "handler", "git-amend", "err", err)
	}
}

// handleGitDiscard handles POST /api/workspaces/{id}/git-discard.
// Discards local changes. If files are specified, only those files are discarded.
func (s *Server) handleGitDiscard(w http.ResponseWriter, r *http.Request) {
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
		s.logger.Error("failed to encode response", "handler", "git-discard", "err", err)
	}
}

// handleGitUncommit handles POST /api/workspaces/{id}/git-uncommit.
// Resets the HEAD commit, keeping changes as unstaged.
// Requires hash parameter to verify we're uncommitting the expected commit.
func (s *Server) handleGitUncommit(w http.ResponseWriter, r *http.Request) {
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
		s.logger.Error("failed to encode response", "handler", "git-uncommit", "err", err)
	}
}
