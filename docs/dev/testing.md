# Testing Guide

Testing conventions and running tests in schmux.

---

## Running Tests

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

## Test Conventions

### Framework
Standard Go `testing` package with `*_test.go` files and `TestXxx` naming.

### Table-Driven Tests
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

### Test Data
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

## Integration Testing

For manual verification of the full system:

1. Start the daemon: `./schmux daemon-run`
2. Spawn sessions via CLI or dashboard
3. Verify:
   - Sessions appear in dashboard
   - Terminal output streams correctly
   - Workspace git status is accurate
   - Disposal works as expected

---

## Adding Tests

When adding new functionality:

1. Add unit tests in the same package
2. For parsing/validation, use table-driven tests
3. For complex operations, add multiple test cases (happy path, errors, edge cases)
4. Run `go test ./...` before committing

---

## See Also

- [Architecture](architecture.md) — Package structure
- [Contributing Guide](README.md) — Development workflow (in this directory)
