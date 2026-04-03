package session

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
	"github.com/sergeknystautas/schmux/internal/tmux"
)

// LocalSource implements ControlSource for local tmux sessions.
// It owns the tmux control mode lifecycle including attachment,
// reconnection, health probes, pause-after handling, and output
// event translation.
type LocalSource struct {
	sessionID   string
	tmuxSession string
	logger      *log.Logger
	events      chan SourceEvent

	stopCh     chan struct{}
	stopCtx    context.Context
	stopCancel context.CancelFunc
	doneCh     chan struct{}

	mu       sync.RWMutex
	cmClient *controlmode.Client
	cmParser *controlmode.Parser
	cmCmd    *exec.Cmd
	cmStdin  io.WriteCloser
	paneID   string

	lastRetryLog time.Time
	hasAttached  bool // tracks whether at least one successful attachment occurred

	// SyncCheckEnabled controls whether pause-after flow control is enabled.
	SyncCheckEnabled bool

	// HealthProbe measures tmux control mode round-trip time.
	HealthProbe *TmuxHealthProbe

	// SyncTrigger is signaled when tmux pauses output delivery (pause-after).
	// Exposed so the tracker/WebSocket handler can perform an immediate sync.
	SyncTrigger chan struct{}
}

// NewLocalSource creates a LocalSource for the given tmux session.
func NewLocalSource(sessionID, tmuxSession string, logger *log.Logger) *LocalSource {
	stopCtx, stopCancel := context.WithCancel(context.Background())
	return &LocalSource{
		sessionID:   sessionID,
		tmuxSession: tmuxSession,
		logger:      logger,
		events:      make(chan SourceEvent, 1000),
		stopCh:      make(chan struct{}),
		stopCtx:     stopCtx,
		stopCancel:  stopCancel,
		doneCh:      make(chan struct{}),
		HealthProbe: NewTmuxHealthProbe(),
		SyncTrigger: make(chan struct{}, 1),
	}
}

func (s *LocalSource) Events() <-chan SourceEvent { return s.events }

func (s *LocalSource) SendKeys(keys string) (controlmode.SendKeysTimings, error) {
	s.mu.RLock()
	client := s.cmClient
	paneID := s.paneID
	s.mu.RUnlock()
	if client == nil {
		return controlmode.SendKeysTimings{}, fmt.Errorf("not attached")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return client.SendKeys(ctx, paneID, keys)
}

func (s *LocalSource) CaptureVisible() (string, error) {
	s.mu.RLock()
	client := s.cmClient
	paneID := s.paneID
	s.mu.RUnlock()
	if client == nil {
		return "", fmt.Errorf("not attached")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return client.CapturePaneVisible(ctx, paneID)
}

func (s *LocalSource) CaptureLines(n int) (string, error) {
	s.mu.RLock()
	client := s.cmClient
	paneID := s.paneID
	s.mu.RUnlock()
	if client == nil {
		return "", fmt.Errorf("not attached")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return client.CapturePaneLines(ctx, paneID, n)
}

func (s *LocalSource) GetCursorState() (controlmode.CursorState, error) {
	s.mu.RLock()
	client := s.cmClient
	paneID := s.paneID
	s.mu.RUnlock()
	if client == nil {
		return controlmode.CursorState{}, fmt.Errorf("not attached")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return client.GetCursorState(ctx, paneID)
}

func (s *LocalSource) Close() error {
	s.stopCancel()
	close(s.stopCh)
	s.closeControlMode()
	<-s.doneCh
	return nil
}

// Start launches the reconnection loop in a background goroutine.
func (s *LocalSource) Start() {
	go s.run()
}

// run is the reconnection loop. On permanent error it emits SourceClosed.
// On transient error it emits SourceGap (after first successful attachment)
// and retries.
func (s *LocalSource) run() {
	defer close(s.doneCh)
	defer close(s.events)

	for {
		select {
		case <-s.stopCh:
			s.emit(SourceEvent{Type: SourceClosed})
			return
		default:
		}

		err := s.attach()
		if err != nil && err != io.EOF {
			if isPermanentError(err) {
				if s.logger != nil {
					s.logger.Debug("stopping: tmux session no longer exists", "session", s.sessionID)
				}
				s.emit(SourceEvent{Type: SourceClosed, Err: err})
				return
			}

			now := time.Now()
			if s.shouldLogRetry(now) {
				if s.logger != nil {
					s.logger.Warn("control mode failed", "session", s.sessionID, "err", err)
				}
			}
		}

		if s.waitOrStop(trackerRestartDelay) {
			s.emit(SourceEvent{Type: SourceClosed})
			return
		}
	}
}

// attach runs a single control mode attachment lifecycle.
func (s *LocalSource) attach() error {
	s.mu.RLock()
	target := s.tmuxSession
	s.mu.RUnlock()

	ctx, cancel := context.WithCancel(s.stopCtx)
	defer cancel()

	// Start tmux in control mode
	cmd := exec.CommandContext(ctx, tmux.Binary(), "-C", "attach-session", "-t", "="+target)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("failed to start control mode: %w", err)
	}

	// Create parser and client
	parser := controlmode.NewParser(stdout, s.logger, s.sessionID)
	go parser.Run()
	client := controlmode.NewClient(stdin, parser, s.logger)
	client.Start()

	// Wait for control mode to be ready
	readyCtx, readyCancel := context.WithTimeout(ctx, 10*time.Second)
	select {
	case <-parser.ControlModeReady():
		readyCancel()
	case <-readyCtx.Done():
		readyCancel()
		stdin.Close()
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("control mode not ready within timeout")
	}

	// Synchronize the FIFO command queue
	const sentinel = "__SCHMUX_SYNC__"
	syncCtx, syncCancel := context.WithTimeout(ctx, 5*time.Second)
	for attempts := 0; attempts < 3; attempts++ {
		output, _, err := client.Execute(syncCtx, fmt.Sprintf("display-message -p '%s'", sentinel))
		if err != nil {
			syncCancel()
			stdin.Close()
			cmd.Process.Kill()
			cmd.Wait()
			return fmt.Errorf("control mode sync failed: %w", err)
		}
		if strings.TrimSpace(output) == sentinel {
			break
		}
	}
	syncCancel()

	// Discover pane ID
	paneID, err := s.discoverPaneID(ctx, client)
	if err != nil {
		stdin.Close()
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("failed to discover pane ID: %w", err)
	}

	// Store references
	s.mu.Lock()
	s.cmClient = client
	s.cmParser = parser
	s.cmCmd = cmd
	s.cmStdin = stdin
	s.paneID = paneID
	s.mu.Unlock()

	defer s.closeControlMode()

	// Emit gap event on reconnect (after first successful attachment)
	if s.hasAttached {
		snapshot, _ := client.CapturePaneVisible(ctx, paneID)
		s.emit(SourceEvent{
			Type:     SourceGap,
			Reason:   "control_mode_reconnect",
			Snapshot: snapshot,
		})
	}
	s.hasAttached = true

	// Enable pause-after if sync check is active
	if s.SyncCheckEnabled {
		pauseCtx, pauseCancel := context.WithTimeout(ctx, 5*time.Second)
		if err := client.EnablePauseAfter(pauseCtx, 1); err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to enable pause-after", "session", s.sessionID[:8], "err", err)
			}
		}
		pauseCancel()
	}

	// Health probe goroutine
	probeStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(healthProbeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				probeCtx, probeCancel := context.WithTimeout(context.Background(), healthProbeTimeout)
				start := time.Now()
				_, _, err := client.Execute(probeCtx, healthProbeCommand)
				rttUs := float64(time.Since(start).Microseconds())
				probeCancel()
				s.HealthProbe.Record(rttUs, err != nil)
			case <-probeStop:
				return
			case <-s.stopCh:
				return
			}
		}
	}()
	defer close(probeStop)

	// Subscribe to output from the control mode client
	outputCh := client.SubscribeOutput(paneID)
	defer client.UnsubscribeOutput(paneID, outputCh)

	// Main output event loop
	for {
		select {
		case event, ok := <-outputCh:
			if !ok {
				return io.EOF
			}
			s.emit(SourceEvent{Type: SourceOutput, Data: event.Data})

		case pausedPane := <-client.Pauses():
			if s.logger != nil {
				s.logger.Info("tmux paused output, triggering sync and continue",
					"session", s.sessionID[:8], "pane", pausedPane)
			}
			// Signal for immediate capture-pane sync
			select {
			case s.SyncTrigger <- struct{}{}:
			default:
			}
			// Resume output delivery
			contCtx, contCancel := context.WithTimeout(ctx, 2*time.Second)
			if err := client.ContinuePane(contCtx, pausedPane); err != nil && s.logger != nil {
				s.logger.Warn("failed to continue pane", "pane", pausedPane, "err", err)
			}
			contCancel()

		case <-s.stopCh:
			return io.EOF
		}
	}
}

// SetTmuxSession updates the target tmux session name.
func (s *LocalSource) SetTmuxSession(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tmuxSession = name
}

// PaneID returns the discovered pane ID (empty if not yet attached).
func (s *LocalSource) PaneID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.paneID
}

// IsAttached reports whether the source has an active control mode attachment.
func (s *LocalSource) IsAttached() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cmClient != nil
}

// Client returns the underlying control mode client (for diagnostics).
func (s *LocalSource) Client() *controlmode.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cmClient
}

// Parser returns the underlying control mode parser (for diagnostics).
func (s *LocalSource) Parser() *controlmode.Parser {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cmParser
}

// Resize resizes the tmux window via control mode.
// Implements ControlSource.Resize.
func (s *LocalSource) Resize(cols, rows int) error {
	s.mu.RLock()
	client := s.cmClient
	paneID := s.paneID
	s.mu.RUnlock()
	if client == nil {
		return fmt.Errorf("not attached")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return client.ResizeWindow(ctx, paneID, cols, rows)
}

func (s *LocalSource) emit(e SourceEvent) {
	select {
	case s.events <- e:
	default:
		// Drop if channel full — should not happen with 1000 buffer
	}
}

func (s *LocalSource) discoverPaneID(ctx context.Context, client *controlmode.Client) (string, error) {
	output, _, err := client.Execute(ctx, "list-panes -F '#{pane_id}'")
	if err != nil {
		return "", err
	}
	paneID := strings.TrimSpace(output)
	if paneID == "" {
		return "", fmt.Errorf("no pane found")
	}
	if idx := strings.Index(paneID, "\n"); idx > 0 {
		paneID = paneID[:idx]
	}
	if len(paneID) < 2 || paneID[0] != '%' {
		return "", fmt.Errorf("invalid pane ID format: %q", paneID)
	}
	return paneID, nil
}

func (s *LocalSource) closeControlMode() {
	s.mu.Lock()
	stdin := s.cmStdin
	cmd := s.cmCmd
	client := s.cmClient
	s.cmClient = nil
	s.cmParser = nil
	s.cmCmd = nil
	s.cmStdin = nil
	s.mu.Unlock()

	if client != nil {
		client.Close()
	}
	if stdin != nil {
		stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
		cmd.Wait()
	}
}

func (s *LocalSource) shouldLogRetry(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastRetryLog.IsZero() || now.Sub(s.lastRetryLog) >= trackerRetryLogInterval {
		s.lastRetryLog = now
		return true
	}
	return false
}

func (s *LocalSource) waitOrStop(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return false
	case <-s.stopCh:
		return true
	}
}
