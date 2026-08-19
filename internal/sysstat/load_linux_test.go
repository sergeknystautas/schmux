//go:build linux

package sysstat

import "testing"

func TestParseProcLoadavg(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    LoadAvg
		wantErr bool
	}{
		{
			name:    "typical line",
			content: "2.22 3.18 3.28 2/1234 56789\n",
			want:    LoadAvg{One: 2.22, Five: 3.18, Fifteen: 3.28},
		},
		{
			name:    "zero load",
			content: "0.00 0.01 0.05 1/100 999\n",
			want:    LoadAvg{One: 0, Five: 0.01, Fifteen: 0.05},
		},
		{name: "empty", content: "", wantErr: true},
		{name: "too few fields", content: "2.22 3.18\n", wantErr: true},
		{name: "non-numeric", content: "abc 3.18 3.28 2/1234 56789\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProcLoadavg(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseProcLoadavg(%q) = %v, want error", tt.content, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProcLoadavg(%q) error: %v", tt.content, err)
			}
			if got != tt.want {
				t.Errorf("parseProcLoadavg(%q) = %+v, want %+v", tt.content, got, tt.want)
			}
		})
	}
}
