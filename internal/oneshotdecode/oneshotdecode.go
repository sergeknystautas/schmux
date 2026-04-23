// Package oneshotdecode holds the JSON decode + error semantics shared by
// the CLI one-shot path (internal/oneshot) and the direct-HTTP path
// (internal/directhttp). It exists because oneshot imports directhttp for
// transport routing; putting the decode machinery here breaks the cycle.
package oneshotdecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidResponse is returned when an LLM response cannot be decoded into
// the expected struct, even after fence-stripping and NormalizeJSONPayload
// recovery. It is wrapped by *InvalidResponseError which carries the raw
// response for logging.
var ErrInvalidResponse = errors.New("invalid oneshot response")

// InvalidResponseError carries the raw LLM response alongside the decode
// failure. Extract via errors.As; errors.Is against ErrInvalidResponse still
// succeeds via the Is method below.
type InvalidResponseError struct {
	Raw string
	Err error
}

func (e *InvalidResponseError) Error() string {
	return fmt.Sprintf("invalid oneshot response: %v", e.Err)
}

func (e *InvalidResponseError) Unwrap() error { return e.Err }

func (e *InvalidResponseError) Is(target error) bool {
	return target == ErrInvalidResponse
}

// NormalizeJSONPayload normalizes common JSON encoding issues in LLM output:
// fancy quotes, stray tabs, doubled spaces. Empty input returns empty string.
func NormalizeJSONPayload(payload string) string {
	fixed := strings.TrimSpace(payload)
	if fixed == "" {
		return ""
	}
	fixed = strings.ReplaceAll(fixed, "“", "\"")
	fixed = strings.ReplaceAll(fixed, "”", "\"")
	fixed = strings.ReplaceAll(fixed, "'", "'")
	fixed = strings.ReplaceAll(fixed, "\t", " ")
	for strings.Contains(fixed, "  ") {
		fixed = strings.ReplaceAll(fixed, "  ", " ")
	}
	fixed = strings.TrimSpace(fixed)
	return fixed
}

// Decode strips ```json code fences, locates the JSON object in raw, and
// decodes it into T. On unmarshal failure it retries once with
// NormalizeJSONPayload applied. Returns the underlying decode error on
// failure; callers wrap it with *InvalidResponseError.
func Decode[T any](raw string) (T, error) {
	var zero T
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return zero, fmt.Errorf("empty response")
	}

	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "```json"))
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "```"))
	}

	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start == -1 || end == -1 || end <= start {
		return zero, fmt.Errorf("no JSON object found")
	}

	payload := trimmed[start : end+1]
	var result T
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		normalized := NormalizeJSONPayload(payload)
		if normalized == "" {
			return zero, err
		}
		if err2 := json.Unmarshal([]byte(normalized), &result); err2 != nil {
			return zero, err2
		}
	}
	return result, nil
}
