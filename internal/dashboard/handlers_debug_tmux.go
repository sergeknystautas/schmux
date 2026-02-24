package dashboard

import (
	"context"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// handleDebugTmuxLeak returns simple tmux counts for dev-mode sidebar diagnostics.
// Shape is intentionally small/stable for UI display: sessions / attach / tmux.
func (s *Server) handleDebugTmuxLeak(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessions, sessionsErr := collectTmuxSessionCount()
	attachCount, tmuxCount, psErr := collectTmuxProcessCounts()

	resp := map[string]any{
		"tmux_sessions": map[string]any{
			"count": sessions,
		},
		"os_processes": map[string]any{
			"attach_session_process_count": attachCount,
			"tmux_process_count":           tmuxCount,
		},
	}
	if sessionsErr != nil {
		resp["tmux_sessions_error"] = sessionsErr.Error()
	}
	if psErr != nil {
		resp["ps_error"] = psErr.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, resp)
}

func collectTmuxSessionCount() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return 0, err
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return 0, nil
	}
	return len(strings.Split(trimmed, "\n")), nil
}

func collectTmuxProcessCounts() (attachCount int, tmuxCount int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "ps", "-axo", "args=").Output()
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		cmd := strings.TrimSpace(line)
		if cmd == "" || !strings.Contains(cmd, "tmux") {
			continue
		}
		tmuxCount++
		if strings.Contains(cmd, "attach-session") {
			attachCount++
		}
	}
	return attachCount, tmuxCount, nil
}
