package nudgenik

import (
	"errors"
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
