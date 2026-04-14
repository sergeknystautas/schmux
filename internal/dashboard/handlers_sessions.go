package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/nudgenik"
	"github.com/sergeknystautas/schmux/internal/persona"
	"github.com/sergeknystautas/schmux/internal/preview"
	"github.com/sergeknystautas/schmux/internal/remote"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// Type aliases for contracts types used throughout this file.
type SessionResponseItem = contracts.SessionResponseItem
type SessionModelInfo = contracts.SessionModelInfo
type WorkspaceResponseItem = contracts.WorkspaceResponseItem

// SessionHandlers groups session-scoped HTTP handlers.
type SessionHandlers struct {
	config         *config.Config
	state          state.StateStore
	session        *session.Manager
	workspace      workspace.WorkspaceManager
	models         *models.Manager
	remoteManager  *remote.Manager
	previewManager *preview.Manager
	personaManager *persona.Manager
	logger         *log.Logger

	// Callbacks into Server methods that cannot be extracted.
	broadcastSessions                 func()
	getLinearSyncResolveConflictState func(workspaceID string) *LinearSyncResolveConflictState

	// Cached default branches: repoURL -> {branch, fetchedAt}
	defaultBranchCache   map[string]defaultBranchEntry
	defaultBranchCacheMu sync.RWMutex
}

// buildSessionsResponse builds the sessions/workspaces response data.
// Used by both the HTTP handler and WebSocket broadcast.
func (h *SessionHandlers) buildSessionsResponse() []WorkspaceResponseItem {
	sessions := h.session.GetAllSessions()

	workspaceMap := make(map[string]*WorkspaceResponseItem)
	workspaces := h.state.GetWorkspaces()
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
			if host, found := h.state.GetRemoteHost(ws.RemoteHostID); found {
				if host.Hostname != "" {
					branch = host.Hostname
				}
				// Use live connection status from remote manager if available,
				// since persisted state can be stale after daemon restarts.
				if h.remoteManager != nil {
					liveStatus, _ := h.remoteManager.GetHostConnectionStatus(ws.RemoteHostID)
					remoteHostStatus = liveStatus
				} else {
					remoteHostStatus = host.Status
				}
				if profile, found := h.config.GetRemoteProfile(host.ProfileID); found {
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
		if cfg := h.workspace.GetWorkspaceConfig(ws.ID); cfg != nil && len(cfg.QuickLaunch) > 0 {
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
		if db, err := h.cachedDefaultBranch(ctx, ws.Repo); err == nil {
			defaultBranch = db
		}

		// Get repo name from config
		repoName := ""
		if r, found := h.config.FindRepoByURL(ws.Repo); found {
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
			IntentShared:            ws.IntentShared,
		}
		if h.previewManager != nil {
			previews := h.state.GetWorkspacePreviews(ws.ID)
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
		wsTabs := h.state.GetWorkspaceTabs(ws.ID)
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
	timeout := time.Duration(h.config.GetXtermQueryTimeoutMs()) * time.Millisecond
	for _, sess := range sessions {
		go func(id string) {
			timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
			r := h.session.IsRunning(timeoutCtx, id)
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

		attachCmd, _ := h.session.GetAttachCommand(sess.ID)
		lastOutputAt := ""
		if !sess.LastOutputAt.IsZero() {
			lastOutputAt = sess.LastOutputAt.Format(time.RFC3339)
		}
		running := runningMap[sess.ID]
		nudgeState, nudgeSummary := parseNudgeSummary(sess.Nudge)

		// Get remote host info if this is a remote session
		var remoteHostname, remoteFlavorName string
		if sess.RemoteHostID != "" {
			if host, found := h.state.GetRemoteHost(sess.RemoteHostID); found {
				remoteHostname = host.Hostname
				if profile, found := h.config.GetRemoteProfile(host.ProfileID); found {
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
						socketName := h.config.GetTmuxSocketName()
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
		if sess.PersonaID != "" && h.personaManager != nil {
			personaID = sess.PersonaID
			if p, err := h.personaManager.Get(sess.PersonaID); err == nil {
				personaIcon = p.Icon
				personaColor = p.Color
				personaName = p.Name
			}
		}

		// Resolve model metadata if models manager is available
		var modelInfo *SessionModelInfo
		if h.models != nil {
			if model, found := h.models.FindModel(sess.Target); found {
				meta := h.models.GetRegistryMeta(model.ID)
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
func (h *SessionHandlers) handleSessions(w http.ResponseWriter, r *http.Request) {
	response := h.buildSessionsResponse()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("failed to encode response", "handler", "sessions", "err", err)
	}
}

// handleUpdateNickname handles session nickname update requests.
func (h *SessionHandlers) handleUpdateNickname(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	// Extract session ID from chi URL param
	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		writeJSONError(w, "session ID is required", http.StatusBadRequest)
		return
	}

	var req UpdateNicknameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Update nickname (and rename tmux session)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(h.config.GetXtermOperationTimeoutMs())*time.Millisecond)
	err := h.session.RenameSession(ctx, sessionID, req.Nickname)
	cancel()
	if err != nil {
		// Check if this is a nickname conflict error
		if errors.Is(err, session.ErrNicknameInUse) {
			writeJSONError(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSONError(w, fmt.Sprintf("Failed to rename session: %v", err), http.StatusInternalServerError)
		return
	}

	// Broadcast update to WebSocket clients
	go h.broadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		h.logger.Error("failed to encode response", "handler", "update-nickname", "err", err)
	}
}

// handleUpdateXtermTitle handles xterm title change reports from the frontend.
// PUT /api/sessions-xterm-title/{sessionID}
func (h *SessionHandlers) handleUpdateXtermTitle(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		writeJSONError(w, "session ID is required", http.StatusBadRequest)
		return
	}

	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	changed := h.state.UpdateSessionXtermTitle(sessionID, req.Title)
	if changed {
		go h.broadcastSessions()
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		h.logger.Error("failed to encode response", "handler", "update-xterm-title", "err", err)
	}
}

// handleAskNudgenik handles GET requests to ask NudgeNik about a session's output.
// GET /api/askNudgenik/{sessionId}
//
// Combines extraction of the latest session response with the Claude CLI call.
// The response extraction happens internally on the server side.
func (h *SessionHandlers) handleAskNudgenik(w http.ResponseWriter, r *http.Request) {
	// Extract session ID from chi wildcard param
	sessionID := chi.URLParam(r, "*")
	if sessionID == "" {
		writeJSONError(w, "session ID is required", http.StatusBadRequest)
		return
	}

	// Verify session exists (for proper 404 response)
	if _, found := h.state.GetSession(sessionID); !found {
		writeJSONError(w, "session not found", http.StatusNotFound)
		return
	}

	// Capture via tracker (handles local and remote sessions via ControlSource)
	tracker, err := h.session.GetTracker(sessionID)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("session tracker not available: %v", err), http.StatusInternalServerError)
		return
	}

	captureCtx, cancel := context.WithTimeout(r.Context(), h.config.XtermOperationTimeout())
	content, err := tracker.CaptureLastLines(captureCtx, 100)
	cancel()
	if err != nil {
		writeJSONError(w, fmt.Sprintf("failed to capture session output: %v", err), http.StatusInternalServerError)
		return
	}

	ctx := context.Background()
	result, err := nudgenik.AskForCapture(ctx, h.config, content)
	if err != nil {
		nudgenikLog := logging.Sub(h.logger, "nudgenik")
		switch {
		case errors.Is(err, nudgenik.ErrDisabled):
			nudgenikLog.Info("nudgenik is disabled")
			writeJSONError(w, "Nudgenik is disabled. Configure a target in settings.", http.StatusServiceUnavailable)
		case errors.Is(err, nudgenik.ErrNoResponse):
			nudgenikLog.Info("no response extracted", "session_id", sessionID)
			writeJSONError(w, "No response found in session output", http.StatusBadRequest)
		case errors.Is(err, nudgenik.ErrTargetNotFound):
			nudgenikLog.Warn("target not found in config")
			writeJSONError(w, "Nudgenik target not found", http.StatusServiceUnavailable)
		case errors.Is(err, nudgenik.ErrTargetNoSecrets):
			nudgenikLog.Warn("target missing required secrets")
			writeJSONError(w, "Nudgenik target missing required secrets", http.StatusServiceUnavailable)
		default:
			nudgenikLog.Error("failed to ask", "session_id", sessionID, "err", err)
			writeJSONError(w, fmt.Sprintf("Failed to ask nudgenik: %v", err), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		h.logger.Error("failed to encode response", "handler", "ask-nudgenik", "err", err)
	}
}

// handleHasNudgenik handles GET requests to check if nudgenik is available globally.
// Returns available: true only when a nudgenik target is configured.
func (h *SessionHandlers) handleHasNudgenik(w http.ResponseWriter, r *http.Request) {
	available := nudgenik.IsEnabled(h.config)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]bool{"available": available}); err != nil {
		h.logger.Error("failed to encode response", "handler", "has-nudgenik", "err", err)
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

// cachedDefaultBranch returns the default branch for a repo URL, using a
// TTL cache to avoid calling into the workspace manager (which
// may run git commands) on every WebSocket broadcast.
func (h *SessionHandlers) cachedDefaultBranch(ctx context.Context, repoURL string) (string, error) {
	now := time.Now()

	h.defaultBranchCacheMu.RLock()
	entry, ok := h.defaultBranchCache[repoURL]
	h.defaultBranchCacheMu.RUnlock()

	if ok && now.Sub(entry.fetchedAt) < defaultBranchCacheTTL {
		if entry.branch == "" {
			return "", fmt.Errorf("default branch unknown for %s", repoURL)
		}
		return entry.branch, nil
	}

	// Cache miss or stale — call through to workspace manager.
	branch, err := h.workspace.GetDefaultBranch(ctx, repoURL)

	h.defaultBranchCacheMu.Lock()
	h.defaultBranchCache[repoURL] = defaultBranchEntry{
		branch:    branch,
		fetchedAt: now,
	}
	h.defaultBranchCacheMu.Unlock()

	return branch, err
}
