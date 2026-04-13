package nudgenik

import (
	"errors"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
)

func nudgenikCfg(n *config.NudgenikConfig) *config.Config {
	cfg := &config.Config{}
	cfg.Nudgenik = n
	return cfg
}

func TestIsEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{name: "nil config", cfg: nil, want: false},
		{name: "empty config", cfg: &config.Config{}, want: false},
		{name: "nil nudgenik", cfg: nudgenikCfg(nil), want: false},
		{name: "empty target", cfg: nudgenikCfg(&config.NudgenikConfig{Target: ""}), want: false},
		{name: "target set", cfg: nudgenikCfg(&config.NudgenikConfig{Target: "claude"}), want: true},
		{name: "whitespace target", cfg: nudgenikCfg(&config.NudgenikConfig{Target: "  "}), want: false}, // trimmed by getter
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsEnabled(tt.cfg); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractLatestFromCapture(t *testing.T) {
	tests := []struct {
		name    string
		capture string
		wantErr error
		wantNE  bool // want non-empty result
	}{
		{name: "empty capture", capture: "", wantErr: ErrNoResponse},
		{name: "only prompt", capture: "❯\n", wantErr: ErrNoResponse},
		{name: "whitespace only", capture: "   \n  \n", wantErr: ErrNoResponse},
		{name: "has response before prompt", capture: "Hello world\n❯\n", wantNE: true},
		{name: "multi-line response", capture: "line 1\nline 2\n❯\n", wantNE: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractLatestFromCapture(tt.capture)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("ExtractLatestFromCapture() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExtractLatestFromCapture() unexpected error: %v", err)
			}
			if tt.wantNE && got == "" {
				t.Error("ExtractLatestFromCapture() returned empty, want non-empty")
			}
		})
	}
}

func TestParseResult(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantOK    bool
		wantState string
		wantMsg   string // substring expected in error message
	}{
		{
			name:      "valid json",
			raw:       `{"state":"Completed","confidence":"high","evidence":["done"],"summary":"Task finished"}`,
			wantOK:    true,
			wantState: "Completed",
		},
		{
			name:      "fenced json",
			raw:       "```json\n{\"state\":\"Needs Input\",\"confidence\":\"medium\",\"evidence\":[\"waiting\"],\"summary\":\"Waiting\"}\n```",
			wantOK:    true,
			wantState: "Needs Input",
		},
		{
			name:      "extra text around json",
			raw:       "Here is the analysis:\n{\"state\":\"Completed\",\"confidence\":\"high\",\"evidence\":[],\"summary\":\"Done\"}\nEnd.",
			wantOK:    true,
			wantState: "Completed",
		},
		{
			name:    "empty string",
			raw:     "",
			wantOK:  false,
			wantMsg: "empty response",
		},
		{
			name:    "no json at all",
			raw:     "just plain text with no braces",
			wantOK:  false,
			wantMsg: "no JSON object found",
		},
		{
			name:    "null literal",
			raw:     "null",
			wantOK:  false,
			wantMsg: "no JSON object found",
		},
		{
			name:   "invalid json",
			raw:    "{invalid: json}",
			wantOK: false,
		},
		{
			name:   "only opening brace",
			raw:    "{",
			wantOK: false,
		},
		{
			name:      "curly quotes",
			raw:       "{\u201cstate\u201d: \u201cCompleted\u201d, \u201cconfidence\u201d: \u201chigh\u201d, \u201cevidence\u201d: [], \u201csummary\u201d: \u201cDone\u201d}",
			wantOK:    true,
			wantState: "Completed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseResult(tt.raw)
			if tt.wantOK {
				if err != nil {
					t.Fatalf("ParseResult() error = %v", err)
				}
				if got.State != tt.wantState {
					t.Errorf("ParseResult().State = %q, want %q", got.State, tt.wantState)
				}
			} else if err == nil {
				t.Fatalf("ParseResult() expected error, got %+v", got)
			} else if tt.wantMsg != "" && !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("ParseResult() error = %q, want substring %q", err.Error(), tt.wantMsg)
			}
		})
	}
}
