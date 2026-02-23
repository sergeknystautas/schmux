package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/difftool"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/vcs"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	// Extract workspace ID from chi URL param
	workspaceID := chi.URLParam(r, "*")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	// Get workspace from state
	ws, found := s.state.GetWorkspace(workspaceID)
	if !found {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}

	// Delegate to remote handler if this is a remote workspace
	if ws.RemoteHostID != "" {
		s.handleRemoteDiff(w, r, ws)
		return
	}

	// Refresh git status so the client gets updated stats
	refreshCtx, refreshCancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutMs())*time.Millisecond)
	if _, err := s.workspace.UpdateGitStatus(refreshCtx, workspaceID); err != nil {
		if errors.Is(err, workspace.ErrWorkspaceLocked) {
			refreshCancel()
			return
		}
		s.logger.Warn("failed to update git status", "err", err)
	}
	refreshCancel()

	// Run git diff in workspace directory
	type FileDiff struct {
		OldPath      string `json:"old_path,omitempty"`
		NewPath      string `json:"new_path,omitempty"`
		OldContent   string `json:"old_content,omitempty"`
		NewContent   string `json:"new_content,omitempty"`
		Status       string `json:"status,omitempty"` // added, modified, deleted, renamed
		LinesAdded   int    `json:"lines_added"`
		LinesRemoved int    `json:"lines_removed"`
		IsBinary     bool   `json:"is_binary"`
	}

	type DiffResponse struct {
		WorkspaceID string     `json:"workspace_id"`
		Repo        string     `json:"repo"`
		Branch      string     `json:"branch"`
		Files       []FileDiff `json:"files"`
	}

	// Step 1: Get file status from git (A=added, M=modified, D=deleted, R=renamed)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutMs())*time.Millisecond)
	statusCmd := exec.CommandContext(ctx, "git", "-C", ws.Path, "diff", "HEAD", "--name-status", "--find-renames")
	statusOutput, _ := statusCmd.Output()
	cancel()

	// Build map of filepath -> status
	fileStatus := make(map[string]string)
	for _, line := range strings.Split(string(statusOutput), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		statusCode := parts[0]
		filePath := parts[len(parts)-1] // Last part is the (new) filepath

		switch {
		case statusCode == "A":
			fileStatus[filePath] = "added"
		case statusCode == "D":
			fileStatus[filePath] = "deleted"
		case statusCode == "M":
			fileStatus[filePath] = "modified"
		case strings.HasPrefix(statusCode, "R"):
			fileStatus[filePath] = "renamed"
		default:
			fileStatus[filePath] = "modified"
		}
	}

	// Step 2: Get line counts from numstat
	ctx, cancel = context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutMs())*time.Millisecond)
	numstatCmd := exec.CommandContext(ctx, "git", "-C", ws.Path, "diff", "HEAD", "--numstat", "--find-renames")
	numstatOutput, _ := numstatCmd.Output()
	cancel()

	// Step 3: Parse numstat and build file diffs, using status from step 1
	files := make([]FileDiff, 0)
	for _, line := range strings.Split(string(numstatOutput), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}

		addedStr := parts[0]
		deletedStr := parts[1]
		filePath := parts[2]

		// Parse line counts (may be "-" for binary files)
		isBinary := addedStr == "-" && deletedStr == "-"
		linesAdded := 0
		linesRemoved := 0
		if addedStr != "-" {
			linesAdded, _ = strconv.Atoi(addedStr)
		}
		if deletedStr != "-" {
			linesRemoved, _ = strconv.Atoi(deletedStr)
		}

		// Get status from name-status output
		status := fileStatus[filePath]
		if status == "" {
			status = "modified"
		}

		if isBinary {
			if status == "deleted" {
				files = append(files, FileDiff{
					OldPath:  filePath,
					Status:   status,
					IsBinary: true,
				})
			} else {
				files = append(files, FileDiff{
					NewPath:  filePath,
					Status:   status,
					IsBinary: true,
				})
			}
			continue
		}

		// Get file content for non-binary files
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutMs())*time.Millisecond)
		var oldContent, newContent string
		if status == "deleted" {
			oldContent = s.getFileContent(ctx, ws.Path, filePath, "HEAD")
		} else if status == "added" {
			newContent = s.getFileContent(ctx, ws.Path, filePath, "worktree")
		} else {
			oldContent = s.getFileContent(ctx, ws.Path, filePath, "HEAD")
			newContent = s.getFileContent(ctx, ws.Path, filePath, "worktree")
		}
		cancel()

		if status == "deleted" {
			files = append(files, FileDiff{
				OldPath:      filePath,
				OldContent:   oldContent,
				Status:       status,
				LinesAdded:   linesAdded,
				LinesRemoved: linesRemoved,
			})
		} else {
			files = append(files, FileDiff{
				NewPath:      filePath,
				OldContent:   oldContent,
				NewContent:   newContent,
				Status:       status,
				LinesAdded:   linesAdded,
				LinesRemoved: linesRemoved,
			})
		}
	}

	// Get untracked files
	// ls-files --others --exclude-standard lists untracked files (respecting .gitignore)
	ctx, cancel = context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutMs())*time.Millisecond)
	untrackedCmd := exec.CommandContext(ctx, "git", "-C", ws.Path, "ls-files", "--others", "--exclude-standard")
	untrackedOutput, err := untrackedCmd.Output()
	cancel()
	if err == nil {
		untrackedLines := strings.Split(string(untrackedOutput), "\n")
		for _, filePath := range untrackedLines {
			if filePath == "" {
				continue
			}
			// Check if file is binary using git's detection (with fast heuristic fallback)
			if difftool.IsBinaryFile(ctx, ws.Path, filePath) {
				files = append(files, FileDiff{
					NewPath:  filePath,
					Status:   "untracked",
					IsBinary: true,
				})
				continue
			}
			// Get content of untracked file from working directory
			newContent := s.getFileContent(ctx, ws.Path, filePath, "worktree")
			// Count lines for untracked files (all lines are additions)
			lineCount := 0
			if newContent != "" {
				lineCount = strings.Count(newContent, "\n")
				if !strings.HasSuffix(newContent, "\n") {
					lineCount++ // Count last line if no trailing newline
				}
			}
			files = append(files, FileDiff{
				NewPath:    filePath,
				NewContent: newContent,
				Status:     "untracked",
				LinesAdded: lineCount,
			})
		}
	}

	response := DiffResponse{
		WorkspaceID: workspaceID,
		Repo:        ws.Repo,
		Branch:      ws.Branch,
		Files:       files,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// getFileContent gets file content from a specific git tree-ish.
// For "worktree", it reads from the working directory directly.
func (s *Server) getFileContent(ctx context.Context, workspacePath, filePath, treeish string) string {
	if treeish == "worktree" {
		fullPath := filepath.Join(workspacePath, filePath)
		if !strings.HasPrefix(filepath.Clean(fullPath), filepath.Clean(workspacePath)+string(filepath.Separator)) && filepath.Clean(fullPath) != filepath.Clean(workspacePath) {
			return ""
		}
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return ""
		}
		// Cap file content to prevent loading massive files into memory
		const maxContentSize = 1024 * 1024 // 1MB
		if len(content) > maxContentSize {
			content = content[:maxContentSize]
		}
		return string(content)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", workspacePath, "show", fmt.Sprintf("%s:%s", treeish, filePath))
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	// Cap git show output too
	const maxContentSize = 1024 * 1024 // 1MB
	if len(output) > maxContentSize {
		output = output[:maxContentSize]
	}
	return string(output)
}

// handleFile serves raw file content from a workspace for image previews.
// Path format: /api/file/{workspaceId}/...
// Security: only allows image files, blocks path traversal, checks .gitignore.
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {

	// Extract workspace ID and file path from chi wildcard param
	trimmedPath := chi.URLParam(r, "*")
	if trimmedPath == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}
	slashIdx := strings.Index(trimmedPath, "/")
	if slashIdx <= 0 {
		http.Error(w, "invalid path format", http.StatusBadRequest)
		return
	}
	workspaceID := trimmedPath[:slashIdx]
	filePath := trimmedPath[slashIdx+1:]
	filePath, err := url.QueryUnescape(filePath)
	if err != nil {
		http.Error(w, "invalid file path", http.StatusBadRequest)
		return
	}

	// Validate workspace ID
	if !isValidResourceID(workspaceID) {
		http.Error(w, "invalid workspace ID", http.StatusBadRequest)
		return
	}

	// Get workspace from state
	ws, found := s.state.GetWorkspace(workspaceID)
	if !found {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}

	// Delegate to remote handler if this is a remote workspace
	if ws.RemoteHostID != "" {
		s.handleRemoteFile(w, r, ws, filePath)
		return
	}

	s.serveWorkspaceFile(w, r, ws, filePath)
}

// serveWorkspaceFile serves a file from a local workspace with security checks.
func (s *Server) serveWorkspaceFile(w http.ResponseWriter, r *http.Request, ws state.Workspace, filePath string) {
	// Validate file path - block path traversal
	fullPath := filepath.Join(ws.Path, filePath)
	cleanFullPath := filepath.Clean(fullPath)
	if !strings.HasPrefix(cleanFullPath, filepath.Clean(ws.Path)+string(filepath.Separator)) && cleanFullPath != filepath.Clean(ws.Path) {
		http.Error(w, "invalid file path", http.StatusForbidden)
		return
	}

	// Check file exists
	info, err := os.Stat(cleanFullPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		http.Error(w, "cannot access file", http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		http.Error(w, "cannot serve directory", http.StatusForbidden)
		return
	}

	// Only allow image files
	ext := strings.ToLower(filepath.Ext(filePath))
	allowedExts := map[string]string{
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".webp": "image/webp",
		".gif":  "image/gif",
	}
	contentType, allowed := allowedExts[ext]
	if !allowed {
		http.Error(w, "only image files are allowed", http.StatusForbidden)
		return
	}

	// Check .gitignore - load gitignore patterns and check if file matches
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	gitignoreMatches, err := s.fileMatchesGitignore(ctx, ws.Path, filePath)
	if err != nil {
		http.Error(w, "failed to check gitignore", http.StatusInternalServerError)
		return
	}
	if gitignoreMatches {
		http.Error(w, "file is ignored by git", http.StatusForbidden)
		return
	}

	// Serve the file
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, cleanFullPath)
}

// fileMatchesGitignore checks if a file path matches any .gitignore pattern.
func (s *Server) fileMatchesGitignore(ctx context.Context, workspacePath, filePath string) (bool, error) {
	// Use git check-ignore to check if file is ignored
	cmd := exec.CommandContext(ctx, "git", "-C", workspacePath, "check-ignore", "-q", filePath)
	err := cmd.Run()
	if err == nil {
		// Exit code 0 means the file is ignored
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		// Exit code 1 means the file is not ignored
		if exitErr.ExitCode() == 1 {
			return false, nil
		}
		// Other exit codes indicate errors
		return false, err
	}
	// Any other error means we can't determine - treat as not ignored for safety
	return false, nil
}

// handleRemoteFile handles file requests for remote workspaces.
func (s *Server) handleRemoteFile(w http.ResponseWriter, r *http.Request, ws state.Workspace, filePath string) {
	// For remote workspaces, we need to fetch the file via remote command
	// This is a simplified implementation - in production you'd use the remote connection
	http.Error(w, "remote file preview not yet supported", http.StatusNotImplemented)
}

// handleRemoteDiff handles diff requests for remote workspaces by executing VCS
// commands on the remote host via tmux control mode.
func (s *Server) handleRemoteDiff(w http.ResponseWriter, r *http.Request, ws state.Workspace) {
	type FileDiff struct {
		OldPath      string `json:"old_path,omitempty"`
		NewPath      string `json:"new_path,omitempty"`
		OldContent   string `json:"old_content,omitempty"`
		NewContent   string `json:"new_content,omitempty"`
		Status       string `json:"status,omitempty"`
		LinesAdded   int    `json:"lines_added"`
		LinesRemoved int    `json:"lines_removed"`
		IsBinary     bool   `json:"is_binary"`
	}
	type DiffResponse struct {
		WorkspaceID string     `json:"workspace_id"`
		Repo        string     `json:"repo"`
		Branch      string     `json:"branch"`
		Files       []FileDiff `json:"files"`
	}

	if s.remoteManager == nil {
		http.Error(w, "remote manager not available", http.StatusServiceUnavailable)
		return
	}

	conn := s.remoteManager.GetConnection(ws.RemoteHostID)
	if conn == nil || !conn.IsConnected() {
		http.Error(w, "remote host not connected", http.StatusServiceUnavailable)
		return
	}

	// Get VCS type from flavor config
	host, _ := s.state.GetRemoteHost(ws.RemoteHostID)
	vcsType := ""
	if host.FlavorID != "" {
		if flavor, found := s.config.GetRemoteFlavor(host.FlavorID); found {
			vcsType = flavor.VCS
		}
	}
	cb := vcs.NewCommandBuilder(vcsType)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	workdir := ws.RemotePath

	// Run diff numstat
	numstatOutput, err := conn.RunCommand(ctx, workdir, cb.DiffNumstat())
	if err != nil {
		numstatOutput = "" // No changes is ok
	}

	files := make([]FileDiff, 0)
	for _, line := range strings.Split(numstatOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}

		addedStr := parts[0]
		deletedStr := parts[1]
		filePath := parts[2]

		isBinary := addedStr == "-" && deletedStr == "-"
		linesAdded := 0
		linesRemoved := 0
		if addedStr != "-" {
			linesAdded, _ = strconv.Atoi(addedStr)
		}
		if deletedStr != "-" {
			linesRemoved, _ = strconv.Atoi(deletedStr)
		}

		if isBinary {
			status := "modified"
			oldContent, _ := conn.RunCommand(ctx, workdir, cb.ShowFile(filePath, "HEAD"))
			if oldContent == "" {
				status = "added"
			}
			files = append(files, FileDiff{
				NewPath:  filePath,
				Status:   status,
				IsBinary: true,
			})
			continue
		}

		// Get file contents
		oldContent, _ := conn.RunCommand(ctx, workdir, cb.ShowFile(filePath, "HEAD"))
		newContent, _ := conn.RunCommand(ctx, workdir, cb.FileContent(filePath))

		status := "modified"
		if oldContent == "" {
			status = "added"
		}

		files = append(files, FileDiff{
			NewPath:      filePath,
			OldContent:   oldContent,
			NewContent:   newContent,
			Status:       status,
			LinesAdded:   linesAdded,
			LinesRemoved: linesRemoved,
		})
	}

	// Get untracked files
	untrackedOutput, err := conn.RunCommand(ctx, workdir, cb.UntrackedFiles())
	if err == nil {
		for _, filePath := range strings.Split(untrackedOutput, "\n") {
			filePath = strings.TrimSpace(filePath)
			if filePath == "" {
				continue
			}
			newContent, _ := conn.RunCommand(ctx, workdir, cb.FileContent(filePath))
			lineCount := 0
			if newContent != "" {
				lineCount = strings.Count(newContent, "\n")
				if !strings.HasSuffix(newContent, "\n") {
					lineCount++
				}
			}
			files = append(files, FileDiff{
				NewPath:    filePath,
				NewContent: newContent,
				Status:     "untracked",
				LinesAdded: lineCount,
			})
		}
	}

	response := DiffResponse{
		WorkspaceID: ws.ID,
		Repo:        ws.Repo,
		Branch:      ws.Branch,
		Files:       files,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleOpenVSCode opens VS Code in a new window for the specified workspace.
func (s *Server) handleOpenVSCode(w http.ResponseWriter, r *http.Request) {
	// Extract workspace ID from chi wildcard param
	workspaceID := chi.URLParam(r, "*")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	type OpenVSCodeResponse struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}

	// Get workspace from state
	ws, found := s.state.GetWorkspace(workspaceID)
	if !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(OpenVSCodeResponse{
			Success: false,
			Message: fmt.Sprintf("workspace %s not found", workspaceID),
		})
		return
	}

	// Use ResolveVSCodePath to find VS Code command
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	vscodePath, found := detect.ResolveVSCodePath(ctx)
	if !found {
		s.logger.Warn("open-vscode: command not found")
		// Determine platform-specific keyboard shortcut
		var shortcut string
		if runtime.GOOS == "darwin" {
			shortcut = "Cmd+Shift+P"
		} else {
			shortcut = "Ctrl+Shift+P"
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(OpenVSCodeResponse{
			Success: false,
			Message: fmt.Sprintf("VS Code command not found\n\nTo fix this:\nOpen VS Code, press %s, then run: Shell Command: Install 'code' command in PATH", shortcut),
		})
		return
	}

	s.logger.Info("open-vscode: found", "source", vscodePath.Source, "path", vscodePath.Path)

	var cmd *exec.Cmd

	// Check if this is a remote workspace
	if ws.IsRemoteWorkspace() {
		// Handle remote workspace - use configured template
		host, found := s.state.GetRemoteHost(ws.RemoteHostID)
		if !found {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(OpenVSCodeResponse{
				Success: false,
				Message: fmt.Sprintf("remote host %s not found", ws.RemoteHostID),
			})
			return
		}

		// If hostname is missing from state, try the live connection
		if host.Hostname == "" && s.remoteManager != nil {
			if conn := s.remoteManager.GetConnection(ws.RemoteHostID); conn != nil {
				if liveHostname := conn.Hostname(); liveHostname != "" {
					host.Hostname = liveHostname
					// Persist back to state so future lookups have it
					s.state.UpdateRemoteHost(conn.Host())
					if err := s.state.Save(); err != nil {
						s.logger.Error("failed to save remote host state", "err", err)
					}
				}
			}
		}

		if host.Hostname == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(OpenVSCodeResponse{
				Success: false,
				Message: "remote host has no hostname",
			})
			return
		}

		// Get VSCode command template - prefer flavor-specific template over global
		templateStr := ""
		if host.FlavorID != "" {
			if flavor, found := s.config.GetRemoteFlavor(host.FlavorID); found && flavor.VSCodeCommandTemplate != "" {
				templateStr = flavor.VSCodeCommandTemplate
				s.logger.Info("open-vscode: using flavor-specific template", "flavor", flavor.DisplayName)
			}
		}
		// Fall back to global template if no flavor-specific template
		if templateStr == "" {
			templateStr = s.config.GetRemoteVSCodeCommandTemplate()
			s.logger.Info("open-vscode: using global template")
		}

		// Parse template
		tmpl, err := template.New("vscode").Parse(templateStr)
		if err != nil {
			s.logger.Error("open-vscode: template parse error", "err", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(OpenVSCodeResponse{
				Success: false,
				Message: fmt.Sprintf("invalid VSCode command template: %v", err),
			})
			return
		}

		// Execute template with data
		type VSCodeTemplateData struct {
			Hostname   string
			Path       string
			VSCodePath string
		}

		data := VSCodeTemplateData{
			Hostname:   host.Hostname,
			Path:       ws.RemotePath,
			VSCodePath: vscodePath.Path,
		}

		var cmdStr strings.Builder
		if err := tmpl.Execute(&cmdStr, data); err != nil {
			s.logger.Error("open-vscode: template execution error", "err", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(OpenVSCodeResponse{
				Success: false,
				Message: fmt.Sprintf("failed to execute VSCode command template: %v", err),
			})
			return
		}

		// Parse the command string into args using shell word parsing
		// This respects quotes and handles spaces in paths correctly
		cmdLine := cmdStr.String()
		args, err := shellSplit(cmdLine)
		if err != nil {
			s.logger.Error("open-vscode: failed to parse command", "err", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(OpenVSCodeResponse{
				Success: false,
				Message: fmt.Sprintf("failed to parse VSCode command: %v", err),
			})
			return
		}
		if len(args) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(OpenVSCodeResponse{
				Success: false,
				Message: "VSCode command template produced empty command",
			})
			return
		}

		s.logger.Info("open-vscode (remote): executing", "command", cmdLine)
		cmd = exec.Command(args[0], args[1:]...)

	} else {
		// Local workspace - check if directory exists
		if _, err := os.Stat(ws.Path); os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(OpenVSCodeResponse{
				Success: false,
				Message: "workspace directory does not exist",
			})
			return
		}

		s.logger.Info("open-vscode (local)", "path", ws.Path)
		cmd = exec.Command(vscodePath.Path, "-n", ws.Path)
	}

	// Execute command
	// Note: We don't wait for the command to complete since VS Code opens as a separate process
	if err := cmd.Start(); err != nil {
		s.logger.Error("open-vscode: failed to launch", "err", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(OpenVSCodeResponse{
			Success: false,
			Message: fmt.Sprintf("failed to launch VS Code: %v", err),
		})
		return
	}

	// Success response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OpenVSCodeResponse{
		Success: true,
		Message: "You can now switch to VS Code.",
	})
}

func (s *Server) handleDiffExternal(w http.ResponseWriter, r *http.Request) {
	// Extract workspace ID from chi wildcard param
	workspaceID := chi.URLParam(r, "*")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	type DiffExternalRequest struct {
		Command string `json:"command"` // Can be a command name from config, or a raw command string
	}

	type DiffExternalResponse struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}

	// Parse request body to get command name
	var req DiffExternalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		s.logger.Error("diff-external: failed to decode request", "err", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(DiffExternalResponse{
			Success: false,
			Message: fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	// Get the external diff commands from config
	externalDiffCommands := s.config.GetExternalDiffCommands()

	// Find the command to use
	var selectedCommand string
	if req.Command != "" {
		// First, try to find the command by name in the config
		for _, cmd := range externalDiffCommands {
			if cmd.Name == req.Command {
				selectedCommand = cmd.Command
				break
			}
		}
		// If not found in config, use the command string directly (for built-in commands)
		if selectedCommand == "" {
			selectedCommand = req.Command
		}
	} else if len(externalDiffCommands) > 0 {
		// No command specified, use the first configured command
		selectedCommand = externalDiffCommands[0].Command
	} else {
		// No command specified and no configured commands
		s.logger.Warn("diff-external: no command specified and no external diff commands configured")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(DiffExternalResponse{
			Success: false,
			Message: "No diff command specified",
		})
		return
	}

	// Get workspace from state
	ws, found := s.state.GetWorkspace(workspaceID)
	if !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(DiffExternalResponse{
			Success: false,
			Message: fmt.Sprintf("workspace %s not found", workspaceID),
		})
		return
	}

	// Delegate to remote handler if this is a remote workspace
	if ws.RemoteHostID != "" {
		s.handleRemoteDiffExternal(w, r, ws, selectedCommand)
		return
	}

	// Check if workspace directory exists
	if _, err := os.Stat(ws.Path); os.IsNotExist(err) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(DiffExternalResponse{
			Success: false,
			Message: "workspace directory does not exist",
		})
		return
	}

	// Get changed files using git diff --numstat
	// HEAD compares against last commit (includes both staged and unstaged)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutMs())*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", ws.Path, "diff", "HEAD", "--numstat", "--find-renames", "--diff-filter=ADM")
	output, err := cmd.Output()
	if err != nil {
		output = []byte{}
	}

	type changedFile struct {
		path   string
		status string // added, modified, deleted, renamed
	}

	files := make([]changedFile, 0)
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		added := parts[0]
		deleted := parts[1]
		filePath := parts[2]

		status := "modified"
		if added == "-" && deleted == "-" {
			// Binary file or special case
			status = "modified"
		} else if added == "0" && deleted != "0" {
			status = "deleted"
		} else if added != "0" && deleted == "0" {
			status = "added"
		}

		files = append(files, changedFile{path: filePath, status: status})
	}

	if len(files) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DiffExternalResponse{
			Success: false,
			Message: "No changes to diff",
		})
		return
	}

	s.logger.Info("diff-external: launching", "command", selectedCommand, "files", len(files), "workspace", workspaceID)

	// Parse the base command (before file paths)
	if strings.TrimSpace(selectedCommand) == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DiffExternalResponse{
			Success: false,
			Message: "Invalid command",
		})
		return
	}

	replacePlaceholders := func(cmd, oldPath, newPath, filePath string) string {
		cmd = strings.ReplaceAll(cmd, "{old_file}", oldPath)
		cmd = strings.ReplaceAll(cmd, "{new_file}", newPath)
		cmd = strings.ReplaceAll(cmd, "{file}", filePath)
		return cmd
	}

	tempRoot, err := difftool.TempDirForWorkspace(workspaceID)
	if err != nil {
		s.logger.Error("diff-external: failed to create temp dir", "err", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DiffExternalResponse{
			Success: false,
			Message: "Failed to create temp dir for diff",
		})
		return
	}
	opened := 0

	for _, file := range files {
		switch file.status {
		case "modified":
			oldPath := fmt.Sprintf("HEAD:%s", file.path)
			newPath := filepath.Join(ws.Path, file.path)
			mergedPath := newPath

			// Create temp file for old version
			tmpPath := filepath.Join(tempRoot, file.path)
			if err := os.MkdirAll(filepath.Dir(tmpPath), 0o755); err != nil {
				s.logger.Error("diff-external: failed to create temp dir for file", "err", err)
				continue
			}
			tmpFile, err := os.Create(tmpPath)
			if err != nil {
				s.logger.Error("diff-external: failed to create temp file", "err", err)
				continue
			}

			// Get old file content from git
			showCmd := exec.CommandContext(ctx, "git", "-C", ws.Path, "show", oldPath)
			showOutput, err := showCmd.Output()
			if err != nil {
				tmpFile.Close()
				os.Remove(tmpPath)
				s.logger.Error("diff-external: failed to get old file", "err", err)
				continue
			}
			if _, err := tmpFile.Write(showOutput); err != nil {
				tmpFile.Close()
				os.Remove(tmpPath)
				s.logger.Error("diff-external: failed to write temp file", "err", err)
				continue
			}
			tmpFile.Close()

			cmdString := replacePlaceholders(selectedCommand, tmpPath, newPath, newPath)
			execCmd := exec.Command("sh", "-c", cmdString)
			execCmd.Dir = ws.Path
			execCmd.Env = append(os.Environ(),
				fmt.Sprintf("LOCAL=%s", tmpPath),
				fmt.Sprintf("REMOTE=%s", newPath),
				fmt.Sprintf("MERGED=%s", mergedPath),
				fmt.Sprintf("BASE=%s", mergedPath),
			)
			if err := execCmd.Start(); err != nil {
				s.logger.Error("diff-external: diff tool exited with error", "err", err)
			} else {
				go func() { _ = execCmd.Wait() }()
				opened++
			}

		case "deleted":
			oldPath := fmt.Sprintf("HEAD:%s", file.path)
			mergedPath := filepath.Join(ws.Path, file.path)
			tmpPath := filepath.Join(tempRoot, file.path)
			if err := os.MkdirAll(filepath.Dir(tmpPath), 0o755); err != nil {
				s.logger.Error("diff-external: failed to create temp dir for file", "err", err)
				continue
			}
			tmpFile, err := os.Create(tmpPath)
			if err != nil {
				s.logger.Error("diff-external: failed to create temp file", "err", err)
				continue
			}

			showCmd := exec.CommandContext(ctx, "git", "-C", ws.Path, "show", oldPath)
			showOutput, err := showCmd.Output()
			if err != nil {
				tmpFile.Close()
				os.Remove(tmpPath)
				s.logger.Error("diff-external: failed to get old file", "err", err)
				continue
			}
			if _, err := tmpFile.Write(showOutput); err != nil {
				tmpFile.Close()
				os.Remove(tmpPath)
				s.logger.Error("diff-external: failed to write temp file", "err", err)
				continue
			}
			tmpFile.Close()

			cmdString := replacePlaceholders(selectedCommand, tmpPath, "", mergedPath)
			execCmd := exec.Command("sh", "-c", cmdString)
			execCmd.Dir = ws.Path
			execCmd.Env = append(os.Environ(),
				fmt.Sprintf("LOCAL=%s", tmpPath),
				fmt.Sprintf("REMOTE="),
				fmt.Sprintf("MERGED=%s", mergedPath),
				fmt.Sprintf("BASE=%s", mergedPath),
			)
			if err := execCmd.Start(); err != nil {
				s.logger.Error("diff-external: diff tool exited with error", "err", err)
			} else {
				go func() { _ = execCmd.Wait() }()
				opened++
			}

		case "added":
			// Skip new/untracked files (git difftool doesn't include them)
			continue
		}
	}

	if opened == 0 {
		os.RemoveAll(tempRoot)
		// No files were added (all were new/untracked)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DiffExternalResponse{
			Success: false,
			Message: "No modified or deleted files to diff",
		})
		return
	}

	cleanupDelay := time.Duration(s.config.GetExternalDiffCleanupAfterMs()) * time.Millisecond
	time.AfterFunc(cleanupDelay, func() {
		if err := os.RemoveAll(tempRoot); err != nil {
			s.logger.Error("diff-external: failed to remove temp dir", "err", err)
		}
	})

	// Success response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(DiffExternalResponse{
		Success: true,
		Message: fmt.Sprintf("Opened %d files in external diff tool", opened),
	})
}

// handleRemoteDiffExternal handles external diff tool requests for remote workspaces.
// It fetches file contents from the remote host, writes them to local temp files,
// and launches the diff tool with those temp files.
func (s *Server) handleRemoteDiffExternal(w http.ResponseWriter, r *http.Request, ws state.Workspace, selectedCommand string) {
	type DiffExternalResponse struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}

	if s.remoteManager == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(DiffExternalResponse{
			Success: false,
			Message: "remote manager not available",
		})
		return
	}

	conn := s.remoteManager.GetConnection(ws.RemoteHostID)
	if conn == nil || !conn.IsConnected() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(DiffExternalResponse{
			Success: false,
			Message: "remote host not connected",
		})
		return
	}

	// Get VCS type from flavor config
	host, _ := s.state.GetRemoteHost(ws.RemoteHostID)
	vcsType := ""
	if host.FlavorID != "" {
		if flavor, found := s.config.GetRemoteFlavor(host.FlavorID); found {
			vcsType = flavor.VCS
		}
	}
	cb := vcs.NewCommandBuilder(vcsType)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	workdir := ws.RemotePath

	// Get changed files using VCS diff numstat
	numstatOutput, err := conn.RunCommand(ctx, workdir, cb.DiffNumstat())
	if err != nil {
		numstatOutput = ""
	}

	type changedFile struct {
		path   string
		status string
	}

	files := make([]changedFile, 0)
	for _, line := range strings.Split(numstatOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		addedStr := parts[0]
		deletedStr := parts[1]
		filePath := parts[2]

		if addedStr == "-" && deletedStr == "-" {
			continue // Skip binary files
		}

		status := "modified"
		if addedStr != "0" && deletedStr == "0" {
			status = "added"
		} else if addedStr == "0" && deletedStr != "0" {
			status = "deleted"
		}

		files = append(files, changedFile{path: filePath, status: status})
	}

	if len(files) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DiffExternalResponse{
			Success: false,
			Message: "No changes to diff",
		})
		return
	}

	s.logger.Info("diff-external (remote): launching", "command", selectedCommand, "files", len(files), "workspace", ws.ID)

	replacePlaceholders := func(cmd, oldPath, newPath, filePath string) string {
		cmd = strings.ReplaceAll(cmd, "{old_file}", oldPath)
		cmd = strings.ReplaceAll(cmd, "{new_file}", newPath)
		cmd = strings.ReplaceAll(cmd, "{file}", filePath)
		return cmd
	}

	tempRoot, err := difftool.TempDirForWorkspace(ws.ID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DiffExternalResponse{
			Success: false,
			Message: "Failed to create temp dir for diff",
		})
		return
	}

	opened := 0
	for _, file := range files {
		switch file.status {
		case "modified":
			// Fetch both old and new content from remote
			oldContent, err := conn.RunCommand(ctx, workdir, cb.ShowFile(file.path, "HEAD"))
			if err != nil {
				s.logger.Error("diff-external (remote): failed to get old file", "file", file.path, "err", err)
				continue
			}
			newContent, err := conn.RunCommand(ctx, workdir, cb.FileContent(file.path))
			if err != nil {
				s.logger.Error("diff-external (remote): failed to get new file", "file", file.path, "err", err)
				continue
			}

			oldPath := filepath.Join(tempRoot, "old", file.path)
			newPath := filepath.Join(tempRoot, "new", file.path)

			if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
				continue
			}
			if err := os.WriteFile(oldPath, []byte(oldContent), 0o644); err != nil {
				continue
			}
			if err := os.WriteFile(newPath, []byte(newContent), 0o644); err != nil {
				continue
			}

			cmdString := replacePlaceholders(selectedCommand, oldPath, newPath, newPath)
			execCmd := exec.Command("sh", "-c", cmdString)
			execCmd.Env = append(os.Environ(),
				fmt.Sprintf("LOCAL=%s", oldPath),
				fmt.Sprintf("REMOTE=%s", newPath),
				fmt.Sprintf("MERGED=%s", newPath),
				fmt.Sprintf("BASE=%s", newPath),
			)
			if err := execCmd.Start(); err != nil {
				s.logger.Error("diff-external (remote): diff tool error", "err", err)
			} else {
				go func() { _ = execCmd.Wait() }()
				opened++
			}

		case "deleted":
			oldContent, err := conn.RunCommand(ctx, workdir, cb.ShowFile(file.path, "HEAD"))
			if err != nil {
				continue
			}

			oldPath := filepath.Join(tempRoot, "old", file.path)
			if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
				continue
			}
			if err := os.WriteFile(oldPath, []byte(oldContent), 0o644); err != nil {
				continue
			}

			cmdString := replacePlaceholders(selectedCommand, oldPath, "", filepath.Join(workdir, file.path))
			execCmd := exec.Command("sh", "-c", cmdString)
			execCmd.Env = append(os.Environ(),
				fmt.Sprintf("LOCAL=%s", oldPath),
				"REMOTE=",
				fmt.Sprintf("MERGED=%s", filepath.Join(workdir, file.path)),
				fmt.Sprintf("BASE=%s", filepath.Join(workdir, file.path)),
			)
			if err := execCmd.Start(); err != nil {
				s.logger.Error("diff-external (remote): diff tool error", "err", err)
			} else {
				go func() { _ = execCmd.Wait() }()
				opened++
			}

		case "added":
			continue
		}
	}

	if opened == 0 {
		os.RemoveAll(tempRoot)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DiffExternalResponse{
			Success: false,
			Message: "No modified or deleted files to diff",
		})
		return
	}

	cleanupDelay := time.Duration(s.config.GetExternalDiffCleanupAfterMs()) * time.Millisecond
	time.AfterFunc(cleanupDelay, func() {
		if err := os.RemoveAll(tempRoot); err != nil {
			s.logger.Error("diff-external (remote): failed to remove temp dir", "err", err)
		}
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(DiffExternalResponse{
		Success: true,
		Message: fmt.Sprintf("Opened %d files in external diff tool", opened),
	})
}
