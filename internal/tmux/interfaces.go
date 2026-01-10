package tmux

import "context"

// TmuxService defines the interface for tmux operations.
type TmuxService interface {
	// CreateSession creates a new tmux session with the given name, directory, and command.
	CreateSession(ctx context.Context, name, dir, command string) error

	// KillSession kills a tmux session.
	KillSession(ctx context.Context, name string) error

	// ListSessions returns a list of all tmux session names.
	ListSessions(ctx context.Context) ([]string, error)

	// SessionExists checks if a tmux session with the given name exists.
	SessionExists(ctx context.Context, name string) bool

	// GetPanePID returns the PID of the first process in the tmux session's pane.
	GetPanePID(ctx context.Context, name string) (int, error)

	// CaptureOutput captures the current output of a tmux session.
	CaptureOutput(ctx context.Context, name string) (string, error)

	// CaptureLastLines captures the last N lines of the pane.
	CaptureLastLines(ctx context.Context, name string, lines int) (string, error)

	// SendKeys sends keys to a tmux session.
	SendKeys(ctx context.Context, name, keys string) error

	// GetAttachCommand returns the command to attach to a tmux session.
	GetAttachCommand(name string) string

	// SetWindowSizeManual forces tmux to ignore client resize requests.
	SetWindowSizeManual(ctx context.Context, sessionName string) error

	// ResizeWindow resizes the window to fixed dimensions.
	ResizeWindow(ctx context.Context, sessionName string, width, height int) error

	// StartPipePane begins streaming pane output to a log file.
	StartPipePane(ctx context.Context, sessionName, logPath string) error

	// StopPipePane stops streaming pane output.
	StopPipePane(ctx context.Context, sessionName string) error

	// IsPipePaneActive checks if pipe-pane is running for a session.
	IsPipePaneActive(ctx context.Context, sessionName string) bool

	// RenameSession renames an existing tmux session.
	RenameSession(ctx context.Context, oldName, newName string) error
}

// Ensure the package functions implement TmuxService via a type adapter.
// The concrete implementation is provided by the package-level functions.
type tmuxService struct{}

// NewTmuxService creates a new TmuxService backed by the package-level functions.
func NewTmuxService() TmuxService {
	return &tmuxService{}
}

func (t *tmuxService) CreateSession(ctx context.Context, name, dir, command string) error {
	return CreateSession(ctx, name, dir, command)
}

func (t *tmuxService) KillSession(ctx context.Context, name string) error {
	return KillSession(ctx, name)
}

func (t *tmuxService) ListSessions(ctx context.Context) ([]string, error) {
	return ListSessions(ctx)
}

func (t *tmuxService) SessionExists(ctx context.Context, name string) bool {
	return SessionExists(ctx, name)
}

func (t *tmuxService) GetPanePID(ctx context.Context, name string) (int, error) {
	return GetPanePID(ctx, name)
}

func (t *tmuxService) CaptureOutput(ctx context.Context, name string) (string, error) {
	return CaptureOutput(ctx, name)
}

func (t *tmuxService) CaptureLastLines(ctx context.Context, name string, lines int) (string, error) {
	return CaptureLastLines(ctx, name, lines)
}

func (t *tmuxService) SendKeys(ctx context.Context, name, keys string) error {
	return SendKeys(ctx, name, keys)
}

func (t *tmuxService) GetAttachCommand(name string) string {
	return GetAttachCommand(name)
}

func (t *tmuxService) SetWindowSizeManual(ctx context.Context, sessionName string) error {
	return SetWindowSizeManual(ctx, sessionName)
}

func (t *tmuxService) ResizeWindow(ctx context.Context, sessionName string, width, height int) error {
	return ResizeWindow(ctx, sessionName, width, height)
}

func (t *tmuxService) StartPipePane(ctx context.Context, sessionName, logPath string) error {
	return StartPipePane(ctx, sessionName, logPath)
}

func (t *tmuxService) StopPipePane(ctx context.Context, sessionName string) error {
	return StopPipePane(ctx, sessionName)
}

func (t *tmuxService) IsPipePaneActive(ctx context.Context, sessionName string) bool {
	return IsPipePaneActive(ctx, sessionName)
}

func (t *tmuxService) RenameSession(ctx context.Context, oldName, newName string) error {
	return RenameSession(ctx, oldName, newName)
}
