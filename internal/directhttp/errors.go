package directhttp

import (
	"errors"
	"fmt"
	"net/http"
)

var ErrNotImplemented = errors.New("direct-HTTP transport not implemented for this target")

var ErrMissingToken = errors.New("direct-HTTP: required auth token is missing")

var ErrHTTP = errors.New("direct-HTTP: non-2xx response")

var ErrModelNotFound = errors.New("direct-HTTP: model not found")

// RateLimitError is returned when the API transport receives HTTP 429.
// Transient per-minute limits and plan-usage exhaustion both arrive as 429
// and are deliberately not distinguished (spec: oneshot-fallback-targets).
type RateLimitError struct {
	Status int    // always 429
	Body   string // response body, snippet-capped
}

// Error reproduces the exact text the plain ErrHTTP path produced before
// (status code plus net/http status line), so existing log-line matching
// keeps working.
func (e *RateLimitError) Error() string {
	statusLine := fmt.Sprintf("%d %s", e.Status, http.StatusText(e.Status))
	return fmt.Sprintf("%s: %d %s: %s", ErrHTTP, e.Status, statusLine, e.Body)
}

// Unwrap keeps errors.Is(err, ErrHTTP) true for 429 responses.
func (e *RateLimitError) Unwrap() error { return ErrHTTP }

// httpError classifies a non-2xx response: 429 → *RateLimitError (chainable
// failover); anything else → the plain ErrHTTP error, exactly as before.
func httpError(statusCode int, respBody string) error {
	snippet := respBody
	if len(snippet) > 512 {
		snippet = snippet[:512]
	}
	if statusCode == http.StatusTooManyRequests {
		return &RateLimitError{Status: statusCode, Body: snippet}
	}
	statusLine := fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode))
	return fmt.Errorf("%w: %d %s: %s", ErrHTTP, statusCode, statusLine, snippet)
}
