package dashboard

import (
	"testing"
)

func TestParseNudgeSummary(t *testing.T) {
	tests := []struct {
		name      string
		nudge     string
		wantState string
		wantSumm  string
	}{
		{
			name:      "empty string",
			nudge:     "",
			wantState: "",
			wantSumm:  "",
		},
		{
			name:      "whitespace only",
			nudge:     "   \n  ",
			wantState: "",
			wantSumm:  "",
		},
		{
			name:      "valid JSON",
			nudge:     `{"state":"Completed","confidence":"high","evidence":["done"],"summary":"Task finished"}`,
			wantState: "Completed",
			wantSumm:  "Task finished",
		},
		{
			name:      "state with whitespace trimmed",
			nudge:     `{"state":"  Needs Input  ","confidence":"medium","evidence":[],"summary":"  Waiting for input  "}`,
			wantState: "Needs Input",
			wantSumm:  "Waiting for input",
		},
		{
			name:      "malformed JSON returns empty",
			nudge:     "{not valid json}",
			wantState: "",
			wantSumm:  "",
		},
		{
			name:      "no braces returns empty",
			nudge:     "just plain text",
			wantState: "",
			wantSumm:  "",
		},
		{
			name:      "fenced JSON",
			nudge:     "```json\n{\"state\":\"Working\",\"confidence\":\"high\",\"evidence\":[],\"summary\":\"Building\"}\n```",
			wantState: "Working",
			wantSumm:  "Building",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotState, gotSumm := parseNudgeSummary(tt.nudge)
			if gotState != tt.wantState {
				t.Errorf("parseNudgeSummary(%q) state = %q, want %q", tt.nudge, gotState, tt.wantState)
			}
			if gotSumm != tt.wantSumm {
				t.Errorf("parseNudgeSummary(%q) summary = %q, want %q", tt.nudge, gotSumm, tt.wantSumm)
			}
		})
	}
}
