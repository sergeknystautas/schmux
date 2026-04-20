package oneshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
)

// ErrDisabled is returned when a oneshot helper is invoked with an empty target
// name. Callers use it to distinguish "feature turned off" from
// ErrTargetNotFound ("configured target does not exist").
var ErrDisabled = errors.New("oneshot target is disabled")

// ErrInvalidResponse is returned when an LLM response cannot be decoded into
// the expected struct, even after fence-stripping and NormalizeJSONPayload
// recovery. It wraps the underlying json.Unmarshal error so callers can
// inspect it with errors.Unwrap.
var ErrInvalidResponse = errors.New("invalid oneshot response")

// ParseJSON strips ```json code fences, locates the JSON object in raw, and
// decodes it into T. On unmarshal failure it retries once with
// NormalizeJSONPayload applied. Returns ErrInvalidResponse (wrapping the
// underlying cause) on failure.
func ParseJSON[T any](raw string) (T, error) {
	var zero T
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return zero, fmt.Errorf("%w: empty response", ErrInvalidResponse)
	}

	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "```json"))
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "```"))
	}

	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start == -1 || end == -1 || end <= start {
		return zero, fmt.Errorf("%w: no JSON object found", ErrInvalidResponse)
	}

	payload := trimmed[start : end+1]
	var result T
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		normalized := NormalizeJSONPayload(payload)
		if normalized == "" {
			return zero, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
		}
		if err2 := json.Unmarshal([]byte(normalized), &result); err2 != nil {
			return zero, fmt.Errorf("%w: %v", ErrInvalidResponse, err2)
		}
	}
	return result, nil
}

// ExecuteTargetJSON resolves a target, runs ExecuteTarget, and decodes the
// response into T. Returns:
//   - ErrDisabled        when targetName is empty
//   - ErrTargetNotFound  when the target is not in config
//   - ErrInvalidResponse (wrapped) when the response cannot be parsed as T
//
// The second return value is the raw LLM response — empty on success and on
// pre-parse errors, populated on parse failures so callers can surface it
// (e.g. conflictresolve → linear_sync).
func ExecuteTargetJSON[T any](
	ctx context.Context,
	cfg *config.Config,
	targetName, prompt, schemaLabel string,
	timeout time.Duration,
	dir string,
) (T, string, error) {
	var zero T
	response, err := ExecuteTarget(ctx, cfg, targetName, prompt, schemaLabel, timeout, dir)
	if err != nil {
		return zero, "", err
	}
	parsed, err := ParseJSON[T](response)
	if err != nil {
		return zero, response, err
	}
	return parsed, "", nil
}
