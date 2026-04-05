package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Client implements DaemonClient for communicating with the schmux daemon.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewDaemonClient creates a new daemon client.
func NewDaemonClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// BaseURL returns the base URL this client is configured to use.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// ResolveURL resolves the daemon URL using a 3-tier priority:
// 1. SCHMUX_URL env var (explicit override)
// 2. ~/.schmux/daemon.url file (runtime breadcrumb)
// 3. http://localhost:7337 (hardcoded default)
func ResolveURL() string {
	if url := os.Getenv("SCHMUX_URL"); url != "" {
		return url
	}

	home, err := os.UserHomeDir()
	if err == nil {
		data, err := os.ReadFile(filepath.Join(home, ".schmux", "daemon.url"))
		if err == nil {
			url := strings.TrimSpace(string(data))
			if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
				return url
			}
		}
	}

	return "http://localhost:7337"
}

// IsRunning checks if the daemon is running.
func (c *Client) IsRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/healthz", nil)
	if err != nil {
		return false
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// GetConfig fetches the daemon configuration.
func (c *Client) GetConfig() (*Config, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/config", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("daemon returned status %d: %s", resp.StatusCode, string(body))
	}

	var cfg Config
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}

	return &cfg, nil
}

// GetWorkspaces fetches all workspaces.
func (c *Client) GetWorkspaces() ([]Workspace, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/workspaces", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("daemon returned status %d: %s", resp.StatusCode, string(body))
	}

	var workspaces []Workspace
	if err := json.NewDecoder(resp.Body).Decode(&workspaces); err != nil {
		return nil, fmt.Errorf("failed to decode workspaces: %w", err)
	}

	return workspaces, nil
}

// GetSessions fetches all sessions grouped by workspace.
func (c *Client) GetSessions() ([]WorkspaceWithSessions, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/sessions", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("daemon returned status %d: %s", resp.StatusCode, string(body))
	}

	var sessions []WorkspaceWithSessions
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, fmt.Errorf("failed to decode sessions: %w", err)
	}

	return sessions, nil
}

// Spawn spawns a new session.
func (c *Client) Spawn(ctx context.Context, req SpawnRequest) ([]SpawnResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
	}

	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/spawn", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	hr.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(hr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errorBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("daemon returned status %d (failed to read error body: %v)", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("daemon returned status %d: %s", resp.StatusCode, string(errorBody))
	}

	var results []SpawnResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return results, nil
}

// DisposeSession disposes a session.
func (c *Client) DisposeSession(ctx context.Context, sessionID string) error {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/sessions/"+sessionID+"/dispose", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errorBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("daemon returned status %d (failed to read error body: %v)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("daemon returned status %d: %s", resp.StatusCode, string(errorBody))
	}

	return nil
}

// ScanWorkspaces triggers a workspace scan.
func (c *Client) ScanWorkspaces(ctx context.Context) (*ScanResult, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/workspaces/scan", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errorBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("daemon returned status %d (failed to read error body: %v)", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("daemon returned status %d: %s", resp.StatusCode, string(errorBody))
	}

	var result ScanResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode scan result: %w", err)
	}

	return &result, nil
}

// RefreshOverlay reapplies overlay files to a workspace.
func (c *Client) RefreshOverlay(ctx context.Context, workspaceID string) error {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/workspaces/"+workspaceID+"/refresh-overlay", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errorBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("daemon returned status %d (failed to read error body: %v)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("daemon returned status %d: %s", resp.StatusCode, string(errorBody))
	}

	return nil
}

// Types

// Config represents the daemon configuration.
type Config struct {
	WorkspacePath string         `json:"workspace_path"`
	Repos         []Repo         `json:"repos"`
	RunTargets    []RunTarget    `json:"run_targets"`
	QuickLaunch   []QuickLaunch  `json:"quick_launch"`
	Models        []Model        `json:"models"`
	Terminal      TerminalConfig `json:"terminal"`
}

// Repo represents a git repository configuration.
type Repo struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	BarePath string `json:"bare_path,omitempty"`
}

// RunTarget represents a user-supplied run target.
type RunTarget struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// QuickLaunch represents a saved run preset.
// Either Command (shell command) or Target+Prompt (AI agent) should be set, not both.
type QuickLaunch struct {
	Name    string  `json:"name"`
	Command string  `json:"command,omitempty"` // shell command to run directly
	Target  string  `json:"target,omitempty"`  // run target (claude, codex, model, etc.)
	Prompt  *string `json:"prompt,omitempty"`  // prompt for the target
}

// RunnerInfo describes a tool/runner at the top level.
type RunnerInfo struct {
	Available    bool     `json:"available"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// Model represents an AI model with metadata and configuration status.
type Model struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"display_name"`
	Provider        string   `json:"provider"`
	Configured      bool     `json:"configured"`
	Runners         []string `json:"runners"`
	RequiredSecrets []string `json:"required_secrets,omitempty"`
}

// TerminalConfig represents terminal dimensions.
type TerminalConfig struct {
	Width     int `json:"width"`
	Height    int `json:"height"`
	SeedLines int `json:"seed_lines"`
}

// Workspace represents a workspace.
type Workspace struct {
	ID     string `json:"id"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Path   string `json:"path"`
	Dirty  bool   `json:"dirty"`
	Ahead  int    `json:"ahead"`
	Behind int    `json:"behind"`
}

// Session represents a session.
type Session struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	Target       string `json:"target"`
	Branch       string `json:"branch"`
	Nickname     string `json:"nickname,omitempty"`
	CreatedAt    string `json:"created_at"`
	LastOutputAt string `json:"last_output_at,omitempty"`
	Running      bool   `json:"running"`
	AttachCmd    string `json:"attach_cmd"`
	TmuxSocket   string `json:"tmux_socket,omitempty"`
	TmuxSession  string `json:"tmux_session,omitempty"`
}

// WorkspaceWithSessions represents a workspace with its sessions.
type WorkspaceWithSessions struct {
	ID           string    `json:"id"`
	Repo         string    `json:"repo"`
	Branch       string    `json:"branch"`
	Path         string    `json:"path"`
	SessionCount int       `json:"session_count"`
	Sessions     []Session `json:"sessions"`
	QuickLaunch  []string  `json:"quick_launch,omitempty"`
	Dirty        bool      `json:"dirty"`
	Ahead        int       `json:"ahead"`
	Behind       int       `json:"behind"`
}

// SpawnRequest represents a spawn request.
type SpawnRequest struct {
	Repo            string         `json:"repo"`
	Branch          string         `json:"branch"`
	Prompt          string         `json:"prompt"`
	Nickname        string         `json:"nickname,omitempty"`
	Targets         map[string]int `json:"targets"`
	WorkspaceID     string         `json:"workspace_id,omitempty"`
	Command         string         `json:"command,omitempty"`
	QuickLaunchName string         `json:"quick_launch_name,omitempty"`
	Resume          bool           `json:"resume,omitempty"`
	RemoteProfileID string         `json:"remote_profile_id,omitempty"`
	RemoteFlavor    string         `json:"remote_flavor,omitempty"`
	NewBranch       string         `json:"new_branch,omitempty"`
}

// SpawnResult represents the result of a spawn operation.
type SpawnResult struct {
	SessionID   string `json:"session_id,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Target      string `json:"target"`
	Error       string `json:"error,omitempty"`
}

// ScanResult represents the result of a workspace scan.
type ScanResult struct {
	Added   []Workspace       `json:"added"`
	Updated []WorkspaceChange `json:"updated"`
	Removed []Workspace       `json:"removed"`
}

// WorkspaceChange represents a workspace that was updated.
type WorkspaceChange struct {
	Old Workspace `json:"old"`
	New Workspace `json:"new"`
}

// RemoteAccessStatusResponse represents the tunnel status.
type RemoteAccessStatusResponse struct {
	State string `json:"state"`
	URL   string `json:"url,omitempty"`
	Error string `json:"error,omitempty"`
}

// RemoteAccessOn starts the remote access tunnel.
func (c *Client) RemoteAccessOn() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/remote-access/on", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	return nil
}

// RemoteAccessOff stops the remote access tunnel.
func (c *Client) RemoteAccessOff() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/remote-access/off", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	return nil
}

// RemoteAccessStatus gets the remote access tunnel status.
func (c *Client) RemoteAccessStatus() (*RemoteAccessStatusResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/remote-access/status", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}

	var status RemoteAccessStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &status, nil
}

// RemoteAccessSetPassword sets the remote access password.
func (c *Client) RemoteAccessSetPassword(password string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	payload, err := json.Marshal(map[string]string{"password": password})
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/remote-access/set-password", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	return nil
}
