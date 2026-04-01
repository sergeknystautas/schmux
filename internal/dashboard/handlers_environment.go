package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/tmux"
)

type envSyncRequest struct {
	Key string `json:"key"`
}

var blockedKeys = map[string]bool{
	// tmux-internal
	"TMUX": true, "TMUX_PANE": true,
	// session-transient
	"SHLVL": true, "PWD": true, "OLDPWD": true, "_": true, "COLUMNS": true, "LINES": true, "TMPDIR": true,
	// terminal-specific
	"TERM_SESSION_ID": true, "ITERM_SESSION_ID": true, "WINDOWID": true, "STY": true, "WINDOW": true,
	"COLORTERM": true, "COLOR": true, "TERM_PROGRAM": true, "TERM_PROGRAM_VERSION": true, "TERMINFO": true,
	// macOS session/system
	"LaunchInstanceID": true, "SECURITYSESSIONID": true, "XPC_FLAGS": true, "XPC_SERVICE_NAME": true,
	"__CFBundleIdentifier": true, "__CF_USER_TEXT_ENCODING": true, "OSLogRateLimit": true, "COMMAND_MODE": true,
	// npm pollution
	"INIT_CWD": true, "NODE": true,
	// session-specific
	"SSH_AUTH_SOCK": true, "EDITOR": true,
}

var blockedPrefixes = []string{"GHOSTTY_", "ITERM_", "npm_"}

func isBlocked(key string) bool {
	if blockedKeys[key] {
		return true
	}
	for _, prefix := range blockedPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func blockedKeysList() []string {
	keys := make([]string, 0, len(blockedKeys)+len(blockedPrefixes))
	for k := range blockedKeys {
		keys = append(keys, k)
	}
	for _, p := range blockedPrefixes {
		keys = append(keys, p+"*")
	}
	sort.Strings(keys)
	return keys
}

func compareEnvironments(system, tmuxEnv map[string]string) []contracts.EnvironmentVar {
	allKeys := make(map[string]bool)
	for k := range system {
		allKeys[k] = true
	}
	for k := range tmuxEnv {
		allKeys[k] = true
	}

	var vars []contracts.EnvironmentVar
	for k := range allKeys {
		if isBlocked(k) {
			continue
		}
		sysVal, inSys := system[k]
		tmuxVal, inTmux := tmuxEnv[k]

		var status string
		switch {
		case inSys && inTmux && sysVal == tmuxVal:
			status = "in_sync"
		case inSys && inTmux:
			status = "differs"
		case inSys:
			status = "system_only"
		default:
			status = "tmux_only"
		}
		vars = append(vars, contracts.EnvironmentVar{Key: k, Status: status})
	}

	sort.Slice(vars, func(i, j int) bool {
		return vars[i].Key < vars[j].Key
	})
	return vars
}

func getSystemEnvironment(ctx context.Context) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	home := os.Getenv("HOME")
	user := os.Getenv("USER")
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}

	cmd := exec.CommandContext(ctx, "env", "-i",
		"HOME="+home,
		"USER="+user,
		"SHELL="+shell,
		"TERM=xterm-256color",
		shell, "-l", "-i", "-c", "env",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("login shell timed out after 10s (check %s profile for blocking commands)", shell)
		}
		return nil, fmt.Errorf("failed to spawn login shell (%s): %w", shell, err)
	}

	env := make(map[string]string)
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.IndexByte(line, '='); idx >= 0 {
			env[line[:idx]] = line[idx+1:]
		}
	}
	return env, nil
}

func updateBaseline(system map[string]string) {
	keys := make(map[string]bool, len(system))
	for k := range system {
		keys[k] = true
	}
	tmux.SetBaseline(keys)
}

func (s *Server) handleGetEnvironment(w http.ResponseWriter, r *http.Request) {
	system, err := getSystemEnvironment(r.Context())
	if err != nil {
		s.logger.Error("failed to get system environment", "err", err)
		http.Error(w, "Failed to read system environment", http.StatusInternalServerError)
		return
	}

	updateBaseline(system)

	tmuxEnv, err := tmux.ShowEnvironment(r.Context())
	if err != nil {
		s.logger.Error("failed to get tmux environment", "err", err)
		http.Error(w, "Failed to read tmux environment", http.StatusInternalServerError)
		return
	}

	vars := compareEnvironments(system, tmuxEnv)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contracts.EnvironmentResponse{
		Vars:    vars,
		Blocked: blockedKeysList(),
	})
}

func (s *Server) handleSyncEnvironment(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	var req envSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Key == "" {
		http.Error(w, "key is required", http.StatusBadRequest)
		return
	}

	if isBlocked(req.Key) {
		http.Error(w, "Cannot sync blocked key", http.StatusBadRequest)
		return
	}

	system, err := getSystemEnvironment(r.Context())
	if err != nil {
		s.logger.Error("failed to get system environment for sync", "err", err)
		http.Error(w, "Failed to read system environment", http.StatusInternalServerError)
		return
	}

	value, ok := system[req.Key]
	if !ok {
		http.Error(w, "Key not found in system environment (tmux-only keys cannot be synced)", http.StatusBadRequest)
		return
	}

	if err := tmux.SetEnvironment(r.Context(), req.Key, value); err != nil {
		s.logger.Error("failed to set tmux environment", "key", req.Key, "err", err)
		http.Error(w, "Failed to set tmux environment variable", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
