package floormanager

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/events"
	"github.com/sergeknystautas/schmux/internal/tmux"
)

// Injector is an events.EventHandler that forwards filtered status events
// into the floor manager's terminal via tmux.
type Injector struct {
	manager    *Manager
	debounceMs int
	logger     *log.Logger

	mu        sync.Mutex
	prevState map[string]string // sessionID -> last known state
	pending   []string          // buffered messages during debounce window
	timer     *time.Timer
	stopCh    chan struct{}
	stopped   bool
}

// NewInjector creates a new Injector that forwards events to the floor manager.
func NewInjector(manager *Manager, debounceMs int, logger *log.Logger) *Injector {
	return &Injector{
		manager:    manager,
		debounceMs: debounceMs,
		logger:     logger,
		prevState:  make(map[string]string),
		stopCh:     make(chan struct{}),
	}
}

// HandleEvent implements events.EventHandler. It receives status events from the
// unified event pipeline, filters them, and queues them for debounced injection.
func (inj *Injector) HandleEvent(ctx context.Context, sessionID string, raw events.RawEvent, data []byte) {
	if raw.Type != "status" {
		return
	}

	var evt events.StatusEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		inj.logger.Warn("failed to unmarshal status event", "err", err)
		return
	}

	inj.mu.Lock()
	defer inj.mu.Unlock()

	if inj.stopped {
		return
	}

	prev := inj.prevState[sessionID]
	inj.prevState[sessionID] = evt.State

	if !shouldInject(prev, evt.State) {
		return
	}

	// Look up session nickname from the manager
	nickname := inj.manager.resolveSessionName(sessionID)

	msg := FormatSignalMessage(
		nickname, prev, evt.State,
		StripControlChars(evt.Message),
		StripControlChars(evt.Intent),
		StripControlChars(evt.Blockers),
	)

	inj.pending = append(inj.pending, msg)

	// Reset debounce timer
	if inj.timer != nil {
		inj.timer.Stop()
	}
	inj.timer = time.AfterFunc(time.Duration(inj.debounceMs)*time.Millisecond, func() {
		inj.flush(ctx)
	})
}

// flush sends all pending messages to the floor manager's terminal.
func (inj *Injector) flush(ctx context.Context) {
	inj.mu.Lock()
	if len(inj.pending) == 0 || inj.stopped {
		inj.mu.Unlock()
		return
	}
	messages := inj.pending
	inj.pending = nil
	inj.mu.Unlock()

	tmuxSession := inj.manager.TmuxSession()
	if tmuxSession == "" {
		return
	}

	text := strings.Join(messages, "\n")
	if err := tmux.SendLiteral(ctx, tmuxSession, text); err != nil {
		inj.logger.Warn("failed to send signal to floor manager", "err", err)
		return
	}
	if err := tmux.SendKeys(ctx, tmuxSession, "Enter"); err != nil {
		inj.logger.Warn("failed to send Enter to floor manager", "err", err)
		return
	}

	inj.manager.IncrementInjectionCount(len(messages))
}

// Stop stops the injector and cancels any pending debounce timer.
func (inj *Injector) Stop() {
	inj.mu.Lock()
	defer inj.mu.Unlock()
	inj.stopped = true
	if inj.timer != nil {
		inj.timer.Stop()
	}
	inj.pending = nil
}

// shouldInject determines whether a state transition should be forwarded to the FM.
func shouldInject(prev, curr string) bool {
	// Skip transitions to "working" — agent is chugging along, no action needed
	if curr == "working" {
		return false
	}
	return true
}

// FormatSignalMessage formats a status event as a [SIGNAL] line for terminal injection.
func FormatSignalMessage(nickname, prev, state, message, intent, blockers string) string {
	var b strings.Builder
	b.WriteString("[SIGNAL] ")
	b.WriteString(nickname)
	b.WriteString(": ")
	if prev != "" {
		b.WriteString(prev)
		b.WriteString(" -> ")
	} else {
		b.WriteString("-> ")
	}
	b.WriteString(state)
	if message != "" {
		b.WriteString(" ")
		b.WriteString(QuoteContentField(message))
	}
	if intent != "" {
		b.WriteString(" intent=")
		b.WriteString(QuoteContentField(intent))
	}
	if blockers != "" {
		b.WriteString(" blocked=")
		b.WriteString(QuoteContentField(blockers))
	}
	return b.String()
}
