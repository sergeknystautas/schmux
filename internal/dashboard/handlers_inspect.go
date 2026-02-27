package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/sergeknystautas/schmux/internal/vcs"
)

type inspectResponse struct {
	WorkspaceID  string   `json:"workspace_id"`
	Repo         string   `json:"repo"`
	Branch       string   `json:"branch"`
	Pushed       bool     `json:"pushed"`
	RemoteBranch string   `json:"remote_branch,omitempty"`
	AheadMain    int      `json:"ahead_main"`
	BehindMain   int      `json:"behind_main"`
	Commits      []string `json:"commits"`
	Uncommitted  []string `json:"uncommitted"`
}

func (s *Server) handleInspectWorkspace(w http.ResponseWriter, r *http.Request) {
	ws, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}

	var resp inspectResponse
	resp.WorkspaceID = ws.ID

	// Get repo name from config
	if repo, found := s.config.FindRepoByURL(ws.Repo); found {
		resp.Repo = repo.Name
	} else {
		resp.Repo = ws.Repo
	}

	// Determine VCS type
	vcsType := "git"
	if ws.RemoteHostID != "" {
		if host, found := s.state.GetRemoteHost(ws.RemoteHostID); found {
			if host.FlavorID != "" {
				if flavor, found := s.config.GetRemoteFlavor(host.FlavorID); found && flavor.VCS != "" {
					vcsType = flavor.VCS
				}
			}
		}
	}
	cb := vcs.NewCommandBuilder(vcsType)

	if ws.RemoteHostID != "" {
		if s.remoteManager == nil {
			writeJSONError(w, "remote manager not available", http.StatusServiceUnavailable)
			return
		}
		conn := s.remoteManager.GetConnection(ws.RemoteHostID)
		if conn == nil {
			writeJSONError(w, "remote host not connected", http.StatusServiceUnavailable)
			return
		}
		s.inspectRemote(r.Context(), w, conn, ws.RemotePath, cb, &resp)
	} else {
		s.inspectLocal(r.Context(), w, ws.Path, cb, &resp)
	}
}

func (s *Server) inspectLocal(ctx context.Context, w http.ResponseWriter, workdir string, cb vcs.CommandBuilder, resp *inspectResponse) {
	run := func(cmd string) string {
		parts := strings.Fields(cmd)
		if len(parts) == 0 {
			return ""
		}
		c := exec.CommandContext(ctx, parts[0], parts[1:]...)
		c.Dir = workdir
		out, _ := c.Output()
		return strings.TrimSpace(string(out))
	}

	resp.Branch = run(cb.CurrentBranch())

	// Detect default branch
	defaultBranch := run(cb.DetectDefaultBranch())
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	// Ahead/behind main
	defaultRef := cb.DefaultBranchRef(defaultBranch)
	aheadStr := run(cb.RevListCount(defaultRef + "..HEAD"))
	behindStr := run(cb.RevListCount("HEAD.." + defaultRef))
	fmt.Sscanf(aheadStr, "%d", &resp.AheadMain)
	fmt.Sscanf(behindStr, "%d", &resp.BehindMain)

	// Check if branch is pushed
	remoteCheck := run(cb.RemoteBranchExists(resp.Branch))
	resp.Pushed = remoteCheck != ""
	if resp.Pushed {
		resp.RemoteBranch = cb.DefaultBranchRef(resp.Branch)
	}

	// Commit log (ahead of main) — use simple oneline format
	c := exec.CommandContext(ctx, "git", "log", "--oneline", defaultRef+"..HEAD")
	c.Dir = workdir
	out, _ := c.Output()
	logOutput := strings.TrimSpace(string(out))
	if logOutput != "" {
		resp.Commits = strings.Split(logOutput, "\n")
	} else {
		resp.Commits = []string{}
	}

	// Uncommitted changes
	statusOutput := run(cb.StatusPorcelain())
	if statusOutput != "" {
		resp.Uncommitted = strings.Split(statusOutput, "\n")
	} else {
		resp.Uncommitted = []string{}
	}

	writeJSON(w, resp)
}

func (s *Server) inspectRemote(ctx context.Context, w http.ResponseWriter, conn interface {
	RunCommand(context.Context, string, string) (string, error)
}, workdir string, cb vcs.CommandBuilder, resp *inspectResponse) {
	run := func(cmd string) string {
		out, err := conn.RunCommand(ctx, workdir, cmd)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}

	resp.Branch = run(cb.CurrentBranch())

	defaultBranch := run(cb.DetectDefaultBranch())
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	defaultRef := cb.DefaultBranchRef(defaultBranch)
	aheadStr := run(cb.RevListCount(defaultRef + "..HEAD"))
	behindStr := run(cb.RevListCount("HEAD.." + defaultRef))
	fmt.Sscanf(aheadStr, "%d", &resp.AheadMain)
	fmt.Sscanf(behindStr, "%d", &resp.BehindMain)

	remoteCheck := run(cb.RemoteBranchExists(resp.Branch))
	resp.Pushed = remoteCheck != ""
	if resp.Pushed {
		resp.RemoteBranch = cb.DefaultBranchRef(resp.Branch)
	}

	// Commit log — use simple oneline for remote
	logOutput := run(fmt.Sprintf("git log --oneline %s..HEAD", defaultRef))
	if logOutput != "" {
		resp.Commits = strings.Split(logOutput, "\n")
	} else {
		resp.Commits = []string{}
	}

	statusOutput := run(cb.StatusPorcelain())
	if statusOutput != "" {
		resp.Uncommitted = strings.Split(statusOutput, "\n")
	} else {
		resp.Uncommitted = []string{}
	}

	writeJSON(w, resp)
}
