package signal

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

const (
	signalSentinelPrefix = "__SCHMUX_SIGNAL__"
	signalSentinelSuffix = "__END__"
)

// WatcherScript returns the shell script to run in the remote watcher pane.
// The script watches the given event file for new JSON lines and emits
// sentinel-wrapped output for each appended line.
func WatcherScript(eventsFilePath string) string {
	return fmt.Sprintf(`EVENTS_FILE=%s; touch "$EVENTS_FILE"; if command -v inotifywait >/dev/null 2>&1; then tail -n0 -f "$EVENTS_FILE" | while IFS= read -r line; do echo "__SCHMUX_SIGNAL__${line}__END__"; done & TAIL_PID=$!; while inotifywait -qq -e modify "$EVENTS_FILE" 2>/dev/null; do :; done; kill $TAIL_PID 2>/dev/null; else tail -n0 -f "$EVENTS_FILE" | while IFS= read -r line; do echo "__SCHMUX_SIGNAL__${line}__END__"; done; fi`,
		shellutil.Quote(eventsFilePath))
}

// ParseSentinelOutput extracts the signal content from a sentinel-wrapped line.
// Returns empty string if the line doesn't contain a valid sentinel.
// Uses LastIndex for the end marker so that __END__ in agent messages doesn't truncate.
func ParseSentinelOutput(data string) string {
	idx := strings.Index(data, signalSentinelPrefix)
	if idx < 0 {
		return ""
	}
	start := idx + len(signalSentinelPrefix)
	endIdx := strings.LastIndex(data, signalSentinelSuffix)
	if endIdx < 0 || endIdx <= start {
		return ""
	}
	return data[start:endIdx]
}

// RemoteSignalWatcher processes output events from a remote watcher pane.
// It parses sentinel-wrapped JSON event lines and invokes the callback on changes.
type RemoteSignalWatcher struct {
	sessionID   string
	callback    func(Signal)
	logger      *log.Logger
	mu          sync.Mutex
	lastState   string
	lastMessage string
}

// NewRemoteSignalWatcher creates a watcher that parses %output events from
// a remote watcher pane.
func NewRemoteSignalWatcher(sessionID string, callback func(Signal)) *RemoteSignalWatcher {
	return &RemoteSignalWatcher{
		sessionID: sessionID,
		callback:  callback,
	}
}

// SetLogger sets the logger for this watcher.
func (w *RemoteSignalWatcher) SetLogger(l *log.Logger) {
	w.logger = l
}

// ProcessOutput handles a chunk of output from the watcher pane.
// Extracts sentinel-framed JSON event lines and invokes callback on change.
func (w *RemoteSignalWatcher) ProcessOutput(data string) {
	content := ParseSentinelOutput(data)
	if content == "" {
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}

	// Parse the JSON event line directly (avoids importing event package
	// which would create an import cycle: signal → event → signal).
	var evt struct {
		Type     string `json:"type"`
		State    string `json:"state"`
		Message  string `json:"message"`
		Intent   string `json:"intent"`
		Blockers string `json:"blockers"`
	}
	if err := json.Unmarshal([]byte(content), &evt); err != nil {
		return
	}
	if evt.Type != "status" || !IsValidState(evt.State) {
		return
	}

	sig := &Signal{
		State:     evt.State,
		Message:   evt.Message,
		Intent:    evt.Intent,
		Blockers:  evt.Blockers,
		Timestamp: time.Now(),
	}

	w.mu.Lock()
	changed := sig.State != w.lastState || sig.Message != w.lastMessage
	if changed {
		w.lastState = sig.State
		w.lastMessage = sig.Message
	}
	w.mu.Unlock()

	if changed {
		w.callback(*sig)
	}
}
