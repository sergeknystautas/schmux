package signal

import (
	"fmt"
	"strings"
	"sync"

	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

const (
	signalSentinelPrefix = "__SCHMUX_SIGNAL__"
	signalSentinelSuffix = "__END__"
)

// WatcherScript returns the shell script to run in the remote watcher pane.
// The script watches the given file for changes and emits sentinel-wrapped
// output when the content changes.
func WatcherScript(statusFilePath string) string {
	return fmt.Sprintf(`STATUS_FILE=%s; LAST=""; check() { if [ -f "$STATUS_FILE" ]; then CURRENT=$(cat "$STATUS_FILE" 2>/dev/null); if [ "$CURRENT" != "$LAST" ]; then LAST="$CURRENT"; echo "__SCHMUX_SIGNAL__${CURRENT}__END__"; fi; fi; }; if command -v inotifywait >/dev/null 2>&1; then check; while inotifywait -qq -e modify -e create "$STATUS_FILE" 2>/dev/null; do sleep 0.1; check; done; else while true; do check; sleep 2; done; fi`,
		shellutil.Quote(statusFilePath))
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
// It parses sentinel-wrapped signal lines and invokes the callback on changes.
type RemoteSignalWatcher struct {
	sessionID   string
	callback    func(Signal)
	mu          sync.Mutex
	lastContent string
}

// NewRemoteSignalWatcher creates a watcher that parses %output events from
// a remote watcher pane.
func NewRemoteSignalWatcher(sessionID string, callback func(Signal)) *RemoteSignalWatcher {
	return &RemoteSignalWatcher{
		sessionID: sessionID,
		callback:  callback,
	}
}

// ProcessOutput handles a chunk of output from the watcher pane.
// Extracts sentinel-framed signals and invokes callback on change.
func (w *RemoteSignalWatcher) ProcessOutput(data string) {
	content := ParseSentinelOutput(data)
	if content == "" {
		return
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return
	}

	w.mu.Lock()
	changed := content != w.lastContent
	if changed {
		w.lastContent = content
	}
	w.mu.Unlock()

	if !changed {
		return
	}

	sig := ParseSignalFile(content)
	if sig == nil {
		fmt.Printf("[signal] %s - invalid remote signal content: %q\n", w.sessionID, content)
		return
	}

	w.callback(*sig)
}
