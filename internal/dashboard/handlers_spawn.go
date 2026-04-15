package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/branchsuggest"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/persona"
	"github.com/sergeknystautas/schmux/internal/remote"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/spawn"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/style"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

//go:embed cookbooks.json
var cookbooksFS embed.FS

// SpawnHandlers groups HTTP handlers for spawn, branch suggestion,
// branch conflict checks, recent branches, and built-in quick launch.
type SpawnHandlers struct {
	config         *config.Config
	state          state.StateStore
	session        *session.Manager
	workspace      workspace.WorkspaceManager
	models         *models.Manager
	remoteManager  *remote.Manager
	personaManager *persona.Manager
	styleManager   *style.Manager
	spawnStore     *spawn.Store
	logger         *log.Logger

	// Callbacks into Server methods that cannot be extracted.
	broadcastSessions   func()
	vcsTypeForWorkspace func(ws state.Workspace) string
}

// SpawnRequest is a type alias for contracts.SpawnRequest.
type SpawnRequest = contracts.SpawnRequest

// handleSpawnPost handles session spawning requests.
func (h *SpawnHandlers) handleSpawnPost(w http.ResponseWriter, r *http.Request) {
	// Spawn requests may include base64-encoded image attachments (up to 5 images).
	// Use a larger body limit than the default 1MB.
	const maxSpawnBodySize = 50 * 1024 * 1024 // 50MB
	r.Body = http.MaxBytesReader(w, r.Body, maxSpawnBodySize)
	var req SpawnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.QuickLaunchName != "" {
		if req.Command != "" || len(req.Targets) > 0 {
			writeJSONError(w, "cannot specify quick_launch_name with command or targets", http.StatusBadRequest)
			return
		}
		if req.WorkspaceID == "" {
			writeJSONError(w, "workspace_id is required for quick_launch_name", http.StatusBadRequest)
			return
		}
		resolved, err := h.resolveQuickLaunchByName(req.WorkspaceID, req.QuickLaunchName)
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
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
		if resolved.PersonaID != "" && req.PersonaID == "" {
			req.PersonaID = resolved.PersonaID
		}
	}

	// Auto-detect remote host/flavor from request or workspace
	remoteHostID := req.RemoteHostID
	if remoteHostID == "" && req.WorkspaceID != "" {
		if ws, found := h.state.GetWorkspace(req.WorkspaceID); found && ws.RemoteHostID != "" {
			remoteHostID = ws.RemoteHostID
		}
	}
	if remoteHostID != "" && req.RemoteProfileID == "" {
		if host, found := h.state.GetRemoteHost(remoteHostID); found {
			req.RemoteProfileID = host.ProfileID
			req.RemoteFlavor = host.Flavor
		}
	}
	// When spawning with a profile+flavor but no specific host, reuse an existing
	// connected host for that combination. This is the default for adding sessions
	// to an existing workspace (e.g., CLI callers, E2E tests). New hosts are
	// only created when no connected host exists or when explicitly requested
	// via the "+ New host" card (which sets remote_host_id).
	if remoteHostID == "" && req.RemoteProfileID != "" && h.remoteManager != nil {
		conns := h.remoteManager.GetConnectionsByProfileAndFlavor(req.RemoteProfileID, req.RemoteFlavor)
		for _, conn := range conns {
			if conn.IsConnected() {
				remoteHostID = conn.Host().ID
				break
			}
		}
	}

	// Validate request
	// Remote spawns don't need repo/branch (they use the remote profile's workspace)
	if req.WorkspaceID == "" && req.RemoteProfileID == "" {
		// When not spawning into existing workspace and not remote, repo and branch are required
		if req.Repo == "" {
			writeJSONError(w, "repo is required (when not using --workspace or remote)", http.StatusBadRequest)
			return
		}
		if req.Branch == "" {
			writeJSONError(w, "branch is required (when not using --workspace or remote)", http.StatusBadRequest)
			return
		}
	}
	// Either command or targets must be provided
	if req.Command == "" && len(req.Targets) == 0 {
		writeJSONError(w, "either command or targets is required", http.StatusBadRequest)
		return
	}
	if req.Command != "" && len(req.Targets) > 0 {
		writeJSONError(w, "cannot specify both command and targets", http.StatusBadRequest)
		return
	}

	// Validate resume mode
	if req.Resume {
		if req.Command != "" {
			writeJSONError(w, "cannot use command mode with resume", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Prompt) != "" {
			writeJSONError(w, "cannot use prompt with resume mode", http.StatusBadRequest)
			return
		}
	}

	// Validate image attachments
	if len(req.ImageAttachments) > 0 {
		if len(req.ImageAttachments) > 5 {
			writeJSONError(w, "maximum 5 image attachments allowed", http.StatusBadRequest)
			return
		}
		if req.Resume {
			writeJSONError(w, "cannot use image attachments with resume mode", http.StatusBadRequest)
			return
		}
		if req.Command != "" {
			writeJSONError(w, "cannot use image attachments with command mode", http.StatusBadRequest)
			return
		}
		if req.RemoteProfileID != "" {
			writeJSONError(w, "image attachments are not supported for remote spawns", http.StatusBadRequest)
			return
		}
	}

	// Detect git URL in repo field and register if new
	if req.Repo != "" && isGitURL(req.Repo) {
		if _, found := h.config.FindRepoByURL(req.Repo); !found {
			existingNames := make([]string, 0, len(h.config.Repos))
			for _, r := range h.config.Repos {
				existingNames = append(existingNames, r.Name)
			}
			name := repoNameFromURL(req.Repo, existingNames)
			h.config.Repos = append(h.config.Repos, config.Repo{
				Name:     name,
				URL:      req.Repo,
				BarePath: name + ".git",
			})
			if err := h.config.Save(); err != nil {
				writeJSONError(w, fmt.Sprintf("failed to register repo: %v", err), http.StatusInternalServerError)
				return
			}
		}
	}

	// Server-side branch conflict check for worktree mode
	// This catches race conditions where UI check passed but another spawn claimed the branch.
	// Recyclable workspaces are excluded — they are available for reuse and GetOrCreate
	// will reclaim the branch via Tier 0 recycling.
	if req.WorkspaceID == "" && h.config.UseWorktrees() {
		for _, ws := range h.state.GetWorkspaces() {
			if ws.Repo == req.Repo && ws.Branch == req.Branch && ws.Status != state.WorkspaceStatusRecyclable {
				writeJSONError(w, fmt.Sprintf("branch_conflict: branch %q is already in use by workspace %q", req.Branch, ws.ID), http.StatusConflict)
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
		if req.RemoteProfileID != "" {
			writeJSONError(w, "remote command spawns are not supported (only target-based spawns work on remote hosts)", http.StatusBadRequest)
			return
		}

		sessionLog := logging.Sub(h.logger, "session")
		sessionLog.Info("spawn request", "repo", req.Repo, "branch", req.Branch, "workspace_id", req.WorkspaceID, "command", req.Command, "nickname", req.Nickname)

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(h.config.GetGitCloneTimeoutMs())*time.Millisecond)
		sess, err := h.session.SpawnCommand(ctx, session.SpawnOptions{
			RepoURL:     req.Repo,
			Branch:      req.Branch,
			Command:     req.Command,
			Nickname:    req.Nickname,
			WorkspaceID: req.WorkspaceID,
			NewBranch:   req.NewBranch,
		})
		cancel()

		if err != nil {
			results = append(results, SessionResult{
				Command:  req.Command,
				Nickname: req.Nickname,
				Error:    err.Error(),
			})
			sessionLog.Error("spawn failed", "command", req.Command, "err", err)
		} else {
			results = append(results, SessionResult{
				SessionID:   sess.ID,
				WorkspaceID: sess.WorkspaceID,
				Command:     req.Command,
				Nickname:    sess.Nickname,
			})
			sessionLog.Info("spawn success", "command", req.Command, "session_id", sess.ID, "workspace_id", sess.WorkspaceID)
		}

		// Broadcast update to WebSocket clients so waitForSession resolves immediately
		if err == nil {
			go h.broadcastSessions()
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(results); err != nil {
			writeJSONError(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// Handle target-based spawn
	promptPreview := req.Prompt
	if len(promptPreview) > 100 {
		promptPreview = promptPreview[:100] + "..."
	}
	sessionLog := logging.Sub(h.logger, "session")
	if req.RemoteProfileID != "" {
		sessionLog.Info("spawn request (remote)", "profile_id", req.RemoteProfileID, "flavor", req.RemoteFlavor, "host_id", remoteHostID, "req_host_id", req.RemoteHostID, "targets", req.Targets, "prompt", promptPreview)
	} else {
		sessionLog.Info("spawn request (local)", "repo", req.Repo, "branch", req.Branch, "workspace_id", req.WorkspaceID, "targets", req.Targets, "prompt", promptPreview)
	}

	// Calculate total sessions to spawn for global nickname numbering
	totalToSpawn := 0
	for targetName, count := range req.Targets {
		promptable, found := h.models.IsModel(targetName)
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

	// Resolve persona
	var personaObj *persona.Persona
	if req.PersonaID != "" {
		p, err := h.personaManager.Get(req.PersonaID)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("persona not found: %s", req.PersonaID), http.StatusBadRequest)
			return
		}
		personaObj = p
	}

	// Resolve explicit style override
	var explicitStyleObj *style.Style
	explicitNone := false
	if req.StyleID == "none" {
		explicitNone = true
	} else if req.StyleID != "" {
		st, err := h.styleManager.Get(req.StyleID)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("style not found: %s", req.StyleID), http.StatusBadRequest)
			return
		}
		explicitStyleObj = st
	}

	for targetName, count := range req.Targets {
		promptable, found := h.models.IsModel(targetName)
		if !found {
			results = append(results, SessionResult{
				Target: targetName,
				Error:  fmt.Sprintf("target not found: %s", targetName),
			})
			continue
		}
		// Prompt is optional for promptable targets — agents can be spawned
		// without one for interactive use
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

			// Resolve style for this target
			var styleObj *style.Style
			if explicitStyleObj != nil {
				styleObj = explicitStyleObj
			} else if !explicitNone {
				baseTool := h.models.ResolveTargetToTool(targetName)
				if baseTool == "" {
					baseTool = targetName // command targets use their name directly
				}
				if defaultID := h.config.GetCommStyles()[baseTool]; defaultID != "" {
					styleObj, _ = h.styleManager.Get(defaultID)
				}
			}

			resolvedStyleID := ""
			if styleObj != nil {
				resolvedStyleID = styleObj.ID
			}

			agentPrompt := formatAgentSystemPrompt(personaObj, styleObj)

			// Session spawn needs a longer timeout for git operations
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(h.config.GetGitCloneTimeoutMs())*time.Millisecond)

			var sess *state.Session
			var err error

			// Route to remote or local spawn based on request
			if req.RemoteProfileID != "" {
				// Remote spawn - use SpawnRemote()
				sess, err = h.session.SpawnRemote(ctx, session.RemoteSpawnOptions{
					ProfileID:     req.RemoteProfileID,
					FlavorStr:     req.RemoteFlavor,
					HostID:        remoteHostID,
					TargetName:    targetName,
					Prompt:        req.Prompt,
					Nickname:      nickname,
					PersonaID:     req.PersonaID,
					PersonaPrompt: agentPrompt,
					StyleID:       resolvedStyleID,
				})
			} else {
				// Local spawn - use existing Spawn()
				sess, err = h.session.Spawn(ctx, session.SpawnOptions{
					RepoURL:          req.Repo,
					Branch:           req.Branch,
					TargetName:       targetName,
					Prompt:           req.Prompt,
					Nickname:         nickname,
					WorkspaceID:      req.WorkspaceID,
					Resume:           req.Resume,
					NewBranch:        req.NewBranch,
					PersonaID:        req.PersonaID,
					PersonaPrompt:    agentPrompt,
					StyleID:          resolvedStyleID,
					ImageAttachments: req.ImageAttachments,
				})
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
			sessionLog.Error("spawn failed", "target", r.Target, "err", r.Error)
		} else {
			sessionLog.Info("spawn success", "target", r.Target, "session_id", r.SessionID, "workspace_id", r.WorkspaceID)
			hasSuccess = true
		}
	}

	// Set intent sharing on workspace if requested
	if hasSuccess && req.IntentShared {
		seen := make(map[string]bool)
		for _, r := range results {
			if r.Error == "" && r.WorkspaceID != "" && !seen[r.WorkspaceID] {
				seen[r.WorkspaceID] = true
				if ws, ok := h.state.GetWorkspace(r.WorkspaceID); ok {
					ws.IntentShared = true
					h.state.UpdateWorkspace(ws)
				}
			}
		}
	}

	// Broadcast update to WebSocket clients
	if hasSuccess {
		go h.broadcastSessions()

		// Track spawn entry usage (non-blocking, best-effort)
		if h.spawnStore != nil && req.Repo != "" {
			if repoInfo, found := h.config.FindRepoByURL(req.Repo); found {
				repoName := repoInfo.Name
				if req.ActionID != "" {
					// Explicit action ID from dropdown click
					h.spawnStore.RecordUse(repoName, req.ActionID)
				} else if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
					// Match prompt against pinned spawn entries
					if entries, err := h.spawnStore.List(repoName); err == nil {
						for _, e := range entries {
							if e.Prompt != "" && e.Prompt == prompt {
								h.spawnStore.RecordUse(repoName, e.ID)
								break
							}
						}
					}
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(results); err != nil {
		h.logger.Error("failed to encode response", "handler", "spawn", "err", err)
	}
}

// handleSuggestBranch handles branch name suggestion requests.
func (h *SpawnHandlers) handleSuggestBranch(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	start := time.Now()

	// Parse request
	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Check if branch suggestion is enabled
	if !branchsuggest.IsEnabled(h.config) {
		writeJSONError(w, "Branch suggestion is not configured", http.StatusServiceUnavailable)
		return
	}

	targetName := h.config.GetBranchSuggestTarget()
	workspaceLog := logging.Sub(h.logger, "workspace")
	workspaceLog.Info("asking for branch suggestion", "target", targetName)

	// Generate branch suggestion
	result, err := branchsuggest.AskForPrompt(r.Context(), h.config, req.Prompt)
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
		workspaceLog.Error("suggest-branch failed", "duration", time.Since(start).Truncate(time.Millisecond), "status", status, "err", err)
		writeJSONError(w, fmt.Sprintf("Failed to generate branch suggestion: %v", err), status)
		return
	}

	workspaceLog.Info("suggest-branch ok", "duration", time.Since(start).Truncate(time.Millisecond))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		h.logger.Error("failed to encode response", "handler", "suggest-branch", "err", err)
	}
}

// handlePrepareBranchSpawn prepares spawn data for an existing branch.
// Gets commit log from the bare clone and returns everything needed to populate
// the spawn form.
func (h *SpawnHandlers) handlePrepareBranchSpawn(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	start := time.Now()

	var req struct {
		RepoName string `json:"repo_name"`
		Branch   string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.RepoName == "" || req.Branch == "" {
		writeJSONError(w, "repo_name and branch are required", http.StatusBadRequest)
		return
	}

	// Look up repo URL from name
	repo, found := h.config.FindRepo(req.RepoName)
	if !found {
		writeJSONError(w, "repo not found", http.StatusNotFound)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Get commit subjects from bare clone
	workspaceLog := logging.Sub(h.logger, "workspace")
	subjects, err := h.workspace.GetBranchCommitLog(ctx, repo.URL, req.Branch, 20)
	if err != nil {
		workspaceLog.Warn("prepare-branch-spawn: failed to get commit log", "err", err)
		// Non-fatal: proceed without commit log
		subjects = nil
	}

	// Build the review prompt with commit history included
	prompt := "Review the current state of this branch and prepare to resume work.\n\n" +
		"1. Read any markdown or spec files in the repo root and docs/ to understand project context and goals\n" +
		"2. Run `git diff --stat main...HEAD` to compare this branch against where it diverged from main\n" +
		"3. Identify what's been completed, what's in progress, and what remains\n\n"

	if len(subjects) > 0 {
		prompt += "<commit_history>\n"
		for i, msg := range subjects {
			if i > 0 {
				prompt += "\n"
			}
			prompt += "---\n" + msg + "\n"
		}
		prompt += "---\n</commit_history>\n\n"
	}

	prompt += "Summarize your findings, then ask what to work on next."

	workspaceLog.Info("prepare-branch-spawn ok", "duration", time.Since(start).Truncate(time.Millisecond))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"repo":   repo.URL,
		"branch": req.Branch,
		"prompt": prompt,
	}); err != nil {
		h.logger.Error("failed to encode response", "handler", "prepare-branch-spawn", "err", err)
	}
}

type resolvedQuickLaunch struct {
	Name      string
	Command   string
	Target    string
	Prompt    string
	PersonaID string
}

func (h *SpawnHandlers) resolveQuickLaunchByName(workspaceID, name string) (*resolvedQuickLaunch, error) {
	if name == "" {
		return nil, fmt.Errorf("quick_launch_name is required")
	}
	if wsCfg := h.workspace.GetWorkspaceConfig(workspaceID); wsCfg != nil {
		if resolved := h.resolveQuickLaunchFromPresets(wsCfg.QuickLaunch, name); resolved != nil {
			return resolved, nil
		}
	}
	if resolved := h.resolveQuickLaunchFromPresets(adaptQuickLaunch(h.config.GetQuickLaunch()), name); resolved != nil {
		return resolved, nil
	}
	return nil, fmt.Errorf("quick launch not found: %s", name)
}

func (h *SpawnHandlers) resolveQuickLaunchFromPresets(presets []contracts.QuickLaunch, name string) *resolvedQuickLaunch {
	for _, preset := range presets {
		if preset.Name != name {
			continue
		}
		if strings.TrimSpace(preset.Command) != "" {
			return &resolvedQuickLaunch{Name: preset.Name, Command: strings.TrimSpace(preset.Command), PersonaID: preset.PersonaID}
		}
		if strings.TrimSpace(preset.Target) == "" {
			return nil
		}
		promptable, found := h.models.IsModel(preset.Target)
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
		return &resolvedQuickLaunch{Name: preset.Name, Target: preset.Target, Prompt: prompt, PersonaID: preset.PersonaID}
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

// These are predefined quick-run shortcuts that ship with schmux.
type BuiltinQuickLaunchCookbook struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Prompt string `json:"prompt"`
}

// handleBuiltinQuickLaunch returns the list of built-in quick launch cookbooks.
func (h *SpawnHandlers) handleBuiltinQuickLaunch(w http.ResponseWriter, r *http.Request) {
	// Try embedded file first (production), fall back to filesystem (development)
	var data []byte
	var readErr error
	sessionLog := logging.Sub(h.logger, "session")
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
			sessionLog.Error("builtin-quick-launch: failed to read file", "err", readErr)
			writeJSONError(w, "Failed to load built-in quick launch cookbooks", http.StatusInternalServerError)
			return
		}
	}

	var cookbooks []BuiltinQuickLaunchCookbook
	if err := json.Unmarshal(data, &cookbooks); err != nil {
		sessionLog.Error("builtin-quick-launch: failed to parse", "err", err)
		writeJSONError(w, "Failed to parse built-in quick launch cookbooks", http.StatusInternalServerError)
		return
	}

	// Validate and filter cookbooks
	validCookbooks := make([]BuiltinQuickLaunchCookbook, 0, len(cookbooks))
	for _, cookbook := range cookbooks {
		if strings.TrimSpace(cookbook.Name) == "" {
			sessionLog.Warn("builtin-quick-launch: skipping cookbook with empty name")
			continue
		}
		if strings.TrimSpace(cookbook.Target) == "" {
			sessionLog.Warn("builtin-quick-launch: skipping cookbook with empty target", "name", cookbook.Name)
			continue
		}
		if strings.TrimSpace(cookbook.Prompt) == "" {
			sessionLog.Warn("builtin-quick-launch: skipping cookbook with empty prompt", "name", cookbook.Name)
			continue
		}
		validCookbooks = append(validCookbooks, cookbook)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(validCookbooks); err != nil {
		h.logger.Error("failed to encode response", "handler", "builtin-quick-launch", "err", err)
	}
}

// handleCheckBranchConflict checks if a branch is already in use by a worktree.
// POST /api/check-branch-conflict
// Request body: {"repo": "git@github.com:user/repo.git", "branch": "main"}
// Response: {"conflict": false} or {"conflict": true, "workspace_id": "repo-001"}
func (h *SpawnHandlers) handleCheckBranchConflict(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Repo == "" || req.Branch == "" {
		writeJSONError(w, "repo and branch are required", http.StatusBadRequest)
		return
	}

	type BranchConflictResponse struct {
		Conflict    bool   `json:"conflict"`
		WorkspaceID string `json:"workspace_id,omitempty"`
	}

	// If not using worktrees, there's no branch conflict concern
	if !h.config.UseWorktrees() {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(BranchConflictResponse{Conflict: false}); err != nil {
			h.logger.Error("failed to encode response", "handler", "check-branch-conflict", "err", err)
		}
		return
	}

	// Check if any existing workspace has this repo+branch combination
	// (which means the branch is already checked out in a worktree).
	// Recyclable workspaces are excluded — they are available for reuse.
	for _, ws := range h.state.GetWorkspaces() {
		if ws.Repo == req.Repo && ws.Branch == req.Branch && ws.Status != state.WorkspaceStatusRecyclable {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(BranchConflictResponse{
				Conflict:    true,
				WorkspaceID: ws.ID,
			}); err != nil {
				h.logger.Error("failed to encode response", "handler", "check-branch-conflict", "err", err)
			}
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(BranchConflictResponse{Conflict: false}); err != nil {
		h.logger.Error("failed to encode response", "handler", "check-branch-conflict", "err", err)
	}
}

// handleRecentBranches returns recent branches from all configured repos.
// GET /api/recent-branches?limit=10
func (h *SpawnHandlers) handleRecentBranches(w http.ResponseWriter, r *http.Request) {
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

	branches, err := h.workspace.GetRecentBranches(ctx, limit)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to get recent branches: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(branches); err != nil {
		h.logger.Error("failed to encode response", "handler", "recent-branches", "err", err)
	}
}

// handleRecentBranchesRefresh handles POST /api/recent-branches/refresh - fetches updates from remotes.
func (h *SpawnHandlers) handleRecentBranchesRefresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Fetch updates from all origin query repos
	h.workspace.FetchOriginQueries(ctx)

	// Return fresh branches
	branches, err := h.workspace.GetRecentBranches(ctx, 10)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to get recent branches: %v", err), http.StatusInternalServerError)
		return
	}

	if branches == nil {
		branches = []workspace.RecentBranch{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"branches":      branches,
		"fetched_count": len(branches),
	}); err != nil {
		h.logger.Error("failed to encode response", "handler", "recent-branches-refresh", "err", err)
	}
}

// formatAgentSystemPrompt composes a system prompt from persona and/or style.
func formatAgentSystemPrompt(p *persona.Persona, st *style.Style) string {
	var b strings.Builder
	if p != nil {
		fmt.Fprintf(&b, "## Persona: %s\n\n", p.Name)
		if p.Expectations != "" {
			fmt.Fprintf(&b, "### Behavioral Expectations\n%s\n\n", strings.TrimSpace(p.Expectations))
		}
		fmt.Fprintf(&b, "### Instructions\n%s\n", strings.TrimSpace(p.Prompt))
	}
	if st != nil {
		if p != nil {
			b.WriteString("\n---\n\n")
		}
		fmt.Fprintf(&b, "## Communication Style: %s\n\n%s\n", st.Name, strings.TrimSpace(st.Prompt))
	}
	return b.String()
}
