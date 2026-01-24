package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/branchsuggest"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/difftool"
	"github.com/sergeknystautas/schmux/internal/nudgenik"
	"github.com/sergeknystautas/schmux/internal/update"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

//go:embed cookbooks.json
var cookbooksFS embed.FS

// handleApp serves the React application entry point for UI routes.
func (s *Server) handleApp(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
		http.NotFound(w, r)
		return
	}
	if !s.requireAuthOrRedirect(w, r) {
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
		BranchURL    string `json:"branch_url,omitempty"`
		Nickname     string `json:"nickname,omitempty"`
		CreatedAt    string `json:"created_at"`
		LastOutputAt string `json:"last_output_at,omitempty"`
		Running      bool   `json:"running"`
		AttachCmd    string `json:"attach_cmd"`
		NudgeState   string `json:"nudge_state,omitempty"`
		NudgeSummary string `json:"nudge_summary,omitempty"`
	}

	type WorkspaceResponse struct {
		ID              string            `json:"id"`
		Repo            string            `json:"repo"`
		Branch          string            `json:"branch"`
		BranchURL       string            `json:"branch_url,omitempty"`
		Path            string            `json:"path"`
		SessionCount    int               `json:"session_count"`
		Sessions        []SessionResponse `json:"sessions"`
		GitAhead        int               `json:"git_ahead"`
		GitBehind       int               `json:"git_behind"`
		GitLinesAdded   int               `json:"git_lines_added"`
		GitLinesRemoved int               `json:"git_lines_removed"`
		GitFilesChanged int               `json:"git_files_changed"`
	}

	workspaceMap := make(map[string]*WorkspaceResponse)
	workspaces := s.state.GetWorkspaces()
	for _, ws := range workspaces {
		// Only build branch URL if the branch has a remote tracking branch (upstream)
		branchURL := ""
		if workspace.BranchHasUpstream(r.Context(), ws.Path) {
			branchURL = workspace.BuildGitBranchURL(ws.Repo, ws.Branch)
		}

		workspaceMap[ws.ID] = &WorkspaceResponse{
			ID:              ws.ID,
			Repo:            ws.Repo,
			Branch:          ws.Branch,
			BranchURL:       branchURL,
			Path:            ws.Path,
			SessionCount:    0,
			Sessions:        []SessionResponse{},
			GitAhead:        ws.GitAhead,
			GitBehind:       ws.GitBehind,
			GitLinesAdded:   ws.GitLinesAdded,
			GitLinesRemoved: ws.GitLinesRemoved,
			GitFilesChanged: ws.GitFilesChanged,
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
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermQueryTimeoutMs())*time.Millisecond)
		running := s.session.IsRunning(ctx, sess.ID)
		cancel()
		nudgeState, nudgeSummary := parseNudgeSummary(sess.Nudge)
		wsResp.Sessions = append(wsResp.Sessions, SessionResponse{
			ID:           sess.ID,
			Target:       sess.Target,
			Branch:       wsResp.Branch,
			BranchURL:    wsResp.BranchURL,
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

// handleHealthz returns a simple health check response with version info.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	v := s.GetVersionInfo()
	response := map[string]any{
		"status":  "ok",
		"version": v.Current,
	}
	if v.Latest != "" {
		response["latest_version"] = v.Latest
		response["update_available"] = v.UpdateAvailable
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleUpdate triggers an update and shuts down the daemon.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Prevent concurrent updates
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updateInProgress {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "update already in progress"})
		return
	}
	s.updateInProgress = true

	fmt.Printf("[daemon] update requested via web UI\n")

	// Run update synchronously so we can report actual success/failure
	if err := update.Update(); err != nil {
		s.updateInProgress = false
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("update failed: %v", err)})
		return
	}

	fmt.Printf("[daemon] update successful, shutting down\n")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Update successful. Restart schmux to use the new version.",
	})

	// Shutdown after sending response
	if s.shutdown != nil {
		go s.shutdown()
	}
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
	fmt.Printf("[session] spawn request: repo=%s branch=%s workspace_id=%s targets=%v prompt=%q\n",
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
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitCloneTimeoutMs())*time.Millisecond)
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
			fmt.Printf("[session] spawn error: target=%s error=%s\n", r.Target, r.Error)
		} else {
			fmt.Printf("[session] spawn success: target=%s session_id=%s workspace_id=%s\n", r.Target, r.SessionID, r.WorkspaceID)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// handleSuggestBranch handles branch name suggestion requests.
func (s *Server) handleSuggestBranch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	start := time.Now()

	// Parse request
	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Check if branch suggestion is enabled
	if !branchsuggest.IsEnabled(s.config) {
		http.Error(w, "Branch suggestion is not configured", http.StatusServiceUnavailable)
		return
	}

	// Generate branch suggestion
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := branchsuggest.AskForPrompt(ctx, s.config, req.Prompt)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, branchsuggest.ErrNoPrompt):
			status = http.StatusBadRequest
		case errors.Is(err, branchsuggest.ErrTargetNotFound):
			status = http.StatusNotFound
		case errors.Is(err, branchsuggest.ErrDisabled):
			status = http.StatusServiceUnavailable
		case errors.Is(err, branchsuggest.ErrInvalidBranch), errors.Is(err, branchsuggest.ErrInvalidResponse):
			status = http.StatusBadRequest
		}
		fmt.Printf("[workspace] suggest-branch error: duration=%s status=%d err=%v\n", time.Since(start).Truncate(time.Millisecond), status, err)
		http.Error(w, fmt.Sprintf("Failed to generate branch suggestion: %v", err), status)
		return
	}

	fmt.Printf("[workspace] suggest-branch ok: duration=%s\n", time.Since(start).Truncate(time.Millisecond))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
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

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
	if err := s.session.Dispose(ctx, sessionID); err != nil {
		cancel()
		fmt.Printf("[session] dispose error: session_id=%s error=%v\n", sessionID, err)
		http.Error(w, fmt.Sprintf("Failed to dispose session: %v", err), http.StatusInternalServerError)
		return
	}
	cancel()
	fmt.Printf("[session] dispose success: session_id=%s\n", sessionID)

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
		fmt.Printf("[workspace] dispose error: workspace_id=%s error=%v\n", workspaceID, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest) // 400 for client-side errors like dirty state
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	fmt.Printf("[workspace] dispose success: workspace_id=%s\n", workspaceID)

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
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
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
	repos := s.config.GetRepos()
	runTargets := s.config.GetRunTargets()
	quickLaunch := s.config.GetQuickLaunch()
	width, height := s.config.GetTerminalSize()
	seedLines := s.config.GetTerminalSeedLines()
	bootstrapLines := s.config.GetTerminalBootstrapLines()

	repoResp := make([]contracts.Repo, len(repos))
	for i, repo := range repos {
		repoResp[i] = contracts.Repo{Name: repo.Name, URL: repo.URL}
	}

	runTargetResp := make([]contracts.RunTarget, 0, len(runTargets))
	seenTargets := make(map[string]struct{}, len(runTargets))
	for _, target := range runTargets {
		runTargetResp = append(runTargetResp, contracts.RunTarget{
			Name:    target.Name,
			Type:    target.Type,
			Command: target.Command,
			Source:  target.Source,
		})
		seenTargets[target.Name] = struct{}{}
	}
	quickLaunchResp := make([]contracts.QuickLaunch, len(quickLaunch))
	for i, preset := range quickLaunch {
		quickLaunchResp[i] = contracts.QuickLaunch{Name: preset.Name, Target: preset.Target, Prompt: preset.Prompt}
	}

	externalDiffCommands := s.config.GetExternalDiffCommands()
	externalDiffCommandsResp := make([]contracts.ExternalDiffCommand, len(externalDiffCommands))
	for i, cmd := range externalDiffCommands {
		externalDiffCommandsResp[i] = contracts.ExternalDiffCommand{Name: cmd.Name, Command: cmd.Command}
	}

	variants := make([]contracts.Variant, len(s.config.GetVariantConfigs()))
	for i, variant := range s.config.GetVariantConfigs() {
		variants[i] = contracts.Variant{Name: variant.Name, Enabled: variant.Enabled, Env: variant.Env}
	}
	availableVariants, err := buildAvailableVariants(s.config)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read variants: %v", err), http.StatusInternalServerError)
		return
	}
	for _, variant := range availableVariants {
		if !variant.Configured {
			continue
		}
		baseTarget, found := s.config.GetDetectedRunTarget(variant.BaseTool)
		if !found {
			continue
		}
		if _, exists := seenTargets[variant.Name]; exists {
			continue
		}
		runTargetResp = append(runTargetResp, contracts.RunTarget{
			Name:    variant.Name,
			Type:    config.RunTargetTypePromptable,
			Command: baseTarget.Command,
			Source:  "variant",
		})
		seenTargets[variant.Name] = struct{}{}
	}

	response := contracts.ConfigResponse{
		WorkspacePath:              s.config.GetWorkspacePath(),
		Repos:                      repoResp,
		RunTargets:                 runTargetResp,
		QuickLaunch:                quickLaunchResp,
		ExternalDiffCommands:       externalDiffCommandsResp,
		ExternalDiffCleanupAfterMs: s.config.GetExternalDiffCleanupAfterMs(),
		Variants:                   variants,
		Terminal:                   contracts.Terminal{Width: width, Height: height, SeedLines: seedLines, BootstrapLines: bootstrapLines},
		Nudgenik: contracts.Nudgenik{
			Target:         s.config.GetNudgenikTarget(),
			ViewedBufferMs: s.config.GetNudgenikViewedBufferMs(),
			SeenIntervalMs: s.config.GetNudgenikSeenIntervalMs(),
		},
		BranchSuggest: contracts.BranchSuggest{
			Target: s.config.GetBranchSuggestTarget(),
		},
		Sessions: contracts.Sessions{
			DashboardPollIntervalMs: s.config.GetDashboardPollIntervalMs(),
			GitStatusPollIntervalMs: s.config.GetGitStatusPollIntervalMs(),
			GitCloneTimeoutMs:       s.config.GetGitCloneTimeoutMs(),
			GitStatusTimeoutMs:      s.config.GetGitStatusTimeoutMs(),
		},
		Xterm: contracts.Xterm{
			MtimePollIntervalMs: s.config.GetXtermMtimePollIntervalMs(),
			QueryTimeoutMs:      s.config.GetXtermQueryTimeoutMs(),
			OperationTimeoutMs:  s.config.GetXtermOperationTimeoutMs(),
			MaxLogSizeMB:        int(s.config.GetXtermMaxLogSizeMB()),
			RotatedLogSizeMB:    int(s.config.GetXtermRotatedLogSizeMB()),
		},
		Network: contracts.Network{
			BindAddress:   s.config.GetBindAddress(),
			Port:          s.config.GetPort(),
			PublicBaseURL: s.config.GetPublicBaseURL(),
			TLS:           buildTLS(s.config),
		},
		AccessControl: contracts.AccessControl{
			Enabled:           s.config.GetAuthEnabled(),
			Provider:          s.config.GetAuthProvider(),
			SessionTTLMinutes: s.config.GetAuthSessionTTLMinutes(),
		},
		NeedsRestart: s.state.GetNeedsRestart(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleConfigUpdate handles config update requests.
func (s *Server) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	var req contracts.ConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fmt.Printf("[config] invalid JSON payload: %v\n", err)
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Reload config from disk to get all current values (including tools, etc.)
	if err := s.config.Reload(); err != nil {
		fmt.Printf("[config] failed to reload config: %v\n", err)
		http.Error(w, fmt.Sprintf("Failed to reload config: %v", err), http.StatusInternalServerError)
		return
	}

	cfg := s.config
	oldNetwork := cloneNetwork(cfg.Network)
	oldAccessControl := cloneAccessControl(cfg.AccessControl)

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

	if req.ExternalDiffCommands != nil {
		cfg.ExternalDiffCommands = make([]config.ExternalDiffCommand, len(req.ExternalDiffCommands))
		for i, c := range req.ExternalDiffCommands {
			cfg.ExternalDiffCommands[i] = config.ExternalDiffCommand{Name: c.Name, Command: c.Command}
		}
	}

	if req.ExternalDiffCleanupAfterMs != nil {
		if *req.ExternalDiffCleanupAfterMs <= 0 {
			http.Error(w, "external diff cleanup delay must be > 0", http.StatusBadRequest)
			return
		}
		cfg.ExternalDiffCleanupAfterMs = *req.ExternalDiffCleanupAfterMs
	}

	if req.Variants != nil {
		cfg.Variants = make([]config.VariantConfig, len(req.Variants))
		for i, v := range req.Variants {
			cfg.Variants[i] = config.VariantConfig{Name: v.Name, Enabled: v.Enabled, Env: v.Env}
		}
	}

	if req.Nudgenik != nil {
		if cfg.Nudgenik == nil {
			cfg.Nudgenik = &config.NudgenikConfig{}
		}
		if req.Nudgenik.Target != nil {
			target := strings.TrimSpace(*req.Nudgenik.Target)
			cfg.Nudgenik.Target = target
		}
		if req.Nudgenik.ViewedBufferMs != nil && *req.Nudgenik.ViewedBufferMs > 0 {
			cfg.Nudgenik.ViewedBufferMs = *req.Nudgenik.ViewedBufferMs
		}
		if req.Nudgenik.SeenIntervalMs != nil && *req.Nudgenik.SeenIntervalMs > 0 {
			cfg.Nudgenik.SeenIntervalMs = *req.Nudgenik.SeenIntervalMs
		}
		if cfg.Nudgenik.Target == "" && cfg.Nudgenik.ViewedBufferMs <= 0 && cfg.Nudgenik.SeenIntervalMs <= 0 {
			cfg.Nudgenik = nil
		}
	}

	if req.BranchSuggest != nil {
		if cfg.BranchSuggest == nil {
			cfg.BranchSuggest = &config.BranchSuggestConfig{}
		}
		if req.BranchSuggest.Target != nil {
			cfg.BranchSuggest.Target = strings.TrimSpace(*req.BranchSuggest.Target)
		}
		if cfg.BranchSuggest.Target == "" {
			cfg.BranchSuggest = nil
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

	if req.Sessions != nil {
		if cfg.Sessions == nil {
			cfg.Sessions = &config.SessionsConfig{}
		}
		if req.Sessions.DashboardPollIntervalMs != nil && *req.Sessions.DashboardPollIntervalMs > 0 {
			cfg.Sessions.DashboardPollIntervalMs = *req.Sessions.DashboardPollIntervalMs
		}
		if req.Sessions.GitStatusPollIntervalMs != nil && *req.Sessions.GitStatusPollIntervalMs > 0 {
			cfg.Sessions.GitStatusPollIntervalMs = *req.Sessions.GitStatusPollIntervalMs
		}
		if req.Sessions.GitCloneTimeoutMs != nil && *req.Sessions.GitCloneTimeoutMs > 0 {
			cfg.Sessions.GitCloneTimeoutMs = *req.Sessions.GitCloneTimeoutMs
		}
		if req.Sessions.GitStatusTimeoutMs != nil && *req.Sessions.GitStatusTimeoutMs > 0 {
			cfg.Sessions.GitStatusTimeoutMs = *req.Sessions.GitStatusTimeoutMs
		}
	}

	if req.Xterm != nil {
		if cfg.Xterm == nil {
			cfg.Xterm = &config.XtermConfig{}
		}
		if req.Xterm.MtimePollIntervalMs != nil && *req.Xterm.MtimePollIntervalMs > 0 {
			cfg.Xterm.MtimePollIntervalMs = *req.Xterm.MtimePollIntervalMs
		}
		if req.Xterm.QueryTimeoutMs != nil && *req.Xterm.QueryTimeoutMs > 0 {
			cfg.Xterm.QueryTimeoutMs = *req.Xterm.QueryTimeoutMs
		}
		if req.Xterm.OperationTimeoutMs != nil && *req.Xterm.OperationTimeoutMs > 0 {
			cfg.Xterm.OperationTimeoutMs = *req.Xterm.OperationTimeoutMs
		}
		if req.Xterm.MaxLogSizeMB != nil && *req.Xterm.MaxLogSizeMB > 0 {
			cfg.Xterm.MaxLogSizeMB = *req.Xterm.MaxLogSizeMB
		}
		if req.Xterm.RotatedLogSizeMB != nil && *req.Xterm.RotatedLogSizeMB > 0 {
			cfg.Xterm.RotatedLogSizeMB = *req.Xterm.RotatedLogSizeMB
		}
	}

	if req.Network != nil {
		if cfg.Network == nil {
			cfg.Network = &config.NetworkConfig{}
		}
		if req.Network.BindAddress != nil {
			cfg.Network.BindAddress = *req.Network.BindAddress
		}
		if req.Network.Port != nil && *req.Network.Port > 0 {
			cfg.Network.Port = *req.Network.Port
		}
		if req.Network.PublicBaseURL != nil {
			cfg.Network.PublicBaseURL = *req.Network.PublicBaseURL
		}
		if req.Network.TLS != nil {
			if cfg.Network.TLS == nil {
				cfg.Network.TLS = &config.TLSConfig{}
			}
			if req.Network.TLS.CertPath != nil {
				cfg.Network.TLS.CertPath = *req.Network.TLS.CertPath
			}
			if req.Network.TLS.KeyPath != nil {
				cfg.Network.TLS.KeyPath = *req.Network.TLS.KeyPath
			}
		}
	}

	if req.AccessControl != nil {
		if cfg.AccessControl == nil {
			cfg.AccessControl = &config.AccessControlConfig{}
		}
		if req.AccessControl.Enabled != nil {
			cfg.AccessControl.Enabled = *req.AccessControl.Enabled
		}
		if req.AccessControl.Provider != nil {
			cfg.AccessControl.Provider = *req.AccessControl.Provider
		}
		if req.AccessControl.SessionTTLMinutes != nil {
			cfg.AccessControl.SessionTTLMinutes = *req.AccessControl.SessionTTLMinutes
		}
	}

	warnings, err := cfg.ValidateForSave()
	if err != nil {
		fmt.Printf("[config] validation error: %v\n", err)
		http.Error(w, fmt.Sprintf("Invalid config: %v", err), http.StatusBadRequest)
		return
	}

	if !reflect.DeepEqual(oldNetwork, cfg.Network) || !reflect.DeepEqual(oldAccessControl, cfg.AccessControl) {
		s.state.SetNeedsRestart(true)
		s.state.Save()
	}

	// Save config
	if err := cfg.Save(); err != nil {
		fmt.Printf("[config] failed to save config: %v\n", err)
		http.Error(w, fmt.Sprintf("Failed to save config: %v", err), http.StatusInternalServerError)
		return
	}

	// Ensure overlay directories exist for all repos if repos were updated
	if reposUpdated {
		if err := s.workspace.EnsureOverlayDirs(cfg.GetRepos()); err != nil {
			fmt.Printf("[workspace] warning: failed to ensure overlay directories: %v\n", err)
			// Don't fail the request for this - overlay dirs can be created manually
		}
	}

	// Return warning if path changed with existing sessions/workspaces
	if pathChanged {
		type WarningResponse struct {
			Warning         string   `json:"warning"`
			SessionCount    int      `json:"session_count"`
			WorkspaceCount  int      `json:"workspace_count"`
			RequiresRestart bool     `json:"requires_restart"`
			Warnings        []string `json:"warnings,omitempty"`
		}
		warning := WarningResponse{
			Warning:         fmt.Sprintf("Changing workspace_path affects only NEW workspaces. %d existing sessions and %d workspaces will keep their current paths.", sessionCount, workspaceCount),
			SessionCount:    sessionCount,
			WorkspaceCount:  workspaceCount,
			RequiresRestart: true,
			Warnings:        warnings,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(warning)
		return
	}

	type ConfigSaveResponse struct {
		Status   string   `json:"status"`
		Message  string   `json:"message"`
		Warnings []string `json:"warnings,omitempty"`
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ConfigSaveResponse{
		Status:   "ok",
		Message:  "Config saved and reloaded. Changes are now in effect.",
		Warnings: warnings,
	})
}

// handleAuthSecrets gets or sets GitHub auth secrets.
func (s *Server) handleAuthSecrets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		secrets, err := config.GetAuthSecrets()
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to read secrets: %v", err), http.StatusInternalServerError)
			return
		}
		clientIDSet := false
		clientSecretSet := false
		if secrets.GitHub != nil {
			clientIDSet = strings.TrimSpace(secrets.GitHub.ClientID) != ""
			clientSecretSet = strings.TrimSpace(secrets.GitHub.ClientSecret) != ""
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{
			"client_id_set":     clientIDSet,
			"client_secret_set": clientSecretSet,
		})
	case http.MethodPost:
		type SecretsRequest struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		}
		var req SecretsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.ClientID) == "" || strings.TrimSpace(req.ClientSecret) == "" {
			http.Error(w, "client_id and client_secret are required", http.StatusBadRequest)
			return
		}
		if err := config.SaveGitHubAuthSecrets(req.ClientID, req.ClientSecret); err != nil {
			http.Error(w, fmt.Sprintf("Failed to save secrets: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleVariants lists available variants.
func (s *Server) handleVariants(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp, err := buildAvailableVariants(s.config)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read variants: %v", err), http.StatusInternalServerError)
		return
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

func buildAvailableVariants(cfg *config.Config) ([]contracts.AvailableVariant, error) {
	available := cfg.GetAvailableVariants(detectedToolsFromConfig(cfg))
	resp := make([]contracts.AvailableVariant, 0, len(available))
	for _, variant := range available {
		configured, err := variantConfigured(variant)
		if err != nil {
			return nil, err
		}
		resp = append(resp, contracts.AvailableVariant{
			Name:            variant.Name,
			DisplayName:     variant.DisplayName,
			BaseTool:        variant.BaseTool,
			RequiredSecrets: variant.RequiredSecrets,
			UsageURL:        variant.UsageURL,
			Configured:      configured,
		})
	}
	return resp, nil
}

func buildTLS(cfg *config.Config) *contracts.TLS {
	certPath := cfg.GetTLSCertPath()
	keyPath := cfg.GetTLSKeyPath()
	if certPath == "" && keyPath == "" {
		return nil
	}
	return &contracts.TLS{
		CertPath: certPath,
		KeyPath:  keyPath,
	}
}

func cloneNetwork(src *config.NetworkConfig) *config.NetworkConfig {
	if src == nil {
		return nil
	}
	cpy := *src
	if src.TLS != nil {
		tlsCopy := *src.TLS
		cpy.TLS = &tlsCopy
	}
	return &cpy
}

func cloneAccessControl(src *config.AccessControlConfig) *config.AccessControlConfig {
	if src == nil {
		return nil
	}
	cpy := *src
	return &cpy
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
		OldPath      string `json:"old_path,omitempty"`
		NewPath      string `json:"new_path,omitempty"`
		OldContent   string `json:"old_content,omitempty"`
		NewContent   string `json:"new_content,omitempty"`
		Status       string `json:"status,omitempty"` // added, modified, deleted, renamed
		LinesAdded   int    `json:"lines_added"`
		LinesRemoved int    `json:"lines_removed"`
	}

	type DiffResponse struct {
		WorkspaceID string     `json:"workspace_id"`
		Repo        string     `json:"repo"`
		Branch      string     `json:"branch"`
		Files       []FileDiff `json:"files"`
	}

	// Get git diff output using porcelain format
	// --numstat shows: added/deleted lines filename
	// HEAD compares against last commit (includes both staged and unstaged)
	// --find-renames finds renames
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutMs())*time.Millisecond)
	cmd := exec.CommandContext(ctx, "git", "-C", ws.Path, "diff", "HEAD", "--numstat", "--find-renames", "--diff-filter=ADM")
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

		addedStr := parts[0]
		deletedStr := parts[1]
		filePath := parts[2]

		// Parse line counts (may be "-" for binary files)
		linesAdded := 0
		linesRemoved := 0
		if addedStr != "-" {
			linesAdded, _ = strconv.Atoi(addedStr)
		}
		if deletedStr != "-" {
			linesRemoved, _ = strconv.Atoi(deletedStr)
		}

		// Skip if file was deleted (added is "-")
		if addedStr == "-" && deletedStr != "-" {
			// For deleted files, get old content
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutMs())*time.Millisecond)
			oldContent := s.getFileContent(ctx, ws.Path, filePath, "HEAD")
			cancel()
			files = append(files, FileDiff{
				NewPath:      filePath,
				OldContent:   oldContent,
				Status:       "deleted",
				LinesAdded:   linesAdded,
				LinesRemoved: linesRemoved,
			})
			continue
		}

		// Check if file is new (deleted is "0" and file doesn't exist in HEAD)
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutMs())*time.Millisecond)
		newContent := s.getFileContent(ctx, ws.Path, filePath, "worktree")
		oldContent := s.getFileContent(ctx, ws.Path, filePath, "HEAD")
		cancel()

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
			// Get content of untracked file from working directory
			newContent := s.getFileContent(context.Background(), ws.Path, filePath, "worktree")
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

	// Use ResolveVSCodePath to find VS Code command
	// This handles PATH, shell aliases, and well-known installation locations
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	vscodePath, found := detect.ResolveVSCodePath(ctx)
	if !found {
		fmt.Printf("[session] open-vscode: command not found\n")
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

	fmt.Printf("[session] open-vscode: found via %s: %s\n", vscodePath.Source, vscodePath.Path)

	// Execute code command
	// Note: We don't wait for the command to complete since VS Code opens as a separate process
	cmd := exec.Command(vscodePath.Path, "-n", ws.Path)
	if err := cmd.Start(); err != nil {
		fmt.Printf("[session] open-vscode: failed to launch: %v\n", err)
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

// handleDiffExternal handles POST requests to open an external diff tool for a workspace.
// POST /api/diff-external/{workspaceId}
//
// Request body: {"command": "ksdiff"} (optional, defaults to first configured command)
//
// The command can use placeholders:
//
//	{old_file} - path to the old version of the file (from HEAD)
//	{new_file} - path to the new version of the file (from worktree)
//	{file}     - path to the file in worktree (for new/deleted files)
//
// Examples:
//
//	"code --diff {old_file} {new_file}"  - VS Code
//	"ksdiff {old_file} {new_file}"      - Kaleidoscope
//	"git difftool {file}"               - git difftool (configured externally)
func (s *Server) handleDiffExternal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract workspace ID from URL: /api/diff-external/{workspace-id}
	workspaceID := strings.TrimPrefix(r.URL.Path, "/api/diff-external/")
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		fmt.Printf("[session] diff-external: failed to decode request: %v\n", err)
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
		fmt.Printf("[session] diff-external: no command specified and no external diff commands configured\n")
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

	fmt.Printf("[session] diff-external: launching %q for %d files in workspace %s\n", selectedCommand, len(files), workspaceID)

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
		fmt.Printf("[session] diff-external: failed to create temp dir: %v\n", err)
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
				fmt.Printf("[session] diff-external: failed to create temp dir for file: %v\n", err)
				continue
			}
			tmpFile, err := os.Create(tmpPath)
			if err != nil {
				fmt.Printf("[session] diff-external: failed to create temp file: %v\n", err)
				continue
			}

			// Get old file content from git
			showCmd := exec.CommandContext(ctx, "git", "-C", ws.Path, "show", oldPath)
			showOutput, err := showCmd.Output()
			if err != nil {
				tmpFile.Close()
				os.Remove(tmpPath)
				fmt.Printf("[session] diff-external: failed to get old file: %v\n", err)
				continue
			}
			if _, err := tmpFile.Write(showOutput); err != nil {
				tmpFile.Close()
				os.Remove(tmpPath)
				fmt.Printf("[session] diff-external: failed to write temp file: %v\n", err)
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
				fmt.Printf("[session] diff-external: diff tool exited with error: %v\n", err)
			} else {
				opened++
			}

		case "deleted":
			oldPath := fmt.Sprintf("HEAD:%s", file.path)
			mergedPath := filepath.Join(ws.Path, file.path)
			tmpPath := filepath.Join(tempRoot, file.path)
			if err := os.MkdirAll(filepath.Dir(tmpPath), 0o755); err != nil {
				fmt.Printf("[session] diff-external: failed to create temp dir for file: %v\n", err)
				continue
			}
			tmpFile, err := os.Create(tmpPath)
			if err != nil {
				fmt.Printf("[session] diff-external: failed to create temp file: %v\n", err)
				continue
			}

			showCmd := exec.CommandContext(ctx, "git", "-C", ws.Path, "show", oldPath)
			showOutput, err := showCmd.Output()
			if err != nil {
				tmpFile.Close()
				os.Remove(tmpPath)
				fmt.Printf("[session] diff-external: failed to get old file: %v\n", err)
				continue
			}
			if _, err := tmpFile.Write(showOutput); err != nil {
				tmpFile.Close()
				os.Remove(tmpPath)
				fmt.Printf("[session] diff-external: failed to write temp file: %v\n", err)
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
				fmt.Printf("[session] diff-external: diff tool exited with error: %v\n", err)
			} else {
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
			fmt.Printf("[session] diff-external: failed to remove temp dir: %v\n", err)
		}
	})

	// Success response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(DiffExternalResponse{
		Success: true,
		Message: fmt.Sprintf("Opened %d files in external diff tool", opened),
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
		case errors.Is(err, nudgenik.ErrDisabled):
			fmt.Printf("[nudgenik] nudgenik is disabled\n")
			http.Error(w, "Nudgenik is disabled. Configure a target in settings.", http.StatusServiceUnavailable)
		case errors.Is(err, nudgenik.ErrNoResponse):
			fmt.Printf("[nudgenik] no response extracted from session %s\n", sessionID)
			http.Error(w, "No response found in session output", http.StatusBadRequest)
		case errors.Is(err, nudgenik.ErrTargetNotFound):
			fmt.Printf("[nudgenik] target not found in config\n")
			http.Error(w, "Nudgenik target not found", http.StatusServiceUnavailable)
		case errors.Is(err, nudgenik.ErrTargetNoSecrets):
			fmt.Printf("[nudgenik] target missing required secrets\n")
			http.Error(w, "Nudgenik target missing required secrets", http.StatusServiceUnavailable)
		default:
			fmt.Printf("[nudgenik] failed to ask for session %s: %v\n", sessionID, err)
			http.Error(w, fmt.Sprintf("Failed to ask nudgenik: %v", err), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleHasNudgenik handles GET requests to check if nudgenik is available globally.
// Returns available: true only when a nudgenik target is configured.
func (s *Server) handleHasNudgenik(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	available := nudgenik.IsEnabled(s.config)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"available": available})
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
			fmt.Printf("[workspace] failed to get overlay directory for %s: %v\n", repo.Name, err)
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
				fmt.Printf("[workspace] failed to list overlay files for %s: %v\n", repo.Name, err)
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

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
	defer cancel()

	if err := s.workspace.RefreshOverlay(ctx, workspaceID); err != nil {
		fmt.Printf("[workspace] refresh-overlay error: workspace_id=%s error=%v\n", workspaceID, err)
		w.Header().Set("Content-Type", "application/json")
		// Return 400 for client errors (active sessions, not found)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	fmt.Printf("[workspace] refresh-overlay success: workspace_id=%s\n", workspaceID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// BuiltinQuickLaunchCookbook represents a built-in quick launch cookbook entry.
// These are predefined quick-run shortcuts that ship with schmux.
type BuiltinQuickLaunchCookbook struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Prompt string `json:"prompt"`
}

// handleBuiltinQuickLaunch returns the list of built-in quick launch cookbooks.
func (s *Server) handleBuiltinQuickLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Try embedded file first (production), fall back to filesystem (development)
	var data []byte
	var readErr error
	data, readErr = cookbooksFS.ReadFile("cookbooks.json")
	if readErr != nil {
		// Fallback to filesystem for development
		candidates := []string{
			"./internal/dashboard/cookbooks.json",
			filepath.Join(filepath.Dir(os.Args[0]), "../internal/dashboard/cookbooks.json"),
		}
		for _, candidate := range candidates {
			data, readErr = os.ReadFile(candidate)
			if readErr == nil {
				break
			}
		}
		if readErr != nil {
			fmt.Printf("[session] builtin-quick-launch: failed to read file: %v\n", readErr)
			http.Error(w, "Failed to load built-in quick launch cookbooks", http.StatusInternalServerError)
			return
		}
	}

	var cookbooks []BuiltinQuickLaunchCookbook
	if err := json.Unmarshal(data, &cookbooks); err != nil {
		fmt.Printf("[session] builtin-quick-launch: failed to parse: %v\n", err)
		http.Error(w, "Failed to parse built-in quick launch cookbooks", http.StatusInternalServerError)
		return
	}

	// Validate and filter cookbooks
	validCookbooks := make([]BuiltinQuickLaunchCookbook, 0, len(cookbooks))
	for _, cookbook := range cookbooks {
		if strings.TrimSpace(cookbook.Name) == "" {
			fmt.Printf("[session] builtin-quick-launch: skipping cookbook with empty name\n")
			continue
		}
		if strings.TrimSpace(cookbook.Target) == "" {
			fmt.Printf("[session] builtin-quick-launch: skipping cookbook %q with empty target\n", cookbook.Name)
			continue
		}
		if strings.TrimSpace(cookbook.Prompt) == "" {
			fmt.Printf("[session] builtin-quick-launch: skipping cookbook %q with empty prompt\n", cookbook.Name)
			continue
		}
		validCookbooks = append(validCookbooks, cookbook)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(validCookbooks)
}
