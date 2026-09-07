package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// claudeProtocol is Claude Code's headless stream-json dialect
// (`claude -p --input-format stream-json --output-format stream-json`),
// exactly as shipped before the Protocol interface existed. It carries no
// state: Claude is addressable from the first byte.
type claudeProtocol struct{}

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

func (claudeProtocol) Observe([]byte) [][]byte                   { return nil }
func (claudeProtocol) Rebuild(Paths, []Record) ([]Record, error) { return nil, nil }
func (claudeProtocol) Addressable() bool                         { return true }

func (claudeProtocol) UserMessage(_ string, text string, images []Image) ([]byte, error) {
	return UserMessageLine(text, images)
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
		blocks := []map[string]any{{"type": "text", "text": text}}
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
