package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// claudeProtocol is Claude Code's headless stream-json dialect
// (`claude -p --input-format stream-json --output-format stream-json`).
// It carries one piece of state: active is the dispatch gate. It goes true
// on a committed dispatch and false on every terminal result.
type claudeProtocol struct {
	active bool
}

func newClaudeProtocol() *claudeProtocol {
	return &claudeProtocol{}
}

func (claudeProtocol) Name() string { return ProtocolClaude }

// Launch: the model flag and value, plus the harness's skip-approvals args
// when fenced. Base args come from the descriptor via ChatArgs; nothing is
// written to the input file up front.
func (claudeProtocol) Launch(o LaunchOpts) ([]string, [][]byte) {
	var argv []string
	if o.Adapter != nil {
		if o.ModelValue != "" && o.Adapter.ModelFlag() != "" {
			argv = append(argv, o.Adapter.ModelFlag(), o.ModelValue)
		}
		if o.Fenced {
			argv = append(argv, o.Adapter.AutoApproveArgs()...)
		}
	}
	return argv, nil
}

// LiveOnly: stream_event deltas. The assistant record that follows carries
// the complete block.
func (claudeProtocol) LiveOnly(line []byte) bool { return isStreamEvent(line) }

// ResumeID: the session_id on system/init, which opens every turn.
func (claudeProtocol) ResumeID(line []byte) string {
	var v struct {
		Type      string `json:"type"`
		Subtype   string `json:"subtype"`
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(line, &v) != nil || v.Type != "system" || v.Subtype != "init" {
		return ""
	}
	return v.SessionID
}

// Observe updates the dispatch gate from one recorded output line. Every
// terminal result clears it so Runtime can dispatch the next held user
// message. Legacy takeover is gated separately by Runtime's reconstructed
// Nudge state because queued_turn_count does not reliably describe pending
// native work.
func (p *claudeProtocol) Observe(line []byte) [][]byte {
	var v struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &v) != nil || v.Type != "result" {
		return nil
	}
	p.active = false
	return nil
}

// Rebuild restores the dispatch gate from the bridge files and the
// record, and returns the user_message records whose dispatch did not
// reach the input file. Two accounting modes share the same record:
//
//   - Legacy prefix (records before the first user_message_dispatch):
//     every user_message's exact `UserMessageLine` is consumed in order
//     from the input file and never resent. Runtime reconstructs Nudge state
//     from those records and gates takeover independently; Claude's
//     `queued_turn_count` is not a reliable pending-work signal.
//
//   - Daemon-held suffix (from the first dispatch marker forward): each
//     marker must match a later, unconsumed user input line. A marker
//     whose input line is missing is an attempted-but-undelivered
//     message, returned as unsent so the runtime can dispatch it again.
//     Markers without a later result commit the protocol to active.
//
// Records before the most recent `session: ended` marker are seed
// history; the current lifetime starts after that index.
func (p *claudeProtocol) Rebuild(paths Paths, records []Record) ([]Record, error) {
	p.active = false

	// Read every user-message line from the input file in order, ignoring
	// control lines (interrupt, control_response, etc.). The legacy prefix
	// matches each user record against the next equal input line; the
	// daemon-held suffix does the same per dispatch marker.
	userLines, err := claudeInputUserLines(paths.Input)
	if err != nil {
		return nil, err
	}

	current := currentLifetime(records)
	firstDispatchIdx, takeoverIdx := claudeRebuildBoundaries(current)
	legacyEnd := firstDispatchIdx
	accountingStart := firstDispatchIdx
	if takeoverIdx >= 0 && (firstDispatchIdx == len(current) || takeoverIdx < firstDispatchIdx) {
		legacyEnd = takeoverIdx
		accountingStart = takeoverIdx
	}

	confirmed, unsent := matchClaudeDispatches(current, userLines, legacyEnd, accountingStart)

	// Interleave record markers with the unrecorded output bridge so a
	// confirmed marker without a later terminal result keeps the
	// protocol active, while an unrecorded later result makes it idle.
	recordIdx := 0
	processToMarker := func() {
		for recordIdx < len(current) {
			rec := current[recordIdx]
			if rec.Type == RecordHarness {
				return
			}
			recordIdx++
			if rec.Type == RecordUserMessageDispatch && confirmed[rec.ID] {
				p.CommitUserMessage(rec.ID)
			}
		}
	}
	processToMarker()
	if err := eachLine(paths.Output, func(line []byte) {
		if len(line) == 0 {
			return
		}
		processToMarker()
		if !p.LiveOnly(line) {
			// Advance past recorded harness lines that match the
			// bridge exactly; unrecorded lines stay in the bridge.
			for recordIdx < len(current) {
				rec := current[recordIdx]
				if rec.Type != RecordHarness {
					processToMarker()
					continue
				}
				if string(rec.Line) == string(line) {
					recordIdx++
				}
				break
			}
		}
		p.Observe(line)
	}); err != nil {
		return nil, err
	}
	processToMarker()
	return unsent, nil
}

// currentLifetime returns the records after the most recent session: ended
// marker. A conversation seeded by Restart (CopyLog of the previous session)
// is seed history and must not be rebuilt against the bridge.
func currentLifetime(records []Record) []Record {
	idx := lastSessionEndedIndex(records)
	if idx < 0 {
		return records
	}
	return records[idx+1:]
}

// claudeRebuildBoundaries returns the first dispatch marker and the persisted
// Claude takeover marker, using len(current) for an absent dispatch marker and
// -1 for an absent takeover marker.
func claudeRebuildBoundaries(current []Record) (int, int) {
	firstDispatch, takeover := len(current), -1
	for i, rec := range current {
		if rec.Type == RecordUserMessageDispatch {
			return i, takeover
		}
		if rec.Type == RecordClaudeTakeover && takeover < 0 {
			takeover = i
		}
	}
	return firstDispatch, takeover
}

// matchClaudeDispatches walks the current lifetime in record order,
// consuming one user-input line for each user_message (legacy) or for
// each dispatch marker (daemon-held), and returns the markers it
// confirmed plus any user_message whose dispatch was never delivered.
// Duplicate markers for the same id are idempotent. A marker that
// references a user_message already matched by the legacy prefix is
// confirmed without consuming another input line.
func matchClaudeDispatches(
	current []Record, lines [][]byte, legacyEnd, accountingStart int,
) (confirmed map[string]bool, unsent []Record) {
	confirmed = map[string]bool{}
	lineIdx := 0
	idToRec := map[string]Record{}
	referenced := map[string]bool{}
	for _, rec := range current {
		if rec.Type == RecordUserMessage && rec.ID != "" {
			idToRec[rec.ID] = rec
		}
		if rec.Type == RecordUserMessageDispatch && rec.ID != "" {
			referenced[rec.ID] = true
		}
	}
	// Legacy prefix: consume input lines for every user_message record
	// before the first dispatch marker. Already-forwarded inputs are
	// matched and consumed; a missing match leaves the line pointer
	// where it was (legacy input failures are not retried). Any
	// user_message matched in the legacy prefix is "confirmed" so a
	// later marker referencing the same id does not need to consume a
	// second input line.
	legacyMatched := map[string]bool{}
	for i := 0; i < legacyEnd; i++ {
		rec := current[i]
		if rec.Type != RecordUserMessage {
			continue
		}
		expected, err := UserMessageLine(rec.Text, rec.Images)
		if err != nil {
			continue
		}
		if nextIdx, ok := takeClaudeInputLine(lines, lineIdx, expected); ok {
			lineIdx = nextIdx
			legacyMatched[rec.ID] = true
		}
	}
	// Daemon-held suffix: every marker must match the next input line
	// for its referenced user_message. A marker whose input line is
	// missing marks the message as attempted-but-undelivered; the
	// user_message then returns as held.
	for i := accountingStart; i < len(current); i++ {
		rec := current[i]
		if rec.Type != RecordUserMessageDispatch {
			continue
		}
		if confirmed[rec.ID] {
			continue // duplicate marker, idempotent
		}
		if legacyMatched[rec.ID] {
			// The legacy prefix already confirmed this dispatch; the
			// marker is satisfied without a second input-line match.
			confirmed[rec.ID] = true
			continue
		}
		msg, ok := idToRec[rec.ID]
		if !ok {
			continue
		}
		expected, err := UserMessageLine(msg.Text, msg.Images)
		if err != nil {
			continue
		}
		nextIdx, matched := takeClaudeInputLine(lines, lineIdx, expected)
		if !matched {
			continue
		}
		lineIdx = nextIdx
		confirmed[rec.ID] = true
	}
	// A user_message is unsent (held) when it appears in the daemon-held
	// suffix without any matching marker (the runtime has yet to
	// dispatch it), or when a marker references it but no input line
	// was consumed (attempted-but-undelivered). Pure legacy sends
	// before the first marker are not retried, even if their input line
	// is absent. Each user_message is added at most once.
	consumed := map[string]bool{}
	for id := range legacyMatched {
		consumed[id] = true
	}
	for id := range confirmed {
		consumed[id] = true
	}
	seen := map[string]bool{}
	for i := accountingStart; i < len(current); i++ {
		rec := current[i]
		if rec.Type != RecordUserMessage {
			continue
		}
		if consumed[rec.ID] || seen[rec.ID] {
			continue
		}
		seen[rec.ID] = true
		unsent = append(unsent, rec)
	}
	for id, rec := range idToRec {
		if referenced[id] && !consumed[id] && !seen[id] {
			seen[id] = true
			unsent = append(unsent, rec)
		}
	}
	return confirmed, unsent
}

// takeClaudeInputLine returns the index of the next exact-equal match
// for expected, or -1 when no such line exists. Comparison is on whole
// JSON lines (control lines were already filtered by the caller).
func takeClaudeInputLine(lines [][]byte, at int, expected []byte) (int, bool) {
	for i := at; i < len(lines); i++ {
		if bytesEqual(lines[i], expected) {
			return i + 1, true
		}
	}
	return -1, false
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// claudeInputUserLines returns every user-message line in the input
// file, in order, skipping control lines (interrupt, control_response,
// etc.) that the runtime writes between user messages.
func claudeInputUserLines(path string) ([][]byte, error) {
	var out [][]byte
	if err := eachLine(path, func(line []byte) {
		var v struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &v) == nil && v.Type == "user" {
			out = append(out, append([]byte{}, line...))
		}
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// Addressable is the inverse of active.
func (p *claudeProtocol) Addressable() bool { return !p.active }

// UserMessage encodes only when addressable. The runtime still records
// the message and appends it to its held queue when this returns
// ErrNotAddressable, so a held follow-up never claims an id that was
// never written.
func (p *claudeProtocol) UserMessage(_ string, text string, images []Image) ([]byte, error) {
	if !p.Addressable() {
		return nil, ErrNotAddressable
	}
	return UserMessageLine(text, images)
}

// CommitUserMessage records that a successfully encoded user-message
// line was appended to the harness input. The gate flips closed and stays
// closed until the next terminal result.
func (p *claudeProtocol) CommitUserMessage(string) {
	p.active = true
}

func (claudeProtocol) Interrupt() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type": "control_request", "request_id": "int-" + NewUserMessage("", nil).ID,
		"request": map[string]any{"subtype": "interrupt"},
	})
}

// Permission answers a can_use_tool request. updatedInput is echoed back on
// allow (nil means "as proposed"); message is the deny reason.
func (claudeProtocol) Permission(requestID string, allow bool, updatedInput json.RawMessage, message string) ([]byte, error) {
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
	return controlResponse(requestID, resp), nil
}

// Answer sets answers on the original AskUserQuestion input and allows the
// tool. Claude takes one string per question; the chosen labels join with ", ".
func (claudeProtocol) Answer(requestID string, answers map[string][]string, input json.RawMessage) ([]byte, error) {
	updated := map[string]any{}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &updated); err != nil {
			return nil, fmt.Errorf("chat: question input: %w", err)
		}
	}
	joined := make(map[string]string, len(answers))
	for q, labels := range answers {
		joined[q] = strings.Join(labels, ", ")
	}
	updated["answers"] = joined
	return controlResponse(requestID, map[string]any{"behavior": "allow", "updatedInput": updated}), nil
}

func (claudeProtocol) Abort(string) ([]byte, error) {
	return nil, errors.New("chat: claude has no abortable requests")
}

// UserMessageLine renders the stream-json user message for stdin. Text-only
// messages use a plain string content; images add content blocks.
func UserMessageLine(text string, images []Image) ([]byte, error) {
	var content any = text
	if len(images) > 0 {
		blocks := []map[string]any{{"type": "text", "text": AppendImagePaths(text, images)}}
		for _, img := range images {
			blocks = append(blocks, map[string]any{
				"type": "image", "source": map[string]any{"type": "base64", "media_type": img.MediaType, "data": img.Data},
			})
		}
		content = blocks
	}
	line := struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"message"`
	}{Type: "user"}
	line.Message.Role = "user"
	line.Message.Content = content
	return json.Marshal(line)
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
