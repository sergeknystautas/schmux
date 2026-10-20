// Package flightname generates short, human-readable display names for
// flights from their task prompt via a oneshot LLM call.
package flightname

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schema"
)

// MaxNameRunes is the longest allowed flight name. Matches the existing
// limit on ShortName set by callers.
const MaxNameRunes = 80

// MaxNameWords is the maximum number of whitespace-separated words in a
// flight name.
const MaxNameWords = 10

// ErrInvalidName is returned when an LLM-produced name fails validation.
var ErrInvalidName = errors.New("invalid flight name")

const flightNameTimeout = 30 * time.Second

// Prompt is the system prompt for flight name generation.
const Prompt = `
You are naming a unit of coding work from its task prompt.

Write a short, human-readable description of the work: a snippet a person can scan in a list.

Rules:
- Fewer than 10 words
- Plain prose, normal capitalization; punctuation is fine
- Describe what the work is about, not that it is a task
- No trailing period

Here is the task prompt:
<<<
{{USER_PROMPT}}
>>>
`

// Result is the parsed flight name response.
// Struct tags control JSON schema generation via swaggest/jsonschema-go.
type Result struct {
	Name string   `json:"name" required:"true"`
	_    struct{} `additionalProperties:"false"`
}

func init() {
	schema.Register(schema.LabelFlightName, Result{})
}

// AskForPrompt generates a flight name from a user prompt.
// Errors surfaced:
//   - oneshot.ErrDisabled          (no target configured)
//   - oneshot.ErrTargetNotFound    (configured target missing)
//   - oneshot.ErrInvalidResponse   (LLM output not parseable)
//   - ErrInvalidName               (LLM returned a name that failed validation)
func AskForPrompt(ctx context.Context, cfg *config.Config, userPrompt string) (Result, error) {
	input := flightNamePrompt(userPrompt)

	result, err := oneshot.ExecuteTarget[Result](ctx, cfg, cfg.GetFlightNameTarget(), input, schema.LabelFlightName, flightNameTimeout, "")
	if err != nil {
		return Result{}, err
	}

	name, err := validateName(result.Name)
	if err != nil {
		return Result{}, err
	}
	result.Name = name
	return result, nil
}

// validateName trims and rejects names that are empty, contain control
// characters, reach MaxNameWords, or exceed MaxNameRunes runes. The error
// wraps ErrInvalidName with the broken rule and the rejected name, so the
// flight's name_error shows what the model returned.
func validateName(name string) (string, error) {
	raw := name
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: empty: %q", ErrInvalidName, raw)
	}
	if n := utf8.RuneCountInString(name); n > MaxNameRunes {
		return "", fmt.Errorf("%w: %d characters, limit %d: %q", ErrInvalidName, n, MaxNameRunes, name)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: contains control character %U: %q", ErrInvalidName, r, name)
		}
	}
	if n := len(strings.Fields(name)); n >= MaxNameWords {
		return "", fmt.Errorf("%w: %d words, must be fewer than %d: %q", ErrInvalidName, n, MaxNameWords, name)
	}
	return name, nil
}

func flightNamePrompt(userPrompt string) string {
	return strings.ReplaceAll(Prompt, "{{USER_PROMPT}}", userPrompt)
}
