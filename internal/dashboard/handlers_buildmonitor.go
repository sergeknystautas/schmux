//go:build !nobuildmonitor && !nogithub

package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/buildmonitor"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/session"
)

// buildMonitorStateDir returns the directory for build monitor state files.
func buildMonitorStateDir() string {
	return filepath.Join(schmuxdir.Get(), "build-monitor")
}

// buildMonitorUnitStatePath returns the path to a unit's state file.
func buildMonitorUnitStatePath(slug string) string {
	return filepath.Join(buildMonitorStateDir(), slug+".json")
}

// githubActionsClient adapts the github package functions to the buildmonitor.Actions interface.
type githubActionsClient struct{}

func (githubActionsClient) ListWorkflows(ctx context.Context, token string, info github.RepoInfo) ([]github.Workflow, error) {
	return github.ListWorkflows(ctx, token, info)
}

func (githubActionsClient) ListRepoRuns(ctx context.Context, token string, info github.RepoInfo, branch string) ([]github.WorkflowRun, error) {
	return github.ListRepoRuns(ctx, token, info, branch)
}

func (githubActionsClient) ListRunJobs(ctx context.Context, token string, info github.RepoInfo, runID int64) ([]github.WorkflowJob, error) {
	return github.ListRunJobs(ctx, token, info, runID)
}

// buildMonitorUnitResponse is the JSON shape for a single unit (one monitored
// repo) in the API response.
type buildMonitorUnitResponse struct {
	Slug                   string                       `json:"slug"`
	RepoName               string                       `json:"repo_name"`
	Repo                   string                       `json:"repo"`
	Branch                 string                       `json:"branch,omitempty"`
	Workflows              []buildmonitor.WorkflowState `json:"workflows,omitempty"`
	CheckedAt              string                       `json:"checked_at,omitempty"`
	LastError              string                       `json:"last_error,omitempty"`
	Configured             bool                         `json:"configured"`
	GitHubLogin            string                       `json:"github_login,omitempty"`
	RemediationWorkspaceID string                       `json:"remediation_workspace_id,omitempty"`
}

// buildMonitorResponse is the JSON shape for GET /api/build-monitor.
type buildMonitorResponse struct {
	Enabled          bool                       `json:"enabled"`
	LaunchConfigured bool                       `json:"launch_configured"`
	Units            []buildMonitorUnitResponse `json:"units"`
}

// launchDirective is one workflow failure the launcher should remediate.
// workflow is a snapshot taken at detection time so later state changes
// can't redirect the launch; stamps re-validate against live state.
type launchDirective struct {
	slug     string
	repoName string
	repoURL  string
	repo     string // owner/repo
	info     github.RepoInfo
	login    string
	workflow buildmonitor.WorkflowState
}

// collectUnitDirectives expands a unit's entered_failure events into launch
// directives, snapshotting each workflow's state row.
func collectUnitDirectives(base launchDirective, events []buildmonitor.TransitionEvent, st *buildmonitor.UnitState) []launchDirective {
	var out []launchDirective
	for _, id := range buildmonitor.PlanLaunches(events) {
		for i := range st.Workflows {
			if st.Workflows[i].WorkflowID == id {
				d := base
				d.workflow = st.Workflows[i]
				out = append(out, d)
			}
		}
	}
	return out
}

// handleBuildMonitorGet returns the persisted build monitor state for all enabled units.
func (s *Server) handleBuildMonitorGet(w http.ResponseWriter, r *http.Request) {
	response := buildMonitorResponse{
		Enabled:          s.config.GetBuildMonitorEnabled(),
		LaunchConfigured: s.config.GetBuildMonitorTarget() != "",
		Units:            []buildMonitorUnitResponse{}, // never nil: JSON must be [], not null
	}

	if !response.Enabled {
		writeJSON(w, response)
		return
	}

	repos := s.config.GetRepos()
	bmRepos := s.config.GetBuildMonitorRepos()

	for _, repo := range repos {
		if !github.IsGitHubURL(repo.URL) {
			continue
		}
		slug := repoSlug(repo.Name)
		if !s.config.GetBuildMonitorRepoEnabled(slug) {
			continue
		}
		repoCfg, ok := bmRepos[slug]
		if !ok {
			continue
		}

		unit := buildMonitorUnitResponse{
			Slug:        slug,
			RepoName:    repo.Name,
			Configured:  repoCfg.GitHubLogin != "",
			GitHubLogin: repoCfg.GitHubLogin,
		}

		// Parse owner/repo from URL
		info, err := github.ParseRepoURL(repo.URL)
		if err == nil {
			unit.Repo = info.Owner + "/" + info.Repo
		}

		// Read persisted state
		state, _ := buildmonitor.ReadState(buildMonitorUnitStatePath(slug))
		if state != nil {
			unit.Branch = state.Branch
			unit.Workflows = state.Workflows
			unit.CheckedAt = state.CheckedAt
			unit.LastError = state.LastError
			unit.RemediationWorkspaceID = state.RemediationWorkspaceID
		}

		response.Units = append(response.Units, unit)
	}

	writeJSON(w, response)
}

// handleBuildMonitorCheck fetches fresh status for all enabled units,
// persists the results, and broadcasts when anything changed.
func (s *Server) handleBuildMonitorCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	response, changed, directives := s.runBuildMonitorCheckPass(r.Context())
	if changed {
		s.BroadcastBuildMonitor()
	}
	if len(directives) > 0 {
		go s.runBuildMonitorLaunches(directives)
	}
	writeJSON(w, response)
}

// RunBuildMonitorCheck executes one scheduled check pass and broadcasts
// build_monitor_updated when anything changed. Called by the daemon scheduler.
func (s *Server) RunBuildMonitorCheck(ctx context.Context) {
	if !s.config.GetBuildMonitorEnabled() {
		return
	}
	if _, changed, directives := s.runBuildMonitorCheckPass(ctx); changed || len(directives) > 0 {
		if changed {
			s.BroadcastBuildMonitor()
		}
		if len(directives) > 0 {
			go s.runBuildMonitorLaunches(directives)
		}
	}
}

// runBuildMonitorCheckPass executes one full check pass over all enabled
// units, persisting each unit's state with transition data. Returns the API
// response, whether any unit's observable state changed, and any launch
// directives for auto-remediation. Serialized by buildMonitorCheckMu so a
// scheduler tick and a manual check cannot interleave state-file writes.
func (s *Server) runBuildMonitorCheckPass(ctx context.Context) (buildMonitorResponse, bool, []launchDirective) {
	s.buildMonitorCheckMu.Lock()
	defer s.buildMonitorCheckMu.Unlock()

	response := buildMonitorResponse{
		Enabled:          s.config.GetBuildMonitorEnabled(),
		LaunchConfigured: s.config.GetBuildMonitorTarget() != "",
		Units:            []buildMonitorUnitResponse{}, // never nil: JSON must be [], not null
	}
	if !response.Enabled {
		return response, false, nil
	}

	repos := s.config.GetRepos()
	bmRepos := s.config.GetBuildMonitorRepos()
	client := githubActionsClient{}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var directives []launchDirective
	launching := s.config.GetBuildMonitorTarget() != "" && s.config.GetBuildMonitorAutoWorkspace()
	changed := false
	for _, repo := range repos {
		if !github.IsGitHubURL(repo.URL) {
			continue
		}
		slug := repoSlug(repo.Name)
		if !s.config.GetBuildMonitorRepoEnabled(slug) {
			continue
		}
		repoCfg, ok := bmRepos[slug]
		if !ok {
			continue
		}

		info, err := github.ParseRepoURL(repo.URL)
		if err != nil {
			continue
		}

		// Resolve the repo's default branch
		branch := "main" // fallback
		if defBranch, err := s.workspace.GetDefaultBranch(ctx, repo.URL); err == nil {
			branch = defBranch
		}

		// Resolve token
		token, err := config.GetGitHubToken(repoCfg.GitHubLogin)
		if err != nil || token == "" {
			unit := buildMonitorUnitResponse{
				Slug:        slug,
				RepoName:    repo.Name,
				Repo:        info.Owner + "/" + info.Repo,
				Branch:      branch,
				Configured:  repoCfg.GitHubLogin != "",
				GitHubLogin: repoCfg.GitHubLogin,
				LastError:   "no token — authorize identity first",
			}
			response.Units = append(response.Units, unit)
			continue
		}

		unit := buildmonitor.Unit{
			Slug:     slug,
			RepoName: repo.Name,
			Repo:     info.Owner + "/" + info.Repo,
			Info:     info,
			Branch:   branch,
			Token:    token,
		}

		state := buildmonitor.CheckUnit(ctx, client, unit)
		if ctx.Err() != nil {
			// The pass was canceled (client disconnect or daemon shutdown);
			// results are tainted with context errors — do not persist them.
			break
		}
		state.CheckedAt = time.Now().UTC().Format(time.RFC3339)

		prev, readErr := buildmonitor.ReadState(buildMonitorUnitStatePath(slug))
		if readErr != nil {
			// The launcher acts on transitions, so a corrupt file silently
			// re-baselining must at least be visible in the logs.
			s.logger.Warn("failed to read previous build monitor state; treating as first check", "slug", slug, "err", readErr)
		}
		events, unitChanged := buildmonitor.ApplyTransitions(prev, state)

		if writeErr := buildmonitor.WriteState(buildMonitorUnitStatePath(slug), state); writeErr != nil {
			s.logger.Error("failed to write build monitor state", "slug", slug, "err", writeErr)
		} else {
			if unitChanged {
				// Broadcast only what is persisted: clients refetch GET, which
				// reads from disk, so a failed write would make them refetch
				// stale state — and re-broadcast on every subsequent tick.
				changed = true
			}
			if launching {
				base := launchDirective{
					slug: slug, repoName: repo.Name, repoURL: repo.URL,
					repo: info.Owner + "/" + info.Repo, info: info,
					login: repoCfg.GitHubLogin,
				}
				directives = append(directives, collectUnitDirectives(base, events, state)...)
			}
		}

		unitResp := buildMonitorUnitResponse{
			Slug:                   slug,
			RepoName:               repo.Name,
			Repo:                   info.Owner + "/" + info.Repo,
			Branch:                 branch,
			Workflows:              state.Workflows,
			CheckedAt:              state.CheckedAt,
			LastError:              state.LastError,
			Configured:             repoCfg.GitHubLogin != "",
			GitHubLogin:            repoCfg.GitHubLogin,
			RemediationWorkspaceID: state.RemediationWorkspaceID,
		}
		response.Units = append(response.Units, unitResp)
	}
	return response, changed, directives
}

// BroadcastBuildMonitor sends a build_monitor_updated message to all
// dashboard WebSocket clients. No payload; clients refetch GET /api/build-monitor.
func (s *Server) BroadcastBuildMonitor() {
	payload, err := json.Marshal(map[string]interface{}{
		"type": "build_monitor_updated",
	})
	if err != nil {
		logging.Sub(s.logger, "ws/dashboard").Error("failed to marshal build_monitor_updated message", "err", err)
		return
	}

	s.sessionsConnsMu.RLock()
	defer s.sessionsConnsMu.RUnlock()

	for conn := range s.sessionsConns {
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			logging.Sub(s.logger, "ws/dashboard").Error("failed to send build_monitor_updated message", "err", err)
		}
	}
}

// handleBuildMonitorIdentities returns the list of authorized GitHub identities for build access.
func (s *Server) handleBuildMonitorIdentities(w http.ResponseWriter, r *http.Request) {
	logins, err := config.GetGitHubIdentityLogins()
	if err != nil {
		writeJSONError(w, "Failed to read identities", http.StatusInternalServerError)
		return
	}
	if logins == nil {
		logins = []string{}
	}
	writeJSON(w, map[string]any{"logins": logins})
}

// handleBuildMonitorConnectIdentity is the connect entry point (delegates to auth_github.go).
func (s *Server) handleBuildMonitorConnectIdentity(w http.ResponseWriter, r *http.Request) {
	s.handleBuildMonitorConnect(w, r)
}

// mutateBuildMonitorState applies fn to a unit's persisted state under the
// check mutex; persists and broadcasts when fn reports a change.
func (s *Server) mutateBuildMonitorState(slug string, fn func(*buildmonitor.UnitState) bool) {
	s.buildMonitorCheckMu.Lock()
	defer s.buildMonitorCheckMu.Unlock()
	st, err := buildmonitor.ReadState(buildMonitorUnitStatePath(slug))
	if err != nil || st == nil {
		s.logger.Warn("build monitor launch: cannot read unit state", "slug", slug, "err", err)
		return
	}
	if !fn(st) {
		return
	}
	if err := buildmonitor.WriteState(buildMonitorUnitStatePath(slug), st); err != nil {
		s.logger.Error("build monitor launch: failed to write unit state", "slug", slug, "err", err)
		return
	}
	s.BroadcastBuildMonitor()
}

// runBuildMonitorLaunches remediates the directed workflow failures: one
// workspace per failure episode (recorded on the unit — never found by
// scanning), one session per workflow. Serialized by buildMonitorLaunchMu
// so overlapping check passes cannot provision concurrently.
func (s *Server) runBuildMonitorLaunches(directives []launchDirective) {
	s.buildMonitorLaunchMu.Lock()
	defer s.buildMonitorLaunchMu.Unlock()
	for _, d := range directives {
		s.launchBuildFailureSession(d)
	}
}

// launchBuildFailureSession provisions (or joins) the episode workspace and
// spawns one remediation session for the directed workflow failure.
func (s *Server) launchBuildFailureSession(d launchDirective) {
	target := s.config.GetBuildMonitorTarget()
	if !s.config.GetBuildMonitorEnabled() || target == "" {
		return // feature reconfigured between detection and launch
	}
	episodeRunID := d.workflow.FirstFailureRunID
	stamp := func(sessionID, launchErr string) {
		s.mutateBuildMonitorState(d.slug, func(st *buildmonitor.UnitState) bool {
			return buildmonitor.StampLaunch(st, d.workflow.WorkflowID, episodeRunID, sessionID, launchErr)
		})
	}
	if d.workflow.HeadSHA == "" {
		stamp("", "failing run has no recorded commit")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Resolve the episode workspace: the one this feature recorded, or
	// create it (first failure of the episode).
	var recorded string
	s.buildMonitorCheckMu.Lock()
	if st, _ := buildmonitor.ReadState(buildMonitorUnitStatePath(d.slug)); st != nil {
		recorded = st.RemediationWorkspaceID
	}
	s.buildMonitorCheckMu.Unlock()

	createdWorkspace := recorded == ""

	var wsID, wsPath string
	if recorded != "" {
		ws, found := s.workspace.GetByID(recorded)
		if !found {
			stamp("", fmt.Sprintf("remediation workspace %s no longer exists", recorded))
			return
		}
		wsID, wsPath = ws.ID, ws.Path
	} else {
		// No label: git workspaces display as ID + branch; labels are sapling-only.
		ws, err := s.workspace.GetOrCreateWithLabel(ctx, d.repoURL, buildmonitor.FixBranch(d.workflow.Name, d.workflow.HeadSHA), "")
		if err != nil {
			stamp("", fmt.Sprintf("workspace creation failed: %v", err))
			return
		}
		wsID, wsPath = ws.ID, ws.Path
		s.mutateBuildMonitorState(d.slug, func(st *buildmonitor.UnitState) bool {
			return buildmonitor.StampWorkspace(st, ws.ID, d.workflow.HeadSHA)
		})
	}

	sessionID, err := s.spawnBuildFailureSession(ctx, d, wsID, wsPath, target)
	if err != nil {
		stamp("", err.Error())
		return
	}
	stamp(sessionID, "")
	if createdWorkspace {
		// One focus pull per failure episode: every connected dashboard
		// navigates to the episode's first remediation session so a broken
		// build is impossible to miss. Joining sessions don't re-yank.
		s.BroadcastPendingNavigation("session", sessionID, "")
	}
	go s.BroadcastSessions()
}

// spawnBuildFailureSession downloads failed-job logs, writes the failure
// context into the workspace, and spawns the remediation session.
func (s *Server) spawnBuildFailureSession(ctx context.Context, d launchDirective, workspaceID, workspacePath, target string) (string, error) {
	token, err := config.GetGitHubToken(d.login)
	if err != nil || token == "" {
		return "", fmt.Errorf("no token for identity %q", d.login)
	}
	logs := map[int64][]byte{}
	logErrors := map[int64]string{}
	for _, j := range d.workflow.FailedJobs {
		if j.ID == 0 {
			continue // state written before job IDs were recorded
		}
		data, err := github.DownloadJobLogs(ctx, token, d.info, j.ID)
		if err != nil {
			logErrors[j.ID] = err.Error()
			continue
		}
		logs[j.ID] = data
	}
	info := buildmonitor.FailureInfo{RepoName: d.repoName, Repo: d.repo, Workflow: d.workflow}
	if _, err := buildmonitor.WriteWorkspaceContext(workspacePath, info, logs, logErrors); err != nil {
		return "", fmt.Errorf("failed to write failure context: %v", err)
	}
	prompt := buildmonitor.BuildPrompt(info, buildmonitor.ContextDir(d.workflow.Name))
	nickname := fmt.Sprintf("Fix %s: %s@%s", d.workflow.Name, d.repoName, buildmonitor.ShortSHA(d.workflow.HeadSHA))
	sess, err := s.session.Spawn(ctx, session.SpawnOptions{
		WorkspaceID: workspaceID,
		TargetName:  target,
		Prompt:      prompt,
		Nickname:    nickname,
	})
	if err != nil {
		return "", fmt.Errorf("session launch failed: %v", err)
	}
	return sess.ID, nil
}

// handleBuildMonitorLaunch handles
// POST /api/build-monitor/repos/{slug}/failures/{runID}/launch-workspace.
// Manual launch: always a fresh, unique workspace (unique branch suffix),
// one remediation session. Does not touch unit state — the response is for
// navigation, not bookkeeping.
func (s *Server) handleBuildMonitorLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.config.GetBuildMonitorEnabled() {
		writeJSONError(w, "Build monitor is not enabled", http.StatusBadRequest)
		return
	}
	target := s.config.GetBuildMonitorTarget()
	if target == "" {
		writeJSONError(w, "No build_monitor target configured", http.StatusBadRequest)
		return
	}
	slug := chi.URLParam(r, "slug")
	runID, err := strconv.ParseInt(chi.URLParam(r, "runID"), 10, 64)
	if err != nil {
		writeJSONError(w, "Invalid run id", http.StatusBadRequest)
		return
	}
	if !s.config.GetBuildMonitorRepoEnabled(slug) {
		writeJSONError(w, "Repo is not monitored", http.StatusNotFound)
		return
	}
	repoCfg, ok := s.config.GetBuildMonitorRepo(slug)
	if !ok || repoCfg.GitHubLogin == "" {
		writeJSONError(w, "Repo has no authorized identity", http.StatusBadRequest)
		return
	}
	var repoName, repoURL string
	for _, repo := range s.config.GetRepos() {
		if repoSlug(repo.Name) == slug {
			repoName, repoURL = repo.Name, repo.URL
			break
		}
	}
	if repoURL == "" {
		writeJSONError(w, "Unknown repo", http.StatusNotFound)
		return
	}
	info, err := github.ParseRepoURL(repoURL)
	if err != nil {
		writeJSONError(w, "Repo is not a GitHub repo", http.StatusBadRequest)
		return
	}
	st, err := buildmonitor.ReadState(buildMonitorUnitStatePath(slug))
	if err != nil || st == nil {
		writeJSONError(w, "No build monitor state for repo — run a check first", http.StatusNotFound)
		return
	}
	var wf *buildmonitor.WorkflowState
	for i := range st.Workflows {
		if st.Workflows[i].RunID == runID && st.Workflows[i].Conclusion == "failure" {
			wf = &st.Workflows[i]
			break
		}
	}
	if wf == nil {
		writeJSONError(w, "Run is not a known failing run", http.StatusNotFound)
		return
	}
	if wf.HeadSHA == "" {
		writeJSONError(w, "Failing run has no recorded commit — run a check first", http.StatusConflict)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	// Always fresh and unique: a timestamp suffix keeps this launch from
	// reusing a prior launch's workspace for the same commit. No label: git
	// workspaces display as ID + branch; labels are sapling-only.
	branch := fmt.Sprintf("%s-%d", buildmonitor.FixBranch(wf.Name, wf.HeadSHA), time.Now().UnixNano()%0xFFFF)
	ws, err := s.workspace.GetOrCreateWithLabel(ctx, repoURL, branch, "")
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to create workspace: %v", err), http.StatusInternalServerError)
		return
	}
	d := launchDirective{
		slug: slug, repoName: repoName, repoURL: repoURL,
		repo: info.Owner + "/" + info.Repo, info: info,
		login: repoCfg.GitHubLogin, workflow: *wf,
	}
	sessionID, err := s.spawnBuildFailureSession(ctx, d, ws.ID, ws.Path, target)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Workspace created but session launch failed: %v", err), http.StatusInternalServerError)
		return
	}
	go s.BroadcastSessions()
	writeJSON(w, contracts.BuildMonitorLaunchResponse{WorkspaceID: ws.ID, SessionID: sessionID})
}
