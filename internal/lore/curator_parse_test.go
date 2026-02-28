package lore

import (
	"testing"
)

func TestParseCuratorResponse_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		wantSum string // expected DiffSummary if no error
	}{
		{
			name:    "bare JSON",
			input:   `{"proposed_files":{},"diff_summary":"bare","entries_used":[],"entries_discarded":{}}`,
			wantSum: "bare",
		},
		{
			name:    "json fenced",
			input:   "```json\n{\"proposed_files\":{},\"diff_summary\":\"fenced\",\"entries_used\":[],\"entries_discarded\":{}}\n```",
			wantSum: "fenced",
		},
		{
			name:    "plain fenced",
			input:   "```\n{\"proposed_files\":{},\"diff_summary\":\"plain fence\",\"entries_used\":[],\"entries_discarded\":{}}\n```",
			wantSum: "plain fence",
		},
		{
			name:    "leading whitespace",
			input:   "  \n  {\"proposed_files\":{},\"diff_summary\":\"ws\",\"entries_used\":[],\"entries_discarded\":{}}  \n  ",
			wantSum: "ws",
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			input:   "   \n  ",
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   "{not valid json}",
			wantErr: true,
		},
		{
			name:    "missing closing fence",
			input:   "```json\n{\"proposed_files\":{},\"diff_summary\":\"no close\",\"entries_used\":[],\"entries_discarded\":{}}",
			wantSum: "no close",
		},
		{
			name:    "optional fields missing",
			input:   `{"proposed_files":{},"diff_summary":"minimal"}`,
			wantSum: "minimal",
		},
		{
			name:    "fence with trailing text",
			input:   "```json\n{\"proposed_files\":{},\"diff_summary\":\"trail\",\"entries_used\":[],\"entries_discarded\":{}}\n```\nsome trailing text",
			wantSum: "trail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCuratorResponse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseCuratorResponse() expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCuratorResponse() unexpected error: %v", err)
			}
			if got.DiffSummary != tt.wantSum {
				t.Errorf("DiffSummary = %q, want %q", got.DiffSummary, tt.wantSum)
			}
		})
	}
}
