package daemon

import (
	"context"
	"path/filepath"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newChatStorageCleaner wires the chat storage cleaner with closures that
// read live config and current state. Factored out of startBackgroundJobs so
// tests can exercise the construction without spinning up a full daemon.
func newChatStorageCleaner(
	cfg *config.Config,
	st *state.State,
	schmuxDir string,
) (*chat.StorageCleaner, map[string]struct{}, string, int) {
	root := filepath.Join(schmuxDir, "chat")

	activeIDs := make(map[string]struct{})
	for _, s := range st.GetSessions() {
		activeIDs[s.ID] = struct{}{}
	}

	maxMB := cfg.GetChatStorageMaxTotalStorageMB()

	cleaner := chat.NewStorageCleaner(chat.StorageCleanerOptions{
		Root: root,
		ActiveSessions: func() map[string]struct{} {
			ids := make(map[string]struct{})
			for _, s := range st.GetSessions() {
				ids[s.ID] = struct{}{}
			}
			return ids
		},
		RetentionDays: cfg.GetChatStorageRetentionDays,
		MaxBytes: func() int64 {
			return int64(cfg.GetChatStorageMaxTotalStorageMB()) * 1024 * 1024
		},
		Now: time.Now,
		Log: nil, // assigned by the caller before scheduling
	})
	return cleaner, activeIDs, root, maxMB
}

// runChatStorageLoop drives the cleaner: one immediate pass, then one per
// tick. Returns when ctx is cancelled. The first pass runs synchronously
// inside the grace window so it cannot race a freshly disposed Restart
// transcript copy.
func runChatStorageLoop(ctx context.Context, cleaner *chat.StorageCleaner, tick time.Duration) {
	if cleaner == nil {
		return
	}
	if err := ctx.Err(); err != nil {
		return
	}
	cleaner.RunOnce(ctx)
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cleaner.RunOnce(ctx)
		}
	}
}

// attachChatStorageCleaner constructs the cleaner, installs it on the
// dashboard server, and starts the background loop. Called from the daemon's
// start path.
func attachChatStorageCleaner(
	ctx context.Context,
	cfg *config.Config,
	st *state.State,
	server chatStorageSetter,
	schmuxDir string,
	logger *log.Logger,
) {
	cleaner, _, _, _ := newChatStorageCleaner(cfg, st, schmuxDir)
	if logger != nil {
		// re-create the cleaner with a logger set so first-pass log lines
		// surface. The cleaner constructor is cheap; rebuilding here is
		// simpler than exposing a setter on the cleaner.
		cleaner = chat.NewStorageCleaner(chat.StorageCleanerOptions{
			Root:           filepath.Join(schmuxDir, "chat"),
			ActiveSessions: cleanerActiveIDs(st),
			RetentionDays:  cfg.GetChatStorageRetentionDays,
			MaxBytes: func() int64 {
				return int64(cfg.GetChatStorageMaxTotalStorageMB()) * 1024 * 1024
			},
			Now: time.Now,
			Log: logging.Sub(logger, "chat-storage"),
		})
	}
	server.SetChatStorageCleaner(cleaner)
	go runChatStorageLoop(ctx, cleaner, time.Hour)
}

func cleanerActiveIDs(st *state.State) func() map[string]struct{} {
	return func() map[string]struct{} {
		ids := make(map[string]struct{})
		for _, s := range st.GetSessions() {
			ids[s.ID] = struct{}{}
		}
		return ids
	}
}

// chatStorageSetter is the subset of *dashboard.Server we touch. Tests
// supply a fake.
type chatStorageSetter interface {
	SetChatStorageCleaner(*chat.StorageCleaner)
}
