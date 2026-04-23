package directhttp

import "errors"

var ErrNotImplemented = errors.New("direct-HTTP transport not implemented for this target")

var ErrMissingToken = errors.New("direct-HTTP: required auth token is missing")

var ErrHTTP = errors.New("direct-HTTP: non-2xx response")

var ErrModelNotFound = errors.New("direct-HTTP: model not found")
