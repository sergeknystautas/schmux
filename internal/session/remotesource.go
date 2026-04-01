package session

import (
	"context"
	"time"

	"github.com/sergeknystautas/schmux/internal/remote"
	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
)

// RemoteSource implements ControlSource for remote sessions by wrapping
// a remote.Connection. It subscribes to the connection's output events
// and translates them to SourceEvents.
type RemoteSource struct {
	conn   *remote.Connection
	paneID string
	events chan SourceEvent
	stopCh chan struct{}
	doneCh chan struct{}
}

// NewRemoteSource creates a RemoteSource for a remote pane.
func NewRemoteSource(conn *remote.Connection, paneID string) *RemoteSource {
	return &RemoteSource{
		conn:   conn,
		paneID: paneID,
		events: make(chan SourceEvent, 1000),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

func (s *RemoteSource) Events() <-chan SourceEvent { return s.events }

func (s *RemoteSource) SendKeys(keys string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.conn.SendKeys(ctx, s.paneID, keys)
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
	return s.conn.ResizePTY(uint16(cols), uint16(rows))
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

	for {
		select {
		case event, ok := <-outputCh:
			if !ok {
				// Connection's output channel closed — connection dropped.
				s.emit(SourceEvent{Type: SourceClosed})
				return
			}
			s.emit(SourceEvent{Type: SourceOutput, Data: event.Data})

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
