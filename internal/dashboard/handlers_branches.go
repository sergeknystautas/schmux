package dashboard

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/sergeknystautas/schmux/internal/vcs"
)

type branchEntry struct {
	WorkspaceID   string   `json:"workspace_id"`
	Repo          string   `json:"repo"`
	Branch        string   `json:"branch"`
	AheadMain     int      `json:"ahead_main"`
	BehindMain    int      `json:"behind_main"`
	Pushed        bool     `json:"pushed"`
	Dirty         bool     `json:"dirty"`
	SessionCount  int      `json:"session_count"`
	SessionStates []string `json:"session_states"`
	Error         string   `json:"error,omitempty"`
}

func (h *SpawnHandlers) handleGetBranches(w http.ResponseWriter, r *http.Request) {
	workspaces := h.state.GetWorkspaces()
	allSessions := h.state.GetSessions()
	var entries []branchEntry

	for _, ws := range workspaces {
		entry := branchEntry{
			WorkspaceID: ws.ID,
		}

		// Get repo name from config
		if repo, found := h.repos.GetByURL(ws.Repo); found {
			entry.Repo = repo.Name
		} else {
			entry.Repo = ws.Repo
		}

		// Get session states for this workspace
		for _, sess := range allSessions {
			if sess.WorkspaceID == ws.ID {
				entry.SessionCount++
				// Derive state label from nudge or status
				stateLabel := "running"
				if sess.Nudge != "" {
					stateLabel = sess.Nudge
				}
				if sess.Status != "" {
					stateLabel = sess.Status
				}
				entry.SessionStates = append(entry.SessionStates, stateLabel)
			}
		}

		cb := vcs.NewCommandBuilder(h.vcsTypeForWorkspace(ws))

		run := func(cmd string) string {
			parts := strings.Fields(cmd)
			if len(parts) == 0 {
				return ""
			}
			c := exec.CommandContext(r.Context(), parts[0], parts[1:]...)
			c.Dir = ws.Path
			out, _ := c.Output()
			return strings.TrimSpace(string(out))
		}
		populateBranchEntry(run, cb, &entry)

		entries = append(entries, entry)
	}

	if entries == nil {
		entries = []branchEntry{}
	}
	writeJSON(w, entries)
}

func populateBranchEntry(run runFunc, cb vcs.CommandBuilder, entry *branchEntry) {
	entry.Branch = run(cb.CurrentBranch())

	defaultBranch := run(cb.DetectDefaultBranch())
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	defaultRef := cb.DefaultBranchRef(defaultBranch)

	aheadStr := run(cb.RevListCount(defaultRef + "..HEAD"))
	behindStr := run(cb.RevListCount("HEAD.." + defaultRef))
	fmt.Sscanf(aheadStr, "%d", &entry.AheadMain)
	fmt.Sscanf(behindStr, "%d", &entry.BehindMain)

	remoteCheck := run(cb.RemoteBranchExists(entry.Branch))
	entry.Pushed = remoteCheck != ""

	statusOutput := run(cb.StatusPorcelain())
	entry.Dirty = statusOutput != ""
}
