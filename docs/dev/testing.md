# Testing Guide

Testing conventions and running tests in schmux.

---

## Running Unit Tests

```bash
# Run all tests
go test ./...

# Verbose output
go test -v ./...

# With coverage
go test -cover ./...

# Specific package
go test ./internal/tmux
```

---

## Unit Test Conventions

### Framework

Standard Go `testing` package with `*_test.go` files and `TestXxx` naming.

### Table-Driven Unit Tests

Prefer table-driven tests for parsing and state transitions:

```go
func TestParseStatus(t *testing.T) {
    tests := []struct {
        name   string
        input  string
        want   Status
    }{
        {"running", "running", StatusRunning},
        {"stopped", "stopped", StatusStopped},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := ParseStatus(tt.input)
            if got != tt.want {
                t.Errorf("ParseStatus() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### Unit Test Data

Test fixtures live in `testdata/` directories next to the code they test.

Example: `internal/tmux/testdata/` contains tmux session captures for testing terminal parsing.

---

## Package-Specific Notes

### `internal/tmux`

Tests use captured tmux output stored in `testdata/`. To update captures:

```bash
# In test directory
tmux new-session -d -s test-capture "your command"
tmux capture-pane -t test-capture -p > testdata/capture.txt
tmux kill-session -t test-capture
```

### `internal/dashboard`

Tests use a mock server. No external dependencies required.

### `internal/workspace`

Tests use temporary directories for workspace operations. Cleaned up automatically.

---

## End-to-End (E2E) Testing

E2E tests validate the full system: CLI → daemon → tmux → HTTP API.

### Running E2E Tests

**In Docker (recommended):**

```bash
# Build and run E2E tests in Docker
docker build -f Dockerfile.e2e -t schmux-e2e .
docker run --rm schmux-e2e

# Or with artifact capture on failure
docker run --rm -v $(pwd)/artifacts:/home/e2e/internal/e2e/testdata/failures schmux-e2e
```

**Locally (requires schmux binary in PATH):**

```bash
# Build schmux first
go build -o schmux ./cmd/schmux

# Run E2E tests
go test -v ./internal/e2e
```

### What E2E Tests Validate

- Daemon lifecycle (start/stop/health endpoint)
- Workspace creation from local git repos
- Session spawning with unique nicknames
- Naming consistency across CLI, tmux, and API
- Session disposal and cleanup

### E2E Test Isolation

E2E tests run in Docker containers. The container provides all isolation:

- Container's `~/.schmux/` is isolated from host
- Container's port 7337 is isolated
- Container's tmux server is isolated

For full details, see `docs/dev/e2e.md`.

---

## Adding Unit and E2E Tests

When adding new functionality:

1. Add unit tests in the same package
2. For parsing/validation, use table-driven tests
3. For complex operations, add multiple test cases (happy path, errors, edge cases)
4. Run both unit and e2e tests before committing

---

## See Also

- [Architecture](architecture.md) — Package structure
- [Contributing Guide](README.md) — Development workflow (in this directory)
