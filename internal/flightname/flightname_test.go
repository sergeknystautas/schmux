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
		// wantMsg is the reason the error must carry so name_error shows
		// what the model returned and which rule it broke.
		wantMsg string
	}{
		{name: "trim whitespace", input: "  Fix login bug  ", want: "Fix login bug"},
		{name: "allow slash", input: "feature/dark-mode for Settings", want: "feature/dark-mode for Settings"},
		{name: "empty rejected", input: "", wantErr: ErrInvalidName, wantMsg: `empty: ""`},
		{name: "whitespace only rejected", input: "   ", wantErr: ErrInvalidName, wantMsg: `empty: "   "`},
		{name: "nine words accepted", input: "one two three four five six seven eight nine", want: "one two three four five six seven eight nine"},
		{name: "ten words rejected", input: "one two three four five six seven eight nine ten", wantErr: ErrInvalidName, wantMsg: `10 words, must be fewer than 10: "one two three four five six seven eight nine ten"`},
		{name: "eighty runes accepted", input: strings.Repeat("x", 80), want: strings.Repeat("x", 80)},
		{name: "eighty-one runes rejected", input: strings.Repeat("x", 81), wantErr: ErrInvalidName, wantMsg: "81 characters, limit 80"},
		{name: "control character rejected", input: "a\nb", wantErr: ErrInvalidName, wantMsg: `control character U+000A: "a\nb"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateName(tt.input)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("validateName(%q) error = %v, want %v", tt.input, err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantMsg) {
					t.Fatalf("validateName(%q) error = %q, want it to contain %q", tt.input, err, tt.wantMsg)
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
