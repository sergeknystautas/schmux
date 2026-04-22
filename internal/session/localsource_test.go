package session

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
)

func TestLocalSource_IsPermanentError_ClosesWithError(t *testing.T) {
	// Verify that isPermanentError correctly identifies permanent errors.
	// LocalSource.run() uses this to decide whether to emit SourceClosed with an error.
	tests := []struct {
		name      string
		err       error
		permanent bool
	}{
		{"can't find session", errors.New("can't find session: test"), true},
		{"no session found", errors.New("no session found: test"), true},
		{"transient", errors.New("connection refused"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPermanentError(tt.err); got != tt.permanent {
				t.Errorf("isPermanentError(%v) = %v, want %v", tt.err, got, tt.permanent)
			}
		})
	}
}

func TestLocalSource_ImplementsControlSource(t *testing.T) {
	source := NewLocalSource("s1", "tmux-s1", nil, nil)
	var _ ControlSource = source
}

func TestLocalSource_MethodsFailWhenNotAttached(t *testing.T) {
	source := NewLocalSource("s1", "tmux-s1", nil, nil)

	if _, err := source.SendKeys("abc"); err == nil {
		t.Error("SendKeys should fail when not attached")
	}
	if _, err := source.CaptureVisible(); err == nil {
		t.Error("CaptureVisible should fail when not attached")
	}
	if _, err := source.CaptureLines(10); err == nil {
		t.Error("CaptureLines should fail when not attached")
	}
	if _, err := source.GetCursorState(); err == nil {
		t.Error("GetCursorState should fail when not attached")
	}
	if err := source.Resize(80, 24); err == nil {
		t.Error("Resize should fail when not attached")
	}
}

func TestLocalSource_IsAttached(t *testing.T) {
	source := NewLocalSource("s1", "tmux-s1", nil, nil)
	if source.IsAttached() {
		t.Error("should not be attached before start")
	}
}

func TestLocalSource_SetTmuxSession(t *testing.T) {
	source := NewLocalSource("s1", "tmux-s1", nil, nil)
	source.SetTmuxSession("tmux-s2")

	source.mu.RLock()
	got := source.tmuxSession
	source.mu.RUnlock()

	if got != "tmux-s2" {
		t.Errorf("tmuxSession = %q, want %q", got, "tmux-s2")
	}
}

// stubExecWriter wraps a strings.Builder so commands written to stdin are
// echoed back as a successful %begin/%end response carrying `respBody`. The
// command-id counter mirrors tmux's monotonic numbering.
type stubExecWriter struct {
	mu       sync.Mutex
	buf      strings.Builder
	pipeIn   io.Writer
	respBody string
	cmdCount int
}

func (w *stubExecWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buf.Write(p)
	if err == nil {
		// Each newline-terminated chunk is one command; emit one response.
		for _, b := range p {
			if b == '\n' {
				id := w.cmdCount
				w.cmdCount++
				resp := "%begin 1 " + itoa(id) + " 0\n" + w.respBody + "\n%end 1 " + itoa(id) + " 0\n"
				_, _ = w.pipeIn.Write([]byte(resp))
			}
		}
	}
	return n, err
}

func itoa(i int) string {
	// strconv.Itoa avoided to keep the helper inline-tiny; this only handles
	// non-negative ints which is all command IDs ever are.
	if i == 0 {
		return "0"
	}
	var out []byte
	for i > 0 {
		out = append([]byte{byte('0' + i%10)}, out...)
		i /= 10
	}
	return string(out)
}

// newStubExecClient returns a control-mode Client whose Execute() always
// succeeds and returns respBody as the command output. Used to test
// fetchPasteBufferEvent without a live tmux process.
func newStubExecClient(t *testing.T, respBody string) (*controlmode.Client, func()) {
	t.Helper()
	pr, pw := io.Pipe()
	parser := controlmode.NewParser(pr, nil)
	stdin := &stubExecWriter{pipeIn: pw, respBody: respBody}
	client := controlmode.NewClient(stdin, parser, nil)
	go parser.Run()
	client.Start()
	client.MarkSynced()
	cleanup := func() {
		client.Close()
		_ = pw.Close()
		_ = pr.Close()
	}
	return client, cleanup
}

// TestFetchPasteBufferEvent_HappyPath verifies a simple buffer fetch produces
// the expected SourcePasteBuffer event with defang stats.
func TestFetchPasteBufferEvent_HappyPath(t *testing.T) {
	client, cleanup := newStubExecClient(t, "hello world")
	defer cleanup()

	event, ok := fetchPasteBufferEvent(context.Background(), client, "buffer0", nil)
	if !ok {
		t.Fatal("expected ok=true for non-empty buffer")
	}
	if event.Type != SourcePasteBuffer {
		t.Errorf("Type = %v, want SourcePasteBuffer", event.Type)
	}
	if event.Data != "hello world" {
		t.Errorf("Data = %q, want %q", event.Data, "hello world")
	}
	if event.ByteCount != 11 {
		t.Errorf("ByteCount = %d, want 11", event.ByteCount)
	}
	if event.StrippedControlChars != 0 {
		t.Errorf("StrippedControlChars = %d, want 0", event.StrippedControlChars)
	}
}

// TestFetchPasteBufferEvent_DefangStripsControlChars verifies the shared defang
// helper is applied to the fetched buffer content (security parity with OSC 52).
func TestFetchPasteBufferEvent_DefangStripsControlChars(t *testing.T) {
	// 0x1b (ESC) and 0x07 (BEL) must be stripped, \n and \t preserved.
	body := "a\nb\x1bc\x07d"
	client, cleanup := newStubExecClient(t, body)
	defer cleanup()

	event, ok := fetchPasteBufferEvent(context.Background(), client, "buffer0", nil)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if event.Data != "a\nbcd" {
		t.Errorf("Data = %q, want %q", event.Data, "a\nbcd")
	}
	if event.StrippedControlChars != 2 {
		t.Errorf("StrippedControlChars = %d, want 2", event.StrippedControlChars)
	}
	if event.ByteCount != len(body) {
		t.Errorf("ByteCount = %d, want %d", event.ByteCount, len(body))
	}
}

// TestFetchPasteBufferEvent_EmptyBuffer returns ok=false so the caller skips
// the emit.
func TestFetchPasteBufferEvent_EmptyBuffer(t *testing.T) {
	client, cleanup := newStubExecClient(t, "")
	defer cleanup()

	_, ok := fetchPasteBufferEvent(context.Background(), client, "buffer0", nil)
	if ok {
		t.Error("expected ok=false for empty buffer")
	}
}

// TestFetchPasteBufferEvent_NilClient is defensive — a connection that hasn't
// fully attached should be a silent no-op, not a panic.
func TestFetchPasteBufferEvent_NilClient(t *testing.T) {
	_, ok := fetchPasteBufferEvent(context.Background(), nil, "buffer0", nil)
	if ok {
		t.Error("expected ok=false when client is nil")
	}
}

// TestFetchPasteBufferEvent_OversizeRejected applies the same 64 KiB cap as
// the OSC 52 path. Use a payload of maxOSC52DecodedSize+1 bytes.
func TestFetchPasteBufferEvent_OversizeRejected(t *testing.T) {
	body := strings.Repeat("a", maxOSC52DecodedSize+1)
	client, cleanup := newStubExecClient(t, body)
	defer cleanup()

	_, ok := fetchPasteBufferEvent(context.Background(), client, "buffer0", nil)
	if ok {
		t.Error("expected ok=false for oversize buffer")
	}
}

// TestFetchPasteBufferEvent_FetchTimeout verifies the helper bounds the
// show-buffer call so a stuck tmux server can't wedge the source loop.
func TestFetchPasteBufferEvent_FetchTimeout(t *testing.T) {
	// Build a client whose parser never delivers any responses — Execute
	// will block on the response channel until the helper's internal
	// 2 s timeout fires. Use a tiny parent ctx to short-circuit; the helper
	// derives its own ctx.WithTimeout from the parent so cancelling the
	// parent also cancels the fetch.
	pr, pw := io.Pipe()
	parser := controlmode.NewParser(pr, nil)
	var stdin strings.Builder
	client := controlmode.NewClient(&stdin, parser, nil)
	go parser.Run()
	client.Start()
	client.MarkSynced()
	defer func() {
		client.Close()
		_ = pw.Close()
		_ = pr.Close()
	}()

	parent, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, ok := fetchPasteBufferEvent(parent, client, "buffer0", nil)
	elapsed := time.Since(start)
	if ok {
		t.Error("expected ok=false on fetch timeout")
	}
	// Sanity: the helper should respect the parent ctx and not wait the
	// full 2 s when the parent already cancelled.
	if elapsed > time.Second {
		t.Errorf("helper waited %v; expected to honour parent ctx (≪ 1 s)", elapsed)
	}
}
