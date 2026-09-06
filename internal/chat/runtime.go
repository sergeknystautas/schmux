package chat

import (
	"bufio"
	"encoding/json"
	"fmt"
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
// state; the page's reducer interprets the record.
type Runtime struct {
	sessionID    string
	paths        Paths
	log          *Log
	eventsFile   string
	eventWatcher *events.EventWatcher
	logger       *log.Logger

	mu           sync.Mutex // guards append+fan-out and subscribe
	subs         map[chan Record]struct{}
	offset       int64 // bytes of Output already consumed
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
func NewRuntime(sessionID string, p Paths, eventsFile string, handlers map[string][]events.EventHandler, logger *log.Logger) (*Runtime, error) {
	l, err := OpenLog(p.Conversation)
	if err != nil {
		return nil, err
	}
	r := &Runtime{
		sessionID:  sessionID,
		paths:      p,
		log:        l,
		eventsFile: eventsFile,
		logger:     logger,
		subs:       map[chan Record]struct{}{},
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
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

func (r *Runtime) warn(msg string, err error) {
	if r.logger != nil {
		r.logger.Warn(msg, "session", r.sessionID, "err", err)
	}
}

// Start resumes the output tail after the lines already recorded.
func (r *Runtime) Start() {
	n, err := r.log.CountHarness()
	if err != nil {
		r.warn("failed to count harness records", err)
	}
	r.offset = offsetAfterRecordableLines(r.paths.Output, n)
	r.started.Store(true)
	go r.run()
}

// offsetAfterRecordableLines returns the byte offset just past the n-th line
// that the runtime would record (every line except stream_event). Deltas
// between recorded lines are skipped: they were forwarded live, never stored.
func offsetAfterRecordableLines(path string, n int) int64 {
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
		if !isStreamEvent(line) {
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
// stream_event lines which are forwarded live and never stored: the durable
// assistant record carries the complete text, so deltas add nothing to history.
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
		if isStreamEvent(line) {
			r.fanOut(rec) // forwarded live, not recorded
			continue
		}
		r.noteResumeID(line)
		if err := r.appendAndFanOut(rec); err != nil {
			r.warn("failed to append harness record", err)
		}
	}
}

func (r *Runtime) noteResumeID(line []byte) {
	if r.eventsFile == "" {
		return
	}
	var v struct {
		Type      string `json:"type"`
		Subtype   string `json:"subtype"`
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(line, &v) != nil || v.Type != "system" || v.Subtype != "init" || v.SessionID == "" || v.SessionID == r.lastResumeID {
		return
	}
	r.lastResumeID = v.SessionID
	ev := map[string]string{"ts": now(), "type": "resume_id", "id": v.SessionID}
	if err := events.AppendEvent(r.eventsFile, ev); err != nil {
		r.warn("failed to write resume_id event", err)
	}
}

func (r *Runtime) appendAndFanOut(rec Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.log.Append(rec); err != nil {
		return err
	}
	r.fanOutLocked(rec)
	return nil
}

// fanOut delivers rec to every subscriber without recording it. Used for
// stream_event lines that should reach the page live but not the file.
func (r *Runtime) fanOut(rec Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fanOutLocked(rec)
}

func (r *Runtime) fanOutLocked(rec Record) {
	for ch := range r.subs {
		select {
		case ch <- rec:
		default:
			delete(r.subs, ch)
			close(ch) // slow client: it reconnects and reloads history
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

// Send records the user's message, then hands it to the harness.
func (r *Runtime) Send(text string, images []Image) (Record, error) {
	line, err := UserMessageLine(text, images)
	if err != nil {
		return Record{}, err
	}
	rec := NewUserMessage(text, images)
	if err := r.appendAndFanOut(rec); err != nil {
		return Record{}, err
	}
	return rec, AppendInput(r.paths, line)
}

func (r *Runtime) sendControl(line []byte) error {
	if err := r.appendAndFanOut(NewControl(line)); err != nil {
		return err
	}
	return AppendInput(r.paths, line)
}

// Interrupt asks the harness to stop the current turn.
func (r *Runtime) Interrupt() error {
	line, _ := json.Marshal(map[string]any{
		"type": "control_request", "request_id": "int-" + NewUserMessage("", nil).ID,
		"request": map[string]any{"subtype": "interrupt"},
	})
	return r.sendControl(line)
}

// AnswerPermission answers a can_use_tool request. updatedInput is echoed
// back on allow (nil means "as proposed"); message is the deny reason.
func (r *Runtime) AnswerPermission(requestID string, allow bool, updatedInput json.RawMessage, message string) error {
	var resp map[string]any
	if allow {
		resp = map[string]any{"behavior": "allow"}
		if len(updatedInput) > 0 {
			resp["updatedInput"] = json.RawMessage(updatedInput)
		} else {
			resp["updatedInput"] = map[string]any{}
		}
	} else {
		if message == "" {
			message = "User denied this from the schmux chat."
		}
		resp = map[string]any{"behavior": "deny", "message": message}
	}
	return r.sendControl(controlResponse(requestID, resp))
}

// AnswerQuestion answers an AskUserQuestion request by setting answers on the
// original input and allowing the tool.
func (r *Runtime) AnswerQuestion(requestID string, answers map[string]string, input json.RawMessage) error {
	updated := map[string]any{}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &updated); err != nil {
			return fmt.Errorf("chat: question input: %w", err)
		}
	}
	updated["answers"] = answers
	return r.sendControl(controlResponse(requestID, map[string]any{"behavior": "allow", "updatedInput": updated}))
}

func controlResponse(requestID string, response map[string]any) []byte {
	line, _ := json.Marshal(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype": "success", "request_id": requestID, "response": response,
		},
	})
	return line
}

// End records that schmux disposed the session. Called from the dispose path
// only; a daemon shutdown leaves the harness running in tmux and writes
// nothing. Idempotent: the first call appends the record and fans it out;
// subsequent calls are no-ops.
func (r *Runtime) End() error {
	if !r.ended.CompareAndSwap(false, true) {
		return nil
	}
	return r.appendAndFanOut(NewSessionEnded())
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
