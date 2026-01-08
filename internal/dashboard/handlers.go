package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// handleIndex serves the React app entry point.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.handleApp(w, r)
}

// handleSessionsList serves the React app entry point.
func (s *Server) handleSessionsList(w http.ResponseWriter, r *http.Request) {
	s.handleApp(w, r)
}

// handleSpawn serves the React app entry point.
func (s *Server) handleSpawn(w http.ResponseWriter, r *http.Request) {
	s.handleApp(w, r)
}

// handleWorkspaces serves the React app entry point.
func (s *Server) handleWorkspaces(w http.ResponseWriter, r *http.Request) {
	s.handleApp(w, r)
}

// handleTips serves the React app entry point.
func (s *Server) handleTips(w http.ResponseWriter, r *http.Request) {
	s.handleApp(w, r)
}

// handleSessionDetail serves the React app entry point.
func (s *Server) handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	s.handleApp(w, r)
}

// handleTerminalHTML serves the React app entry point.
func (s *Server) handleTerminalHTML(w http.ResponseWriter, r *http.Request) {
	s.handleApp(w, r)
}

// handleApp serves the React application entry point for UI routes.
func (s *Server) handleApp(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
		http.NotFound(w, r)
		return
	}

	// Serve static files at root (e.g., favicon.ico) if they exist in dist.
	if path.Ext(r.URL.Path) != "" {
		if s.serveFileIfExists(w, r, r.URL.Path) {
			return
		}
	}

	s.serveAppIndex(w, r)
}

func (s *Server) serveFileIfExists(w http.ResponseWriter, r *http.Request, requestPath string) bool {
	distPath := s.getDashboardDistPath()
	cleanPath := filepath.Clean(strings.TrimPrefix(requestPath, "/"))
	if strings.HasPrefix(cleanPath, "..") {
		return false
	}
	filePath := filepath.Join(distPath, cleanPath)
	if _, err := os.Stat(filePath); err == nil {
		http.ServeFile(w, r, filePath)
		return true
	}
	return false
}

// serveAppIndex serves the built React index.html from the dist directory.
func (s *Server) serveAppIndex(w http.ResponseWriter, r *http.Request) {
	distPath := s.getDashboardDistPath()
	filePath := filepath.Join(distPath, "index.html")

	content, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "Dashboard assets not built. Run `npm install` and `npm run build` in assets/dashboard.", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

// handleSessions returns the list of workspaces and their sessions as JSON.
// Returns a hierarchical structure: workspaces -> sessions
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessions := s.session.GetAllSessions()

	// Group sessions by workspace
	type SessionResponse struct {
		ID           string `json:"id"`
		Agent        string `json:"agent"`
		Branch       string `json:"branch"`
		Prompt       string `json:"prompt"`
		Nickname     string `json:"nickname,omitempty"`
		CreatedAt    string `json:"created_at"`
		LastOutputAt string `json:"last_output_at,omitempty"`
		Running      bool   `json:"running"`
		AttachCmd    string `json:"attach_cmd"`
	}

	type WorkspaceResponse struct {
		ID           string            `json:"id"`
		Repo         string            `json:"repo"`
		Branch       string            `json:"branch"`
		SessionCount int               `json:"session_count"`
		Sessions     []SessionResponse `json:"sessions"`
	}

	workspaceMap := make(map[string]*WorkspaceResponse)

	for _, sess := range sessions {
		// Get workspace info
		ws, found := s.state.GetWorkspace(sess.WorkspaceID)
		if !found {
			continue
		}

		// Get or create workspace response
		wsResp, ok := workspaceMap[sess.WorkspaceID]
		if !ok {
			wsResp = &WorkspaceResponse{
				ID:       ws.ID,
				Repo:     ws.Repo,
				Branch:   ws.Branch,
				Sessions: []SessionResponse{},
			}
			workspaceMap[sess.WorkspaceID] = wsResp
		}

		attachCmd, _ := s.session.GetAttachCommand(sess.ID)
		lastOutputAt := ""
		if !sess.LastOutputAt.IsZero() {
			lastOutputAt = sess.LastOutputAt.Format("2006-01-02T15:04:05")
		}
		wsResp.Sessions = append(wsResp.Sessions, SessionResponse{
			ID:           sess.ID,
			Agent:        sess.Agent,
			Branch:       ws.Branch,
			Prompt:       sess.Prompt,
			Nickname:     sess.Nickname,
			CreatedAt:    sess.CreatedAt.Format("2006-01-02T15:04:05"),
			LastOutputAt: lastOutputAt,
			Running:      s.session.IsRunning(sess.ID),
			AttachCmd:    attachCmd,
		})
		wsResp.SessionCount = len(wsResp.Sessions)
	}

	// Convert map to slice and sort workspaces by ID
	response := make([]WorkspaceResponse, 0, len(workspaceMap))
	for _, ws := range workspaceMap {
		response = append(response, *ws)
	}
	sort.Slice(response, func(i, j int) bool {
		return response[i].ID < response[j].ID
	})

	// Sort sessions within each workspace by nickname (or agent if no nickname)
	for i := range response {
		sort.Slice(response[i].Sessions, func(j, k int) bool {
			sessJ := response[i].Sessions[j]
			sessK := response[i].Sessions[k]
			// Use nickname if set, otherwise fall back to agent name
			nameJ := sessJ.Nickname
			if nameJ == "" {
				nameJ = sessJ.Agent
			}
			nameK := sessK.Nickname
			if nameK == "" {
				nameK = sessK.Agent
			}
			return nameJ < nameK
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleWorkspacesAPI returns the list of all workspaces as JSON.
func (s *Server) handleWorkspacesAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type WorkspaceResponse struct {
		ID     string `json:"id"`
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
		Path   string `json:"path"`
	}

	workspaces := s.state.GetWorkspaces()
	response := make([]WorkspaceResponse, len(workspaces))
	for i, ws := range workspaces {
		response[i] = WorkspaceResponse{
			ID:     ws.ID,
			Repo:   ws.Repo,
			Branch: ws.Branch,
			Path:   ws.Path,
		}
	}
	sort.Slice(response, func(i, j int) bool {
		return response[i].ID < response[j].ID
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleHealthz returns a simple health check response.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// SpawnRequest represents a request to spawn sessions.
type SpawnRequest struct {
	Repo        string         `json:"repo"`
	Branch      string         `json:"branch"`
	Prompt      string         `json:"prompt"`
	Nickname    string         `json:"nickname,omitempty"`     // optional human-friendly name for sessions
	Agents      map[string]int `json:"agents"`                 // agent name -> quantity
	WorkspaceID string         `json:"workspace_id,omitempty"` // optional: spawn into specific workspace
}

// handleSpawnPost handles session spawning requests.
func (s *Server) handleSpawnPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SpawnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Repo == "" {
		http.Error(w, "repo is required", http.StatusBadRequest)
		return
	}
	if req.Branch == "" {
		http.Error(w, "branch is required", http.StatusBadRequest)
		return
	}
	if req.Prompt == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}
	if len(req.Agents) == 0 {
		http.Error(w, "at least one agent is required", http.StatusBadRequest)
		return
	}

	// Spawn sessions
	type SessionResult struct {
		SessionID   string `json:"session_id"`
		WorkspaceID string `json:"workspace_id"`
		Agent       string `json:"agent"`
		Error       string `json:"error,omitempty"`
	}

	results := make([]SessionResult, 0)

	for agentName, count := range req.Agents {
		for i := 0; i < count; i++ {
			nickname := req.Nickname
			if nickname != "" && count > 1 {
				nickname = fmt.Sprintf("%s (%d)", nickname, i+1)
			}
			sess, err := s.session.Spawn(req.Repo, req.Branch, agentName, req.Prompt, nickname, req.WorkspaceID)
			if err != nil {
				results = append(results, SessionResult{
					Agent: agentName,
					Error: err.Error(),
				})
			} else {
				results = append(results, SessionResult{
					SessionID:   sess.ID,
					WorkspaceID: sess.WorkspaceID,
					Agent:       agentName,
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// handleDispose handles session disposal requests.
func (s *Server) handleDispose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL
	sessionID := strings.TrimPrefix(r.URL.Path, "/api/dispose/")
	if sessionID == "" {
		http.Error(w, "session ID is required", http.StatusBadRequest)
		return
	}

	if err := s.session.Dispose(sessionID); err != nil {
		http.Error(w, fmt.Sprintf("Failed to dispose session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleDisposeWorkspace handles workspace disposal requests.
func (s *Server) handleDisposeWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract workspace ID from URL
	workspaceID := strings.TrimPrefix(r.URL.Path, "/api/dispose-workspace/")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	if err := s.workspace.Dispose(workspaceID); err != nil {
		http.Error(w, fmt.Sprintf("Failed to dispose workspace: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// UpdateNicknameRequest represents a request to update a session's nickname.
type UpdateNicknameRequest struct {
	Nickname string `json:"nickname"`
}

// handleUpdateNickname handles session nickname update requests.
func (s *Server) handleUpdateNickname(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL: /api/sessions-nickname/{session-id}
	sessionID := strings.TrimPrefix(r.URL.Path, "/api/sessions-nickname/")
	if sessionID == "" {
		http.Error(w, "session ID is required", http.StatusBadRequest)
		return
	}

	var req UpdateNicknameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Update nickname (and rename tmux session)
	if err := s.session.RenameSession(sessionID, req.Nickname); err != nil {
		http.Error(w, fmt.Sprintf("Failed to rename session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleConfig returns the config (repos and agents) for the spawn form.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type RepoResponse struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}

	type AgentResponse struct {
		Name string `json:"name"`
	}

	type TerminalResponse struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}

	type InternalResponse struct {
		MtimePollIntervalMs    int `json:"mtime_poll_interval_ms"`
		SessionsPollIntervalMs int `json:"sessions_poll_interval_ms"`
		ViewedBufferMs         int `json:"viewed_buffer_ms"`
		SessionSeenIntervalMs  int `json:"session_seen_interval_ms"`
	}

	type ConfigResponse struct {
		Repos    []RepoResponse   `json:"repos"`
		Agents   []AgentResponse  `json:"agents"`
		Terminal TerminalResponse `json:"terminal"`
		Internal InternalResponse `json:"internal"`
	}

	repos := s.config.GetRepos()
	agents := s.config.GetAgents()
	width, height := s.config.GetTerminalSize()

	repoResp := make([]RepoResponse, len(repos))
	for i, repo := range repos {
		repoResp[i] = RepoResponse{Name: repo.Name, URL: repo.URL}
	}

	agentResp := make([]AgentResponse, len(agents))
	for i, agent := range agents {
		agentResp[i] = AgentResponse{Name: agent.Name}
	}

	response := ConfigResponse{
		Repos:    repoResp,
		Agents:   agentResp,
		Terminal: TerminalResponse{Width: width, Height: height},
		Internal: InternalResponse{
			MtimePollIntervalMs:    s.config.GetMtimePollIntervalMs(),
			SessionsPollIntervalMs: s.config.GetSessionsPollIntervalMs(),
			ViewedBufferMs:         s.config.GetViewedBufferMs(),
			SessionSeenIntervalMs:  s.config.GetSessionSeenIntervalMs(),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleDiff returns git diff for a workspace.
func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract workspace ID from URL: /api/diff/{workspace-id}
	workspaceID := strings.TrimPrefix(r.URL.Path, "/api/diff/")
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

	// Run git diff in workspace directory
	type FileDiff struct {
		OldPath string   `json:"old_path,omitempty"`
		NewPath string   `json:"new_path,omitempty"`
		OldContent string `json:"old_content,omitempty"`
		NewContent string `json:"new_content,omitempty"`
		Status string `json:"status,omitempty"` // added, modified, deleted, renamed
	}

	type DiffResponse struct {
		WorkspaceID string     `json:"workspace_id"`
		Repo        string     `json:"repo"`
		Branch      string     `json:"branch"`
		Files       []FileDiff `json:"files"`
	}

	// Get git diff output using porcelain format
	// --numstat shows: added/deleted lines filename
	// -z uses null terminators for parsing
	// --find-renames finds renames
	cmd := exec.Command("git", "-C", ws.Path, "diff", "--numstat", "--find-renames", "--diff-filter=ADM")
	output, err := cmd.Output()
	if err != nil {
		// No changes is not an error - return empty diff
		response := DiffResponse{
			WorkspaceID: workspaceID,
			Repo:        ws.Repo,
			Branch:      ws.Branch,
			Files:       []FileDiff{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Parse numstat output and get file diffs
	files := make([]FileDiff, 0)
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}

		added := parts[0]
		_ = parts[1] // deleted lines (not currently used)
		filePath := parts[2]

		// Skip if file was deleted (added is "-")
		if added == "-" {
			// For deleted files, get old content
			oldContent := s.getFileContent(ws.Path, filePath, "HEAD")
			files = append(files, FileDiff{
				NewPath:    filePath,
				OldContent: oldContent,
				Status:     "deleted",
			})
			continue
		}

		// Check if file is new (deleted is "0" and file doesn't exist in HEAD)
		newContent := s.getFileContent(ws.Path, filePath, "worktree")
		oldContent := s.getFileContent(ws.Path, filePath, "HEAD")

		status := "modified"
		if oldContent == "" {
			status = "added"
		}

		files = append(files, FileDiff{
			NewPath:    filePath,
			OldContent: oldContent,
			NewContent: newContent,
			Status:     status,
		})
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
func (s *Server) getFileContent(workspacePath, filePath, treeish string) string {
	if treeish == "worktree" {
		fullPath := filepath.Join(workspacePath, filePath)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return ""
		}
		return string(content)
	}
	cmd := exec.Command("git", "-C", workspacePath, "show", fmt.Sprintf("%s:%s", treeish, filePath))
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(output)
}

// handleDiffPage serves the React app entry point for the diff page.
func (s *Server) handleDiffPage(w http.ResponseWriter, r *http.Request) {
	s.handleApp(w, r)
}
