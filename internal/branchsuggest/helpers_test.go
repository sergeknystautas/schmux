package branchsuggest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/oneshot"
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
			name:    "nil config",
			cfg:     nil,
			prompt:  "add dark mode",
			wantErr: oneshot.ErrDisabled,
		},
		{
			name:    "empty config (no target)",
			cfg:     &config.Config{},
			prompt:  "add dark mode",
			wantErr: oneshot.ErrDisabled,
		},
		{
			name:    "empty target string",
			cfg:     branchSuggestCfg(&config.BranchSuggestConfig{Target: ""}),
			prompt:  "add dark mode",
			wantErr: oneshot.ErrDisabled,
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

func TestBranchSuggestPrompt_AllowsEmptyPrompt(t *testing.T) {
	input := branchSuggestPrompt("   ")
	if !strings.Contains(input, blankPromptDescription) {
		t.Fatalf("branchSuggestPrompt() did not include blank prompt description")
	}
	if strings.Contains(input, "{{USER_PROMPT}}") {
		t.Fatalf("branchSuggestPrompt() left template placeholder unresolved")
	}
}

func TestValidateSuggestedBranchRejectsDefaultBranches(t *testing.T) {
	for _, branch := range []string{"main", "master"} {
		t.Run(branch, func(t *testing.T) {
			if err := validateSuggestedBranch(branch); !errors.Is(err, ErrInvalidBranch) {
				t.Fatalf("validateSuggestedBranch(%q) error = %v, want %v", branch, err, ErrInvalidBranch)
			}
		})
	}
}
