package flightname

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/oneshot"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "trim whitespace", input: "  Fix login bug  ", want: "Fix login bug"},
		{name: "allow slash", input: "feature/dark-mode for Settings", want: "feature/dark-mode for Settings"},
		{name: "empty rejected", input: "", wantErr: ErrInvalidName},
		{name: "whitespace only rejected", input: "   ", wantErr: ErrInvalidName},
		{name: "nine words accepted", input: "one two three four five six seven eight nine", want: "one two three four five six seven eight nine"},
		{name: "ten words rejected", input: "one two three four five six seven eight nine ten", wantErr: ErrInvalidName},
		{name: "eighty runes accepted", input: strings.Repeat("x", 80), want: strings.Repeat("x", 80)},
		{name: "eighty-one runes rejected", input: strings.Repeat("x", 81), wantErr: ErrInvalidName},
		{name: "control character rejected", input: "a\nb", wantErr: ErrInvalidName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateName(tt.input)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("validateName(%q) error = %v, want %v", tt.input, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateName(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("validateName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFlightNamePrompt(t *testing.T) {
	userPrompt := "Fix the login bug"
	out := flightNamePrompt(userPrompt)
	if !strings.Contains(out, "Fewer than 10 words") {
		t.Errorf("prompt missing constraint phrase")
	}
	if !strings.Contains(out, userPrompt) {
		t.Errorf("prompt missing user input")
	}
	if !strings.Contains(out, "<<<") || !strings.Contains(out, ">>>") {
		t.Errorf("prompt missing delimiters")
	}
	if strings.Contains(out, "{{USER_PROMPT}}") {
		t.Errorf("prompt still contains template placeholder")
	}
}

func TestAskForPrompt_EmptyTargetIsDisabled(t *testing.T) {
	_, err := AskForPrompt(context.Background(), &config.Config{}, "x")
	if !errors.Is(err, oneshot.ErrDisabled) {
		t.Errorf("AskForPrompt with empty config error = %v, want oneshot.ErrDisabled", err)
	}
}
