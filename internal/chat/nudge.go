package chat

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Nudge is the existing Session.Nudge payload, derived from the headless wire.
type Nudge struct {
	State   string `json:"state"`
	Summary string `json:"summary"`
	Source  string `json:"source"`
}

func (n Nudge) Equal(o Nudge) bool { return n == o }

type pendingRequest struct {
	id      string
	summary string
	count   int // questions in this request, or one approval/unsupported action
}

// NudgeTracker tracks only the waiting-for field. Runtime serializes access.
// Feed control records only after successful input writes, including on replay.
type NudgeTracker struct {
	protoName   string
	openTurn    bool
	queued      bool // Codex input awaiting turn/started; mid-turn sends steer
	interrupted bool // intent, not confirmation
	completed   bool
	errorMsg    string
	pending     []pendingRequest

	// Keep the active message separate so its echo cannot consume an identical
	// queued follow-up. Claude marks consumed messages with isReplay.
	claudeActive *string
	claudeQueue  []string
	threadID     string
	activeTurnID string
}

func NewNudgeTracker(protoName string) *NudgeTracker {
	return &NudgeTracker{protoName: protoName}
}

func (t *NudgeTracker) Rec(r Record) {
	switch r.Type {
	case RecordUserMessage:
		t.errorMsg = ""
		if t.protoName == ProtocolClaude {
			if !t.openTurn && len(t.claudeQueue) == 0 {
				t.startClaudeTurn(r.Text)
			} else {
				t.claudeQueue = append(t.claudeQueue, r.Text)
			}
		} else if !t.openTurn {
			t.queued = true
		}
	case RecordControl:
		t.observeControl(r.Line)
	case RecordHarness:
		switch t.protoName {
		case ProtocolClaude:
			t.observeClaude(r.Line)
		case ProtocolCodex:
			t.observeCodex(r.Line)
		}
	case RecordSession:
		if r.Event == "ended" {
			*t = NudgeTracker{protoName: t.protoName}
		}
	}
}

func (t *NudgeTracker) startClaudeTurn(text string) {
	t.openTurn = true
	t.interrupted = false
	t.errorMsg = ""
	t.claudeActive = &text
}

// WroteControl resolves exactly one successfully answered server request.
func (t *NudgeTracker) WroteControl(id string) {
	for i, p := range t.pending {
		if p.id == id {
			t.pending = append(t.pending[:i], t.pending[i+1:]...)
			return
		}
	}
}

// WroteInterrupt records intent. Only a terminal harness result confirms it.
func (t *NudgeTracker) WroteInterrupt() {
	if t.openTurn {
		t.interrupted = true
	}
}

func (t *NudgeTracker) Result() Nudge {
	n := Nudge{State: "Idle", Source: "headless"}
	switch {
	case t.errorMsg != "":
		n.State, n.Summary = "Error", t.errorMsg
	case len(t.pending) > 0:
		n.State, n.Summary = "Needs Input", t.pending[0].summary
		count := 0
		for _, p := range t.pending {
			count += p.count
		}
		if count > 1 {
			n.Summary += " (+" + strconv.Itoa(count-1) + " more)"
		}
	case t.openTurn || t.queued || len(t.claudeQueue) > 0:
		n.State = "Working"
	case t.completed:
		n.State, n.Summary = "Completed", "Done"
	}
	return n
}

func (t *NudgeTracker) addPending(p pendingRequest) {
	if p.id == "" {
		return
	}
	for _, existing := range t.pending {
		if existing.id == p.id {
			return
		}
	}
	if p.count == 0 {
		p.count = 1
	}
	t.pending = append(t.pending, p)
}

func (t *NudgeTracker) observeControl(line []byte) {
	if id := extractControlRequestID(line, t.protoName); id != "" {
		t.WroteControl(id)
		return
	}
	var v struct {
		Type    string `json:"type"`
		Method  string `json:"method"`
		Request struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
	}
	if json.Unmarshal(line, &v) != nil {
		return
	}
	if (t.protoName == ProtocolClaude && v.Type == "control_request" && v.Request.Subtype == "interrupt") ||
		(t.protoName == ProtocolCodex && v.Method == "turn/interrupt") {
		t.WroteInterrupt()
	}
}

func (t *NudgeTracker) observeClaude(line []byte) {
	var v struct {
		Type      string          `json:"type"`
		Subtype   string          `json:"subtype"`
		Parent    string          `json:"parent_tool_use_id"`
		RequestID string          `json:"request_id"`
		Request   json.RawMessage `json:"request"`
		IsError   bool            `json:"is_error"`
		IsReplay  bool            `json:"isReplay"`
		Message   struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &v) != nil {
		return
	}
	// A child's permission request still needs the user's attention.
	if v.Parent != "" && v.Type != "control_request" && v.Type != "control_cancel_request" {
		return
	}
	switch v.Type {
	case "user":
		if v.IsReplay {
			text := claudeMessageText(v.Message.Content)
			if t.claudeActive != nil && *t.claudeActive == text {
				t.claudeActive = nil
				return
			}
			for i, queued := range t.claudeQueue {
				if queued == text {
					t.claudeQueue = append(t.claudeQueue[:i], t.claudeQueue[i+1:]...)
					if !t.openTurn {
						t.startClaudeTurn(text)
					}
					t.claudeActive = nil
					return
				}
			}
		}
	case "assistant":
		if !t.openTurn {
			text := ""
			if len(t.claudeQueue) > 0 {
				text = t.claudeQueue[0]
				t.claudeQueue = t.claudeQueue[1:]
			}
			t.startClaudeTurn(text)
		}
	case "control_request":
		t.handleClaudeControlRequest(v.RequestID, v.Request)
	case "control_cancel_request":
		t.WroteControl(v.RequestID)
	case "result":
		t.openTurn = false
		t.pending = nil
		t.claudeActive = nil
		t.completed = false
		t.errorMsg = ""
		if !t.interrupted {
			if v.IsError || strings.HasPrefix(v.Subtype, "error") {
				t.errorMsg = claudeErrorMessage(line)
			} else if v.Subtype == "success" {
				t.completed = true
			}
		}
		t.interrupted = false
	}
}

func claudeMessageText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	for _, b := range blocks {
		if b.Type == "text" {
			text += b.Text
		}
	}
	return text
}

func (t *NudgeTracker) handleClaudeControlRequest(id string, raw json.RawMessage) {
	var req struct {
		Subtype string `json:"subtype"`
		Tool    string `json:"tool_name"`
		Reason  string `json:"reason"`
		Input   struct {
			Command   string          `json:"command"`
			FilePath  string          `json:"file_path"`
			Path      string          `json:"path"`
			Questions []nudgeQuestion `json:"questions"`
		} `json:"input"`
	}
	if json.Unmarshal(raw, &req) != nil || req.Subtype != "can_use_tool" {
		return
	}
	t.openTurn = true
	p := pendingRequest{id: id}
	if len(req.Input.Questions) > 0 {
		p.summary, p.count = questionSummary(req.Input.Questions)
	} else {
		path := req.Input.FilePath
		if path == "" {
			path = req.Input.Path
		}
		p.summary = approvalSummary(req.Tool, req.Input.Command, path, req.Reason)
	}
	t.addPending(p)
}

type nudgeQuestion struct {
	Question string `json:"question"`
}

func questionSummary(questions []nudgeQuestion) (string, int) {
	if len(questions) == 0 || strings.TrimSpace(questions[0].Question) == "" {
		return "Needs input", max(1, len(questions))
	}
	return questions[0].Question, len(questions)
}

func approvalSummary(tool, command, path, reason string) string {
	if command != "" {
		return "Approve " + tool + ": " + command
	}
	if path != "" {
		return "Approve " + tool + ": " + path
	}
	if reason != "" {
		return "Approve: " + reason
	}
	if tool != "" {
		return "Approve " + tool
	}
	return "Approve file changes"
}

func claudeErrorMessage(line []byte) string {
	var v struct {
		Subtype string   `json:"subtype"`
		Errors  []string `json:"errors"`
		Result  string   `json:"result"`
		Message string   `json:"message"`
	}
	if json.Unmarshal(line, &v) != nil {
		return "claude error"
	}
	for _, msg := range v.Errors {
		if msg != "" {
			return msg
		}
	}
	if v.Message != "" {
		return v.Message
	}
	if v.Result != "" {
		return v.Result
	}
	if v.Subtype != "" {
		return "claude " + v.Subtype
	}
	return "claude error"
}

func (t *NudgeTracker) fail(message string) {
	t.openTurn = false
	t.activeTurnID = ""
	t.pending = nil
	t.completed = false
	t.errorMsg = message
}

func (t *NudgeTracker) observeCodex(line []byte) {
	v, ok := parseCodexLine(line)
	if !ok {
		return
	}
	if v.Method == "" {
		// Client replies and server requests have separate ID namespaces.
		if v.ID != nil && *v.ID == codexThreadID {
			if id := threadIDOf(v.Result); id != "" {
				t.threadID = id
			}
		}
		if len(v.Error) > 0 && string(v.Error) != "null" {
			var err struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(v.Error, &err) == nil && err.Message != "" {
				t.fail(err.Message)
			} else {
				t.fail("codex request failed")
			}
		} else if v.ID != nil && *v.ID == codexAccountID {
			var account struct {
				Account json.RawMessage `json:"account"`
			}
			if json.Unmarshal(v.Result, &account) == nil && string(account.Account) == "null" {
				t.fail("Codex is not logged in")
			}
		}
		return
	}
	var p struct {
		ThreadID  string `json:"threadId"`
		TurnID    string `json:"turnId"`
		RequestID *int   `json:"requestId"`
		Turn      struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		WillRetry bool            `json:"willRetry"`
		Questions []nudgeQuestion `json:"questions"`
		Kind      string          `json:"kind"`
		Command   string          `json:"command"`
		Path      string          `json:"path"`
		Reason    string          `json:"reason"`
	}
	if json.Unmarshal(v.Params, &p) != nil {
		return
	}
	if t.threadID != "" && p.ThreadID != "" && p.ThreadID != t.threadID {
		return
	}
	if p.TurnID != "" && p.TurnID != t.activeTurnID {
		return
	}
	switch v.Method {
	case "turn/started":
		if t.openTurn && t.activeTurnID != "" && t.activeTurnID != p.Turn.ID {
			return
		}
		if t.threadID == "" {
			t.threadID = p.ThreadID
		}
		t.activeTurnID = p.Turn.ID
		t.openTurn = true
		t.queued = false
		t.interrupted = false
		t.errorMsg = ""
	case "turn/completed":
		if p.Turn.ID != "" && p.Turn.ID != t.activeTurnID {
			return
		}
		if p.Turn.Status != "completed" && p.Turn.Status != "interrupted" && p.Turn.Status != "failed" {
			return
		}
		t.openTurn = false
		t.activeTurnID = ""
		t.pending = nil
		t.interrupted = false
		t.completed = p.Turn.Status == "completed"
		if p.Turn.Status == "failed" {
			msg := "codex turn failed"
			if p.Turn.Error != nil && p.Turn.Error.Message != "" {
				msg = p.Turn.Error.Message
			}
			t.fail(msg)
		}
	case "error":
		if !p.WillRetry {
			msg := p.Error.Message
			if msg == "" {
				msg = "codex error"
			}
			t.fail(msg)
		}
	case "serverRequest/resolved":
		if p.RequestID != nil {
			t.WroteControl(strconv.Itoa(*p.RequestID))
		}
	default:
		if v.ID == nil {
			return
		}
		req := pendingRequest{id: strconv.Itoa(*v.ID)}
		switch v.Method {
		case "item/tool/requestUserInput":
			req.summary, req.count = questionSummary(p.Questions)
		case "item/commandExecution/requestApproval":
			tool := p.Kind
			if tool == "" {
				tool = "command"
			}
			req.summary = approvalSummary(tool, p.Command, p.Path, p.Reason)
		case "item/fileChange/requestApproval":
			req.summary = approvalSummary("", "", "", p.Reason)
			if p.Path != "" {
				req.summary = approvalSummary("Edit", "", p.Path, p.Reason)
			}
		default:
			req.summary = "Respond to " + v.Method
		}
		t.addPending(req)
	}
}
