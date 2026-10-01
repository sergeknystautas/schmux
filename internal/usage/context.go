package usage

import "encoding/json"

// ParseClaudeContextTokens reads the tokens in the model's context window — the
// input the latest call read, cached included — from a top-level message_delta
// stream event. Assistant lines are not used: third-party models served through
// the Claude CLI report zero usage there. Subagent events (parent_tool_use_id
// set) describe a different context and are ignored.
func ParseClaudeContextTokens(line []byte) (int, bool) {
	var v struct {
		Type            string  `json:"type"`
		ParentToolUseID *string `json:"parent_tool_use_id"`
		Event           struct {
			Type  string `json:"type"`
			Usage *struct {
				InputTokens              int `json:"input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		} `json:"event"`
	}
	if json.Unmarshal(line, &v) != nil || v.Type != "stream_event" || v.ParentToolUseID != nil ||
		v.Event.Type != "message_delta" || v.Event.Usage == nil {
		return 0, false
	}
	u := v.Event.Usage
	tokens := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
	return tokens, tokens > 0
}

// ParseCodexContextTokens reads the input tokens of the thread's latest request
// (cached input included) from a thread/tokenUsage/updated notification.
func ParseCodexContextTokens(line []byte) (int, bool) {
	var v struct {
		Method string `json:"method"`
		Params struct {
			TokenUsage *struct {
				Last struct {
					InputTokens int `json:"inputTokens"`
				} `json:"last"`
			} `json:"tokenUsage"`
		} `json:"params"`
	}
	if json.Unmarshal(line, &v) != nil || v.Method != "thread/tokenUsage/updated" || v.Params.TokenUsage == nil {
		return 0, false
	}
	tokens := v.Params.TokenUsage.Last.InputTokens
	return tokens, tokens > 0
}
