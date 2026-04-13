package branchsuggest

import (
	"context"
	"errors"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
)

func branchSuggestCfg(bs *config.BranchSuggestConfig) *config.Config {
	cfg := &config.Config{}
	cfg.BranchSuggest = bs
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
		{name: "nil branch suggest", cfg: branchSuggestCfg(nil), want: false},
		{name: "empty target", cfg: branchSuggestCfg(&config.BranchSuggestConfig{Target: ""}), want: false},
		{name: "whitespace target", cfg: branchSuggestCfg(&config.BranchSuggestConfig{Target: "  "}), want: false},
		{name: "target set", cfg: branchSuggestCfg(&config.BranchSuggestConfig{Target: "claude"}), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsEnabled(tt.cfg); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAskForPrompt_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		prompt  string
		wantErr error
	}{
		{
			name:    "empty prompt",
			cfg:     branchSuggestCfg(&config.BranchSuggestConfig{Target: "claude"}),
			prompt:  "",
			wantErr: ErrNoPrompt,
		},
		{
			name:    "whitespace-only prompt",
			cfg:     branchSuggestCfg(&config.BranchSuggestConfig{Target: "claude"}),
			prompt:  "   ",
			wantErr: ErrNoPrompt,
		},
		{
			name:    "nil config",
			cfg:     nil,
			prompt:  "add dark mode",
			wantErr: ErrDisabled,
		},
		{
			name:    "empty config (no target)",
			cfg:     &config.Config{},
			prompt:  "add dark mode",
			wantErr: ErrDisabled,
		},
		{
			name:    "empty target string",
			cfg:     branchSuggestCfg(&config.BranchSuggestConfig{Target: ""}),
			prompt:  "add dark mode",
			wantErr: ErrDisabled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := AskForPrompt(context.Background(), tt.cfg, tt.prompt)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("AskForPrompt() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
