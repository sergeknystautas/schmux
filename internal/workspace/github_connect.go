package workspace

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/github"
)

// Step names for the GitHub connect flow (mirrored in the contracts doc
// comments and the dashboard's step-label map).
const (
	ConnectStepSetOrigin      = "set_origin"
	ConnectStepCreateRepo     = "create_repo"
	ConnectStepUpdateConfig   = "update_config"
	ConnectStepLinkWorkspaces = "link_workspaces"
	ConnectStepInitialPush    = "initial_push"
)

// ConnectDetection is the observed connect state of a workspace, computed
// fresh before every plan preview (GET) and every run (POST).
type ConnectDetection struct {
	Eligible         bool
	RepoName         string // schmux repo name, e.g. "talkback"
	OriginURL        string // empty when no origin remote is set
	RemoteReachable  bool
	RemoteHasRefs    bool
	ConfigURLIsLocal bool
	StateRepoIsLocal bool
}

// DetectGitHubConnect inspects a workspace's git dir, config entry, and state
// record to determine which connect steps are still needed.
func (m *Manager) DetectGitHubConnect(ctx context.Context, workspaceID string) (*ConnectDetection, error) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	d := &ConnectDetection{}
	d.StateRepoIsLocal = isLocalRepoURL(w.Repo)
	if d.StateRepoIsLocal {
		d.RepoName = strings.TrimPrefix(w.Repo, "local:")
	}

	if url, err := m.gitGetRemoteURL(w.Path); err == nil {
		d.OriginURL = url
	}

	if d.RepoName != "" {
		if r, ok := m.config.FindRepo(d.RepoName); ok {
			d.ConfigURLIsLocal = isLocalRepoURL(r.URL)
		}
	} else if d.OriginURL != "" {
		// State already points at a remote URL; the config entry may still be
		// local: (hand-edited state). Try to find it by the repo's short name.
		if info, err := github.ParseRepoURL(d.OriginURL); err == nil {
			if r, ok := m.config.FindRepo(info.Repo); ok && isLocalRepoURL(r.URL) {
				d.RepoName = info.Repo
				d.ConfigURLIsLocal = true
			}
		}
	}

	d.Eligible = d.StateRepoIsLocal || d.ConfigURLIsLocal
	if d.Eligible && d.OriginURL != "" {
		d.RemoteReachable, d.RemoteHasRefs = m.gitLsRemote(ctx, w.Path)
	}
	return d, nil
}

// gitLsRemote probes origin. reachable=false covers both "no such repo" and
// network/auth failures; hasRefs is true when the remote has any branch or tag.
func (m *Manager) gitLsRemote(ctx context.Context, dir string) (reachable, hasRefs bool) {
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", "--tags", "origin")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false, false
	}
	return true, strings.TrimSpace(string(out)) != ""
}

// BuildConnectPlan renders a detection as the ordered step plan shown in the
// dashboard dialog and used to drive RunGitHubConnect.
func BuildConnectPlan(d *ConnectDetection) []contracts.GitHubConnectPlanStep {
	reason := func(needed bool, yes, no string) string {
		if needed {
			return yes
		}
		return no
	}
	setOrigin := d.OriginURL == ""
	create := !d.RemoteReachable
	push := !d.RemoteHasRefs
	return []contracts.GitHubConnectPlanStep{
		{Step: ConnectStepSetOrigin, Needed: setOrigin, Reason: reason(setOrigin, "workspace has no origin remote", "origin already set")},
		{Step: ConnectStepCreateRepo, Needed: create, Reason: reason(create, "no reachable remote repository", "remote repository exists")},
		{Step: ConnectStepUpdateConfig, Needed: d.ConfigURLIsLocal, Reason: reason(d.ConfigURLIsLocal, "schmux config still records a local repo", "config already updated")},
		{Step: ConnectStepLinkWorkspaces, Needed: d.StateRepoIsLocal, Reason: reason(d.StateRepoIsLocal, "workspace still linked to the local repo", "workspace already linked")},
		{Step: ConnectStepInitialPush, Needed: push, Reason: reason(push, "remote has no branches yet", "remote already has refs")},
	}
}

// GitHubRepoCreator is the gh-CLI surface RunGitHubConnect needs.
// *github.CLI implements it; tests use a fake backed by local bare repos.
type GitHubRepoCreator interface {
	CreateRepo(ctx context.Context, owner, name string, private bool) error
	RepoURL(owner, name string) string
}

var connectBranchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// RunGitHubConnect executes the connect pipeline: re-detects, then runs only
// the needed steps in order, stopping at the first failure. Idempotent —
// re-running skips completed steps.
func (m *Manager) RunGitHubConnect(ctx context.Context, workspaceID string, req contracts.GitHubConnectRequest, gh GitHubRepoCreator) (*contracts.GitHubConnectResult, error) {
	if !m.LockWorkspace(workspaceID) {
		return nil, ErrWorkspaceLocked
	}
	defer m.UnlockWorkspace(workspaceID)

	d, err := m.DetectGitHubConnect(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if !d.Eligible {
		return nil, ErrNotConnectEligible
	}
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	owner := strings.TrimSpace(req.Owner)
	name := strings.TrimSpace(req.Name)
	branch := strings.TrimSpace(req.DefaultBranch)
	if branch == "" {
		branch = "main"
	}
	needCreate := !d.RemoteReachable
	if d.OriginURL == "" && (owner == "" || name == "") {
		return nil, ErrConnectMissingTarget
	}

	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = w.Path
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	res := &contracts.GitHubConnectResult{}
	add := func(step, status, detail string) {
		res.Steps = append(res.Steps, contracts.GitHubConnectStepResult{Step: step, Status: status, Detail: detail})
	}
	abort := func(step, detail string, rest ...string) *contracts.GitHubConnectResult {
		add(step, "failed", detail)
		for _, r := range rest {
			add(r, "not_run", "")
		}
		return res
	}

	// Step 1: set origin. Runs BEFORE repo creation so the chosen target is
	// durably recorded in the workspace even if a later step (or the daemon)
	// dies — re-detection then resumes from the origin URL.
	originURL := d.OriginURL
	switch {
	case originURL == "":
		originURL = gh.RepoURL(owner, name)
		if out, err := run("remote", "add", "origin", originURL); err != nil {
			return abort(ConnectStepSetOrigin, out, ConnectStepCreateRepo, ConnectStepUpdateConfig, ConnectStepLinkWorkspaces, ConnectStepInitialPush), nil
		}
		add(ConnectStepSetOrigin, "done", originURL)
	case needCreate && owner != "" && name != "":
		// Re-submission after a failed create: repoint origin if the target changed.
		if info, err := github.ParseRepoURL(originURL); err != nil || info.Owner != owner || info.Repo != name {
			originURL = gh.RepoURL(owner, name)
			if out, err := run("remote", "set-url", "origin", originURL); err != nil {
				return abort(ConnectStepSetOrigin, out, ConnectStepCreateRepo, ConnectStepUpdateConfig, ConnectStepLinkWorkspaces, ConnectStepInitialPush), nil
			}
			add(ConnectStepSetOrigin, "done", originURL)
		} else {
			add(ConnectStepSetOrigin, "skipped", "origin already set")
		}
	default:
		add(ConnectStepSetOrigin, "skipped", "origin already set")
	}

	// Step 2: create the remote repo.
	if needCreate {
		if owner == "" || name == "" {
			info, err := github.ParseRepoURL(originURL)
			if err != nil {
				return abort(ConnectStepCreateRepo, fmt.Sprintf("cannot determine owner/name from origin URL %q", originURL), ConnectStepUpdateConfig, ConnectStepLinkWorkspaces, ConnectStepInitialPush), nil
			}
			owner, name = info.Owner, info.Repo
		}
		private := req.Visibility != "public"
		if err := gh.CreateRepo(ctx, owner, name, private); err != nil {
			return abort(ConnectStepCreateRepo, err.Error(), ConnectStepUpdateConfig, ConnectStepLinkWorkspaces, ConnectStepInitialPush), nil
		}
		add(ConnectStepCreateRepo, "done", owner+"/"+name)
	} else {
		add(ConnectStepCreateRepo, "skipped", "remote repository exists")
	}

	// Step 3: update the config entry (verbatim origin URL — the scanner
	// reconciles state against on-disk origin, so the strings must match).
	if d.ConfigURLIsLocal {
		if existing, ok := m.config.FindRepoByURL(originURL); ok && existing.Name != d.RepoName {
			if err := m.config.RemoveRepo(d.RepoName); err != nil {
				return abort(ConnectStepUpdateConfig, err.Error(), ConnectStepLinkWorkspaces, ConnectStepInitialPush), nil
			}
			add(ConnectStepUpdateConfig, "done", fmt.Sprintf("merged into existing repo %q", existing.Name))
		} else if err := m.config.SetRepoURL(d.RepoName, originURL); err != nil {
			// Entry deleted by hand — recreate it. The name cannot collide:
			// SetRepoURL just failed because no entry has that name.
			if err := m.config.AddRepo(config.Repo{Name: d.RepoName, URL: originURL, BarePath: d.RepoName + ".git"}); err != nil {
				return abort(ConnectStepUpdateConfig, err.Error(), ConnectStepLinkWorkspaces, ConnectStepInitialPush), nil
			}
			add(ConnectStepUpdateConfig, "done", "recreated config entry")
		} else {
			add(ConnectStepUpdateConfig, "done", originURL)
		}
	} else {
		add(ConnectStepUpdateConfig, "skipped", "config already updated")
	}

	// Step 4: relink every workspace still pointing at the local repo.
	localURL := "local:" + d.RepoName
	linked := 0
	for _, ws := range m.state.GetWorkspaces() {
		if ws.Repo == localURL {
			ws.Repo = originURL
			if err := m.state.UpdateWorkspace(ws); err != nil {
				return abort(ConnectStepLinkWorkspaces, err.Error(), ConnectStepInitialPush), nil
			}
			linked++
		}
	}
	if linked > 0 {
		add(ConnectStepLinkWorkspaces, "done", fmt.Sprintf("%d workspace(s) linked", linked))
	} else {
		add(ConnectStepLinkWorkspaces, "skipped", "already linked")
	}

	// Step 5: push current history under the chosen default-branch name —
	// never under the schmux-generated local branch name. The first branch
	// pushed to an empty repo becomes GitHub's default branch.
	if !d.RemoteHasRefs {
		if !connectBranchPattern.MatchString(branch) {
			return abort(ConnectStepInitialPush, fmt.Sprintf("invalid default branch name %q", branch)), nil
		}
		if out, err := run("push", "origin", "HEAD:refs/heads/"+branch); err != nil {
			return abort(ConnectStepInitialPush, out), nil
		}
		add(ConnectStepInitialPush, "done", "pushed HEAD to "+branch)
	} else {
		add(ConnectStepInitialPush, "skipped", "remote already has refs")
	}

	res.Success = true
	res.RepoURL = originURL
	return res, nil
}
