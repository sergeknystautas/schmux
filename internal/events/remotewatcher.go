package events

import (
	"context"
	"strings"
	"sync"
)

const (
	sentinelStart = "__SCHMUX_SIGNAL__"
	sentinelEnd   = "__END__"
)

// RemoteWatcherScript generates the shell script to run on the remote host.
// It streams new JSONL lines from the event file, wrapped in sentinel markers.
func RemoteWatcherScript(eventsFilePath string) string {
	return `EVENTS_FILE='` + eventsFilePath + `'; ` +
		`if [ -f "$EVENTS_FILE" ]; then ` +
		`while IFS= read -r line; do echo "` + sentinelStart + `${line}` + sentinelEnd + `"; done < "$EVENTS_FILE"; ` +
		`fi; ` +
		`touch "$EVENTS_FILE"; ` +
		`tail -f -n 0 "$EVENTS_FILE" 2>/dev/null | while IFS= read -r line; do ` +
		`echo "` + sentinelStart + `${line}` + sentinelEnd + `"; ` +
		`done`
}

// RemoteEventWatcher processes sentinel-wrapped event output from a remote host.
type RemoteEventWatcher struct {
	sessionID string
	handlers  map[string][]EventHandler
	mu        sync.Mutex
	lastTs    string // for deduplication
}

// NewRemoteEventWatcher creates a remote event watcher.
func NewRemoteEventWatcher(sessionID string, handlers map[string][]EventHandler) *RemoteEventWatcher {
	return &RemoteEventWatcher{
		sessionID: sessionID,
		handlers:  handlers,
	}
}

// ProcessOutput extracts sentinel-wrapped events from control mode output.
func (w *RemoteEventWatcher) ProcessOutput(data string) {
	content := ParseSentinelContent(data)
	if content == "" {
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}

	raw, err := ParseRawEvent([]byte(content))
	if err != nil {
		return
	}

	// Dedup by timestamp
	w.mu.Lock()
	if raw.Ts != "" && raw.Ts == w.lastTs {
		w.mu.Unlock()
		return
	}
	if raw.Ts != "" {
		w.lastTs = raw.Ts
	}
	w.mu.Unlock()

	handlers, ok := w.handlers[raw.Type]
	if !ok || len(handlers) == 0 {
		return
	}

	dataCopy := []byte(content)
	for _, h := range handlers {
		h.HandleEvent(context.Background(), w.sessionID, raw, dataCopy)
	}
}

// ParseSentinelContent extracts content between sentinel markers.
func ParseSentinelContent(data string) string {
	start := strings.Index(data, sentinelStart)
	if start == -1 {
		return ""
	}
	start += len(sentinelStart)
	end := strings.LastIndex(data, sentinelEnd)
	if end == -1 || end <= start {
		return ""
	}
	return data[start:end]
}
