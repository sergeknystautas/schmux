# Markdown Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the read-only local Markdown page into an autosaving editor whose edits are merged on the server with whatever agents write to the same file.

**Architecture:** One WebSocket per open local file carries the whole document each way. A server-side `Document` (package `internal/mdedit`) owns a parent-directory fsnotify watch, a mutex, and the last-written hash; every save is patched onto current disk with diff-match-patch and written atomically, and the result is pushed to every subscriber. In the browser a pure reducer owns base/draft/in-flight state, a hook binds it to the socket, and a thin ByteMD adapter renders it.

**Tech Stack:** Go 1.26, `github.com/sergi/go-diff` v1.4.0, `github.com/fsnotify/fsnotify` v1.9.0 (already present), gorilla/websocket (already present); React 19, Vite 7, `bytemd@1.22.0`, `@bytemd/react@1.22.0`, `@bytemd/plugin-gfm@1.22.0`; Vitest + React Testing Library; Playwright scenarios.

**Spec:** `docs/superpowers/specs/2026-09-23-markdown-editor-design.md`

## Global Constraints

- Local workspaces only: `state.Workspace.RemoteHostID == ""`.
- Extensions `.md` and `.mdx`, matched case-insensitively.
- Document limit 1 MiB; content must be valid UTF-8.
- Markdown socket read limit 4 MiB; the shared `wsReadLimit` (64 KB) is not changed.
- Autosave delay 500 ms from the last edit; one save in flight per tab.
- Reconnect backoff: start 2000 ms, double with jitter, cap 30000 ms (same as `useSessionsWebSocket`).
- Server messages: exactly one type each way (`save` in, `document` out). Everything else is a socket close with a reason.
- Close reasons: `deleted`, `not_utf8`, `too_large`, `invalid_path`, `write_failed`, `watcher_error`, `bad_request`. Terminal on the client: `too_large`, `not_utf8`, `invalid_path`, `bad_request`.
- Applied save ids retained per document: last 64, in memory.
- No conflict state, no conflict markers, no recovery storage, no workspace-lock coordination, no git commands.
- Logs never contain document content.
- Never run `npm install`, `npm run build`, `vite build`, or `npx vitest` directly. Build with `go run ./cmd/build-dashboard`; test with `./test.sh --quick` while iterating and `./test.sh` before claiming done.
- Never edit `assets/dashboard/src/lib/types.generated.ts`; edit `internal/api/contracts/` and run `go run ./cmd/gen-types`.
- Go builds and `./badcode.sh` run with `-mod=vendor`; after changing `go.mod`, run `go mod vendor` (the `vendor/` directory is gitignored).
- All tests follow `docs/testing.md`: injected clocks, awaited transitions, no sleeps, no retried assertions.
- Dashboard CSS uses tokens only, in a CSS module for page-specific rules; no bare `button`/`input`/`select`/`textarea` selectors.
- Commits: the user owns git. Do not run `git commit`. At the end of each task, report it done and wait for the user to run `/commit`.

## Review Focus

1. **A file whose content is exactly 1 MiB plus one byte** must be refused with `too_large` and rendered read-only, not truncated or edited. Test in Task 3.
2. **A draft that is byte-identical to base** (the browser sends a save after a no-op edit) must not rewrite the file or bump `lastRevision`. Test in Task 4.
3. **An agent write that lands between two coalesced watcher events** must produce one document push with the final content, never a stale intermediate. Test in Task 5.
4. **A file path with a percent sign or space** must round-trip through the route the same way `/api/file` does. Test in Task 7.
5. **A reconnect while a save is in flight** must resend the same id and must not produce a duplicate insertion. Test in Task 9.

---

## File Structure

**Server**

- Create `internal/mdedit/merge.go` — `Merge(base, draft, disk string) (string, int)`.
- Create `internal/mdedit/document.go` — `Document`, `Conn`, `CloseReason`, `EventSource`, subscribe/save/watch.
- Create `internal/mdedit/hub.go` — `Hub`.
- Create `internal/mdedit/fsnotify.go` — production `EventSource` over fsnotify.
- Create `internal/api/contracts/markdown.go` — `MarkdownSave`, `MarkdownDocument`.
- Modify `cmd/gen-types/main.go` — add the two root types.
- Modify `internal/dashboard/validation.go` — `splitWorkspaceFileParam`, `isMarkdownPath`, free `fileMatchesVCSIgnore`.
- Modify `internal/dashboard/handlers_diff.go` — use the two helpers.
- Create `internal/dashboard/websocket_markdown.go` — route handler and `wsConn` adapter.
- Modify `internal/dashboard/server.go` — `markdownHub` field, route, `Stop`/`CloseForTest` cleanup.

**Browser**

- Modify `assets/dashboard/package.json` — three ByteMD packages.
- Create `assets/dashboard/src/lib/markdownDocument.ts` — reducer.
- Create `assets/dashboard/src/hooks/useMarkdownDocument.ts` — socket + timer + backoff.
- Create `assets/dashboard/src/components/markdown/MarkdownEditor.tsx` — ByteMD adapter.
- Create `assets/dashboard/src/components/markdown/MarkdownViewer.tsx` — today's react-markdown rendering, extracted.
- Create `assets/dashboard/src/styles/markdownEditor.module.css`.
- Modify `assets/dashboard/src/routes/MarkdownPreviewPage.tsx` — controller.

**Tests and docs**

- Go tests beside each Go file; Vitest tests beside each TS file.
- Create `test/scenarios/markdown-editor.md`; generate its spec.
- Modify `docs/api.md`, `docs/web.md`, `docs/react.md`.

---

### Task 1: Merge function on go-diff

**Files:**

- Modify: `go.mod`, `go.sum`
- Create: `internal/mdedit/merge.go`
- Test: `internal/mdedit/merge_test.go`

**Interfaces:**

- Produces: `func Merge(base, draft, disk string) (out string, dropped int)`.

- [ ] **Step 1: Add the dependency**

Run from the repo root:

```bash
go get github.com/sergi/go-diff@v1.4.0
go mod vendor
```

Expected: `go.mod` gains `github.com/sergi/go-diff v1.4.0`; `vendor/modules.txt` lists it.

- [ ] **Step 2: Write the failing test**

Create `internal/mdedit/merge_test.go`:

```go
package mdedit

import "testing"

func TestMerge(t *testing.T) {
	tests := []struct {
		name        string
		base, draft string
		disk        string
		want        string
		wantDropped int
	}{
		{"browser unchanged, disk wins", "x\n", "x\n", "y\n", "y\n", 0},
		{"no agent change", "a\nb\nc\n", "a\nB\nc\n", "a\nb\nc\n", "a\nB\nc\n", 0},
		{"different lines", "a\nb\nc\n", "A\nb\nc\n", "a\nb\nC\n", "A\nb\nC\n", 0},
		{"same line, different spots",
			"The cat sat on the mat.\n", "The big cat sat on the mat.\n", "The cat sat on the rug.\n",
			"The big cat sat on the rug.\n", 0},
		{"identical replacement on both sides", "one\ntwo\nthree\n", "one\n2\nthree\n", "one\n2\nthree\n", "one\n2\nthree\n", 0},
		{"agent inserted lines above", "alpha\nbeta\ngamma\n", "alpha\nbeta!\ngamma\n", "intro\nintro2\nalpha\nbeta\ngamma\n",
			"intro\nintro2\nalpha\nbeta!\ngamma\n", 0},
		{"agent rewrote the region, fuzzy match lands the edit",
			"# T\n\nold paragraph text here\n", "# T\n\nold paragraph text here, plus\n", "# T\n\nCompletely different content now\n",
			"# T\n\nCompletely different content now, plus\n", 0},
		{"insertion already present is applied again (why save ids exist)",
			"# Title\n\nThe cat sat on the mat.\n\nSecond paragraph here.\n",
			"# Title\n\nThe big cat sat on the mat.\n\nSecond paragraph here.\n",
			"# Title\n\nThe big cat sat on the mat.\n\nSecond paragraph here, edited by agent.\n",
			"# Title\n\nThe big big cat sat on the mat.\n\nSecond paragraph here, edited by agent.\n", 0},
		{"no context anywhere: hunk dropped, disk unchanged",
			"aaaa bbbb cccc dddd\n", "aaaa bbbb XXXX cccc dddd\n", "completely unrelated text of a different nature\n",
			"completely unrelated text of a different nature\n", 1},
		{"empty base", "", "hello\n", "agent\n", "hello\nagent\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, dropped := Merge(tt.base, tt.draft, tt.disk)
			if got != tt.want {
				t.Fatalf("Merge() = %q, want %q", got, tt.want)
			}
			if dropped != tt.wantDropped {
				t.Fatalf("dropped = %d, want %d", dropped, tt.wantDropped)
			}
		})
	}
}
```

Every `want` above was produced by running go-diff v1.4.0 on those inputs; they are the library's behavior, not aspirations.

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/mdedit/ -run TestMerge -v`
Expected: FAIL, `undefined: Merge`.

- [ ] **Step 4: Implement**

Create `internal/mdedit/merge.go`:

```go
// Package mdedit serves the dashboard's Markdown editor: one Document per open
// file, merging browser saves onto whatever agents have written to disk.
package mdedit

import "github.com/sergi/go-diff/diffmatchpatch"

// Merge applies the change from base to draft onto disk and returns the result
// plus the number of hunks that found no context to attach to. It never
// reports a conflict: overlapping edits land at diff-match-patch's best fuzzy
// match, and a hunk with no match within the default MatchThreshold is
// dropped. When the browser changed nothing, disk is returned as is.
func Merge(base, draft, disk string) (string, int) {
	if base == draft {
		return disk, 0
	}
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(base, draft, false)
	patches := dmp.PatchMake(base, diffs)
	out, applied := dmp.PatchApply(patches, disk)
	dropped := 0
	for _, ok := range applied {
		if !ok {
			dropped++
		}
	}
	return out, dropped
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/mdedit/ -run TestMerge -v`
Expected: PASS for all ten cases.

- [ ] **Step 6: Report done**

Tell the user Task 1 is complete and wait for `/commit`.

---

### Task 2: Wire protocol contracts

**Files:**

- Create: `internal/api/contracts/markdown.go`
- Modify: `cmd/gen-types/main.go:27-41`
- Generated: `assets/dashboard/src/lib/types.generated.ts`

**Interfaces:**

- Produces: `contracts.MarkdownSave{Type, ID, Base, Draft}`, `contracts.MarkdownDocument{Type, Content, Revision, Reply}`; TS `MarkdownSave`, `MarkdownDocument`.

- [ ] **Step 1: Write the structs**

Create `internal/api/contracts/markdown.go`:

```go
package contracts

// MarkdownSave is the only client → server frame on /ws/markdown/{workspaceId}/{path}.
// Base is the text the draft was edited from; Draft is the editor's text now.
// ID is client-generated and unique per save so a resend after a dropped
// socket is not applied twice.
type MarkdownSave struct {
	Type  string `json:"type"` // always "save"
	ID    string `json:"id"`
	Base  string `json:"base"`
	Draft string `json:"draft"`
}

// MarkdownDocument is the only server → client frame. Content is the whole
// file. Revision is "sha256:" plus the lowercase hex digest of Content's bytes.
// Reply carries the id of the save this message answers and is present only
// on the requester's copy of that message.
type MarkdownDocument struct {
	Type     string `json:"type"` // always "document"
	Content  string `json:"content"`
	Revision string `json:"revision"`
	Reply    string `json:"reply,omitempty"`
}
```

- [ ] **Step 2: Register with gen-types**

In `cmd/gen-types/main.go`, inside the `rootTypes` slice, after `reflect.TypeOf(contracts.BuildMonitorResponse{}),` add:

```go
		reflect.TypeOf(contracts.MarkdownSave{}),
		reflect.TypeOf(contracts.MarkdownDocument{}),
```

- [ ] **Step 3: Regenerate**

Run: `go run ./cmd/gen-types`
Expected: `assets/dashboard/src/lib/types.generated.ts` now contains `export interface MarkdownSave` and `export interface MarkdownDocument` with `reply?: string`.

- [ ] **Step 4: Verify the build**

Run: `go build ./... && go vet ./internal/api/... ./cmd/gen-types/`
Expected: no output.

- [ ] **Step 5: Report done**

---

### Task 3: Document subscribe, initial read, and close reasons

**Files:**

- Create: `internal/mdedit/document.go`
- Test: `internal/mdedit/document_test.go`

**Interfaces:**

- Produces:

```go
type CloseReason string
const (
	ReasonDeleted      CloseReason = "deleted"
	ReasonNotUTF8      CloseReason = "not_utf8"
	ReasonTooLarge     CloseReason = "too_large"
	ReasonInvalidPath  CloseReason = "invalid_path"
	ReasonWriteFailed  CloseReason = "write_failed"
	ReasonWatcherError CloseReason = "watcher_error"
	ReasonBadRequest   CloseReason = "bad_request"
)
type Conn interface {
	Send(msg contracts.MarkdownDocument) error
	CloseWithReason(reason CloseReason)
}
type EventSource interface {
	Events() <-chan fsnotify.Event
	Errors() <-chan error
	Close() error
}
type Options struct {
	Path      string                              // absolute path of the file
	Validate  func() error                        // re-run on every save; non-nil error closes with invalid_path
	Events    func(dir string) (EventSource, error) // nil → fsnotify on the parent directory
	AfterFunc func(time.Duration, func()) *time.Timer // nil → time.AfterFunc
	Coalesce  time.Duration                       // nil → 50ms
	Logger    *log.Logger
}
func NewDocument(opts Options) *Document
func (d *Document) Subscribe(c Conn) error   // sends the first document; returns *CloseError on refusal
func (d *Document) Unsubscribe(c Conn) (empty bool)
func (d *Document) Close()
type CloseError struct{ Reason CloseReason; Err error }
func Revision(b []byte) string               // "sha256:<hex>"
const MaxDocumentBytes = 1 << 20
```

- [ ] **Step 1: Write the failing tests**

Create `internal/mdedit/document_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/mdedit/ -run 'TestSubscribe|TestWatch|TestUnsubscribe' -v`
Expected: FAIL to compile, `undefined: NewDocument` and friends.

- [ ] **Step 3: Implement the document skeleton**

Create `internal/mdedit/document.go`:

```go
package mdedit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/log"
	"github.com/fsnotify/fsnotify"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/fileutil"
)

// MaxDocumentBytes is the largest file the editor opens or writes.
const MaxDocumentBytes = 1 << 20

// CloseReason is the reason string sent in a WebSocket close frame.
type CloseReason string

const (
	ReasonDeleted      CloseReason = "deleted"
	ReasonNotUTF8      CloseReason = "not_utf8"
	ReasonTooLarge     CloseReason = "too_large"
	ReasonInvalidPath  CloseReason = "invalid_path"
	ReasonWriteFailed  CloseReason = "write_failed"
	ReasonWatcherError CloseReason = "watcher_error"
	ReasonBadRequest   CloseReason = "bad_request"
)

// CloseError is returned by Subscribe when the file cannot be opened for editing.
type CloseError struct {
	Reason CloseReason
	Err    error
}

func (e *CloseError) Error() string { return fmt.Sprintf("%s: %v", e.Reason, e.Err) }
func (e *CloseError) Unwrap() error { return e.Err }

// Conn is one subscribed WebSocket. The document is its only writer.
type Conn interface {
	Send(msg contracts.MarkdownDocument) error
	CloseWithReason(reason CloseReason)
}

// EventSource delivers filesystem events for the file's parent directory.
type EventSource interface {
	Events() <-chan fsnotify.Event
	Errors() <-chan error
	Close() error
}

// Options configures a Document. Zero values select production behavior.
type Options struct {
	Path      string
	Validate  func() error
	Events    func(dir string) (EventSource, error)
	AfterFunc func(time.Duration, func()) *time.Timer
	Coalesce  time.Duration
	Logger    *log.Logger
}

// Revision hashes exact bytes into the wire revision string.
func Revision(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Document is one open Markdown file shared by every socket that has it open.
type Document struct {
	path      string
	validate  func() error
	newEvents func(dir string) (EventSource, error)
	afterFunc func(time.Duration, func()) *time.Timer
	coalesce  time.Duration
	logger    *log.Logger

	// readFile is swappable in tests to observe ordering.
	readFile func(path string) ([]byte, os.FileMode, error)

	mu           sync.Mutex
	subscribers  map[Conn]struct{}
	lastRevision string
	appliedIDs   []string // most recent last; bounded to maxAppliedIDs
	events       EventSource
	stop         chan struct{}
	loopDone     chan struct{}
	pending      *time.Timer
	closed       bool
}

const maxAppliedIDs = 64

// NewDocument builds a document for path. Nothing touches the filesystem
// until the first Subscribe.
func NewDocument(opts Options) *Document {
	d := &Document{
		path:        opts.Path,
		validate:    opts.Validate,
		newEvents:   opts.Events,
		afterFunc:   opts.AfterFunc,
		coalesce:    opts.Coalesce,
		logger:      opts.Logger,
		subscribers: map[Conn]struct{}{},
		stop:        make(chan struct{}),
		loopDone:    make(chan struct{}),
	}
	if d.validate == nil {
		d.validate = func() error { return nil }
	}
	if d.newEvents == nil {
		d.newEvents = newFsnotifySource
	}
	if d.afterFunc == nil {
		d.afterFunc = time.AfterFunc
	}
	if d.coalesce == 0 {
		d.coalesce = 50 * time.Millisecond
	}
	if d.logger == nil {
		d.logger = log.New(os.Stderr)
	}
	d.readFile = readRegularFile
	return d
}

func readRegularFile(path string) ([]byte, os.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("not a regular file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	return b, info.Mode().Perm(), nil
}

// checkContent applies the size and encoding limits to bytes read from disk.
func checkContent(b []byte) *CloseError {
	if len(b) > MaxDocumentBytes {
		return &CloseError{Reason: ReasonTooLarge, Err: fmt.Errorf("%d bytes", len(b))}
	}
	if !utf8.Valid(b) {
		return &CloseError{Reason: ReasonNotUTF8, Err: errors.New("invalid utf-8")}
	}
	return nil
}

// Subscribe adds c and sends it the current document. The first subscriber
// registers the directory watch before the first read, so no write can fall
// between them unobserved.
func (d *Document) Subscribe(c Conn) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return &CloseError{Reason: ReasonWatcherError, Err: errors.New("document closed")}
	}
	if d.events == nil {
		src, err := d.newEvents(filepath.Dir(d.path))
		if err != nil {
			return &CloseError{Reason: ReasonWatcherError, Err: err}
		}
		d.events = src
		go d.loop()
	}
	b, _, err := d.readFile(d.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &CloseError{Reason: ReasonDeleted, Err: err}
		}
		return &CloseError{Reason: ReasonInvalidPath, Err: err}
	}
	if ce := checkContent(b); ce != nil {
		return ce
	}
	d.lastRevision = Revision(b)
	d.subscribers[c] = struct{}{}
	d.sendLocked(c, contracts.MarkdownDocument{Type: "document", Content: string(b), Revision: d.lastRevision})
	return nil
}

// Unsubscribe removes c and reports whether the document is now empty.
func (d *Document) Unsubscribe(c Conn) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.subscribers, c)
	return len(d.subscribers) == 0
}

// Close stops the watcher loop and waits for it. Safe to call twice.
func (d *Document) Close() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	if d.pending != nil {
		d.pending.Stop()
	}
	started := d.events != nil
	if started {
		_ = d.events.Close()
	}
	close(d.stop)
	d.mu.Unlock()
	if started {
		<-d.loopDone
	}
}

// sendLocked writes to one subscriber; a failed write drops that subscriber.
func (d *Document) sendLocked(c Conn, msg contracts.MarkdownDocument) {
	if err := c.Send(msg); err != nil {
		delete(d.subscribers, c)
		c.CloseWithReason(ReasonWriteFailed)
	}
}

// broadcastLocked sends content to every subscriber; requester, if non-nil,
// gets the copy carrying reply.
func (d *Document) broadcastLocked(content, revision string, requester Conn, reply string) {
	for c := range d.subscribers {
		msg := contracts.MarkdownDocument{Type: "document", Content: content, Revision: revision}
		if c == requester {
			msg.Reply = reply
		}
		d.sendLocked(c, msg)
	}
}

// closeAllLocked closes every subscriber with reason and forgets them.
func (d *Document) closeAllLocked(reason CloseReason) {
	for c := range d.subscribers {
		c.CloseWithReason(reason)
		delete(d.subscribers, c)
	}
}

// loop is filled in by Task 5. Until then it only waits for stop.
func (d *Document) loop() {
	defer close(d.loopDone)
	<-d.stop
}

// writeAtomic is used by Save in Task 4.
func (d *Document) writeAtomic(b []byte, mode os.FileMode) error {
	return fileutil.AtomicWriteFile(d.path, b, mode)
}
```

Also create `internal/mdedit/fsnotify.go`:

```go
package mdedit

import "github.com/fsnotify/fsnotify"

type fsnotifySource struct{ w *fsnotify.Watcher }

// newFsnotifySource watches dir. The directory rather than the file is
// watched because atomic saves (schmux's own, sed -i, most editors) replace
// the inode, and a watch on the old inode would go silent.
func newFsnotifySource(dir string) (EventSource, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := w.Add(dir); err != nil {
		_ = w.Close()
		return nil, err
	}
	return &fsnotifySource{w: w}, nil
}

func (s *fsnotifySource) Events() <-chan fsnotify.Event { return s.w.Events }
func (s *fsnotifySource) Errors() <-chan error          { return s.w.Errors }
func (s *fsnotifySource) Close() error                  { return s.w.Close() }
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/mdedit/ -run 'TestSubscribe|TestWatch|TestUnsubscribe' -v`
Expected: PASS ×6.

- [ ] **Step 5: Report done**

---

### Task 4: Document save

**Files:**

- Modify: `internal/mdedit/document.go`
- Test: `internal/mdedit/document_test.go`

**Interfaces:**

- Produces: `func (d *Document) Save(c Conn, msg contracts.MarkdownSave)` and `func (d *Document) Reject(c Conn, reason CloseReason)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/mdedit/document_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/mdedit/ -run 'TestSave|TestReject' -v`
Expected: FAIL to compile, `d.Save undefined`.

- [ ] **Step 3: Implement Save and Reject**

Append to `internal/mdedit/document.go`:

```go
// Reject closes one connection with reason. The handler calls it for frames
// it cannot parse so the document stays the only writer to the socket.
func (d *Document) Reject(c Conn, reason CloseReason) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.subscribers, c)
	c.CloseWithReason(reason)
}

// Save applies one browser save under the document mutex:
//
//  0. a repeated id answers with the current document and stops;
//  1. the path is re-validated;
//  2. disk is read;
//  3. output is the draft when disk still equals base, else Merge();
//  4. output is written atomically with the file's mode;
//  5. lastRevision and the applied id are recorded;
//  6. every subscriber gets the document, the requester's copy with reply.
func (d *Document) Save(c Conn, msg contracts.MarkdownSave) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.subscribers[c]; !ok {
		return
	}
	if len(msg.Base) > MaxDocumentBytes || len(msg.Draft) > MaxDocumentBytes || msg.ID == "" {
		delete(d.subscribers, c)
		c.CloseWithReason(ReasonBadRequest)
		return
	}
	if d.hasApplied(msg.ID) {
		b, _, err := d.readFile(d.path)
		if err != nil {
			d.closeAllLocked(ReasonDeleted)
			return
		}
		d.sendLocked(c, contracts.MarkdownDocument{Type: "document", Content: string(b), Revision: Revision(b), Reply: msg.ID})
		return
	}
	if err := d.validate(); err != nil {
		d.logger.Warn("save rejected", "path", d.path, "err", err)
		delete(d.subscribers, c)
		c.CloseWithReason(ReasonInvalidPath)
		return
	}
	disk, mode, err := d.readFile(d.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			d.closeAllLocked(ReasonDeleted)
			return
		}
		delete(d.subscribers, c)
		c.CloseWithReason(ReasonInvalidPath)
		return
	}
	if ce := checkContent(disk); ce != nil {
		d.closeAllLocked(ce.Reason)
		return
	}

	var out string
	dropped := 0
	if Revision(disk) == Revision([]byte(msg.Base)) {
		out = msg.Draft
	} else {
		out, dropped = Merge(msg.Base, msg.Draft, string(disk))
	}
	if len(out) > MaxDocumentBytes {
		delete(d.subscribers, c)
		c.CloseWithReason(ReasonBadRequest)
		return
	}

	revision := Revision([]byte(out))
	if out != string(disk) {
		if err := d.writeAtomic([]byte(out), mode); err != nil {
			d.logger.Error("write failed", "path", d.path, "err", err)
			delete(d.subscribers, c)
			c.CloseWithReason(ReasonWriteFailed)
			return
		}
	}
	d.lastRevision = revision
	d.recordApplied(msg.ID)
	d.logger.Debug("saved", "path", d.path, "revision", revision, "bytes", len(out), "dropped_hunks", dropped)
	d.broadcastLocked(out, revision, c, msg.ID)
}

func (d *Document) hasApplied(id string) bool {
	for _, v := range d.appliedIDs {
		if v == id {
			return true
		}
	}
	return false
}

func (d *Document) recordApplied(id string) {
	d.appliedIDs = append(d.appliedIDs, id)
	if len(d.appliedIDs) > maxAppliedIDs {
		d.appliedIDs = d.appliedIDs[len(d.appliedIDs)-maxAppliedIDs:]
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/mdedit/ -v`
Expected: PASS, every test so far.

- [ ] **Step 5: Report done**

---

### Task 5: Watcher loop and Hub

**Files:**

- Modify: `internal/mdedit/document.go` (replace the placeholder `loop`)
- Create: `internal/mdedit/hub.go`
- Test: `internal/mdedit/document_test.go`, `internal/mdedit/hub_test.go`

**Interfaces:**

- Produces:

```go
type Hub struct{ ... }
func NewHub(logger *log.Logger) *Hub
func (h *Hub) Subscribe(path string, validate func() error, c Conn) (*Document, error)
func (h *Hub) Unsubscribe(path string, c Conn)
func (h *Hub) Close()
```

- [ ] **Step 1: Write the failing watcher tests**

Append to `internal/mdedit/document_test.go`:

```go
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
```

- [ ] **Step 2: Write the failing hub test**

Create `internal/mdedit/hub_test.go`:

```go
package mdedit

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/log"
)

func TestHubSharesDocumentPerPathAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.md")
	p2 := filepath.Join(dir, "b.md")
	for _, p := range []string{p1, p2} {
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hub := NewHub(log.NewWithOptions(io.Discard, log.Options{}))
	ok := func() error { return nil }
	a, b, c := newFakeConn(), newFakeConn(), newFakeConn()
	d1, err := hub.Subscribe(p1, ok, a)
	if err != nil {
		t.Fatal(err)
	}
	d1b, err := hub.Subscribe(p1, ok, b)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := hub.Subscribe(p2, ok, c)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d1b {
		t.Fatal("same path produced two documents")
	}
	if d1 == d2 {
		t.Fatal("different paths share a document")
	}
	if n := hub.count(); n != 2 {
		t.Fatalf("documents = %d, want 2", n)
	}
	hub.Unsubscribe(p1, a)
	if n := hub.count(); n != 2 {
		t.Fatalf("document removed with a subscriber left")
	}
	hub.Unsubscribe(p1, b)
	if n := hub.count(); n != 1 {
		t.Fatalf("documents = %d after last unsubscribe, want 1", n)
	}
	hub.Close()
	if n := hub.count(); n != 0 {
		t.Fatalf("documents = %d after Close, want 0", n)
	}
}

func TestHubSubscribeRefusalLeavesNoDocument(t *testing.T) {
	hub := NewHub(log.NewWithOptions(io.Discard, log.Options{}))
	_, err := hub.Subscribe(filepath.Join(t.TempDir(), "missing.md"), func() error { return nil }, newFakeConn())
	if err == nil {
		t.Fatal("expected refusal for a missing file")
	}
	if n := hub.count(); n != 0 {
		t.Fatalf("documents = %d after refusal, want 0", n)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/mdedit/ -run 'TestExternal|TestOwnWrite|TestOtherBasename|TestDelete|TestWatcherError|TestHub' -v`
Expected: FAIL (`NewHub` undefined; watcher tests time out or fail on content).

- [ ] **Step 4: Implement the loop**

In `internal/mdedit/document.go`, replace the placeholder `loop` with:

```go
// loop turns directory events for this file into document pushes. A burst of
// events is coalesced with one timer; when it fires the file is reread under
// the mutex and pushed unless its hash equals lastRevision (our own write).
func (d *Document) loop() {
	defer close(d.loopDone)
	base := filepath.Base(d.path)
	for {
		select {
		case <-d.stop:
			return
		case ev, ok := <-d.events.Events():
			if !ok {
				return
			}
			if filepath.Base(ev.Name) != base {
				continue
			}
			if !ev.Has(fsnotify.Write) && !ev.Has(fsnotify.Create) && !ev.Has(fsnotify.Rename) && !ev.Has(fsnotify.Remove) {
				continue
			}
			d.mu.Lock()
			if d.pending == nil && !d.closed {
				d.pending = d.afterFunc(d.coalesce, d.reread)
			}
			d.mu.Unlock()
		case err, ok := <-d.events.Errors():
			if !ok {
				return
			}
			d.logger.Error("watch error", "path", d.path, "err", err)
			d.mu.Lock()
			d.closeAllLocked(ReasonWatcherError)
			d.mu.Unlock()
		}
	}
}

// reread runs when the coalesce timer fires.
func (d *Document) reread() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pending = nil
	if d.closed {
		return
	}
	b, _, err := d.readFile(d.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			d.lastRevision = ""
			d.closeAllLocked(ReasonDeleted)
			return
		}
		d.closeAllLocked(ReasonInvalidPath)
		return
	}
	if ce := checkContent(b); ce != nil {
		d.closeAllLocked(ce.Reason)
		return
	}
	rev := Revision(b)
	if rev == d.lastRevision {
		return
	}
	d.lastRevision = rev
	d.logger.Debug("external update", "path", d.path, "revision", rev, "bytes", len(b))
	d.broadcastLocked(string(b), rev, nil, "")
}
```

- [ ] **Step 5: Implement the hub**

Create `internal/mdedit/hub.go`:

```go
package mdedit

import (
	"sync"

	"github.com/charmbracelet/log"
)

// Hub maps an absolute file path to the one Document every socket on that
// file shares. Owned by the dashboard server.
type Hub struct {
	mu     sync.Mutex
	docs   map[string]*Document
	logger *log.Logger
	closed bool
}

func NewHub(logger *log.Logger) *Hub {
	return &Hub{docs: map[string]*Document{}, logger: logger}
}

// Subscribe attaches c to the document for path, creating it on first use.
// validate is re-run on every save from any subscriber; the last one wins,
// which is fine because every subscriber validates the same path the same way.
func (h *Hub) Subscribe(path string, validate func() error, c Conn) (*Document, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, &CloseError{Reason: ReasonWatcherError}
	}
	doc, ok := h.docs[path]
	if !ok {
		doc = NewDocument(Options{Path: path, Validate: validate, Logger: h.logger})
		h.docs[path] = doc
	}
	h.mu.Unlock()

	if err := doc.Subscribe(c); err != nil {
		h.mu.Lock()
		if doc.Unsubscribe(c) && h.docs[path] == doc {
			delete(h.docs, path)
			doc.Close()
		}
		h.mu.Unlock()
		return nil, err
	}
	return doc, nil
}

// Unsubscribe detaches c; the last subscriber's departure closes the document.
func (h *Hub) Unsubscribe(path string, c Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	doc, ok := h.docs[path]
	if !ok {
		return
	}
	if doc.Unsubscribe(c) {
		delete(h.docs, path)
		doc.Close()
	}
}

// Close stops every document and waits for their goroutines.
func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	docs := h.docs
	h.docs = map[string]*Document{}
	h.mu.Unlock()
	for _, doc := range docs {
		doc.Close()
	}
}

func (h *Hub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.docs)
}
```

- [ ] **Step 6: Run the package tests, then with the race detector**

Run: `go test ./internal/mdedit/ -v && go test -race ./internal/mdedit/`
Expected: PASS both. If `-race` reports a data race, it is in Document; fix it under `d.mu` rather than widening waits.

- [ ] **Step 7: Report done**

---

### Task 6: Shared validation helpers in the dashboard package

**Files:**

- Modify: `internal/dashboard/validation.go`
- Modify: `internal/dashboard/handlers_diff.go:537-560` (`handleFile`) and `:668-686` (`fileMatchesVCSIgnore`)
- Test: `internal/dashboard/validation_test.go`

**Interfaces:**

- Produces:

```go
func splitWorkspaceFileParam(param string) (workspaceID, filePath, errMsg string)
func isMarkdownPath(filePath string) bool
func fileMatchesVCSIgnore(ctx context.Context, workspacePath, filePath, vcsType string) (bool, error)
```

- [ ] **Step 1: Write the failing tests**

Create `internal/dashboard/validation_test.go` (if one exists, append):

```go
package dashboard

import "testing"

func TestSplitWorkspaceFileParam(t *testing.T) {
	tests := []struct {
		param, wantWS, wantPath, wantErr string
	}{
		{"ws-1/docs%2Fnotes.md", "ws-1", "docs/notes.md", ""},
		{"ws-1/percent%252Fname.md", "ws-1", "percent%2Fname.md", ""},
		{"ws-1/with%20space.md", "ws-1", "with space.md", ""},
		{"", "", "", "workspace ID is required"},
		{"ws-1", "", "", "invalid path format"},
		{"/notes.md", "", "", "invalid path format"},
		{"ws-1/%zz", "", "", "invalid file path"},
	}
	for _, tt := range tests {
		t.Run(tt.param, func(t *testing.T) {
			ws, p, errMsg := splitWorkspaceFileParam(tt.param)
			if ws != tt.wantWS || p != tt.wantPath || errMsg != tt.wantErr {
				t.Fatalf("got (%q, %q, %q), want (%q, %q, %q)", ws, p, errMsg, tt.wantWS, tt.wantPath, tt.wantErr)
			}
		})
	}
}

func TestIsMarkdownPath(t *testing.T) {
	for _, p := range []string{"a.md", "A.MD", "docs/x.mdx", "x.Mdx"} {
		if !isMarkdownPath(p) {
			t.Errorf("%q should be markdown", p)
		}
	}
	for _, p := range []string{"a.mmd", "a.markdown", "md", "a.md.txt", ""} {
		if isMarkdownPath(p) {
			t.Errorf("%q should not be markdown", p)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/dashboard/ -run 'TestSplitWorkspaceFileParam|TestIsMarkdownPath' -v`
Expected: FAIL to compile.

- [ ] **Step 3: Implement the helpers**

Append to `internal/dashboard/validation.go`:

```go
// splitWorkspaceFileParam parses a chi "*" wildcard of the form
// "{workspaceId}/{url-encoded path}" the way /api/file does. The path is
// decoded exactly once with url.QueryUnescape, matching the frontend's
// encodeURIComponent. Returns an error message for the caller to surface.
func splitWorkspaceFileParam(param string) (workspaceID, filePath, errMsg string) {
	if param == "" {
		return "", "", "workspace ID is required"
	}
	slashIdx := strings.Index(param, "/")
	if slashIdx <= 0 {
		return "", "", "invalid path format"
	}
	decoded, err := url.QueryUnescape(param[slashIdx+1:])
	if err != nil {
		return "", "", "invalid file path"
	}
	return param[:slashIdx], decoded, ""
}

// isMarkdownPath reports whether the editor may open filePath.
func isMarkdownPath(filePath string) bool {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".md", ".mdx":
		return true
	}
	return false
}

// fileMatchesVCSIgnore runs the VCS's check-ignore for filePath inside
// workspacePath. Exit 0 means ignored, exit 1 means not ignored.
func fileMatchesVCSIgnore(ctx context.Context, workspacePath, filePath, vcsType string) (bool, error) {
	cb := vcs.NewCommandBuilder(vcsType)
	run := localShellRun(ctx, workspacePath)
	_, err := run(cb.CheckIgnore(filePath))
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return false, nil
}
```

Add the imports `context`, `errors`, `net/url`, `os/exec`, `path/filepath`, `strings`, and `github.com/sergeknystautas/schmux/internal/vcs` to `validation.go` as needed (keep whatever it already imports).

- [ ] **Step 4: Point the existing handler at the helpers**

In `internal/dashboard/handlers_diff.go`, replace the body of `fileMatchesVCSIgnore` on `GitHandlers` with a one-line delegation:

```go
// fileMatchesVCSIgnore checks if a file path matches VCS ignore patterns.
func (h *GitHandlers) fileMatchesVCSIgnore(ctx context.Context, workspacePath, filePath, vcsType string) (bool, error) {
	return fileMatchesVCSIgnore(ctx, workspacePath, filePath, vcsType)
}
```

In `handleFile`, replace the block from `trimmedPath := chi.URLParam(r, "*")` through `filePath, err := url.QueryUnescape(filePath)` and its error return with:

```go
	workspaceID, filePath, errMsg := splitWorkspaceFileParam(chi.URLParam(r, "*"))
	if errMsg != "" {
		writeJSONError(w, errMsg, http.StatusBadRequest)
		return
	}
```

Remove the now-unused `url` import from `handlers_diff.go` if nothing else uses it.

- [ ] **Step 5: Run the dashboard tests**

Run: `go test ./internal/dashboard/ -run 'TestSplitWorkspaceFileParam|TestIsMarkdownPath|TestHandleFile' -v`
Expected: PASS, including every pre-existing `TestHandleFile*` test (same status codes and messages as before).

- [ ] **Step 6: Report done**

---

### Task 7: Markdown WebSocket handler and server wiring

**Files:**

- Create: `internal/dashboard/websocket_markdown.go`
- Modify: `internal/dashboard/server.go` (struct field near line 265, `NewServer`, route list at line 767, `Stop`, `CloseForTest`)
- Test: `internal/dashboard/websocket_markdown_test.go`

**Interfaces:**

- Consumes: `mdedit.Hub`, `mdedit.Conn`, `contracts.MarkdownSave`, `validateWorkspaceFileTarget`, `splitWorkspaceFileParam`, `isMarkdownPath`, `fileMatchesVCSIgnore`, `s.upgradeWebSocket`'s pieces.
- Produces: route `GET /ws/markdown/*`, `func (s *Server) handleMarkdownWebSocket(w, r)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/dashboard/websocket_markdown_test.go`:

```go
package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/state"
)

func (s *Server) markdownTestRouter() http.Handler {
	r := chi.NewRouter()
	r.HandleFunc("/ws/markdown/*", s.handleMarkdownWebSocket)
	return r
}

// newMarkdownWorkspace creates a git repo with notes.md and a gitignored file.
func newMarkdownWorkspace(t *testing.T, st *state.State) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ws-md")
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	files := map[string]string{
		"notes.md":         "# notes\n",
		"docs/deep.mdx":    "deep\n",
		"secret.md":        "ignored\n",
		".gitignore":       "secret.md\n",
		"code.go":          "package x\n",
		"with space.md":    "space\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "notes.md"), filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspace(state.Workspace{ID: "ws-md", Path: dir, VCS: "git"}); err != nil {
		t.Fatal(err)
	}
	return "ws-md", dir
}

func dialMarkdown(t *testing.T, server *Server, path string) (*websocket.Conn, *http.Response, error, func()) {
	t.Helper()
	ts := httptest.NewServer(server.markdownTestRouter())
	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+path, nil)
	return conn, resp, err, func() {
		if conn != nil {
			conn.Close()
		}
		ts.Close()
	}
}

func readDocument(t *testing.T, conn *websocket.Conn) contracts.MarkdownDocument {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second)) // deadline backstop
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var msg contracts.MarkdownDocument
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return msg
}

func TestMarkdownWS_RejectsBeforeUpgrade(t *testing.T) {
	server, _, st := newTestServer(t)
	newMarkdownWorkspace(t, st)
	tests := []struct {
		name string
		path string
		code int
	}{
		{"unknown workspace", "/ws/markdown/nope/notes.md", http.StatusNotFound},
		{"traversal", "/ws/markdown/ws-md/..%2F..%2Fetc%2Fpasswd", http.StatusBadRequest},
		{"symlink", "/ws/markdown/ws-md/link.md", http.StatusForbidden},
		{"not markdown", "/ws/markdown/ws-md/code.go", http.StatusForbidden},
		{"ignored", "/ws/markdown/ws-md/secret.md", http.StatusForbidden},
		{"directory", "/ws/markdown/ws-md/docs", http.StatusForbidden},
		{"missing", "/ws/markdown/ws-md/none.md", http.StatusNotFound},
		{"wrong case", "/ws/markdown/ws-md/NOTES.md", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, resp, err, done := dialMarkdown(t, server, tt.path)
			defer done()
			if err == nil {
				t.Fatal("expected upgrade to be refused")
			}
			if resp == nil || resp.StatusCode != tt.code {
				t.Fatalf("status = %v, want %d", resp, tt.code)
			}
		})
	}
}

func TestMarkdownWS_RejectsRemoteWorkspace(t *testing.T) {
	server, _, st := newTestServer(t)
	if err := st.AddWorkspace(state.Workspace{ID: "ws-remote", Path: t.TempDir(), RemoteHostID: "host-1"}); err != nil {
		t.Fatal(err)
	}
	_, resp, err, done := dialMarkdown(t, server, "/ws/markdown/ws-remote/notes.md")
	defer done()
	if err == nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for remote workspace, got err=%v resp=%v", err, resp)
	}
}

func TestMarkdownWS_SnapshotSaveAndPercentPath(t *testing.T) {
	server, _, st := newTestServer(t)
	_, dir := newMarkdownWorkspace(t, st)
	conn, _, err, done := dialMarkdown(t, server, "/ws/markdown/ws-md/with%20space.md")
	defer done()
	if err != nil {
		t.Fatal(err)
	}
	first := readDocument(t, conn)
	if first.Content != "space\n" || first.Reply != "" {
		t.Fatalf("first = %+v", first)
	}
	save := contracts.MarkdownSave{Type: "save", ID: "s1", Base: "space\n", Draft: "space edited\n"}
	if err := conn.WriteJSON(save); err != nil {
		t.Fatal(err)
	}
	reply := readDocument(t, conn)
	if reply.Reply != "s1" || reply.Content != "space edited\n" {
		t.Fatalf("reply = %+v", reply)
	}
	got, err := os.ReadFile(filepath.Join(dir, "with space.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "space edited\n" {
		t.Fatalf("disk = %q", got)
	}
}

func TestMarkdownWS_BadFrameClosesWithReason(t *testing.T) {
	server, _, st := newTestServer(t)
	newMarkdownWorkspace(t, st)
	conn, _, err, done := dialMarkdown(t, server, "/ws/markdown/ws-md/notes.md")
	defer done()
	if err != nil {
		t.Fatal(err)
	}
	readDocument(t, conn)
	if err := conn.WriteMessage(websocket.TextMessage, []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err = conn.ReadMessage()
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Text != "bad_request" {
		t.Fatalf("expected close with bad_request, got %v", err)
	}
}

func TestMarkdownWS_ReadLimitIsDedicated(t *testing.T) {
	if wsReadLimit != 64*1024 {
		t.Fatalf("shared wsReadLimit changed to %d", wsReadLimit)
	}
	if markdownWSReadLimit != 4*1024*1024 {
		t.Fatalf("markdownWSReadLimit = %d", markdownWSReadLimit)
	}
}
```

Add `"errors"` to the test file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/dashboard/ -run TestMarkdownWS -v`
Expected: FAIL to compile, `s.handleMarkdownWebSocket undefined`.

- [ ] **Step 3: Implement the handler**

Create `internal/dashboard/websocket_markdown.go`:

```go
package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/mdedit"
)

// markdownWSReadLimit fits a base and a draft of MaxDocumentBytes each plus
// JSON framing and escaping. The shared wsReadLimit stays at 64 KB.
const markdownWSReadLimit = 4 * 1024 * 1024

// markdownConn adapts wsConn to mdedit.Conn. The document is the only writer.
type markdownConn struct{ conn *wsConn }

func (c *markdownConn) Send(msg contracts.MarkdownDocument) error {
	return c.conn.WriteJSON(msg)
}

func (c *markdownConn) CloseWithReason(reason mdedit.CloseReason) {
	frame := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, string(reason))
	_ = c.conn.WriteMessage(websocket.CloseMessage, frame)
	_ = c.conn.Close()
}

// handleMarkdownWebSocket serves /ws/markdown/{workspaceId}/{url-encoded path}.
// Authentication and every path rule run before upgrade; afterwards the
// document owns the socket and this loop only feeds it save frames.
func (s *Server) handleMarkdownWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.requiresAuth() {
		if s.authEnabled() || !s.isTrustedRequest(r) {
			if _, err := s.authenticateRequest(r); err != nil {
				writeJSONError(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
	}

	workspaceID, filePath, errMsg := splitWorkspaceFileParam(chi.URLParam(r, "*"))
	if errMsg != "" {
		writeJSONError(w, errMsg, http.StatusBadRequest)
		return
	}
	if !isMarkdownPath(filePath) {
		writeJSONError(w, "file type not allowed", http.StatusForbidden)
		return
	}
	if verr := validateWorkspaceFileTarget(s.state, workspaceID, filePath); verr != nil {
		writeJSONError(w, verr.message, verr.status)
		return
	}
	ws, _ := s.state.GetWorkspace(workspaceID)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	ignored, err := fileMatchesVCSIgnore(ctx, ws.Path, filePath, s.vcsTypeForWorkspace(ws))
	cancel()
	if err != nil {
		writeJSONError(w, "failed to check ignore patterns", http.StatusInternalServerError)
		return
	}
	if ignored {
		writeJSONError(w, "file is ignored by git", http.StatusForbidden)
		return
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 64 * 1024,
		CheckOrigin:     s.checkWSOrigin,
	}
	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	rawConn.SetReadLimit(markdownWSReadLimit)
	conn := &wsConn{conn: rawConn}
	mc := &markdownConn{conn: conn}
	defer conn.Close()

	absPath := filepath.Join(ws.Path, filePath)
	validate := func() error {
		if verr := validateWorkspaceFileTarget(s.state, workspaceID, filePath); verr != nil {
			return errors.New(verr.message)
		}
		return nil
	}
	doc, err := s.markdownHub.Subscribe(absPath, validate, mc)
	if err != nil {
		reason := mdedit.ReasonInvalidPath
		if ce, ok := err.(*mdedit.CloseError); ok {
			reason = ce.Reason
		}
		mc.CloseWithReason(reason)
		return
	}
	defer s.markdownHub.Unsubscribe(absPath, mc)

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if msgType != websocket.TextMessage {
			doc.Reject(mc, mdedit.ReasonBadRequest)
			return
		}
		var save contracts.MarkdownSave
		if err := json.Unmarshal(data, &save); err != nil || save.Type != "save" {
			doc.Reject(mc, mdedit.ReasonBadRequest)
			return
		}
		doc.Save(mc, save)
	}
}
```

Add `"errors"` to the file's import block.

- [ ] **Step 4: Wire the server**

In `internal/dashboard/server.go`:

1. Add the field next to `gitHandlers *GitHandlers` (around line 265):

```go
	markdownHub       *mdedit.Hub
```

2. In `NewServer`, after the logger is available and before `return`, add:

```go
	s.markdownHub = mdedit.NewHub(logging.Sub(logger, "mdedit"))
```

(`logging` is already imported by `websocket_helpers.go`; add `"github.com/sergeknystautas/schmux/internal/mdedit"` and, if not present in this file, `"github.com/sergeknystautas/schmux/internal/logging"` to the import block.)

3. In the WebSocket route list (line 767 onward), add:

```go
	r.HandleFunc("/ws/markdown/*", s.handleMarkdownWebSocket)
```

4. In `Stop()`, before the function returns, and in `CloseForTest()` after `<-s.broadcastExited`, add:

```go
	if s.markdownHub != nil {
		s.markdownHub.Close()
	}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/dashboard/ -run 'TestMarkdownWS|TestHandleFile' -v && go build ./...`
Expected: PASS. If the symlink case returns 404 instead of 403, `validateWorkspaceFileTarget` is being called after a stat that follows the link; the order in Step 3 calls it first, so check the route table.

- [ ] **Step 6: Report done**

---

### Task 8: Browser reducer

**Files:**

- Modify: `assets/dashboard/package.json`
- Create: `assets/dashboard/src/lib/markdownDocument.ts`
- Test: `assets/dashboard/src/lib/markdownDocument.test.ts`

**Interfaces:**

- Produces:

```ts
export type DocStatus = 'connecting' | 'saved' | 'saving' | 'error';
export interface InFlight {
  id: string;
  base: string;
  draft: string;
}
export interface DocState {
  base: string;
  draft: string;
  inFlight: InFlight | null;
  status: DocStatus;
  reason: string | null;
  awaitingFirst: boolean;
}
export type DocEvent =
  | { type: 'edit'; text: string }
  | { type: 'timer' }
  | { type: 'document'; content: string; reply?: string }
  | { type: 'open' }
  | { type: 'close'; reason: string | null };
export type Effect =
  | { type: 'send'; id: string; base: string; draft: string }
  | { type: 'armTimer' }
  | { type: 'cancelTimer' };
export const initialState: DocState;
export const TERMINAL_REASONS: ReadonlySet<string>;
export function reduce(state: DocState, event: DocEvent, newId: () => string): [DocState, Effect[]];
```

Note for the implementer: the spec lists a `replace(text)` effect. It is realized by passing `state.draft` as ByteMD's `value` prop (Task 10): ByteMD calls `setValue` only when the prop differs from the editor's text, which is exactly the "replace" cases. The reducer therefore has no separate replace effect. `awaitingFirst` is the one field beyond the spec's four; it marks the first document after `open`.

- [ ] **Step 1: Add the editor packages**

In `assets/dashboard/package.json`, under `"dependencies"`, add (keep alphabetical order):

```json
    "@bytemd/plugin-gfm": "1.22.0",
    "@bytemd/react": "1.22.0",
    "bytemd": "1.22.0",
```

Then run from the repo root: `go run ./cmd/build-dashboard`
Expected: the wrapper installs the packages and builds. Note the build output's chunk list; nothing named bytemd should be in the entry chunk yet (nothing imports it).

- [ ] **Step 2: Write the failing tests**

Create `assets/dashboard/src/lib/markdownDocument.test.ts`:

```ts
import { describe, it, expect } from 'vitest';
import {
  reduce,
  initialState,
  TERMINAL_REASONS,
  type DocState,
  type Effect,
} from './markdownDocument';

let counter = 0;
const newId = () => `id-${++counter}`;

function run(state: DocState, ...events: Parameters<typeof reduce>[1][]): [DocState, Effect[]] {
  let s = state;
  const all: Effect[] = [];
  for (const e of events) {
    const [next, effects] = reduce(s, e, newId);
    s = next;
    all.push(...effects);
  }
  return [s, all];
}

const opened = (content: string): DocState =>
  run(initialState, { type: 'open' }, { type: 'document', content })[0];

describe('markdownDocument reducer', () => {
  it('adopts the first document as clean', () => {
    const [s, effects] = run(initialState, { type: 'open' }, { type: 'document', content: '# a' });
    expect(s).toMatchObject({
      base: '# a',
      draft: '# a',
      inFlight: null,
      status: 'saved',
      awaitingFirst: false,
    });
    expect(effects).toEqual([]);
  });

  it('an edit arms the timer only when dirty', () => {
    const s0 = opened('a');
    const [, e1] = run(s0, { type: 'edit', text: 'a' });
    expect(e1).toEqual([]);
    const [s2, e2] = run(s0, { type: 'edit', text: 'ab' });
    expect(s2.draft).toBe('ab');
    expect(e2).toEqual([{ type: 'armTimer' }]);
  });

  it('timer sends base and draft with a fresh id and records inFlight', () => {
    const [s, effects] = run(opened('a'), { type: 'edit', text: 'ab' }, { type: 'timer' });
    expect(effects[1]).toMatchObject({ type: 'send', base: 'a', draft: 'ab' });
    expect(s.inFlight).toMatchObject({ base: 'a', draft: 'ab' });
    expect(s.status).toBe('saving');
  });

  it('reply with no typing since send adopts the merged content', () => {
    const s0 = run(opened('a'), { type: 'edit', text: 'ab' }, { type: 'timer' })[0];
    const id = s0.inFlight!.id;
    const [s, effects] = run(s0, { type: 'document', content: 'ab+agent', reply: id });
    expect(s).toMatchObject({
      base: 'ab+agent',
      draft: 'ab+agent',
      inFlight: null,
      status: 'saved',
    });
    expect(effects).toEqual([]);
  });

  it('reply after more typing rebases onto the sent draft and saves again', () => {
    const s0 = run(
      opened('a'),
      { type: 'edit', text: 'ab' },
      { type: 'timer' },
      { type: 'edit', text: 'abc' }
    )[0];
    const id = s0.inFlight!.id;
    const [s, effects] = run(s0, { type: 'document', content: 'ab+agent', reply: id });
    expect(s.draft).toBe('abc');
    expect(s.base).toBe('ab');
    expect(effects).toEqual([{ type: 'send', id: s.inFlight!.id, base: 'ab', draft: 'abc' }]);
    expect(s.inFlight!.id).not.toBe(id);
  });

  it('a reply for an unknown id is treated as an external document', () => {
    const s0 = run(opened('a'), { type: 'edit', text: 'ab' }, { type: 'timer' })[0];
    const [s, effects] = run(s0, { type: 'document', content: 'zzz', reply: 'stale' });
    expect(s.inFlight).not.toBeNull();
    expect(s.draft).toBe('ab');
    expect(effects).toEqual([]);
  });

  it('external document while clean is adopted', () => {
    const [s, effects] = run(opened('a'), { type: 'document', content: 'agent' });
    expect(s).toMatchObject({ base: 'agent', draft: 'agent' });
    expect(effects).toEqual([]);
  });

  it('external document while dirty cancels the timer and saves now', () => {
    const [s, effects] = run(
      opened('a'),
      { type: 'edit', text: 'ab' },
      { type: 'document', content: 'agent' }
    );
    expect(s.base).toBe('a');
    expect(effects).toEqual([
      { type: 'armTimer' },
      { type: 'cancelTimer' },
      { type: 'send', id: s.inFlight!.id, base: 'a', draft: 'ab' },
    ]);
  });

  it('external document while a save is in flight is ignored', () => {
    const s0 = run(opened('a'), { type: 'edit', text: 'ab' }, { type: 'timer' })[0];
    const [s, effects] = run(s0, { type: 'document', content: 'agent' });
    expect(s).toEqual(s0);
    expect(effects).toEqual([]);
  });

  it('reconnect with a save in flight resends the same id', () => {
    const s0 = run(opened('a'), { type: 'edit', text: 'ab' }, { type: 'timer' })[0];
    const id = s0.inFlight!.id;
    const [s, effects] = run(
      s0,
      { type: 'close', reason: null },
      { type: 'open' },
      { type: 'document', content: 'whatever' }
    );
    expect(effects).toEqual([
      { type: 'cancelTimer' },
      { type: 'send', id, base: 'a', draft: 'ab' },
    ]);
    expect(s.status).toBe('saving');
  });

  it('reconnect while dirty without inFlight sends a fresh save', () => {
    const s0 = run(opened('a'), { type: 'edit', text: 'ab' })[0];
    const [, effects] = run(
      s0,
      { type: 'close', reason: null },
      { type: 'open' },
      { type: 'document', content: 'agent' }
    );
    expect(effects.at(-1)).toMatchObject({ type: 'send', base: 'a', draft: 'ab' });
  });

  it('reconnect while clean adopts the new document', () => {
    const [s] = run(
      opened('a'),
      { type: 'close', reason: 'deleted' },
      { type: 'open' },
      { type: 'document', content: 'back' }
    );
    expect(s).toMatchObject({ base: 'back', draft: 'back', status: 'saved', reason: null });
  });

  it('close keeps the draft, records the reason, and cancels the timer', () => {
    const [s, effects] = run(
      opened('a'),
      { type: 'edit', text: 'ab' },
      { type: 'close', reason: 'write_failed' }
    );
    expect(s).toMatchObject({ draft: 'ab', base: 'a', status: 'error', reason: 'write_failed' });
    expect(effects.at(-1)).toEqual({ type: 'cancelTimer' });
  });

  it('names the terminal reasons', () => {
    expect([...TERMINAL_REASONS].sort()).toEqual([
      'bad_request',
      'invalid_path',
      'not_utf8',
      'too_large',
    ]);
  });
});
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `./test.sh --quick`
Expected: the frontend suite fails on `markdownDocument.test.ts` with a module-not-found error. (The Go suite still passes.)

- [ ] **Step 4: Implement the reducer**

Create `assets/dashboard/src/lib/markdownDocument.ts`:

```ts
// Pure state machine for the Markdown editor's document. The hook feeds it
// socket and timer events and executes the effects it returns; nothing here
// touches React, timers, or the network, so every rule is testable in
// isolation. Terminology follows the spec: base is the text the draft was
// edited from, draft is the editor text, inFlight is the one outstanding save.

export type DocStatus = 'connecting' | 'saved' | 'saving' | 'error';

export interface InFlight {
  id: string;
  base: string;
  draft: string;
}

export interface DocState {
  base: string;
  draft: string;
  inFlight: InFlight | null;
  status: DocStatus;
  reason: string | null;
  // True between 'open' and the first document, which is the server's snapshot.
  awaitingFirst: boolean;
}

export type DocEvent =
  | { type: 'edit'; text: string }
  | { type: 'timer' }
  | { type: 'document'; content: string; reply?: string }
  | { type: 'open' }
  | { type: 'close'; reason: string | null };

export type Effect =
  | { type: 'send'; id: string; base: string; draft: string }
  | { type: 'armTimer' }
  | { type: 'cancelTimer' };

export const TERMINAL_REASONS: ReadonlySet<string> = new Set([
  'too_large',
  'not_utf8',
  'invalid_path',
  'bad_request',
]);

export const initialState: DocState = {
  base: '',
  draft: '',
  inFlight: null,
  status: 'connecting',
  reason: null,
  awaitingFirst: true,
};

const isDirty = (s: DocState) => s.draft !== s.base;

function send(s: DocState, id: string, base: string, draft: string): [DocState, Effect[]] {
  return [
    { ...s, inFlight: { id, base, draft }, status: 'saving' },
    [{ type: 'send', id, base, draft }],
  ];
}

export function reduce(
  state: DocState,
  event: DocEvent,
  newId: () => string
): [DocState, Effect[]] {
  switch (event.type) {
    case 'edit': {
      const s = { ...state, draft: event.text };
      if (isDirty(s) && !s.inFlight && !s.awaitingFirst && s.status !== 'error') {
        return [s, [{ type: 'armTimer' }]];
      }
      return [s, []];
    }

    case 'timer': {
      if (state.inFlight || !isDirty(state)) return [state, []];
      return send(state, newId(), state.base, state.draft);
    }

    case 'open':
      return [{ ...state, status: 'connecting', awaitingFirst: true }, []];

    case 'close': {
      const reason = event.reason ?? state.reason;
      return [
        { ...state, status: 'error', reason, awaitingFirst: false },
        [{ type: 'cancelTimer' }],
      ];
    }

    case 'document': {
      const { content } = event;

      if (state.awaitingFirst) {
        const s = { ...state, awaitingFirst: false, reason: null };
        if (s.inFlight) {
          // A save was outstanding when the socket dropped; resend it with the
          // same id so a committed-but-unacknowledged save is not applied twice.
          return send(s, s.inFlight.id, s.inFlight.base, s.inFlight.draft);
        }
        if (isDirty(s)) {
          return send(s, newId(), s.base, s.draft);
        }
        return [{ ...s, base: content, draft: content, status: 'saved' }, []];
      }

      const isReply =
        event.reply !== undefined && state.inFlight !== null && event.reply === state.inFlight.id;

      if (isReply) {
        const sent = state.inFlight!;
        if (state.draft === sent.draft) {
          return [{ ...state, base: content, draft: content, inFlight: null, status: 'saved' }, []];
        }
        // Typed since sending: the new keystrokes sit on top of the sent draft.
        return send({ ...state, base: sent.draft }, newId(), sent.draft, state.draft);
      }

      if (state.inFlight) {
        return [state, []];
      }
      if (!isDirty(state)) {
        return [{ ...state, base: content, draft: content, status: 'saved' }, []];
      }
      const [s, effects] = send(state, newId(), state.base, state.draft);
      return [s, [{ type: 'cancelTimer' }, ...effects]];
    }
  }
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `./test.sh --quick`
Expected: PASS, including all fourteen reducer tests.

- [ ] **Step 6: Report done**

---

### Task 9: Socket hook

**Files:**

- Create: `assets/dashboard/src/hooks/useMarkdownDocument.ts`
- Test: `assets/dashboard/src/hooks/useMarkdownDocument.test.ts`

**Interfaces:**

- Consumes: `reduce`, `initialState`, `TERMINAL_REASONS` from `../lib/markdownDocument`; `transport.createWebSocket`.
- Produces:

```ts
export interface MarkdownDocumentHandle {
  draft: string;
  status: DocStatus;
  reason: string | null;
  onEdit: (text: string) => void;
}
export default function useMarkdownDocument(
  workspaceId: string,
  filePath: string
): MarkdownDocumentHandle;
```

- [ ] **Step 1: Write the failing tests**

Create `assets/dashboard/src/hooks/useMarkdownDocument.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import useMarkdownDocument from './useMarkdownDocument';

class MockWebSocket {
  static instances: MockWebSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: ((ev: { code: number; reason: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  close = vi.fn();
  send = vi.fn();
  constructor(public url: string) {
    MockWebSocket.instances.push(this);
  }
}

beforeEach(() => {
  MockWebSocket.instances = [];
  vi.stubGlobal('WebSocket', MockWebSocket);
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const lastWS = () => MockWebSocket.instances[MockWebSocket.instances.length - 1];
const open = (ws: MockWebSocket) => ws.onopen?.();
const doc = (ws: MockWebSocket, content: string, reply?: string) =>
  ws.onmessage?.({
    data: JSON.stringify({
      type: 'document',
      content,
      revision: 'sha256:x',
      ...(reply ? { reply } : {}),
    }),
  });
const sentSaves = (ws: MockWebSocket) => ws.send.mock.calls.map((c) => JSON.parse(c[0] as string));

function mount() {
  const hook = renderHook(() => useMarkdownDocument('ws-1', 'docs/notes.md'));
  const ws = lastWS();
  act(() => open(ws));
  act(() => doc(ws, '# a'));
  return { hook, ws };
}

describe('useMarkdownDocument', () => {
  it('connects to the markdown route with the encoded path', () => {
    mount();
    expect(lastWS().url).toBe(`ws://${window.location.host}/ws/markdown/ws-1/docs%2Fnotes.md`);
  });

  it('debounces 500 ms and sends one save with base and draft', () => {
    const { hook, ws } = mount();
    act(() => hook.result.current.onEdit('# ab'));
    act(() => vi.advanceTimersByTime(499));
    expect(ws.send).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(1));
    const saves = sentSaves(ws);
    expect(saves).toHaveLength(1);
    expect(saves[0]).toMatchObject({ type: 'save', base: '# a', draft: '# ab' });
    expect(saves[0].id).toEqual(expect.any(String));
    expect(hook.result.current.status).toBe('saving');
  });

  it('keeps one save in flight and folds later typing into the next', () => {
    const { hook, ws } = mount();
    act(() => hook.result.current.onEdit('# ab'));
    act(() => vi.advanceTimersByTime(500));
    act(() => hook.result.current.onEdit('# abc'));
    act(() => vi.advanceTimersByTime(500));
    expect(sentSaves(ws)).toHaveLength(1);
    act(() => doc(ws, '# ab', sentSaves(ws)[0].id));
    const saves = sentSaves(ws);
    expect(saves).toHaveLength(2);
    expect(saves[1]).toMatchObject({ base: '# ab', draft: '# abc' });
    expect(hook.result.current.draft).toBe('# abc');
  });

  it('adopts an external document when clean and saves at once when dirty', () => {
    const { hook, ws } = mount();
    act(() => doc(ws, '# agent'));
    expect(hook.result.current.draft).toBe('# agent');
    act(() => hook.result.current.onEdit('# agent!'));
    act(() => doc(ws, '# agent2'));
    expect(sentSaves(ws)).toHaveLength(1);
    expect(sentSaves(ws)[0]).toMatchObject({ base: '# agent', draft: '# agent!' });
  });

  it('reconnects with backoff after a non-terminal close and resends the in-flight save', () => {
    const { hook, ws } = mount();
    act(() => hook.result.current.onEdit('# ab'));
    act(() => vi.advanceTimersByTime(500));
    const id = sentSaves(ws)[0].id;
    act(() => ws.onclose?.({ code: 1008, reason: 'write_failed' }));
    expect(hook.result.current.status).toBe('error');
    expect(hook.result.current.reason).toBe('write_failed');
    expect(MockWebSocket.instances).toHaveLength(1);
    act(() => vi.advanceTimersByTime(3000)); // 2000 ms base × up to 1.5 jitter
    expect(MockWebSocket.instances).toHaveLength(2);
    const ws2 = lastWS();
    act(() => open(ws2));
    act(() => doc(ws2, '# a'));
    expect(sentSaves(ws2)).toEqual([expect.objectContaining({ id, base: '# a', draft: '# ab' })]);
  });

  it('does not reconnect after a terminal close', () => {
    const { hook, ws } = mount();
    act(() => ws.onclose?.({ code: 1008, reason: 'too_large' }));
    act(() => vi.advanceTimersByTime(60_000));
    expect(MockWebSocket.instances).toHaveLength(1);
    expect(hook.result.current.reason).toBe('too_large');
  });

  it('closes the socket on unmount', () => {
    const { hook, ws } = mount();
    hook.unmount();
    expect(ws.close).toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `./test.sh --quick`
Expected: frontend suite fails with module-not-found for `useMarkdownDocument`.

- [ ] **Step 3: Implement the hook**

Create `assets/dashboard/src/hooks/useMarkdownDocument.ts`:

```ts
import { useCallback, useEffect, useReducer, useRef } from 'react';
import { transport } from '../lib/transport';
import {
  reduce,
  initialState,
  TERMINAL_REASONS,
  type DocEvent,
  type DocState,
  type DocStatus,
  type Effect,
} from '../lib/markdownDocument';
import type { MarkdownDocument } from '../lib/types.generated';

// useMarkdownDocument binds the pure document reducer to one WebSocket on
// /ws/markdown/{workspaceId}/{path}. It owns the 500 ms autosave timer and
// reconnection; every decision about what to send lives in the reducer.

export interface MarkdownDocumentHandle {
  draft: string;
  status: DocStatus;
  reason: string | null;
  onEdit: (text: string) => void;
}

const AUTOSAVE_MS = 500;
const RECONNECT_DELAY_MS = 2000;
const MAX_RECONNECT_DELAY_MS = 30000;

interface WebSocketLike {
  onopen: (() => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: ((ev: { code: number; reason: string }) => void) | null;
  onerror: (() => void) | null;
  send(data: string): void;
  close(): void;
}

const newId = () =>
  typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;

export default function useMarkdownDocument(
  workspaceId: string,
  filePath: string
): MarkdownDocumentHandle {
  // The reducer runs synchronously against a ref, then a counter bump
  // re-renders. React's useReducer is not used because it may defer running
  // the reducer until render, which would run effects against stale state.
  const stateRef = useRef<DocState>(initialState);
  const [, rerender] = useReducer((n: number) => n + 1, 0);

  const wsRef = useRef<WebSocketLike | null>(null);
  const timerRef = useRef<number | null>(null);
  const reconnectRef = useRef<number | null>(null);
  const reconnectDelayRef = useRef(RECONNECT_DELAY_MS);
  const stoppedRef = useRef(false);

  const dispatch = useCallback((e: DocEvent) => {
    const [next, effects] = reduce(stateRef.current, e, newId);
    stateRef.current = next;
    rerender();
    runEffects(effects);
  }, []);

  function runEffects(effects: Effect[]) {
    for (const effect of effects) {
      switch (effect.type) {
        case 'send':
          wsRef.current?.send(
            JSON.stringify({ type: 'save', id: effect.id, base: effect.base, draft: effect.draft })
          );
          break;
        case 'armTimer':
          if (timerRef.current !== null) window.clearTimeout(timerRef.current);
          timerRef.current = window.setTimeout(() => {
            timerRef.current = null;
            dispatch({ type: 'timer' });
          }, AUTOSAVE_MS);
          break;
        case 'cancelTimer':
          if (timerRef.current !== null) {
            window.clearTimeout(timerRef.current);
            timerRef.current = null;
          }
          break;
      }
    }
  }

  useEffect(() => {
    stoppedRef.current = false;
    reconnectDelayRef.current = RECONNECT_DELAY_MS;

    const connect = () => {
      if (stoppedRef.current) return;
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const url = `${protocol}//${window.location.host}/ws/markdown/${workspaceId}/${encodeURIComponent(filePath)}`;
      const ws = transport.createWebSocket(url) as unknown as WebSocketLike;
      wsRef.current = ws;

      ws.onopen = () => {
        if (wsRef.current !== ws) return;
        reconnectDelayRef.current = RECONNECT_DELAY_MS;
        dispatch({ type: 'open' });
      };
      ws.onmessage = (event) => {
        if (wsRef.current !== ws) return;
        let msg: MarkdownDocument;
        try {
          msg = JSON.parse(event.data as string) as MarkdownDocument;
        } catch {
          return;
        }
        if (msg.type !== 'document') return;
        dispatch({ type: 'document', content: msg.content, reply: msg.reply });
      };
      ws.onclose = (event) => {
        if (wsRef.current !== ws) return;
        wsRef.current = null;
        const reason = event.reason || null;
        dispatch({ type: 'close', reason });
        if (stoppedRef.current || (reason && TERMINAL_REASONS.has(reason))) return;
        const jitter = reconnectDelayRef.current * (0.5 + Math.random());
        reconnectRef.current = window.setTimeout(() => {
          reconnectDelayRef.current = Math.min(
            reconnectDelayRef.current * 2,
            MAX_RECONNECT_DELAY_MS
          );
          connect();
        }, jitter);
      };
      ws.onerror = () => {
        // onclose follows; nothing to do here.
      };
    };

    connect();

    return () => {
      stoppedRef.current = true;
      if (timerRef.current !== null) window.clearTimeout(timerRef.current);
      if (reconnectRef.current !== null) window.clearTimeout(reconnectRef.current);
      const ws = wsRef.current;
      wsRef.current = null;
      ws?.close();
    };
  }, [workspaceId, filePath, dispatch]);

  const onEdit = useCallback((text: string) => dispatch({ type: 'edit', text }), [dispatch]);

  const state = stateRef.current;
  return { draft: state.draft, status: state.status, reason: state.reason, onEdit };
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `./test.sh --quick`
Expected: PASS. If the reconnect test sees no second socket after 3000 ms, `Math.random` returned above 0.5; stub it in that test with `vi.spyOn(Math, 'random').mockReturnValue(0)` at the top of the test and advance by 2000 ms instead.

- [ ] **Step 5: Report done**

---

### Task 10: ByteMD adapter and styles

**Files:**

- Create: `assets/dashboard/src/components/markdown/MarkdownEditor.tsx`
- Create: `assets/dashboard/src/components/markdown/bytemdSchmuxPlugin.ts`
- Create: `assets/dashboard/src/styles/markdownEditor.module.css`
- Test: `assets/dashboard/src/components/markdown/MarkdownEditor.test.tsx`, `assets/dashboard/src/components/markdown/bytemdSchmuxPlugin.test.ts`

**Interfaces:**

- Produces:

```ts
// MarkdownEditor.tsx
export interface MarkdownEditorProps {
  value: string;
  onChange: (text: string) => void;
  workspaceId: string;
  filePath: string;
}
export default function MarkdownEditor(props: MarkdownEditorProps): JSX.Element;
// bytemdSchmuxPlugin.ts
export function mapCursorOffset(oldText: string, newText: string, offset: number): number;
export function rewriteRelativeImages(tree: HastRoot, workspaceId: string, filePath: string): void;
export function schmuxPlugin(workspaceId: string, filePath: string): BytemdPlugin;
```

- [ ] **Step 1: Write the failing plugin tests**

Create `assets/dashboard/src/components/markdown/bytemdSchmuxPlugin.test.ts`:

```ts
import { describe, it, expect } from 'vitest';
import { mapCursorOffset, rewriteRelativeImages } from './bytemdSchmuxPlugin';

describe('mapCursorOffset', () => {
  it('keeps an offset inside the unchanged prefix', () => {
    expect(mapCursorOffset('hello world', 'hello brave world', 3)).toBe(3);
  });
  it('shifts an offset that sits past the change by the length delta', () => {
    expect(mapCursorOffset('hello world', 'hello brave world', 9)).toBe(15);
  });
  it('clamps an offset that fell inside a removed region', () => {
    expect(mapCursorOffset('abcdefgh', 'abgh', 5)).toBe(2);
  });
  it('clamps to the new length', () => {
    expect(mapCursorOffset('abcdef', 'ab', 6)).toBe(2);
  });
});

describe('rewriteRelativeImages', () => {
  const img = (src: string) => ({
    type: 'element',
    tagName: 'img',
    properties: { src },
    children: [],
  });
  it('rewrites relative sources to the authenticated file route', () => {
    const tree = { type: 'root', children: [img('./pics/a.png'), img('../b.png')] };
    rewriteRelativeImages(tree as never, 'ws-1', 'docs/readme.md');
    expect(tree.children.map((c) => c.properties.src)).toEqual([
      '/api/file/ws-1/docs%2Fpics%2Fa.png',
      '/api/file/ws-1/b.png',
    ]);
  });
  it('leaves absolute, data, and escaping sources alone', () => {
    const tree = {
      type: 'root',
      children: [
        img('https://x/y.png'),
        img('data:image/png;base64,AA=='),
        img('../../../etc/x.png'),
      ],
    };
    rewriteRelativeImages(tree as never, 'ws-1', 'docs/readme.md');
    expect(tree.children.map((c) => c.properties.src)).toEqual([
      'https://x/y.png',
      'data:image/png;base64,AA==',
      '../../../etc/x.png',
    ]);
  });
});
```

- [ ] **Step 2: Write the failing editor test**

Create `assets/dashboard/src/components/markdown/MarkdownEditor.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import MarkdownEditor from './MarkdownEditor';

// ByteMD renders a real CodeMirror 5 instance into jsdom.

describe('MarkdownEditor', () => {
  it('renders the value and reports typing through onChange', async () => {
    const onChange = vi.fn();
    render(<MarkdownEditor value="# hi" onChange={onChange} workspaceId="ws-1" filePath="a.md" />);
    const cm = await waitFor(() => {
      const el = document.querySelector('.CodeMirror') as HTMLElement & {
        CodeMirror?: {
          getValue(): string;
          setValue(v: string): void;
          replaceRange(t: string, p: { line: number; ch: number }): void;
        };
      };
      if (!el?.CodeMirror) throw new Error('CodeMirror not mounted yet');
      return el.CodeMirror;
    });
    expect(cm.getValue()).toBe('# hi');
    cm.replaceRange('!', { line: 0, ch: 4 });
    expect(onChange).toHaveBeenLastCalledWith('# hi!');
  });

  it('gives every toolbar icon a button role, tab stop, and accessible name', async () => {
    render(<MarkdownEditor value="x" onChange={() => {}} workspaceId="ws-1" filePath="a.md" />);
    await waitFor(() => {
      const icons = document.querySelectorAll('.bytemd-toolbar-icon');
      if (icons.length === 0) throw new Error('toolbar not mounted yet');
    });
    for (const icon of Array.from(document.querySelectorAll('.bytemd-toolbar-icon'))) {
      expect(icon.getAttribute('role')).toBe('button');
      expect(icon.getAttribute('tabindex')).toBe('0');
      expect(icon.getAttribute('aria-label')).toBeTruthy();
    }
  });

  it('activates a toolbar icon with Enter', async () => {
    render(<MarkdownEditor value="x" onChange={() => {}} workspaceId="ws-1" filePath="a.md" />);
    const icon = await waitFor(() => {
      const el = document.querySelector('.bytemd-toolbar-icon') as HTMLElement | null;
      if (!el) throw new Error('toolbar not mounted yet');
      return el;
    });
    const click = vi.spyOn(icon, 'click');
    fireEvent.keyDown(icon, { key: 'Enter' });
    expect(click).toHaveBeenCalled();
  });

  it('restores the cursor after an external value replacement', async () => {
    const { rerender } = render(
      <MarkdownEditor value="hello world" onChange={() => {}} workspaceId="ws-1" filePath="a.md" />
    );
    const cm = await waitFor(() => {
      const el = document.querySelector('.CodeMirror') as HTMLElement & {
        CodeMirror?: {
          setCursor(p: { line: number; ch: number }): void;
          getCursor(): { line: number; ch: number };
        };
      };
      if (!el?.CodeMirror) throw new Error('CodeMirror not mounted yet');
      return el.CodeMirror;
    });
    cm.setCursor({ line: 0, ch: 9 });
    rerender(
      <MarkdownEditor
        value="hello brave world"
        onChange={() => {}}
        workspaceId="ws-1"
        filePath="a.md"
      />
    );
    await waitFor(() => {
      expect(cm.getCursor()).toEqual({ line: 0, ch: 15 });
    });
  });

  it('does not render a script from the document', async () => {
    // mode="split" forces the preview pane to render regardless of jsdom's zero width.
    render(
      <MarkdownEditor
        value={'<script>window.__pwned = 1</script>\n\n# ok'}
        onChange={() => {}}
        workspaceId="ws-1"
        filePath="a.md"
        mode="split"
      />
    );
    await screen.findByText('ok');
    expect(document.querySelector('.bytemd-preview script')).toBeNull();
    expect((window as unknown as { __pwned?: number }).__pwned).toBeUndefined();
  });
});
```

If jsdom lacks `document.createRange` or `getClientRects` for CodeMirror 5, add this at the top of the test file (jsdom polyfill, test-only):

```ts
if (!document.createRange) {
  document.createRange = () =>
    ({
      setStart() {},
      setEnd() {},
      getBoundingClientRect: () => ({ right: 0 }),
      getClientRects: () => ({ length: 0, item: () => null, [Symbol.iterator]: function* () {} }),
    }) as unknown as Range;
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `./test.sh --quick`
Expected: frontend suite fails with module-not-found for both new modules.

- [ ] **Step 4: Implement the plugin**

Create `assets/dashboard/src/components/markdown/bytemdSchmuxPlugin.ts`:

```ts
import type { BytemdPlugin } from 'bytemd';
import type { Editor as CodeMirrorEditor, EditorChange } from 'codemirror';
import { getWorkspaceFileUrl } from '../../lib/api';
import { resolveRelativePath } from '../../lib/pathUtils';

// The one ByteMD plugin schmux owns. It does three things and nothing else:
// rewrite relative image URLs (after ByteMD's sanitizer has run), keep the
// cursor in place when the value prop replaces the text, and give ByteMD's
// div-based toolbar icons keyboard and screen-reader semantics.

interface HastNode {
  type: string;
  tagName?: string;
  properties?: Record<string, unknown>;
  children?: HastNode[];
}
export type HastRoot = HastNode;

const ABSOLUTE = /^([a-z][a-z0-9+.-]*:|\/\/|\/)/i;

export function rewriteRelativeImages(tree: HastRoot, workspaceId: string, filePath: string): void {
  const visit = (node: HastNode) => {
    if (node.type === 'element' && node.tagName === 'img' && node.properties) {
      const src = node.properties.src;
      if (typeof src === 'string' && !ABSOLUTE.test(src)) {
        const resolved = resolveRelativePath(src, filePath);
        if (resolved !== null) node.properties.src = getWorkspaceFileUrl(workspaceId, resolved);
      }
    }
    node.children?.forEach(visit);
  };
  visit(tree);
}

// mapCursorOffset moves a character offset from oldText to newText: unchanged
// prefix keeps it, unchanged suffix shifts it by the length delta, anything
// inside the changed region clamps to the start of that region.
export function mapCursorOffset(oldText: string, newText: string, offset: number): number {
  let prefix = 0;
  const max = Math.min(oldText.length, newText.length);
  while (prefix < max && oldText[prefix] === newText[prefix]) prefix++;
  if (offset <= prefix) return Math.min(offset, newText.length);
  let suffix = 0;
  while (
    suffix < max - prefix &&
    oldText[oldText.length - 1 - suffix] === newText[newText.length - 1 - suffix]
  ) {
    suffix++;
  }
  if (offset >= oldText.length - suffix) return offset + (newText.length - oldText.length);
  return prefix;
}

function labelFor(icon: HTMLElement): string {
  const tippy = (icon as HTMLElement & { _tippy?: { props?: { content?: unknown } } })._tippy;
  const content = tippy?.props?.content;
  if (typeof content === 'string' && content.trim()) return content.trim();
  return icon.getAttribute('aria-label') || 'Toolbar action';
}

function normalizeToolbar(root: HTMLElement): () => void {
  const apply = () => {
    root.querySelectorAll<HTMLElement>('.bytemd-toolbar-icon').forEach((icon) => {
      icon.setAttribute('role', 'button');
      icon.setAttribute('tabindex', '0');
      icon.setAttribute('aria-label', labelFor(icon));
    });
  };
  const onKeyDown = (e: KeyboardEvent) => {
    const target = e.target as HTMLElement | null;
    if (!target?.classList.contains('bytemd-toolbar-icon')) return;
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      target.click();
    }
  };
  apply();
  const observer = new MutationObserver(apply);
  observer.observe(root, { childList: true, subtree: true });
  root.addEventListener('keydown', onKeyDown);
  return () => {
    observer.disconnect();
    root.removeEventListener('keydown', onKeyDown);
  };
}

export function schmuxPlugin(workspaceId: string, filePath: string): BytemdPlugin {
  return {
    rehype: (processor) =>
      processor.use(() => (tree: unknown) => {
        rewriteRelativeImages(tree as HastRoot, workspaceId, filePath);
      }),
    editorEffect({ editor, root }) {
      const cm = editor as CodeMirrorEditor;
      let saved: { offset: number; scroll: { left: number; top: number }; text: string } | null =
        null;
      const onBefore = (_: CodeMirrorEditor, change: EditorChange) => {
        if (change.origin !== 'setValue') return;
        const info = cm.getScrollInfo();
        saved = {
          offset: cm.indexFromPos(cm.getCursor()),
          scroll: { left: info.left, top: info.top },
          text: cm.getValue(),
        };
      };
      const onChange = (_: CodeMirrorEditor, change: EditorChange) => {
        if (change.origin !== 'setValue' || !saved) return;
        const next = mapCursorOffset(saved.text, cm.getValue(), saved.offset);
        cm.setCursor(cm.posFromIndex(next));
        cm.scrollTo(saved.scroll.left, saved.scroll.top);
        saved = null;
      };
      cm.on('beforeChange', onBefore);
      cm.on('change', onChange);
      const stopToolbar = normalizeToolbar(root);
      return () => {
        cm.off('beforeChange', onBefore);
        cm.off('change', onChange);
        stopToolbar();
      };
    },
  };
}
```

`codemirror` types come from ByteMD's own dependency `@types/codemirror`; if `tsc` cannot resolve `'codemirror'`, add `"@types/codemirror": "5.60.15"` to `devDependencies` in `package.json` and rerun `go run ./cmd/build-dashboard`.

- [ ] **Step 5: Implement the editor component**

Create `assets/dashboard/src/components/markdown/MarkdownEditor.tsx`:

```tsx
import { useMemo } from 'react';
import { Editor } from '@bytemd/react';
import gfm from '@bytemd/plugin-gfm';
import 'bytemd/dist/index.css';
import { schmuxPlugin } from './bytemdSchmuxPlugin';
import styles from '../../styles/markdownEditor.module.css';

export interface MarkdownEditorProps {
  value: string;
  onChange: (text: string) => void;
  workspaceId: string;
  filePath: string;
  // Tests only; production always uses ByteMD's "auto".
  mode?: 'auto' | 'split' | 'tab';
}

// MarkdownEditor is the whole ByteMD surface. `value` is the reducer's draft:
// ByteMD only calls CodeMirror setValue when the prop differs from the
// editor's text, so a typed change is a no-op here and an incoming document
// is a replacement. mode="auto" is ByteMD's own layout rule: side-by-side
// with its Write-only/Preview-only toggles above 800 px, tabs below.
export default function MarkdownEditor({
  value,
  onChange,
  workspaceId,
  filePath,
  mode = 'auto',
}: MarkdownEditorProps) {
  const plugins = useMemo(
    () => [gfm(), schmuxPlugin(workspaceId, filePath)],
    [workspaceId, filePath]
  );
  return (
    <div className={styles.editor} data-testid="markdown-editor">
      <Editor value={value} plugins={plugins} mode={mode} onChange={onChange} />
    </div>
  );
}
```

- [ ] **Step 6: Write the styles**

Create `assets/dashboard/src/styles/markdownEditor.module.css`:

```css
/* Page-scoped layout plus every ByteMD override. ByteMD ships fixed light
   colors; these rules map its surfaces to schmux tokens in both themes. */

.editor {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.editor :global(.bytemd) {
  flex: 1;
  min-height: 0;
  height: auto;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background: var(--color-surface);
  color: var(--color-text);
  font-family: inherit;
}

.editor :global(.bytemd-toolbar) {
  background: var(--color-surface-alt);
  border-bottom: 1px solid var(--color-border);
  color: var(--color-text-muted);
}

.editor :global(.bytemd-toolbar-icon:hover) {
  background: var(--color-surface-elevated);
  color: var(--color-text);
}

.editor :global(.bytemd-toolbar-icon:focus-visible) {
  outline: 2px solid var(--color-accent);
  outline-offset: 2px;
}

.editor :global(.bytemd-toolbar-icon-active) {
  color: var(--color-accent);
}

.editor :global(.bytemd-editor),
.editor :global(.CodeMirror) {
  background: var(--color-surface);
  color: var(--color-text);
  font-family: var(--font-mono);
  font-size: 0.875rem;
}

.editor :global(.CodeMirror-gutters) {
  background: var(--color-surface-alt);
  border-right: 1px solid var(--color-border-subtle);
}

.editor :global(.CodeMirror-cursor) {
  border-left-color: var(--color-text);
}

.editor :global(.CodeMirror-selected) {
  background: var(--color-accent-subtle);
}

.editor :global(.bytemd-preview) {
  background: var(--color-surface);
  color: var(--color-text);
  border-left: 1px solid var(--color-border);
}

.editor :global(.bytemd-preview .markdown-body) {
  color: var(--color-text);
  background: transparent;
  font-size: 0.875rem;
  line-height: 1.6;
}

.editor :global(.bytemd-preview .markdown-body a) {
  color: var(--color-accent);
}

.editor :global(.bytemd-preview .markdown-body code),
.editor :global(.bytemd-preview .markdown-body pre) {
  background: var(--color-surface-alt);
  color: var(--color-text);
}

.editor :global(.bytemd-preview .markdown-body blockquote) {
  color: var(--color-text-muted);
  border-left-color: var(--color-border);
}

.editor :global(.bytemd-preview .markdown-body table td),
.editor :global(.bytemd-preview .markdown-body table th) {
  border-color: var(--color-border);
}

.editor :global(.bytemd-status) {
  background: var(--color-surface-alt);
  border-top: 1px solid var(--color-border);
  color: var(--color-text-muted);
}

.editor :global(.bytemd-sidebar) {
  background: var(--color-surface);
  border-left: 1px solid var(--color-border);
  color: var(--color-text);
}

.editor :global(.bytemd-fullscreen) {
  background: var(--color-surface);
}

/* tippy tooltips portal to body; scope by ByteMD's theme class. */
:global(.tippy-box) {
  background: var(--color-surface-elevated);
  color: var(--color-text);
  border: 1px solid var(--color-border);
  box-shadow: var(--shadow-md);
}

.status {
  margin-left: auto;
  color: var(--color-text-muted);
  font-size: 0.75rem;
}

.statusError {
  color: var(--color-danger);
}
```

Every token above exists in `global.css` (`--radius-md: 6px`, `--font-mono`, and the `--color-*`/`--shadow-*` names are defined in its `:root` and theme blocks). Do not add new tokens.

- [ ] **Step 7: Run the tests**

Run: `./test.sh --quick`
Expected: PASS for both new test files. If the cursor test fails with the cursor at `{line:0, ch:0}`, ByteMD's `change` event fired before the plugin's handler was attached; move `cm.on('beforeChange'/'change')` registration to run synchronously in `editorEffect` (it already does) and confirm `editorEffect` is invoked once on mount by adding `console.log` temporarily, then remove it.

- [ ] **Step 8: Report done**

---

### Task 11: Page controller and viewer extraction

**Files:**

- Create: `assets/dashboard/src/components/markdown/MarkdownViewer.tsx`
- Modify: `assets/dashboard/src/routes/MarkdownPreviewPage.tsx`
- Test: `assets/dashboard/src/routes/MarkdownPreviewPage.test.tsx`

**Interfaces:**

- Consumes: `useMarkdownDocument`, `MarkdownEditor`.
- Produces: `MarkdownViewer({ workspaceId, filePath, content })`.

- [ ] **Step 1: Extract the viewer**

Create `assets/dashboard/src/components/markdown/MarkdownViewer.tsx` with the render-only part of today's page, unchanged in behavior:

```tsx
import { useMemo } from 'react';
import type { ImgHTMLAttributes } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { getWorkspaceFileUrl } from '../../lib/api';
import { resolveRelativePath } from '../../lib/pathUtils';

export interface MarkdownViewerProps {
  workspaceId: string;
  filePath: string;
  content: string;
}

// MarkdownViewer is the read-only rendering the page used before the editor:
// remote workspaces and files the editor refuses (too large, not UTF-8).
export default function MarkdownViewer({ workspaceId, filePath, content }: MarkdownViewerProps) {
  const components = useMemo(
    () => ({
      img: ({ src, alt, ...rest }: ImgHTMLAttributes<HTMLImageElement>) => {
        if (typeof src !== 'string') {
          return <img src={src} alt={alt} {...rest} />;
        }
        const resolved = resolveRelativePath(src, filePath);
        const finalSrc = resolved === null ? src : getWorkspaceFileUrl(workspaceId, resolved);
        return <img src={finalSrc} alt={alt} {...rest} />;
      },
    }),
    [workspaceId, filePath]
  );
  return (
    <div className="markdown-preview-content" data-testid="markdown-viewer">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  );
}
```

- [ ] **Step 2: Update the page tests**

In `assets/dashboard/src/routes/MarkdownPreviewPage.test.tsx`, add these mocks beside the existing ones near the top (after the `SessionTabs` mock):

```tsx
const hookState = {
  draft: '',
  status: 'connecting' as 'connecting' | 'saved' | 'saving' | 'error',
  reason: null as string | null,
  onEdit: vi.fn(),
};
vi.mock('../hooks/useMarkdownDocument', () => ({
  default: () => hookState,
}));
vi.mock('../components/markdown/MarkdownEditor', () => ({
  default: ({ value }: { value: string }) => <div data-testid="markdown-editor">{value}</div>,
}));
```

Change the `useSessions` mock so the workspace list is configurable:

```tsx
const workspacesRef = {
  current: [
    { id: 'ws-001', files_changed: 0, lines_added: 0, lines_removed: 0, sessions: [] },
  ] as Array<Record<string, unknown>>,
};
vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({ workspaces: workspacesRef.current }),
}));
```

Then add a new `describe` block at the end of the file:

```tsx
describe('MarkdownPreviewPage editor/viewer split', () => {
  beforeEach(() => {
    hookState.draft = '';
    hookState.status = 'connecting';
    hookState.reason = null;
    workspacesRef.current = [
      { id: 'ws-001', files_changed: 0, lines_added: 0, lines_removed: 0, sessions: [] },
    ];
  });

  it('renders the editor for a local workspace with the hook draft and status', () => {
    hookState.draft = '# from socket';
    hookState.status = 'saved';
    renderAt('/diff/ws-001/md/notes.md');
    expect(screen.getByTestId('markdown-editor')).toHaveTextContent('# from socket');
    expect(screen.getByTestId('markdown-status')).toHaveTextContent('Saved');
    expect(mockGetFileContent).not.toHaveBeenCalled();
  });

  it('shows Saving… while a save is in flight and the close reason on error', () => {
    hookState.status = 'saving';
    const { unmount } = renderAt('/diff/ws-001/md/notes.md');
    expect(screen.getByTestId('markdown-status')).toHaveTextContent('Saving…');
    unmount();
    hookState.status = 'error';
    hookState.reason = 'write_failed';
    renderAt('/diff/ws-001/md/notes.md');
    expect(screen.getByTestId('markdown-status')).toHaveTextContent('write_failed');
  });

  it('renders the read-only viewer for a remote workspace', async () => {
    workspacesRef.current = [
      {
        id: 'ws-001',
        remote_host_id: 'host-1',
        files_changed: 0,
        lines_added: 0,
        lines_removed: 0,
        sessions: [],
      },
    ];
    mockGetFileContent.mockResolvedValue('# remote');
    renderAt('/diff/ws-001/md/notes.md');
    expect(await screen.findByTestId('markdown-viewer')).toHaveTextContent('remote');
    expect(screen.queryByTestId('markdown-editor')).toBeNull();
  });

  it('falls back to the viewer when the editor refuses the file', async () => {
    hookState.status = 'error';
    hookState.reason = 'too_large';
    mockGetFileContent.mockResolvedValue('# big');
    renderAt('/diff/ws-001/md/notes.md');
    expect(await screen.findByTestId('markdown-viewer')).toHaveTextContent('big');
    expect(screen.queryByTestId('markdown-editor')).toBeNull();
  });

  it('keeps the Download link', () => {
    hookState.status = 'saved';
    renderAt('/diff/ws-001/md/notes.md');
    expect(screen.getByTestId('download-markdown')).toHaveAttribute(
      'href',
      '/api/file/ws-001/notes.md'
    );
  });
});
```

Existing tests in that file that assert `getFileContent` is called on mount or on VCS-stat change for a local workspace now describe the viewer path only: set `workspacesRef.current[0].remote_host_id = 'host-1'` inside those tests, or delete the ones that assert the local refetch, since the spec removes that behavior for the editor path.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `./test.sh --quick`
Expected: the new page tests fail (no `markdown-status`, editor not rendered).

- [ ] **Step 4: Rewrite the page**

Replace the body of `assets/dashboard/src/routes/MarkdownPreviewPage.tsx` with:

```tsx
import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useParams, Link, useNavigate, useLocation } from 'react-router';
import { getFileContent, getWorkspaceFileUrl, getErrorMessage } from '../lib/api';
import { useSessions } from '../contexts/SessionsContext';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import MarkdownViewer from '../components/markdown/MarkdownViewer';
import MarkdownEditor from '../components/markdown/MarkdownEditor';
import useMarkdownDocument from '../hooks/useMarkdownDocument';
import styles from '../styles/markdownEditor.module.css';

const VIEWER_FALLBACK_REASONS = new Set(['too_large', 'not_utf8']);

// Controller for /diff/:workspaceId/md/:filepath. Local workspaces get the
// editor over its WebSocket; remote workspaces, and files the editor refuses,
// get the read-only viewer over GET /api/file.
export default function MarkdownPreviewPage() {
  const { workspaceId = '', filepath = '' } = useParams();
  const navigate = useNavigate();
  const { workspaces } = useSessions();
  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  const isRemote = Boolean(workspace?.remote_host_id);

  // Same behavior as before the editor: once the workspace list has loaded,
  // a workspace that no longer exists sends the user home.
  useEffect(() => {
    if (workspaces && workspaceId && !workspace) navigate('/');
  }, [workspaces, workspaceId, workspace, navigate]);

  if (!workspaceId || !filepath) return null;
  if (isRemote) {
    return <ViewerPage workspaceId={workspaceId} filePath={filepath} />;
  }
  return <EditorPage workspaceId={workspaceId} filePath={filepath} />;
}

function Frame({
  workspaceId,
  filePath,
  status,
  children,
}: {
  workspaceId: string;
  filePath: string;
  status?: { text: string; error: boolean };
  children: ReactNode;
}) {
  const { workspaces } = useSessions();
  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  return (
    <>
      {workspace && (
        <>
          <WorkspaceHeader workspace={workspace} />
          <SessionTabs sessions={workspace.sessions || []} workspace={workspace} />
        </>
      )}
      <div className="diff-page">
        <div className="diff-content diff-content--standalone">
          <div className="diff-content__header">
            <h2 className="diff-content__title">
              {filePath}
              <a
                className="btn btn--sm btn--secondary"
                data-testid="download-markdown"
                title="Download Markdown file"
                href={getWorkspaceFileUrl(workspaceId, filePath)}
                download={filePath.split('/').pop() || 'file.md'}
              >
                Download
              </a>
              {status && (
                <span
                  className={
                    status.error ? `${styles.status} ${styles.statusError}` : styles.status
                  }
                  data-testid="markdown-status"
                  role="status"
                >
                  {status.text}
                </span>
              )}
            </h2>
          </div>
          {children}
        </div>
      </div>
    </>
  );
}

function EditorPage({ workspaceId, filePath }: { workspaceId: string; filePath: string }) {
  const { draft, status, reason, onEdit } = useMarkdownDocument(workspaceId, filePath);
  if (status === 'error' && reason && VIEWER_FALLBACK_REASONS.has(reason)) {
    return <ViewerPage workspaceId={workspaceId} filePath={filePath} notice={reason} />;
  }
  const statusText =
    status === 'saving'
      ? 'Saving…'
      : status === 'saved'
        ? 'Saved'
        : status === 'error'
          ? reason || 'Disconnected'
          : 'Connecting…';
  return (
    <Frame
      workspaceId={workspaceId}
      filePath={filePath}
      status={{ text: statusText, error: status === 'error' }}
    >
      <MarkdownEditor
        value={draft}
        onChange={onEdit}
        workspaceId={workspaceId}
        filePath={filePath}
      />
    </Frame>
  );
}

const getMarkdownScrollPositionKey = (workspaceId: string, filepath: string) =>
  `schmux-markdown-scroll-position-${workspaceId}-${filepath}`;

function ViewerPage({
  workspaceId,
  filePath,
  notice,
}: {
  workspaceId: string;
  filePath: string;
  notice?: string;
}) {
  const location = useLocation();
  const { workspaces } = useSessions();
  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const prevGitStatsRef = useRef<{ files: number; added: number; removed: number } | null>(null);
  const contentRef = useRef<HTMLDivElement>(null);

  const loadFile = async () => {
    setLoading(true);
    setError('');
    try {
      setContent(await getFileContent(workspaceId, filePath));
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to load file'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadFile();
  }, [workspaceId, filePath, location.key]);

  // The viewer keeps the VCS-counter refetch; the editor path does not need it.
  useEffect(() => {
    if (!workspace) return;
    const current = {
      files: workspace.files_changed,
      added: workspace.lines_added,
      removed: workspace.lines_removed,
    };
    const prev = prevGitStatsRef.current;
    if (
      prev !== null &&
      (prev.files !== current.files ||
        prev.added !== current.added ||
        prev.removed !== current.removed)
    ) {
      loadFile();
    }
    prevGitStatsRef.current = current;
  }, [workspace, workspaceId]);

  useEffect(() => {
    if (!contentRef.current || !content) return;
    const scrollEl = contentRef.current;
    const key = getMarkdownScrollPositionKey(workspaceId, filePath);
    const handleScroll = () => localStorage.setItem(key, scrollEl.scrollTop.toString());
    scrollEl.addEventListener('scroll', handleScroll);
    const saved = localStorage.getItem(key);
    if (saved)
      requestAnimationFrame(() => {
        scrollEl.scrollTop = parseInt(saved, 10);
      });
    return () => scrollEl.removeEventListener('scroll', handleScroll);
  }, [workspaceId, filePath, content]);

  if (loading) {
    return (
      <Frame workspaceId={workspaceId} filePath={filePath}>
        <div className="loading-state flex-1">
          <div className="spinner"></div>
          <span>Loading preview...</span>
        </div>
      </Frame>
    );
  }
  if (error) {
    return (
      <Frame workspaceId={workspaceId} filePath={filePath}>
        <div className="empty-state flex-1">
          <div className="empty-state__icon">!</div>
          <h3 className="empty-state__title">Failed to load preview</h3>
          <p className="empty-state__description">{error}</p>
          <Link to={`/diff/${workspaceId}`} className="btn btn--primary">
            Back to Diff
          </Link>
        </div>
      </Frame>
    );
  }
  return (
    <Frame
      workspaceId={workspaceId}
      filePath={filePath}
      status={notice ? { text: `Read-only: ${notice}`, error: true } : undefined}
    >
      <div className="diff-viewer-wrapper" ref={contentRef}>
        <MarkdownViewer workspaceId={workspaceId} filePath={filePath} content={content} />
      </div>
    </Frame>
  );
}
```

The old "redirect home if the workspace no longer exists" behavior now lives once, in `MarkdownPreviewPage`, covering both paths.

- [ ] **Step 5: Run the tests and the build**

Run: `./test.sh --quick && go run ./cmd/build-dashboard`
Expected: PASS; the build output lists a chunk containing `bytemd` separate from the entry chunk (the page is route-lazy, and the editor is imported only by the page). If `bytemd` appears in the entry chunk, something outside the lazy page imports the editor; remove that import.

- [ ] **Step 6: Report done**

---

### Task 12: Scenario test

**Files:**

- Create: `test/scenarios/markdown-editor.md`
- Generate: `test/scenarios/generated/markdown-editor.spec.ts`

- [ ] **Step 1: Write the scenario**

Create `test/scenarios/markdown-editor.md`:

```markdown
# Edit a Markdown file while an agent changes it

A user opens a Markdown file from a local workspace, types into it, and sees
the change land on disk without a Save button. An agent then rewrites part of
the file on disk; the editor shows the agent's change without a reload, and
when both sides change the same line the file ends up containing both edits.

## Preconditions

- The dashboard is running with a local git workspace that contains `docs/notes.md`
  with the three lines `alpha`, `beta`, `gamma`.
- No agent session is required; "the agent" is the test writing the file on disk.

## Verifications

- Opening `/diff/{workspaceId}/md/docs%2Fnotes.md` renders the editor (`data-testid="markdown-editor"`)
  with the file's text and a status of `Saved`.
- Typing ` one` at the end of the first line changes the status to `Saving…` and then `Saved`,
  and the file on disk reads `alpha one`, `beta`, `gamma`.
- Writing `alpha one`, `beta`, `gamma two` to the file on disk (atomic rename, the way `sed -i` does)
  updates the editor's third line to `gamma two` without reload, and the status stays `Saved`.
- Typing ` three` at the end of the second line while, before autosave fires, the test rewrites the
  same line on disk as `beta four`, results in one file whose second line contains both `three`
  and `four`, and the editor shows that merged line.
- The Download link points at `/api/file/{workspaceId}/docs%2Fnotes.md`.
```

- [ ] **Step 2: Generate the Playwright spec**

Invoke the `generate-scenario-tests` skill (`/generate-scenario-tests`). It reads every `test/scenarios/*.md`, so confirm it wrote `test/scenarios/generated/markdown-editor.spec.ts` and did not alter other generated specs beyond formatting. Review the generated file against these requirements:

- It creates the workspace with `createTestRepo` from `helpers.ts` and writes `docs/notes.md` before opening the page.
- Disk writes in the test use write-temp-then-rename (`fs.writeFile` to `notes.md.tmp` then `fs.rename`).
- Every wait is a Playwright locator assertion on visible state (`expect(locator).toHaveText(...)`, `expect.poll(() => fs.readFile(...))` for disk), per `docs/testing.md` rule 5. No `waitForTimeout`.
- The same-line case types, then writes disk, then asserts the merged line; it must not sleep to "let autosave fire".

- [ ] **Step 3: Run the scenario suite**

Run: `./test.sh --scenarios`
Expected: PASS for `markdown-editor.spec.ts`. On failure, artifacts land in `test/scenarios/artifacts/`; read them before changing the test.

- [ ] **Step 4: Report done**

---

### Task 13: Documentation and full gates

**Files:**

- Modify: `docs/api.md` (after the `### WS /ws/logs/fence/{id}` section)
- Modify: `docs/web.md` (Diff section, near the "Preview changed Markdown…" bullet)
- Modify: `docs/react.md` (WebSocket table, Gotchas)

- [ ] **Step 1: docs/api.md**

Add after the `/ws/logs/fence/{id}` section:

````markdown
### WS /ws/markdown/{workspaceId}/{filepath}

Bidirectional socket for the dashboard's Markdown editor. Local workspaces only. `{filepath}` is URL-encoded once, exactly as for `GET /api/file`.

Before upgrade the request must pass: the terminal socket's authentication rules; the file-jump validator (inside the workspace, exact on-disk casing, no symlink at any path component, regular file); extension `.md` or `.mdx` (case-insensitive); not ignored by the workspace's VCS. Failures return the same JSON errors and status codes as `/api/file` and `/jump`. The socket's read limit is 4 MiB.

Client → server, one message type:

```json
{
  "type": "save",
  "id": "<unique per save>",
  "base": "<text the draft was edited from>",
  "draft": "<editor text>"
}
```

Server → client, one message type:

```json
{ "type": "document", "content": "<whole file>", "revision": "sha256:<hex>", "reply": "<save id>" }
```

The first message after connect is the current file. `reply` is present only on the message answering that connection's save. Every `content` is the complete document; `revision` hashes its exact bytes.

Save semantics: under a per-file mutex the daemon reads disk; if disk still equals `base` the draft is written as is, otherwise the change from `base` to `draft` is patched onto disk with diff-match-patch (`github.com/sergi/go-diff`) and the result is written. Writes are atomic (temp file + rename) and keep the file's mode. There is no conflict state: overlapping edits land at the best fuzzy match; a hunk with no matching context is dropped and counted in the daemon log. A save whose `id` was already applied is answered with the current document without re-applying. Agent writes to the file are detected by a watch on its directory and pushed as a `document` message to every open socket.

Close reasons (WebSocket close frame text): `deleted`, `not_utf8`, `too_large` (over 1 MiB), `invalid_path`, `write_failed`, `watcher_error`, `bad_request` (bad JSON, unknown type, or a field over 1 MiB).

Limitations: a write by another process between the daemon's read and its rename is overwritten; there is no cross-process lock. A browser edit inside a region the agent rewrote is dropped when no context survives.
````

- [ ] **Step 2: docs/web.md**

Replace the bullet beginning "Preview changed Markdown, image, HTML, and Mermaid" with:

```markdown
- Preview changed image, HTML, and Mermaid (`.mmd`) files; Mermaid previews
  keep Mermaid's light node palette in both dashboard themes (raising edge contrast in
  dark mode) and support toolbar or Ctrl/Cmd-scroll zoom, drag-to-pan, and
  double-click-to-fit, with zoom and scroll position retained per workspace file;
  rendered diagrams can be opened as standalone SVGs in a new tab
- Edit Markdown (`.md`, `.mdx`) files in local workspaces at `/diff/{ws}/md/{path}`.
  The editor (ByteMD) opens side-by-side with Write-only and Preview-only toggles and
  falls back to tabs on narrow widths. Edits autosave 500 ms after the last keystroke;
  the header shows `Saving…`, `Saved`, or the disconnect reason. Agent writes to the
  open file appear live; when both sides edit, the daemon merges them into one file
  and the editor shows the result, cursor kept in place. There is no conflict view.
  Remote workspaces, files over 1 MiB, and non-UTF-8 files render read-only as before.
```

- [ ] **Step 3: docs/react.md**

In the WebSocket connections table add a row:

```markdown
| `/ws/markdown/{ws}/{path}` | Bidirectional | Markdown editor: whole-document saves in, whole-document pushes out |
```

Under "Specialized" in the technology stack add:

```markdown
- **ByteMD 1.22.0** (`bytemd`, `@bytemd/react`, `@bytemd/plugin-gfm`, MIT) — the Markdown editor, loaded only by the route-lazy `MarkdownPreviewPage`. `components/markdown/bytemdSchmuxPlugin.ts` is the whole schmux-side integration: image URL rewriting, cursor preservation on external replacement, toolbar keyboard/ARIA normalization. `lib/markdownDocument.ts` is the pure save/merge state machine; `hooks/useMarkdownDocument.ts` binds it to the socket.
```

- [ ] **Step 4: Run every gate**

From the repository root, in this order, and paste each result into the task report:

```bash
./format.sh
go run ./cmd/build-dashboard      # confirm bytemd is in its own chunk
./test.sh                         # full suite: quick + e2e + scenarios
./badcode.sh
cd assets/dashboard && npx license-checker --production --summary; cd ../..
```

Expected: every command exits 0; the license summary lists only MIT and BSD-3-Clause. If `./badcode.sh` reports `knip` unused exports in `bytemdSchmuxPlugin.ts`, the `HastRoot` export is only used by tests; keep it exported (tests count as usage) or inline the type in the test.

- [ ] **Step 5: Human visual pass**

Ask the user to open a local Markdown file in both themes and check the style guide's seven rubric points. The recipe to hand them: open any `.md` from the diff tab, toggle the theme, type a line, run `sed -i '' 's/^# /# Agent: /' <file>` in the workspace, confirm the heading updates in place while the cursor stays put, then narrow the window below 800 px and confirm ByteMD switches to Write/Preview tabs.

- [ ] **Step 6: Report done**

State the outputs of Step 4 verbatim. Do not claim the feature is done without them.
