//go:build !nobuildmonitor && !nogithub

package dashboard

import (
	"strings"

	"github.com/sergeknystautas/schmux/internal/buildmonitor"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// buildMonitorWorkspacePrefix is the name prefix every Build Monitor
// dedicated workspace carries.
const buildMonitorWorkspacePrefix = "build-monitor/"

// syncBuildMonitorWorkspaces reconciles the dedicated workspace
// registrations with the monitored repos: every workflow of a monitored repo
// is registered, and the names of repos that are no longer monitored are
// given up. A workflow missing from one check pass keeps its workspace — a
// transient gap must not orphan it.
//
// It runs at daemon start, after every config save, after every config file
// change, and after every check pass.
func (s *Server) syncBuildMonitorWorkspaces() {
	monitored := map[string]bool{}
	if s.config.GetBuildMonitorEnabled() {
		bmRepos := s.config.GetBuildMonitorRepos()
		for _, repo := range s.repos.List() {
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
			monitored[slug] = true
			s.registerBuildMonitorWorkflows(slug, repo.URL)
		}
	}

	changed := false
	for _, name := range s.workspace.RegisteredDedicated(buildMonitorWorkspacePrefix) {
		if monitored[buildMonitorRepoSlug(name)] {
			continue
		}
		if err := s.workspace.UnregisterDedicated(name); err != nil {
			s.logger.Warn("build monitor: failed to unregister the dedicated workspace", "name", name, "err", err)
			continue
		}
		changed = true
	}
	if changed {
		s.BroadcastSessions()
	}
}

// registerBuildMonitorWorkflows registers one dedicated workspace per
// workflow of a monitored repo, on the branch that workflow's agent works
// on.
func (s *Server) registerBuildMonitorWorkflows(slug, repoURL string) {
	st, err := buildmonitor.ReadState(buildMonitorUnitStatePath(slug))
	if err != nil || st == nil {
		return
	}
	for _, wf := range st.Workflows {
		s.workspace.RegisterDedicated(buildmonitor.WorkspaceName(slug, wf.Name), workspace.DedicatedRegistration{
			RepoURL: repoURL,
			Branch:  buildmonitor.WorkspaceBranch(wf.Name),
			Label:   buildmonitor.WorkspaceLabel(slug, wf.Name),
		})
	}
}

// buildMonitorRepoSlug returns the repo segment of a Build Monitor workspace
// name ("build-monitor/<repo-slug>/<workflow-slug>"). A name of any other
// shape belongs to no repo.
func buildMonitorRepoSlug(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) != 3 {
		return ""
	}
	return parts[1]
}
