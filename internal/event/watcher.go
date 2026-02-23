package event

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const eventDebounce = 100 * time.Millisecond

// Handler is a callback for event dispatch.
type Handler func(sessionID string, event Event)

// EventWatcher watches a session's event file for appended lines and dispatches
// them to registered handlers by event type. Unlike signal.FileWatcher which
// re-reads the whole file on each change, EventWatcher tracks the file offset
// and only reads new lines.
type EventWatcher struct {
	path      string
	sessionID string
	offset    int64
	handlers  map[string][]Handler
	watcher   *fsnotify.Watcher
	mu        sync.Mutex
	stopOnce  sync.Once
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// NewEventWatcher creates an event watcher for the given event file.
// The directory containing the file must exist. The file itself may not exist yet.
// Call Subscribe() to register handlers, then Start() to begin processing.
func NewEventWatcher(sessionID, filePath string) (*EventWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	dir := filepath.Dir(filePath)
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to watch directory %s: %w", dir, err)
	}

	ew := &EventWatcher{
		path:      filePath,
		sessionID: sessionID,
		handlers:  make(map[string][]Handler),
		watcher:   watcher,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
	return ew, nil
}

// Subscribe registers a handler for an event type.
// Must be called before Start().
func (ew *EventWatcher) Subscribe(eventType string, handler Handler) {
	ew.mu.Lock()
	defer ew.mu.Unlock()
	ew.handlers[eventType] = append(ew.handlers[eventType], handler)
}

// Start begins processing filesystem events and dispatching to handlers.
// Must be called after all Subscribe() calls.
func (ew *EventWatcher) Start() {
	go ew.run()
}

// Stop terminates the event watcher. Safe to call concurrently.
func (ew *EventWatcher) Stop() {
	ew.stopOnce.Do(func() {
		close(ew.stopCh)
		ew.watcher.Close()
	})
	<-ew.doneCh
}

// ReadCurrent scans the event file for the last "status" event.
// Used for daemon restart recovery. Does not invoke handlers.
// Updates the internal offset to EOF.
func (ew *EventWatcher) ReadCurrent() *Event {
	f, err := os.Open(ew.path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var lastStatus *Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		e, err := ParseEvent(line)
		if err != nil {
			continue
		}
		if e.Type == "status" {
			evt := e // copy
			lastStatus = &evt
		}
	}

	// Update offset to EOF
	info, err := f.Stat()
	if err == nil {
		ew.mu.Lock()
		ew.offset = info.Size()
		ew.mu.Unlock()
	}

	return lastStatus
}

func (ew *EventWatcher) run() {
	defer close(ew.doneCh)

	var debounceTimer *time.Timer
	var debounceCh <-chan time.Time

	fileName := filepath.Base(ew.path)

	for {
		select {
		case <-ew.stopCh:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-ew.watcher.Events:
			if !ok {
				return
			}
			if filepath.Base(event.Name) != fileName {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.NewTimer(eventDebounce)
			debounceCh = debounceTimer.C

		case <-debounceCh:
			debounceCh = nil
			ew.readNewLines()

		case err, ok := <-ew.watcher.Errors:
			if !ok {
				return
			}
			fmt.Printf("[event] %s - fsnotify error: %v\n", ew.sessionID, err)
		}
	}
}

// readNewLines reads lines appended since the last read and dispatches them.
func (ew *EventWatcher) readNewLines() {
	ew.mu.Lock()
	currentOffset := ew.offset
	ew.mu.Unlock()

	f, err := os.Open(ew.path)
	if err != nil {
		return
	}
	defer f.Close()

	// Seek to last known position
	if currentOffset > 0 {
		if _, err := f.Seek(currentOffset, 0); err != nil {
			return
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var events []Event
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		e, err := ParseEvent(line)
		if err != nil {
			fmt.Printf("[event] %s - skipping malformed event: %v\n", ew.sessionID, err)
			continue
		}
		events = append(events, e)
	}

	// Update offset to current position
	newOffset, err := f.Seek(0, 2) // seek to end
	if err == nil {
		ew.mu.Lock()
		ew.offset = newOffset
		ew.mu.Unlock()
	}

	// Dispatch events to handlers (outside lock)
	ew.mu.Lock()
	handlersCopy := make(map[string][]Handler, len(ew.handlers))
	for k, v := range ew.handlers {
		handlersCopy[k] = v
	}
	ew.mu.Unlock()

	for _, e := range events {
		if handlers, ok := handlersCopy[e.Type]; ok {
			for _, h := range handlers {
				h(ew.sessionID, e)
			}
		}
	}
}
