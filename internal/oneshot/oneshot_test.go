package oneshot

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/detect"
)

func TestBuildOneShotCommand(t *testing.T) {
	tests := []struct {
		name         string
		agentName    string
		agentCommand string
		jsonSchema   string
		model        *detect.Model
		want         []string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "claude simple",
			agentName:    "claude",
			agentCommand: "claude",
			jsonSchema:   "",
			model:        nil,
			want:         []string{"claude", "-p", "--dangerously-skip-permissions", "--output-format", "json"},
			wantErr:      false,
		},
		{
			name:         "claude with path",
			agentName:    "claude",
			agentCommand: "/home/user/.local/bin/claude",
			jsonSchema:   "",
			model:        nil,
			want:         []string{"/home/user/.local/bin/claude", "-p", "--dangerously-skip-permissions", "--output-format", "json"},
			wantErr:      false,
		},
		{
			name:         "claude with json schema",
			agentName:    "claude",
			agentCommand: "claude",
			jsonSchema:   `{"type":"object"}`,
			model:        nil,
			want:         []string{"claude", "-p", "--dangerously-skip-permissions", "--output-format", "json", "--json-schema", `{"type":"object"}`},
			wantErr:      false,
		},
		{
			name:         "codex simple",
			agentName:    "codex",
			agentCommand: "codex",
			jsonSchema:   "",
			model:        nil,
			want:         []string{"codex", "exec", "--json"},
			wantErr:      false,
		},
		{
			name:         "codex with json schema",
			agentName:    "codex",
			agentCommand: "codex",
			jsonSchema:   "/tmp/schema.json",
			model:        nil,
			want:         []string{"codex", "exec", "--json", "--output-schema", "/tmp/schema.json"},
			wantErr:      false,
		},
		{
			name:         "codex with model flag",
			agentName:    "codex",
			agentCommand: "codex",
			jsonSchema:   "",
			model: &detect.Model{
				ID:         "gpt-5.2-codex",
				ModelValue: "gpt-5.2-codex",
				ModelFlag:  "-m",
			},
			want:    []string{"codex", "exec", "--json", "-m", "gpt-5.2-codex"},
			wantErr: false,
		},
		{
			name:         "codex with model flag and json schema",
			agentName:    "codex",
			agentCommand: "codex",
			jsonSchema:   "/tmp/schema.json",
			model: &detect.Model{
				ID:         "gpt-5.3-codex",
				ModelValue: "gpt-5.3-codex",
				ModelFlag:  "-m",
			},
			want:    []string{"codex", "exec", "--json", "-m", "gpt-5.3-codex", "--output-schema", "/tmp/schema.json"},
			wantErr: false,
		},
		{
			name:         "claude with model flag is ignored (no flag)",
			agentName:    "claude",
			agentCommand: "claude",
			jsonSchema:   "",
			model: &detect.Model{
				ID:         "claude-sonnet",
				ModelValue: "claude-sonnet-4-5-20250929",
				ModelFlag:  "", // No flag - uses env vars
			},
			want:    []string{"claude", "-p", "--dangerously-skip-permissions", "--output-format", "json"},
			wantErr: false,
		},
		{
			name:         "gemini not supported",
			agentName:    "gemini",
			agentCommand: "gemini",
			jsonSchema:   "",
			model:        nil,
			want:         nil,
			wantErr:      true,
			errContains:  "not supported",
		},
		{
			name:         "unknown agent",
			agentName:    "unknown",
			agentCommand: "unknown",
			jsonSchema:   "",
			model:        nil,
			want:         nil,
			wantErr:      true,
			errContains:  "unknown tool",
		},
		{
			name:         "empty command",
			agentName:    "claude",
			agentCommand: "",
			jsonSchema:   "",
			model:        nil,
			want:         nil,
			wantErr:      true,
			errContains:  "empty command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := detect.BuildCommandParts(tt.agentName, tt.agentCommand, detect.ToolModeOneshot, tt.jsonSchema, tt.model)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildCommandParts() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("BuildCommandParts() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("BuildCommandParts() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExecuteInputValidation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		agentName   string
		agentCmd    string
		prompt      string
		wantErr     bool
		errContains string
	}{
		{
			name:        "empty agent name",
			agentName:   "",
			agentCmd:    "claude",
			prompt:      "test",
			wantErr:     true,
			errContains: "agent name cannot be empty",
		},
		{
			name:        "empty agent command",
			agentName:   "claude",
			agentCmd:    "",
			prompt:      "test",
			wantErr:     true,
			errContains: "agent command cannot be empty",
		},
		{
			name:        "empty prompt",
			agentName:   "claude",
			agentCmd:    "claude",
			prompt:      "",
			wantErr:     true,
			errContains: "prompt cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Execute(ctx, tt.agentName, tt.agentCmd, tt.prompt, "", nil, "", nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Execute() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestExecuteRejectsEmptySchemaLabel(t *testing.T) {
	ctx := context.Background()
	_, err := Execute(ctx, "claude", "claude", "test prompt", "", nil, "", nil)
	if err == nil {
		t.Fatal("expected error when schemaLabel is empty")
	}
	if !strings.Contains(err.Error(), "schema label cannot be empty") {
		t.Errorf("expected 'schema label cannot be empty' error, got: %v", err)
	}
}

func TestParseClaudeStructuredOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "extracts structured_output",
			input:    `{"structured_output":{"branch":"feature/test","nickname":"Test"},"duration_ms":1000}`,
			expected: `{"branch":"feature/test","nickname":"Test"}`,
		},
		{
			name:     "returns as-is when not json",
			input:    "plain text output",
			expected: "plain text output",
		},
		{
			name:     "returns as-is when json without structured_output",
			input:    `{"result":"something"}`,
			expected: `{"result":"something"}`,
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "banner preamble before json with structured_output",
			input:    "Some CLI Banner v1.2.3 (https://example.com)\n" + `{"structured_output":{"branch":"feature/test"},"duration_ms":500}`,
			expected: `{"branch":"feature/test"}`,
		},
		{
			name:     "banner preamble before json without structured_output",
			input:    "Some CLI Banner v1.2.3 (https://example.com)\n" + `{"result":"something"}`,
			expected: "Some CLI Banner v1.2.3 (https://example.com)\n" + `{"result":"something"}`,
		},
		{
			name:     "banner preamble and trailing stderr",
			input:    "Some CLI Banner v1.2.3\n" + `{"structured_output":{"key":"val"},"duration_ms":100}` + "\nfatal: signal killed",
			expected: `{"key":"val"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseClaudeStructuredOutput(tt.input)
			if got != tt.expected {
				t.Errorf("parseClaudeStructuredOutput() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestParseCodexJSONLOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "extracts agent_message from jsonl",
			input: `{"type":"thread.started","thread_id":"abc"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"{\"branch\":\"feature/test\",\"nickname\":\"Test\"}"}}`,
			expected: `{"branch":"feature/test","nickname":"Test"}`,
		},
		{
			name:     "returns as-is for plain text",
			input:    "plain text output",
			expected: "plain text output",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name: "malformed json lines ignored",
			input: `not json
{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"{\"result\":\"value\"}"}}`,
			expected: `{"result":"value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCodexJSONLOutput(tt.input)
			if got != tt.expected {
				t.Errorf("parseCodexJSONLOutput() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// Note: TestSchemaRegistry has been moved to internal/oneshot/schema_integration_test.go
// to avoid import cycles. It validates that all registered schemas meet OpenAI requirements.

func TestParseResponse(t *testing.T) {
	tests := []struct {
		name      string
		agentName string
		output    string
		expected  string
	}{
		{
			name:      "claude extracts structured output",
			agentName: "claude",
			output:    `{"structured_output":{"greeting":"hello"},"duration_ms":1000}`,
			expected:  `{"greeting":"hello"}`,
		},
		{
			name:      "claude returns as-is when no structured output",
			agentName: "claude",
			output:    `{"result":"something"}`,
			expected:  `{"result":"something"}`,
		},
		{
			name:      "codex extracts agent message",
			agentName: "codex",
			output:    `{"type":"item.completed","item":{"type":"agent_message","text":"{\"greeting\":\"hello\"}"}}`,
			expected:  `{"greeting":"hello"}`,
		},
		{
			name:      "codex returns as-is for plain text",
			agentName: "codex",
			output:    "Some output",
			expected:  "Some output",
		},
		{
			name:      "unknown agent passes through",
			agentName: "unknown",
			output:    "Fallback output",
			expected:  "Fallback output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseResponse(tt.agentName, tt.output)
			if got != tt.expected {
				t.Errorf("parseResponse() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestStreamEventParsing(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantType    string
		wantSubtype string
	}{
		{
			name:        "system init event",
			line:        `{"type":"system","subtype":"init","session_id":"abc123"}`,
			wantType:    "system",
			wantSubtype: "init",
		},
		{
			name:     "assistant event",
			line:     `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}`,
			wantType: "assistant",
		},
		{
			name:     "result event",
			line:     `{"type":"result","subtype":"success","is_error":false,"result":"done","structured_output":{"key":"value"}}`,
			wantType: "result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ev StreamEvent
			if err := json.Unmarshal([]byte(tt.line), &ev); err != nil {
				t.Fatalf("failed to parse: %v", err)
			}
			if ev.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", ev.Type, tt.wantType)
			}
			if tt.wantSubtype != "" && ev.Subtype != tt.wantSubtype {
				t.Errorf("Subtype = %q, want %q", ev.Subtype, tt.wantSubtype)
			}
		})
	}
}

func TestResultEventParsing(t *testing.T) {
	line := `{"type":"result","subtype":"success","is_error":false,"duration_ms":5000,"result":"done","structured_output":{"proposed_files":{"README.md":"# Hello"},"diff_summary":"Added readme"}}`

	var re ResultEvent
	if err := json.Unmarshal([]byte(line), &re); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if re.Type != "result" {
		t.Errorf("Type = %q, want 'result'", re.Type)
	}
	if re.IsError {
		t.Error("IsError should be false")
	}
	if re.DurationMs != 5000 {
		t.Errorf("DurationMs = %d, want 5000", re.DurationMs)
	}
	if len(re.StructuredOutput) == 0 {
		t.Fatal("StructuredOutput should not be empty")
	}
	// Verify we can extract the structured output as a string
	output := string(re.StructuredOutput)
	if !strings.Contains(output, "proposed_files") {
		t.Errorf("StructuredOutput should contain 'proposed_files', got: %s", output)
	}
}

func TestResultEventErrorParsing(t *testing.T) {
	line := `{"type":"result","subtype":"error","is_error":true,"result":"something went wrong","structured_output":null}`

	var re ResultEvent
	if err := json.Unmarshal([]byte(line), &re); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if !re.IsError {
		t.Error("IsError should be true")
	}
	if re.Result != "something went wrong" {
		t.Errorf("Result = %q, want 'something went wrong'", re.Result)
	}
}

func TestExecuteTargetStreamingInputValidation(t *testing.T) {
	ctx := context.Background()

	_, err := ExecuteTargetStreaming(ctx, nil, "test", "", "schema", time.Minute, "", nil)
	if err == nil || !strings.Contains(err.Error(), "prompt cannot be empty") {
		t.Errorf("expected 'prompt cannot be empty' error, got: %v", err)
	}

	_, err = ExecuteTargetStreaming(ctx, nil, "test", "prompt", "", time.Minute, "", nil)
	if err == nil || !strings.Contains(err.Error(), "schema label cannot be empty") {
		t.Errorf("expected 'schema label cannot be empty' error, got: %v", err)
	}
}

func TestMergeEnvMaps(t *testing.T) {
	t.Parallel()

	t.Run("overrides win on conflict", func(t *testing.T) {
		base := map[string]string{"A": "1", "B": "2"}
		overrides := map[string]string{"B": "override", "C": "3"}
		got := mergeEnvMaps(base, overrides)

		if got["A"] != "1" {
			t.Errorf("A = %q, want '1'", got["A"])
		}
		if got["B"] != "override" {
			t.Errorf("B = %q, want 'override'", got["B"])
		}
		if got["C"] != "3" {
			t.Errorf("C = %q, want '3'", got["C"])
		}
	})

	t.Run("both nil returns nil", func(t *testing.T) {
		got := mergeEnvMaps(nil, nil)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("nil base returns overrides copy", func(t *testing.T) {
		overrides := map[string]string{"K": "V"}
		got := mergeEnvMaps(nil, overrides)
		if got["K"] != "V" || len(got) != 1 {
			t.Errorf("got %v, want map[K:V]", got)
		}
	})

	t.Run("nil overrides returns base copy", func(t *testing.T) {
		base := map[string]string{"K": "V"}
		got := mergeEnvMaps(base, nil)
		if got["K"] != "V" || len(got) != 1 {
			t.Errorf("got %v, want map[K:V]", got)
		}
	})

	t.Run("does not mutate inputs", func(t *testing.T) {
		base := map[string]string{"A": "1"}
		overrides := map[string]string{"B": "2"}
		got := mergeEnvMaps(base, overrides)
		got["A"] = "mutated"
		if base["A"] == "mutated" {
			t.Error("mergeEnvMaps should not allow mutation of base")
		}
	})
}

func TestNormalizeJSONPayload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty returns empty", "", ""},
		{"whitespace only returns empty", "   \t  ", ""},
		{"replaces curly double quotes", "\u201chello\u201d", "\"hello\""},
		{"collapses multiple spaces", "a  b   c", "a b c"},
		{"replaces tabs with spaces", "a\tb", "a b"},
		{"trims surrounding whitespace", "  hello  ", "hello"},
		{"combined normalization", " \u201ckey\u201d :  \u201cvalue\u201d ", "\"key\" : \"value\""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeJSONPayload(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeJSONPayload(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
