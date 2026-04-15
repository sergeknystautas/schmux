package config

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/fsnotify/fsnotify"
)

// selfWriteGrace is the minimum time after a Save() completes during which
// fsnotify events are treated as self-writes and ignored.
const selfWriteGrace = 750 * time.Millisecond

// configDebounce is how long to wait after the last write event before
// reloading, so editors that perform multi-step write sequences (e.g.
// vim backup→rename) don't trigger multiple reloads.
const configDebounce = 500 * time.Millisecond

// ConfigWatcher watches the config file for external changes and reloads
// the in-memory Config when the file is modified outside of Save().
type ConfigWatcher struct {
	cfg       *Config
	logger    *log.Logger
	broadcast func() // called after successful reload
	stopCh    chan struct{}
	stopOnce  sync.Once
}

// NewConfigWatcher creates a new ConfigWatcher. The broadcast function is
// called after a successful reload to notify the dashboard.
func NewConfigWatcher(cfg *Config, logger *log.Logger, broadcast func()) *ConfigWatcher {
	return &ConfigWatcher{
		cfg:       cfg,
		logger:    logger,
		broadcast: broadcast,
		stopCh:    make(chan struct{}),
	}
}

// Start begins watching the config file's parent directory for changes.
// Watching the directory (not the file) handles atomic writes (rename)
// that may remove the original inode — this is the same approach used
// by git_watcher.go and events/watcher.go.
func (w *ConfigWatcher) Start() error {
	configPath := w.cfg.FilePath()
	if configPath == "" {
		return fmt.Errorf("config path not set")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create fsnotify watcher: %w", err)
	}

	dir := filepath.Dir(configPath)
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return fmt.Errorf("watch config directory: %w", err)
	}

	go w.loop(watcher, configPath)
	return nil
}

func (w *ConfigWatcher) loop(watcher *fsnotify.Watcher, configPath string) {
	defer watcher.Close()

	var debounce *time.Timer
	baseName := filepath.Base(configPath)

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Only care about the config file itself.
			if filepath.Base(event.Name) != baseName {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				// Suppress events caused by our own Save(). Check at event
				// arrival time (not after debounce) so the grace window doesn't
				// need to be longer than the debounce period.
				if w.cfg.TimeSinceLastSave() < selfWriteGrace {
					if w.logger != nil {
						w.logger.Debug("ignoring config change from self-write")
					}
					continue
				}
				// Debounce: editors often write multiple times in quick succession.
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(configDebounce, func() {
					w.reload()
				})
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			if w.logger != nil {
				w.logger.Warn("config watcher error", "err", err)
			}

		case <-w.stopCh:
			if debounce != nil {
				debounce.Stop()
			}
			return
		}
	}
}

func (w *ConfigWatcher) reload() {
	if err := w.cfg.Reload(); err != nil {
		if w.logger != nil {
			w.logger.Warn("failed to reload config from disk", "err", err)
		}
		return
	}
	if w.logger != nil {
		w.logger.Info("config reloaded from external change")
	}
	if w.broadcast != nil {
		w.broadcast()
	}
}

// Stop signals the watcher to exit. Safe to call multiple times.
func (w *ConfigWatcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
	})
}
