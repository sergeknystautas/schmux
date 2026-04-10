package events

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// EventWatcher monitors a per-session event file and dispatches events to handlers.
type EventWatcher struct {
	path      string
	sessionID string
	offset    int64
	handlers  map[string][]EventHandler
	ctx       context.Context
	cancel    context.CancelFunc
	fsw       *fsnotify.Watcher
	mu        sync.Mutex
}

// NewEventWatcher creates a watcher for the given event file.
// handlers maps event type strings to slices of handlers.
func NewEventWatcher(path, sessionID string, handlers map[string][]EventHandler) (*EventWatcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(path)
	if err := fsw.Add(dir); err != nil {
		fsw.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	w := &EventWatcher{
		path:      path,
		sessionID: sessionID,
		handlers:  handlers,
		ctx:       ctx,
		cancel:    cancel,
		fsw:       fsw,
	}

	// Set initial offset to current file size (skip existing content)
	if info, err := os.Stat(path); err == nil {
		w.offset = info.Size()
	}

	go w.run()
	return w, nil
}

func (w *EventWatcher) run() {
	fileName := filepath.Base(w.path)
	var debounce *time.Timer

	for {
		select {
		case <-w.ctx.Done():
			if debounce != nil {
				debounce.Stop()
			}
			return
		case event, ok := <-w.fsw.Events:
			if !ok {
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
			debounce = time.AfterFunc(100*time.Millisecond, func() {
				w.processNewLines()
			})
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}

func (w *EventWatcher) processNewLines() {
	w.mu.Lock()
	defer w.mu.Unlock()

	f, err := os.Open(w.path)
	if err != nil {
		return
	}
	defer f.Close()

	if _, err := f.Seek(w.offset, io.SeekStart); err != nil {
		return
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		raw, err := ParseRawEvent(line)
		if err != nil {
			continue
		}
		handlers, ok := w.handlers[raw.Type]
		if !ok || len(handlers) == 0 {
			continue
		}
		dataCopy := make([]byte, len(line))
		copy(dataCopy, line)
		for _, h := range handlers {
			h.HandleEvent(w.ctx, w.sessionID, raw, dataCopy)
		}
	}

	pos, err := f.Seek(0, io.SeekCurrent)
	if err == nil {
		w.offset = pos
	}
}

// Stop shuts down the watcher.
func (w *EventWatcher) Stop() {
	w.cancel()
	w.fsw.Close()
}
