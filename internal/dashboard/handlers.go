package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"text/template"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/branchsuggest"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/difftool"
	"github.com/sergeknystautas/schmux/internal/nudgenik"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/update"
	"github.com/sergeknystautas/schmux/internal/vcs"
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

// maxBodySize is the maximum request body size for JSON requests (1MB).
const maxBodySize = 1 << 20

// SessionResponseItem represents a session in the API response.
type SessionResponseItem struct {
	ID           string `json:"id"`
	Target       string `json:"target"`
	Branch       string `json:"branch"`
	BranchURL    string `json:"branch_url,omitempty"`
	Nickname     string `json:"nickname,omitempty"`
	CreatedAt    string `json:"created_at"`
	LastOutputAt string `json:"last_output_at,omitempty"`
	Running      bool   `json:"running"`
	Status       string `json:"status,omitempty"` // "provisioning", "running", "failed" for remote sessions
	AttachCmd    string `json:"attach_cmd"`
	NudgeState   string `json:"nudge_state,omitempty"`
	NudgeSummary string `json:"nudge_summary,omitempty"`
	NudgeSeq     uint64 `json:"nudge_seq,omitempty"`
	// Remote session fields
	RemoteHostID     string `json:"remote_host_id,omitempty"`
	RemotePaneID     string `json:"remote_pane_id,omitempty"`
	RemoteHostname   string `json:"remote_hostname,omitempty"`
	RemoteFlavorName string `json:"remote_flavor_name,omitempty"`
}

// WorkspaceResponseItem represents a workspace in the API response.
type WorkspaceResponseItem struct {
	ID                      string                `json:"id"`
	Repo                    string                `json:"repo"`
	RepoName                string                `json:"repo_name,omitempty"`
	DefaultBranch           string                `json:"default_branch,omitempty"`
	Branch                  string                `json:"branch"`
	BranchURL               string                `json:"branch_url,omitempty"`
	Path                    string                `json:"path"`
	SessionCount            int                   `json:"session_count"`
	Sessions                []SessionResponseItem `json:"sessions"`
	QuickLaunch             []string              `json:"quick_launch,omitempty"`
	GitAhead                int                   `json:"git_ahead"`
	GitBehind               int                   `json:"git_behind"`
	GitLinesAdded           int                   `json:"git_lines_added"`
	GitLinesRemoved         int                   `json:"git_lines_removed"`
	GitFilesChanged         int                   `json:"git_files_changed"`
	RemoteHostID            string                `json:"remote_host_id,omitempty"`
	RemoteHostStatus        string                `json:"remote_host_status,omitempty"`
	RemoteFlavorName        string                `json:"remote_flavor_name,omitempty"`
	RemoteFlavor            string                `json:"remote_flavor,omitempty"`
	VCS                     string                `json:"vcs,omitempty"`                // "git", "sapling", etc. Omitted defaults to "git".
	ConflictOnBranch        string                `json:"conflict_on_branch,omitempty"` // Branch where sync conflict was detected
	CommitsSyncedWithRemote bool                  `json:"commits_synced_with_remote"`   // true if local HEAD matches origin/{branch}
}

// buildSessionsResponse builds the sessions/workspaces response data.
// Used by both the HTTP handler and WebSocket broadcast.
func (s *Server) buildSessionsResponse() []WorkspaceResponseItem {
	sessions := s.session.GetAllSessions()

	workspaceMap := make(map[string]*WorkspaceResponseItem)
	workspaces := s.state.GetWorkspaces()
	ctx := context.Background()
	for _, ws := range workspaces {
		// Only build branch URL if the branch exists on the remote
		branchURL := ""
		if wb, found := s.state.GetWorktreeBaseByURL(ws.Repo); found {
			if workspace.RemoteBranchExists(ctx, wb.Path, ws.Branch) {
				branchURL = workspace.BuildGitBranchURL(ws.Repo, ws.Branch)
			}
		}

		// Look up remote host status for remote workspaces
		branch := ws.Branch
		remoteHostID := ""
		remoteHostStatus := ""
		remoteFlavorName := ""
		remoteFlavor := ""
		vcs := ""
		if ws.RemoteHostID != "" {
			remoteHostID = ws.RemoteHostID
			if host, found := s.state.GetRemoteHost(ws.RemoteHostID); found {
				if host.Hostname != "" {
					branch = host.Hostname
				}
				// Use live connection status from remote manager if available,
				// since persisted state can be stale after daemon restarts.
				if s.remoteManager != nil {
					liveStatus, _ := s.remoteManager.GetHostConnectionStatus(ws.RemoteHostID)
					remoteHostStatus = liveStatus
				} else {
					remoteHostStatus = host.Status
				}
				if flavor, found := s.config.GetRemoteFlavor(host.FlavorID); found {
					remoteFlavorName = flavor.DisplayName
					remoteFlavor = flavor.Flavor
					vcs = flavor.VCS
				}
			} else {
				remoteHostStatus = state.RemoteHostStatusDisconnected
			}
		}

		var quickLaunchNames []string
		if cfg := s.workspace.GetWorkspaceConfig(ws.ID); cfg != nil && len(cfg.QuickLaunch) > 0 {
			quickLaunchNames = make([]string, 0, len(cfg.QuickLaunch))
			for _, preset := range cfg.QuickLaunch {
				if preset.Name != "" {
					quickLaunchNames = append(quickLaunchNames, preset.Name)
				}
			}
		}

		conflictOnBranch := ""
		if ws.ConflictOnBranch != nil {
			conflictOnBranch = *ws.ConflictOnBranch
		}

		// Get default branch from workspace manager
		defaultBranch := ""
		if db, err := s.workspace.GetDefaultBranch(ctx, ws.Repo); err == nil {
			defaultBranch = db
		}

		workspaceMap[ws.ID] = &WorkspaceResponseItem{
			ID:                      ws.ID,
			Repo:                    ws.Repo,
			DefaultBranch:           defaultBranch,
			Branch:                  branch,
			BranchURL:               branchURL,
			Path:                    ws.Path,
			SessionCount:            0,
			Sessions:                []SessionResponseItem{},
			QuickLaunch:             quickLaunchNames,
			GitAhead:                ws.GitAhead,
			GitBehind:               ws.GitBehind,
			GitLinesAdded:           ws.GitLinesAdded,
			GitLinesRemoved:         ws.GitLinesRemoved,
			GitFilesChanged:         ws.GitFilesChanged,
			RemoteHostID:            remoteHostID,
			RemoteHostStatus:        remoteHostStatus,
			RemoteFlavorName:        remoteFlavorName,
			RemoteFlavor:            remoteFlavor,
			VCS:                     vcs,
			ConflictOnBranch:        conflictOnBranch,
			CommitsSyncedWithRemote: ws.CommitsSyncedWithRemote,
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
			lastOutputAt = sess.LastOutputAt.Format(time.RFC3339)
		}
		timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(s.config.GetXtermQueryTimeoutMs())*time.Millisecond)
		running := s.session.IsRunning(timeoutCtx, sess.ID)
		cancel()
		nudgeState, nudgeSummary := parseNudgeSummary(sess.Nudge)

		// Get remote host info if this is a remote session
		var remoteHostname, remoteFlavorName string
		if sess.RemoteHostID != "" {
			if host, found := s.state.GetRemoteHost(sess.RemoteHostID); found {
				remoteHostname = host.Hostname
				if flavor, found := s.config.GetRemoteFlavor(host.FlavorID); found {
					remoteFlavorName = flavor.DisplayName
					// Build remote attach command from reconnect template + hostname
					if host.Hostname != "" {
						templateStr := flavor.GetReconnectCommandTemplate()
						if tmpl, err := template.New("attach").Parse(templateStr); err == nil {
							var cmdStr strings.Builder
							tmplData := struct {
								Hostname string
								Flavor   string
							}{Hostname: host.Hostname, Flavor: flavor.Flavor}
							if err := tmpl.Execute(&cmdStr, tmplData); err == nil {
								attachCmd = cmdStr.String()
							}
						}
					}
				}
			}
		}

		wsResp.Sessions = append(wsResp.Sessions, SessionResponseItem{
			ID:               sess.ID,
			Target:           sess.Target,
			Branch:           wsResp.Branch,
			BranchURL:        wsResp.BranchURL,
			Nickname:         sess.Nickname,
			CreatedAt:        sess.CreatedAt.Format(time.RFC3339),
			LastOutputAt:     lastOutputAt,
			Running:          running,
			Status:           sess.Status, // Expose session status for remote sessions
			AttachCmd:        attachCmd,
			NudgeState:       nudgeState,
			NudgeSummary:     nudgeSummary,
			NudgeSeq:         sess.NudgeSeq,
			RemoteHostID:     sess.RemoteHostID,
			RemotePaneID:     sess.RemotePaneID,
			RemoteHostname:   remoteHostname,
			RemoteFlavorName: remoteFlavorName,
		})
		wsResp.SessionCount = len(wsResp.Sessions)
	}

	// Convert map to slice and sort workspaces by repo name, then branch name
	response := make([]WorkspaceResponseItem, 0, len(workspaceMap))
	for _, ws := range workspaceMap {
		response = append(response, *ws)
	}
	sort.Slice(response, func(i, j int) bool {
		repoI := response[i].Repo
		repoJ := response[j].Repo
		if r, found := s.config.FindRepoByURL(response[i].Repo); found {
			repoI = r.Name
		}
		if r, found := s.config.FindRepoByURL(response[j].Repo); found {
			repoJ = r.Name
		}
		if repoI != repoJ {
			return repoI < repoJ
		}
		return response[i].Branch < response[j].Branch
	})

	// Sort sessions within each workspace by creation time (oldest first)
	for i := range response {
		sort.Slice(response[i].Sessions, func(j, k int) bool {
			timeJ, _ := time.Parse(time.RFC3339, response[i].Sessions[j].CreatedAt)
			timeK, _ := time.Parse(time.RFC3339, response[i].Sessions[k].CreatedAt)
			return timeJ.Before(timeK)
		})
	}

	return response
}

// handleSessions returns the list of workspaces and their sessions as JSON.
// Returns a hierarchical structure: workspaces -> sessions
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := s.buildSessionsResponse()
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
	if s.devMode {
		response["dev_mode"] = true
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
	Repo            string         `json:"repo"`
	Branch          string         `json:"branch"`
	Prompt          string         `json:"prompt"`
	Nickname        string         `json:"nickname,omitempty"`     // optional human-friendly name for sessions
	Targets         map[string]int `json:"targets"`                // target name -> quantity
	WorkspaceID     string         `json:"workspace_id,omitempty"` // optional: spawn into specific workspace
	Command         string         `json:"command,omitempty"`      // shell command to run directly (alternative to targets)
	QuickLaunchName string         `json:"quick_launch_name,omitempty"`
	Resume          bool           `json:"resume,omitempty"`           // resume mode: use agent's resume command
	RemoteFlavorID  string         `json:"remote_flavor_id,omitempty"` // optional: spawn on remote host
	NewBranch       string         `json:"new_branch,omitempty"`       // create new workspace with this branch from source workspace
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

	if req.QuickLaunchName != "" {
		if req.Command != "" || len(req.Targets) > 0 {
			http.Error(w, "cannot specify quick_launch_name with command or targets", http.StatusBadRequest)
			return
		}
		if req.WorkspaceID == "" {
			http.Error(w, "workspace_id is required for quick_launch_name", http.StatusBadRequest)
			return
		}
		resolved, err := s.resolveQuickLaunchByName(req.WorkspaceID, req.QuickLaunchName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Nickname == "" {
			req.Nickname = resolved.Name
		}
		if resolved.Command != "" {
			req.Command = resolved.Command
		} else {
			req.Targets = map[string]int{resolved.Target: 1}
			req.Prompt = resolved.Prompt
		}
	}

	// Auto-detect remote flavor when spawning into a remote workspace
	if req.WorkspaceID != "" && req.RemoteFlavorID == "" {
		if ws, found := s.state.GetWorkspace(req.WorkspaceID); found && ws.RemoteHostID != "" {
			if host, found := s.state.GetRemoteHost(ws.RemoteHostID); found {
				req.RemoteFlavorID = host.FlavorID
			}
		}
	}

	// Validate request
	// Remote spawns don't need repo/branch (they use the remote flavor's workspace)
	if req.WorkspaceID == "" && req.RemoteFlavorID == "" {
		// When not spawning into existing workspace and not remote, repo and branch are required
		if req.Repo == "" {
			http.Error(w, "repo is required (when not using --workspace or remote)", http.StatusBadRequest)
			return
		}
		if req.Branch == "" {
			http.Error(w, "branch is required (when not using --workspace or remote)", http.StatusBadRequest)
			return
		}
	}
	// Either command or targets must be provided
	if req.Command == "" && len(req.Targets) == 0 {
		http.Error(w, "either command or targets is required", http.StatusBadRequest)
		return
	}
	if req.Command != "" && len(req.Targets) > 0 {
		http.Error(w, "cannot specify both command and targets", http.StatusBadRequest)
		return
	}

	// Validate resume mode
	if req.Resume {
		if req.Command != "" {
			http.Error(w, "cannot use command mode with resume", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Prompt) != "" {
			http.Error(w, "cannot use prompt with resume mode", http.StatusBadRequest)
			return
		}
	}

	// Server-side branch conflict check for worktree mode
	// This catches race conditions where UI check passed but another spawn claimed the branch
	if req.WorkspaceID == "" && s.config.UseWorktrees() {
		for _, ws := range s.state.GetWorkspaces() {
			if ws.Repo == req.Repo && ws.Branch == req.Branch {
				http.Error(w, fmt.Sprintf("branch_conflict: branch %q is already in use by workspace %q", req.Branch, ws.ID), http.StatusConflict)
				return
			}
		}
	}

	// Spawn sessions
	type SessionResult struct {
		SessionID   string `json:"session_id"`
		WorkspaceID string `json:"workspace_id"`
		Target      string `json:"target,omitempty"`
		Command     string `json:"command,omitempty"`
		Prompt      string `json:"prompt,omitempty"`
		Nickname    string `json:"nickname,omitempty"`
		Error       string `json:"error,omitempty"`
	}

	results := make([]SessionResult, 0)

	// Handle command-based spawn (quick launch with shell command)
	if req.Command != "" {
		// Remote command spawns are not currently supported
		if req.RemoteFlavorID != "" {
			http.Error(w, "remote command spawns are not supported (only target-based spawns work on remote hosts)", http.StatusBadRequest)
			return
		}

		fmt.Printf("[session] spawn request: repo=%s branch=%s workspace_id=%s command=%q nickname=%q\n",
			req.Repo, req.Branch, req.WorkspaceID, req.Command, req.Nickname)

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitCloneTimeoutMs())*time.Millisecond)
		sess, err := s.session.SpawnCommand(ctx, req.Repo, req.Branch, req.Command, req.Nickname, req.WorkspaceID, req.NewBranch)
		cancel()

		if err != nil {
			results = append(results, SessionResult{
				Command:  req.Command,
				Nickname: req.Nickname,
				Error:    err.Error(),
			})
			fmt.Printf("[session] spawn error: command=%q error=%s\n", req.Command, err.Error())
		} else {
			results = append(results, SessionResult{
				SessionID:   sess.ID,
				WorkspaceID: sess.WorkspaceID,
				Command:     req.Command,
				Nickname:    sess.Nickname,
			})
			fmt.Printf("[session] spawn success: command=%q session_id=%s workspace_id=%s\n", req.Command, sess.ID, sess.WorkspaceID)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(results); err != nil {
			http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// Handle target-based spawn
	promptPreview := req.Prompt
	if len(promptPreview) > 100 {
		promptPreview = promptPreview[:100] + "..."
	}
	if req.RemoteFlavorID != "" {
		fmt.Printf("[session] spawn request (remote): flavor_id=%s targets=%v prompt=%q\n",
			req.RemoteFlavorID, req.Targets, promptPreview)
	} else {
		fmt.Printf("[session] spawn request (local): repo=%s branch=%s workspace_id=%s targets=%v prompt=%q\n",
			req.Repo, req.Branch, req.WorkspaceID, req.Targets, promptPreview)
	}

	// Calculate total sessions to spawn for global nickname numbering
	totalToSpawn := 0
	detected := s.config.GetDetectedRunTargets()
	for targetName, count := range req.Targets {
		promptable, found := config.IsTargetPromptable(s.config, detected, targetName)
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
		promptable, found := config.IsTargetPromptable(s.config, detected, targetName)
		if !found {
			results = append(results, SessionResult{
				Target: targetName,
				Error:  fmt.Sprintf("target not found: %s", targetName),
			})
			continue
		}
		if promptable && strings.TrimSpace(req.Prompt) == "" && !req.Resume {
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

			var sess *state.Session
			var err error

			// Route to remote or local spawn based on request
			if req.RemoteFlavorID != "" {
				// Remote spawn - use SpawnRemote()
				sess, err = s.session.SpawnRemote(ctx, req.RemoteFlavorID, targetName, req.Prompt, nickname)
			} else {
				// Local spawn - use existing Spawn()
				sess, err = s.session.Spawn(ctx, req.Repo, req.Branch, targetName, req.Prompt, nickname, req.WorkspaceID, req.Resume, req.NewBranch)
			}

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
	hasSuccess := false
	for _, r := range results {
		if r.Error != "" {
			fmt.Printf("[session] spawn error: target=%s error=%s\n", r.Target, r.Error)
		} else {
			fmt.Printf("[session] spawn success: target=%s session_id=%s workspace_id=%s\n", r.Target, r.SessionID, r.WorkspaceID)
			hasSuccess = true
		}
	}

	// Broadcast update to WebSocket clients
	if hasSuccess {
		go s.BroadcastSessions()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// handleSuggestBranch handles branch name suggestion requests.
func (s *Server) handleSuggestBranch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed"})
		return
	}

	start := time.Now()

	// Parse request
	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	// Check if branch suggestion is enabled
	if !branchsuggest.IsEnabled(s.config) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "Branch suggestion is not configured"})
		return
	}

	targetName := s.config.GetBranchSuggestTarget()
	fmt.Printf("[workspace] asking %s for branch suggestion\n", targetName)

	// Generate branch suggestion
	result, err := branchsuggest.AskForPrompt(r.Context(), s.config, req.Prompt)
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to generate branch suggestion: %v", err)})
		return
	}

	fmt.Printf("[workspace] suggest-branch ok: duration=%s\n", time.Since(start).Truncate(time.Millisecond))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handlePrepareBranchSpawn prepares spawn data for an existing branch.
// Gets commit log from the bare clone, generates a nickname via branch suggestion, and returns
// everything needed to populate the spawn form.
func (s *Server) handlePrepareBranchSpawn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed"})
		return
	}

	start := time.Now()

	var req struct {
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}
	if req.Repo == "" || req.Branch == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "repo and branch are required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Get commit subjects from bare clone
	subjects, err := s.workspace.GetBranchCommitLog(ctx, req.Repo, req.Branch, 20)
	if err != nil {
		fmt.Printf("[workspace] prepare-branch-spawn: failed to get commit log: %v\n", err)
		// Non-fatal: proceed without commit log
		subjects = nil
	}

	// Build the review prompt with commit history included
	prompt := "Review the current state of this branch and prepare to resume work.\n\n" +
		"1. Read any markdown or spec files in the repo root and docs/ to understand project context and goals\n" +
		"2. Run `git diff --stat main...HEAD` to compare this branch against where it diverged from main\n" +
		"3. Identify what's been completed, what's in progress, and what remains\n\n"

	if len(subjects) > 0 {
		prompt += "Here is the commit history on this branch:\n\n"
		for i, msg := range subjects {
			if i > 0 {
				prompt += "\n"
			}
			prompt += "---\n" + msg + "\n"
		}
		prompt += "---\n\n"
	}

	prompt += "Summarize your findings, then ask what to work on next."

	// Generate nickname from commit messages if branch suggestion is enabled
	nickname := ""
	if branchsuggest.IsEnabled(s.config) && len(subjects) > 0 {
		commitSummary := strings.Join(subjects, "\n")
		suggestionPrompt := fmt.Sprintf("Branch: %s\n\nCommit messages:\n%s", req.Branch, commitSummary)

		fmt.Printf("[workspace] prepare-branch-spawn: asking for nickname from %d commits\n", len(subjects))
		result, err := branchsuggest.AskForPrompt(ctx, s.config, suggestionPrompt)
		if err != nil {
			fmt.Printf("[workspace] prepare-branch-spawn: nickname suggestion failed: %v\n", err)
			// Non-fatal: proceed without nickname
		} else {
			nickname = result.Nickname
			fmt.Printf("[workspace] prepare-branch-spawn ok: duration=%s nickname=%q\n", time.Since(start).Truncate(time.Millisecond), nickname)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"repo":     req.Repo,
		"branch":   req.Branch,
		"prompt":   prompt,
		"nickname": nickname,
	})
}

// handleDispose handles session disposal requests.
func (s *Server) handleDispose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL: /api/sessions/{id}/dispose
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	sessionID := strings.TrimSuffix(path, "/dispose")
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

	// Broadcast update to WebSocket clients
	go s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleDisposeWorkspace handles workspace disposal requests.
func (s *Server) handleDisposeWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract workspace ID from URL: /api/workspaces/{id}/dispose
	path := strings.TrimPrefix(r.URL.Path, "/api/workspaces/")
	workspaceID := strings.TrimSuffix(path, "/dispose")
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

	// Broadcast update to WebSocket clients
	go s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleDisposeWorkspaceAll handles workspace disposal requests including all sessions.
func (s *Server) handleDisposeWorkspaceAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract workspace ID from URL: /api/workspaces/{id}/dispose-all
	path := strings.TrimPrefix(r.URL.Path, "/api/workspaces/")
	workspaceID := strings.TrimSuffix(path, "/dispose-all")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	// First, dispose all sessions in the workspace
	sessions := s.state.GetSessions()
	var sessionsDisposed []string
	for _, sess := range sessions {
		if sess.WorkspaceID == workspaceID {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
			if err := s.session.Dispose(ctx, sess.ID); err != nil {
				cancel()
				fmt.Printf("[workspace] dispose-all error: failed to dispose session %s: %v\n", sess.ID, err)
			} else {
				cancel()
				sessionsDisposed = append(sessionsDisposed, sess.ID)
				fmt.Printf("[workspace] dispose-all: disposed session %s\n", sess.ID)
			}
		}
	}

	// Then dispose the workspace
	if err := s.workspace.Dispose(workspaceID); err != nil {
		fmt.Printf("[workspace] dispose-all error: workspace_id=%s error=%v\n", workspaceID, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	fmt.Printf("[workspace] dispose-all success: workspace_id=%s sessions_disposed=%d\n", workspaceID, len(sessionsDisposed))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":            "ok",
		"sessions_disposed": len(sessionsDisposed),
	})
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

	// Broadcast update to WebSocket clients
	go s.BroadcastSessions()

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

	// Build repo response with default branch from cache
	ctx := r.Context()
	repoResp := make([]contracts.RepoWithConfig, len(repos))
	for i, repo := range repos {
		resp := contracts.RepoWithConfig{Name: repo.Name, URL: repo.URL}
		// Try to get default branch from cache (omit if not detected)
		if defaultBranch, err := s.workspace.GetDefaultBranch(ctx, repo.URL); err == nil {
			resp.DefaultBranch = defaultBranch
		}
		repoResp[i] = resp
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
		quickLaunchResp[i] = contracts.QuickLaunch{Name: preset.Name, Command: preset.Command, Target: preset.Target, Prompt: preset.Prompt}
	}

	externalDiffCommands := s.config.GetExternalDiffCommands()
	externalDiffCommandsResp := make([]contracts.ExternalDiffCommand, len(externalDiffCommands))
	for i, cmd := range externalDiffCommands {
		externalDiffCommandsResp[i] = contracts.ExternalDiffCommand{Name: cmd.Name, Command: cmd.Command}
	}

	// Build models list with full metadata
	models, err := buildAvailableModels(s.config)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read models: %v", err), http.StatusInternalServerError)
		return
	}

	// Add configured models as run targets
	for _, model := range models {
		if !model.Configured {
			continue
		}
		baseTarget, found := s.config.GetDetectedRunTarget(model.BaseTool)
		if !found {
			continue
		}
		if _, exists := seenTargets[model.ID]; exists {
			continue
		}
		runTargetResp = append(runTargetResp, contracts.RunTarget{
			Name:    model.ID,
			Type:    config.RunTargetTypePromptable,
			Command: baseTarget.Command,
			Source:  "model",
		})
		seenTargets[model.ID] = struct{}{}
	}

	response := contracts.ConfigResponse{
		WorkspacePath:              s.config.GetWorkspacePath(),
		SourceCodeManagement:       s.config.GetSourceCodeManagement(),
		Repos:                      repoResp,
		RunTargets:                 runTargetResp,
		QuickLaunch:                quickLaunchResp,
		ExternalDiffCommands:       externalDiffCommandsResp,
		ExternalDiffCleanupAfterMs: s.config.GetExternalDiffCleanupAfterMs(),
		Models:                     models,
		ModelVersions:              s.config.GetModelVersions(),
		Terminal:                   contracts.Terminal{Width: width, Height: height, SeedLines: seedLines, BootstrapLines: bootstrapLines},
		Nudgenik: contracts.Nudgenik{
			Target:         s.config.GetNudgenikTarget(),
			ViewedBufferMs: s.config.GetNudgenikViewedBufferMs(),
			SeenIntervalMs: s.config.GetNudgenikSeenIntervalMs(),
		},
		BranchSuggest: contracts.BranchSuggest{
			Target: s.config.GetBranchSuggestTarget(),
		},
		ConflictResolve: contracts.ConflictResolve{
			Target:    s.config.GetConflictResolveTarget(),
			TimeoutMs: s.config.GetConflictResolveTimeoutMs(),
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
		PrReview: contracts.PrReview{
			Target: s.config.GetPrReviewTarget(),
		},
		Notifications: contracts.Notifications{
			SoundDisabled:      !s.config.GetNotificationSoundEnabled(),
			ConfirmBeforeClose: s.config.GetConfirmBeforeClose(),
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
	oldRepos := cfg.GetRepos()

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

	if req.SourceCodeManagement != nil {
		scm := *req.SourceCodeManagement
		if scm != "" && scm != config.SourceCodeManagementGit && scm != config.SourceCodeManagementGitWorktree {
			http.Error(w, fmt.Sprintf("invalid source_code_management: %q (must be %q or %q)",
				scm, config.SourceCodeManagementGit, config.SourceCodeManagementGitWorktree), http.StatusBadRequest)
			return
		}
		cfg.SourceCodeManagement = scm
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
		detectedTools := config.DetectedToolsFromConfig(cfg)
		cfg.RunTargets = config.MergeDetectedRunTargets(userTargets, detectedTools)
	}

	if req.QuickLaunch != nil {
		cfg.QuickLaunch = make([]config.QuickLaunch, len(req.QuickLaunch))
		for i, q := range req.QuickLaunch {
			cfg.QuickLaunch[i] = config.QuickLaunch{Name: q.Name, Command: q.Command, Target: q.Target, Prompt: q.Prompt}
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

	if req.ConflictResolve != nil {
		if cfg.ConflictResolve == nil {
			cfg.ConflictResolve = &config.ConflictResolveConfig{}
		}
		if req.ConflictResolve.Target != nil {
			cfg.ConflictResolve.Target = strings.TrimSpace(*req.ConflictResolve.Target)
		}
		if req.ConflictResolve.TimeoutMs != nil && *req.ConflictResolve.TimeoutMs > 0 {
			cfg.ConflictResolve.TimeoutMs = *req.ConflictResolve.TimeoutMs
		}
		if cfg.ConflictResolve.Target == "" && cfg.ConflictResolve.TimeoutMs <= 0 {
			cfg.ConflictResolve = nil
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

	if req.PrReview != nil {
		if cfg.PrReview == nil {
			cfg.PrReview = &config.PrReviewConfig{}
		}
		if req.PrReview.Target != nil {
			cfg.PrReview.Target = *req.PrReview.Target
		}
	}

	if req.Notifications != nil {
		if cfg.Notifications == nil {
			cfg.Notifications = &config.NotificationsConfig{}
		}
		if req.Notifications.SoundDisabled != nil {
			cfg.Notifications.SoundDisabled = *req.Notifications.SoundDisabled
		}
		if req.Notifications.ConfirmBeforeClose != nil {
			cfg.Notifications.ConfirmBeforeClose = *req.Notifications.ConfirmBeforeClose
		}
	}

	if req.ModelVersions != nil {
		cfg.SetModelVersions(*req.ModelVersions)
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

	// Update PR discovery polling based on new config
	// Pass a function so poll always uses current repos list
	s.prDiscovery.SetTarget(cfg.GetPrReviewTarget(), func() []config.Repo { return cfg.GetRepos() })

	// Ensure overlay directories exist for all repos if repos were actually updated
	newRepos := cfg.GetRepos()
	if !reposEqual(oldRepos, newRepos) {
		if err := s.workspace.EnsureOverlayDirs(newRepos); err != nil {
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

// handleModels lists available models.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp, err := buildAvailableModels(s.config)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read models: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"models": resp})
}

func buildAvailableModels(cfg *config.Config) ([]contracts.Model, error) {
	available := cfg.GetAvailableModels(config.DetectedToolsFromConfig(cfg))
	versions := cfg.GetModelVersions()
	resp := make([]contracts.Model, 0, len(available))
	for _, model := range available {
		configured, err := modelConfigured(model)
		if err != nil {
			return nil, err
		}
		pinnedVersion := ""
		if versions != nil {
			pinnedVersion = versions[model.ID]
		}
		resp = append(resp, contracts.Model{
			ID:              model.ID,
			DisplayName:     model.DisplayName,
			BaseTool:        model.BaseTool,
			Provider:        model.Provider,
			Category:        model.Category,
			RequiredSecrets: model.RequiredSecrets,
			UsageURL:        model.UsageURL,
			Configured:      configured,
			PinnedVersion:   pinnedVersion,
			DefaultValue:    model.ModelValue,
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

// reposEqual compares two slices of repos for equality.
func reposEqual(a, b []config.Repo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].URL != b[i].URL {
			return false
		}
	}
	return true
}

// handleModel handles model secret/configured requests.
func (s *Server) handleModel(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/models/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.Error(w, "model name and action required", http.StatusBadRequest)
		return
	}
	name := parts[0]
	action := parts[1]

	model, ok := detect.FindModel(name)
	if !ok {
		http.Error(w, "model not found", http.StatusNotFound)
		return
	}

	switch action {
	case "configured":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		configured, err := modelConfigured(model)
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
			if err := validateModelSecrets(model, req.Secrets); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := config.SaveModelSecrets(model.ID, req.Secrets); err != nil {
				http.Error(w, fmt.Sprintf("Failed to save secrets: %v", err), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case http.MethodDelete:
			if targetInUseByNudgenikOrQuickLaunch(s.config, model.ID) {
				http.Error(w, "model is in use by nudgenik or quick launch", http.StatusBadRequest)
				return
			}
			if model.Provider != "" && model.Provider != "anthropic" {
				if err := config.DeleteProviderSecrets(model.Provider); err != nil {
					http.Error(w, fmt.Sprintf("Failed to delete secrets: %v", err), http.StatusInternalServerError)
					return
				}
			} else {
				if err := config.DeleteModelSecrets(model.ID); err != nil {
					http.Error(w, fmt.Sprintf("Failed to delete secrets: %v", err), http.StatusInternalServerError)
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
	default:
		http.Error(w, "unknown model action", http.StatusNotFound)
	}
}

func targetInUseByNudgenikOrQuickLaunch(cfg *config.Config, targetName string) bool {
	if cfg == nil || targetName == "" {
		return false
	}

	// Normalize to canonical model ID if targetName is a model or alias
	canonicalName := targetName
	if model, ok := detect.FindModel(targetName); ok {
		canonicalName = model.ID
	}

	if cfg.GetNudgenikTarget() == canonicalName {
		return true
	}
	for _, preset := range cfg.GetQuickLaunch() {
		if preset.Target == canonicalName {
			return true
		}
		// Also check if preset.Target is an alias that resolves to this model
		if model, ok := detect.FindModel(preset.Target); ok && model.ID == canonicalName {
			return true
		}
	}
	return false
}

func modelConfigured(model detect.Model) (bool, error) {
	secrets, err := config.GetEffectiveModelSecrets(model)
	if err != nil {
		return false, err
	}
	for _, key := range model.RequiredSecrets {
		if strings.TrimSpace(secrets[key]) == "" {
			return false, nil
		}
	}
	return true, nil
}

func validateModelSecrets(model detect.Model, secrets map[string]string) error {
	for _, key := range model.RequiredSecrets {
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
		fmt.Printf("[workspace] warning: failed to update git status: %v\n", err)
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
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetGitStatusTimeoutMs())*time.Millisecond)
			oldExists := s.getFileContent(ctx, ws.Path, filePath, "HEAD") != ""
			cancel()
			if !oldExists {
				status = "added"
			}
			files = append(files, FileDiff{
				NewPath:  filePath,
				Status:   status,
				IsBinary: true,
			})
			continue
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

type resolvedQuickLaunch struct {
	Name    string
	Command string
	Target  string
	Prompt  string
}

func (s *Server) resolveQuickLaunchByName(workspaceID, name string) (*resolvedQuickLaunch, error) {
	if name == "" {
		return nil, fmt.Errorf("quick_launch_name is required")
	}
	detected := s.config.GetDetectedRunTargets()
	if wsCfg := s.workspace.GetWorkspaceConfig(workspaceID); wsCfg != nil {
		if resolved := resolveQuickLaunchFromPresets(wsCfg.QuickLaunch, detected, s.config, name); resolved != nil {
			return resolved, nil
		}
	}
	if resolved := resolveQuickLaunchFromPresets(adaptQuickLaunch(s.config.GetQuickLaunch()), detected, s.config, name); resolved != nil {
		return resolved, nil
	}
	return nil, fmt.Errorf("quick launch not found: %s", name)
}

func resolveQuickLaunchFromPresets(presets []contracts.QuickLaunch, detected []config.RunTarget, cfg *config.Config, name string) *resolvedQuickLaunch {
	for _, preset := range presets {
		if preset.Name != name {
			continue
		}
		if strings.TrimSpace(preset.Command) != "" {
			return &resolvedQuickLaunch{Name: preset.Name, Command: strings.TrimSpace(preset.Command)}
		}
		if strings.TrimSpace(preset.Target) == "" {
			return nil
		}
		promptable, found := config.IsTargetPromptable(cfg, detected, preset.Target)
		if !found {
			return nil
		}
		prompt := ""
		if preset.Prompt != nil {
			prompt = strings.TrimSpace(*preset.Prompt)
		}
		if promptable && prompt == "" {
			return nil
		}
		if !promptable && prompt != "" {
			return nil
		}
		return &resolvedQuickLaunch{Name: preset.Name, Target: preset.Target, Prompt: prompt}
	}
	return nil
}

func adaptQuickLaunch(presets []config.QuickLaunch) []contracts.QuickLaunch {
	if len(presets) == 0 {
		return nil
	}
	converted := make([]contracts.QuickLaunch, 0, len(presets))
	for _, preset := range presets {
		converted = append(converted, contracts.QuickLaunch{
			Name:    preset.Name,
			Command: preset.Command,
			Target:  preset.Target,
			Prompt:  preset.Prompt,
		})
	}
	return converted
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

// handleRemoteGitGraph handles git graph requests for remote workspaces.
func (s *Server) handleRemoteGitGraph(w http.ResponseWriter, r *http.Request, ws state.Workspace, maxTotal int, mainContext int) {
	if s.remoteManager == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "remote manager not available"})
		return
	}

	conn := s.remoteManager.GetConnection(ws.RemoteHostID)
	if conn == nil || !conn.IsConnected() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "remote host not connected"})
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "cannot resolve HEAD"})
		return
	}
	localHead := strings.TrimSpace(localHeadOutput)
	if !isValidVCSHash(localHead) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("HEAD resolved to invalid hash: %q", localHead)})
		return
	}

	originMainHead := ""
	if out, err := conn.RunCommand(ctx, workdir, cb.ResolveRef(defaultBranchRef)); err == nil {
		trimmed := strings.TrimSpace(out)
		if isValidVCSHash(trimmed) {
			originMainHead = trimmed
		} else {
			fmt.Printf("[remote git-graph] ignoring invalid default branch ref output: %q\n", trimmed)
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
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("log failed: %v", err)})
			return
		}
		logOutput = out
	} else if forkPoint == "" {
		out, err := conn.RunCommand(ctx, workdir, cb.Log([]string{"HEAD", defaultBranchRef}, maxTotal))
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("log failed: %v", err)})
			return
		}
		logOutput = out
	} else {
		// Divergence: get local commits + context (no main-ahead data)
		// Get local commits from HEAD
		out, err := conn.RunCommand(ctx, workdir, cb.Log([]string{"HEAD"}, 50))
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("log failed: %v", err)})
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

	resp := workspace.BuildGraphResponse(rawNodes, localBranch, defaultBranch, localHead, originMainHead, forkPoint, branchWorkspaces, ws.Repo, maxTotal, mainAheadCount)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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

	// Use ResolveVSCodePath to find VS Code command
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
					s.state.Save()
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
				fmt.Printf("[session] open-vscode: using flavor-specific template for %s\n", flavor.DisplayName)
			}
		}
		// Fall back to global template if no flavor-specific template
		if templateStr == "" {
			templateStr = s.config.GetRemoteVSCodeCommandTemplate()
			fmt.Printf("[session] open-vscode: using global template\n")
		}

		// Parse template
		tmpl, err := template.New("vscode").Parse(templateStr)
		if err != nil {
			fmt.Printf("[session] open-vscode: template parse error: %v\n", err)
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
			fmt.Printf("[session] open-vscode: template execution error: %v\n", err)
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
			fmt.Printf("[session] open-vscode: failed to parse command: %v\n", err)
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

		fmt.Printf("[session] open-vscode (remote): executing: %s\n", cmdLine)
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

		fmt.Printf("[session] open-vscode (local): %s\n", ws.Path)
		cmd = exec.Command(vscodePath.Path, "-n", ws.Path)
	}

	// Execute command
	// Note: We don't wait for the command to complete since VS Code opens as a separate process
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
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

	fmt.Printf("[session] diff-external (remote): launching %q for %d files in workspace %s\n", selectedCommand, len(files), ws.ID)

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
				fmt.Printf("[session] diff-external (remote): failed to get old file %s: %v\n", file.path, err)
				continue
			}
			newContent, err := conn.RunCommand(ctx, workdir, cb.FileContent(file.path))
			if err != nil {
				fmt.Printf("[session] diff-external (remote): failed to get new file %s: %v\n", file.path, err)
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
				fmt.Printf("[session] diff-external (remote): diff tool error: %v\n", err)
			} else {
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
				fmt.Printf("[session] diff-external (remote): diff tool error: %v\n", err)
			} else {
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
			fmt.Printf("[session] diff-external (remote): failed to remove temp dir: %v\n", err)
		}
	})

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

// handleCheckBranchConflict checks if a branch is already in use by a worktree.
// POST /api/check-branch-conflict
// Request body: {"repo": "git@github.com:user/repo.git", "branch": "main"}
// Response: {"conflict": false} or {"conflict": true, "workspace_id": "repo-001"}
func (s *Server) handleCheckBranchConflict(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Repo == "" || req.Branch == "" {
		http.Error(w, "repo and branch are required", http.StatusBadRequest)
		return
	}

	type BranchConflictResponse struct {
		Conflict    bool   `json:"conflict"`
		WorkspaceID string `json:"workspace_id,omitempty"`
	}

	// If not using worktrees, there's no branch conflict concern
	if !s.config.UseWorktrees() {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(BranchConflictResponse{Conflict: false})
		return
	}

	// Check if any existing workspace has this repo+branch combination
	// (which means the branch is already checked out in a worktree)
	for _, ws := range s.state.GetWorkspaces() {
		if ws.Repo == req.Repo && ws.Branch == req.Branch {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(BranchConflictResponse{
				Conflict:    true,
				WorkspaceID: ws.ID,
			})
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(BranchConflictResponse{Conflict: false})
}

// handleRecentBranches returns recent branches from all configured repos.
// GET /api/recent-branches?limit=10
func (s *Server) handleRecentBranches(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse limit from query string, default to 10
	limit := 10
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	// Cap limit
	if limit > 50 {
		limit = 50
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	branches, err := s.workspace.GetRecentBranches(ctx, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get recent branches: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(branches)
}

// handleWorkspaceGitGraph handles GET /api/workspaces/{id}/git-graph.
func (s *Server) handleWorkspaceGitGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract workspace ID: /api/workspaces/{id}/git-graph
	path := strings.TrimPrefix(r.URL.Path, "/api/workspaces/")
	workspaceID := strings.TrimSuffix(path, "/git-graph")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	// Verify workspace exists
	ws, ok := s.state.GetWorkspace(workspaceID)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "workspace not found: " + workspaceID})
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

	// Delegate to remote handler if this is a remote workspace
	if ws.RemoteHostID != "" {
		s.handleRemoteGitGraph(w, r, ws, maxTotal, mainContext)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := s.workspace.GetGitGraph(ctx, workspaceID, maxTotal, mainContext)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Populate dirty state from workspace git stats
	if ws.GitFilesChanged > 0 {
		resp.DirtyState = &contracts.GitGraphDirtyState{
			FilesChanged: ws.GitFilesChanged,
			LinesAdded:   ws.GitLinesAdded,
			LinesRemoved: ws.GitLinesRemoved,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// validateGitFilePaths checks that none of the file paths contain path traversal
// components (e.g., "../"). Returns an error message if any path is invalid.
func validateGitFilePaths(files []string) string {
	for _, f := range files {
		cleaned := filepath.Clean(f)
		if cleaned == "." || filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
			return fmt.Sprintf("invalid file path: %q", f)
		}
	}
	return ""
}

// handleGitCommitStage handles POST /api/workspaces/{id}/git-commit-stage.
// Stages the specified files for commit.
func (s *Server) handleGitCommitStage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/workspaces/")
	workspaceID := strings.TrimSuffix(path, "/git-commit-stage")
	if workspaceID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "workspace ID is required"})
		return
	}

	ws, ok := s.state.GetWorkspace(workspaceID)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "workspace not found"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		Files []string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	if msg := validateGitFilePaths(req.Files); msg != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": msg})
		return
	}

	ctx := context.Background()
	for _, file := range req.Files {
		cmd := exec.CommandContext(ctx, "git", "add", "--", file)
		cmd.Dir = ws.Path
		if output, err := cmd.CombinedOutput(); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("git add failed: %s", string(output))})
			return
		}
	}

	if _, err := s.workspace.UpdateGitStatus(ctx, ws.ID); err != nil {
		fmt.Printf("[dashboard] failed to update git status after stage: %v\n", err)
	}
	s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Files staged"})
}

// handleGitAmend handles POST /api/workspaces/{id}/git-amend.
// Stages the specified files and amends the last commit.
func (s *Server) handleGitAmend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/workspaces/")
	workspaceID := strings.TrimSuffix(path, "/git-amend")
	if workspaceID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "workspace ID is required"})
		return
	}

	ws, ok := s.state.GetWorkspace(workspaceID)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "workspace not found"})
		return
	}

	if ws.GitAhead <= 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "No commits to amend"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		Files []string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	if len(req.Files) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "at least one file is required"})
		return
	}

	if msg := validateGitFilePaths(req.Files); msg != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": msg})
		return
	}

	ctx := context.Background()
	for _, file := range req.Files {
		cmd := exec.CommandContext(ctx, "git", "add", "--", file)
		cmd.Dir = ws.Path
		if output, err := cmd.CombinedOutput(); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("git add failed: %s", string(output))})
			return
		}
	}

	cmd := exec.CommandContext(ctx, "git", "commit", "--amend", "--no-edit")
	cmd.Dir = ws.Path
	if output, err := cmd.CombinedOutput(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("git commit --amend failed: %s", string(output))})
		return
	}

	if _, err := s.workspace.UpdateGitStatus(ctx, ws.ID); err != nil {
		fmt.Printf("[dashboard] failed to update git status after amend: %v\n", err)
	}
	s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Commit amended"})
}

// handleGitDiscard handles POST /api/workspaces/{id}/git-discard.
// Discards local changes. If files are specified, only those files are discarded.
func (s *Server) handleGitDiscard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/workspaces/")
	workspaceID := strings.TrimSuffix(path, "/git-discard")
	if workspaceID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "workspace ID is required"})
		return
	}

	ws, ok := s.state.GetWorkspace(workspaceID)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "workspace not found"})
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
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
			return
		}
	}

	if len(req.Files) > 0 {
		if msg := validateGitFilePaths(req.Files); msg != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": msg})
			return
		}
	}

	ctx := context.Background()

	fmt.Printf("[git-discard] workspace=%s path=%s files=%v\n", ws.ID, ws.Path, req.Files)

	if len(req.Files) > 0 {
		// Discard specific files
		for _, file := range req.Files {
			// Try git checkout HEAD for tracked files (restores from HEAD, undoing both staging and edits)
			cmd := exec.CommandContext(ctx, "git", "checkout", "HEAD", "--", file)
			cmd.Dir = ws.Path
			output, err := cmd.CombinedOutput()
			if err != nil {
				fmt.Printf("[git-discard] checkout HEAD failed for %q: %s\n", file, strings.TrimSpace(string(output)))
				// File might be staged-new (added but not in HEAD) — unstage and remove
				cmd = exec.CommandContext(ctx, "git", "rm", "-f", "--cached", "--", file)
				cmd.Dir = ws.Path
				if output2, err2 := cmd.CombinedOutput(); err2 == nil {
					fmt.Printf("[git-discard] unstaged new file %q\n", file)
					// Now remove the working tree copy
					filePath := filepath.Join(ws.Path, file)
					if rmErr := os.Remove(filePath); rmErr != nil {
						fmt.Printf("[git-discard] failed to remove working tree file %q: %v\n", file, rmErr)
					}
				} else {
					fmt.Printf("[git-discard] rm --cached failed for %q: %s\n", file, strings.TrimSpace(string(output2)))
					// Last resort: try git clean for truly untracked files
					cmd = exec.CommandContext(ctx, "git", "clean", "-f", "--", file)
					cmd.Dir = ws.Path
					if output3, err3 := cmd.CombinedOutput(); err3 != nil {
						fmt.Printf("[git-discard] clean also failed for %q: %s\n", file, strings.TrimSpace(string(output3)))
					} else {
						fmt.Printf("[git-discard] cleaned untracked file %q\n", file)
					}
				}
			} else {
				fmt.Printf("[git-discard] restored %q from HEAD\n", file)
			}
		}
	} else {
		// Discard all changes
		cmd := exec.CommandContext(ctx, "git", "clean", "-fd")
		cmd.Dir = ws.Path
		if output, err := cmd.CombinedOutput(); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("git clean failed: %s", string(output))})
			return
		}

		cmd = exec.CommandContext(ctx, "git", "checkout", "--", ".")
		cmd.Dir = ws.Path
		if output, err := cmd.CombinedOutput(); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("git checkout failed: %s", string(output))})
			return
		}
	}

	if _, err := s.workspace.UpdateGitStatus(ctx, ws.ID); err != nil {
		fmt.Printf("[dashboard] failed to update git status after discard: %v\n", err)
	}
	s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Changes discarded"})
}

// shellSplit splits a command line string into arguments, respecting quotes.
// Handles single quotes, double quotes, and backslash escaping.
// This prevents breakage when workspace paths contain spaces.
func shellSplit(input string) ([]string, error) {
	var args []string
	var current strings.Builder
	var inSingleQuote, inDoubleQuote bool
	var escaped bool

	for i := 0; i < len(input); i++ {
		c := input[i]

		if escaped {
			// Previous char was backslash, add this char literally
			current.WriteByte(c)
			escaped = false
			continue
		}

		switch c {
		case '\\':
			if inSingleQuote {
				// Backslash is literal in single quotes
				current.WriteByte(c)
			} else {
				// Set escaped flag for next character
				escaped = true
			}

		case '\'':
			if inDoubleQuote {
				// Single quote is literal inside double quotes
				current.WriteByte(c)
			} else {
				// Toggle single quote mode
				inSingleQuote = !inSingleQuote
			}

		case '"':
			if inSingleQuote {
				// Double quote is literal inside single quotes
				current.WriteByte(c)
			} else {
				// Toggle double quote mode
				inDoubleQuote = !inDoubleQuote
			}

		case ' ', '\t', '\n', '\r':
			if inSingleQuote || inDoubleQuote {
				// Whitespace is literal inside quotes
				current.WriteByte(c)
			} else {
				// Whitespace outside quotes separates arguments
				if current.Len() > 0 {
					args = append(args, current.String())
					current.Reset()
				}
			}

		default:
			current.WriteByte(c)
		}
	}

	// Handle unterminated quotes
	if inSingleQuote || inDoubleQuote {
		return nil, fmt.Errorf("unterminated quote in command")
	}

	// Add final argument if any
	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args, nil
}
