package flights

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/internal/chat"
)

// PlannerTurn is a completed root response observed by the flight monitor.
// Text contains every response block; FinalText is the final decision response.
// It includes the durable record range that produced it. The monitor
// only emits turns with a successful, interrupted, or failed outcome; an
// interrupted or failed turn keeps the harness-reported error so the pilot
// notification can quote it without re-classifying.
type PlannerTurn struct {
	SessionID string `json:"session_id"`
	StartSeq  uint64 `json:"start_seq"`
	EndSeq    uint64 `json:"end_seq"`
	Text      string `json:"text"`
	FinalText string `json:"final_text,omitempty"`
	Outcome   string `json:"outcome"` // completed, interrupted, failed
	Error     string `json:"error,omitempty"`
}

// TurnKey returns the work key the manager deduplicates by: one session, one
// end sequence. Replaying the same fixture into a fresh collector yields the
// same key.
func (t PlannerTurn) Key() string { return t.SessionID + "/" + uint64Key(t.EndSeq) }

func uint64Key(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// turnCollector assembles one completed root turn at a time. It is single-
// session and single-protocol: the monitor spawns one collector per planner
// session, feeds it records, and acts on the returned turn. consume returns
// a turn when the boundary is reached and nil otherwise; the error path is
// reserved for unknown protocols or malformed boundary records — the caller
// must not invent a successful empty response to mask a failure.
type turnCollector struct {
	protocol  string
	sessionID string

	// claude bookkeeping.
	claudeOpen   bool
	claudeStart  uint64
	claudeBlocks []string
	claudeSeen   map[string]bool // record UUID to avoid duplicate snapshots

	// codex bookkeeping.
	codexThreadID string
	codexTurnID   string
	codexOpen     bool
	codexStart    uint64
	codexItems    map[string]codexResponseBlock
	codexOrder    []string // item ids in completion order
}

type codexResponseBlock struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Text  string `json:"text"`
	Phase string `json:"phase"`
}

func newTurnCollector(protocol, sessionID string) *turnCollector {
	return &turnCollector{
		protocol:   protocol,
		sessionID:  sessionID,
		claudeSeen: map[string]bool{},
		codexItems: map[string]codexResponseBlock{},
	}
}

// consume feeds one chat record. It returns a non-nil turn exactly once per
// completed root turn. The returned turn is a deep-enough copy for the manager
// to retain; the collector resets its open state before returning.
func (c *turnCollector) consume(rec chat.Record) (*PlannerTurn, error) {
	switch c.protocol {
	case chat.ProtocolClaude:
		return c.consumeClaude(rec)
	case chat.ProtocolCodex:
		return c.consumeCodex(rec)
	}
	return nil, fmt.Errorf("monitor: unknown protocol %q", c.protocol)
}

// consumeClaude opens on the first dispatch boundary or legacy root assistant
// traffic, accumulates text blocks from root assistant messages, and closes
// on a root result. Child responses (records with parent_tool_use_id set) do
// not open a turn and never reset one already in progress.
func (c *turnCollector) consumeClaude(rec chat.Record) (*PlannerTurn, error) {
	if rec.Type != chat.RecordHarness {
		return nil, nil
	}
	var v struct {
		Type    string          `json:"type"`
		UUID    string          `json:"uuid"`
		Subtype string          `json:"subtype"`
		Parent  string          `json:"parent_tool_use_id"`
		Message json.RawMessage `json:"message"`
		Result  string          `json:"result"`
		IsError bool            `json:"is_error"`
	}
	if err := json.Unmarshal(rec.Line, &v); err != nil {
		return nil, nil // unknown line shapes do not open or close a turn
	}

	switch v.Type {
	case "assistant":
		if v.Parent != "" {
			return nil, nil // child agent output, ignore
		}
		if !c.claudeOpen {
			c.claudeOpen = true
			c.claudeStart = rec.Seq
			c.claudeBlocks = nil
		}
		var msg struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(v.Message, &msg); err != nil {
			return nil, nil
		}
		if v.UUID != "" && c.claudeSeen[v.UUID] {
			return nil, nil
		}
		if v.UUID != "" {
			c.claudeSeen[v.UUID] = true
		}
		for _, block := range msg.Content {
			if block.Type == "text" && block.Text != "" {
				c.claudeBlocks = append(c.claudeBlocks, block.Text)
			}
		}
		return nil, nil
	case "result":
		if v.Parent != "" {
			return nil, nil
		}
		if !c.claudeOpen {
			// An empty successful turn still needs a record so the monitor
			// can advance its cursor: produce a no-text turn anchored at
			// this result.
			c.claudeStart = rec.Seq
		}
		outcome := "completed"
		errMsg := ""
		if v.IsError || strings.HasPrefix(v.Subtype, "error") {
			outcome = "failed"
			errMsg = strings.TrimSpace(v.Result)
			if errMsg == "" {
				errMsg = v.Subtype
			}
		}
		turn := PlannerTurn{
			SessionID: c.sessionID,
			StartSeq:  c.claudeStart,
			EndSeq:    rec.Seq,
			Text:      joinBlocks(c.claudeBlocks),
			FinalText: v.Result,
			Outcome:   outcome,
			Error:     errMsg,
		}
		c.reset()
		return &turn, nil
	}
	return nil, nil
}

// consumeCodex tracks the root thread and active turn. item/completed records
// for agentMessage items belonging to the current turn are accumulated; the
// turn/completed.params.turn.items updates final text but may summarize only
// the final answer. Earlier completed blocks remain part of the response.
// Transient item/agentMessage/delta events are ignored.
func (c *turnCollector) consumeCodex(rec chat.Record) (*PlannerTurn, error) {
	if rec.Type != chat.RecordHarness {
		return nil, nil
	}
	var v struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(rec.Line, &v); err != nil {
		return nil, nil
	}

	switch v.Method {
	case "thread/started":
		var params struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if err := json.Unmarshal(v.Params, &params); err == nil && params.Thread.ID != "" && c.codexThreadID == "" {
			c.codexThreadID = params.Thread.ID
		}
		return nil, nil
	case "turn/started":
		var params struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Turn     struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(v.Params, &params); err != nil {
			return nil, nil
		}
		if c.codexThreadID != "" && params.ThreadID != "" && params.ThreadID != c.codexThreadID {
			return nil, nil
		}
		if c.codexThreadID == "" {
			c.codexThreadID = params.ThreadID
		}
		if params.Turn.ID != "" {
			params.TurnID = params.Turn.ID
		}
		c.codexTurnID = params.TurnID
		c.codexOpen = true
		c.codexStart = rec.Seq
		c.codexItems = map[string]codexResponseBlock{}
		c.codexOrder = nil
		return nil, nil
	case "item/completed":
		var params struct {
			Item   codexResponseBlock `json:"item"`
			TurnID string             `json:"turnId"`
		}
		if err := json.Unmarshal(v.Params, &params); err != nil {
			return nil, nil
		}
		if !c.codexOpen || params.TurnID != c.codexTurnID {
			return nil, nil
		}
		if params.Item.Type != "agentMessage" || params.Item.ID == "" {
			return nil, nil
		}
		if _, seen := c.codexItems[params.Item.ID]; !seen {
			c.codexOrder = append(c.codexOrder, params.Item.ID)
		}
		c.codexItems[params.Item.ID] = params.Item
		return nil, nil
	case "turn/completed":
		var params struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Turn     struct {
				ID     string               `json:"id"`
				Items  []codexResponseBlock `json:"items"`
				Status string               `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(v.Params, &params); err != nil {
			return nil, fmt.Errorf("monitor: malformed codex turn/completed: %w", err)
		}
		if c.codexThreadID != "" && params.ThreadID != "" && params.ThreadID != c.codexThreadID {
			return nil, nil
		}
		if params.Turn.ID != "" {
			params.TurnID = params.Turn.ID
		}
		if !c.codexOpen || params.TurnID != c.codexTurnID {
			return nil, nil
		}
		// Completion summaries can omit commentary already observed above.
		for _, item := range params.Turn.Items {
			if item.Type != "agentMessage" || item.ID == "" {
				continue
			}
			if _, seen := c.codexItems[item.ID]; !seen {
				c.codexOrder = append(c.codexOrder, item.ID)
			}
			c.codexItems[item.ID] = item
		}
		blocks := make([]string, 0, len(c.codexOrder))
		var finalText string
		for _, id := range c.codexOrder {
			if item := c.codexItems[id]; item.Text != "" {
				blocks = append(blocks, item.Text)
				if item.Phase == "final_answer" || item.Phase == "" {
					finalText = item.Text
				}
			}
		}
		outcome := "completed"
		errMsg := ""
		if params.Turn.Status == "failed" || params.Turn.Status == "interrupted" {
			outcome = params.Turn.Status
			if params.Turn.Error != nil && params.Turn.Error.Message != "" {
				errMsg = params.Turn.Error.Message
			}
		}
		turn := PlannerTurn{
			SessionID: c.sessionID,
			StartSeq:  c.codexStart,
			EndSeq:    rec.Seq,
			Text:      joinBlocks(blocks),
			FinalText: finalText,
			Outcome:   outcome,
			Error:     errMsg,
		}
		c.reset()
		return &turn, nil
	}
	return nil, nil
}

func (c *turnCollector) reset() {
	c.claudeOpen = false
	c.claudeStart = 0
	c.claudeBlocks = nil
	c.claudeSeen = map[string]bool{}
	c.codexOpen = false
	c.codexStart = 0
	c.codexItems = map[string]codexResponseBlock{}
	c.codexOrder = nil
}

// joinBlocks concatenates collected blocks with a single newline between them.
// Empty input returns an empty string, which the monitor treats as a no-op
// turn — Jev is not called for empty responses.
func joinBlocks(blocks []string) string {
	cleaned := make([]string, 0, len(blocks))
	for _, b := range blocks {
		b = strings.TrimSpace(b)
		if b != "" {
			cleaned = append(cleaned, b)
		}
	}
	return strings.Join(cleaned, "\n")
}
