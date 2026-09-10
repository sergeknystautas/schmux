package chat

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sergeknystautas/schmux/internal/version"
)

// Handshake request ids. The thread request keeps id 3 whether it is written
// up front or as the follow-up to a resume-most-recent thread/list (id 4).
// Client request ids continue from codexListID+1.
const (
	codexInitID    = 1
	codexAccountID = 2
	codexThreadID  = 3
	codexListID    = 4
)

// codexThreadSourceKinds are the thread/list sources a resume-most-recent
// considers: the user's own terminal and editor sessions plus app-server
// threads (an earlier chat session). Sub-agent threads are never resumed.
var codexThreadSourceKinds = []string{"cli", "vscode", "appServer"}

// codexProtocol is Codex's app-server JSON-RPC dialect (`codex app-server
// --stdio`). It carries the addressing state a request needs: the thread id
// (every turn/start names it), the active turn (turn/interrupt requires it),
// the next client request id, and the account check. None of it decides what
// the page renders; it is rebuilt from the bridge files on every start.
type codexProtocol struct {
	threadID   string
	activeTurn string
	nextID     int
	account    int // 0 unknown, 1 logged in, -1 logged out
	// pendingThread holds the thread/start params of a resume-most-recent
	// launch until thread/list answers; Observe then writes the thread
	// request with the newest thread id (or a plain thread/start when the
	// workspace has none). Set by Launch only, so a restarted runtime
	// (Rebuild) never re-issues it.
	pendingThread map[string]any
}

func newCodexProtocol() *codexProtocol { return &codexProtocol{nextID: codexListID + 1} }
func (*codexProtocol) Name() string    { return ProtocolCodex }

// Addressable requires the thread id and a logged-in account: a turn started
// while logged out never terminates (it loops on 401 retries), so it is
// never sent.
func (p *codexProtocol) Addressable() bool { return p.threadID != "" && p.account == 1 }

// Launch: no argv (resume, model, approval policy, and sandbox are request
// parameters), and the four handshake lines. Fenced sessions use the
// request-parameter equivalent of --dangerously-bypass-approvals-and-sandbox;
// the fence is the sandbox, as for a fenced terminal session. Resume passes
// excludeTurns because the conversation record already holds the history.
// Resume without an id (the wizard's resume mode) asks thread/list for the
// workspace's newest thread first; Observe writes the thread request when
// the answer arrives.
func (p *codexProtocol) Launch(o LaunchOpts) ([]string, [][]byte) {
	thread := map[string]any{
		"cwd":            o.Cwd,
		"approvalPolicy": "on-request",
		"sandbox":        "workspace-write",
	}
	if o.Fenced {
		thread["approvalPolicy"] = "never"
		thread["sandbox"] = "danger-full-access"
	}
	if o.ModelValue != "" {
		thread["model"] = o.ModelValue
	}
	lines := [][]byte{
		mustJSON(map[string]any{"id": codexInitID, "method": "initialize", "params": map[string]any{
			"clientInfo": map[string]any{"name": "schmux", "version": version.Version},
		}}),
		mustJSON(map[string]any{"method": "initialized"}),
		mustJSON(map[string]any{"id": codexAccountID, "method": "account/read", "params": map[string]any{}}),
	}
	switch {
	case o.ResumeID != "":
		lines = append(lines, codexThreadRequest(thread, o.ResumeID))
	case o.Resume:
		p.pendingThread = thread
		lines = append(lines, mustJSON(map[string]any{"id": codexListID, "method": "thread/list", "params": map[string]any{
			"cwd":         o.Cwd,
			"limit":       1,
			"sortKey":     "updatedAt",
			"sourceKinds": codexThreadSourceKinds,
		}}))
	default:
		lines = append(lines, codexThreadRequest(thread, ""))
	}
	return nil, lines
}

// codexThreadRequest is the id-3 handshake line: thread/resume of threadID
// when set, else thread/start. The params map is shared, not copied.
func codexThreadRequest(thread map[string]any, threadID string) []byte {
	method := "thread/start"
	if threadID != "" {
		method = "thread/resume"
		thread["threadId"] = threadID
		thread["excludeTurns"] = true
	}
	return mustJSON(map[string]any{"id": codexThreadID, "method": method, "params": thread})
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// codexLine is the subset of a JSON-RPC line the protocol reads. A line with
// a method and an id is a request (ours, or a server request to answer); a
// method without an id is a notification; an id without a method is a
// response.
type codexLine struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
	Params json.RawMessage `json:"params"`
}

func parseCodexLine(line []byte) (codexLine, bool) {
	var v codexLine
	if err := json.Unmarshal(line, &v); err != nil {
		return codexLine{}, false
	}
	return v, true
}

// Live-only notifications: every one has a durable counterpart (item/completed
// carries the full text, output, or patch) or is telemetry the page does not show.
var codexLiveSuffixes = []string{"/delta", "/textDelta", "/summaryTextDelta", "/summaryPartAdded", "/outputDelta", "/patchUpdated"}

func (*codexProtocol) LiveOnly(line []byte) bool {
	v, ok := parseCodexLine(line)
	if !ok || v.Method == "" {
		return false
	}
	if v.Method == "thread/tokenUsage/updated" || v.Method == "account/rateLimits/updated" {
		return true
	}
	for _, s := range codexLiveSuffixes {
		if strings.HasSuffix(v.Method, s) {
			return true
		}
	}
	return false
}

// threadIDOf reads thread.id from a thread/start or thread/resume result or a
// thread/started notification's params.
func threadIDOf(raw json.RawMessage) string {
	var v struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return v.Thread.ID
}

// newestThreadIDOf reads the first thread id of a thread/list result, or ""
// when the workspace has no thread to resume.
func newestThreadIDOf(raw json.RawMessage) string {
	var v struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &v) != nil || len(v.Data) == 0 {
		return ""
	}
	return v.Data[0].ID
}

// ResumeID: the thread id, known from the handshake's thread response before
// the first turn, so Restart is available immediately.
func (*codexProtocol) ResumeID(line []byte) string {
	v, ok := parseCodexLine(line)
	if !ok {
		return ""
	}
	if v.ID != nil && *v.ID == codexThreadID && v.Method == "" {
		return threadIDOf(v.Result)
	}
	if v.Method == "thread/started" {
		return threadIDOf(v.Params)
	}
	return ""
}

// Observe is idempotent: replaying the same lines in the same order leaves
// the same state, which is what lets Rebuild scan the whole output file and
// drain then observe the unrecorded tail a second time. The one line it
// answers is the thread/list response of a resume-most-recent launch, and
// only once: the pending thread params are consumed by the follow-up.
func (p *codexProtocol) Observe(line []byte) [][]byte {
	v, ok := parseCodexLine(line)
	if !ok {
		return nil
	}
	if v.Method == "" && v.ID != nil {
		switch *v.ID {
		case codexAccountID:
			var r struct {
				Account json.RawMessage `json:"account"`
			}
			if len(v.Error) > 0 || json.Unmarshal(v.Result, &r) != nil || len(r.Account) == 0 || string(r.Account) == "null" {
				p.account = -1
			} else {
				p.account = 1
			}
		case codexThreadID:
			if id := threadIDOf(v.Result); id != "" {
				p.threadID = id
			}
		case codexListID:
			if p.pendingThread == nil {
				return nil
			}
			thread := p.pendingThread
			p.pendingThread = nil
			return [][]byte{codexThreadRequest(thread, newestThreadIDOf(v.Result))}
		}
		return nil
	}
	var turn struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	switch v.Method {
	case "thread/started":
		if id := threadIDOf(v.Params); id != "" {
			p.threadID = id
		}
	case "turn/started":
		if json.Unmarshal(v.Params, &turn) == nil && (turn.ThreadID == "" || turn.ThreadID == p.threadID) {
			p.activeTurn = turn.Turn.ID
		}
	case "turn/completed":
		if json.Unmarshal(v.Params, &turn) == nil && (turn.ThreadID == "" || turn.ThreadID == p.threadID) && turn.Turn.ID == p.activeTurn {
			p.activeTurn = ""
		}
	}
	return nil
}

// Rebuild replays Observe over the output file, resumes client ids after the
// highest one in the input file, and returns the user messages recorded after
// the last session-ended record whose clientUserMessageId never reached the
// input file. The seed of a restarted session sits before that session record,
// so a restart never re-sends the old conversation.
func (p *codexProtocol) Rebuild(paths Paths, records []Record) ([]Record, error) {
	if err := eachLine(paths.Output, func(line []byte) {
		if !p.LiveOnly(line) {
			p.Observe(line)
		}
	}); err != nil {
		return nil, err
	}
	sent := map[string]bool{}
	maxID := codexListID
	if err := eachLine(paths.Input, func(line []byte) {
		v, ok := parseCodexLine(line)
		if !ok || v.Method == "" {
			return
		}
		if v.ID != nil && *v.ID > maxID {
			maxID = *v.ID
		}
		var params struct {
			ClientUserMessageID string `json:"clientUserMessageId"`
		}
		if json.Unmarshal(v.Params, &params) == nil && params.ClientUserMessageID != "" {
			sent[params.ClientUserMessageID] = true
		}
	}); err != nil {
		return nil, err
	}
	p.nextID = maxID + 1
	start := 0
	for i, r := range records {
		if r.Type == RecordSession {
			start = i + 1
		}
	}
	var unsent []Record
	for _, r := range records[start:] {
		if r.Type == RecordUserMessage && !sent[r.ID] {
			unsent = append(unsent, r)
		}
	}
	return unsent, nil
}

func eachLine(path string, fn func([]byte)) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			fn(sc.Bytes())
		}
	}
	return sc.Err()
}

func (p *codexProtocol) allocID() int {
	id := p.nextID
	p.nextID++
	return id
}

// UserMessage encodes turn/start. Before the thread id and account check are
// in there is nothing correct to encode (the thread id is part of the line),
// so it returns ErrNotAddressable and allocates no id; the runtime holds the
// record and calls again at flush time. clientUserMessageId is the record id,
// which is how Rebuild tells a sent message from a held one.
func (p *codexProtocol) UserMessage(id, text string, images []Image) ([]byte, error) {
	if !p.Addressable() {
		return nil, ErrNotAddressable
	}
	inputs := []map[string]any{{"type": "text", "text": text}}
	for _, img := range images {
		inputs = append(inputs, map[string]any{"type": "image", "url": "data:" + img.MediaType + ";base64," + img.Data})
	}
	return mustJSON(map[string]any{"id": p.allocID(), "method": "turn/start", "params": map[string]any{
		"threadId":            p.threadID,
		"clientUserMessageId": id,
		"input":               inputs,
		// Codex sends reasoning summaries only when asked; "auto" is its TUI default and persists across turns.
		"summary": "auto",
	}}), nil
}

// Interrupt needs the active turn id; with none there is nothing to stop.
func (p *codexProtocol) Interrupt() ([]byte, error) {
	if p.activeTurn == "" {
		return nil, errors.New("chat: no turn to interrupt")
	}
	return mustJSON(map[string]any{"id": p.allocID(), "method": "turn/interrupt", "params": map[string]any{
		"threadId": p.threadID, "turnId": p.activeTurn,
	}}), nil
}

// serverRequestID parses a server request id (approvals and questions carry
// integer ids that start at 0 in each process).
func serverRequestID(requestID string) (int, error) {
	n, err := strconv.Atoi(requestID)
	if err != nil {
		return 0, fmt.Errorf("chat: codex request id %q is not numeric", requestID)
	}
	return n, nil
}

// Permission answers a requestApproval server request with accept or decline.
// updatedInput and message are Claude concepts and are ignored; the "always
// allow" decisions in availableDecisions are not surfaced.
func (*codexProtocol) Permission(requestID string, allow bool, _ json.RawMessage, _ string) ([]byte, error) {
	n, err := serverRequestID(requestID)
	if err != nil {
		return nil, err
	}
	decision := "decline"
	if allow {
		decision = "accept"
	}
	return []byte(fmt.Sprintf(`{"id":%d,"result":{"decision":%q}}`, n, decision)), nil
}

// Answer answers a requestUserInput server request: one label array per
// question id.
func (*codexProtocol) Answer(requestID string, answers map[string][]string, _ json.RawMessage) ([]byte, error) {
	n, err := serverRequestID(requestID)
	if err != nil {
		return nil, err
	}
	wrapped := make(map[string]map[string][]string, len(answers))
	for q, labels := range answers {
		if labels == nil {
			labels = []string{}
		}
		wrapped[q] = map[string][]string{"answers": labels}
	}
	return json.Marshal(map[string]any{"id": n, "result": map[string]any{"answers": wrapped}})
}

// Abort answers any server request with a JSON-RPC error. app-server routes
// this to the pending request as an abort instead of leaving the turn blocked.
func (*codexProtocol) Abort(requestID string) ([]byte, error) {
	n, err := serverRequestID(requestID)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(`{"id":%d,"error":{"code":-32601,"message":"schmux: unsupported server request"}}`, n)), nil
}
