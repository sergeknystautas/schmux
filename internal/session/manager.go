package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/tmux"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

const (
	// maxNicknameAttempts is the maximum number of attempts to find a unique nickname
	// before falling back to a UUID suffix.
	maxNicknameAttempts = 100

	// processKillGracePeriod is how long to wait for SIGTERM before SIGKILL
	processKillGracePeriod = 100 * time.Millisecond
)

// Manager manages sessions.
type Manager struct {
	config    *config.Config
	state     state.StateStore
	workspace workspace.WorkspaceManager
}

// ResolvedTarget is a resolved run target with command and env info.
type ResolvedTarget struct {
	Name       string
	Kind       string
	Command    string
	Promptable bool
	Env        map[string]string
}

const (
	TargetKindDetected = "detected"
	TargetKindVariant  = "variant"
	TargetKindUser     = "user"
)

// New creates a new session manager.
func New(cfg *config.Config, st state.StateStore, statePath string, wm workspace.WorkspaceManager) *Manager {
	return &Manager{
		config:    cfg,
		state:     st,
		workspace: wm,
	}
}

// Spawn creates a new session.
// If workspaceID is provided, spawn into that specific workspace (Existing Directory Spawn mode).
// Otherwise, find or create a workspace by repoURL/branch.
// nickname is an optional human-friendly name for the session.
// prompt is only used if the target is promptable.
func (m *Manager) Spawn(ctx context.Context, repoURL, branch, targetName, prompt, nickname string, workspaceID string) (*state.Session, error) {
	resolved, err := m.ResolveTarget(ctx, targetName)
	if err != nil {
		return nil, err
	}

	var w *state.Workspace

	if workspaceID != "" {
		// Spawn into specific workspace (Existing Directory Spawn mode - no git operations)
		ws, found := m.workspace.GetByID(workspaceID)
		if !found {
			return nil, fmt.Errorf("workspace not found: %s", workspaceID)
		}
		w = ws
	} else {
		// Get or create workspace (includes fetch/pull/clean)
		w, err = m.workspace.GetOrCreate(ctx, repoURL, branch)
		if err != nil {
			return nil, fmt.Errorf("failed to get workspace: %w", err)
		}
	}

	command, err := buildCommand(resolved, prompt)
	if err != nil {
		return nil, err
	}

	// Create session ID
	sessionID := fmt.Sprintf("%s-%s", w.ID, uuid.New().String()[:8])

	// Generate unique nickname if provided (auto-suffix if duplicate)
	uniqueNickname := nickname
	if nickname != "" {
		uniqueNickname = m.generateUniqueNickname(nickname)
	}

	// Use sanitized unique nickname for tmux session name if provided, otherwise use sessionID
	tmuxSession := sessionID
	if uniqueNickname != "" {
		tmuxSession = sanitizeNickname(uniqueNickname)
	}

	// Create tmux session
	if err := tmux.CreateSession(ctx, tmuxSession, w.Path, command); err != nil {
		return nil, fmt.Errorf("failed to create tmux session: %w", err)
	}

	// Set up log file for pipe-pane streaming
	logPath, err := m.ensureLogFile(sessionID)
	if err != nil {
		fmt.Printf("[session] warning: failed to create log file: %v\n", err)
	} else {
		// Force fixed window size for deterministic TUI output
		width, height := m.config.GetTerminalSize()
		if err := tmux.SetWindowSizeManual(ctx, tmuxSession); err != nil {
			fmt.Printf("[session] warning: failed to set manual window size: %v\n", err)
		}
		if err := tmux.ResizeWindow(ctx, tmuxSession, width, height); err != nil {
			fmt.Printf("[session] warning: failed to resize window: %v\n", err)
		}
		// Start pipe-pane to log file
		if err := tmux.StartPipePane(ctx, tmuxSession, logPath); err != nil {
			return nil, fmt.Errorf("failed to start pipe-pane (session created): %w", err)
		}
	}

	// Get the PID of the agent process from tmux pane
	pid, err := tmux.GetPanePID(ctx, tmuxSession)
	if err != nil {
		return nil, fmt.Errorf("failed to get pane PID: %w", err)
	}

	// Create session state with cached PID (no Prompt field)
	sess := state.Session{
		ID:          sessionID,
		WorkspaceID: w.ID,
		Target:      targetName,
		Nickname:    uniqueNickname,
		TmuxSession: tmuxSession,
		CreatedAt:   time.Now(),
		Pid:         pid,
	}

	if err := m.state.AddSession(sess); err != nil {
		return nil, fmt.Errorf("failed to add session to state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	return &sess, nil
}

// SpawnCommand spawns a session running a raw shell command.
// Used for quick launch presets with a direct command (no target resolution).
func (m *Manager) SpawnCommand(ctx context.Context, repoURL, branch, command, nickname, workspaceID string) (*state.Session, error) {
	var w *state.Workspace
	var err error

	if workspaceID != "" {
		// Spawn into specific workspace (Existing Directory Spawn mode - no git operations)
		ws, found := m.workspace.GetByID(workspaceID)
		if !found {
			return nil, fmt.Errorf("workspace not found: %s", workspaceID)
		}
		w = ws
	} else {
		// Get or create workspace (includes fetch/pull/clean)
		w, err = m.workspace.GetOrCreate(ctx, repoURL, branch)
		if err != nil {
			return nil, fmt.Errorf("failed to get workspace: %w", err)
		}
	}

	// Create session ID
	sessionID := fmt.Sprintf("%s-%s", w.ID, uuid.New().String()[:8])

	// Generate unique nickname if provided (auto-suffix if duplicate)
	uniqueNickname := nickname
	if nickname != "" {
		uniqueNickname = m.generateUniqueNickname(nickname)
	}

	// Use sanitized unique nickname for tmux session name if provided, otherwise use sessionID
	tmuxSession := sessionID
	if uniqueNickname != "" {
		tmuxSession = sanitizeNickname(uniqueNickname)
	}

	// Create tmux session with the raw command
	if err := tmux.CreateSession(ctx, tmuxSession, w.Path, command); err != nil {
		return nil, fmt.Errorf("failed to create tmux session: %w", err)
	}

	// Set up log file for pipe-pane streaming
	logPath, err := m.ensureLogFile(sessionID)
	if err != nil {
		fmt.Printf("[session] warning: failed to create log file: %v\n", err)
	} else {
		// Force fixed window size for deterministic TUI output
		width, height := m.config.GetTerminalSize()
		if err := tmux.SetWindowSizeManual(ctx, tmuxSession); err != nil {
			fmt.Printf("[session] warning: failed to set manual window size: %v\n", err)
		}
		if err := tmux.ResizeWindow(ctx, tmuxSession, width, height); err != nil {
			fmt.Printf("[session] warning: failed to resize window: %v\n", err)
		}
		// Start pipe-pane to log file
		if err := tmux.StartPipePane(ctx, tmuxSession, logPath); err != nil {
			return nil, fmt.Errorf("failed to start pipe-pane (session created): %w", err)
		}
	}

	// Get the PID of the process from tmux pane
	pid, err := tmux.GetPanePID(ctx, tmuxSession)
	if err != nil {
		return nil, fmt.Errorf("failed to get pane PID: %w", err)
	}

	// Create session state (Target uses a stable value for command-based sessions)
	sess := state.Session{
		ID:          sessionID,
		WorkspaceID: w.ID,
		Target:      "command",
		Nickname:    uniqueNickname,
		TmuxSession: tmuxSession,
		CreatedAt:   time.Now(),
		Pid:         pid,
	}

	if err := m.state.AddSession(sess); err != nil {
		return nil, fmt.Errorf("failed to add session to state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	return &sess, nil
}

// ResolveTarget resolves a target name to a command and env.
func (m *Manager) ResolveTarget(_ context.Context, targetName string) (ResolvedTarget, error) {
	for _, variant := range m.config.GetMergedVariants() {
		if variant.Name == targetName {
			baseTarget, found := m.config.GetDetectedRunTarget(variant.BaseTool)
			if !found {
				return ResolvedTarget{}, fmt.Errorf("variant %s requires base tool %s which is not available", variant.Name, variant.BaseTool)
			}
			secrets, err := config.GetVariantSecrets(variant.Name)
			if err != nil {
				return ResolvedTarget{}, fmt.Errorf("failed to load secrets for variant %s: %w", variant.Name, err)
			}
			if err := ensureVariantSecrets(variant, secrets); err != nil {
				return ResolvedTarget{}, err
			}
			env := mergeEnvMaps(variant.Env, secrets)
			return ResolvedTarget{
				Name:       variant.Name,
				Kind:       TargetKindVariant,
				Command:    baseTarget.Command,
				Promptable: true,
				Env:        env,
			}, nil
		}
	}

	if target, found := m.config.GetRunTarget(targetName); found {
		kind := TargetKindUser
		if target.Source == config.RunTargetSourceDetected {
			kind = TargetKindDetected
		}
		return ResolvedTarget{
			Name:       target.Name,
			Kind:       kind,
			Command:    target.Command,
			Promptable: target.Type == config.RunTargetTypePromptable,
		}, nil
	}

	return ResolvedTarget{}, fmt.Errorf("target not found: %s", targetName)
}

func buildCommand(target ResolvedTarget, prompt string) (string, error) {
	trimmedPrompt := strings.TrimSpace(prompt)
	if target.Promptable {
		if trimmedPrompt == "" {
			return "", fmt.Errorf("prompt is required for target %s", target.Name)
		}
		command := fmt.Sprintf("%s %s", target.Command, strconv.Quote(prompt))
		if len(target.Env) > 0 {
			return fmt.Sprintf("%s %s", buildEnvPrefix(target.Env), command), nil
		}
		return command, nil
	}

	if trimmedPrompt != "" {
		return "", fmt.Errorf("prompt is not allowed for command target %s", target.Name)
	}
	if len(target.Env) > 0 {
		return fmt.Sprintf("%s %s", buildEnvPrefix(target.Env), target.Command), nil
	}
	return target.Command, nil
}

func buildEnvPrefix(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, strconv.Quote(env[k])))
	}
	return strings.Join(parts, " ")
}

func mergeEnvMaps(base, overrides map[string]string) map[string]string {
	if base == nil && overrides == nil {
		return nil
	}
	out := make(map[string]string, len(base)+len(overrides))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}

func ensureVariantSecrets(variant detect.Variant, secrets map[string]string) error {
	for _, key := range variant.RequiredSecrets {
		val := strings.TrimSpace(secrets[key])
		if val == "" {
			return fmt.Errorf("variant %s missing required secret %s", variant.Name, key)
		}
	}
	return nil
}

// IsRunning checks if the agent process is still running.
// Uses the cached PID from tmux pane, which is more reliable than searching by process name.
func (m *Manager) IsRunning(ctx context.Context, sessionID string) bool {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return false
	}

	// If we don't have a PID, check if tmux session exists as fallback
	if sess.Pid == 0 {
		return tmux.SessionExists(ctx, sess.TmuxSession)
	}

	// Check if the process is still running
	process, err := os.FindProcess(sess.Pid)
	if err != nil {
		return false
	}

	// Send signal 0 to check if process exists
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false
	}

	return true
}

// killProcessGroup kills a process and its entire process group.
// On Unix, a negative PID to syscall.Kill signals the entire process group.
func killProcessGroup(pid int) error {
	// First try to kill the process group (negative PID)
	// Use syscall.Kill directly since os.FindProcess doesn't handle negative PIDs correctly
	if err := syscall.Kill(-pid, syscall.SIGTERM); err == nil {
		// Successfully sent SIGTERM to process group
		// Wait for graceful shutdown
		time.Sleep(processKillGracePeriod)

		// Check if process group is still alive and force kill if needed
		if err := syscall.Kill(-pid, syscall.Signal(0)); err == nil {
			// Process group still exists, send SIGKILL
			if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
				// Log but don't fail - process may have exited
				fmt.Printf("[session] warning: failed to send SIGKILL to process group %d: %v\n", -pid, err)
			}
		}
		return nil
	}

	// Fallback: process group may not exist, try killing the process directly
	// Check if process exists first
	if err := syscall.Kill(pid, syscall.Signal(0)); err != nil {
		// Process doesn't exist, nothing to do
		return nil
	}

	// Send SIGTERM for graceful shutdown
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		// Process may already be dead, which is fine
		return nil
	}

	// Wait for graceful shutdown
	time.Sleep(processKillGracePeriod)

	// Force kill if still running
	if err := syscall.Kill(pid, syscall.Signal(0)); err == nil {
		// Process still exists, send SIGKILL
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			fmt.Printf("[session] warning: failed to send SIGKILL to process %d: %v\n", pid, err)
		}
	}

	return nil
}

// findProcessesInWorkspace finds all processes with a working directory in the given workspace path.
// Returns a list of PIDs. Returns empty slice if no processes found (not an error).
func findProcessesInWorkspace(workspacePath string) ([]int, error) {
	// Normalize workspace path for proper matching
	workspacePath = filepath.Clean(workspacePath)
	// Ensure path ends with separator for proper prefix matching
	workspacePrefix := workspacePath + string(filepath.Separator)

	// Use ps to find processes with cwd matching the workspace path
	cmd := exec.Command("ps", "-eo", "pid,cwd")
	output, err := cmd.Output()
	// If ps fails, return empty (no processes to kill)
	// This handles cases where ps returns exit status 1 due to no matches
	if err != nil {
		return nil, nil
	}

	var pids []int
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pidStr := fields[0]
		cwd := fields[1]

		// Check if the working directory matches or is within the workspace
		// Use proper path separator to avoid matching similar paths (e.g., /workspace vs /workspace-backup)
		if cwd == workspacePath || strings.HasPrefix(cwd, workspacePrefix) {
			pid, err := strconv.Atoi(pidStr)
			if err != nil {
				continue
			}
			pids = append(pids, pid)
		}
	}

	return pids, nil
}

// Dispose disposes of a session.
func (m *Manager) Dispose(ctx context.Context, sessionID string) error {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Track what we've done for the summary
	var warnings []string
	processesKilled := 0
	orphanKilled := 0
	tmuxKilled := false

	// Get the workspace for process cleanup fallback
	ws, found := m.workspace.GetByID(sess.WorkspaceID)
	if found {
		// Step 1: Kill the tracked process group (if we have a PID)
		if sess.Pid > 0 {
			if err := killProcessGroup(sess.Pid); err != nil {
				warnings = append(warnings, fmt.Sprintf("failed to kill process group %d: %v", sess.Pid, err))
			} else {
				processesKilled = 1
			}
		}

		// Step 2: Fallback - find and kill any orphaned processes in the workspace directory
		// This catches processes that may have escaped the process group
		// Check context before doing expensive process scan
		if ctx.Err() == nil {
			orphanPIDs, _ := findProcessesInWorkspace(ws.Path)
			for _, pid := range orphanPIDs {
				// Check context before each kill
				if ctx.Err() != nil {
					break
				}
				// Skip the tracked PID since we already tried to kill it
				if sess.Pid > 0 && pid == sess.Pid {
					continue
				}
				if err := killProcessGroup(pid); err != nil {
					warnings = append(warnings, fmt.Sprintf("failed to kill orphaned process %d: %v", pid, err))
				} else {
					orphanKilled++
				}
			}
		}
	}

	// Step 3: Kill tmux session (ignore error if already gone - that's success)
	if err := tmux.KillSession(ctx, sess.TmuxSession); err == nil {
		tmuxKilled = true
	}

	// Delete log file for this session
	if err := m.deleteLogFile(sessionID); err != nil {
		warnings = append(warnings, fmt.Sprintf("failed to delete log file: %v", err))
	}

	// Note: workspace is NOT cleaned up on session disposal.
	// Workspaces persist and are only reset when reused for a new spawn.

	// Remove session from state
	if err := m.state.RemoveSession(sessionID); err != nil {
		return fmt.Errorf("failed to remove session from state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	// Print summary
	summary := fmt.Sprintf("Disposed session %s: killed %d process group", sessionID, processesKilled)
	if orphanKilled > 0 {
		summary += fmt.Sprintf(" + %d orphaned process(es)", orphanKilled)
	}
	if tmuxKilled {
		summary += " + tmux session"
	}
	fmt.Printf("[session] %s\n", summary)

	// Print warnings if any
	for _, w := range warnings {
		fmt.Printf("[session]   warning: %s\n", w)
	}

	return nil
}

// GetAttachCommand returns the tmux attach command for a session.
func (m *Manager) GetAttachCommand(sessionID string) (string, error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	return tmux.GetAttachCommand(sess.TmuxSession), nil
}

// GetOutput returns the current terminal output for a session.
func (m *Manager) GetOutput(ctx context.Context, sessionID string) (string, error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	return tmux.CaptureOutput(ctx, sess.TmuxSession)
}

// GetAllSessions returns all sessions.
func (m *Manager) GetAllSessions() []state.Session {
	return m.state.GetSessions()
}

// GetSession returns a session by ID.
func (m *Manager) GetSession(sessionID string) (*state.Session, error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return &sess, nil
}

// RenameSession updates a session's nickname and renames the tmux session.
// The nickname is sanitized before use as the tmux session name.
// Returns an error if the new nickname conflicts with an existing session.
func (m *Manager) RenameSession(ctx context.Context, sessionID, newNickname string) error {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Check if new nickname conflicts with an existing session
	if conflictingID := m.nicknameExists(newNickname, sessionID); conflictingID != "" {
		return fmt.Errorf("nickname %q already in use by session %s", newNickname, conflictingID)
	}

	oldTmuxName := sess.TmuxSession
	newTmuxName := oldTmuxName
	if newNickname != "" {
		newTmuxName = sanitizeNickname(newNickname)
	}

	// Rename the tmux session
	if err := tmux.RenameSession(ctx, oldTmuxName, newTmuxName); err != nil {
		return fmt.Errorf("failed to rename tmux session: %w", err)
	}

	// Update session state
	sess.Nickname = newNickname
	sess.TmuxSession = newTmuxName
	if err := m.state.UpdateSession(sess); err != nil {
		return fmt.Errorf("failed to update session in state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	return nil
}

// getLogDir returns the log directory path, creating it if needed.
func (m *Manager) getLogDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	logDir := filepath.Join(homeDir, ".schmux", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create log directory: %w", err)
	}
	return logDir, nil
}

// getLogPath returns the log file path for a session.
func (m *Manager) getLogPath(sessionID string) (string, error) {
	logDir, err := m.getLogDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(logDir, fmt.Sprintf("%s.log", sessionID)), nil
}

// ensureLogFile ensures the log file exists for a session.
func (m *Manager) ensureLogFile(sessionID string) (string, error) {
	logPath, err := m.getLogPath(sessionID)
	if err != nil {
		return "", err
	}
	fd, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return "", fmt.Errorf("failed to create log file: %w", err)
	}
	fd.Close()
	return logPath, nil
}

// deleteLogFile removes the log file for a session.
func (m *Manager) deleteLogFile(sessionID string) error {
	logPath, err := m.getLogPath(sessionID)
	if err != nil {
		return err
	}
	if err := os.Remove(logPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete log file: %w", err)
	}
	return nil
}

// pruneLogFiles removes log files for sessions not in the active list.
func (m *Manager) pruneLogFiles(activeSessions []state.Session) error {
	logDir, err := m.getLogDir()
	if err != nil {
		return err
	}
	activeIDs := make(map[string]bool)
	for _, sess := range activeSessions {
		activeIDs[sess.ID] = true
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return fmt.Errorf("failed to read log directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".log")
		if !activeIDs[sessionID] {
			logPath := filepath.Join(logDir, entry.Name())
			if err := os.Remove(logPath); err != nil {
				fmt.Printf("[session] warning: failed to delete orphaned log %s: %v\n", entry.Name(), err)
			}
		}
	}
	return nil
}

// GetLogPath returns the log file path for a session (public for WebSocket).
func (m *Manager) GetLogPath(sessionID string) (string, error) {
	return m.getLogPath(sessionID)
}

// EnsurePipePane ensures pipe-pane is active for a session (auto-migrate old sessions).
func (m *Manager) EnsurePipePane(ctx context.Context, sessionID string) error {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	// Check if pipe-pane is already active
	if tmux.IsPipePaneActive(ctx, sess.TmuxSession) {
		return nil
	}
	// Ensure log file exists
	logPath, err := m.ensureLogFile(sessionID)
	if err != nil {
		return fmt.Errorf("failed to ensure log file: %w", err)
	}
	// Set window size and start pipe-pane
	width, height := m.config.GetTerminalSize()
	if err := tmux.SetWindowSizeManual(ctx, sess.TmuxSession); err != nil {
		fmt.Printf("[session] warning: failed to set manual window size: %v\n", err)
	}
	if err := tmux.ResizeWindow(ctx, sess.TmuxSession, width, height); err != nil {
		fmt.Printf("[session] warning: failed to resize window: %v\n", err)
	}
	if err := tmux.StartPipePane(ctx, sess.TmuxSession, logPath); err != nil {
		return fmt.Errorf("failed to start pipe-pane: %w", err)
	}
	return nil
}

// StartLogPruner starts periodic log pruning. Returns cancel function.
func (m *Manager) StartLogPruner(interval time.Duration) func() {
	stopChan := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		m.pruneLogs() // Run once on startup
		for {
			select {
			case <-ticker.C:
				m.pruneLogs()
			case <-stopChan:
				return
			}
		}
	}()
	return func() { close(stopChan) }
}

// pruneLogs runs pruneLogFiles with current sessions.
func (m *Manager) pruneLogs() {
	activeSessions := m.state.GetSessions()
	if err := m.pruneLogFiles(activeSessions); err != nil {
		fmt.Printf("[session] warning: log prune failed: %v\n", err)
	}
}

// sanitizeNickname sanitizes a nickname for use as a tmux session name.
// tmux session names cannot contain dots (.) or colons (:).
func sanitizeNickname(nickname string) string {
	result := strings.ReplaceAll(nickname, ".", "-")
	result = strings.ReplaceAll(result, ":", "-")
	return result
}

// nicknameExists checks if a nickname (or its sanitized tmux session name) already exists.
// Returns the conflicting session ID if found, empty string otherwise.
// excludeSessionID is used during rename to skip the session being renamed.
func (m *Manager) nicknameExists(nickname, excludeSessionID string) string {
	if nickname == "" {
		return ""
	}
	tmuxName := sanitizeNickname(nickname)
	sessions := m.state.GetSessions()
	for _, sess := range sessions {
		// Skip the session being edited (for rename operations)
		if sess.ID == excludeSessionID {
			continue
		}
		// Check if tmux session name matches (nicknames are sanitized for tmux)
		if sess.TmuxSession == tmuxName {
			return sess.ID
		}
	}
	return ""
}

// generateUniqueNickname generates a unique nickname by trying the base name,
// then "name (1)", "name (2)", etc. until a unique name is found.
func (m *Manager) generateUniqueNickname(baseNickname string) string {
	if baseNickname == "" {
		return ""
	}
	// Try base name first
	if m.nicknameExists(baseNickname, "") == "" {
		return baseNickname
	}
	// Try numbered suffixes
	for i := 1; i <= maxNicknameAttempts; i++ {
		candidate := fmt.Sprintf("%s (%d)", baseNickname, i)
		if m.nicknameExists(candidate, "") == "" {
			return candidate
		}
	}
	// Fallback: use base nickname with a UUID suffix (should never happen in practice)
	return fmt.Sprintf("%s-%s", baseNickname, uuid.New().String()[:8])
}
