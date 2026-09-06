package chat

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/events"
)

const (
	pollInterval   = 50 * time.Millisecond
	subscriberSize = 1024
)

// Runtime bridges one chat session: it tails the harness output into the
// conversation record, fans records out to subscribers, and writes what the
// user does to the record first and the harness second. It keeps no turn
// state; the page's reducer interprets the record. What differs per harness
// (launch, encode, observe) is the Protocol it holds.
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
	n := 0
	for _, rec := range recs {
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
	r.mu.Unlock()
	r.started.Store(true)
	go r.run()
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
			r.fanOutLocked(rec) // forwarded live, not recorded
			r.mu.Unlock()
			continue
		}
		r.noteResumeID(line)
		if err := r.appendLocked(rec); err != nil {
			r.warn("failed to append harness record", err)
		}
		r.proto.Observe(line)
		r.flushHeldLocked()
		r.mu.Unlock()
	}
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
		if err := AppendInput(r.paths, line); err != nil {
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
	line, err := r.proto.UserMessage(rec.ID, text, images)
	if errors.Is(err, ErrNotAddressable) {
		r.held = append(r.held, rec)
		return rec, nil
	}
	if err != nil {
		return rec, err
	}
	return rec, AppendInput(r.paths, line)
}

// sendControlLocked records a line schmux sends the harness, then writes it.
// Caller holds r.mu.
func (r *Runtime) sendControlLocked(line []byte) error {
	if err := r.appendLocked(NewControl(line)); err != nil {
		return err
	}
	return AppendInput(r.paths, line)
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
	return r.appendLocked(NewSessionEnded())
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
		for ch := range r.subs {
			delete(r.subs, ch)
			close(ch)
		}
		r.mu.Unlock()
	})
}
