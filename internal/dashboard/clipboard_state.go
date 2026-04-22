package dashboard

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/session"
)

// clipboardDebounceWindow is the wait between the last OSC 52 emit and the
// broadcast. TUIs that are mid-render often emit several OSC 52 frames in
// quick succession; coalescing them into one banner keeps the UI calm.
//
// clipboardTTL bounds how long a single pending request can sit unanswered
// before we drop it as stale. This is overridden in tests via the package
// variable to avoid 5-minute delays.
//
// clipboardDedupWindow is the cross-session content+timestamp dedup window.
// All schmux sessions on a single daemon share one tmux socket, and
// %paste-buffer-changed is server-scoped — every control-mode client receives
// the same notification when one TUI writes to the paste buffer. Without
// dedup, N sessions produce N banners for one copy. The same window also
// catches copy-mode emitting both OSC 52 (caught by the byte extractor) and
// set-buffer (caught by paste-buffer-changed) for one user action.
//
// Set equal to the per-session debounce window: any duplicate within the
// same UI heartbeat is suppressed.
//
// clipboardPromptSuppressionTTL bounds how long a spawn-time prompt remains
// in the workspace-scoped suppression registry. The motivating round-trip
// (Claude Code reads its own argv and OSC 52s the same string back through
// the system clipboard) happens within seconds of startup; if the agent
// never echoes within this window we lazily evict the entry so a later
// legitimate yank of identical content can still surface a banner.
// Package-level var so tests can override without sleeping for a real minute.
var (
	clipboardDebounceWindow       = 200 * time.Millisecond
	clipboardDedupWindow          = 200 * time.Millisecond
	clipboardTTL                  = 5 * time.Minute
	clipboardPromptSuppressionTTL = 60 * time.Second
)

// pendingEntry holds the latest ClipboardRequest for one session, plus the
// timers governing debounce-then-broadcast and post-broadcast TTL.
//
// gen is a monotonic counter bumped on every onRequest call. The debounce
// and TTL timer closures capture the gen value at the moment they are armed
// and verify it inside the lock before acting. This defends against the
// stale-callback race: time.AfterFunc.Stop() returns false (and does NOT
// abort) if the callback is already running or queued waiting on the lock,
// so a re-armed timer can otherwise produce a duplicate broadcast.
type pendingEntry struct {
	req       session.ClipboardRequest
	requestID string
	gen       uint64
	debounce  *time.Timer
	ttl       *time.Timer
	// createdAt records when this entry was first created (or re-armed) so
	// onRequest can apply cross-session content+timestamp dedup. See
	// clipboardDedupWindow for the rationale.
	createdAt time.Time
}

// clipboardBroadcaster is satisfied by *Server. Defined as an interface so
// clipboard_state_test.go can swap in a capturing fake.
type clipboardBroadcaster interface {
	BroadcastClipboardRequest(ev contracts.ClipboardRequestEvent)
	BroadcastClipboardCleared(ev contracts.ClipboardClearedEvent)
}

// clipboardState is the per-server pendingClipboard map: sessionID -> latest
// pending OSC 52 request awaiting user approval. New OSC 52 emits restart a
// 200ms debounce; once it fires we broadcast clipboardRequest and start a
// 5-minute TTL. ack/clear stops both timers and broadcasts clipboardCleared.
//
// suppressedPrompts is the workspace-scoped registry of spawn-time prompts
// to drop. Daemon-wide rather than per-session because tmux's
// %paste-buffer-changed notification is server-scoped: when an agent emits
// OSC 52 (or `tmux load-buffer`) for its argv prompt, every control-mode
// client on the daemon socket receives the notification, so suppression
// must apply across sessions, not just the source session.
type clipboardState struct {
	mu                       sync.Mutex
	pending                  map[string]*pendingEntry
	suppressedPrompts        map[string]time.Time
	broadcaster              clipboardBroadcaster
	logger                   *log.Logger
	promptSuppressionCounter atomic.Int64
}

func newClipboardState(b clipboardBroadcaster, logger *log.Logger) *clipboardState {
	return &clipboardState{
		pending:           map[string]*pendingEntry{},
		suppressedPrompts: map[string]time.Time{},
		broadcaster:       b,
		logger:            logger,
	}
}

// RegisterSpawnPrompt records a spawn-time prompt in the workspace-scoped
// suppression registry. Subsequent ClipboardRequests whose Text exactly
// matches an unexpired registered prompt are dropped silently — the prompt
// was just typed into the spawn form by the user, and the agent's
// argv-prompt round-trip (Claude Code's pattern of reading its own argv
// and OSC 52ing the same string back through the system clipboard) is
// not a real clipboard event the user needs to ack.
//
// Empty prompts are a no-op so we don't accidentally arm suppression to
// swallow the next OSC 52 of "". Re-registering the same prompt refreshes
// its TTL. Lazy expiry only — entries are removed inline in onRequest the
// next time the prompt comes back for matching, so we don't need a sweeper
// goroutine.
func (cs *clipboardState) RegisterSpawnPrompt(text string) {
	if text == "" {
		return
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.suppressedPrompts[text] = time.Now().Add(clipboardPromptSuppressionTTL)
}

// PromptSuppressionCount returns the count of clipboard requests dropped
// because they matched a registered spawn prompt. Exposed for diagnostics.
func (cs *clipboardState) PromptSuppressionCount() int64 {
	return cs.promptSuppressionCounter.Load()
}

// onRequest is the entry point from the per-session subscriber goroutine.
// Coalesces rapid emits via a 200ms debounce: only the latest text is
// broadcast. A new requestID is minted on each call so the frontend can
// stale-check on ack.
//
// Spawn-prompt suppression runs first: if req.Text matches a prompt
// registered via RegisterSpawnPrompt and that registration has not expired,
// the request is dropped silently. Workspace-scoped (not per-session) so
// every session that receives tmux's shared %paste-buffer-changed
// notification for the same content has its banner suppressed.
//
// Cross-session dedup runs second: if any other session has a pending entry
// with the same Text within clipboardDedupWindow, we drop this request
// silently. This collapses the N notifications generated by tmux's shared
// %paste-buffer-changed (one per control-mode client on the daemon socket),
// and also prevents copy-mode from producing two banners (OSC 52 byte path
// + paste-buffer-changed path) for one user action.
func (cs *clipboardState) onRequest(req session.ClipboardRequest) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	now := time.Now()

	if expiry, ok := cs.suppressedPrompts[req.Text]; ok {
		if now.Before(expiry) {
			cs.promptSuppressionCounter.Add(1)
			return
		}
		// Lazily evict the expired entry — saves us a sweeper goroutine.
		delete(cs.suppressedPrompts, req.Text)
	}

	for sid, existing := range cs.pending {
		if sid == req.SessionID {
			continue // per-session debounce handles same-session dedup
		}
		if existing.req.Text == req.Text && now.Sub(existing.createdAt) < clipboardDedupWindow {
			// Duplicate from another session within the dedup window. Drop
			// silently — the existing entry's debounce/broadcast covers it.
			return
		}
	}

	entry, ok := cs.pending[req.SessionID]
	if ok && entry.debounce != nil {
		entry.debounce.Stop()
	}
	if !ok {
		entry = &pendingEntry{}
		cs.pending[req.SessionID] = entry
	}
	entry.req = req
	entry.requestID = uuid.New().String()
	entry.createdAt = now
	// Bump generation so any debounce/TTL callback already in flight (or
	// queued waiting on cs.mu) sees a stale value and bails out. See
	// pendingEntry.gen for the rationale.
	entry.gen++
	gen := entry.gen

	sid := req.SessionID
	entry.debounce = time.AfterFunc(clipboardDebounceWindow, func() {
		cs.fireBroadcast(sid, gen)
	})
}

// fireBroadcast is the debounce-timer callback. It promotes the pending entry
// to "broadcast" state by starting the TTL timer and emitting the event.
// The broadcast call happens outside the lock so a slow client doesn't stall
// other onRequest/clear calls.
//
// gen is the generation captured when the debounce timer was armed. If the
// entry has been re-armed since (or removed entirely), gen no longer matches
// and we abort — preventing a stale callback that was already queued behind
// the lock from emitting a duplicate broadcast.
func (cs *clipboardState) fireBroadcast(sessionID string, gen uint64) {
	cs.mu.Lock()
	entry, ok := cs.pending[sessionID]
	if !ok || entry.gen != gen {
		cs.mu.Unlock()
		return
	}
	if entry.ttl != nil {
		entry.ttl.Stop()
	}
	ttlGen := entry.gen
	entry.ttl = time.AfterFunc(clipboardTTL, func() {
		cs.fireTTL(sessionID, ttlGen)
	})
	ev := contracts.ClipboardRequestEvent{
		Type:                 "clipboardRequest",
		SessionID:            sessionID,
		RequestID:            entry.requestID,
		Text:                 entry.req.Text,
		ByteCount:            entry.req.ByteCount,
		StrippedControlChars: entry.req.StrippedControlChars,
	}
	cs.mu.Unlock()
	cs.broadcaster.BroadcastClipboardRequest(ev)
}

// fireTTL is the TTL-timer callback. Like fireBroadcast it verifies the
// captured generation against the current entry to defend against stale
// callbacks that were already queued behind cs.mu when a new onRequest
// re-armed the timers. Duplicate clipboardCleared is mostly benign, but
// consistency keeps the contract simple.
func (cs *clipboardState) fireTTL(sessionID string, gen uint64) {
	cs.mu.Lock()
	entry, ok := cs.pending[sessionID]
	if !ok || entry.gen != gen {
		cs.mu.Unlock()
		return
	}
	cs.mu.Unlock()
	cs.clear(sessionID, "")
}

// clear removes the pending entry for a session and emits clipboardCleared.
// Called from the HTTP ack handler, the TTL timer, and on session dispose.
//
// requestID semantics:
//   - "" matches anything (used by TTL and session-dispose paths)
//   - non-empty must match the current entry's requestID; otherwise we treat
//     the call as stale and return false (lets the HTTP handler distinguish
//     "ok" vs "stale" for the client).
//
// Returns true if a matching entry was actually cleared.
func (cs *clipboardState) clear(sessionID, requestID string) bool {
	cs.mu.Lock()
	entry, ok := cs.pending[sessionID]
	if !ok {
		cs.mu.Unlock()
		return false
	}
	if requestID != "" && entry.requestID != requestID {
		cs.mu.Unlock()
		return false
	}
	if entry.debounce != nil {
		entry.debounce.Stop()
	}
	if entry.ttl != nil {
		entry.ttl.Stop()
	}
	clearedRequestID := entry.requestID
	delete(cs.pending, sessionID)
	cs.mu.Unlock()

	cs.broadcaster.BroadcastClipboardCleared(contracts.ClipboardClearedEvent{
		Type:      "clipboardCleared",
		SessionID: sessionID,
		RequestID: clearedRequestID,
	})
	return true
}

// snapshot returns the current set of pending clipboard requests as a slice
// of ClipboardRequestEvents, used by the WS-reconnect rehydration path.
func (cs *clipboardState) snapshot() []contracts.ClipboardRequestEvent {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	out := make([]contracts.ClipboardRequestEvent, 0, len(cs.pending))
	for sid, entry := range cs.pending {
		out = append(out, contracts.ClipboardRequestEvent{
			Type:                 "clipboardRequest",
			SessionID:            sid,
			RequestID:            entry.requestID,
			Text:                 entry.req.Text,
			ByteCount:            entry.req.ByteCount,
			StrippedControlChars: entry.req.StrippedControlChars,
		})
	}
	return out
}
