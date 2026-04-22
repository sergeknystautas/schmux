package session

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/remote"
	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
)

// RemoteSource implements ControlSource for remote sessions by wrapping
// a remote.Connection. It subscribes to the connection's output events
// and translates them to SourceEvents.
type RemoteSource struct {
	conn        *remote.Connection
	paneID      string
	windowID    string
	events      chan SourceEvent
	stopCh      chan struct{}
	doneCh      chan struct{}
	healthProbe *TmuxHealthProbe
	logger      *log.Logger
}

// NewRemoteSource creates a RemoteSource for a remote pane.
func NewRemoteSource(conn *remote.Connection, paneID, windowID string) *RemoteSource {
	return &RemoteSource{
		conn:        conn,
		paneID:      paneID,
		windowID:    windowID,
		events:      make(chan SourceEvent, 1000),
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
		healthProbe: NewTmuxHealthProbe(),
	}
}

// SetLogger attaches a structured logger for diagnostics (e.g. show-buffer
// failures from the paste-buffer-changed listener). Optional — if unset,
// these warnings are silently dropped.
func (s *RemoteSource) SetLogger(logger *log.Logger) {
	s.logger = logger
}

func (s *RemoteSource) Events() <-chan SourceEvent { return s.events }

func (s *RemoteSource) SendKeys(keys string) (controlmode.SendKeysTimings, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.conn.SendKeys(ctx, s.paneID, keys)
}

func (s *RemoteSource) SendTmuxKeyName(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := s.conn.Client()
	if client == nil {
		return fmt.Errorf("not connected")
	}
	cmd := fmt.Sprintf("send-keys -t %s %s", s.windowID, name)
	_, _, err := client.Execute(ctx, cmd)
	return err
}

func (s *RemoteSource) IsAttached() bool {
	return s.conn != nil && s.conn.IsConnected()
}

// CaptureVisible returns the visible screen. Connection does not expose
// CapturePaneVisible directly, so we use CapturePaneLines with a
// standard terminal height as an approximation.
func (s *RemoteSource) CaptureVisible() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.conn.CapturePaneLines(ctx, s.paneID, 100)
}

func (s *RemoteSource) CaptureLines(n int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.conn.CapturePaneLines(ctx, s.paneID, n)
}

func (s *RemoteSource) GetCursorState() (controlmode.CursorState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.conn.GetCursorState(ctx, s.paneID)
}

func (s *RemoteSource) Resize(cols, rows int) error {
	if s.windowID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.conn.Client().ResizeWindow(ctx, s.windowID, cols, rows)
}

// GetHealthProbe returns the source's health probe.
func (s *RemoteSource) GetHealthProbe() *TmuxHealthProbe {
	return s.healthProbe
}

func (s *RemoteSource) Close() error {
	close(s.stopCh)
	<-s.doneCh
	return nil
}

// Start launches the event forwarding goroutine.
func (s *RemoteSource) Start() {
	go s.run()
}

func (s *RemoteSource) run() {
	defer close(s.doneCh)
	defer close(s.events)

	outputCh := s.conn.SubscribeOutput(s.paneID)
	defer s.conn.UnsubscribeOutput(s.paneID, outputCh)

	// Snapshot the control client once so the select below has a stable
	// PasteBuffers() channel reference. nil-safe: if the connection isn't
	// fully attached yet the case becomes a never-ready select branch.
	cmClient := s.conn.Client()
	var pasteBufferCh <-chan string
	if cmClient != nil {
		pasteBufferCh = cmClient.PasteBuffers()
	}

	probeStop := make(chan struct{})
	go func() {
		jitter := time.Duration(rand.Int63n(int64(healthProbeInterval)))
		select {
		case <-time.After(jitter):
		case <-probeStop:
			return
		case <-s.stopCh:
			return
		}

		ticker := time.NewTicker(healthProbeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), healthProbeTimeout)
				start := time.Now()
				_, _, err := s.conn.ExecuteHealthProbe(ctx)
				rttUs := float64(time.Since(start).Microseconds())
				cancel()
				s.healthProbe.Record(rttUs, err != nil)
			case <-probeStop:
				return
			case <-s.stopCh:
				return
			}
		}
	}()
	defer close(probeStop)

	for {
		select {
		case event, ok := <-outputCh:
			if !ok {
				s.emit(SourceEvent{Type: SourceClosed})
				return
			}
			s.emit(SourceEvent{Type: SourceOutput, Data: event.Data})
		case bufferName := <-pasteBufferCh:
			// Mirror LocalSource: TUIs that detect tmux control mode bypass
			// OSC 52 and write to the paste buffer instead. Fetch + defang +
			// emit so the dashboard surfaces a banner identical to the OSC
			// 52 path. Shared helper enforces the same 64 KiB cap and 2 s
			// fetch timeout as LocalSource.
			if event, ok := fetchPasteBufferEvent(context.Background(), cmClient, bufferName, s.logger); ok {
				s.emit(event)
			}
		case <-s.stopCh:
			s.emit(SourceEvent{Type: SourceClosed})
			return
		}
	}
}

func (s *RemoteSource) emit(e SourceEvent) {
	select {
	case s.events <- e:
	default:
	}
}
