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

	"github.com/sergek/schmux/internal/config"
	"github.com/gorilla/websocket"
)

const (
	// DefaultDaemonURL is the default URL for the daemon API
	DefaultDaemonURL = "http://127.0.0.1:7337"
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
}

// APIWorkspace represents a workspace from the API response.
type APIWorkspace struct {
	ID           string       `json:"id"`
	Repo         string       `json:"repo"`
	Branch       string       `json:"branch"`
	Path         string       `json:"path"`
	SessionCount int          `json:"session_count"`
	Sessions     []APISession `json:"sessions"`
	GitDirty     bool         `json:"git_dirty"`
	GitAhead     int          `json:"git_ahead"`
	GitBehind    int          `json:"git_behind"`
}

// Env is the E2E test environment.
// Docker provides isolation - no need for HOME overrides or env vars.
type Env struct {
	T             *testing.T
	SchmuxBin     string
	DaemonURL     string
	daemonStarted bool
	gitRepoDir    string // temp local git repo for testing
}

// New creates a new E2E test environment.
func New(t *testing.T) *Env {
	t.Helper()

	// Find schmux binary - it should be built and in PATH
	schmuxBin, err := exec.LookPath("schmux")
	if err != nil {
		t.Skipf("schmux binary not found in PATH (run `go build ./cmd/schmux` first)")
	}

	e := &Env{
		T:         t,
		SchmuxBin: schmuxBin,
		DaemonURL: DefaultDaemonURL,
	}

	t.Cleanup(e.Cleanup)
	return e
}

// Cleanup cleans up the test environment.
func (e *Env) Cleanup() {
	if e.daemonStarted {
		e.T.Log("Stopping daemon...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, e.SchmuxBin, "stop")
		out, _ := cmd.CombinedOutput()
		cancel()
		e.T.Logf("stop output: %s", out)

		// Wait a bit for daemon to fully stop
		time.Sleep(500 * time.Millisecond)
	}

	if e.gitRepoDir != "" {
		os.RemoveAll(e.gitRepoDir)
	}
}

// DaemonStart starts the schmux daemon.
func (e *Env) DaemonStart() {
	e.T.Helper()
	e.T.Log("Starting daemon...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	cmd := exec.CommandContext(ctx, e.SchmuxBin, "start")
	out, err := cmd.CombinedOutput()
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to start daemon: %v\nOutput: %s", err, out)
	}

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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	cmd := exec.CommandContext(ctx, e.SchmuxBin, "stop")
	out, err := cmd.CombinedOutput()
	cancel()
	if err != nil {
		e.T.Logf("Warning: stop command failed: %v\nOutput: %s", err, out)
	}

	e.daemonStarted = false

	// Verify daemon is stopped
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.DaemonURL+"/api/healthz", nil)
	_, err = http.DefaultClient.Do(req)
	cancel()
	if err == nil {
		e.T.Error("Daemon is still running after stop")
	}

	time.Sleep(500 * time.Millisecond)
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

	homeDir, err := os.UserHomeDir()
	if err != nil {
		e.T.Fatalf("Failed to get home dir: %v", err)
	}

	schmuxDir := filepath.Join(homeDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		e.T.Fatalf("Failed to create .schmux dir: %v", err)
	}

	configPath := filepath.Join(schmuxDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = workspacePath
	cfg.RunTargets = []config.RunTarget{
		// Keep the session alive long enough for pipe-pane and tmux assertions.
		{Name: "echo", Type: "command", Command: "sh -c 'echo hello; sleep 600'", Source: "user"},
		// Echoes input back for websocket output tests.
		{Name: "cat", Type: "command", Command: "cat", Source: "user"},
	}

	if err := cfg.Save(); err != nil {
		e.T.Fatalf("Failed to save config: %v", err)
	}
}

// WSOutputMessage represents a WebSocket message to the client.
type WSOutputMessage struct {
	Type    string `json:"type"` // "full", "append", "reconnect"
	Content string `json:"content"`
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
	header.Set("Origin", "http://localhost:7337")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL.String(), header)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// WaitForWebSocketContent reads websocket output until it finds the substring or times out.
func (e *Env) WaitForWebSocketContent(conn *websocket.Conn, substr string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var buffer strings.Builder

	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		_, data, err := conn.ReadMessage()
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				continue
			}
			return buffer.String(), err
		}

		var msg WSOutputMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Content != "" {
			buffer.WriteString(msg.Content)
			if strings.Contains(buffer.String(), substr) {
				return buffer.String(), nil
			}
		}
	}

	return buffer.String(), fmt.Errorf("timed out waiting for websocket output: %q", substr)
}

// SendKeysToTmux sends literal keys plus Enter to a tmux session.
func (e *Env) SendKeysToTmux(sessionName, text string) {
	e.T.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	cmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", sessionName, "-l", text)
	out, err := cmd.CombinedOutput()
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to send keys to tmux: %v\nOutput: %s", err, out)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	cmd = exec.CommandContext(ctx, "tmux", "send-keys", "-t", sessionName, "C-m")
	out, err = cmd.CombinedOutput()
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to send Enter to tmux: %v\nOutput: %s", err, out)
	}
}

// AddRepoToConfig adds a repo to the config file.
func (e *Env) AddRepoToConfig(name, url string) {
	e.T.Helper()
	e.T.Logf("Adding repo to config: %s -> %s", name, url)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		e.T.Fatalf("Failed to get home dir: %v", err)
	}

	schmuxDir := filepath.Join(homeDir, ".schmux")
	configPath := filepath.Join(schmuxDir, "config.json")

	cfg, err := config.Load(configPath)
	if err != nil {
		e.T.Fatalf("Failed to load config: %v", err)
	}

	cfg.Repos = append(cfg.Repos, config.Repo{Name: name, URL: url})
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

// ListSessions lists sessions via CLI.
// Returns the raw output.
func (e *Env) ListSessions() string {
	e.T.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	cmd := exec.CommandContext(ctx, e.SchmuxBin, "list")
	out, err := cmd.CombinedOutput()
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to list sessions: %v\nOutput: %s", err, out)
	}

	return string(out)
}

// DisposeSession disposes a session via CLI.
func (e *Env) DisposeSession(sessionID string) {
	e.T.Helper()
	e.T.Logf("Disposing session: %s", sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	cmd := exec.CommandContext(ctx, e.SchmuxBin, "dispose", sessionID)
	// Confirm the interactive prompt.
	cmd.Stdin = strings.NewReader("y\n")
	out, err := cmd.CombinedOutput()
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to dispose session: %v\nOutput: %s", err, out)
	}
}

// GetTmuxSessions returns the list of tmux session names.
func (e *Env) GetTmuxSessions() []string {
	e.T.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	cmd := exec.CommandContext(ctx, "tmux", "ls")
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

// GetAPISessions returns the list of sessions from the API.
func (e *Env) GetAPISessions() []APISession {
	e.T.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.DaemonURL+"/api/sessions", nil)
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		e.T.Fatalf("Failed to get sessions from API: %v", err)
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

	// Flatten sessions from all workspaces
	var allSessions []APISession
	for _, ws := range workspaces {
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
		e.T.Logf("Failed to create failure dir: %v", err)
		return
	}

	e.T.Logf("Capturing artifacts to: %s", failureDir)

	// Capture config.json and state.json
	homeDir, err := os.UserHomeDir()
	if err == nil {
		configPath := filepath.Join(homeDir, ".schmux", "config.json")
		if data, err := os.ReadFile(configPath); err == nil {
			os.WriteFile(filepath.Join(failureDir, "config.json"), data, 0644)
		}

		statePath := filepath.Join(homeDir, ".schmux", "state.json")
		if data, err := os.ReadFile(statePath); err == nil {
			os.WriteFile(filepath.Join(failureDir, "state.json"), data, 0644)
		}
	}

	// Capture tmux ls output
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	cmd := exec.CommandContext(ctx, "tmux", "ls")
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

// RunCmd runs a command in the given directory.
func RunCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
