package chat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/events"
)

const (
	pollInterval     = 50 * time.Millisecond
	subscriberSize   = 1024
	activityInterval = 500 * time.Millisecond
)

// NudgeUpdate is the value the chat runtime delivers to its headless
// callback. The dashboard layer translates it into the JSON stored on
// Session.Nudge; the chat package does not depend on the state package.
type NudgeUpdate struct {
	State   string
	Summary string
}

// NudgeCallback is invoked by the runtime when the headless tracker
// produces a new Nudge value. Implementations may update session state
// and schedule a broadcast; they must not call back into the runtime or
// the session manager.
type NudgeCallback func(update NudgeUpdate)

// TurnErrorEvent is one live chat turn ending in error. Protocol is the
// chat protocol name; Text is the harness's error text as extracted by the
// nudge tracker. It never fires for replayed history.
type TurnErrorEvent struct {
	Protocol string
	Text     string
}

// TurnErrorCallback receives live turn errors. Like NudgeCallback, it must
// not re-enter the runtime.
type TurnErrorCallback func(TurnErrorEvent)

// Runtime bridges one chat session: it tails the harness output into the
// conversation record, fans records out to subscribers, and writes what the
// user does to the record first and the harness second. NudgeTracker derives
// the waiting-for field; transcript rendering remains in the page's reducer.
type Runtime struct {
	sessionID    string
	proto        Protocol
	paths        Paths
	log          *Log
	eventsFile   string
	eventWatcher *events.EventWatcher
	logger       *log.Logger

	// mu guards one whole step: record append, fan-out, subscribe, the
	// protocol encode (which for Codex allocates a request id), the held
	// queue, and the input-file append. Holding it across all of them keeps
	// the input order equal to the record order and makes Protocol
	// implementations single-threaded by contract.
	mu           sync.Mutex
	subs         map[chan Record]struct{}
	offset       int64    // bytes of Output already consumed
	held         []Record // user_message records waiting for the protocol to become addressable
	lastResumeID string

	// nudgeTracker derives the Session.Nudge value from records and
	// user-action outcomes. nudgeCallback is the in-process sink; nil
	// disables emission (still computed for callers that read it
	// directly, e.g. tests). lastNudge is the previously delivered
	// value used for change detection.
	nudgeTracker        *NudgeTracker
	nudgeCallback       NudgeCallback
	lastNudge           Nudge
	activityCallback    func(time.Time)
	lastActivity        time.Time
	reportedActivity    time.Time
	activityPublishedAt time.Time

	// turnErrorCallback receives live chat turn errors; nil disables the path.
	turnErrorCallback TurnErrorCallback

	// appendInput is the function used to write a line to the input
	// file. It defaults to the package-level AppendInput but tests
	// override it to simulate write failures.
	appendInput func(line []byte) error

	started  atomic.Bool
	ended    atomic.Bool // End has been called; subsequent End calls are no-ops
	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewRuntime prepares a runtime; call Start to begin tailing. eventsFile and
// handlers may be empty (tests); when both are set, a hooks event watcher is
// started exactly like a terminal session's.
func NewRuntime(sessionID string, proto Protocol, p Paths, eventsFile string, handlers map[string][]events.EventHandler, logger *log.Logger) (*Runtime, error) {
	l, err := OpenLog(p.Conversation)
	if err != nil {
		return nil, err
	}
	r := &Runtime{
		sessionID: sessionID, proto: proto, paths: p, log: l, eventsFile: eventsFile,
		logger: logger, subs: map[chan Record]struct{}{}, stopCh: make(chan struct{}), doneCh: make(chan struct{}),
		nudgeTracker: NewNudgeTracker(proto.Name()),
		appendInput:  func(line []byte) error { return AppendInput(p, line) },
	}
	if eventsFile != "" && len(handlers) > 0 {
		ew, err := events.NewEventWatcher(eventsFile, sessionID, handlers)
		if err != nil {
			r.warn("failed to create event watcher", err)
		} else {
			r.eventWatcher = ew
		}
	}
	return r, nil
}

// SetNudgeCallback registers the in-process sink the runtime calls when the
// headless tracker produces a new Nudge value. nil disables emission. The
// callback runs synchronously on the runtime goroutine; it must not call
// back into the runtime.
func (r *Runtime) SetNudgeCallback(cb NudgeCallback) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nudgeCallback = cb
}

// SetTurnErrorCallback registers the sink for live turn-ending errors.
// Register before Start. Replayed history never fires it.
func (r *Runtime) SetTurnErrorCallback(cb TurnErrorCallback) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turnErrorCallback = cb
	protoName := r.proto.Name()
	r.nudgeTracker.onTurnError = func(text string) {
		if cb != nil {
			cb(TurnErrorEvent{Protocol: protoName, Text: text})
		}
	}
}

// SetActivityCallback wires the existing session activity clock. Creation time
// is the baseline for a fresh session; replay replaces it with the latest
// current-lifetime record timestamp, never the time the daemon was restarted.
// Register before Start. Like NudgeCallback, cb must not re-enter the runtime.
func (r *Runtime) SetActivityCallback(createdAt time.Time, cb func(time.Time)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastActivity = createdAt
	r.activityCallback = cb
}

// Protocol returns the protocol name; the WebSocket history frame carries it
// so the page can pick the matching reducer.
func (r *Runtime) Protocol() string { return r.proto.Name() }

func (r *Runtime) warn(msg string, err error) {
	if r.logger != nil {
		r.logger.Warn(msg, "session", r.sessionID, "err", err)
	}
}

// Start resumes the output tail after the lines already recorded, rebuilds
// the protocol's addressing state from the bridge files, and writes any
// user messages the record holds but the input file never received.
func (r *Runtime) Start() {
	recs, err := r.log.ReadAll()
	if err != nil {
		r.warn("failed to read record", err)
	}
	// Count only the current lifetime's harness records. The record
	// may carry copied history (from a Restart seed); seeking the
	// output file past that history would skip the new lifetime's
	// output.
	start := lastSessionEndedIndex(recs) + 1
	if start < 0 {
		start = 0
	}
	n := 0
	for _, rec := range recs[start:] {
		if rec.Type == RecordHarness {
			n++
		}
	}
	r.offset = offsetAfterRecordableLines(r.paths.Output, n, r.proto.LiveOnly)
	unsent, err := r.proto.Rebuild(r.paths, recs)
	if err != nil {
		r.warn("failed to rebuild protocol state", err)
	}
	r.mu.Lock()
	r.held = append(r.held, unsent...)
	r.flushHeldLocked() // writes them now if Rebuild found the harness addressable
	// Replay the current lifetime's records through the headless
	// tracker. Intermediate states are not published: only the final
	// snapshot, after the last session-ended boundary, is delivered here.
	r.replayNudgeLocked(recs)
	r.mu.Unlock()
	r.started.Store(true)
	go r.run()
}

// lastSessionEndedIndex returns the index of the most recent
// session-ended record, or -1 when none exists. The current lifetime
// starts at the returned index + 1.
func lastSessionEndedIndex(recs []Record) int {
	idx := -1
	for i, rec := range recs {
		if rec.Type == RecordSession && rec.Event == "ended" {
			idx = i
		}
	}
	return idx
}

// replayNudgeLocked feeds records through the headless tracker in
// record order, only for the current lifetime (records after the most
// recent session-ended marker). It does not emit intermediate states.
// Caller holds r.mu.
func (r *Runtime) replayNudgeLocked(recs []Record) {
	start := lastSessionEndedIndex(recs) + 1
	current := recs[start:]
	written, err := controlsInInput(r.paths.Input, current)
	if err != nil {
		r.warn("failed to reconcile chat controls", err)
	}
	r.nudgeTracker.replaying = true
	defer func() { r.nudgeTracker.replaying = false }()
	for i, rec := range current {
		if rec.Type == RecordHarness && r.proto.LiveOnly(rec.Line) {
			continue
		}
		if rec.Type == RecordControl && !written[i] {
			continue // record-before-input append may have failed
		}
		r.noteActivityLocked(rec)
		r.nudgeTracker.Rec(rec)
	}
	r.publishActivityLocked(time.Now(), true)
	// Publish the restored snapshot once, but only if a callback is
	// registered. An unchanged restored snapshot still emits here so
	// the dashboard sees a consistent value, but the dedup in
	// dashboard writer keeps the sequence number unchanged.
	r.publishNudgeLocked()
}

// offsetAfterRecordableLines returns the byte offset just past the n-th line
// the runtime would record (every line the protocol does not call live-only).
// Live-only lines between recorded ones are skipped: they were forwarded
// live, never stored.
func offsetAfterRecordableLines(path string, n int, liveOnly func([]byte) bool) int64 {
	if n == 0 {
		return 0
	}
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 1024*1024)
	var off int64
	seen := 0
	for seen < n {
		line, err := br.ReadBytes('\n')
		off += int64(len(line))
		if err != nil {
			break
		}
		if !liveOnly(line) {
			seen++
		}
	}
	return off
}

func isStreamEvent(line []byte) bool {
	var v struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(line, &v) == nil && v.Type == "stream_event"
}

func (r *Runtime) run() {
	defer close(r.doneCh)
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-t.C:
			r.drain()
			r.mu.Lock()
			r.publishActivityLocked(time.Now(), false)
			r.mu.Unlock()
		}
	}
}

// drain appends every complete new output line as a harness record, except
// the protocol's live-only lines (deltas), which are fanned out and never
// stored: the durable line that follows carries the complete content, so
// deltas add nothing to history. Every recorded line is also shown to the
// protocol, which may make the harness addressable and release held sends.
func (r *Runtime) drain() {
	f, err := os.Open(r.paths.Output)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(r.offset, io.SeekStart); err != nil {
		return
	}
	br := bufio.NewReaderSize(f, 1024*1024)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			return // partial line stays unconsumed until it completes
		}
		r.offset += int64(len(line))
		line = line[:len(line)-1]
		if len(line) == 0 {
			continue
		}
		rec := NewHarness(line)
		r.mu.Lock()
		if r.proto.LiveOnly(line) {
			r.noteActivityLocked(rec)
			r.publishActivityLocked(time.Now(), false)
			r.fanOutLocked(rec) // forwarded live, not recorded
			r.mu.Unlock()
			continue
		}
		r.noteResumeID(line)
		if err := r.appendLocked(rec); err != nil {
			r.warn("failed to append harness record", err)
		}
		for _, follow := range r.proto.Observe(line) {
			if err := r.appendInput(follow); err != nil {
				r.warn("failed to write handshake follow-up", err)
			}
		}
		r.feedAndEmitLocked(rec)
		r.flushHeldLocked()
		r.mu.Unlock()
	}
}

// feedAndEmitLocked feeds a record to the headless tracker and emits the
// new Nudge to the registered callback if it changed. Caller holds r.mu.
func (r *Runtime) feedAndEmitLocked(rec Record) {
	r.noteActivityLocked(rec)
	r.nudgeTracker.Rec(rec)
	r.publishNudgeLocked()
	r.publishActivityLocked(time.Now(), false)
}

func (r *Runtime) noteActivityLocked(rec Record) {
	if rec.Type == RecordSession {
		return
	}
	if at, err := time.Parse(time.RFC3339Nano, rec.Ts); err == nil && at.After(r.lastActivity) {
		r.lastActivity = at
	}
}

// Rate-limit token-stream updates without losing the trailing timestamp. Turn
// state changes flush immediately so Done/Idle are timed from the final event.
// This path only updates memory and broadcasts; it never saves Nudge/state.
func (r *Runtime) publishActivityLocked(now time.Time, force bool) {
	if r.activityCallback == nil || r.lastActivity.IsZero() || r.lastActivity.Equal(r.reportedActivity) {
		return
	}
	if !force && !r.activityPublishedAt.IsZero() && now.Sub(r.activityPublishedAt) < activityInterval {
		return
	}
	r.reportedActivity = r.lastActivity
	r.activityPublishedAt = now
	r.activityCallback(r.lastActivity)
}

// publishNudgeLocked invokes the registered callback with the current
// Nudge value when it differs from the last published value, flushing activity
// at that boundary even without a Nudge callback. Caller holds r.mu.
func (r *Runtime) publishNudgeLocked() {
	current := r.nudgeTracker.Result()
	if current.Equal(r.lastNudge) {
		return
	}
	r.lastNudge = current
	r.publishActivityLocked(time.Now(), true)
	if r.nudgeCallback != nil {
		r.nudgeCallback(NudgeUpdate{State: current.State, Summary: current.Summary})
	}
}

// controlsInInput identifies successful control writes by record position, not
// request ID. Replay applies each matched occurrence in chronological order;
// a later request reusing an ID must not be resolved by an earlier answer.
func controlsInInput(inputPath string, currentLifetime []Record) (map[int]bool, error) {
	f, err := os.Open(inputPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var inputLines [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			inputLines = append(inputLines, append([]byte{}, sc.Bytes()...))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	written := make(map[int]bool)
	inputIdx := 0
	for recordIdx, rec := range currentLifetime {
		if rec.Type != RecordControl {
			continue
		}
		recLine := []byte(rec.Line)
		for i := inputIdx; i < len(inputLines); i++ {
			if bytes.Equal(inputLines[i], recLine) {
				written[recordIdx] = true
				inputIdx = i + 1
				break
			}
		}
	}
	return written, nil
}

// extractControlRequestID reads the protocol request id from a control
// line. Only outbound responses resolve server requests; an outbound client
// request (such as turn/interrupt) has a different ID namespace.
func extractControlRequestID(line []byte, protocol string) string {
	var v struct {
		Type     string          `json:"type"`
		ID       *int            `json:"id"`
		Method   string          `json:"method"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(line, &v); err != nil {
		return ""
	}
	switch {
	case protocol == ProtocolClaude && v.Type == "control_response":
		var resp struct {
			RequestID string `json:"request_id"`
		}
		if json.Unmarshal(v.Response, &resp) == nil {
			return resp.RequestID
		}
	case protocol == ProtocolCodex && v.Method == "":
		if v.ID != nil {
			return strconv.Itoa(*v.ID)
		}
	}
	return ""
}

// noteResumeID writes a resume_id event, once per distinct id, when an output
// line carries the harness conversation id. The events file path feeds the
// same idempotent UpdateSessionResumeID the hook path uses.
func (r *Runtime) noteResumeID(line []byte) {
	id := r.proto.ResumeID(line)
	if r.eventsFile == "" || id == "" || id == r.lastResumeID {
		return
	}
	r.lastResumeID = id
	if err := events.AppendEvent(r.eventsFile, map[string]string{"ts": now(), "type": "resume_id", "id": id}); err != nil {
		r.warn("failed to write resume_id event", err)
	}
}

// appendLocked writes rec and fans it out. Caller holds r.mu.
func (r *Runtime) appendLocked(rec Record) error {
	if err := r.log.Append(rec); err != nil {
		return err
	}
	r.fanOutLocked(rec)
	return nil
}

// fanOutLocked delivers rec to every subscriber. A subscriber that cannot
// keep up is dropped and closed: it reconnects and reloads history. Caller
// holds r.mu.
func (r *Runtime) fanOutLocked(rec Record) {
	for ch := range r.subs {
		select {
		case ch <- rec:
		default:
			delete(r.subs, ch)
			close(ch)
		}
	}
}

// Subscribe returns the full record and a channel of records appended after
// it. Both happen under the same mutex as appends, so a subscriber sees each
// record exactly once.
func (r *Runtime) Subscribe() ([]Record, <-chan Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hist, err := r.log.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	ch := make(chan Record, subscriberSize)
	r.subs[ch] = struct{}{}
	return hist, ch, nil
}

// Unsubscribe removes a subscriber. Safe to call after the runtime closed it.
func (r *Runtime) Unsubscribe(live <-chan Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for ch := range r.subs {
		if (<-chan Record)(ch) == live {
			delete(r.subs, ch)
			close(ch)
			return
		}
	}
}

// flushHeldLocked encodes and writes the held user messages, in order, once
// the protocol can take them. Encoding happens here, not when the message
// was held, so the line carries the thread id and a request id allocated at
// write time. On a failure the remaining records stay held. Caller holds r.mu.
func (r *Runtime) flushHeldLocked() {
	if len(r.held) == 0 || !r.proto.Addressable() {
		return
	}
	for i, rec := range r.held {
		line, err := r.proto.UserMessage(rec.ID, rec.Text, rec.Images)
		if err != nil {
			r.warn("failed to encode held message", err)
			r.held = r.held[i:]
			return
		}
		if err := r.appendInput(line); err != nil {
			r.warn("failed to flush held input", err)
			r.held = r.held[i:]
			return
		}
	}
	r.held = nil
}

// Send records the user's message, then hands it to the harness. When the
// harness is not addressable yet (Codex before its thread id and account
// check), the record is held and written by flushHeldLocked later; the page
// shows the message immediately either way.
func (r *Runtime) Send(text string, images []Image) (Record, error) {
	rec := NewUserMessage(text, images)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.appendLocked(rec); err != nil {
		return Record{}, err
	}
	r.feedAndEmitLocked(rec)
	line, err := r.proto.UserMessage(rec.ID, text, images)
	if errors.Is(err, ErrNotAddressable) {
		r.held = append(r.held, rec)
		return rec, nil
	}
	if err != nil {
		return rec, err
	}
	return rec, r.appendInput(line)
}

// sendControlLocked records a line schmux sends the harness, then writes it.
// The record is fed to the headless tracker only after the input write
// succeeds: a control line whose input write failed must not be treated
// as a successful answer. Caller holds r.mu.
func (r *Runtime) sendControlLocked(line []byte) error {
	rec := NewControl(line)
	if err := r.appendLocked(rec); err != nil {
		return err
	}
	if err := r.appendInput(line); err != nil {
		return err
	}
	r.feedAndEmitLocked(rec)
	return nil
}

// Interrupt asks the harness to stop the current turn. A protocol that has
// no turn to interrupt (Codex) returns an error and nothing is recorded.
func (r *Runtime) Interrupt() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	line, err := r.proto.Interrupt()
	if err != nil {
		return err
	}
	return r.sendControlLocked(line)
}

// AnswerPermission answers a permission request. For Claude, updatedInput is
// echoed back on allow (nil means "as proposed") and message is the deny
// reason; Codex takes only the decision.
func (r *Runtime) AnswerPermission(requestID string, allow bool, updatedInput json.RawMessage, message string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	line, err := r.proto.Permission(requestID, allow, updatedInput, message)
	if err != nil {
		return err
	}
	return r.sendControlLocked(line)
}

// AnswerQuestion answers a question request. answers is keyed by question id
// with the chosen labels; input is the original request input echoed back
// for Claude and ignored by Codex.
func (r *Runtime) AnswerQuestion(requestID string, answers map[string][]string, input json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	line, err := r.proto.Answer(requestID, answers, input)
	if err != nil {
		return err
	}
	return r.sendControlLocked(line)
}

// Abort answers a server request the page cannot render (see Protocol.Abort).
func (r *Runtime) Abort(requestID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	line, err := r.proto.Abort(requestID)
	if err != nil {
		return err
	}
	return r.sendControlLocked(line)
}

// End records that schmux disposed the session. Called from the dispose path
// only; a daemon shutdown leaves the harness running in tmux and writes
// nothing. Idempotent: the first call appends the record and fans it out;
// subsequent calls are no-ops.
func (r *Runtime) End() error {
	if !r.ended.CompareAndSwap(false, true) {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := NewSessionEnded()
	if err := r.appendLocked(rec); err != nil {
		return err
	}
	r.feedAndEmitLocked(rec)
	return nil
}

// Stop ends tailing, the event watcher, and every subscriber.
func (r *Runtime) Stop() {
	r.stopOnce.Do(func() {
		close(r.stopCh)
		if r.started.Load() {
			<-r.doneCh
		}
		if r.eventWatcher != nil {
			r.eventWatcher.Stop()
		}
		r.mu.Lock()
		if r.started.Load() {
			r.publishActivityLocked(time.Now(), true)
		}
		for ch := range r.subs {
			delete(r.subs, ch)
			close(ch)
		}
		r.mu.Unlock()
	})
}
