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
	rev := Revision(b)
	if rev != d.lastRevision {
		// Either this is the first subscriber, or the file changed since the
		// document last read it and the watcher's reread has not run yet.
		// Existing subscribers get the new content now; recording the hash
		// here is what makes the pending reread drop it as already-sent,
		// rather than as an echo of our own write.
		d.lastRevision = rev
		d.broadcastLocked(string(b), rev, nil, "")
	}
	d.subscribers[c] = struct{}{}
	d.sendLocked(c, contracts.MarkdownDocument{Type: "document", Content: string(b), Revision: rev})
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

// writeAtomic is used by Save in Task 4.
func (d *Document) writeAtomic(b []byte, mode os.FileMode) error {
	return fileutil.AtomicWriteFile(d.path, b, mode)
}

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
