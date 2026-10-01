package usage

import "testing"

func TestCapturedContextTokens(t *testing.T) {
	// interrupt-pending: first message_delta usage is 10 + 14630 + 9545.
	if got := capturedReport(t, "claude/interrupt-pending.jsonl", ParseClaudeContextTokens); got != 24185 {
		t.Fatalf("claude context tokens = %d, want 24185", got)
	}
	// approval: first tokenUsage last.inputTokens (cached 6912 is a subset).
	if got := capturedReport(t, "codex/approval.out.jsonl", ParseCodexContextTokens); got != 16896 {
		t.Fatalf("codex context tokens = %d, want 16896", got)
	}
}

func TestParseClaudeContextTokens(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		want   int
		wantOK bool
	}{
		{
			name:   "anthropic message_delta",
			line:   `{"type":"stream_event","parent_tool_use_id":null,"event":{"type":"message_delta","usage":{"input_tokens":2,"cache_creation_input_tokens":1799,"cache_read_input_tokens":510390,"output_tokens":219}}}`,
			want:   512191,
			wantOK: true,
		},
		{
			name:   "third-party model with null cache fields",
			line:   `{"type":"stream_event","parent_tool_use_id":null,"event":{"type":"message_delta","usage":{"input_tokens":1632,"cache_read_input_tokens":155456,"cache_creation_input_tokens":null}}}`,
			want:   157088,
			wantOK: true,
		},
		{
			name: "subagent message_delta",
			line: `{"type":"stream_event","parent_tool_use_id":"toolu_01","event":{"type":"message_delta","usage":{"input_tokens":5,"cache_read_input_tokens":9000,"cache_creation_input_tokens":0}}}`,
		},
		{
			name: "all-zero message_delta",
			line: `{"type":"stream_event","parent_tool_use_id":null,"event":{"type":"message_delta","usage":{"input_tokens":0,"cache_read_input_tokens":null,"cache_creation_input_tokens":null}}}`,
		},
		{
			name: "assistant line is not a source",
			line: `{"type":"assistant","parent_tool_use_id":null,"message":{"usage":{"input_tokens":2,"cache_read_input_tokens":510000,"cache_creation_input_tokens":390}}}`,
		},
		{
			name: "message_start is not a source",
			line: `{"type":"stream_event","parent_tool_use_id":null,"event":{"type":"message_start","message":{"usage":{"input_tokens":10}}}}`,
		},
		{
			name: "message_delta without usage",
			line: `{"type":"stream_event","parent_tool_use_id":null,"event":{"type":"message_delta","delta":{"stop_reason":"end_turn"}}}`,
		},
		{name: "codex line", line: `{"method":"thread/tokenUsage/updated","params":{"tokenUsage":{"last":{"inputTokens":10}}}}`},
		{name: "not json", line: `not json`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseClaudeContextTokens([]byte(tt.line))
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("ParseClaudeContextTokens = (%d, %v), want (%d, %v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestParseCodexContextTokens(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		want   int
		wantOK bool
	}{
		{
			name:   "token usage update",
			line:   `{"method":"thread/tokenUsage/updated","params":{"threadId":"t","turnId":"u","tokenUsage":{"total":{"totalTokens":40000,"inputTokens":39000},"last":{"totalTokens":19015,"inputTokens":18088,"cachedInputTokens":12160,"outputTokens":927,"reasoningOutputTokens":191},"modelContextWindow":258400}}}`,
			want:   18088,
			wantOK: true,
		},
		{
			name: "zero last input",
			line: `{"method":"thread/tokenUsage/updated","params":{"tokenUsage":{"last":{"inputTokens":0}}}}`,
		},
		{
			name: "missing tokenUsage",
			line: `{"method":"thread/tokenUsage/updated","params":{}}`,
		},
		{name: "rate limits", line: `{"method":"account/rateLimits/updated","params":{"rateLimits":{}}}`},
		{name: "claude line", line: `{"type":"stream_event","event":{"type":"message_delta","usage":{"input_tokens":10}}}`},
		{name: "not json", line: `not json`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseCodexContextTokens([]byte(tt.line))
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("ParseCodexContextTokens = (%d, %v), want (%d, %v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
