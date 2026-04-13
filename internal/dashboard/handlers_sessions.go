package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/nudgenik"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// Type aliases for contracts types used throughout this file.
type SessionResponseItem = contracts.SessionResponseItem
type SessionModelInfo = contracts.SessionModelInfo
type WorkspaceResponseItem = contracts.WorkspaceResponseItem

// buildSessionsResponse builds the sessions/workspaces response data.
// Used by both the HTTP handler and WebSocket broadcast.
func (s *Server) buildSessionsResponse() []WorkspaceResponseItem {
	sessions := s.session.GetAllSessions()

	workspaceMap := make(map[string]*WorkspaceResponseItem)
	workspaces := s.state.GetWorkspaces()
	ctx := context.Background()
	for _, ws := range workspaces {
		// Hide recyclable workspaces from the dashboard
		if ws.Status == state.WorkspaceStatusRecyclable {
			continue
		}

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
		vcs := ws.VCS
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
				if profile, found := s.config.GetRemoteProfile(host.ProfileID); found {
					if resolved, err := config.ResolveProfileFlavor(profile, host.Flavor); err == nil {
						remoteFlavorName = resolved.FlavorDisplayName
						remoteFlavor = resolved.Flavor
						vcs = resolved.VCS
					} else {
						remoteFlavorName = profile.DisplayName
						vcs = profile.VCS
					}
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

		// Get default branch from server-level TTL cache
		defaultBranch := ""
		if db, err := s.cachedDefaultBranch(ctx, ws.Repo); err == nil {
			defaultBranch = db
		}

		// Get repo name from config
		repoName := ""
		if r, found := s.config.FindRepoByURL(ws.Repo); found {
			repoName = r.Name
		}

		workspaceMap[ws.ID] = &WorkspaceResponseItem{
			ID:                      ws.ID,
			Repo:                    ws.Repo,
			RepoName:                repoName,
			DefaultBranch:           defaultBranch,
			Branch:                  branch,
			BranchURL:               branchURL,
			Path:                    ws.Path,
			SessionCount:            0,
			Sessions:                []SessionResponseItem{},
			QuickLaunch:             quickLaunchNames,
			Ahead:                   ws.Ahead,
			Behind:                  ws.Behind,
			LinesAdded:              ws.LinesAdded,
			LinesRemoved:            ws.LinesRemoved,
			FilesChanged:            ws.FilesChanged,
			RemoteHostID:            remoteHostID,
			RemoteHostStatus:        remoteHostStatus,
			RemoteFlavorName:        remoteFlavorName,
			RemoteFlavor:            remoteFlavor,
			VCS:                     vcs,
			ConflictOnBranch:        conflictOnBranch,
			CommitsSyncedWithRemote: ws.CommitsSyncedWithRemote,
			DefaultBranchOrphaned:   ws.DefaultBranchOrphaned,
			RemoteBranchExists:      ws.RemoteBranchExists,
			LocalUniqueCommits:      ws.LocalUniqueCommits,
			RemoteUniqueCommits:     ws.RemoteUniqueCommits,
			Previews:                []contracts.PreviewResponse{},
			ResolveConflicts:        ws.ResolveConflicts,
			Status:                  ws.Status,
			Backburner:              ws.Backburner,
		}
		if s.previewManager != nil {
			previews := s.state.GetWorkspacePreviews(ws.ID)
			sort.Slice(previews, func(i, j int) bool {
				if previews[i].TargetPort == previews[j].TargetPort {
					return previews[i].ID < previews[j].ID
				}
				return previews[i].TargetPort < previews[j].TargetPort
			})
			items := make([]contracts.PreviewResponse, 0, len(previews))
			for _, p := range previews {
				items = append(items, toPreviewResponse(p))
			}
			workspaceMap[ws.ID].Previews = items
		}

		// Populate tabs from top-level state — no field rewriting.
		wsTabs := s.state.GetWorkspaceTabs(ws.ID)
		tabItems := make([]contracts.Tab, 0, len(wsTabs))
		for _, tab := range wsTabs {
			tabItems = append(tabItems, contracts.Tab{
				ID:        tab.ID,
				Kind:      tab.Kind,
				Label:     tab.Label,
				Route:     tab.Route,
				Closable:  tab.Closable,
				Meta:      tab.Meta,
				CreatedAt: tab.CreatedAt.Format(time.RFC3339),
			})
		}
		workspaceMap[ws.ID].Tabs = tabItems
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
				if profile, found := s.config.GetRemoteProfile(host.ProfileID); found {
					resolved, resolveErr := config.ResolveProfileFlavor(profile, host.Flavor)
					if resolveErr == nil {
						remoteFlavorName = resolved.FlavorDisplayName
					} else {
						remoteFlavorName = profile.DisplayName
					}
					// Build user-facing attach command targeting the specific window
					if host.Hostname != "" {
						reconnectCmd := profile.ReconnectCommand
						if reconnectCmd == "" {
							reconnectCmd = profile.ConnectCommand
						}
						if reconnectCmd == "" {
							reconnectCmd = "ssh -tt {{.Hostname}} --"
						}
						// Target the agent's window on the isolated socket
						socketName := s.config.GetTmuxSocketName()
						tmuxTarget := socketName
						if sess.RemoteWindow != "" {
							tmuxTarget = socketName + ":" + sess.RemoteWindow
						}
						templateStr := reconnectCmd + " tmux -L " + socketName + " attach -t " + tmuxTarget
						if tmpl, err := template.New("attach").Parse(templateStr); err == nil {
							var cmdStr strings.Builder
							tmplData := struct {
								Hostname string
								Flavor   string
							}{Hostname: host.Hostname, Flavor: host.Flavor}
							if err := tmpl.Execute(&cmdStr, tmplData); err == nil {
								attachCmd = cmdStr.String()
							}
						}
					}
				}
			}
		}

		// Resolve persona info for display
		var personaID, personaIcon, personaColor, personaName string
		if sess.PersonaID != "" && s.personaManager != nil {
			personaID = sess.PersonaID
			if p, err := s.personaManager.Get(sess.PersonaID); err == nil {
				personaIcon = p.Icon
				personaColor = p.Color
				personaName = p.Name
			}
		}

		// Resolve model metadata if models manager is available
		var modelInfo *SessionModelInfo
		if s.models != nil {
			if model, found := s.models.FindModel(sess.Target); found {
				meta := s.models.GetRegistryMeta(model.ID)
				if meta.ContextWindow > 0 || meta.CostInput > 0 || meta.CostOutput > 0 {
					modelInfo = &SessionModelInfo{
						ContextWindow:     meta.ContextWindow,
						CostInputPerMTok:  meta.CostInput,
						CostOutputPerMTok: meta.CostOutput,
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
			XtermTitle:       sess.XtermTitle,
			CreatedAt:        sess.CreatedAt.Format(time.RFC3339),
			LastOutputAt:     lastOutputAt,
			Running:          running,
			Status:           sess.Status, // Expose session status for remote sessions
			AttachCmd:        attachCmd,
			TmuxSocket:       sess.TmuxSocket,
			TmuxSession:      sess.TmuxSession,
			NudgeState:       nudgeState,
			NudgeSummary:     nudgeSummary,
			NudgeSeq:         sess.NudgeSeq,
			Model:            modelInfo,
			RemoteHostID:     sess.RemoteHostID,
			RemotePaneID:     sess.RemotePaneID,
			RemoteHostname:   remoteHostname,
			RemoteFlavorName: remoteFlavorName,
			PersonaID:        personaID,
			PersonaIcon:      personaIcon,
			PersonaColor:     personaColor,
			PersonaName:      personaName,
			StyleID:          sess.StyleID,
		})
		wsResp.SessionCount = len(wsResp.Sessions)
	}

	// Convert map to slice (client handles sorting)
	response := make([]WorkspaceResponseItem, 0, len(workspaceMap))
	for _, ws := range workspaceMap {
		response = append(response, *ws)
	}

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
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("failed to encode response", "handler", "sessions", "err", err)
	}
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

// pluralS returns "s" if n != 1, otherwise "".
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// cachedDefaultBranch returns the default branch for a repo URL, using a
// server-level TTL cache to avoid calling into the workspace manager (which
// may run git commands) on every WebSocket broadcast.
func (s *Server) cachedDefaultBranch(ctx context.Context, repoURL string) (string, error) {
	now := time.Now()

	s.defaultBranchCacheMu.RLock()
	entry, ok := s.defaultBranchCache[repoURL]
	s.defaultBranchCacheMu.RUnlock()

	if ok && now.Sub(entry.fetchedAt) < defaultBranchCacheTTL {
		if entry.branch == "" {
			return "", fmt.Errorf("default branch unknown for %s", repoURL)
		}
		return entry.branch, nil
	}

	// Cache miss or stale — call through to workspace manager.
	branch, err := s.workspace.GetDefaultBranch(ctx, repoURL)

	s.defaultBranchCacheMu.Lock()
	s.defaultBranchCache[repoURL] = defaultBranchEntry{
		branch:    branch,
		fetchedAt: now,
	}
	s.defaultBranchCacheMu.Unlock()

	return branch, err
}
