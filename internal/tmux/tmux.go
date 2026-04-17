package tmux

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

// validSessionNameChars allows characters that are safe inside a tmux target
// AND safe inside a double-quoted shell string (for GetAttachCommand copy-paste).
// Rejected: $ ` \ " (shell escape), ; & ' (shell quoting hazards), . : (tmux rewrites),
// control chars, newline, tab.
var validSessionNameChars = regexp.MustCompile(`^[a-zA-Z0-9 _\-+*?#%^~@/,<>()\[\]{}|!]+$`)

// ValidateSessionName checks if a session name is safe to use with tmux and
// safe to embed in a double-quoted shell string.
func ValidateSessionName(name string) error {
	if name == "" {
		return fmt.Errorf("session name cannot be empty")
	}
	if !validSessionNameChars.MatchString(name) {
		return fmt.Errorf("invalid session name %q: contains disallowed characters", name)
	}
	if name[0] == '-' || name[0] == ' ' {
		return fmt.Errorf("invalid session name %q: cannot start with %q", name, name[0])
	}
	if name[len(name)-1] == ' ' {
		return fmt.Errorf("invalid session name %q: cannot end with a space", name)
	}
	return nil
}

// ValidateBinary checks that the given path is a valid tmux executable.
// It verifies the file exists, is executable, and outputs a recognized tmux
// version string. Returns the version string (e.g. "tmux 3.5") on success.
func ValidateBinary(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("file not found: %s", path)
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory, not a file: %s", path)
	}
	if info.Mode()&0111 == 0 {
		return "", fmt.Errorf("not executable: %s", path)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-V")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to execute %s -V: %w", path, err)
	}

	version := strings.TrimSpace(string(output))
	if !strings.HasPrefix(version, "tmux ") {
		return "", fmt.Errorf("not a tmux binary (output: %q)", version)
	}
	return version, nil
}

// TmuxServer manages an isolated tmux server accessed via the -L flag.
// All methods prepend "-L socketName" to tmux commands automatically,
// ensuring socket isolation between different server instances.
type TmuxServer struct {
	binary     string
	socketName string
	logger     *log.Logger
}

// NewTmuxServer creates a TmuxServer that targets the named socket.
func NewTmuxServer(binary, socketName string, logger *log.Logger) *TmuxServer {
	return &TmuxServer{binary: binary, socketName: socketName, logger: logger}
}

// Binary returns the tmux executable path.
func (s *TmuxServer) Binary() string { return s.binary }

// SocketName returns the tmux socket name used with -L.
func (s *TmuxServer) SocketName() string { return s.socketName }

// cmd builds an exec.Cmd that targets this server's socket.
func (s *TmuxServer) cmd(ctx context.Context, args ...string) *exec.Cmd {
	fullArgs := append([]string{"-L", s.socketName}, args...)
	return exec.CommandContext(ctx, s.binary, fullArgs...)
}

// Check verifies that the tmux binary is installed and accessible.
func (s *TmuxServer) Check() error {
	cmd := exec.Command(s.binary, "-V")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux is not installed or not accessible.\n-> %w", err)
	}
	if len(output) == 0 {
		return fmt.Errorf("tmux command produced no output")
	}
	return nil
}

// StartServer starts the tmux server for this socket.
// This is a no-op if the server is already running.
func (s *TmuxServer) StartServer(ctx context.Context) error {
	cmd := s.cmd(ctx, "start-server")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start tmux server: %w", err)
	}
	return nil
}

// CreateSession creates a new tmux session with the given name, directory, and command.
func (s *TmuxServer) CreateSession(ctx context.Context, name, dir, command string) error {
	// Validate session name to prevent command injection
	if err := ValidateSessionName(name); err != nil {
		return fmt.Errorf("invalid session name: %w", err)
	}

	args := []string{
		"new-session",
		"-d",       // detached
		"-s", name, // session name
		"-c", dir, // working directory
		command,
	}

	cmd := s.cmd(ctx, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tmux session: %w: %s", err, string(output))
	}

	// Set scrollback to 10000 lines (tmux default is 2000)
	if err := s.SetOption(ctx, name, "history-limit", "10000"); err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to set history-limit", "session", name, "err", err)
		}
	}

	return nil
}

// KillSession kills a tmux session.
func (s *TmuxServer) KillSession(ctx context.Context, name string) error {
	// Validate session name to prevent command injection
	if err := ValidateSessionName(name); err != nil {
		return fmt.Errorf("invalid session name: %w", err)
	}
	// tmux kill-session -t <name> (= prefix for exact match)
	cmd := s.cmd(ctx, "kill-session", "-t", "="+name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to kill tmux session: %w: %s", err, string(output))
	}
	return nil
}

// SessionExists checks if a tmux session with the given name exists.
func (s *TmuxServer) SessionExists(ctx context.Context, name string) bool {
	// Validate session name to prevent command injection
	if err := ValidateSessionName(name); err != nil {
		return false
	}
	// tmux has-session -t <name> (= prefix for exact match)
	cmd := s.cmd(ctx, "has-session", "-t", "="+name)
	err := cmd.Run()
	return err == nil
}

// ListSessions returns a list of all tmux session names.
func (s *TmuxServer) ListSessions(ctx context.Context) ([]string, error) {
	// tmux list-sessions -F "#{session_name}"
	cmd := s.cmd(ctx, "list-sessions", "-F", "#{session_name}")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to list tmux sessions: %w", err)
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		return []string{}, nil
	}

	sessions := strings.Split(output, "\n")
	return sessions, nil
}

// SetOption sets a tmux option on a session.
func (s *TmuxServer) SetOption(ctx context.Context, sessionName, option, value string) error {
	// Validate session name to prevent command injection
	if err := ValidateSessionName(sessionName); err != nil {
		return fmt.Errorf("invalid session name: %w", err)
	}
	cmd := s.cmd(ctx, "set-option", "-t", sessionName, option, value)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set %s: %w: %s", option, err, string(output))
	}
	return nil
}

// ConfigureStatusBar sets the standard schmux status bar on a tmux session:
// process name on left, empty center, empty right.
func (s *TmuxServer) ConfigureStatusBar(ctx context.Context, sessionName string) {
	_ = s.SetOption(ctx, sessionName, "status-left", "#{pane_current_command} ")
	_ = s.SetOption(ctx, sessionName, "window-status-format", "")
	_ = s.SetOption(ctx, sessionName, "window-status-current-format", "")
	_ = s.SetOption(ctx, sessionName, "status-right", "")
}

// GetPanePID returns the PID of the first process in the tmux session's pane.
func (s *TmuxServer) GetPanePID(ctx context.Context, name string) (int, error) {
	// Validate session name to prevent command injection
	if err := ValidateSessionName(name); err != nil {
		return 0, fmt.Errorf("invalid session name: %w", err)
	}
	// tmux display-message -p -t <name> "#{pane_pid}"
	cmd := s.cmd(ctx, "display-message", "-p", "-t", name, "#{pane_pid}")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("failed to get pane PID: %w", err)
	}

	pidStr := strings.TrimSpace(stdout.String())
	var pid int
	if _, err := fmt.Sscanf(pidStr, "%d", &pid); err != nil {
		return 0, fmt.Errorf("failed to parse PID: %w", err)
	}

	return pid, nil
}

// GetPaneSize returns the width and height of the first pane in the given tmux session.
func (s *TmuxServer) GetPaneSize(ctx context.Context, name string) (width, height int, err error) {
	if err := ValidateSessionName(name); err != nil {
		return 0, 0, fmt.Errorf("invalid session name: %w", err)
	}
	cmd := s.cmd(ctx, "display-message", "-p", "-t", name, "#{pane_width} #{pane_height}")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return 0, 0, fmt.Errorf("failed to get pane size: %w", err)
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(stdout.String()), "%d %d", &width, &height); err != nil {
		return 0, 0, fmt.Errorf("failed to parse pane size: %w", err)
	}
	return width, height, nil
}

// GetAttachCommand returns the command to attach to a tmux session on this server's socket.
func (s *TmuxServer) GetAttachCommand(name string) string {
	// Validate session name to prevent command injection
	if err := ValidateSessionName(name); err != nil {
		return ""
	}
	return fmt.Sprintf("%s -L %s attach -t \"=%s\"", s.binary, s.socketName, name)
}

// CaptureOutput captures the current output of a tmux session, including full scrollback history.
func (s *TmuxServer) CaptureOutput(ctx context.Context, name string) (string, error) {
	// Validate session name to prevent command injection
	if err := ValidateSessionName(name); err != nil {
		return "", fmt.Errorf("invalid session name: %w", err)
	}
	// -e includes escape sequences for colors/attributes
	// -p outputs to stdout
	// -S - captures from the start of the scrollback buffer
	cmd := s.cmd(ctx, "capture-pane", "-e", "-p", "-S", "-", "-t", name)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to capture tmux output: %w", err)
	}

	return stdout.String(), nil
}

// CaptureLastLines captures the last N lines of the pane.
func (s *TmuxServer) CaptureLastLines(ctx context.Context, name string, lines int, includeEscapes bool) (string, error) {
	// Validate session name to prevent command injection
	if err := ValidateSessionName(name); err != nil {
		return "", fmt.Errorf("invalid session name: %w", err)
	}
	if lines <= 0 {
		return "", fmt.Errorf("invalid line count: %d", lines)
	}
	args := []string{"capture-pane"}
	if includeEscapes {
		args = append(args, "-e") // include escape sequences
	}
	args = append(args,
		"-p", // output to stdout
		"-S", fmt.Sprintf("-%d", lines),
		"-t", name, // target session/pane
	)

	cmd := s.cmd(ctx, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to capture tmux output: %w", err)
	}

	return stdout.String(), nil
}

// GetCursorState returns the cursor position and visibility for a session.
// Coordinates are 0-indexed.
func (s *TmuxServer) GetCursorState(ctx context.Context, sessionName string) (CursorState, error) {
	// Validate session name to prevent command injection
	if err := ValidateSessionName(sessionName); err != nil {
		return CursorState{}, fmt.Errorf("invalid session name: %w", err)
	}
	cmd := s.cmd(ctx, "display-message", "-p", "-t", sessionName,
		"#{cursor_x} #{cursor_y} #{cursor_flag} #{alternate_on} #{mouse_standard_flag} #{mouse_button_flag} #{mouse_any_flag} #{mouse_sgr_flag}")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return CursorState{}, fmt.Errorf("failed to get cursor state: %w", err)
	}

	parts := strings.Fields(strings.TrimSpace(stdout.String()))
	if len(parts) < 3 {
		return CursorState{}, fmt.Errorf("unexpected cursor state format: %q", stdout.String())
	}

	var cs CursorState
	_, err := fmt.Sscanf(parts[0], "%d", &cs.X)
	if err != nil {
		return CursorState{}, fmt.Errorf("failed to parse cursor_x: %w", err)
	}
	_, err = fmt.Sscanf(parts[1], "%d", &cs.Y)
	if err != nil {
		return CursorState{}, fmt.Errorf("failed to parse cursor_y: %w", err)
	}
	cs.Visible = parts[2] == "1"
	if len(parts) >= 4 {
		cs.AlternateOn = parts[3] == "1"
	}
	if len(parts) >= 8 {
		cs.MouseStandard = parts[4] == "1"
		cs.MouseButton = parts[5] == "1"
		cs.MouseAny = parts[6] == "1"
		cs.MouseSGR = parts[7] == "1"
	}
	return cs, nil
}

// RenameSession renames an existing tmux session.
func (s *TmuxServer) RenameSession(ctx context.Context, oldName, newName string) error {
	// Validate session names to prevent command injection
	if err := ValidateSessionName(oldName); err != nil {
		return fmt.Errorf("invalid old session name: %w", err)
	}
	if err := ValidateSessionName(newName); err != nil {
		return fmt.Errorf("invalid new session name: %w", err)
	}
	cmd := s.cmd(ctx, "rename-session", "-t", "="+oldName, newName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to rename tmux session: %w: %s", err, string(output))
	}
	return nil
}

// ShowEnvironment returns the tmux server's global environment as a map.
func (s *TmuxServer) ShowEnvironment(ctx context.Context) (map[string]string, error) {
	cmd := s.cmd(ctx, "show-environment", "-g")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("show-environment failed: %w", err)
	}

	env := make(map[string]string)
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-") {
			continue
		}
		if idx := strings.IndexByte(line, '='); idx >= 0 {
			env[line[:idx]] = line[idx+1:]
		}
	}
	return env, nil
}

// SetEnvironment sets a global environment variable on the tmux server.
func (s *TmuxServer) SetEnvironment(ctx context.Context, key, value string) error {
	cmd := s.cmd(ctx, "set-environment", "-g", key, value)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("set-environment %s failed: %w: %s", key, err, string(output))
	}
	return nil
}

// CursorState holds the cursor position, visibility, and terminal mode state for a session.
type CursorState struct {
	X             int
	Y             int
	Visible       bool
	AlternateOn   bool // true when the pane is in alternate screen mode (TUI apps)
	MouseStandard bool // mode 1000
	MouseButton   bool // mode 1002
	MouseAny      bool // mode 1003
	MouseSGR      bool // mode 1006
}

const (
	// MaxExtractedLines is the maximum number of lines to extract from terminal output.
	MaxExtractedLines = 40
)

// ExtractLatestResponse extracts the latest meaningful response from captured tmux lines.
func ExtractLatestResponse(lines []string) string {
	promptIdx := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		if IsPromptLine(lines[i]) {
			promptIdx = i
			break
		}
	}

	var response []string
	contentCount := 0
	for i := promptIdx - 1; i >= 0; i-- {
		text := strings.TrimSpace(lines[i])
		if text == "" {
			continue
		}
		if IsPromptLine(text) {
			continue
		}
		if IsSeparatorLine(text) {
			continue
		}
		if IsAgentStatusLine(text) {
			continue
		}

		response = append([]string{text}, response...)
		contentCount++
		if contentCount >= MaxExtractedLines {
			break
		}
	}

	choices := extractChoiceLines(lines, promptIdx)
	if len(choices) > 0 {
		response = append(response, choices...)
	}

	return strings.Join(response, "\n")
}

func extractChoiceLines(lines []string, promptIdx int) []string {
	if promptIdx < 0 || promptIdx >= len(lines) {
		return nil
	}

	// First, find the index of the first choice line
	firstChoiceIdx := -1
	var contextLines []string
	for i := promptIdx; i < len(lines); i++ {
		text := strings.TrimSpace(lines[i])
		if text == "" {
			continue
		}

		// If this is a choice line, we found our start
		if IsChoiceLine(text) {
			firstChoiceIdx = i
			break
		}

		// Collect short non-separator context lines before choices
		if len(text) < 100 && !IsSeparatorLine(text) {
			contextLines = append(contextLines, text)
		} else {
			// Reset if we hit a long line or separator
			contextLines = nil
		}
	}

	if firstChoiceIdx == -1 {
		return nil
	}

	// Now collect all consecutive choice lines
	var choices []string
	choices = append(choices, contextLines...)
	for i := firstChoiceIdx; i < len(lines); i++ {
		text := strings.TrimSpace(lines[i])
		if text == "" {
			break
		}
		if !IsChoiceLine(text) {
			break
		}
		choices = append(choices, text)
	}

	if len(choices) < 2 {
		return nil
	}

	return choices
}

// IsSeparatorLine returns true if the line is mostly repeated separator characters.
func IsSeparatorLine(text string) bool {
	if len(text) < 10 {
		return false
	}
	runes := []rune(text)
	// Check if 80%+ of the line is the same character (dashes, equals, etc.)
	firstChar := runes[0]
	count := 0
	for _, c := range runes {
		if c == firstChar {
			count++
		}
	}
	return float64(count)/float64(len(runes)) > 0.8
}

// IsPromptLine returns true if the line looks like a shell prompt.
func IsPromptLine(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "❯") || strings.HasPrefix(trimmed, "›")
}

// IsChoiceLine returns true if the line looks like a prompt choice entry.
func IsChoiceLine(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}

	if strings.HasPrefix(trimmed, "❯") || strings.HasPrefix(trimmed, "›") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "❯"), "›"))
	}

	if trimmed == "" {
		return false
	}

	dot := strings.IndexAny(trimmed, ".)")
	if dot <= 0 {
		return false
	}

	for _, r := range trimmed[:dot] {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

// IsAgentStatusLine returns true if the line looks like agent UI noise.
func IsAgentStatusLine(text string) bool {
	// Filter out Claude Code's vertical bar status lines (⎿)
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "⎿")
}
