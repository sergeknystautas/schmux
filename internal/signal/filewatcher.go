package signal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileWatcher watches a signal file for changes and invokes a callback
// when the signal content changes. Deduplication is a simple string
// comparison of the file content.
type FileWatcher struct {
	sessionID   string
	filePath    string
	callback    func(Signal)
	watcher     *fsnotify.Watcher
	mu          sync.Mutex
	lastContent string
	stopCh      chan struct{}
	doneCh      chan struct{}
}

// NewFileWatcher creates and starts a file watcher for the given signal file.
// The directory containing filePath must exist. The file itself may not exist yet.
func NewFileWatcher(sessionID, filePath string, callback func(Signal)) (*FileWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	dir := filepath.Dir(filePath)
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to watch directory %s: %w", dir, err)
	}

	fw := &FileWatcher{
		sessionID: sessionID,
		filePath:  filePath,
		callback:  callback,
		watcher:   watcher,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}

	go fw.run()
	return fw, nil
}

// Stop terminates the file watcher.
func (fw *FileWatcher) Stop() {
	select {
	case <-fw.stopCh:
		return
	default:
		close(fw.stopCh)
	}
	fw.watcher.Close()
	<-fw.doneCh
}

// ReadCurrent reads the current signal file content and returns the parsed signal.
// Does not invoke the callback. Used for daemon restart recovery.
func (fw *FileWatcher) ReadCurrent() *Signal {
	content, err := os.ReadFile(fw.filePath)
	if err != nil {
		return nil
	}
	s := strings.TrimSpace(string(content))
	if s == "" {
		return nil
	}
	fw.mu.Lock()
	fw.lastContent = s
	fw.mu.Unlock()
	return ParseSignalFile(s)
}

func (fw *FileWatcher) run() {
	defer close(fw.doneCh)

	var debounceTimer *time.Timer
	var debounceCh <-chan time.Time

	fileName := filepath.Base(fw.filePath)

	for {
		select {
		case <-fw.stopCh:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-fw.watcher.Events:
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
			debounceTimer = time.NewTimer(100 * time.Millisecond)
			debounceCh = debounceTimer.C

		case <-debounceCh:
			debounceCh = nil
			fw.checkFile()

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			fmt.Printf("[signal] %s - fsnotify error: %v\n", fw.sessionID, err)
		}
	}
}

func (fw *FileWatcher) checkFile() {
	content, err := os.ReadFile(fw.filePath)
	if err != nil {
		return
	}
	s := strings.TrimSpace(string(content))
	if s == "" {
		return
	}

	fw.mu.Lock()
	changed := s != fw.lastContent
	if changed {
		fw.lastContent = s
	}
	fw.mu.Unlock()

	if !changed {
		return
	}

	sig := ParseSignalFile(s)
	if sig == nil {
		fmt.Printf("[signal] %s - invalid signal file content: %q\n", fw.sessionID, s)
		return
	}

	fw.callback(*sig)
}
