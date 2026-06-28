package spawnlog

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

// Tailer invokes onLine for each newline-terminated line appended to path after
// startOffset. Mirrors internal/events.EventWatcher.
type Tailer struct {
	path   string
	offset int64
	onLine func([]byte)
	ctx    context.Context
	cancel context.CancelFunc
	fsw    *fsnotify.Watcher
	mu     sync.Mutex
}

func NewTailer(path string, startOffset int64, onLine func([]byte)) (*Tailer, error) {
	// The log dir is created lazily by Append on the first spawn. fsnotify cannot
	// watch a missing dir, so ensure it exists — otherwise opening /logs before
	// any spawn has happened would fail here and close the connection.
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
	ctx, cancel := context.WithCancel(context.Background())
	t := &Tailer{path: path, offset: startOffset, onLine: onLine, ctx: ctx, cancel: cancel, fsw: fsw}
	go t.run()
	return t, nil
}

func (t *Tailer) run() {
	fileName := filepath.Base(t.path)
	// Catch up on anything appended between the caller's offset snapshot and the
	// watcher being registered — those writes fire no event for this watcher, so
	// without this they stay invisible until the next append.
	t.processNewLines()
	var debounce *time.Timer
	for {
		select {
		case <-t.ctx.Done():
			if debounce != nil {
				debounce.Stop()
			}
			return
		case event, ok := <-t.fsw.Events:
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
			debounce = time.AfterFunc(100*time.Millisecond, t.processNewLines)
		case _, ok := <-t.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}

func (t *Tailer) processNewLines() {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, err := os.Open(t.path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		cp := make([]byte, len(line))
		copy(cp, line)
		t.onLine(cp)
	}
	if pos, err := f.Seek(0, io.SeekCurrent); err == nil {
		t.offset = pos
	}
}

// Stop shuts down the tailer.
func (t *Tailer) Stop() {
	t.cancel()
	t.fsw.Close()
}
