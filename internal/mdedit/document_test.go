package mdedit

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/fsnotify/fsnotify"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// fakeConn records everything the document sends or does to it. Both the
// message and the close paths signal a channel so tests await the transition
// (docs/testing.md rule 1) instead of polling.
type fakeConn struct {
	mu       sync.Mutex
	sent     []contracts.MarkdownDocument
	closed   []CloseReason
	gotMsg   chan contracts.MarkdownDocument
	gotClose chan CloseReason
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		gotMsg:   make(chan contracts.MarkdownDocument, 16),
		gotClose: make(chan CloseReason, 16),
	}
}

func (c *fakeConn) Send(msg contracts.MarkdownDocument) error {
	c.mu.Lock()
	c.sent = append(c.sent, msg)
	c.mu.Unlock()
	c.gotMsg <- msg
	return nil
}

func (c *fakeConn) CloseWithReason(r CloseReason) {
	c.mu.Lock()
	c.closed = append(c.closed, r)
	c.mu.Unlock()
	c.gotClose <- r
}

func (c *fakeConn) closedWith() []CloseReason {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]CloseReason(nil), c.closed...)
}

// next awaits the next document message (rule 1: await the transition).
func (c *fakeConn) next(t *testing.T) contracts.MarkdownDocument {
	t.Helper()
	select {
	case m := <-c.gotMsg:
		return m
	case <-time.After(5 * time.Second): // deadline backstop only, never the assertion
		t.Fatalf("no document message within deadline; sent so far: %d", len(c.sent))
		return contracts.MarkdownDocument{}
	}
}

// fakeEvents is an injected EventSource; tests push events by hand.
type fakeEvents struct {
	events chan fsnotify.Event
	errs   chan error
	closed bool
}

func newFakeEvents() *fakeEvents {
	return &fakeEvents{events: make(chan fsnotify.Event, 16), errs: make(chan error, 1)}
}
func (f *fakeEvents) Events() <-chan fsnotify.Event { return f.events }
func (f *fakeEvents) Errors() <-chan error          { return f.errs }
func (f *fakeEvents) Close() error                  { f.closed = true; return nil }

// fakeTimer captures AfterFunc callbacks so tests fire coalescing explicitly.
// armed is signalled once per captured callback so tests can await it.
type fakeTimer struct {
	mu    sync.Mutex
	fns   []func()
	armed chan struct{}
}

func newFakeTimer() *fakeTimer { return &fakeTimer{armed: make(chan struct{}, 16)} }

func (f *fakeTimer) afterFunc(_ time.Duration, fn func()) *time.Timer {
	f.mu.Lock()
	f.fns = append(f.fns, fn)
	f.mu.Unlock()
	f.armed <- struct{}{}
	return time.NewTimer(time.Hour) // never fires on its own
}

func (f *fakeTimer) fire() {
	f.mu.Lock()
	fns := f.fns
	f.fns = nil
	f.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

type harness struct {
	dir    string
	path   string
	events *fakeEvents
	timer  *fakeTimer
	doc    *Document
}

func newHarness(t *testing.T, content string) *harness {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &harness{dir: dir, path: path, events: newFakeEvents(), timer: newFakeTimer()}
	h.doc = NewDocument(Options{
		Path:      path,
		Validate:  func() error { return nil },
		Events:    func(string) (EventSource, error) { return h.events, nil },
		AfterFunc: h.timer.afterFunc,
		Logger:    log.NewWithOptions(io.Discard, log.Options{}),
	})
	t.Cleanup(h.doc.Close)
	return h
}

func TestSubscribeSendsInitialDocument(t *testing.T) {
	h := newHarness(t, "# hi\n")
	c := newFakeConn()
	if err := h.doc.Subscribe(c); err != nil {
		t.Fatal(err)
	}
	msg := c.next(t)
	if msg.Type != "document" || msg.Content != "# hi\n" || msg.Reply != "" {
		t.Fatalf("unexpected first message: %+v", msg)
	}
	if msg.Revision != Revision([]byte("# hi\n")) {
		t.Fatalf("revision = %q", msg.Revision)
	}
}

func TestSubscribeRefusesTooLarge(t *testing.T) {
	big := make([]byte, MaxDocumentBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	h := newHarness(t, string(big))
	c := newFakeConn()
	err := h.doc.Subscribe(c)
	var ce *CloseError
	if !errors.As(err, &ce) || ce.Reason != ReasonTooLarge {
		t.Fatalf("err = %v, want CloseError{too_large}", err)
	}
	if got, _ := os.ReadFile(h.path); len(got) != MaxDocumentBytes+1 {
		t.Fatalf("file was modified: %d bytes", len(got))
	}
}

func TestSubscribeRefusesInvalidUTF8(t *testing.T) {
	h := newHarness(t, "ok\xff\xfe\n")
	c := newFakeConn()
	err := h.doc.Subscribe(c)
	var ce *CloseError
	if !errors.As(err, &ce) || ce.Reason != ReasonNotUTF8 {
		t.Fatalf("err = %v, want CloseError{not_utf8}", err)
	}
}

func TestSubscribeRefusesMissingFile(t *testing.T) {
	h := newHarness(t, "x\n")
	if err := os.Remove(h.path); err != nil {
		t.Fatal(err)
	}
	c := newFakeConn()
	err := h.doc.Subscribe(c)
	var ce *CloseError
	if !errors.As(err, &ce) || ce.Reason != ReasonDeleted {
		t.Fatalf("err = %v, want CloseError{deleted}", err)
	}
}

func TestWatchIsRegisteredBeforeInitialRead(t *testing.T) {
	h := newHarness(t, "x\n")
	registered := false
	h.doc = NewDocument(Options{
		Path:     h.path,
		Validate: func() error { return nil },
		Events: func(string) (EventSource, error) {
			registered = true
			return h.events, nil
		},
		AfterFunc: h.timer.afterFunc,
		Logger:    log.NewWithOptions(io.Discard, log.Options{}),
	})
	t.Cleanup(h.doc.Close)
	// A read function that observes whether the watch exists when it runs.
	h.doc.readFile = func(p string) ([]byte, os.FileMode, error) {
		if !registered {
			t.Fatal("file read before watch registration")
		}
		b, err := os.ReadFile(p)
		return b, 0o644, err
	}
	if err := h.doc.Subscribe(newFakeConn()); err != nil {
		t.Fatal(err)
	}
}

func TestUnsubscribeLastClosesWatch(t *testing.T) {
	h := newHarness(t, "x\n")
	a, b := newFakeConn(), newFakeConn()
	if err := h.doc.Subscribe(a); err != nil {
		t.Fatal(err)
	}
	if err := h.doc.Subscribe(b); err != nil {
		t.Fatal(err)
	}
	if empty := h.doc.Unsubscribe(a); empty {
		t.Fatal("document reported empty with one subscriber left")
	}
	if empty := h.doc.Unsubscribe(b); !empty {
		t.Fatal("document did not report empty after last unsubscribe")
	}
	h.doc.Close()
	if !h.events.closed {
		t.Fatal("event source not closed")
	}
}

func subscribed(t *testing.T, h *harness) *fakeConn {
	t.Helper()
	c := newFakeConn()
	if err := h.doc.Subscribe(c); err != nil {
		t.Fatal(err)
	}
	c.next(t) // drain the initial document
	return c
}

func readDisk(t *testing.T, h *harness) string {
	t.Helper()
	b, err := os.ReadFile(h.path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSaveDirectWhenDiskMatchesBase(t *testing.T) {
	h := newHarness(t, "a\nb\n")
	c := subscribed(t, h)
	h.doc.Save(c, contracts.MarkdownSave{Type: "save", ID: "s1", Base: "a\nb\n", Draft: "a\nB\n"})
	msg := c.next(t)
	if msg.Reply != "s1" || msg.Content != "a\nB\n" {
		t.Fatalf("reply = %+v", msg)
	}
	if got := readDisk(t, h); got != "a\nB\n" {
		t.Fatalf("disk = %q", got)
	}
}

func TestSaveMergesWhenDiskMoved(t *testing.T) {
	h := newHarness(t, "a\nb\nc\n")
	c := subscribed(t, h)
	// Agent edits line c while the browser edits line a.
	if err := os.WriteFile(h.path, []byte("a\nb\nC\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.doc.Save(c, contracts.MarkdownSave{Type: "save", ID: "s1", Base: "a\nb\nc\n", Draft: "A\nb\nc\n"})
	msg := c.next(t)
	if msg.Content != "A\nb\nC\n" {
		t.Fatalf("merged = %q", msg.Content)
	}
	if got := readDisk(t, h); got != "A\nb\nC\n" {
		t.Fatalf("disk = %q", got)
	}
}

func TestSaveNoopDraftDoesNotRewrite(t *testing.T) {
	h := newHarness(t, "same\n")
	c := subscribed(t, h)
	before, err := os.Stat(h.path)
	if err != nil {
		t.Fatal(err)
	}
	h.doc.Save(c, contracts.MarkdownSave{Type: "save", ID: "s1", Base: "same\n", Draft: "same\n"})
	msg := c.next(t)
	if msg.Reply != "s1" || msg.Content != "same\n" {
		t.Fatalf("reply = %+v", msg)
	}
	after, err := os.Stat(h.path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("file was rewritten for a no-op save")
	}
}

func TestSaveRepeatedIDIsNotAppliedTwice(t *testing.T) {
	h := newHarness(t, "The cat sat.\n")
	c := subscribed(t, h)
	save := contracts.MarkdownSave{Type: "save", ID: "s1", Base: "The cat sat.\n", Draft: "The big cat sat.\n"}
	h.doc.Save(c, save)
	c.next(t)
	h.doc.Save(c, save) // resend after a dropped socket
	msg := c.next(t)
	if msg.Reply != "s1" || msg.Content != "The big cat sat.\n" {
		t.Fatalf("second reply = %+v", msg)
	}
	if got := readDisk(t, h); got != "The big cat sat.\n" {
		t.Fatalf("disk = %q (duplicated insertion)", got)
	}
}

func TestSavePreservesMode(t *testing.T) {
	h := newHarness(t, "x\n")
	if err := os.Chmod(h.path, 0o600); err != nil {
		t.Fatal(err)
	}
	c := subscribed(t, h)
	h.doc.Save(c, contracts.MarkdownSave{Type: "save", ID: "s1", Base: "x\n", Draft: "y\n"})
	c.next(t)
	info, err := os.Stat(h.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestSaveBroadcastsToOtherSubscriberWithoutReply(t *testing.T) {
	h := newHarness(t, "x\n")
	a := subscribed(t, h)
	b := subscribed(t, h)
	h.doc.Save(a, contracts.MarkdownSave{Type: "save", ID: "s1", Base: "x\n", Draft: "y\n"})
	ma, mb := a.next(t), b.next(t)
	if ma.Reply != "s1" || mb.Reply != "" {
		t.Fatalf("reply a=%q b=%q", ma.Reply, mb.Reply)
	}
	if mb.Content != "y\n" {
		t.Fatalf("other subscriber got %q", mb.Content)
	}
}

func TestSaveClosesOnValidationFailure(t *testing.T) {
	h := newHarness(t, "x\n")
	c := subscribed(t, h)
	h.doc.validate = func() error { return errors.New("symlink appeared") }
	h.doc.Save(c, contracts.MarkdownSave{Type: "save", ID: "s1", Base: "x\n", Draft: "y\n"})
	if got := c.closedWith(); len(got) != 1 || got[0] != ReasonInvalidPath {
		t.Fatalf("closed with %v", got)
	}
	if got := readDisk(t, h); got != "x\n" {
		t.Fatalf("disk changed: %q", got)
	}
}

func TestSaveRejectsOversizedFields(t *testing.T) {
	h := newHarness(t, "x\n")
	c := subscribed(t, h)
	big := make([]byte, MaxDocumentBytes+1)
	h.doc.Save(c, contracts.MarkdownSave{Type: "save", ID: "s1", Base: "x\n", Draft: string(big)})
	if got := c.closedWith(); len(got) != 1 || got[0] != ReasonBadRequest {
		t.Fatalf("closed with %v", got)
	}
}

func TestRejectClosesOnlyThatConn(t *testing.T) {
	h := newHarness(t, "x\n")
	a := subscribed(t, h)
	b := subscribed(t, h)
	h.doc.Reject(a, ReasonBadRequest)
	if got := a.closedWith(); len(got) != 1 || got[0] != ReasonBadRequest {
		t.Fatalf("a closed with %v", got)
	}
	if got := b.closedWith(); len(got) != 0 {
		t.Fatalf("b closed with %v", got)
	}
}

func TestExternalWriteIsPushedAfterCoalesce(t *testing.T) {
	h := newHarness(t, "v1\n")
	c := subscribed(t, h)
	if err := os.WriteFile(h.path, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.events.events <- fsnotify.Event{Name: h.path, Op: fsnotify.Write}
	// Second write before the coalesce timer fires: only the final content is pushed.
	if err := os.WriteFile(h.path, []byte("v3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.events.events <- fsnotify.Event{Name: h.path, Op: fsnotify.Write}
	waitForTimers(t, h.timer, 1)
	h.timer.fire()
	msg := c.next(t)
	if msg.Content != "v3\n" || msg.Reply != "" {
		t.Fatalf("pushed %+v", msg)
	}
	select {
	case extra := <-c.gotMsg:
		t.Fatalf("second push for a coalesced burst: %+v", extra)
	default:
	}
}

func TestOwnWriteEchoIsDropped(t *testing.T) {
	h := newHarness(t, "x\n")
	c := subscribed(t, h)
	h.doc.Save(c, contracts.MarkdownSave{Type: "save", ID: "s1", Base: "x\n", Draft: "y\n"})
	c.next(t)
	h.events.events <- fsnotify.Event{Name: h.path, Op: fsnotify.Create}
	h.events.events <- fsnotify.Event{Name: h.path, Op: fsnotify.Rename}
	waitForTimers(t, h.timer, 1)
	h.timer.fire()
	select {
	case extra := <-c.gotMsg:
		t.Fatalf("echo of own write was pushed: %+v", extra)
	case <-time.After(100 * time.Millisecond):
		// Negative claim (rule 4): a push would arrive synchronously from fire();
		// this window only guards against a goroutine hand-off.
	}
}

func TestOtherBasenameIsIgnored(t *testing.T) {
	h := newHarness(t, "x\n")
	subscribed(t, h)
	// Two events for other files, then one for ours. The loop handles events
	// in order, so when the timer for ours is armed, the others have already
	// been processed; exactly one timer proves they armed nothing.
	h.events.events <- fsnotify.Event{Name: filepath.Join(h.dir, "other.md"), Op: fsnotify.Write}
	h.events.events <- fsnotify.Event{Name: filepath.Join(h.dir, "other2.md"), Op: fsnotify.Write}
	h.events.events <- fsnotify.Event{Name: h.path, Op: fsnotify.Write}
	waitForTimers(t, h.timer, 1)
	h.timer.mu.Lock()
	n := len(h.timer.fns)
	h.timer.mu.Unlock()
	if n != 1 {
		t.Fatalf("timers armed = %d, want exactly 1", n)
	}
}

func TestDeleteClosesSubscribersWithDeleted(t *testing.T) {
	h := newHarness(t, "x\n")
	c := subscribed(t, h)
	if err := os.Remove(h.path); err != nil {
		t.Fatal(err)
	}
	h.events.events <- fsnotify.Event{Name: h.path, Op: fsnotify.Remove}
	waitForTimers(t, h.timer, 1)
	h.timer.fire()
	if got := c.closedWith(); len(got) != 1 || got[0] != ReasonDeleted {
		t.Fatalf("closed with %v", got)
	}
}

func TestWatcherErrorClosesWithWatcherError(t *testing.T) {
	h := newHarness(t, "x\n")
	c := subscribed(t, h)
	h.events.errs <- errors.New("kqueue exhausted")
	waitForClose(t, c)
	if got := c.closedWith(); got[0] != ReasonWatcherError {
		t.Fatalf("closed with %v", got)
	}
}

// waitForTimers awaits n coalesce-timer arms. The deadline is a failure
// backstop (rule 2), never the thing that makes the test pass.
func waitForTimers(t *testing.T, ft *fakeTimer, n int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case <-ft.armed:
		case <-deadline:
			t.Fatalf("only %d of %d timers armed before deadline", i, n)
		}
	}
}

// waitForClose awaits the close signal on c.
func waitForClose(t *testing.T, c *fakeConn) {
	t.Helper()
	select {
	case <-c.gotClose:
	case <-time.After(5 * time.Second):
		t.Fatal("connection never closed before deadline")
	}
}

func TestSecondSubscriberDoesNotSwallowPendingExternalUpdate(t *testing.T) {
	h := newHarness(t, "v1\n")
	a := subscribed(t, h)
	// Agent writes; the coalesce timer is armed but has not fired yet.
	if err := os.WriteFile(h.path, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.events.events <- fsnotify.Event{Name: h.path, Op: fsnotify.Write}
	waitForTimers(t, h.timer, 1)
	// A second tab opens the same file and reads v2 directly.
	b := subscribed(t, h)
	_ = b
	// The pending reread must not be mistaken for our own echo: A gets v2,
	// either from the subscribe path or from the timer, but it gets it.
	h.timer.fire()
	msg := a.next(t)
	if msg.Content != "v2\n" || msg.Reply != "" {
		t.Fatalf("first subscriber got %+v, want v2 external update", msg)
	}
}
