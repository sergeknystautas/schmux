package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/sergek/schmux/internal/config"
	"github.com/sergek/schmux/internal/detect"
	"github.com/sergek/schmux/internal/nudgenik"
)

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
		Nickname     string `json:"nickname,omitempty"`
		CreatedAt    string `json:"created_at"`
		LastOutputAt string `json:"last_output_at,omitempty"`
		Running      bool   `json:"running"`
		AttachCmd    string `json:"attach_cmd"`
		NudgeState   string `json:"nudge_state,omitempty"`
		NudgeSummary string `json:"nudge_summary,omitempty"`
	}

	type WorkspaceResponse struct {
		ID           string            `json:"id"`
		Repo         string            `json:"repo"`
		Branch       string            `json:"branch"`
		Path         string            `json:"path"`
		SessionCount int               `json:"session_count"`
		Sessions     []SessionResponse `json:"sessions"`
		GitDirty     bool              `json:"git_dirty"`
		GitAhead     int               `json:"git_ahead"`
		GitBehind    int               `json:"git_behind"`
	}

	workspaceMap := make(map[string]*WorkspaceResponse)
	workspaces := s.state.GetWorkspaces()
	for _, ws := range workspaces {
		workspaceMap[ws.ID] = &WorkspaceResponse{
			ID:           ws.ID,
			Repo:         ws.Repo,
			Branch:       ws.Branch,
			Path:         ws.Path,
			SessionCount: 0,
			Sessions:     []SessionResponse{},
			GitDirty:     ws.GitDirty,
			GitAhead:     ws.GitAhead,
			GitBehind:    ws.GitBehind,
		}
	}

	for _, sess := range sessions {
		// Get workspace info
		wsResp, ok := workspaceMap[sess.WorkspaceID]
		if !ok {
			continue
		}

		attachCmd, _ := s.session.GetAttachCommand(sess.ID)
		lastOutputAt := ""
		if !sess.LastOutputAt.IsZero() {
			lastOutputAt = sess.LastOutputAt.Format("2006-01-02T15:04:05")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetTmuxQueryTimeoutSeconds())*time.Second)
		running := s.session.IsRunning(ctx, sess.ID)
		cancel()
		nudgeState, nudgeSummary := parseNudgeSummary(sess.Nudge)
		wsResp.Sessions = append(wsResp.Sessions, SessionResponse{
			ID:           sess.ID,
			Agent:        sess.Agent,
			Branch:       wsResp.Branch,
			Nickname:     sess.Nickname,
			CreatedAt:    sess.CreatedAt.Format("2006-01-02T15:04:05"),
			LastOutputAt: lastOutputAt,
			Running:      running,
			AttachCmd:    attachCmd,
			NudgeState:   nudgeState,
			NudgeSummary: nudgeSummary,
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

	// Sort sessions within each workspace by display name
	for i := range response {
		sort.Slice(response[i].Sessions, func(j, k int) bool {
			nameJ := response[i].Sessions[j].Nickname
			if nameJ == "" {
				nameJ = response[i].Sessions[j].Agent
			}
			nameK := response[i].Sessions[k].Nickname
			if nameK == "" {
				nameK = response[i].Sessions[k].Agent
			}
			return nameJ < nameK
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

type nudgenikPayload struct {
	State   string `json:"state"`
	Summary string `json:"summary"`
}

func parseNudgeSummary(nudge string) (string, string) {
	trimmed := strings.TrimSpace(nudge)
	if trimmed == "" {
		return "", ""
	}

	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "```json"))
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "```"))
	}

	var payload nudgenikPayload
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return "", ""
	}

	return strings.TrimSpace(payload.State), strings.TrimSpace(payload.Summary)
}

// handleWorkspacesScan scans the workspace directory and reconciles with state.
func (s *Server) handleWorkspacesScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, err := s.workspace.Scan()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to scan workspaces: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
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
	if req.WorkspaceID == "" {
		// When not spawning into existing workspace, repo and branch are required
		if req.Repo == "" {
			http.Error(w, "repo is required (when not using --workspace)", http.StatusBadRequest)
			return
		}
		if req.Branch == "" {
			http.Error(w, "branch is required (when not using --workspace)", http.StatusBadRequest)
			return
		}
	}
	if len(req.Agents) == 0 {
		http.Error(w, "at least one agent is required", http.StatusBadRequest)
		return
	}

	// Check if any agentic agents are being spawned (require prompt)
	hasAgentic := false
	for agentName := range req.Agents {
		if agent, found := s.config.GetAgentConfig(agentName); found && agent.Agentic != nil && *agent.Agentic {
			hasAgentic = true
			break
		}
	}
	if hasAgentic && req.Prompt == "" {
		http.Error(w, "prompt is required when spawning agentic agents", http.StatusBadRequest)
		return
	}

	// Spawn sessions
	type SessionResult struct {
		SessionID   string `json:"session_id"`
		WorkspaceID string `json:"workspace_id"`
		Agent       string `json:"agent"`
		Prompt      string `json:"prompt,omitempty"`
		Nickname    string `json:"nickname,omitempty"`
		Error       string `json:"error,omitempty"`
	}

	// Log the spawn request
	promptPreview := req.Prompt
	if len(promptPreview) > 100 {
		promptPreview = promptPreview[:100] + "..."
	}
	log.Printf("[spawn] request: repo=%s branch=%s workspace_id=%s agents=%v prompt=%q",
		req.Repo, req.Branch, req.WorkspaceID, req.Agents, promptPreview)

	results := make([]SessionResult, 0)

	for agentName, count := range req.Agents {
		// Get agent config to check if it's agentic
		agent, found := s.config.GetAgentConfig(agentName)
		if !found {
			results = append(results, SessionResult{
				Agent: agentName,
				Error: fmt.Sprintf("agent not found: %s", agentName),
			})
			continue
		}

		isAgentic := agent.Agentic != nil && *agent.Agentic

		// Non-agentic commands spawn single instance (ignore count)
		spawnCount := count
		if !isAgentic {
			spawnCount = 1
		}

		for i := 0; i < spawnCount; i++ {
			nickname := req.Nickname
			if nickname != "" && spawnCount > 1 {
				nickname = fmt.Sprintf("%s (%d)", nickname, i+1)
			}
			// Session spawn needs a longer timeout for git operations
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitCloneTimeoutSeconds())*time.Second)
			sess, err := s.session.Spawn(ctx, req.Repo, req.Branch, agentName, req.Prompt, nickname, req.WorkspaceID)
			cancel()
			if err != nil {
				results = append(results, SessionResult{
					Agent:    agentName,
					Prompt:   req.Prompt,
					Nickname: nickname,
					Error:    err.Error(),
				})
			} else {
				results = append(results, SessionResult{
					SessionID:   sess.ID,
					WorkspaceID: sess.WorkspaceID,
					Agent:       agentName,
					Prompt:      req.Prompt,
					Nickname:    nickname,
				})
			}
		}
	}

	// Log the results
	for _, r := range results {
		if r.Error != "" {
			log.Printf("[spawn] error: agent=%s error=%s", r.Agent, r.Error)
		} else {
			log.Printf("[spawn] success: agent=%s session_id=%s workspace_id=%s", r.Agent, r.SessionID, r.WorkspaceID)
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

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetTmuxOperationTimeoutSeconds())*time.Second)
	if err := s.session.Dispose(ctx, sessionID); err != nil {
		cancel()
		log.Printf("[dispose] error: session_id=%s error=%v", sessionID, err)
		http.Error(w, fmt.Sprintf("Failed to dispose session: %v", err), http.StatusInternalServerError)
		return
	}
	cancel()
	log.Printf("[dispose] success: session_id=%s", sessionID)

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
		log.Printf("[dispose-workspace] error: workspace_id=%s error=%v", workspaceID, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest) // 400 for client-side errors like dirty state
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	log.Printf("[dispose-workspace] success: workspace_id=%s", workspaceID)

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
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetTmuxOperationTimeoutSeconds())*time.Second)
	err := s.session.RenameSession(ctx, sessionID, req.Nickname)
	cancel()
	if err != nil {
		// Check if this is a nickname conflict error
		if strings.Contains(err.Error(), "already in use") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict) // 409 Conflict
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		http.Error(w, fmt.Sprintf("Failed to rename session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleConfig returns the config (repos and agents) for the spawn form,
// or updates the config via POST.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleConfigGet(w, r)
	case http.MethodPost, http.MethodPut:
		s.handleConfigUpdate(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleDetectAgents runs agent detection and returns the detected agents.
func (s *Server) handleDetectAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type AgentResponse struct {
		Name    string `json:"name"`
		Command string `json:"command"`
		Agentic *bool  `json:"agentic,omitempty"`
	}

	type Response struct {
		Agents []AgentResponse `json:"agents"`
	}

	// Run detection with a reasonable timeout
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	detectedAgents, err := detect.DetectAvailableAgentsContext(ctx, false)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			http.Error(w, "Detection timed out. Some agents may not be available or took too long to respond.", http.StatusGatewayTimeout)
			return
		}
		http.Error(w, fmt.Sprintf("Detection failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert detected agents to config.Tool format and update config
	tools := make([]config.Tool, len(detectedAgents))
	for i, da := range detectedAgents {
		tools[i] = config.Tool{
			Name:    da.Name,
			Command: da.Command,
			Source:  da.Source,
			Agentic: da.Agentic,
		}
	}
	s.config.SetTools(tools)
	if err := s.config.Save(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save detected tools: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert to response format (for agents)
	agentResp := make([]AgentResponse, len(detectedAgents))
	for i, da := range detectedAgents {
		agentic := da.Agentic
		agentResp[i] = AgentResponse{
			Name:    da.Name,
			Command: da.Command,
			Agentic: &agentic,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(Response{Agents: agentResp}); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleConfigGet returns the current config.
func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	type RepoResponse struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}

	type AgentResponse struct {
		Name    string `json:"name"`
		Command string `json:"command"`
		Agentic *bool  `json:"agentic,omitempty"` // true = takes prompt, false = command only
	}

	type TerminalResponse struct {
		Width          int `json:"width"`
		Height         int `json:"height"`
		SeedLines      int `json:"seed_lines"`
		BootstrapLines int `json:"bootstrap_lines"`
	}

	type InternalResponse struct {
		MtimePollIntervalMs     int `json:"mtime_poll_interval_ms"`
		SessionsPollIntervalMs  int `json:"sessions_poll_interval_ms"`
		ViewedBufferMs          int `json:"viewed_buffer_ms"`
		SessionSeenIntervalMs   int `json:"session_seen_interval_ms"`
		GitStatusPollIntervalMs int `json:"git_status_poll_interval_ms"`
		GitCloneTimeoutSeconds  int `json:"git_clone_timeout_seconds"`
		GitStatusTimeoutSeconds int `json:"git_status_timeout_seconds"`
	}

	type ConfigResponse struct {
		WorkspacePath string           `json:"workspace_path"`
		Repos         []RepoResponse   `json:"repos"`
		Agents        []AgentResponse  `json:"agents"`
		Terminal      TerminalResponse `json:"terminal"`
		Internal      InternalResponse `json:"internal"`
	}

	repos := s.config.GetRepos()
	agents := s.config.GetAgents()
	width, height := s.config.GetTerminalSize()
	seedLines := s.config.GetTerminalSeedLines()
	bootstrapLines := s.config.GetTerminalBootstrapLines()

	repoResp := make([]RepoResponse, len(repos))
	for i, repo := range repos {
		repoResp[i] = RepoResponse{Name: repo.Name, URL: repo.URL}
	}

	agentResp := make([]AgentResponse, len(agents))
	for i, agent := range agents {
		agentResp[i] = AgentResponse{Name: agent.Name, Command: agent.Command, Agentic: agent.Agentic}
	}

	response := ConfigResponse{
		WorkspacePath: s.config.GetWorkspacePath(),
		Repos:         repoResp,
		Agents:        agentResp,
		Terminal:      TerminalResponse{Width: width, Height: height, SeedLines: seedLines, BootstrapLines: bootstrapLines},
		Internal: InternalResponse{
			MtimePollIntervalMs:     s.config.GetMtimePollIntervalMs(),
			SessionsPollIntervalMs:  s.config.GetSessionsPollIntervalMs(),
			ViewedBufferMs:          s.config.GetViewedBufferMs(),
			SessionSeenIntervalMs:   s.config.GetSessionSeenIntervalMs(),
			GitStatusPollIntervalMs: s.config.GetGitStatusPollIntervalMs(),
			GitCloneTimeoutSeconds:  s.config.GetGitCloneTimeoutSeconds(),
			GitStatusTimeoutSeconds: s.config.GetGitStatusTimeoutSeconds(),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ConfigUpdateRequest represents a request to update the config.
type ConfigUpdateRequest struct {
	WorkspacePath *string `json:"workspace_path,omitempty"`
	Repos         []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"repos,omitempty"`
	Agents []struct {
		Name    string `json:"name"`
		Command string `json:"command"`
		Agentic *bool  `json:"agentic,omitempty"`
	} `json:"agents,omitempty"`
	Terminal *struct {
		Width          *int `json:"width,omitempty"`
		Height         *int `json:"height,omitempty"`
		SeedLines      *int `json:"seed_lines,omitempty"`
		BootstrapLines *int `json:"bootstrap_lines,omitempty"`
	} `json:"terminal,omitempty"`
	Internal *struct {
		MtimePollIntervalMs     *int `json:"mtime_poll_interval_ms,omitempty"`
		SessionsPollIntervalMs  *int `json:"sessions_poll_interval_ms,omitempty"`
		ViewedBufferMs          *int `json:"viewed_buffer_ms,omitempty"`
		SessionSeenIntervalMs   *int `json:"session_seen_interval_ms,omitempty"`
		GitStatusPollIntervalMs *int `json:"git_status_poll_interval_ms,omitempty"`
		GitCloneTimeoutSeconds  *int `json:"git_clone_timeout_seconds,omitempty"`
		GitStatusTimeoutSeconds *int `json:"git_status_timeout_seconds,omitempty"`
	} `json:"internal,omitempty"`
}

// handleConfigUpdate handles config update requests.
func (s *Server) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	var req ConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Reload config from disk to get all current values (including tools, etc.)
	if err := s.config.Reload(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to reload config: %v", err), http.StatusInternalServerError)
		return
	}

	cfg := s.config

	// Check for workspace path change (for warning after save)
	sessionCount := len(s.state.GetSessions())
	workspaceCount := len(s.state.GetWorkspaces())
	pathChanged := false
	var newPath string

	// Apply updates
	if req.WorkspacePath != nil {
		newPath = *req.WorkspacePath
		// Expand ~ if present
		homeDir, _ := os.UserHomeDir()
		if len(newPath) > 0 && newPath[0] == '~' && homeDir != "" {
			newPath = filepath.Join(homeDir, newPath[1:])
		}
		pathChanged = (newPath != cfg.GetWorkspacePath() && (sessionCount > 0 || workspaceCount > 0))
		cfg.WorkspacePath = newPath
	}

	if req.Repos != nil {
		// Validate repos
		for _, repo := range req.Repos {
			if repo.Name == "" {
				http.Error(w, "repo name is required", http.StatusBadRequest)
				return
			}
			if repo.URL == "" {
				http.Error(w, fmt.Sprintf("repo URL is required for %s", repo.Name), http.StatusBadRequest)
				return
			}
		}
		cfg.Repos = make([]config.Repo, len(req.Repos))
		for i, r := range req.Repos {
			cfg.Repos[i] = config.Repo{Name: r.Name, URL: r.URL}
		}
	}

	if req.Agents != nil {
		// Validate agents
		for _, agent := range req.Agents {
			if agent.Name == "" {
				http.Error(w, "agent name is required", http.StatusBadRequest)
				return
			}
			if agent.Command == "" {
				http.Error(w, fmt.Sprintf("agent command is required for %s", agent.Name), http.StatusBadRequest)
				return
			}
		}
		cfg.Agents = make([]config.Agent, len(req.Agents))
		for i, a := range req.Agents {
			cfg.Agents[i] = config.Agent{Name: a.Name, Command: a.Command, Agentic: a.Agentic}
		}
	}

	if req.Terminal != nil {
		if cfg.Terminal == nil {
			cfg.Terminal = &config.TerminalSize{}
		}
		if req.Terminal.Width != nil && *req.Terminal.Width > 0 {
			cfg.Terminal.Width = *req.Terminal.Width
		}
		if req.Terminal.Height != nil && *req.Terminal.Height > 0 {
			cfg.Terminal.Height = *req.Terminal.Height
		}
		if req.Terminal.SeedLines != nil && *req.Terminal.SeedLines > 0 {
			cfg.Terminal.SeedLines = *req.Terminal.SeedLines
		}
		if req.Terminal.BootstrapLines != nil && *req.Terminal.BootstrapLines > 0 {
			cfg.Terminal.BootstrapLines = *req.Terminal.BootstrapLines
		}
	}

	if req.Internal != nil {
		if cfg.Internal == nil {
			cfg.Internal = &config.InternalIntervals{}
		}
		if cfg.Internal.Timeouts == nil {
			cfg.Internal.Timeouts = &config.Timeouts{}
		}
		if req.Internal.MtimePollIntervalMs != nil && *req.Internal.MtimePollIntervalMs > 0 {
			cfg.Internal.MtimePollIntervalMs = *req.Internal.MtimePollIntervalMs
		}
		if req.Internal.SessionsPollIntervalMs != nil && *req.Internal.SessionsPollIntervalMs > 0 {
			cfg.Internal.SessionsPollIntervalMs = *req.Internal.SessionsPollIntervalMs
		}
		if req.Internal.ViewedBufferMs != nil && *req.Internal.ViewedBufferMs > 0 {
			cfg.Internal.ViewedBufferMs = *req.Internal.ViewedBufferMs
		}
		if req.Internal.SessionSeenIntervalMs != nil && *req.Internal.SessionSeenIntervalMs > 0 {
			cfg.Internal.SessionSeenIntervalMs = *req.Internal.SessionSeenIntervalMs
		}
		if req.Internal.GitStatusPollIntervalMs != nil && *req.Internal.GitStatusPollIntervalMs > 0 {
			cfg.Internal.GitStatusPollIntervalMs = *req.Internal.GitStatusPollIntervalMs
		}
		if req.Internal.GitCloneTimeoutSeconds != nil && *req.Internal.GitCloneTimeoutSeconds > 0 {
			cfg.Internal.Timeouts.GitCloneSeconds = *req.Internal.GitCloneTimeoutSeconds
		}
		if req.Internal.GitStatusTimeoutSeconds != nil && *req.Internal.GitStatusTimeoutSeconds > 0 {
			cfg.Internal.Timeouts.GitStatusSeconds = *req.Internal.GitStatusTimeoutSeconds
		}
	}

	// Save config
	if err := cfg.Save(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save config: %v", err), http.StatusInternalServerError)
		return
	}

	// Return warning if path changed with existing sessions/workspaces
	if pathChanged {
		type WarningResponse struct {
			Warning         string `json:"warning"`
			SessionCount    int    `json:"session_count"`
			WorkspaceCount  int    `json:"workspace_count"`
			RequiresRestart bool   `json:"requires_restart"`
		}
		warning := WarningResponse{
			Warning:         fmt.Sprintf("Changing workspace_path affects only NEW workspaces. %d existing sessions and %d workspaces will keep their current paths.", sessionCount, workspaceCount),
			SessionCount:    sessionCount,
			WorkspaceCount:  workspaceCount,
			RequiresRestart: true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(warning)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Config saved and reloaded. Changes are now in effect.",
	})
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
		OldPath    string `json:"old_path,omitempty"`
		NewPath    string `json:"new_path,omitempty"`
		OldContent string `json:"old_content,omitempty"`
		NewContent string `json:"new_content,omitempty"`
		Status     string `json:"status,omitempty"` // added, modified, deleted, renamed
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutSeconds())*time.Second)
	cmd := exec.CommandContext(ctx, "git", "-C", ws.Path, "diff", "--numstat", "--find-renames", "--diff-filter=ADM")
	output, err := cmd.Output()
	cancel()
	if err != nil {
		// No changes is not an error - continue, we'll still check for untracked files
		output = []byte{}
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
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutSeconds())*time.Second)
			oldContent := s.getFileContent(ctx, ws.Path, filePath, "HEAD")
			cancel()
			files = append(files, FileDiff{
				NewPath:    filePath,
				OldContent: oldContent,
				Status:     "deleted",
			})
			continue
		}

		// Check if file is new (deleted is "0" and file doesn't exist in HEAD)
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutSeconds())*time.Second)
		newContent := s.getFileContent(ctx, ws.Path, filePath, "worktree")
		oldContent := s.getFileContent(ctx, ws.Path, filePath, "HEAD")
		cancel()

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

	// Get untracked files
	// ls-files --others --exclude-standard lists untracked files (respecting .gitignore)
	ctx, cancel = context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutSeconds())*time.Second)
	untrackedCmd := exec.CommandContext(ctx, "git", "-C", ws.Path, "ls-files", "--others", "--exclude-standard")
	untrackedOutput, err := untrackedCmd.Output()
	cancel()
	if err == nil {
		untrackedLines := strings.Split(string(untrackedOutput), "\n")
		for _, filePath := range untrackedLines {
			if filePath == "" {
				continue
			}
			// Get content of untracked file from working directory
			newContent := s.getFileContent(context.Background(), ws.Path, filePath, "worktree")
			files = append(files, FileDiff{
				NewPath:    filePath,
				NewContent: newContent,
				Status:     "untracked",
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
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return ""
		}
		return string(content)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", workspacePath, "show", fmt.Sprintf("%s:%s", treeish, filePath))
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(output)
}

// handleOpenVSCode opens VS Code in a new window for the specified workspace.
func (s *Server) handleOpenVSCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract workspace ID from URL: /api/open-vscode/{workspace-id}
	workspaceID := strings.TrimPrefix(r.URL.Path, "/api/open-vscode/")
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

	// Check if workspace directory exists
	if _, err := os.Stat(ws.Path); os.IsNotExist(err) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(OpenVSCodeResponse{
			Success: false,
			Message: "workspace directory does not exist",
		})
		return
	}

	// Run `code -n <path>` to open VS Code in a new window
	// Use LookPath to check if code command exists
	codePath, err := exec.LookPath("code")
	if err != nil {
		log.Printf("[open-vscode] VS Code command not found in PATH")
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
			Message: fmt.Sprintf("VS Code command not found in PATH\n\nTo fix this:\nOpen VS Code, press %s, then run: Shell Command: Install 'code' command in PATH", shortcut),
		})
		return
	}

	// Execute code command
	// Note: We don't wait for the command to complete since VS Code opens as a separate process
	cmd := exec.Command(codePath, "-n", ws.Path)
	if err := cmd.Start(); err != nil {
		log.Printf("[open-vscode] failed to launch: %v", err)
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

// handleAskNudgenik handles GET requests to ask NudgeNik about a session's output.
// GET /api/askNudgenik/{sessionId}
//
// Combines extraction of the latest session response with the Claude CLI call.
// The response extraction happens internally on the server side.
func (s *Server) handleAskNudgenik(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL: /api/askNudgenik/{session-id}
	sessionID := strings.TrimPrefix(r.URL.Path, "/api/askNudgenik/")
	if sessionID == "" {
		http.Error(w, "session ID is required", http.StatusBadRequest)
		return
	}

	// Get session from state
	sess, found := s.state.GetSession(sessionID)
	if !found {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	ctx := context.Background()
	response, err := nudgenik.AskForSession(ctx, s.config, sess)
	if err != nil {
		switch {
		case errors.Is(err, nudgenik.ErrNoResponse):
			log.Printf("[ask-nudgenik] no response extracted from session %s", sessionID)
			http.Error(w, "No response found in session output", http.StatusBadRequest)
		case errors.Is(err, nudgenik.ErrAgentNotFound):
			log.Printf("[ask-nudgenik] claude agent not found in config")
			http.Error(w, "Claude agent not found. Please run agent detection first.", http.StatusServiceUnavailable)
		default:
			log.Printf("[ask-nudgenik] failed to ask for session %s: %v", sessionID, err)
			http.Error(w, fmt.Sprintf("Failed to ask nudgenik: %v", err), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"response": response})
}

// handleHasNudgenik handles GET requests to check if nudgenik is available globally.
// SPIKE: Always returns true - we use CLI tools directly, no session needed.
func (s *Server) handleHasNudgenik(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// SPIKE: Always available - we call CLI tools directly, no session needed
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"available": true})
}
