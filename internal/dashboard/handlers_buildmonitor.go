//go:build !nobuildmonitor && !nogithub

package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/buildmonitor"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
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
	Slug        string                       `json:"slug"`
	RepoName    string                       `json:"repo_name"`
	Repo        string                       `json:"repo"`
	Branch      string                       `json:"branch,omitempty"`
	Workflows   []buildmonitor.WorkflowState `json:"workflows,omitempty"`
	CheckedAt   string                       `json:"checked_at,omitempty"`
	LastError   string                       `json:"last_error,omitempty"`
	Configured  bool                         `json:"configured"`
	GitHubLogin string                       `json:"github_login,omitempty"`
}

// buildMonitorResponse is the JSON shape for GET /api/build-monitor.
type buildMonitorResponse struct {
	Enabled bool                       `json:"enabled"`
	Units   []buildMonitorUnitResponse `json:"units"`
}

// handleBuildMonitorGet returns the persisted build monitor state for all enabled units.
func (s *Server) handleBuildMonitorGet(w http.ResponseWriter, r *http.Request) {
	response := buildMonitorResponse{
		Enabled: s.config.GetBuildMonitorEnabled(),
		Units:   []buildMonitorUnitResponse{}, // never nil: JSON must be [], not null
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
	response, changed := s.runBuildMonitorCheckPass(r.Context())
	if changed {
		s.BroadcastBuildMonitor()
	}
	writeJSON(w, response)
}

// RunBuildMonitorCheck executes one scheduled check pass and broadcasts
// build_monitor_updated when anything changed. Called by the daemon scheduler.
func (s *Server) RunBuildMonitorCheck(ctx context.Context) {
	if !s.config.GetBuildMonitorEnabled() {
		return
	}
	if _, changed := s.runBuildMonitorCheckPass(ctx); changed {
		s.BroadcastBuildMonitor()
	}
}

// runBuildMonitorCheckPass executes one full check pass over all enabled
// units, persisting each unit's state with transition data. Returns the API
// response and whether any unit's observable state changed. Serialized by
// buildMonitorCheckMu so a scheduler tick and a manual check cannot
// interleave state-file writes.
func (s *Server) runBuildMonitorCheckPass(ctx context.Context) (buildMonitorResponse, bool) {
	s.buildMonitorCheckMu.Lock()
	defer s.buildMonitorCheckMu.Unlock()

	response := buildMonitorResponse{
		Enabled: s.config.GetBuildMonitorEnabled(),
		Units:   []buildMonitorUnitResponse{}, // never nil: JSON must be [], not null
	}
	if !response.Enabled {
		return response, false
	}

	repos := s.config.GetRepos()
	bmRepos := s.config.GetBuildMonitorRepos()
	client := githubActionsClient{}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

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
			// Phase C will act on transitions, so a corrupt file silently
			// re-baselining must at least be visible in the logs.
			s.logger.Warn("failed to read previous build monitor state; treating as first check", "slug", slug, "err", readErr)
		}
		_, unitChanged := buildmonitor.ApplyTransitions(prev, state)

		if writeErr := buildmonitor.WriteState(buildMonitorUnitStatePath(slug), state); writeErr != nil {
			s.logger.Error("failed to write build monitor state", "slug", slug, "err", writeErr)
		} else if unitChanged {
			// Broadcast only what is persisted: clients refetch GET, which
			// reads from disk, so a failed write would make them refetch
			// stale state — and re-broadcast on every subsequent tick.
			changed = true
		}

		unitResp := buildMonitorUnitResponse{
			Slug:        slug,
			RepoName:    repo.Name,
			Repo:        info.Owner + "/" + info.Repo,
			Branch:      branch,
			Workflows:   state.Workflows,
			CheckedAt:   state.CheckedAt,
			LastError:   state.LastError,
			Configured:  repoCfg.GitHubLogin != "",
			GitHubLogin: repoCfg.GitHubLogin,
		}
		response.Units = append(response.Units, unitResp)
	}
	return response, changed
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
