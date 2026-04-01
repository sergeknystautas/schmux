package session

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
)

// MockControlSource implements ControlSource with a buffered Events() channel.
// Tests push events via Emit(), consumer drains them.
type MockControlSource struct {
	events chan SourceEvent
	closed bool
}

func NewMockControlSource(bufSize int) *MockControlSource {
	return &MockControlSource{events: make(chan SourceEvent, bufSize)}
}

func (m *MockControlSource) Events() <-chan SourceEvent         { return m.events }
func (m *MockControlSource) SendKeys(keys string) error         { return nil }
func (m *MockControlSource) CaptureVisible() (string, error)    { return "", nil }
func (m *MockControlSource) CaptureLines(n int) (string, error) { return "", nil }
func (m *MockControlSource) Resize(cols, rows int) error        { return nil }
func (m *MockControlSource) GetCursorState() (controlmode.CursorState, error) {
	return controlmode.CursorState{}, nil
}

func (m *MockControlSource) Close() error {
	if !m.closed {
		m.closed = true
		close(m.events)
	}
	return nil
}

func (m *MockControlSource) Emit(e SourceEvent) { m.events <- e }

func TestSourceEventTypes(t *testing.T) {
	// Verify the iota constants have the expected values.
	if SourceOutput != 0 {
		t.Errorf("SourceOutput = %d, want 0", SourceOutput)
	}
	if SourceGap != 1 {
		t.Errorf("SourceGap = %d, want 1", SourceGap)
	}
	if SourceResize != 2 {
		t.Errorf("SourceResize = %d, want 2", SourceResize)
	}
	if SourceClosed != 3 {
		t.Errorf("SourceClosed = %d, want 3", SourceClosed)
	}
}

func TestSourceEventConstruction(t *testing.T) {
	tests := []struct {
		name  string
		event SourceEvent
	}{
		{
			name:  "output event",
			event: SourceEvent{Type: SourceOutput, Data: "hello"},
		},
		{
			name:  "gap event",
			event: SourceEvent{Type: SourceGap, Reason: "reconnect", Snapshot: "$ "},
		},
		{
			name:  "resize event",
			event: SourceEvent{Type: SourceResize, Width: 80, Height: 24},
		},
		{
			name:  "closed event clean",
			event: SourceEvent{Type: SourceClosed, Err: nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify fields are accessible — this is a compilation + construction test.
			_ = tt.event.Type
			_ = tt.event.Data
			_ = tt.event.Reason
			_ = tt.event.Snapshot
			_ = tt.event.Width
			_ = tt.event.Height
			_ = tt.event.Err
		})
	}
}

func TestMockControlSource(t *testing.T) {
	mock := NewMockControlSource(10)

	// Verify it implements ControlSource.
	var _ ControlSource = mock

	// Emit and receive events.
	mock.Emit(SourceEvent{Type: SourceOutput, Data: "test"})
	mock.Emit(SourceEvent{Type: SourceGap, Reason: "reconnect"})

	event1 := <-mock.Events()
	if event1.Type != SourceOutput || event1.Data != "test" {
		t.Errorf("event1 = %+v, want SourceOutput with Data=test", event1)
	}

	event2 := <-mock.Events()
	if event2.Type != SourceGap || event2.Reason != "reconnect" {
		t.Errorf("event2 = %+v, want SourceGap with Reason=reconnect", event2)
	}

	// Close and verify channel is closed.
	if err := mock.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	_, ok := <-mock.Events()
	if ok {
		t.Error("Events() channel should be closed after Close()")
	}

	// Double close should not panic.
	if err := mock.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}
