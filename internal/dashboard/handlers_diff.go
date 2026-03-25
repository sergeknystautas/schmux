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

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/difftool"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/vcs"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// builtinDiffCommands defines diff commands that are always available,
// matching the BUILTIN_DIFF_COMMANDS constant in the React frontend.
// The backend MUST be the source of truth for what commands can execute.
var builtinDiffCommands = []config.ExternalDiffCommand{
	{Name: "VS Code", Command: `code --diff "$LOCAL" "$REMOTE"`},
}

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

	// Refresh VCS status so the client gets updated stats
	refreshCtx, refreshCancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutMs())*time.Millisecond)
	if _, err := s.workspace.UpdateVCSStatus(refreshCtx, workspaceID); err != nil {
		if errors.Is(err, workspace.ErrWorkspaceLocked) {
			refreshCancel()
			return
		}
		s.logger.Warn("failed to update VCS status", "err", err)
	}
	refreshCancel()

	cb := vcs.NewCommandBuilder(s.vcsTypeForWorkspace(ws))
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutMs())*time.Millisecond)
	defer cancel()
	run := localShellRun(ctx, ws.Path)

	resp, err := buildDiffResponse(run, cb, ws.Path, ws.ID, ws.Repo, ws.Branch)
	if err != nil {
		s.logger.Error("diff failed", "err", err)
		http.Error(w, `{"error":"diff failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode response", "handler", "diff", "err", err)
	}
}

// vcsRunFunc is the function signature for executing a VCS shell command.
// Returns trimmed output and any error (unlike runFunc which has no error return).
type vcsRunFunc = func(string) (string, error)

// buildDiffResponse builds a diff response using VCS-agnostic commands.
// Used by both local and remote diff handlers.
func buildDiffResponse(run vcsRunFunc, cb vcs.CommandBuilder, workspacePath, workspaceID, repo, branch string) (*diffResponse, error) {
	type fileDiff = diffFileDiff

	// Get numstat for file list and line counts
	numstatOutput, _ := run(cb.DiffNumstat())

	files := make([]fileDiff, 0)
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

		// Get old and new content to determine status
		oldContent, _ := run(cb.ShowFile(filePath, "HEAD"))
		oldContent = capContent(oldContent)

		if isBinary {
			status := "modified"
			if oldContent == "" {
				status = "added"
			}
			files = append(files, fileDiff{
				NewPath:  filePath,
				Status:   status,
				IsBinary: true,
			})
			continue
		}

		newContent := readWorkingFile(workspacePath, filePath)

		status := "modified"
		if oldContent == "" {
			status = "added"
		} else if newContent == "" {
			status = "deleted"
		}

		if status == "deleted" {
			files = append(files, fileDiff{
				OldPath:      filePath,
				OldContent:   oldContent,
				Status:       status,
				LinesAdded:   linesAdded,
				LinesRemoved: linesRemoved,
			})
		} else {
			files = append(files, fileDiff{
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
	untrackedOutput, err := run(cb.UntrackedFiles())
	if err == nil {
		for _, filePath := range strings.Split(untrackedOutput, "\n") {
			filePath = strings.TrimSpace(filePath)
			if filePath == "" {
				continue
			}
			if difftool.IsBinaryFile(context.Background(), workspacePath, filePath) {
				files = append(files, fileDiff{
					NewPath:  filePath,
					Status:   "untracked",
					IsBinary: true,
				})
				continue
			}
			newContent := readWorkingFile(workspacePath, filePath)
			lineCount := 0
			if newContent != "" {
				lineCount = strings.Count(newContent, "\n")
				if !strings.HasSuffix(newContent, "\n") {
					lineCount++
				}
			}
			files = append(files, fileDiff{
				NewPath:    filePath,
				NewContent: newContent,
				Status:     "untracked",
				LinesAdded: lineCount,
			})
		}
	}

	return &diffResponse{
		WorkspaceID: workspaceID,
		Repo:        repo,
		Branch:      branch,
		Files:       files,
	}, nil
}

// diffFileDiff is the per-file structure in a diff response.
type diffFileDiff struct {
	OldPath      string `json:"old_path,omitempty"`
	NewPath      string `json:"new_path,omitempty"`
	OldContent   string `json:"old_content,omitempty"`
	NewContent   string `json:"new_content,omitempty"`
	Status       string `json:"status,omitempty"`
	LinesAdded   int    `json:"lines_added"`
	LinesRemoved int    `json:"lines_removed"`
	IsBinary     bool   `json:"is_binary"`
}

// diffResponse is the top-level diff API response.
type diffResponse struct {
	WorkspaceID string         `json:"workspace_id"`
	Repo        string         `json:"repo"`
	Branch      string         `json:"branch"`
	Files       []diffFileDiff `json:"files"`
}

// readWorkingFile reads a file from the working directory with a 1MB cap.
func readWorkingFile(workspacePath, filePath string) string {
	fullPath := filepath.Join(workspacePath, filePath)
	if !isPathWithinDir(fullPath, workspacePath) {
		return ""
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return ""
	}
	const maxContentSize = 1024 * 1024 // 1MB
	if len(content) > maxContentSize {
		content = content[:maxContentSize]
	}
	return string(content)
}

// capContent truncates content to 1MB.
func capContent(s string) string {
	const maxContentSize = 1024 * 1024
	if len(s) > maxContentSize {
		return s[:maxContentSize]
	}
	return s
}

// getFileContent gets file content from a specific VCS revision or worktree.
// For "worktree", it reads from the working directory directly.
// For other values, it uses the workspace's VCS command builder.
func (s *Server) getFileContent(ctx context.Context, workspacePath, filePath, treeish string) string {
	if treeish == "worktree" {
		return readWorkingFile(workspacePath, filePath)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", workspacePath, "show", fmt.Sprintf("%s:%s", treeish, filePath))
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
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
	if !isPathWithinDir(fullPath, ws.Path) {
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

	gitignoreMatches, err := s.fileMatchesVCSIgnore(ctx, ws.Path, filePath, s.vcsTypeForWorkspace(ws))
	if err != nil {
		http.Error(w, "failed to check ignore patterns", http.StatusInternalServerError)
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

// fileMatchesVCSIgnore checks if a file path matches VCS ignore patterns.
func (s *Server) fileMatchesVCSIgnore(ctx context.Context, workspacePath, filePath, vcsType string) (bool, error) {
	cb := vcs.NewCommandBuilder(vcsType)
	run := localShellRun(ctx, workspacePath)
	_, err := run(cb.CheckIgnore(filePath))
	if err == nil {
		// Exit code 0 means the file is ignored
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		// Exit code 1 means the file is not ignored
		if exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
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
	if s.remoteManager == nil {
		http.Error(w, "remote manager not available", http.StatusServiceUnavailable)
		return
	}

	conn := s.remoteManager.GetConnection(ws.RemoteHostID)
	if conn == nil || !conn.IsConnected() {
		http.Error(w, "remote host not connected", http.StatusServiceUnavailable)
		return
	}

	cb := vcs.NewCommandBuilder(s.vcsTypeForWorkspace(ws))

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	workdir := ws.RemotePath
	run := func(cmd string) (string, error) {
		return conn.RunCommand(ctx, workdir, cmd)
	}

	resp, err := buildDiffResponse(run, cb, workdir, ws.ID, ws.Repo, ws.Branch)
	if err != nil {
		s.logger.Error("remote diff failed", "err", err)
		http.Error(w, `{"error":"remote diff failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode response", "handler", "remote-diff", "err", err)
	}
}

// handleOpenVSCode opens VS Code for the specified workspace.
//
// Two modes controlled by ?mode= query parameter:
//   - (default): Executes the "code" command on the server to open VS Code locally.
//   - mode=uri:  Returns a vscode:// URI for opening VS Code from a remote browser
//     via the Remote-SSH extension, without executing anything on the server.
func (s *Server) handleOpenVSCode(w http.ResponseWriter, r *http.Request) {
	// Extract workspace ID from chi wildcard param
	workspaceID := chi.URLParam(r, "*")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	type OpenVSCodeResponse struct {
		Success    bool   `json:"success"`
		Message    string `json:"message"`
		VSCodeURI  string `json:"vscode_uri,omitempty"`
		ServerInfo *struct {
			Hostname        string `json:"hostname,omitempty"`
			WebServerURL    string `json:"web_server_url,omitempty"`
			HasVSCodeServer bool   `json:"has_vscode_server,omitempty"`
			TunnelRunning   bool   `json:"tunnel_running,omitempty"`
		} `json:"server_info,omitempty"`
	}

	uriMode := r.URL.Query().Get("mode") == "uri"

	// Get workspace from state
	ws, found := s.state.GetWorkspace(workspaceID)
	if !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, OpenVSCodeResponse{
			Success: false,
			Message: fmt.Sprintf("workspace %s not found", workspaceID),
		})
		return
	}

	// --- URI mode: return a vscode:// URI for remote browser clients ---
	if uriMode {
		s.handleOpenVSCodeURI(w, ws)
		return
	}

	// --- Local execution mode (original behavior) ---

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
		writeJSON(w, OpenVSCodeResponse{
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
			writeJSON(w, OpenVSCodeResponse{
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
			writeJSON(w, OpenVSCodeResponse{
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
			writeJSON(w, OpenVSCodeResponse{
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
			writeJSON(w, OpenVSCodeResponse{
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
			writeJSON(w, OpenVSCodeResponse{
				Success: false,
				Message: fmt.Sprintf("failed to parse VSCode command: %v", err),
			})
			return
		}
		if len(args) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, OpenVSCodeResponse{
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
			writeJSON(w, OpenVSCodeResponse{
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
		writeJSON(w, OpenVSCodeResponse{
			Success: false,
			Message: fmt.Sprintf("failed to launch VS Code: %v", err),
		})
		return
	}

	// Success response
	writeJSON(w, OpenVSCodeResponse{
		Success: true,
		Message: "You can now switch to VS Code.",
	})
}

// handleOpenVSCodeURI handles the URI mode of the VS Code endpoint.
// It returns a vscode:// URI and server detection info so a remote browser
// can open VS Code with SSH Remote or connect to a running VS Code Server.
func (s *Server) handleOpenVSCodeURI(w http.ResponseWriter, ws state.Workspace) {
	type serverInfoPayload struct {
		Hostname        string `json:"hostname,omitempty"`
		WebServerURL    string `json:"web_server_url,omitempty"`
		HasVSCodeServer bool   `json:"has_vscode_server,omitempty"`
		TunnelRunning   bool   `json:"tunnel_running,omitempty"`
	}

	type OpenVSCodeResponse struct {
		Success    bool               `json:"success"`
		Message    string             `json:"message"`
		VSCodeURI  string             `json:"vscode_uri,omitempty"`
		ServerInfo *serverInfoPayload `json:"server_info,omitempty"`
	}

	var sshHostname string
	var wsPath string

	if ws.IsRemoteWorkspace() {
		// Remote workspace: use the remote host's hostname and remote path
		host, found := s.state.GetRemoteHost(ws.RemoteHostID)
		if !found {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, OpenVSCodeResponse{
				Success: false,
				Message: fmt.Sprintf("remote host %s not found", ws.RemoteHostID),
			})
			return
		}

		// Try live connection if hostname is missing
		if host.Hostname == "" && s.remoteManager != nil {
			if conn := s.remoteManager.GetConnection(ws.RemoteHostID); conn != nil {
				if liveHostname := conn.Hostname(); liveHostname != "" {
					host.Hostname = liveHostname
					s.state.UpdateRemoteHost(conn.Host())
					_ = s.state.Save()
				}
			}
		}

		if host.Hostname == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, OpenVSCodeResponse{
				Success: false,
				Message: "remote host has no hostname",
			})
			return
		}

		sshHostname = host.Hostname
		wsPath = ws.RemotePath
	} else {
		// Local workspace: resolve the server's own hostname
		sshHostname = s.resolveServerHostname()
		if sshHostname == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, OpenVSCodeResponse{
				Success: false,
				Message: "Cannot determine server hostname. Set network.dashboard_hostname in config.",
			})
			return
		}
		wsPath = ws.Path
	}

	// Build the vscode:// URI for SSH Remote
	vsCodeURI := detect.BuildVSCodeRemoteURI(sshHostname, wsPath)
	s.logger.Info("open-vscode (uri mode)", "uri", vsCodeURI, "hostname", sshHostname, "path", wsPath)

	// Detect VS Code server processes on this machine
	serverInfo := detect.DetectVSCodeServer()
	var infoPayload *serverInfoPayload
	if serverInfo.WebServerRunning || serverInfo.TunnelRunning || serverInfo.HasVSCodeServer {
		infoPayload = &serverInfoPayload{
			Hostname:        serverInfo.Hostname,
			HasVSCodeServer: serverInfo.HasVSCodeServer,
			TunnelRunning:   serverInfo.TunnelRunning,
		}
		if serverInfo.WebServerRunning && serverInfo.WebServerPort > 0 {
			infoPayload.WebServerURL = fmt.Sprintf("http://%s:%d", sshHostname, serverInfo.WebServerPort)
		}
	}

	writeJSON(w, OpenVSCodeResponse{
		Success:    true,
		Message:    "Open the VS Code URI to connect remotely.",
		VSCodeURI:  vsCodeURI,
		ServerInfo: infoPayload,
	})
}

// resolveServerHostname returns the SSH-reachable hostname for this server.
// Uses dashboard_hostname from config if set, otherwise falls back to os.Hostname().
func (s *Server) resolveServerHostname() string {
	if h := s.config.GetDashboardHostname(); h != "" {
		return h
	}
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return ""
}

func (s *Server) handleDiffExternal(w http.ResponseWriter, r *http.Request) {
	// Extract workspace ID from chi wildcard param
	workspaceID := chi.URLParam(r, "*")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	type DiffExternalRequest struct {
		Command string `json:"command"` // Command name (looked up in config or built-in list)
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
		writeJSON(w, DiffExternalResponse{
			Success: false,
			Message: fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	// Get the external diff commands from config
	externalDiffCommands := s.config.GetExternalDiffCommands()

	// Find the command to use — only allow commands from config or built-in list.
	// req.Command is a command NAME (not a raw shell string).
	var selectedCommand string
	if req.Command != "" {
		// Search configured commands by name
		for _, cmd := range externalDiffCommands {
			if cmd.Name == req.Command {
				selectedCommand = cmd.Command
				break
			}
		}
		// Search built-in commands by name
		if selectedCommand == "" {
			for _, cmd := range builtinDiffCommands {
				if cmd.Name == req.Command {
					selectedCommand = cmd.Command
					break
				}
			}
		}
		// Reject unknown command names — never use req.Command as a raw shell string
		if selectedCommand == "" {
			s.logger.Warn("diff-external: unknown command name", "name", req.Command)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, DiffExternalResponse{
				Success: false,
				Message: fmt.Sprintf("Unknown diff command: %s", req.Command),
			})
			return
		}
	} else if len(externalDiffCommands) > 0 {
		// No command specified, use the first configured command
		selectedCommand = externalDiffCommands[0].Command
	} else {
		// No command specified and no configured commands
		s.logger.Warn("diff-external: no command specified and no external diff commands configured")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, DiffExternalResponse{
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
		writeJSON(w, DiffExternalResponse{
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
		writeJSON(w, DiffExternalResponse{
			Success: false,
			Message: "workspace directory does not exist",
		})
		return
	}

	// Get changed files using VCS numstat
	cb := vcs.NewCommandBuilder(s.vcsTypeForWorkspace(ws))
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutMs())*time.Millisecond)
	defer cancel()
	run := localShellRun(ctx, ws.Path)

	numstatOutput, _ := run(cb.DiffNumstat())
	output := []byte(numstatOutput)

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
		writeJSON(w, DiffExternalResponse{
			Success: false,
			Message: "No changes to diff",
		})
		return
	}

	s.logger.Info("diff-external: launching", "command", selectedCommand, "files", len(files), "workspace", workspaceID)

	// Parse the base command (before file paths)
	if strings.TrimSpace(selectedCommand) == "" {
		writeJSON(w, DiffExternalResponse{
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
		writeJSON(w, DiffExternalResponse{
			Success: false,
			Message: "Failed to create temp dir for diff",
		})
		return
	}
	opened := 0

	for _, file := range files {
		switch file.status {
		case "modified":
			newPath := filepath.Join(ws.Path, file.path)
			mergedPath := newPath

			// Create temp file for old version
			tmpPath := filepath.Join(tempRoot, file.path)
			if err := os.MkdirAll(filepath.Dir(tmpPath), 0o755); err != nil {
				s.logger.Error("diff-external: failed to create temp dir for file", "err", err)
				continue
			}
			tmpFile, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
			if err != nil {
				s.logger.Error("diff-external: failed to create temp file", "err", err)
				continue
			}

			// Get old file content from VCS
			showOutputStr, err := run(cb.ShowFile(file.path, "HEAD"))
			if err != nil {
				tmpFile.Close()
				os.Remove(tmpPath)
				s.logger.Error("diff-external: failed to get old file", "err", err)
				continue
			}
			showOutput := []byte(showOutputStr)
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
			mergedPath := filepath.Join(ws.Path, file.path)
			tmpPath := filepath.Join(tempRoot, file.path)
			if err := os.MkdirAll(filepath.Dir(tmpPath), 0o755); err != nil {
				s.logger.Error("diff-external: failed to create temp dir for file", "err", err)
				continue
			}
			tmpFile, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
			if err != nil {
				s.logger.Error("diff-external: failed to create temp file", "err", err)
				continue
			}

			showOutputStr, err := run(cb.ShowFile(file.path, "HEAD"))
			if err != nil {
				tmpFile.Close()
				os.Remove(tmpPath)
				s.logger.Error("diff-external: failed to get old file", "err", err)
				continue
			}
			showOutput := []byte(showOutputStr)
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
		writeJSON(w, DiffExternalResponse{
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
	writeJSON(w, DiffExternalResponse{
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
		writeJSON(w, DiffExternalResponse{
			Success: false,
			Message: "remote manager not available",
		})
		return
	}

	conn := s.remoteManager.GetConnection(ws.RemoteHostID)
	if conn == nil || !conn.IsConnected() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, DiffExternalResponse{
			Success: false,
			Message: "remote host not connected",
		})
		return
	}

	cb := vcs.NewCommandBuilder(s.vcsTypeForWorkspace(ws))

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
		writeJSON(w, DiffExternalResponse{
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
		writeJSON(w, DiffExternalResponse{
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
			if err := os.WriteFile(oldPath, []byte(oldContent), 0o600); err != nil {
				continue
			}
			if err := os.WriteFile(newPath, []byte(newContent), 0o600); err != nil {
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
			if err := os.WriteFile(oldPath, []byte(oldContent), 0o600); err != nil {
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
		writeJSON(w, DiffExternalResponse{
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

	writeJSON(w, DiffExternalResponse{
		Success: true,
		Message: fmt.Sprintf("Opened %d files in external diff tool", opened),
	})
}
