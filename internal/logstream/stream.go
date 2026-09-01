// Package logstream owns one bidirectional read of an append-only log file.
// A Stream is created per Logs WebSocket connection; it is the sole reader of
// file offsets for that connection and the only writer of the History / Live
// split the dashboard surfaces to the browser.
package logstream

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Page is the public result of a backward read. Before never leaves the
// package; the connection's historyBefore is updated internally.
type Page struct {
	Lines   [][]byte // newest first
	HasMore bool
}

// Stream is the connection-scoped owner of historyBefore and liveOffset and
// the live-tail watcher. Both positions are private and never exposed.
type Stream struct {
	path     string
	pageSize int

	mu            sync.Mutex
	historyBefore int64
	liveOffset    int64

	onAppend func([]byte)
	onError  func(error)

	ctx       context.Context
	cancel    context.CancelFunc
	fsw       *fsnotify.Watcher
	closeOnce sync.Once
}

// New creates one Stream for the given log file.
//
//   - pageSize must be > 0; the same value is used for every LoadOlder call.
//   - onAppend is called for each non-empty line appended to the file after
//     the snapshot offset. The bytes are owned by Stream and must not be
//     retained past the callback.
//   - onError is called for non-NotExist open/seek/scan failures during the
//     live tail. History-read failures are reported through LoadOlder, not
//     onError, so the retry path stays inside the request.
func New(path string, pageSize int, onAppend func([]byte), onError func(error)) (*Stream, error) {
	if pageSize <= 0 {
		return nil, fmt.Errorf("logstream: pageSize must be > 0, got %d", pageSize)
	}
	if onAppend == nil {
		onAppend = func([]byte) {}
	}
	if onError == nil {
		onError = func(error) {}
	}

	// Ensure the parent dir exists; fsnotify cannot watch a missing dir, and
	// the Logs page may open before any spawn has happened.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fsw.Add(filepath.Dir(path)); err != nil {
		fsw.Close()
		return nil, err
	}

	// Snapshot the file size once. Missing files are valid (the tailer will
	// deliver the file once it appears).
	var snapshot int64
	info, err := os.Stat(path)
	if err == nil {
		snapshot = info.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		fsw.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &Stream{
		path:          path,
		pageSize:      pageSize,
		historyBefore: snapshot,
		liveOffset:    snapshot,
		onAppend:      onAppend,
		onError:       onError,
		ctx:           ctx,
		cancel:        cancel,
		fsw:           fsw,
	}
	go s.run()
	return s, nil
}

// LoadOlder returns the next page of older history. The page is newest-first
// and contains up to pageSize non-empty lines. HasMore is true when at least
// one additional non-empty line exists before the returned Before. A failed
// read leaves historyBefore unchanged so the next Retry request reads the same
// bytes. When history is exhausted, historyBefore is parked at 0 so further
// requests return an empty terminal page.
func (s *Stream) LoadOlder() (Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	page, err := readPageBefore(s.path, s.historyBefore, s.pageSize)
	if err != nil {
		return Page{}, err
	}
	if page.HasMore {
		s.historyBefore = page.Before
	} else {
		s.historyBefore = 0
	}
	return Page{Lines: page.Lines, HasMore: page.HasMore}, nil
}

// Close stops the live tailer and closes the watcher. Idempotent.
func (s *Stream) Close() {
	s.closeOnce.Do(func() {
		s.cancel()
		s.fsw.Close()
	})
}

// run drains writes between snapshot and watcher startup, then forwards
// fsnotify events through processNewNewlines with a debounce so multiple flushes
// within the same write burst collapse into a single scan.
func (s *Stream) run() {
	fileName := filepath.Base(s.path)
	s.processNewLines()

	var debounce *time.Timer
	for {
		select {
		case <-s.ctx.Done():
			if debounce != nil {
				debounce.Stop()
			}
			return
		case event, ok := <-s.fsw.Events:
			if !ok {
				if s.ctx.Err() == nil {
					s.onError(errors.New("logstream: filesystem watcher events closed"))
				}
				return
			}
			if filepath.Base(event.Name) != fileName {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(100*time.Millisecond, s.processNewLines)
		case err, ok := <-s.fsw.Errors:
			if !ok {
				if s.ctx.Err() == nil {
					s.onError(errors.New("logstream: filesystem watcher errors closed"))
				}
				return
			}
			s.onError(err)
			return
		}
	}
}

// processNewLines reads appended lines from liveOffset to EOF and dispatches
// each non-empty line to onAppend. Only liveOffset moves.
func (s *Stream) processNewLines() {
	// Cancel check first so a queued debounce after Close never fires.
	if s.ctx.Err() != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		s.onError(err)
		return
	}
	defer f.Close()

	if _, err := f.Seek(s.liveOffset, io.SeekStart); err != nil {
		s.onError(err)
		return
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)
	for scanner.Scan() {
		if s.ctx.Err() != nil {
			return
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		cp := make([]byte, len(line))
		copy(cp, line)
		s.onAppend(cp)
	}
	if err := scanner.Err(); err != nil {
		// Errors here are open/scan errors, not EOF — report them.
		s.onError(err)
		return
	}
	if pos, err := f.Seek(0, io.SeekCurrent); err == nil {
		s.liveOffset = pos
	}
}
