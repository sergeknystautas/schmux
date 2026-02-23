package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/sergeknystautas/schmux/internal/nudgenik"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

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
	ID                       string                `json:"id"`
	Repo                     string                `json:"repo"`
	RepoName                 string                `json:"repo_name,omitempty"`
	DefaultBranch            string                `json:"default_branch,omitempty"`
	Branch                   string                `json:"branch"`
	BranchURL                string                `json:"branch_url,omitempty"`
	Path                     string                `json:"path"`
	SessionCount             int                   `json:"session_count"`
	Sessions                 []SessionResponseItem `json:"sessions"`
	QuickLaunch              []string              `json:"quick_launch,omitempty"`
	GitAhead                 int                   `json:"git_ahead"`
	GitBehind                int                   `json:"git_behind"`
	GitLinesAdded            int                   `json:"git_lines_added"`
	GitLinesRemoved          int                   `json:"git_lines_removed"`
	GitFilesChanged          int                   `json:"git_files_changed"`
	RemoteHostID             string                `json:"remote_host_id,omitempty"`
	RemoteHostStatus         string                `json:"remote_host_status,omitempty"`
	RemoteFlavorName         string                `json:"remote_flavor_name,omitempty"`
	RemoteFlavor             string                `json:"remote_flavor,omitempty"`
	VCS                      string                `json:"vcs,omitempty"`                // "git", "sapling", etc. Omitted defaults to "git".
	ConflictOnBranch         string                `json:"conflict_on_branch,omitempty"` // Branch where sync conflict was detected
	CommitsSyncedWithRemote  bool                  `json:"commits_synced_with_remote"`   // true if local HEAD matches origin/{branch}
	GitDefaultBranchOrphaned bool                  `json:"git_default_branch_orphaned"`  // true if origin/default has no common ancestor with HEAD
	RemoteBranchExists       bool                  `json:"remote_branch_exists"`         // true if origin/{branch} exists
	LocalUniqueCommits       int                   `json:"local_unique_commits"`         // commits in local not in remote
	RemoteUniqueCommits      int                   `json:"remote_unique_commits"`        // commits in remote not in local
	Previews                 []previewResponse     `json:"previews,omitempty"`
}

// buildSessionsResponse builds the sessions/workspaces response data.
// Used by both the HTTP handler and WebSocket broadcast.
func (s *Server) buildSessionsResponse() []WorkspaceResponseItem {
	sessions := s.session.GetAllSessions()

	workspaceMap := make(map[string]*WorkspaceResponseItem)
	workspaces := s.state.GetWorkspaces()
	ctx := context.Background()
	for _, ws := range workspaces {
		// Use cached RemoteBranchExists from UpdateGitStatus (avoids per-broadcast git calls)
		branchURL := ""
		if ws.RemoteBranchExists {
			branchURL = workspace.BuildGitBranchURL(ws.Repo, ws.Branch)
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
			ID:                       ws.ID,
			Repo:                     ws.Repo,
			DefaultBranch:            defaultBranch,
			Branch:                   branch,
			BranchURL:                branchURL,
			Path:                     ws.Path,
			SessionCount:             0,
			Sessions:                 []SessionResponseItem{},
			QuickLaunch:              quickLaunchNames,
			GitAhead:                 ws.GitAhead,
			GitBehind:                ws.GitBehind,
			GitLinesAdded:            ws.GitLinesAdded,
			GitLinesRemoved:          ws.GitLinesRemoved,
			GitFilesChanged:          ws.GitFilesChanged,
			RemoteHostID:             remoteHostID,
			RemoteHostStatus:         remoteHostStatus,
			RemoteFlavorName:         remoteFlavorName,
			RemoteFlavor:             remoteFlavor,
			VCS:                      vcs,
			ConflictOnBranch:         conflictOnBranch,
			CommitsSyncedWithRemote:  ws.CommitsSyncedWithRemote,
			GitDefaultBranchOrphaned: ws.GitDefaultBranchOrphaned,
			RemoteBranchExists:       ws.RemoteBranchExists,
			LocalUniqueCommits:       ws.LocalUniqueCommits,
			RemoteUniqueCommits:      ws.RemoteUniqueCommits,
			Previews:                 []previewResponse{},
		}
		if s.previewManager != nil {
			previews := s.state.GetWorkspacePreviews(ws.ID)
			sort.Slice(previews, func(i, j int) bool {
				if previews[i].TargetPort == previews[j].TargetPort {
					return previews[i].ID < previews[j].ID
				}
				return previews[i].TargetPort < previews[j].TargetPort
			})
			items := make([]previewResponse, 0, len(previews))
			for _, p := range previews {
				items = append(items, toPreviewResponse(p))
			}
			workspaceMap[ws.ID].Previews = items
		}
	}

	// Pre-compute IsRunning status for all sessions in parallel.
	// Each check spawns a tmux process, so parallelism avoids serial latency.
	type runningResult struct {
		id      string
		running bool
	}
	runningCh := make(chan runningResult, len(sessions))
	timeout := time.Duration(s.config.GetXtermQueryTimeoutMs()) * time.Millisecond
	for _, sess := range sessions {
		go func(id string) {
			timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
			r := s.session.IsRunning(timeoutCtx, id)
			cancel()
			runningCh <- runningResult{id: id, running: r}
		}(sess.ID)
	}
	runningMap := make(map[string]bool, len(sessions))
	for range sessions {
		res := <-runningCh
		runningMap[res.id] = res.running
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
		running := runningMap[sess.ID]
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
