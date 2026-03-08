package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/tunnel"
)

// handleDetectTools returns detected tools (GET only).
func (s *Server) handleDetectTools(w http.ResponseWriter, r *http.Request) {
	type ToolResponse struct {
		Name    string `json:"name"`
		Command string `json:"command"`
		Source  string `json:"source"`
	}

	type Response struct {
		Tools []ToolResponse `json:"tools"`
	}

	detectedTools := s.models.GetDetectedTools()
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
	for _, target := range runTargets {
		runTargetResp = append(runTargetResp, contracts.RunTarget{
			Name:    target.Name,
			Command: target.Command,
		})
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
	catalog, err := s.models.GetCatalog()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read models: %v", err), http.StatusInternalServerError)
		return
	}

	response := contracts.ConfigResponse{
		WorkspacePath:              s.config.GetWorkspacePath(),
		SourceCodeManagement:       s.config.GetSourceCodeManagement(),
		Repos:                      repoResp,
		RunTargets:                 runTargetResp,
		QuickLaunch:                quickLaunchResp,
		ExternalDiffCommands:       externalDiffCommandsResp,
		ExternalDiffCleanupAfterMs: s.config.GetExternalDiffCleanupAfterMs(),
		Runners:                    catalog.Runners,
		Models:                     catalog.Models,
		EnabledModels:              s.models.GetEnabledModels(),
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
			QueryTimeoutMs:     s.config.GetXtermQueryTimeoutMs(),
			OperationTimeoutMs: s.config.GetXtermOperationTimeoutMs(),
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
		CommitMessage: contracts.CommitMessage{
			Target: s.config.GetCommitMessageTarget(),
		},
		Desync: contracts.Desync{
			Enabled: s.config.GetDesyncEnabled(),
			Target:  s.config.GetDesyncTarget(),
		},
		IOWorkspaceTelemetry: contracts.IOWorkspaceTelemetry{
			Enabled: s.config.GetIOWorkspaceTelemetryEnabled(),
			Target:  s.config.GetIOWorkspaceTelemetryTarget(),
		},
		Notifications: contracts.Notifications{
			SoundDisabled:           !s.config.GetNotificationSoundEnabled(),
			ConfirmBeforeClose:      s.config.GetConfirmBeforeClose(),
			SuggestDisposeAfterPush: s.config.GetSuggestDisposeAfterPush(),
		},
		Lore: contracts.Lore{
			Enabled:         s.config.GetLoreEnabled(),
			LLMTarget:       s.config.GetLoreTargetRaw(),
			CurateOnDispose: s.config.GetLoreCurateOnDispose(),
			AutoPR:          s.config.GetLoreAutoPR(),
		},
		Subreddit: contracts.Subreddit{
			Target:        s.config.GetSubredditTarget(),
			Interval:      s.config.GetSubredditInterval(),
			CheckingRange: s.config.GetSubredditCheckingRange(),
			MaxPosts:      s.config.GetSubredditMaxPosts(),
			MaxAge:        s.config.GetSubredditMaxAge(),
			Repos:         s.config.GetSubredditRepos(),
		},
		FloorManager: contracts.FloorManager{
			Enabled:           s.config.GetFloorManagerEnabled(),
			Target:            s.config.GetFloorManagerTarget(),
			RotationThreshold: s.config.GetFloorManagerRotationThreshold(),
			DebounceMs:        s.config.GetFloorManagerDebounceMs(),
		},
		RemoteAccess: contracts.RemoteAccess{
			Enabled:         s.config.GetRemoteAccessEnabled(),
			TimeoutMinutes:  s.config.GetRemoteAccessTimeoutMinutes(),
			PasswordHashSet: s.config.GetRemoteAccessPasswordHash() != "",
			Notify: contracts.RemoteAccessNotify{
				NtfyTopic: s.config.GetRemoteAccessNtfyTopic(),
				Command:   s.config.GetRemoteAccessNotifyCommand(),
			},
		},
		NeedsRestart: s.state.GetNeedsRestart(),
		SystemCapabilities: contracts.SystemCapabilities{
			ITerm2Available: detect.ITerm2Available(),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("failed to encode response", "handler", "config", "err", err)
	}
}

// handleConfigUpdate handles config update requests.
func (s *Server) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req contracts.ConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.logger.Error("invalid JSON payload", "err", err)
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Reload config from disk to get all current values (including tools, etc.)
	if err := s.config.Reload(); err != nil {
		s.logger.Error("failed to reload config", "err", err)
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
		// Build lookup of existing repos by URL to preserve bare_path
		existingByURL := make(map[string]string, len(cfg.Repos))
		for _, repo := range cfg.Repos {
			if repo.BarePath != "" {
				existingByURL[repo.URL] = repo.BarePath
			}
		}
		cfg.Repos = make([]config.Repo, len(req.Repos))
		for i, r := range req.Repos {
			barePath := existingByURL[r.URL]
			if barePath == "" {
				barePath = r.Name + ".git"
			}
			cfg.Repos[i] = config.Repo{Name: r.Name, URL: r.URL, BarePath: barePath}
		}
	}

	if req.RunTargets != nil {
		userTargets := make([]config.RunTarget, len(req.RunTargets))
		for i, t := range req.RunTargets {
			if t.Name == "" {
				http.Error(w, "run target name is required", http.StatusBadRequest)
				return
			}
			if t.Command == "" {
				http.Error(w, fmt.Sprintf("run target command is required for %s", t.Name), http.StatusBadRequest)
				return
			}
			userTargets[i] = config.RunTarget{Name: t.Name, Command: t.Command}
		}
		cfg.RunTargets = userTargets
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
		if req.Xterm.QueryTimeoutMs != nil && *req.Xterm.QueryTimeoutMs > 0 {
			cfg.Xterm.QueryTimeoutMs = *req.Xterm.QueryTimeoutMs
		}
		if req.Xterm.OperationTimeoutMs != nil && *req.Xterm.OperationTimeoutMs > 0 {
			cfg.Xterm.OperationTimeoutMs = *req.Xterm.OperationTimeoutMs
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

	if req.CommitMessage != nil {
		if cfg.CommitMessage == nil {
			cfg.CommitMessage = &config.CommitMessageConfig{}
		}
		if req.CommitMessage.Target != nil {
			cfg.CommitMessage.Target = *req.CommitMessage.Target
		}
	}

	if req.Desync != nil {
		if cfg.Desync == nil {
			cfg.Desync = &config.DesyncConfig{}
		}
		if req.Desync.Enabled != nil {
			enabled := *req.Desync.Enabled
			cfg.Desync.Enabled = &enabled
		}
		if req.Desync.Target != nil {
			cfg.Desync.Target = strings.TrimSpace(*req.Desync.Target)
		}
		// Nil out if everything is at zero value
		if (cfg.Desync.Enabled == nil || !*cfg.Desync.Enabled) && cfg.Desync.Target == "" {
			cfg.Desync = nil
		}
	}

	if req.IOWorkspaceTelemetry != nil {
		if cfg.IOWorkspaceTelemetry == nil {
			cfg.IOWorkspaceTelemetry = &config.IOWorkspaceTelemetryConfig{}
		}
		if req.IOWorkspaceTelemetry.Enabled != nil {
			enabled := *req.IOWorkspaceTelemetry.Enabled
			cfg.IOWorkspaceTelemetry.Enabled = &enabled
		}
		if req.IOWorkspaceTelemetry.Target != nil {
			cfg.IOWorkspaceTelemetry.Target = strings.TrimSpace(*req.IOWorkspaceTelemetry.Target)
		}
		// Nil out if everything is at zero value
		if (cfg.IOWorkspaceTelemetry.Enabled == nil || !*cfg.IOWorkspaceTelemetry.Enabled) && cfg.IOWorkspaceTelemetry.Target == "" {
			cfg.IOWorkspaceTelemetry = nil
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
		if req.Notifications.SuggestDisposeAfterPush != nil {
			v := *req.Notifications.SuggestDisposeAfterPush
			cfg.Notifications.SuggestDisposeAfterPush = &v
		}
	}

	if req.Lore != nil {
		if cfg.Lore == nil {
			cfg.Lore = &config.LoreConfig{}
		}
		if req.Lore.Enabled != nil {
			enabled := *req.Lore.Enabled
			cfg.Lore.Enabled = &enabled
		}
		if req.Lore.LLMTarget != nil {
			cfg.Lore.Target = strings.TrimSpace(*req.Lore.LLMTarget)
		}
		if req.Lore.CurateOnDispose != nil {
			v := *req.Lore.CurateOnDispose
			switch v {
			case "session", "workspace", "never":
				cfg.Lore.CurateOnDispose = v
			}
		}
		if req.Lore.AutoPR != nil {
			autoPR := *req.Lore.AutoPR
			cfg.Lore.AutoPR = &autoPR
		}
	}

	if req.Subreddit != nil {
		if cfg.Subreddit == nil {
			cfg.Subreddit = &config.SubredditConfig{}
		}
		if req.Subreddit.Target != nil {
			cfg.Subreddit.Target = strings.TrimSpace(*req.Subreddit.Target)
		}
		if req.Subreddit.Interval != nil {
			cfg.Subreddit.Interval = *req.Subreddit.Interval
		}
		if req.Subreddit.CheckingRange != nil {
			cfg.Subreddit.CheckingRange = *req.Subreddit.CheckingRange
		}
		if req.Subreddit.MaxPosts != nil {
			cfg.Subreddit.MaxPosts = *req.Subreddit.MaxPosts
		}
		if req.Subreddit.MaxAge != nil {
			cfg.Subreddit.MaxAge = *req.Subreddit.MaxAge
		}
		if req.Subreddit.Repos != nil {
			cfg.Subreddit.Repos = req.Subreddit.Repos
		}
	}

	if req.FloorManager != nil {
		if cfg.FloorManager == nil {
			cfg.FloorManager = &config.FloorManagerConfig{}
		}
		if req.FloorManager.Enabled != nil {
			enabled := *req.FloorManager.Enabled
			cfg.FloorManager.Enabled = &enabled
		}
		if req.FloorManager.Target != nil {
			cfg.FloorManager.Target = strings.TrimSpace(*req.FloorManager.Target)
		}
		if req.FloorManager.RotationThreshold != nil {
			cfg.FloorManager.RotationThreshold = *req.FloorManager.RotationThreshold
		}
		if req.FloorManager.DebounceMs != nil {
			cfg.FloorManager.DebounceMs = *req.FloorManager.DebounceMs
		}
	}

	if req.EnabledModels != nil {
		cfg.SetEnabledModels(*req.EnabledModels)
	}

	if req.RemoteAccess != nil {
		if cfg.RemoteAccess == nil {
			cfg.RemoteAccess = &config.RemoteAccessConfig{}
		}
		if req.RemoteAccess.Enabled != nil {
			enabled := *req.RemoteAccess.Enabled
			cfg.RemoteAccess.Enabled = &enabled
			// Clear deprecated field when new field is explicitly set
			cfg.RemoteAccess.Disabled = nil
		}
		if req.RemoteAccess.TimeoutMinutes != nil {
			cfg.RemoteAccess.TimeoutMinutes = *req.RemoteAccess.TimeoutMinutes
		}
		if req.RemoteAccess.Notify != nil {
			if cfg.RemoteAccess.Notify == nil {
				cfg.RemoteAccess.Notify = &config.RemoteAccessNotifyConfig{}
			}
			if req.RemoteAccess.Notify.NtfyTopic != nil {
				cfg.RemoteAccess.Notify.NtfyTopic = strings.TrimSpace(*req.RemoteAccess.Notify.NtfyTopic)
			}
			if req.RemoteAccess.Notify.Command != nil {
				cfg.RemoteAccess.Notify.Command = strings.TrimSpace(*req.RemoteAccess.Notify.Command)
			}
		}
	}

	warnings, err := cfg.ValidateForSave()
	if err != nil {
		s.logger.Error("validation error", "err", err)
		http.Error(w, fmt.Sprintf("Invalid config: %v", err), http.StatusBadRequest)
		return
	}

	if !reflect.DeepEqual(oldNetwork, cfg.Network) || !reflect.DeepEqual(oldAccessControl, cfg.AccessControl) {
		s.state.SetNeedsRestart(true)
		if err := s.state.Save(); err != nil {
			s.logger.Error("failed to save restart-needed state", "err", err)
		}
	}

	// Save config
	if err := cfg.Save(); err != nil {
		s.logger.Error("failed to save config", "err", err)
		http.Error(w, fmt.Sprintf("Failed to save config: %v", err), http.StatusInternalServerError)
		return
	}

	// If remote access is disabled but a tunnel is active, stop it immediately.
	// This prevents a security hole where an active tunnel could bypass auth
	// after the config is reloaded with remote_access.enabled = false.
	if !cfg.GetRemoteAccessEnabled() && s.tunnelManager != nil {
		status := s.tunnelManager.Status()
		if status.State == tunnel.StateConnected || status.State == tunnel.StateStarting {
			s.logger.Info("stopping tunnel because remote_access is disabled")
			s.tunnelManager.Stop()
			s.ClearRemoteAuth()
		}
	}

	// Update PR discovery polling based on new config
	// Pass a function so poll always uses current repos list
	s.prDiscovery.SetTarget(cfg.GetPrReviewTarget(), func() []config.Repo { return cfg.GetRepos() })

	// Refresh lore executor when lore target changes
	s.refreshLoreExecutor(cfg)

	// Trigger subreddit generation if newly enabled
	if req.Subreddit != nil {
		s.TriggerSubredditGeneration()
	}

	// Toggle floor manager if enabled changed
	if req.FloorManager != nil && req.FloorManager.Enabled != nil && OnFloorManagerToggle != nil {
		OnFloorManagerToggle(*req.FloorManager.Enabled)
	}

	// Ensure overlay directories exist for all repos if repos were actually updated
	newRepos := cfg.GetRepos()
	if !reposEqual(oldRepos, newRepos) {
		if err := s.workspace.EnsureOverlayDirs(newRepos); err != nil {
			s.logger.Warn("failed to ensure overlay directories", "err", err)
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
		writeJSON(w, warning)
		return
	}

	type ConfigSaveResponse struct {
		Status   string   `json:"status"`
		Message  string   `json:"message"`
		Warnings []string `json:"warnings,omitempty"`
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(ConfigSaveResponse{
		Status:   "ok",
		Message:  "Config saved and reloaded. Changes are now in effect.",
		Warnings: warnings,
	}); err != nil {
		s.logger.Error("failed to encode response", "handler", "config-update", "err", err)
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

// handleAuthSecretsGet returns GitHub auth secrets status.
func (s *Server) handleAuthSecretsGet(w http.ResponseWriter, r *http.Request) {
	secrets, err := config.GetAuthSecrets()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read secrets: %v", err), http.StatusInternalServerError)
		return
	}
	clientID := ""
	clientSecretSet := false
	if secrets.GitHub != nil {
		clientID = strings.TrimSpace(secrets.GitHub.ClientID)
		clientSecretSet = strings.TrimSpace(secrets.GitHub.ClientSecret) != ""
	}
	writeJSON(w, map[string]interface{}{
		"client_id":         clientID,
		"client_secret_set": clientSecretSet,
	})
}

// handleAuthSecretsUpdate saves GitHub auth secrets.
func (s *Server) handleAuthSecretsUpdate(w http.ResponseWriter, r *http.Request) {
	type SecretsRequest struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	var req SecretsRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ClientID) == "" {
		http.Error(w, "client_id is required", http.StatusBadRequest)
		return
	}
	// Check if a secret already exists
	existingSecrets, err := config.GetAuthSecrets()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read existing secrets: %v", err), http.StatusInternalServerError)
		return
	}
	existingSecret := ""
	if existingSecrets.GitHub != nil {
		existingSecret = existingSecrets.GitHub.ClientSecret
	}
	// If no new secret provided and no existing secret, error
	if strings.TrimSpace(req.ClientSecret) == "" && strings.TrimSpace(existingSecret) == "" {
		http.Error(w, "client_secret is required for initial setup", http.StatusBadRequest)
		return
	}
	// Use existing secret if new one not provided
	secretToSave := strings.TrimSpace(req.ClientSecret)
	if secretToSave == "" {
		secretToSave = existingSecret
	}
	if err := config.SaveGitHubAuthSecrets(req.ClientID, secretToSave); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save secrets: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}
