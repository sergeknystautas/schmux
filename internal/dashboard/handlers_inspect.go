package dashboard

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/sergeknystautas/schmux/internal/vcs"
)

// runFunc executes a shell command string and returns trimmed output.
type runFunc func(cmd string) string

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

	cb := vcs.NewCommandBuilder(s.vcsTypeForWorkspace(ws))

	var run runFunc
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
		run = func(cmd string) string {
			out, err := conn.RunCommand(r.Context(), ws.RemotePath, cmd)
			if err != nil {
				return ""
			}
			return strings.TrimSpace(out)
		}
	} else {
		run = func(cmd string) string {
			parts := strings.Fields(cmd)
			if len(parts) == 0 {
				return ""
			}
			c := exec.CommandContext(r.Context(), parts[0], parts[1:]...)
			c.Dir = ws.Path
			out, _ := c.Output()
			return strings.TrimSpace(string(out))
		}
	}

	s.inspect(w, run, cb, &resp)
}

func (s *Server) inspect(w http.ResponseWriter, run runFunc, cb vcs.CommandBuilder, resp *inspectResponse) {
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
