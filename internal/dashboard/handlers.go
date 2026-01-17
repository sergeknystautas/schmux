package dashboard

import (
	"context"
	"embed"
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
	"github.com/sergek/schmux/internal/workspace"
)

//go:embed builtin_quick_launch.json
var builtinQuickLaunchFS embed.FS

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
		Target       string `json:"target"`
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
			Target:       sess.Target,
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
				nameJ = response[i].Sessions[j].Target
			}
			nameK := response[i].Sessions[k].Nickname
			if nameK == "" {
				nameK = response[i].Sessions[k].Target
			}
			return nameJ < nameK
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func parseNudgeSummary(nudge string) (string, string) {
	trimmed := strings.TrimSpace(nudge)
	if trimmed == "" {
		return "", ""
	}

	result, err := nudgenik.ParseResult(trimmed)
	if err != nil {
		return "", ""
	}

	return strings.TrimSpace(result.State), strings.TrimSpace(result.Summary)
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
	Targets     map[string]int `json:"targets"`                // target name -> quantity
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
	if len(req.Targets) == 0 {
		http.Error(w, "at least one target is required", http.StatusBadRequest)
		return
	}

	// Spawn sessions
	type SessionResult struct {
		SessionID   string `json:"session_id"`
		WorkspaceID string `json:"workspace_id"`
		Target      string `json:"target"`
		Prompt      string `json:"prompt,omitempty"`
		Nickname    string `json:"nickname,omitempty"`
		Error       string `json:"error,omitempty"`
	}

	// Log the spawn request
	promptPreview := req.Prompt
	if len(promptPreview) > 100 {
		promptPreview = promptPreview[:100] + "..."
	}
	log.Printf("[spawn] request: repo=%s branch=%s workspace_id=%s targets=%v prompt=%q",
		req.Repo, req.Branch, req.WorkspaceID, req.Targets, promptPreview)

	results := make([]SessionResult, 0)

	// Calculate total sessions to spawn for global nickname numbering
	totalToSpawn := 0
	detected := s.config.GetDetectedRunTargets()
	for targetName, count := range req.Targets {
		promptable, found := getTargetPromptable(s.config, detected, targetName)
		if !found || (promptable && strings.TrimSpace(req.Prompt) == "") || (!promptable && strings.TrimSpace(req.Prompt) != "") {
			continue
		}
		spawnCount := count
		if !promptable {
			spawnCount = 1
		}
		totalToSpawn += spawnCount
	}

	// Global counter for nickname numbering across all targets
	globalIndex := 0

	for targetName, count := range req.Targets {
		promptable, found := getTargetPromptable(s.config, detected, targetName)
		if !found {
			results = append(results, SessionResult{
				Target: targetName,
				Error:  fmt.Sprintf("target not found: %s", targetName),
			})
			continue
		}
		if promptable && strings.TrimSpace(req.Prompt) == "" {
			results = append(results, SessionResult{
				Target: targetName,
				Error:  "prompt is required for promptable targets",
			})
			continue
		}
		if !promptable && strings.TrimSpace(req.Prompt) != "" {
			results = append(results, SessionResult{
				Target: targetName,
				Error:  "prompt is not allowed for command targets",
			})
			continue
		}

		spawnCount := count
		if !promptable {
			spawnCount = 1
		}

		for i := 0; i < spawnCount; i++ {
			globalIndex++
			var nickname string
			if req.Nickname != "" && totalToSpawn > 1 {
				nickname = fmt.Sprintf("%s (%d)", req.Nickname, globalIndex)
			} else {
				nickname = req.Nickname
			}
			// Session spawn needs a longer timeout for git operations
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitCloneTimeoutSeconds())*time.Second)
			sess, err := s.session.Spawn(ctx, req.Repo, req.Branch, targetName, req.Prompt, nickname, req.WorkspaceID)
			cancel()
			if err != nil {
				results = append(results, SessionResult{
					Target:   targetName,
					Prompt:   req.Prompt,
					Nickname: nickname,
					Error:    err.Error(),
				})
			} else {
				results = append(results, SessionResult{
					SessionID:   sess.ID,
					WorkspaceID: sess.WorkspaceID,
					Target:      targetName,
					Prompt:      req.Prompt,
					Nickname:    sess.Nickname, // Return actual nickname, not input
				})
			}
		}
	}

	// Log the results
	for _, r := range results {
		if r.Error != "" {
			log.Printf("[spawn] error: target=%s error=%s", r.Target, r.Error)
		} else {
			log.Printf("[spawn] success: target=%s session_id=%s workspace_id=%s", r.Target, r.SessionID, r.WorkspaceID)
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

// handleDetectTools returns detected targets from config (GET only).
func (s *Server) handleDetectTools(w http.ResponseWriter, r *http.Request) {
	type ToolResponse struct {
		Name    string `json:"name"`
		Command string `json:"command"`
		Source  string `json:"source"`
	}

	type Response struct {
		Tools []ToolResponse `json:"tools"`
	}

	var detectedTools []detect.Tool
	switch r.Method {
	case http.MethodGet:
		for _, target := range s.config.GetDetectedRunTargets() {
			detectedTools = append(detectedTools, detect.Tool{
				Name:    target.Name,
				Command: target.Command,
				Source:  "config",
				Agentic: true,
			})
		}
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	toolResp := make([]ToolResponse, len(detectedTools))
	for i, dt := range detectedTools {
		toolResp[i] = ToolResponse{
			Name:    dt.Name,
			Command: dt.Command,
			Source:  dt.Source,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(Response{Tools: toolResp}); err != nil {
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

	type RunTargetResponse struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Command string `json:"command"`
		Source  string `json:"source,omitempty"`
	}

	type QuickLaunchResponse struct {
		Name   string  `json:"name"`
		Target string  `json:"target"`
		Prompt *string `json:"prompt"`
	}

	type TerminalResponse struct {
		Width          int `json:"width"`
		Height         int `json:"height"`
		SeedLines      int `json:"seed_lines"`
		BootstrapLines int `json:"bootstrap_lines"`
	}

	type NudgenikResponse struct {
		Target string `json:"target,omitempty"`
	}

	type InternalResponse struct {
		MtimePollIntervalMs     int  `json:"mtime_poll_interval_ms"`
		SessionsPollIntervalMs  int  `json:"sessions_poll_interval_ms"`
		ViewedBufferMs          int  `json:"viewed_buffer_ms"`
		SessionSeenIntervalMs   int  `json:"session_seen_interval_ms"`
		GitStatusPollIntervalMs int  `json:"git_status_poll_interval_ms"`
		GitCloneTimeoutSeconds  int  `json:"git_clone_timeout_seconds"`
		GitStatusTimeoutSeconds int  `json:"git_status_timeout_seconds"`
		NetworkAccess           bool `json:"network_access"`
	}

	type ConfigResponse struct {
		WorkspacePath string                 `json:"workspace_path"`
		Repos         []RepoResponse         `json:"repos"`
		RunTargets    []RunTargetResponse    `json:"run_targets"`
		QuickLaunch   []QuickLaunchResponse  `json:"quick_launch"`
		Variants      []config.VariantConfig `json:"variants,omitempty"`
		Nudgenik      NudgenikResponse       `json:"nudgenik"`
		Terminal      TerminalResponse       `json:"terminal"`
		Internal      InternalResponse       `json:"internal"`
		NeedsRestart  bool                   `json:"needs_restart"`
	}

	repos := s.config.GetRepos()
	runTargets := s.config.GetRunTargets()
	quickLaunch := s.config.GetQuickLaunch()
	width, height := s.config.GetTerminalSize()
	seedLines := s.config.GetTerminalSeedLines()
	bootstrapLines := s.config.GetTerminalBootstrapLines()

	repoResp := make([]RepoResponse, len(repos))
	for i, repo := range repos {
		repoResp[i] = RepoResponse{Name: repo.Name, URL: repo.URL}
	}

	runTargetResp := make([]RunTargetResponse, len(runTargets))
	for i, target := range runTargets {
		runTargetResp[i] = RunTargetResponse{Name: target.Name, Type: target.Type, Command: target.Command, Source: target.Source}
	}
	quickLaunchResp := make([]QuickLaunchResponse, len(quickLaunch))
	for i, preset := range quickLaunch {
		quickLaunchResp[i] = QuickLaunchResponse{Name: preset.Name, Target: preset.Target, Prompt: preset.Prompt}
	}

	response := ConfigResponse{
		WorkspacePath: s.config.GetWorkspacePath(),
		Repos:         repoResp,
		RunTargets:    runTargetResp,
		QuickLaunch:   quickLaunchResp,
		Variants:      s.config.GetVariantConfigs(),
		Nudgenik:      NudgenikResponse{Target: s.config.GetNudgenikTarget()},
		Terminal:      TerminalResponse{Width: width, Height: height, SeedLines: seedLines, BootstrapLines: bootstrapLines},
		Internal: InternalResponse{
			MtimePollIntervalMs:     s.config.GetMtimePollIntervalMs(),
			SessionsPollIntervalMs:  s.config.GetSessionsPollIntervalMs(),
			ViewedBufferMs:          s.config.GetViewedBufferMs(),
			SessionSeenIntervalMs:   s.config.GetSessionSeenIntervalMs(),
			GitStatusPollIntervalMs: s.config.GetGitStatusPollIntervalMs(),
			GitCloneTimeoutSeconds:  s.config.GetGitCloneTimeoutSeconds(),
			GitStatusTimeoutSeconds: s.config.GetGitStatusTimeoutSeconds(),
			NetworkAccess:           s.config.GetNetworkAccess(),
		},
		NeedsRestart: s.state.GetNeedsRestart(),
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
	RunTargets []struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Command string `json:"command"`
		Source  string `json:"source,omitempty"`
	} `json:"run_targets,omitempty"`
	QuickLaunch []struct {
		Name   string  `json:"name"`
		Target string  `json:"target"`
		Prompt *string `json:"prompt"`
	} `json:"quick_launch,omitempty"`
	Variants []struct {
		Name    string            `json:"name"`
		Enabled *bool             `json:"enabled,omitempty"`
		Env     map[string]string `json:"env,omitempty"`
	} `json:"variants,omitempty"`
	Nudgenik *struct {
		Target *string `json:"target,omitempty"`
	} `json:"nudgenik,omitempty"`
	Terminal *struct {
		Width          *int `json:"width,omitempty"`
		Height         *int `json:"height,omitempty"`
		SeedLines      *int `json:"seed_lines,omitempty"`
		BootstrapLines *int `json:"bootstrap_lines,omitempty"`
	} `json:"terminal,omitempty"`
	Internal *struct {
		MtimePollIntervalMs     *int  `json:"mtime_poll_interval_ms,omitempty"`
		SessionsPollIntervalMs  *int  `json:"sessions_poll_interval_ms,omitempty"`
		ViewedBufferMs          *int  `json:"viewed_buffer_ms,omitempty"`
		SessionSeenIntervalMs   *int  `json:"session_seen_interval_ms,omitempty"`
		GitStatusPollIntervalMs *int  `json:"git_status_poll_interval_ms,omitempty"`
		GitCloneTimeoutSeconds  *int  `json:"git_clone_timeout_seconds,omitempty"`
		GitStatusTimeoutSeconds *int  `json:"git_status_timeout_seconds,omitempty"`
		NetworkAccess           *bool `json:"network_access,omitempty"`
	} `json:"internal,omitempty"`
}

// handleConfigUpdate handles config update requests.
func (s *Server) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	var req ConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[config] invalid JSON payload: %v", err)
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Reload config from disk to get all current values (including tools, etc.)
	if err := s.config.Reload(); err != nil {
		log.Printf("[config] failed to reload config: %v", err)
		http.Error(w, fmt.Sprintf("Failed to reload config: %v", err), http.StatusInternalServerError)
		return
	}

	cfg := s.config

	// Track if repos were updated for overlay dir creation
	reposUpdated := req.Repos != nil

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

	if req.RunTargets != nil {
		for _, target := range req.RunTargets {
			if target.Name == "" {
				http.Error(w, "run target name is required", http.StatusBadRequest)
				return
			}
			if target.Command == "" {
				http.Error(w, fmt.Sprintf("run target command is required for %s", target.Name), http.StatusBadRequest)
				return
			}
			if target.Source == config.RunTargetSourceDetected {
				http.Error(w, fmt.Sprintf("run target %s cannot be marked as detected", target.Name), http.StatusBadRequest)
				return
			}
			if target.Source != "" && target.Source != config.RunTargetSourceUser {
				http.Error(w, fmt.Sprintf("run target %s has invalid source %q", target.Name, target.Source), http.StatusBadRequest)
				return
			}
		}
		userTargets := make([]config.RunTarget, len(req.RunTargets))
		for i, t := range req.RunTargets {
			source := t.Source
			if source == "" {
				source = config.RunTargetSourceUser
			}
			userTargets[i] = config.RunTarget{Name: t.Name, Type: t.Type, Command: t.Command, Source: source}
		}
		detectedTools := detectedToolsFromRunTargets(cfg.GetDetectedRunTargets())
		cfg.RunTargets = config.MergeDetectedRunTargets(userTargets, detectedTools)
	}

	if req.QuickLaunch != nil {
		cfg.QuickLaunch = make([]config.QuickLaunch, len(req.QuickLaunch))
		for i, q := range req.QuickLaunch {
			cfg.QuickLaunch[i] = config.QuickLaunch{Name: q.Name, Target: q.Target, Prompt: q.Prompt}
		}
	}

	if req.Variants != nil {
		cfg.Variants = make([]config.VariantConfig, len(req.Variants))
		for i, v := range req.Variants {
			cfg.Variants[i] = config.VariantConfig{Name: v.Name, Enabled: v.Enabled, Env: v.Env}
		}
	}

	if req.Nudgenik != nil {
		target := ""
		if req.Nudgenik.Target != nil {
			target = strings.TrimSpace(*req.Nudgenik.Target)
		}
		if target == "" {
			cfg.Nudgenik = nil
		} else {
			cfg.Nudgenik = &config.NudgenikConfig{Target: target}
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
		if req.Internal.NetworkAccess != nil {
			// Check if network access is changing
			if *req.Internal.NetworkAccess != cfg.NetworkAccess {
				s.state.SetNeedsRestart(true)
				s.state.Save()
			}
			cfg.NetworkAccess = *req.Internal.NetworkAccess
		}
	}

	if err := cfg.Validate(); err != nil {
		log.Printf("[config] validation error: %v", err)
		http.Error(w, fmt.Sprintf("Invalid config: %v", err), http.StatusBadRequest)
		return
	}

	// Save config
	if err := cfg.Save(); err != nil {
		log.Printf("[config] failed to save config: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save config: %v", err), http.StatusInternalServerError)
		return
	}

	// Ensure overlay directories exist for all repos if repos were updated
	if reposUpdated {
		if err := s.workspace.EnsureOverlayDirs(cfg.GetRepos()); err != nil {
			log.Printf("[config] warning: failed to ensure overlay directories: %v", err)
			// Don't fail the request for this - overlay dirs can be created manually
		}
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

// handleVariants lists available variants.
func (s *Server) handleVariants(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type VariantResponse struct {
		Name            string   `json:"name"`
		DisplayName     string   `json:"display_name"`
		BaseTool        string   `json:"base_tool"`
		RequiredSecrets []string `json:"required_secrets"`
		UsageURL        string   `json:"usage_url"`
		Configured      bool     `json:"configured"`
	}

	available := s.config.GetAvailableVariants(detectedToolsFromConfig(s.config))
	resp := make([]VariantResponse, 0, len(available))
	for _, variant := range available {
		configured, err := variantConfigured(variant)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to read secrets: %v", err), http.StatusInternalServerError)
			return
		}
		resp = append(resp, VariantResponse{
			Name:            variant.Name,
			DisplayName:     variant.DisplayName,
			BaseTool:        variant.BaseTool,
			RequiredSecrets: variant.RequiredSecrets,
			UsageURL:        variant.UsageURL,
			Configured:      configured,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"variants": resp})
}

func detectedToolsFromConfig(cfg *config.Config) []detect.Tool {
	return detectedToolsFromRunTargets(cfg.GetDetectedRunTargets())
}

func detectedToolsFromRunTargets(targets []config.RunTarget) []detect.Tool {
	tools := make([]detect.Tool, 0, len(targets))
	for _, target := range targets {
		tools = append(tools, detect.Tool{
			Name:    target.Name,
			Command: target.Command,
			Source:  "config",
			Agentic: true,
		})
	}
	return tools
}

// handleVariant handles variant secret/configured requests.
func (s *Server) handleVariant(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/variants/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.Error(w, "variant name and action required", http.StatusBadRequest)
		return
	}
	name := parts[0]
	action := parts[1]

	variant, ok := detect.FindVariant(name)
	if !ok {
		http.Error(w, "variant not found", http.StatusNotFound)
		return
	}

	switch action {
	case "configured":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		configured, err := variantConfigured(variant)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to read secrets: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"configured": configured})
	case "secrets":
		switch r.Method {
		case http.MethodPost:
			type SecretsRequest struct {
				Secrets map[string]string `json:"secrets"`
			}
			var req SecretsRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
				return
			}
			if err := validateVariantSecrets(variant, req.Secrets); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := config.SaveVariantSecrets(variant.Name, req.Secrets); err != nil {
				http.Error(w, fmt.Sprintf("Failed to save secrets: %v", err), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case http.MethodDelete:
			if targetInUseByNudgenikOrQuickLaunch(s.config, variant.Name) {
				http.Error(w, "variant is in use by nudgenik or quick launch", http.StatusBadRequest)
				return
			}
			if err := config.DeleteVariantSecrets(variant.Name); err != nil {
				http.Error(w, fmt.Sprintf("Failed to delete secrets: %v", err), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
	default:
		http.Error(w, "unknown variant action", http.StatusNotFound)
	}
}

func targetInUseByNudgenikOrQuickLaunch(cfg *config.Config, targetName string) bool {
	if cfg == nil || targetName == "" {
		return false
	}
	if cfg.GetNudgenikTarget() == targetName {
		return true
	}
	for _, preset := range cfg.GetQuickLaunch() {
		if preset.Target == targetName {
			return true
		}
	}
	return false
}

func variantConfigured(variant detect.Variant) (bool, error) {
	secrets, err := config.GetVariantSecrets(variant.Name)
	if err != nil {
		return false, err
	}
	for _, key := range variant.RequiredSecrets {
		if strings.TrimSpace(secrets[key]) == "" {
			return false, nil
		}
	}
	return true, nil
}

func validateVariantSecrets(variant detect.Variant, secrets map[string]string) error {
	for _, key := range variant.RequiredSecrets {
		val := strings.TrimSpace(secrets[key])
		if val == "" {
			return fmt.Errorf("missing required secret %s", key)
		}
	}
	return nil
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

func getTargetPromptable(cfg *config.Config, detected []config.RunTarget, name string) (bool, bool) {
	if detect.IsVariantName(name) {
		for _, v := range cfg.GetVariantConfigs() {
			if v.Name == name {
				if v.Enabled != nil && !*v.Enabled {
					return false, false
				}
				break
			}
		}
		return true, true
	}
	if detect.IsBuiltinToolName(name) {
		for _, target := range detected {
			if target.Name == name {
				return true, true
			}
		}
		return true, false
	}
	if target, found := cfg.GetRunTarget(name); found {
		return target.Type == config.RunTargetTypePromptable, true
	}
	return false, false
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
	result, err := nudgenik.AskForSession(ctx, s.config, sess)
	if err != nil {
		switch {
		case errors.Is(err, nudgenik.ErrNoResponse):
			log.Printf("[ask-nudgenik] no response extracted from session %s", sessionID)
			http.Error(w, "No response found in session output", http.StatusBadRequest)
		case errors.Is(err, nudgenik.ErrTargetNotFound):
			log.Printf("[ask-nudgenik] target not found in config")
		case errors.Is(err, nudgenik.ErrTargetNoSecrets):
			log.Printf("[ask-nudgenik] target missing required secrets")
			http.Error(w, "Claude agent not found. Please run agent detection first.", http.StatusServiceUnavailable)
		default:
			log.Printf("[ask-nudgenik] failed to ask for session %s: %v", sessionID, err)
			http.Error(w, fmt.Sprintf("Failed to ask nudgenik: %v", err), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
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

// handleOverlays returns overlay information for all repos.
func (s *Server) handleOverlays(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type OverlayInfo struct {
		RepoName  string `json:"repo_name"`
		Path      string `json:"path"`
		Exists    bool   `json:"exists"`
		FileCount int    `json:"file_count"`
	}

	type Response struct {
		Overlays []OverlayInfo `json:"overlays"`
	}

	repos := s.config.GetRepos()
	overlays := make([]OverlayInfo, 0, len(repos))

	for _, repo := range repos {
		overlayDir, err := workspace.OverlayDir(repo.Name)
		if err != nil {
			log.Printf("[overlays] failed to get overlay directory for %s: %v", repo.Name, err)
			continue
		}

		// Check if overlay directory exists
		exists := true
		if _, err := os.Stat(overlayDir); os.IsNotExist(err) {
			exists = false
		}

		// Count files if directory exists
		fileCount := 0
		if exists {
			files, err := workspace.ListOverlayFiles(repo.Name)
			if err != nil {
				log.Printf("[overlays] failed to list overlay files for %s: %v", repo.Name, err)
			} else {
				fileCount = len(files)
			}
		}

		overlays = append(overlays, OverlayInfo{
			RepoName:  repo.Name,
			Path:      overlayDir,
			Exists:    exists,
			FileCount: fileCount,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Response{Overlays: overlays})
}

// handleRefreshOverlay handles POST requests to refresh overlay files for a workspace.
func (s *Server) handleRefreshOverlay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract workspace ID from URL: /api/workspaces/:id/refresh-overlay
	workspaceID := strings.TrimPrefix(r.URL.Path, "/api/workspaces/")
	workspaceID = strings.TrimSuffix(workspaceID, "/refresh-overlay")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetTmuxOperationTimeoutSeconds())*time.Second)
	defer cancel()

	if err := s.workspace.RefreshOverlay(ctx, workspaceID); err != nil {
		log.Printf("[refresh-overlay] error: workspace_id=%s error=%v", workspaceID, err)
		w.Header().Set("Content-Type", "application/json")
		// Return 400 for client errors (active sessions, not found)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	log.Printf("[refresh-overlay] success: workspace_id=%s", workspaceID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// BuiltinQuickLaunch represents a built-in quick launch preset.
// These are predefined quick-run shortcuts that ship with schmux.
type BuiltinQuickLaunch struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Prompt string `json:"prompt"`
}

// handleBuiltinQuickLaunch returns the list of built-in quick launch presets.
func (s *Server) handleBuiltinQuickLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Try embedded file first (production), fall back to filesystem (development)
	var data []byte
	var readErr error
	data, readErr = builtinQuickLaunchFS.ReadFile("builtin_quick_launch.json")
	if readErr != nil {
		// Fallback to filesystem for development
		candidates := []string{
			"./internal/dashboard/builtin_quick_launch.json",
			filepath.Join(filepath.Dir(os.Args[0]), "../internal/dashboard/builtin_quick_launch.json"),
		}
		for _, candidate := range candidates {
			data, readErr = os.ReadFile(candidate)
			if readErr == nil {
				break
			}
		}
		if readErr != nil {
			log.Printf("[builtin-quick-launch] failed to read file: %v", readErr)
			http.Error(w, "Failed to load built-in quick launch presets", http.StatusInternalServerError)
			return
		}
	}

	var presets []BuiltinQuickLaunch
	if err := json.Unmarshal(data, &presets); err != nil {
		log.Printf("[builtin-quick-launch] failed to parse: %v", err)
		http.Error(w, "Failed to parse built-in quick launch presets", http.StatusInternalServerError)
		return
	}

	// Validate and filter presets
	validPresets := make([]BuiltinQuickLaunch, 0, len(presets))
	for _, preset := range presets {
		if strings.TrimSpace(preset.Name) == "" {
			log.Printf("[builtin-quick-launch] skipping preset with empty name")
			continue
		}
		if strings.TrimSpace(preset.Target) == "" {
			log.Printf("[builtin-quick-launch] skipping preset %q with empty target", preset.Name)
			continue
		}
		if strings.TrimSpace(preset.Prompt) == "" {
			log.Printf("[builtin-quick-launch] skipping preset %q with empty prompt", preset.Name)
			continue
		}
		validPresets = append(validPresets, preset)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(validPresets)
}
