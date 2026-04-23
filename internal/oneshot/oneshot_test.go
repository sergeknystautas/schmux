package oneshot

import (
	"context"
	"errors"
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
			name:         "codex with model via Runners",
			agentName:    "codex",
			agentCommand: "codex",
			jsonSchema:   "",
			model: &detect.Model{
				ID: "gpt-5.2-codex",
				Runners: map[string]detect.RunnerSpec{
					"codex":    {ModelValue: "gpt-5.2-codex"},
					"opencode": {ModelValue: "openai/gpt-5.2-codex"},
				},
			},
			want:    []string{"codex", "exec", "--json", "-m", "gpt-5.2-codex"},
			wantErr: false,
		},
		{
			name:         "codex with model and json schema",
			agentName:    "codex",
			agentCommand: "codex",
			jsonSchema:   "/tmp/schema.json",
			model: &detect.Model{
				ID: "gpt-5.3-codex",
				Runners: map[string]detect.RunnerSpec{
					"codex":    {ModelValue: "gpt-5.3-codex"},
					"opencode": {ModelValue: "openai/gpt-5.3-codex"},
				},
			},
			want:    []string{"codex", "exec", "--json", "-m", "gpt-5.3-codex", "--output-schema", "/tmp/schema.json"},
			wantErr: false,
		},
		{
			name:         "claude model uses RunnerFor - no oneshot model injection",
			agentName:    "claude",
			agentCommand: "claude",
			jsonSchema:   "",
			model: &detect.Model{
				ID: "claude-sonnet-4-6",
				Runners: map[string]detect.RunnerSpec{
					"claude": {ModelValue: "claude-sonnet-4-5-20250929"},
				},
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

func TestExecuteAcceptsEmptySchemaLabel(t *testing.T) {
	ctx := context.Background()
	// Use a non-existent binary so Execute fails fast at exec, not at validation.
	// We only care that empty schemaLabel passes validation — not that the command succeeds.
	_, err := Execute(ctx, "claude", "/nonexistent-schmux-test-binary", "test prompt", "", nil, "", nil)
	if err != nil && strings.Contains(err.Error(), "schema label cannot be empty") {
		t.Fatal("Execute should accept empty schema label to allow prompt-only JSON output without constrained decoding")
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
			name:     "extracts result when no structured_output",
			input:    `{"result":"something"}`,
			expected: "something",
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
			name:     "banner preamble before json extracts result",
			input:    "Some CLI Banner v1.2.3 (https://example.com)\n" + `{"result":"something"}`,
			expected: "something",
		},
		{
			name:     "banner preamble and trailing stderr",
			input:    "Some CLI Banner v1.2.3\n" + `{"structured_output":{"key":"val"},"duration_ms":100}` + "\nfatal: signal killed",
			expected: `{"key":"val"}`,
		},
		{
			name:     "result field newlines are unescaped",
			input:    `{"result":"<SUMMARY>test</SUMMARY>\n<MERGED>\n# Project\n\nContent here\n</MERGED>","is_error":false}`,
			expected: "<SUMMARY>test</SUMMARY>\n<MERGED>\n# Project\n\nContent here\n</MERGED>",
		},
		{
			name:     "null structured_output falls through to result",
			input:    `{"structured_output":null,"result":"fallback text","is_error":false}`,
			expected: "fallback text",
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
			name:      "claude extracts result when no structured output",
			agentName: "claude",
			output:    `{"result":"something"}`,
			expected:  "something",
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

// ===== Tests for unified ExecuteTarget[T] generic (rev 2026-04-20) =====

type unifiedTestResult struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestExecuteTarget_RejectsEmptySchemaLabel(t *testing.T) {
	_, err := ExecuteTarget[unifiedTestResult](context.Background(), nil, "some-target", "some prompt", "", 0, "")
	if !errors.Is(err, ErrNoSchemaLabel) {
		t.Fatalf("want ErrNoSchemaLabel, got %v", err)
	}
}

func TestExecuteTarget_ErrorPrecedence_SchemaLabelBeforeTarget(t *testing.T) {
	// Both empty: ErrNoSchemaLabel must win.
	_, err := ExecuteTarget[unifiedTestResult](context.Background(), nil, "", "some prompt", "", 0, "")
	if !errors.Is(err, ErrNoSchemaLabel) {
		t.Fatalf("want ErrNoSchemaLabel (schemaLabel check precedes target check), got %v", err)
	}
}

func TestExecuteTarget_EmptyTargetReturnsErrDisabled_WithSchema(t *testing.T) {
	// Schema present, target empty: ErrDisabled.
	_, err := ExecuteTarget[unifiedTestResult](context.Background(), nil, "", "some prompt", "test-label", 0, "")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("want ErrDisabled, got %v", err)
	}
}

func TestInvalidResponseError_Unwraps(t *testing.T) {
	inner := errors.New("json decode boom")
	ire := &InvalidResponseError{Raw: "raw payload here", Err: inner}

	if !errors.Is(ire, ErrInvalidResponse) {
		t.Fatalf("errors.Is ErrInvalidResponse should match, got false")
	}
	if !errors.Is(ire, inner) {
		t.Fatalf("errors.Is underlying error should match")
	}

	var extracted *InvalidResponseError
	if !errors.As(ire, &extracted) {
		t.Fatalf("errors.As should extract *InvalidResponseError")
	}
	if extracted.Raw != "raw payload here" {
		t.Fatalf("Raw mismatch: %q", extracted.Raw)
	}
}

// ===== Tests for decodeResponse[T] (relocated from oneshot_json_test.go) =====

type decodeTestResult struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestDecodeResponse_PlainObject(t *testing.T) {
	got, err := decodeResponse[decodeTestResult](`{"name":"foo","value":7}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "foo" || got.Value != 7 {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeResponse_StripsCodeFence(t *testing.T) {
	raw := "```json\n{\"name\":\"bar\",\"value\":1}\n```"
	got, err := decodeResponse[decodeTestResult](raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "bar" || got.Value != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeResponse_HandlesBannerBeforeAndAfter(t *testing.T) {
	raw := "blah blah {\"name\":\"baz\",\"value\":42} trailing"
	got, err := decodeResponse[decodeTestResult](raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "baz" || got.Value != 42 {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeResponse_CurlyQuotesRecoveredByNormalize(t *testing.T) {
	raw := "{\u201cname\u201d:\u201chello\u201d,\u201cvalue\u201d:3}"
	got, err := decodeResponse[decodeTestResult](raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "hello" || got.Value != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeResponse_EmptyInput(t *testing.T) {
	_, err := decodeResponse[decodeTestResult]("")
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("want empty-response error, got %v", err)
	}
}

func TestDecodeResponse_NoBraces(t *testing.T) {
	_, err := decodeResponse[decodeTestResult]("nothing json-like here")
	if err == nil || !strings.Contains(err.Error(), "no JSON object") {
		t.Fatalf("want no-JSON error, got %v", err)
	}
}

func TestDecodeResponse_MalformedJSONBeyondRecovery(t *testing.T) {
	_, err := decodeResponse[decodeTestResult](`{"name": "unterminated`)
	if err == nil {
		t.Fatalf("want decode error, got nil")
	}
}

func TestExecuteTarget_OldEmptyTargetTest(t *testing.T) {
	// Legacy test: empty target with empty schema returns ErrNoSchemaLabel now.
	_, err := ExecuteTarget[unifiedTestResult](context.TODO(), nil, "", "some prompt", "", 0, "")
	if !errors.Is(err, ErrNoSchemaLabel) {
		t.Fatalf("want ErrNoSchemaLabel, got %v", err)
	}
}

// ===== Tests: additional fence / prefix edge cases (§6.1 backfill) =====

func TestDecodeResponse_MissingClosingFence(t *testing.T) {
	raw := "```json\n{\"name\":\"foo\",\"value\":1}"
	got, err := decodeResponse[decodeTestResult](raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "foo" || got.Value != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeResponse_FenceWithTrailingText(t *testing.T) {
	raw := "```json\n{\"name\":\"foo\",\"value\":1}\n```\n\nextra commentary"
	got, err := decodeResponse[decodeTestResult](raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "foo" {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeResponse_TextBeforeFence(t *testing.T) {
	raw := "Here is the result:\n```json\n{\"name\":\"foo\",\"value\":1}\n```"
	got, err := decodeResponse[decodeTestResult](raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "foo" {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeResponse_PlainFencedNoLangTag(t *testing.T) {
	raw := "```\n{\"name\":\"foo\",\"value\":1}\n```"
	got, err := decodeResponse[decodeTestResult](raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "foo" {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeResponse_SpuriousPrefix(t *testing.T) {
	raw := "Of course - here you go. {\"name\":\"foo\",\"value\":1}"
	got, err := decodeResponse[decodeTestResult](raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "foo" {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeResponse_WhitespaceOnly(t *testing.T) {
	_, err := decodeResponse[decodeTestResult]("   \n\t  ")
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("want empty-response, got %v", err)
	}
}

// End-to-end: ExecuteTarget wraps decode failure in *InvalidResponseError with raw.
func TestExecuteTarget_DecodeFailureWrapsRaw(t *testing.T) {
	raw := "not-json-at-all"
	_, err := decodeResponse[decodeTestResult](raw)
	if err == nil {
		t.Fatal("expected decode error")
	}
	wrapped := &InvalidResponseError{Raw: raw, Err: err}
	if !errors.Is(wrapped, ErrInvalidResponse) {
		t.Fatalf("errors.Is ErrInvalidResponse should match wrapped error")
	}
	var ire *InvalidResponseError
	if !errors.As(wrapped, &ire) {
		t.Fatal("errors.As should extract *InvalidResponseError")
	}
	if ire.Raw != raw {
		t.Fatalf("Raw mismatch: %q", ire.Raw)
	}
}

func TestExecuteTarget_APISuffix_RoutesToDirectHTTP(t *testing.T) {
	_, err := ExecuteTarget[unifiedTestResult](
		context.Background(), nil, "claude-sonnet-4-6::api",
		"some prompt", "someLabel", 5*time.Second, "")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ErrTargetNotFound) {
		t.Fatal("::api target incorrectly reached the CLI path")
	}
}

func TestExecuteTarget_BareID_StaysOnCLIPath(t *testing.T) {
	_, err := ExecuteTarget[unifiedTestResult](
		context.Background(), nil, "claude-sonnet-4-6",
		"some prompt", "someLabel", 5*time.Second, "")

	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("expected ErrTargetNotFound (CLI path), got: %v", err)
	}
}
