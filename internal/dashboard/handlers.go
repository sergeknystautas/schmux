package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// handleIndex serves the main dashboard page.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	s.serveHTML(w, r, "index.html")
}

// handleSpawn serves the spawn page.
func (s *Server) handleSpawn(w http.ResponseWriter, r *http.Request) {
	s.serveHTML(w, r, "spawn.html")
}

// handleSessionDetail serves the session detail page.
func (s *Server) handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	// Extract session ID from URL path: /sessions/{id}
	sessionID := strings.TrimPrefix(r.URL.Path, "/sessions/")
	if sessionID == "" {
		http.NotFound(w, r)
		return
	}

	// Serve terminal.html directly at this URL
	// The JavaScript will extract the session ID from the path
	s.serveHTML(w, r, "terminal.html")
}

// handleTerminalHTML serves the terminal view page.
func (s *Server) handleTerminalHTML(w http.ResponseWriter, r *http.Request) {
	s.serveHTML(w, r, "terminal.html")
}

// serveHTML serves an HTML file from the assets directory.
func (s *Server) serveHTML(w http.ResponseWriter, r *http.Request, filename string) {
	assetPath := s.getAssetPath()
	filePath := filepath.Join(assetPath, filename)

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Set content type and serve
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

// handleStatic serves static assets (CSS, JS) from the assets directory.
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	assetPath := s.getAssetPath()
	filename := filepath.Base(r.URL.Path)
	filePath := filepath.Join(assetPath, filename)
	http.ServeFile(w, r, filePath)
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
		ID        string `json:"id"`
		Agent     string `json:"agent"`
		Branch    string `json:"branch"`
		Prompt    string `json:"prompt"`
		CreatedAt string `json:"created_at"`
		Running   bool   `json:"running"`
		AttachCmd string `json:"attach_cmd"`
	}

	type WorkspaceResponse struct {
		ID          string            `json:"id"`
		Repo        string            `json:"repo"`
		Branch      string            `json:"branch"`
		SessionCount int              `json:"session_count"`
		Sessions    []SessionResponse `json:"sessions"`
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
		wsResp.Sessions = append(wsResp.Sessions, SessionResponse{
			ID:        sess.ID,
			Agent:     sess.Agent,
			Branch:    sess.Branch,
			Prompt:    sess.Prompt,
			CreatedAt: sess.CreatedAt.Format("2006-01-02T15:04:05Z"),
			Running:   s.session.IsRunning(sess.ID),
			AttachCmd: attachCmd,
		})
		wsResp.SessionCount = len(wsResp.Sessions)
	}

	// Convert map to slice
	response := make([]WorkspaceResponse, 0, len(workspaceMap))
	for _, ws := range workspaceMap {
		response = append(response, *ws)
	}

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
	Agents      map[string]int `json:"agents"` // agent name -> quantity
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
			sess, err := s.session.Spawn(req.Repo, req.Branch, agentName, req.Prompt, req.WorkspaceID)
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

	type ConfigResponse struct {
		Repos  []RepoResponse  `json:"repos"`
		Agents []AgentResponse `json:"agents"`
	}

	repos := s.config.GetRepos()
	agents := s.config.GetAgents()

	repoResp := make([]RepoResponse, len(repos))
	for i, repo := range repos {
		repoResp[i] = RepoResponse{Name: repo.Name, URL: repo.URL}
	}

	agentResp := make([]AgentResponse, len(agents))
	for i, agent := range agents {
		agentResp[i] = AgentResponse{Name: agent.Name}
	}

	response := ConfigResponse{
		Repos:  repoResp,
		Agents: agentResp,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
