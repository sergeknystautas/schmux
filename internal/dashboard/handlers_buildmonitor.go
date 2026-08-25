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
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
)

// buildMonitorUnitStatePath returns the path to a unit's state file.
// (buildMonitorStateDir lives in server.go, untagged.)
func buildMonitorUnitStatePath(slug string) string {
	return filepath.Join(buildMonitorStateDir(), slug+".json")
}

// hydrateBuildMonitor seeds the monitor's commit store from the durable unit
// snapshots at startup so CI chips show last-known status before the first
// check pass completes. (The page reads the snapshot files directly.) No-op
// when the feature is disabled.
func (s *Server) hydrateBuildMonitor() {
	if !s.config.GetBuildMonitorEnabled() {
		return
	}
	snapshots := map[string]*buildmonitor.UnitState{}
	for _, repo := range s.config.GetRepos() {
		slug := repoSlug(repo.Name)
		if !s.config.GetBuildMonitorRepoEnabled(slug) {
			continue
		}
		st, err := buildmonitor.ReadState(buildMonitorUnitStatePath(slug))
		if err != nil {
			s.logger.Warn("build monitor: cannot read unit state, skipping hydration", "slug", slug, "err", err)
			continue
		}
		if st != nil {
			snapshots[slug] = st
		}
	}
	s.buildMonitor.Hydrate(snapshots)
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
func collectUnitDirectives(base launchDirective, events []buildmonitor.TransitionEvent, st *buildmonitor.UnitState, observedAt string) []launchDirective {
	var out []launchDirective
	for _, id := range buildmonitor.PlanLaunches(events) {
		for i := range st.Workflows {
			if st.Workflows[i].WorkflowID == id {
				if !buildmonitor.ClaimRemediation(st, st.Workflows[i], observedAt) {
					continue
				}
				d := base
				d.workflow = st.Workflows[i]
				out = append(out, d)
			}
		}
	}
	return out
}

// handleBuildMonitorGet serves the build monitor's live state for all enabled units.
func (s *Server) handleBuildMonitorGet(w http.ResponseWriter, r *http.Request) {
	response := contracts.BuildMonitorResponse{
		Enabled:          s.config.GetBuildMonitorEnabled(),
		LaunchConfigured: s.config.GetBuildMonitorTarget() != "",
		Units:            []contracts.BuildMonitorUnit{}, // never nil: JSON must be [], not null
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
		if _, ok := bmRepos[slug]; !ok {
			continue
		}

		login := s.config.GetGitHubLogin(repo.URL)
		unit := contracts.BuildMonitorUnit{
			Slug:        slug,
			RepoName:    repo.Name,
			Configured:  login != "",
			GitHubLogin: login,
		}

		info, err := github.ParseRepoURL(repo.URL)
		if err != nil {
			response.Units = append(response.Units, unit)
			continue
		}
		unit.Repo = info.Owner + "/" + info.Repo

		// The durable unit file is the only copy of the snapshot (the pass
		// writes it, launch stamps write it); the head commit's status comes
		// from the single Status derivation.
		if st, _ := buildmonitor.ReadState(buildMonitorUnitStatePath(slug)); st != nil {
			unit.Branch = st.Branch
			unit.HeadSHA = st.HeadSHA
			unit.Workflows = toContractWorkflows(st.Workflows)
			unit.CheckedAt = st.CheckedAt
			unit.LastError = st.LastError
			unit.RemediationWorkspaceID = st.RemediationWorkspaceID
			if status, _, ok := s.buildMonitor.Status(info, st.Branch, st.HeadSHA); ok {
				unit.Status = status
			}
		}

		response.Units = append(response.Units, unit)
	}

	writeJSON(w, response)
}

func toContractWorkflows(in []buildmonitor.WorkflowState) []contracts.BuildMonitorWorkflow {
	if len(in) == 0 {
		return []contracts.BuildMonitorWorkflow{}
	}
	out := make([]contracts.BuildMonitorWorkflow, 0, len(in))
	for _, wf := range in {
		out = append(out, contracts.BuildMonitorWorkflow{
			Name:        wf.Name,
			Path:        wf.Path,
			RunID:       wf.RunID,
			RunNumber:   wf.RunNumber,
			Status:      wf.Status,
			Conclusion:  wf.Conclusion,
			HTMLURL:     wf.HTMLURL,
			HeadSHA:     wf.HeadSHA,
			SessionID:   wf.SessionID,
			LaunchError: wf.LaunchError,
			FailedJobs:  toContractFailedJobs(wf.FailedJobs),
		})
	}
	return out
}

func toContractFailedJobs(in []buildmonitor.FailedJob) []contracts.BuildMonitorFailedJob {
	if len(in) == 0 {
		return []contracts.BuildMonitorFailedJob{}
	}
	out := make([]contracts.BuildMonitorFailedJob, 0, len(in))
	for _, j := range in {
		out = append(out, contracts.BuildMonitorFailedJob{
			ID:      j.ID,
			Name:    j.Name,
			HTMLURL: j.HTMLURL,
		})
	}
	return out
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
// build_monitor_updated when anything changed. Called by the daemon scheduler,
// unconditionally: the enabled flag is pass input so the monitor itself
// observes and handles the enabled→disabled transition.
func (s *Server) RunBuildMonitorCheck(ctx context.Context) {
	_, changed, directives := s.runBuildMonitorCheckPass(ctx)
	// Every enabled pass advances CheckedAt, which the page displays, so
	// notify after each one — not only on status changes. A disable
	// transition reports changed once and then goes quiet.
	if changed || s.config.GetBuildMonitorEnabled() {
		s.BroadcastBuildMonitor()
	}
	if len(directives) > 0 {
		go s.runBuildMonitorLaunches(directives)
	}
}

// runBuildMonitorCheckPass gathers inputs (units + watched heads), asks the
// build monitor to run a commit-centric pass, refreshes PR visibility per
// repo, and translates the result into the API response plus any launch
// directives. Serialized by buildMonitorCheckMu so a scheduler tick and a
// manual check cannot interleave.
func (s *Server) runBuildMonitorCheckPass(ctx context.Context) (contracts.BuildMonitorResponse, bool, []launchDirective) {
	s.buildMonitorCheckMu.Lock()
	defer s.buildMonitorCheckMu.Unlock()

	response := contracts.BuildMonitorResponse{
		Enabled:          s.config.GetBuildMonitorEnabled(),
		LaunchConfigured: s.config.GetBuildMonitorTarget() != "",
		Units:            []contracts.BuildMonitorUnit{}, // never nil: JSON must be [], not null
	}

	// Disabled is a state update through the monitor, not a cleanup the
	// dashboard performs: the monitor observes enabled=false and drops its
	// fetched knowledge as part of the transition.
	if !response.Enabled {
		passResult := s.buildMonitor.CheckPass(ctx, githubActionsClient{}, false, nil, nil)
		changed := passResult.Changed || s.prTracker.Clear()
		if changed {
			s.BroadcastSessions()
		}
		return response, changed, nil
	}

	repos := s.config.GetRepos()
	bmRepos := s.config.GetBuildMonitorRepos()
	client := githubActionsClient{}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Gather unit + head inputs once.
	units, heads, eligibleByRepo, noTokenUnits := s.buildMonitorInputs(ctx, repos, bmRepos)
	// Add the no-token placeholders so the response keeps the configured/non-configured
	// shape users expect even before they authorize.
	response.Units = append(response.Units, noTokenUnits...)

	passResult := s.buildMonitor.CheckPass(ctx, client, true, units, heads)
	sessionsChanged := false

	// Build the API response from the monitor's live state. Iterating the
	// pass inputs keeps no-token repos from being appended twice.
	for _, u := range units {
		login := s.config.GetGitHubLogin(repoURLByName(repos, u.RepoName))
		unitResp := contracts.BuildMonitorUnit{
			Slug:        u.Slug,
			RepoName:    u.RepoName,
			Repo:        u.Info.Owner + "/" + u.Info.Repo,
			Branch:      u.Branch,
			HeadSHA:     u.HeadSHA,
			Configured:  login != "",
			GitHubLogin: login,
		}
		if st := passResult.UnitStates[u.Slug]; st != nil {
			unitResp.Workflows = toContractWorkflows(st.Workflows)
			unitResp.CheckedAt = st.CheckedAt
			unitResp.LastError = st.LastError
			unitResp.RemediationWorkspaceID = st.RemediationWorkspaceID
			if unitResp.HeadSHA == "" {
				unitResp.HeadSHA = st.HeadSHA
			}
		}
		if status, _, ok := s.buildMonitor.Status(u.Info, u.Branch, u.HeadSHA); ok {
			unitResp.Status = status
		}
		response.Units = append(response.Units, unitResp)

		// Refresh PR visibility for eligible workspaces of this repo.
		token, err := config.GetGitHubToken(login)
		if err == nil && token != "" {
			if s.prTracker.Refresh(ctx, token, u.Info, eligibleByRepo[u.Info]) {
				sessionsChanged = true
			}
		}
	}

	// Build directives from the unit transition events the monitor surfaced.
	launching := s.config.GetBuildMonitorTarget() != "" && s.config.GetBuildMonitorAutoWorkspace()
	var directives []launchDirective
	if launching {
		for _, u := range units {
			events := passResult.Events[u.Slug]
			if len(events) == 0 {
				continue
			}
			st := passResult.UnitStates[u.Slug]
			if st == nil {
				continue
			}
			base := launchDirective{
				slug: u.Slug, repoName: u.RepoName, repoURL: repoURLByName(repos, u.RepoName),
				repo: u.Info.Owner + "/" + u.Info.Repo, info: u.Info,
				login: s.config.GetGitHubLogin(repoURLByName(repos, u.RepoName)),
			}
			directives = append(directives, collectUnitDirectives(base, events, st, st.CheckedAt)...)
		}
	}

	// Drop evicted workspaces from the PR tracker.
	liveIDs := map[string]bool{}
	for _, pws := range eligibleByRepo {
		for _, pw := range pws {
			liveIDs[pw.ID] = true
		}
	}
	prChanged := s.prTracker.DropExcept(liveIDs)

	changed := passResult.Changed || sessionsChanged || prChanged
	if changed {
		s.BroadcastSessions()
	}
	return response, changed, directives
}

// repoURLByName finds a configured repo's URL by display name.
func repoURLByName(repos []config.Repo, name string) string {
	for _, repo := range repos {
		if repo.Name == name {
			return repo.URL
		}
	}
	return ""
}

// buildMonitorInputs assembles unit + watched-head inputs for one check pass.
// Workspaces are resolved to (CI repo, branch, head SHA) here — including
// fork CI repos — so no workspace identity crosses the monitor boundary. Also
// returns placeholders for repos with no GitHub identity token (so the API
// response still surfaces "Configured=false"), plus eligible workspaces
// grouped by base repo for the PR refresh step.
func (s *Server) buildMonitorInputs(
	ctx context.Context,
	repos []config.Repo,
	bmRepos map[string]config.BuildMonitorRepoConfig,
) (
	[]buildmonitor.UnitInput,
	[]buildmonitor.HeadInput,
	map[github.RepoInfo][]prWorkspace,
	[]contracts.BuildMonitorUnit,
) {
	var units []buildmonitor.UnitInput
	var heads []buildmonitor.HeadInput
	eligible := map[github.RepoInfo][]prWorkspace{}
	var placeholders []contracts.BuildMonitorUnit

	for _, repo := range repos {
		if !github.IsGitHubURL(repo.URL) {
			continue
		}
		slug := repoSlug(repo.Name)
		if !s.config.GetBuildMonitorRepoEnabled(slug) {
			continue
		}
		if _, ok := bmRepos[slug]; !ok {
			continue
		}
		info, err := github.ParseRepoURL(repo.URL)
		if err != nil {
			continue
		}

		branch := "main"
		if defBranch, err := s.workspace.GetDefaultBranch(ctx, repo.URL); err == nil && defBranch != "" {
			branch = defBranch
		}

		login := s.config.GetGitHubLogin(repo.URL)
		token, err := config.GetGitHubToken(login)
		if err != nil || token == "" {
			placeholders = append(placeholders, contracts.BuildMonitorUnit{
				Slug:        slug,
				RepoName:    repo.Name,
				Repo:        info.Owner + "/" + info.Repo,
				Branch:      branch,
				Configured:  login != "",
				GitHubLogin: login,
				LastError:   "no token — authorize identity first",
			})
			continue
		}

		headSHA, err := s.workspace.GetRemoteHeadSHA(ctx, repo.URL, branch)
		if err != nil {
			s.logger.Debug("build monitor: default-branch head unresolvable", "repo", repo.Name, "err", err)
		}

		units = append(units, buildmonitor.UnitInput{
			Slug:      slug,
			RepoName:  repo.Name,
			Branch:    branch,
			Token:     token,
			Info:      info,
			HeadSHA:   headSHA,
			StatePath: buildMonitorUnitStatePath(slug),
		})

		// Eligible workspaces: same base repo, branch exists, not remote, not
		// recyclable, remote head known. Each becomes a watched head under its
		// CI repo (fork-resolved). The head comes from the SAME stored fields
		// the sessions read path uses (ciRepoForWorkspace + RemoteHeadSHA) —
		// never from a separate git derivation — so the watch set and the CI
		// chips cannot ask about different commits.
		for _, w := range s.state.GetWorkspaces() {
			if w.Repo != repo.URL || !w.RemoteBranchExists || w.RemoteHostID != "" || w.Status == state.WorkspaceStatusRecyclable {
				continue
			}
			if w.RemoteHeadSHA == "" {
				continue
			}
			if w.RemoteBranchIsFork && !github.IsGitHubURL(w.RemoteBranchURL) {
				continue
			}
			heads = append(heads, buildmonitor.HeadInput{
				Info:   ciRepoForWorkspace(w),
				Branch: w.Branch,
				SHA:    w.RemoteHeadSHA,
				Token:  token,
			})

			pw := prWorkspace{ID: w.ID, URL: w.Repo, Branch: w.Branch}
			if w.RemoteBranchIsFork {
				pw.ForkRemoteURL = w.RemoteBranchURL
			}
			eligible[info] = append(eligible[info], pw)
		}
	}

	return units, heads, eligible, placeholders
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
	}
	s.mutateBuildMonitorState(d.slug, func(st *buildmonitor.UnitState) bool {
		changed := false
		if createdWorkspace && buildmonitor.StampWorkspace(st, wsID, d.workflow.HeadSHA) {
			changed = true
		}
		if buildmonitor.StampRemediationWorkspace(st, d.workflow.WorkflowID, episodeRunID, wsID) {
			changed = true
		}
		return changed
	})

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

// buildMonitorFenceOptions decides whether build-monitor remediation sessions
// run fenced. Returns (false, "") — graceful unfenced fallback — whenever the
// toggle is off, fence mode is disabled, or the fence tool is unavailable.
// (The interactive spawn handler hard-fails an un-honorable fence request; the
// automated build-monitor path must degrade quietly instead.)
func (s *Server) buildMonitorFenceOptions() (bool, string) {
	if !s.config.GetFenceBuildMonitor() {
		return false, ""
	}
	if s.config.GetFenceMode() == config.FenceModeDisabled {
		return false, ""
	}
	st, ok := s.dependencyReport().Status("fence")
	if !ok || !st.Detected || st.Command == "" {
		return false, ""
	}
	return true, st.Command
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
	fence, fenceCommand := s.buildMonitorFenceOptions()
	sess, err := s.session.Spawn(ctx, session.SpawnOptions{
		WorkspaceID:  workspaceID,
		TargetName:   target,
		Prompt:       prompt,
		Nickname:     nickname,
		Fence:        fence,
		FenceCommand: fenceCommand,
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
	login := s.config.GetGitHubLogin(repoURL)
	if login == "" {
		writeJSONError(w, "Repo has no authorized identity", http.StatusBadRequest)
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
		login: login, workflow: *wf,
	}
	sessionID, err := s.spawnBuildFailureSession(ctx, d, ws.ID, ws.Path, target)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Workspace created but session launch failed: %v", err), http.StatusInternalServerError)
		return
	}
	go s.BroadcastSessions()
	writeJSON(w, contracts.BuildMonitorLaunchResponse{WorkspaceID: ws.ID, SessionID: sessionID})
}
