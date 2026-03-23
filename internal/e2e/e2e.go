// Package e2e provides end-to-end testing infrastructure for schmux.
// Tests run in Docker containers for full isolation.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/config"
)

const (
	// DaemonStartupTimeout is how long to wait for daemon to start
	DaemonStartupTimeout = 10 * time.Second
)

// APISession represents a session from the API response.
type APISession struct {
	ID           string `json:"id"`
	Target       string `json:"target"`
	Branch       string `json:"branch"`
	Nickname     string `json:"nickname,omitempty"`
	CreatedAt    string `json:"created_at"`
	LastOutputAt string `json:"last_output_at,omitempty"`
	Running      bool   `json:"running"`
	AttachCmd    string `json:"attach_cmd"`
	NudgeState   string `json:"nudge_state,omitempty"`
	NudgeSummary string `json:"nudge_summary,omitempty"`
	NudgeSeq     uint64 `json:"nudge_seq,omitempty"`
}

// APIWorkspace represents a workspace from the API response.
type APIWorkspace struct {
	ID           string       `json:"id"`
	Repo         string       `json:"repo"`
	Branch       string       `json:"branch"`
	Path         string       `json:"path"`
	SessionCount int          `json:"session_count"`
	Sessions     []APISession `json:"sessions"`
	QuickLaunch  []string     `json:"quick_launch,omitempty"`
	Ahead        int          `json:"ahead"`
	Behind       int          `json:"behind"`
	LinesAdded   int          `json:"lines_added"`
	LinesRemoved int          `json:"lines_removed"`
	FilesChanged int          `json:"files_changed"`
}

// Env is the E2E test environment.
// Each test gets its own isolated HOME directory and ephemeral daemon port
// so tests can run concurrently via t.Parallel().
type Env struct {
	daemonLogFile *os.File // daemon stderr log file, closed in Cleanup
	T             *testing.T
	SchmuxBin     string
	DaemonURL     string
	HomeDir       string // isolated temp HOME for this test
	daemonPort    int    // ephemeral port for this test's daemon
	daemonStarted bool
	gitRepoDir    string // temp local git repo for testing
}

// New creates a new E2E test environment.
// Each call allocates an ephemeral port and isolated HOME directory.
func New(t *testing.T) *Env {
	t.Helper()

	// Find schmux binary - it should be built and in PATH
	schmuxBin, err := exec.LookPath("schmux")
	if err != nil {
		t.Skipf("schmux binary not found in PATH (run `go build ./cmd/schmux` first)")
	}

	// Allocate ephemeral port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to allocate ephemeral port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Create isolated HOME directory
	homeDir, err := os.MkdirTemp("", "schmux-e2e-home-")
	if err != nil {
		t.Fatalf("Failed to create temp home dir: %v", err)
	}
	// Create .schmux subdir
	if err := os.MkdirAll(filepath.Join(homeDir, ".schmux"), 0755); err != nil {
		t.Fatalf("Failed to create .schmux dir: %v", err)
	}

	e := &Env{
		T:          t,
		SchmuxBin:  schmuxBin,
		DaemonURL:  fmt.Sprintf("http://127.0.0.1:%d", port),
		HomeDir:    homeDir,
		daemonPort: port,
	}

	t.Cleanup(e.Cleanup)
	return e
}

// DaemonPort returns the ephemeral port allocated for this test's daemon.
func (e *Env) DaemonPort() int {
	return e.daemonPort
}

// Nickname returns a test-unique nickname by prefixing the base name with
// a short identifier derived from the daemon port. This prevents tmux
// session name collisions when tests run in parallel.
func (e *Env) Nickname(base string) string {
	return fmt.Sprintf("p%d-%s", e.daemonPort, base)
}

// Cleanup cleans up the test environment.
func (e *Env) Cleanup() {
	if e.daemonStarted {
		e.T.Log("Stopping daemon...")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		cmd := exec.CommandContext(ctx, e.SchmuxBin, "stop")
		cmd.Env = append(os.Environ(), "HOME="+e.HomeDir, "TMUX_TMPDIR="+e.HomeDir)
		out, _ := cmd.CombinedOutput()
		cancel()
		e.T.Logf("stop output: %s", out)

		// Poll healthz until connection refused (daemon fully stopped)
		for i := 0; i < 150; i++ {
			hctx, hcancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			hreq, _ := http.NewRequestWithContext(hctx, http.MethodGet, e.DaemonURL+"/api/healthz", nil)
			_, herr := http.DefaultClient.Do(hreq)
			hcancel()
			if herr != nil {
				break // connection refused — daemon is down
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	if e.daemonLogFile != nil {
		e.daemonLogFile.Close()
		e.daemonLogFile = nil
	}

	if e.gitRepoDir != "" {
		os.RemoveAll(e.gitRepoDir)
	}

	// Clean up isolated HOME
	if e.HomeDir != "" {
		os.RemoveAll(e.HomeDir)
	}
}

// DaemonStart starts the schmux daemon.
func (e *Env) DaemonStart() {
	e.T.Helper()
	e.T.Log("Starting daemon...")

	// Start daemon in foreground mode in a goroutine to capture stderr
	cmd := exec.Command(e.SchmuxBin, "daemon-run")

	// Set HOME to isolated dir so daemon uses its own ~/.schmux/
	// Set TMUX_TMPDIR to isolated dir so each test's daemon gets its own tmux
	// server socket, preventing session name collisions between parallel tests.
	cmd.Env = append(os.Environ(), "HOME="+e.HomeDir, "TMUX_TMPDIR="+e.HomeDir)

	// Capture stderr to a log file for debugging
	logFile := filepath.Join(e.HomeDir, ".schmux", "e2e-daemon.log")
	os.MkdirAll(filepath.Dir(logFile), 0755)
	stderr, err := os.Create(logFile)
	if err != nil {
		e.T.Fatalf("Failed to create daemon log file: %v", err)
	}
	cmd.Stderr = stderr
	cmd.Stdout = stderr // Capture stdout too

	if err := cmd.Start(); err != nil {
		stderr.Close()
		e.T.Fatalf("Failed to start daemon: %v", err)
	}

	// Store the log file handle so Cleanup() can close it
	e.daemonLogFile = stderr

	// Wait for daemon to be ready
	e.T.Log("Waiting for daemon to be ready...")
	deadline := time.Now().Add(DaemonStartupTimeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.DaemonURL+"/api/healthz", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			e.T.Log("Daemon is ready")
			e.daemonStarted = true
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	e.T.Fatalf("Daemon failed to become ready within %v", DaemonStartupTimeout)
}

// DaemonStop stops the schmux daemon.
func (e *Env) DaemonStop() {
	e.T.Helper()
	e.T.Log("Stopping daemon...")

	// Use a generous timeout — under CPU contention (parallel Docker
	// containers) the stop command can take much longer than expected.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	cmd := exec.CommandContext(ctx, e.SchmuxBin, "stop")
	cmd.Env = append(os.Environ(), "HOME="+e.HomeDir, "TMUX_TMPDIR="+e.HomeDir)
	out, err := cmd.CombinedOutput()
	cancel()
	if err != nil {
		e.T.Logf("Warning: stop command failed: %v\nOutput: %s", err, out)
	}

	e.daemonStarted = false

	// Poll healthz until connection refused (daemon fully stopped).
	// Allow up to 15s — contention can delay shutdown significantly.
	stopped := false
	for i := 0; i < 150; i++ {
		ctx, cancel = context.WithTimeout(context.Background(), 500*time.Millisecond)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.DaemonURL+"/api/healthz", nil)
		_, err = http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			stopped = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !stopped {
		// Log rather than Error: cleanup failures in deferred code should
		// not mask actual test results. The container will clean up the
		// daemon process on exit regardless.
		e.T.Log("Warning: daemon is still running after stop")
	}
}

// CreateLocalGitRepo creates a local git repo for testing.
// Returns the actual file path to the repo (can be used as workspace path).
func (e *Env) CreateLocalGitRepo(name string) string {
	e.T.Helper()
	e.T.Logf("Creating local git repo: %s", name)

	dir, err := os.MkdirTemp("", "schmux-e2e-repo-")
	if err != nil {
		e.T.Fatalf("Failed to create temp dir: %v", err)
	}

	repoPath := filepath.Join(dir, name)
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		e.T.Fatalf("Failed to create repo dir: %v", err)
	}

	// Initialize git repo on main to match test branch usage.
	RunCmd(e.T, repoPath, "git", "init", "-b", "main")
	RunCmd(e.T, repoPath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(e.T, repoPath, "git", "config", "user.name", "E2E Test")

	// Create a test file
	testFile := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test Repo\n"), 0644); err != nil {
		e.T.Fatalf("Failed to create test file: %v", err)
	}

	// Commit
	RunCmd(e.T, repoPath, "git", "add", ".")
	RunCmd(e.T, repoPath, "git", "commit", "-m", "Initial commit")

	e.gitRepoDir = dir
	e.T.Logf("Created git repo at: %s", repoPath)
	return repoPath
}

// CreateConfig creates a minimal config file for E2E testing.
// Includes a test repo and a dummy run target.
func (e *Env) CreateConfig(workspacePath string) {
	e.T.Helper()
	e.T.Log("Creating config...")

	schmuxDir := filepath.Join(e.HomeDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		e.T.Fatalf("Failed to create .schmux dir: %v", err)
	}

	// Clear state file to prevent stale remote hosts from leaking between tests
	statePath := filepath.Join(schmuxDir, "state.json")
	os.Remove(statePath) // Ignore error if file doesn't exist

	configPath := filepath.Join(schmuxDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = workspacePath
	cfg.Network = &config.NetworkConfig{Port: e.daemonPort}
	// E2E sessions run trivial commands (echo/sleep/cat) — use a short grace
	// period so dispose-all doesn't block 30s per session waiting for SIGKILL.
	cfg.Sessions = &config.SessionsConfig{DisposeGracePeriodMs: 500}
	cfg.RunTargets = []config.RunTarget{
		// Keep the session alive long enough for pipe-pane and tmux assertions.
		{Name: "echo", Command: "sh -c 'echo hello; sleep 600'"},
		// Echoes input back for websocket output tests (emits START first for reliable bootstrap).
		{Name: "cat", Command: "sh -c 'echo START; exec cat'"},
	}

	if err := cfg.Save(); err != nil {
		e.T.Fatalf("Failed to save config: %v", err)
	}
}

// WSOutputMessage represents a WebSocket message to the client (for terminal).
type WSOutputMessage struct {
	Type    string `json:"type"` // "full", "append", "reconnect"
	Content string `json:"content"`
}

// DashboardMessage represents a WebSocket message from /ws/dashboard.
type DashboardMessage struct {
	Type       string         `json:"type"` // "sessions", "config"
	Workspaces []APIWorkspace `json:"workspaces,omitempty"`
}

// ConnectTerminalWebSocket connects to the terminal websocket for a session.
func (e *Env) ConnectTerminalWebSocket(sessionID string) (*websocket.Conn, error) {
	base, err := url.Parse(e.DaemonURL)
	if err != nil {
		return nil, err
	}
	wsURL := url.URL{
		Scheme: "ws",
		Host:   base.Host,
		Path:   "/ws/terminal/" + sessionID,
	}

	header := http.Header{}
	header.Set("Origin", e.DaemonURL)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL.String(), header)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// ConnectDashboardWebSocket connects to the dashboard websocket.
func (e *Env) ConnectDashboardWebSocket() (*websocket.Conn, error) {
	base, err := url.Parse(e.DaemonURL)
	if err != nil {
		return nil, err
	}
	wsURL := url.URL{
		Scheme: "ws",
		Host:   base.Host,
		Path:   "/ws/dashboard",
	}

	header := http.Header{}
	header.Set("Origin", e.DaemonURL)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL.String(), header)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// ReadDashboardMessage reads a single message from the dashboard websocket.
func (e *Env) ReadDashboardMessage(conn *websocket.Conn, timeout time.Duration) (*DashboardMessage, error) {
	conn.SetReadDeadline(time.Now().Add(timeout))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	var msg DashboardMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal dashboard message: %w", err)
	}
	return &msg, nil
}

// WaitForDashboardSession waits for a session to appear in dashboard websocket messages.
func (e *Env) WaitForDashboardSession(conn *websocket.Conn, sessionID string, timeout time.Duration) (*DashboardMessage, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		msg, err := e.ReadDashboardMessage(conn, time.Until(deadline))
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				return nil, fmt.Errorf("timed out waiting for session %s", sessionID)
			}
			return nil, err
		}
		if msg.Type == "sessions" {
			for _, ws := range msg.Workspaces {
				for _, sess := range ws.Sessions {
					if sess.ID == sessionID {
						return msg, nil
					}
				}
			}
		}
	}
	return nil, fmt.Errorf("timed out waiting for session %s", sessionID)
}

// WaitForDashboardSessionGone waits for a session to disappear from dashboard websocket messages.
func (e *Env) WaitForDashboardSessionGone(conn *websocket.Conn, sessionID string, timeout time.Duration) (*DashboardMessage, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		msg, err := e.ReadDashboardMessage(conn, time.Until(deadline))
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				return nil, fmt.Errorf("timed out waiting for session %s to be gone", sessionID)
			}
			return nil, err
		}
		if msg.Type == "sessions" {
			found := false
			for _, ws := range msg.Workspaces {
				for _, sess := range ws.Sessions {
					if sess.ID == sessionID {
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			if !found {
				return msg, nil
			}
		}
	}
	return nil, fmt.Errorf("timed out waiting for session %s to be gone", sessionID)
}

// WaitForSessionNudgeState waits for a session to have the expected nudge state
// in dashboard WebSocket messages. Pass empty string to wait for nudge to be cleared.
func (e *Env) WaitForSessionNudgeState(conn *websocket.Conn, sessionID, expectedNudgeState string, timeout time.Duration) (*APISession, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		msg, err := e.ReadDashboardMessage(conn, time.Until(deadline))
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				return nil, fmt.Errorf("timed out waiting for nudge state %q on session %s", expectedNudgeState, sessionID)
			}
			return nil, err
		}
		if msg.Type == "sessions" {
			for _, ws := range msg.Workspaces {
				for _, sess := range ws.Sessions {
					if sess.ID == sessionID && sess.NudgeState == expectedNudgeState {
						return &sess, nil
					}
				}
			}
		}
	}
	return nil, fmt.Errorf("timed out waiting for nudge state %q on session %s", expectedNudgeState, sessionID)
}

// WaitForWebSocketContent reads websocket output until it finds the substring or times out.
// Handles both binary frames (raw terminal bytes) and JSON text frames.
func (e *Env) WaitForWebSocketContent(conn *websocket.Conn, substr string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var buffer strings.Builder
	msgCount := 0

	for time.Now().Before(deadline) {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return buffer.String(), err
		}
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				e.T.Logf("WaitForWebSocketContent timeout waiting for %q, received %d messages, buffer: %q", substr, msgCount, buffer.String())
				return buffer.String(), fmt.Errorf("timed out waiting for websocket output: %q", substr)
			}
			return buffer.String(), err
		}
		msgCount++

		if msgType == websocket.BinaryMessage {
			// Binary frame: raw terminal bytes
			content := string(data)
			e.T.Logf("WaitForWebSocketContent received binary message %d: %q", msgCount, content)
			buffer.WriteString(content)
		} else {
			// Text frame: JSON message (legacy or control)
			var msg WSOutputMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				e.T.Logf("WaitForWebSocketContent received non-JSON text message %d: %s", msgCount, string(data))
				continue
			}
			e.T.Logf("WaitForWebSocketContent received JSON message %d: type=%q content=%q", msgCount, msg.Type, msg.Content)
			if msg.Content != "" {
				buffer.WriteString(msg.Content)
			}
		}

		if strings.Contains(buffer.String(), substr) {
			return buffer.String(), nil
		}
	}

	return buffer.String(), fmt.Errorf("timed out waiting for websocket output: %q", substr)
}

// SendWebSocketInput sends input to a session via the WebSocket "input" message type.
// This is used for remote sessions which don't have local tmux sessions.
func (e *Env) SendWebSocketInput(conn *websocket.Conn, data string) {
	e.T.Helper()
	msg := struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}{
		Type: "input",
		Data: data,
	}
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		e.T.Fatalf("Failed to marshal WebSocket input message: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
		e.T.Fatalf("Failed to send WebSocket input: %v", err)
	}
}

// SendKeysToTmux sends literal keys plus Enter to a tmux session.
func (e *Env) SendKeysToTmux(sessionName, text string) {
	e.T.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	cmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", sessionName, "-l", text)
	cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+e.HomeDir)
	out, err := cmd.CombinedOutput()
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to send keys to tmux: %v\nOutput: %s", err, out)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	cmd = exec.CommandContext(ctx, "tmux", "send-keys", "-t", sessionName, "C-m")
	cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+e.HomeDir)
	out, err = cmd.CombinedOutput()
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to send Enter to tmux: %v\nOutput: %s", err, out)
	}
}

// SetSaplingCommands sets the sapling template commands in the config.
func (e *Env) SetSaplingCommands(cmds config.SaplingCommands) {
	e.T.Helper()
	configPath := filepath.Join(e.HomeDir, ".schmux", "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		e.T.Fatalf("Failed to load config: %v", err)
	}
	cfg.SaplingCommands = cmds
	if err := cfg.Save(); err != nil {
		e.T.Fatalf("Failed to save config: %v", err)
	}
}

// AddSaplingRepoToConfig adds a sapling repo to the config file.
func (e *Env) AddSaplingRepoToConfig(name, url string) {
	e.T.Helper()
	e.T.Logf("Adding sapling repo to config: %s -> %s", name, url)

	configPath := filepath.Join(e.HomeDir, ".schmux", "config.json")

	cfg, err := config.Load(configPath)
	if err != nil {
		e.T.Fatalf("Failed to load config: %v", err)
	}

	cfg.Repos = append(cfg.Repos, config.Repo{Name: name, URL: url, BarePath: name + ".sl", VCS: "sapling"})
	if err := cfg.Save(); err != nil {
		e.T.Fatalf("Failed to save config: %v", err)
	}
}

// AddRepoToConfig adds a repo to the config file.
func (e *Env) AddRepoToConfig(name, url string) {
	e.T.Helper()
	e.T.Logf("Adding repo to config: %s -> %s", name, url)

	configPath := filepath.Join(e.HomeDir, ".schmux", "config.json")

	cfg, err := config.Load(configPath)
	if err != nil {
		e.T.Fatalf("Failed to load config: %v", err)
	}

	cfg.Repos = append(cfg.Repos, config.Repo{Name: name, URL: url, BarePath: name + ".git"})
	if err := cfg.Save(); err != nil {
		e.T.Fatalf("Failed to save config: %v", err)
	}
}

// SpawnSession spawns a new session via the daemon API directly.
// repoURL should be a repo URL (contract pre-2093ccf).
// Returns the session ID from the API response (or empty if spawn failed).
func (e *Env) SpawnSession(repoURL, branch, target, prompt, nickname string) string {
	e.T.Helper()
	e.T.Logf("Spawning session via API: repo=%s branch=%s target=%s nickname=%s", repoURL, branch, target, nickname)

	// Spawn via API using repo/branch
	type SpawnRequest struct {
		Repo     string         `json:"repo"`
		Branch   string         `json:"branch"`
		Prompt   string         `json:"prompt"`
		Nickname string         `json:"nickname,omitempty"`
		Targets  map[string]int `json:"targets"`
	}

	spawnReqBody := SpawnRequest{
		Repo:     repoURL,
		Branch:   branch,
		Prompt:   prompt,
		Nickname: nickname,
		Targets:  map[string]int{target: 1},
	}

	reqBody, err := json.Marshal(spawnReqBody)
	if err != nil {
		e.T.Fatalf("Failed to marshal spawn request: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	spawnReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.DaemonURL+"/api/spawn", bytes.NewReader(reqBody))
	spawnReq.Header.Set("Content-Type", "application/json")
	spawnResp, err := http.DefaultClient.Do(spawnReq)
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to spawn: %v", err)
	}
	defer spawnResp.Body.Close()

	if spawnResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(spawnResp.Body)
		e.T.Fatalf("Spawn returned non-200: %d\nBody: %s", spawnResp.StatusCode, body)
	}

	// Parse response to get session ID
	type SpawnResult struct {
		SessionID   string `json:"session_id"`
		WorkspaceID string `json:"workspace_id"`
		Target      string `json:"target"`
		Error       string `json:"error,omitempty"`
	}

	var results []SpawnResult
	if err := json.NewDecoder(spawnResp.Body).Decode(&results); err != nil {
		e.T.Logf("Failed to decode spawn response: %v", err)
		return ""
	}

	if len(results) > 0 && results[0].Error != "" {
		e.T.Fatalf("Spawn failed: %s", results[0].Error)
	}

	if len(results) > 0 {
		return results[0].SessionID
	}

	return ""
}

// SpawnSessionInWorkspace spawns a session into an existing workspace via the daemon API.
// This uses the workspace_id field to target a specific workspace, avoiding branch conflict checks.
func (e *Env) SpawnSessionInWorkspace(workspaceID, target, prompt, nickname string) string {
	e.T.Helper()
	e.T.Logf("Spawning session via API into workspace: workspace_id=%s target=%s nickname=%s", workspaceID, target, nickname)

	type SpawnRequest struct {
		WorkspaceID string         `json:"workspace_id"`
		Prompt      string         `json:"prompt"`
		Nickname    string         `json:"nickname,omitempty"`
		Targets     map[string]int `json:"targets"`
	}

	spawnReqBody := SpawnRequest{
		WorkspaceID: workspaceID,
		Prompt:      prompt,
		Nickname:    nickname,
		Targets:     map[string]int{target: 1},
	}

	reqBody, err := json.Marshal(spawnReqBody)
	if err != nil {
		e.T.Fatalf("Failed to marshal spawn request: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	spawnReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.DaemonURL+"/api/spawn", bytes.NewReader(reqBody))
	spawnReq.Header.Set("Content-Type", "application/json")
	spawnResp, err := http.DefaultClient.Do(spawnReq)
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to spawn: %v", err)
	}
	defer spawnResp.Body.Close()

	if spawnResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(spawnResp.Body)
		e.T.Fatalf("Spawn returned non-200: %d\nBody: %s", spawnResp.StatusCode, body)
	}

	type SpawnResult struct {
		SessionID   string `json:"session_id"`
		WorkspaceID string `json:"workspace_id"`
		Error       string `json:"error,omitempty"`
	}

	var results []SpawnResult
	if err := json.NewDecoder(spawnResp.Body).Decode(&results); err != nil {
		e.T.Logf("Failed to decode spawn response: %v", err)
		return ""
	}

	if len(results) > 0 && results[0].Error != "" {
		e.T.Fatalf("Spawn failed: %s", results[0].Error)
	}

	if len(results) > 0 {
		return results[0].SessionID
	}

	return ""
}

// SpawnCommandSession spawns a session using the command field (SpawnCommand code path).
// This exercises a different code path than target-based spawning.
func (e *Env) SpawnCommandSession(repoURL, branch, command, nickname, workspaceID string) string {
	e.T.Helper()
	e.T.Logf("Spawning command session via API: command=%q workspace_id=%s nickname=%s", command, workspaceID, nickname)

	type SpawnRequest struct {
		Repo        string `json:"repo,omitempty"`
		Branch      string `json:"branch,omitempty"`
		Command     string `json:"command"`
		Nickname    string `json:"nickname,omitempty"`
		WorkspaceID string `json:"workspace_id,omitempty"`
	}

	spawnReqBody := SpawnRequest{
		Repo:        repoURL,
		Branch:      branch,
		Command:     command,
		Nickname:    nickname,
		WorkspaceID: workspaceID,
	}

	reqBody, err := json.Marshal(spawnReqBody)
	if err != nil {
		e.T.Fatalf("Failed to marshal spawn request: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	spawnReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.DaemonURL+"/api/spawn", bytes.NewReader(reqBody))
	spawnReq.Header.Set("Content-Type", "application/json")
	spawnResp, err := http.DefaultClient.Do(spawnReq)
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to spawn: %v", err)
	}
	defer spawnResp.Body.Close()

	if spawnResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(spawnResp.Body)
		e.T.Fatalf("Spawn returned non-200: %d\nBody: %s", spawnResp.StatusCode, body)
	}

	type SpawnResult struct {
		SessionID   string `json:"session_id"`
		WorkspaceID string `json:"workspace_id"`
		Error       string `json:"error,omitempty"`
	}

	var results []SpawnResult
	if err := json.NewDecoder(spawnResp.Body).Decode(&results); err != nil {
		e.T.Logf("Failed to decode spawn response: %v", err)
		return ""
	}

	if len(results) > 0 && results[0].Error != "" {
		e.T.Fatalf("Spawn failed: %s", results[0].Error)
	}

	if len(results) > 0 {
		return results[0].SessionID
	}

	return ""
}

// SpawnQuickLaunchWithoutWorkspace posts a quick_launch_name spawn request without a workspace_id.
// Returns the HTTP status code.
func (e *Env) SpawnQuickLaunchWithoutWorkspace(repoURL, branch, name string) int {
	e.T.Helper()
	type SpawnRequest struct {
		Repo            string `json:"repo"`
		Branch          string `json:"branch"`
		QuickLaunchName string `json:"quick_launch_name"`
	}
	spawnReqBody := SpawnRequest{
		Repo:            repoURL,
		Branch:          branch,
		QuickLaunchName: name,
	}
	reqBody, err := json.Marshal(spawnReqBody)
	if err != nil {
		e.T.Fatalf("Failed to marshal spawn request: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	spawnReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.DaemonURL+"/api/spawn", bytes.NewReader(reqBody))
	spawnReq.Header.Set("Content-Type", "application/json")
	spawnResp, err := http.DefaultClient.Do(spawnReq)
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to spawn: %v", err)
	}
	defer spawnResp.Body.Close()
	return spawnResp.StatusCode
}

// ListSessions lists sessions via the API.
// Returns a formatted string containing session nicknames and IDs for assertion.
func (e *Env) ListSessions() string {
	e.T.Helper()

	sessions := e.GetAPISessions()
	var sb strings.Builder
	for _, sess := range sessions {
		fmt.Fprintf(&sb, "%s (%s) target=%s running=%v\n", sess.Nickname, sess.ID, sess.Target, sess.Running)
	}

	return sb.String()
}

// DisposeSession disposes a session via the API.
func (e *Env) DisposeSession(sessionID string) {
	e.T.Helper()
	e.T.Logf("Disposing session: %s", sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.DaemonURL+"/api/sessions/"+sessionID+"/dispose", nil)
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to dispose session: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		e.T.Fatalf("Dispose returned non-200: %d\nBody: %s", resp.StatusCode, body)
	}
}

// GetTmuxSessions returns the list of tmux session names.
func (e *Env) GetTmuxSessions() []string {
	e.T.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	cmd := exec.CommandContext(ctx, "tmux", "ls")
	cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+e.HomeDir)
	out, err := cmd.CombinedOutput()
	cancel()
	if err != nil {
		// tmux ls returns error if no sessions - that's ok
		if strings.Contains(string(out), "no server running") {
			return []string{}
		}
		e.T.Fatalf("Failed to list tmux sessions: %v\nOutput: %s", err, out)
	}

	output := string(out)
	var sessions []string
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "session-name: (date)" - extract name
		parts := strings.SplitN(line, ":", 2)
		if len(parts) > 0 {
			sessions = append(sessions, parts[0])
		}
	}

	return sessions
}

// GetAPIWorkspaces returns the list of workspaces from the API.
func (e *Env) GetAPIWorkspaces() []APIWorkspace {
	e.T.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.DaemonURL+"/api/sessions", nil)
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to get workspaces from API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		e.T.Fatalf("API returned non-200 status: %d\nBody: %s", resp.StatusCode, body)
	}

	var workspaces []APIWorkspace
	if err := json.NewDecoder(resp.Body).Decode(&workspaces); err != nil {
		e.T.Fatalf("Failed to decode API response: %v", err)
	}

	return workspaces
}

// GetAPISessions returns the list of sessions from the API.
func (e *Env) GetAPISessions() []APISession {
	e.T.Helper()

	// Flatten sessions from all workspaces
	var allSessions []APISession
	for _, ws := range e.GetAPIWorkspaces() {
		allSessions = append(allSessions, ws.Sessions...)
	}

	return allSessions
}

// HealthCheck returns true if the daemon health endpoint responds.
func (e *Env) HealthCheck() bool {
	e.T.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.DaemonURL+"/api/healthz", nil)
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// CaptureArtifacts captures debug artifacts when a test fails.
func (e *Env) CaptureArtifacts() {
	e.T.Helper()

	failureDir := filepath.Join("testdata", "failures", e.T.Name())
	if err := os.MkdirAll(failureDir, 0755); err != nil {
		// Fall back to a temp directory if cwd isn't writable (e.g. Docker
		// container where the WORKDIR is owned by root).
		fallback := filepath.Join(os.TempDir(), "schmux-e2e-failures", e.T.Name())
		if err2 := os.MkdirAll(fallback, 0755); err2 != nil {
			e.T.Logf("Failed to create failure dir (primary: %v, fallback: %v)", err, err2)
			return
		}
		e.T.Logf("Using fallback failure dir (primary failed: %v)", err)
		failureDir = fallback
	}

	e.T.Logf("Capturing artifacts to: %s", failureDir)

	// Capture config.json and state.json
	schmuxDir := filepath.Join(e.HomeDir, ".schmux")
	configPath := filepath.Join(schmuxDir, "config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		os.WriteFile(filepath.Join(failureDir, "config.json"), data, 0644)
	}

	statePath := filepath.Join(schmuxDir, "state.json")
	if data, err := os.ReadFile(statePath); err == nil {
		os.WriteFile(filepath.Join(failureDir, "state.json"), data, 0644)
	}

	// Capture daemon log if it exists
	daemonLogPath := filepath.Join(schmuxDir, "e2e-daemon.log")
	if data, err := os.ReadFile(daemonLogPath); err == nil {
		os.WriteFile(filepath.Join(failureDir, "daemon.log"), data, 0644)
		// Also print to test output for immediate visibility
		e.T.Logf("=== DAEMON LOG ===\n%s", string(data))
	}

	// Capture tmux ls output
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	cmd := exec.CommandContext(ctx, "tmux", "ls")
	cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+e.HomeDir)
	out, _ := cmd.CombinedOutput()
	cancel()
	os.WriteFile(filepath.Join(failureDir, "tmux-ls.txt"), out, 0644)

	// Capture API responses
	if e.HealthCheck() {
		if sessions := e.GetAPISessions(); sessions != nil {
			data, _ := json.MarshalIndent(sessions, "", "  ")
			os.WriteFile(filepath.Join(failureDir, "api-sessions.json"), data, 0644)
		}
	}

	e.T.Logf("Artifacts captured to: %s", failureDir)
}

// SetSourceCodeManagement updates the config file to use the specified source code manager.
func (e *Env) SetSourceCodeManagement(scm string) {
	e.T.Helper()
	e.T.Logf("Setting source_code_management to: %s", scm)

	configPath := filepath.Join(e.HomeDir, ".schmux", "config.json")

	cfg, err := config.Load(configPath)
	if err != nil {
		e.T.Fatalf("Failed to load config: %v", err)
	}

	cfg.SourceCodeManagement = scm
	if err := cfg.Save(); err != nil {
		e.T.Fatalf("Failed to save config: %v", err)
	}
}

// BranchConflictResult is the result of a branch conflict check.
type BranchConflictResult struct {
	Conflict    bool   `json:"conflict"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// OverlayAPIResponse represents the GET /api/overlays response.
type OverlayAPIResponse struct {
	Overlays []OverlayAPIInfo `json:"overlays"`
}

type OverlayAPIInfo struct {
	RepoName       string            `json:"repo_name"`
	Path           string            `json:"path"`
	Exists         bool              `json:"exists"`
	FileCount      int               `json:"file_count"`
	DeclaredPaths  []OverlayPathInfo `json:"declared_paths"`
	NudgeDismissed bool              `json:"nudge_dismissed"`
}

type OverlayPathInfo struct {
	Path   string `json:"path"`
	Source string `json:"source"` // "builtin", "global", "repo"
	Status string `json:"status"` // "synced", "pending"
}

type OverlayScanCandidate struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Detected bool   `json:"detected"`
}

type OverlayAddResult struct {
	Success    bool     `json:"success"`
	Copied     []string `json:"copied"`
	Registered []string `json:"registered"`
}

// CheckBranchConflict calls the /api/check-branch-conflict endpoint.
func (e *Env) CheckBranchConflict(repo, branch string) BranchConflictResult {
	e.T.Helper()
	e.T.Logf("Checking branch conflict: repo=%s branch=%s", repo, branch)

	type CheckRequest struct {
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
	}

	reqBody, err := json.Marshal(CheckRequest{Repo: repo, Branch: branch})
	if err != nil {
		e.T.Fatalf("Failed to marshal request: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.DaemonURL+"/api/check-branch-conflict", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to check branch conflict: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		e.T.Fatalf("Branch conflict check returned non-200: %d\nBody: %s", resp.StatusCode, body)
	}

	var result BranchConflictResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		e.T.Fatalf("Failed to decode response: %v", err)
	}

	return result
}

// SetupTestRepo creates a git repo in the workspace root, initializes it with an
// initial commit, configures git user identity, and registers it in the schmux config.
// This extracts the common boilerplate found in most E2E tests.
// Returns the absolute path to the created repo directory.
func (e *Env) SetupTestRepo(workspaceRoot, name string) string {
	e.T.Helper()

	repoPath := workspaceRoot + "/" + name
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		e.T.Fatalf("Failed to create repo dir: %v", err)
	}

	RunCmd(e.T, repoPath, "git", "init", "-b", "main")
	RunCmd(e.T, repoPath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(e.T, repoPath, "git", "config", "user.name", "E2E Test")

	readmePath := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(readmePath, []byte("# "+name+"\n"), 0644); err != nil {
		e.T.Fatalf("Failed to create README: %v", err)
	}

	RunCmd(e.T, repoPath, "git", "add", ".")
	RunCmd(e.T, repoPath, "git", "commit", "-m", "Initial commit")

	e.AddRepoToConfig(name, "file://"+repoPath)

	return repoPath
}

// RunCmd runs a command in the given directory.
func RunCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	cancel()
	if err != nil {
		t.Fatalf("Command failed: %s %v\nStdout: %s\nStderr: %s", name, args, stdout.String(), stderr.String())
	}
}

// RemoteHostResponse represents a remote host from the API.
type RemoteHostResponse struct {
	ID          string `json:"id"`
	FlavorID    string `json:"flavor_id"`
	DisplayName string `json:"display_name,omitempty"`
	Hostname    string `json:"hostname"`
	Status      string `json:"status"`
	VCS         string `json:"vcs,omitempty"`
	ConnectedAt string `json:"connected_at,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// RemoteFlavorResponse represents a remote flavor from the API.
type RemoteFlavorResponse struct {
	ID            string `json:"id"`
	Flavor        string `json:"flavor"`
	DisplayName   string `json:"display_name"`
	VCS           string `json:"vcs"`
	WorkspacePath string `json:"workspace_path"`
}

// AddRemoteFlavorToConfig adds a remote flavor to the config file.
func (e *Env) AddRemoteFlavorToConfig(flavor, displayName, workspacePath, connectCommand string) string {
	e.T.Helper()
	e.T.Logf("Adding remote flavor to config: %s", displayName)

	configPath := filepath.Join(e.HomeDir, ".schmux", "config.json")

	cfg, err := config.Load(configPath)
	if err != nil {
		e.T.Fatalf("Failed to load config: %v", err)
	}

	rf := config.RemoteFlavor{
		Flavor:         flavor,
		DisplayName:    displayName,
		VCS:            "git",
		WorkspacePath:  workspacePath,
		ConnectCommand: connectCommand,
	}

	if err := cfg.AddRemoteFlavor(rf); err != nil {
		e.T.Fatalf("Failed to add remote flavor: %v", err)
	}

	if err := cfg.Save(); err != nil {
		e.T.Fatalf("Failed to save config: %v", err)
	}

	// Return the generated ID (config generates ID from flavor string)
	flavorID := config.GenerateRemoteFlavorID(flavor)
	e.T.Logf("Remote flavor added with ID: %s", flavorID)
	return flavorID
}

// GetRemoteHosts returns the list of remote hosts from the API.
func (e *Env) GetRemoteHosts() []RemoteHostResponse {
	e.T.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.DaemonURL+"/api/remote/hosts", nil)
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to get remote hosts from API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		e.T.Fatalf("API returned non-200 status: %d\nBody: %s", resp.StatusCode, body)
	}

	var hosts []RemoteHostResponse
	if err := json.NewDecoder(resp.Body).Decode(&hosts); err != nil {
		e.T.Fatalf("Failed to decode API response: %v", err)
	}

	return hosts
}

// SpawnRemoteSession spawns a session on a remote host via the daemon API.
// Returns the session ID from the API response. Retries up to 3 times on
// transient "control mode not ready" / "client closed" errors.
func (e *Env) SpawnRemoteSession(flavorID, target, prompt, nickname string) string {
	e.T.Helper()
	e.T.Logf("Spawning remote session via API: flavor=%s target=%s nickname=%s", flavorID, target, nickname)

	type SpawnRequest struct {
		RemoteFlavorID string         `json:"remote_flavor_id"`
		Prompt         string         `json:"prompt"`
		Nickname       string         `json:"nickname,omitempty"`
		Targets        map[string]int `json:"targets"`
	}

	spawnReqBody := SpawnRequest{
		RemoteFlavorID: flavorID,
		Prompt:         prompt,
		Nickname:       nickname,
		Targets:        map[string]int{target: 1},
	}

	reqBody, err := json.Marshal(spawnReqBody)
	if err != nil {
		e.T.Fatalf("Failed to marshal spawn request: %v", err)
	}

	// Parse response to get session ID
	type SpawnResult struct {
		SessionID   string `json:"session_id"`
		WorkspaceID string `json:"workspace_id"`
		Target      string `json:"target"`
		Status      string `json:"status"`
		Error       string `json:"error,omitempty"`
	}

	const maxRetries = 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		spawnReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.DaemonURL+"/api/spawn", bytes.NewReader(reqBody))
		spawnReq.Header.Set("Content-Type", "application/json")
		spawnResp, err := http.DefaultClient.Do(spawnReq)
		cancel()
		if err != nil {
			if attempt < maxRetries {
				e.T.Logf("Spawn attempt %d/%d failed (HTTP error): %v — retrying in 2s", attempt, maxRetries, err)
				time.Sleep(2 * time.Second)
				continue
			}
			daemonLogPath := filepath.Join(e.HomeDir, ".schmux", "e2e-daemon.log")
			if data, err2 := os.ReadFile(daemonLogPath); err2 == nil {
				e.T.Logf("=== DAEMON LOG (spawn failed) ===\n%s", string(data))
			}
			e.T.Fatalf("Failed to spawn remote session: %v", err)
		}

		body, _ := io.ReadAll(spawnResp.Body)
		spawnResp.Body.Close()

		if spawnResp.StatusCode != http.StatusOK {
			bodyStr := string(body)
			if attempt < maxRetries && (strings.Contains(bodyStr, "control mode not ready") || strings.Contains(bodyStr, "client closed")) {
				e.T.Logf("Spawn attempt %d/%d failed (transient): %s — retrying in 2s", attempt, maxRetries, bodyStr)
				time.Sleep(2 * time.Second)
				continue
			}
			e.T.Fatalf("Remote spawn returned non-200: %d\nBody: %s", spawnResp.StatusCode, bodyStr)
		}

		var results []SpawnResult
		if err := json.Unmarshal(body, &results); err != nil {
			e.T.Logf("Failed to decode spawn response: %v", err)
			return ""
		}

		if len(results) > 0 && results[0].Error != "" {
			errMsg := results[0].Error
			if attempt < maxRetries && (strings.Contains(errMsg, "control mode not ready") || strings.Contains(errMsg, "client closed")) {
				e.T.Logf("Spawn attempt %d/%d failed (transient error in response): %s — retrying in 2s", attempt, maxRetries, errMsg)
				time.Sleep(2 * time.Second)
				continue
			}
			e.T.Fatalf("Remote spawn failed: %s", errMsg)
		}

		if len(results) > 0 {
			e.T.Logf("Remote session spawned: %s (status: %s)", results[0].SessionID, results[0].Status)
			return results[0].SessionID
		}

		return ""
	}

	e.T.Fatalf("Remote spawn failed after %d attempts", maxRetries)
	return ""
}

// WaitForRemoteHostStatus waits for a remote host to reach a specific status.
func (e *Env) WaitForRemoteHostStatus(flavorID, expectedStatus string, timeout time.Duration) *RemoteHostResponse {
	e.T.Helper()
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		hosts := e.GetRemoteHosts()
		for _, host := range hosts {
			if host.FlavorID == flavorID && host.Status == expectedStatus {
				return &host
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	e.T.Fatalf("Timed out waiting for remote host %s to reach status %s", flavorID, expectedStatus)
	return nil
}

// WaitForSessionRunning waits for a session to reach running status via API.
func (e *Env) WaitForSessionRunning(sessionID string, timeout time.Duration) *APISession {
	e.T.Helper()
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		sessions := e.GetAPISessions()
		for _, sess := range sessions {
			if sess.ID == sessionID && sess.Running {
				return &sess
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	e.T.Fatalf("Timed out waiting for session %s to be running", sessionID)
	return nil
}

// AddCommandTargetToConfig adds a command run target to the config.
// This is used to add custom command targets in E2E tests.
// Note: detected tools (claude, codex, etc.) are now resolved at runtime
// by the model manager and do NOT need to be added to run_targets.
func (e *Env) AddCommandTargetToConfig(name, command string) {
	e.T.Helper()
	e.T.Logf("Adding command target to config: %s", name)

	configPath := filepath.Join(e.HomeDir, ".schmux", "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		e.T.Fatalf("Failed to load config: %v", err)
	}

	cfg.RunTargets = append(cfg.RunTargets, config.RunTarget{
		Name:    name,
		Command: command,
	})

	if err := cfg.Save(); err != nil {
		e.T.Fatalf("Failed to save config: %v", err)
	}
}

// ReloadConfig triggers the daemon to reload its config from disk.
// This is needed after modifying the config file while the daemon is running
// (e.g., after AddCommandTargetToConfig).
func (e *Env) ReloadConfig() {
	e.T.Helper()
	e.T.Logf("Reloading daemon config from disk")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, e.DaemonURL+"/api/config", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.T.Fatalf("Failed to reload config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		e.T.Fatalf("Config reload failed (status %d): %s", resp.StatusCode, string(body))
	}
}

// SetCompoundConfig enables compounding in the config with a fast debounce for testing.
func (e *Env) SetCompoundConfig(debounceMs int) {
	e.T.Helper()
	e.T.Logf("Setting compound config: enabled=true debounce_ms=%d", debounceMs)

	configPath := filepath.Join(e.HomeDir, ".schmux", "config.json")

	cfg, err := config.Load(configPath)
	if err != nil {
		e.T.Fatalf("Failed to load config: %v", err)
	}

	enabled := true
	cfg.Compound = &config.CompoundConfig{
		Enabled:          &enabled,
		DebounceMs:       debounceMs,
		SuppressionTTLMs: 1000, // 1s suppression window for E2E tests (default is 5s)
	}

	if err := cfg.Save(); err != nil {
		e.T.Fatalf("Failed to save config: %v", err)
	}
}

// SetCompoundDisabled explicitly disables compounding in the config.
func (e *Env) SetCompoundDisabled() {
	e.T.Helper()
	e.T.Log("Setting compound config: enabled=false")

	configPath := filepath.Join(e.HomeDir, ".schmux", "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		e.T.Fatalf("Failed to load config: %v", err)
	}

	enabled := false
	cfg.Compound = &config.CompoundConfig{
		Enabled: &enabled,
	}

	if err := cfg.Save(); err != nil {
		e.T.Fatalf("Failed to save config: %v", err)
	}
}

// CreateOverlayFile writes a file into ~/.schmux/overlays/<repoName>/<relPath>.
func (e *Env) CreateOverlayFile(repoName, relPath, content string) {
	e.T.Helper()
	e.T.Logf("Creating overlay file: repo=%s path=%s", repoName, relPath)

	overlayPath := filepath.Join(e.HomeDir, ".schmux", "overlays", repoName, relPath)
	if err := os.MkdirAll(filepath.Dir(overlayPath), 0755); err != nil {
		e.T.Fatalf("Failed to create overlay directory: %v", err)
	}
	if err := os.WriteFile(overlayPath, []byte(content), 0644); err != nil {
		e.T.Fatalf("Failed to write overlay file: %v", err)
	}
}

// GetWorkspacePath returns the filesystem path for a workspace by session ID.
func (e *Env) GetWorkspacePath(sessionID string) string {
	e.T.Helper()

	workspaces := e.GetAPIWorkspaces()
	for _, ws := range workspaces {
		for _, sess := range ws.Sessions {
			if sess.ID == sessionID {
				return ws.Path
			}
		}
	}

	e.T.Fatalf("Could not find workspace path for session %s", sessionID)
	return ""
}

// SetRepoOverlayPaths sets overlay_paths on a repo in the config.
func (e *Env) SetRepoOverlayPaths(repoName string, paths []string) {
	e.T.Helper()
	e.T.Logf("Setting overlay paths for repo %s: %v", repoName, paths)

	configPath := filepath.Join(e.HomeDir, ".schmux", "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		e.T.Fatalf("Failed to load config: %v", err)
	}

	for i := range cfg.Repos {
		if cfg.Repos[i].Name == repoName {
			cfg.Repos[i].OverlayPaths = paths
			break
		}
	}

	if err := cfg.Save(); err != nil {
		e.T.Fatalf("Failed to save config: %v", err)
	}
}

// GetOverlayAPI calls GET /api/overlays and returns the parsed response.
func (e *Env) GetOverlayAPI() OverlayAPIResponse {
	e.T.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.DaemonURL+"/api/overlays", nil)
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to GET /api/overlays: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		e.T.Fatalf("GET /api/overlays returned %d: %s", resp.StatusCode, body)
	}

	var result OverlayAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		e.T.Fatalf("Failed to decode overlay response: %v", err)
	}

	return result
}

// PostOverlayScan calls POST /api/overlays/scan and returns the scan candidates.
func (e *Env) PostOverlayScan(workspaceID, repoName string) []OverlayScanCandidate {
	e.T.Helper()

	reqBody, _ := json.Marshal(map[string]string{
		"workspace_id": workspaceID,
		"repo_name":    repoName,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.DaemonURL+"/api/overlays/scan", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to POST /api/overlays/scan: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		e.T.Fatalf("POST /api/overlays/scan returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Candidates []OverlayScanCandidate `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		e.T.Fatalf("Failed to decode scan response: %v", err)
	}

	return result.Candidates
}

// PostOverlayAdd calls POST /api/overlays/add and returns the result.
func (e *Env) PostOverlayAdd(workspaceID, repoName string, paths, customPaths []string) OverlayAddResult {
	e.T.Helper()

	reqBody, _ := json.Marshal(map[string]any{
		"workspace_id": workspaceID,
		"repo_name":    repoName,
		"paths":        paths,
		"custom_paths": customPaths,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.DaemonURL+"/api/overlays/add", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to POST /api/overlays/add: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		e.T.Fatalf("POST /api/overlays/add returned %d: %s", resp.StatusCode, body)
	}

	var result OverlayAddResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		e.T.Fatalf("Failed to decode add response: %v", err)
	}

	return result
}

// PostDismissNudge calls POST /api/overlays/dismiss-nudge.
func (e *Env) PostDismissNudge(repoName string) {
	e.T.Helper()

	reqBody, _ := json.Marshal(map[string]string{"repo_name": repoName})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.DaemonURL+"/api/overlays/dismiss-nudge", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.T.Fatalf("Failed to POST /api/overlays/dismiss-nudge: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		e.T.Fatalf("POST /api/overlays/dismiss-nudge returned %d: %s", resp.StatusCode, body)
	}
}

// GetWorkspaceIDForSession returns the workspace ID for a session.
func (e *Env) GetWorkspaceIDForSession(sessionID string) string {
	e.T.Helper()

	workspaces := e.GetAPIWorkspaces()
	for _, ws := range workspaces {
		for _, sess := range ws.Sessions {
			if sess.ID == sessionID {
				return ws.ID
			}
		}
	}

	e.T.Fatalf("Could not find workspace ID for session %s", sessionID)
	return ""
}

// PollUntil repeatedly calls check until it returns true or the timeout expires.
// It polls every 50ms. If the timeout expires, it calls t.Fatalf with the given message.
func (e *Env) PollUntil(timeout time.Duration, failMsg string, check func() bool) {
	e.T.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	e.T.Fatalf("PollUntil timed out after %v: %s", timeout, failMsg)
}

// OverlayChangeMessage represents an overlay_change WebSocket message from /ws/dashboard.
type OverlayChangeMessage struct {
	Type               string   `json:"type"`
	RelPath            string   `json:"rel_path"`
	SourceWorkspaceID  string   `json:"source_workspace_id"`
	SourceBranch       string   `json:"source_branch"`
	TargetWorkspaceIDs []string `json:"target_workspace_ids"`
	Timestamp          int64    `json:"timestamp"`
}

// DashboardRawMessage is a partially-parsed dashboard WebSocket message,
// keeping the payload as raw JSON so callers can decode type-specific fields.
type DashboardRawMessage struct {
	Type string
	Data json.RawMessage
}

// ReadDashboardRawMessage reads a single raw message from the dashboard WebSocket.
// Unlike ReadDashboardMessage, it preserves the full JSON payload for type-specific decoding.
func (e *Env) ReadDashboardRawMessage(conn *websocket.Conn, timeout time.Duration) (*DashboardRawMessage, error) {
	conn.SetReadDeadline(time.Now().Add(timeout))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	// Extract the "type" field without fully unmarshaling
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("failed to unmarshal dashboard message type: %w", err)
	}
	return &DashboardRawMessage{Type: envelope.Type, Data: data}, nil
}

// WaitForOverlayChange waits for an overlay_change WebSocket message with a matching relPath.
// It skips "sessions", "github_status", and other message types until it finds a match.
// The overlay_change message is sent AFTER files are written to disk, so after this returns
// the file content is already on disk and can be read directly.
func (e *Env) WaitForOverlayChange(conn *websocket.Conn, relPath string, timeout time.Duration) (*OverlayChangeMessage, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := e.ReadDashboardRawMessage(conn, time.Until(deadline))
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				return nil, fmt.Errorf("timed out waiting for overlay_change with rel_path=%q", relPath)
			}
			return nil, err
		}
		if raw.Type != "overlay_change" {
			continue
		}
		var msg OverlayChangeMessage
		if err := json.Unmarshal(raw.Data, &msg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal overlay_change message: %w", err)
		}
		if msg.RelPath == relPath {
			return &msg, nil
		}
	}
	return nil, fmt.Errorf("timed out waiting for overlay_change with rel_path=%q", relPath)
}
