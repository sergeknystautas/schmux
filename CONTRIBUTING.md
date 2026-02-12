# Contributing to schmux

Thanks for your interest in contributing! This document provides quick links to detailed guides.

## Quick Start

1. Fork and clone the repository
2. Read [Architecture](docs/dev/architecture.md) for an overview
3. Follow the [Development Guide](docs/dev/README.md) for setup and workflow
4. Write tests (see [Testing Guide](docs/dev/testing.md))
5. Run `go test ./...` before pushing

## Important: Building the Dashboard

**NEVER run `npm install`, `npm run build`, or `vite build` directly.**

The React dashboard MUST be built via:

```bash
go run ./cmd/build-dashboard
```

This Go wrapper:

- Installs npm deps correctly
- Runs vite build with proper environment
- Outputs to `assets/dashboard/dist/` which gets embedded in the Go binary

See [React Architecture](docs/dev/react.md) for more details.

## Development with Hot-Reload

For active development, use the hot-reload script instead of manual builds:

```bash
./dev.sh
```

This starts both servers with automatic rebuilding:

| Component      | Tool                         | What happens on save             |
| -------------- | ---------------------------- | -------------------------------- |
| Go backend     | `dev.sh` loop + `--dev-mode` | Rebuilds and restarts on exit 42 |
| React frontend | Vite HMR                     | Instant browser update (<100ms)  |

**First run** will install npm dependencies if missing.

**Output** is prefixed with `[backend]` and `[frontend]` for clarity.

**Access** the dashboard at http://localhost:7337 (same as production).

**Stop** with `Ctrl+C` - both servers shut down cleanly.

### How it works

The Go daemon runs with `--dev-mode` flag, which reverse-proxies non-API requests to the Vite dev server (port 5173). This gives you:

- React HMR without page refresh
- Working API endpoints and WebSockets
- Same URL as production

### When to use what

| Task                 | Command                                                 |
| -------------------- | ------------------------------------------------------- |
| Active development   | `./dev.sh`                                              |
| One-shot build + run | `./run.sh`                                              |
| Production build     | `go run ./cmd/build-dashboard && go build ./cmd/schmux` |

## Pre-Commit Requirements

Before committing changes, you MUST run:

1. **Run all tests**: `./test.sh --all`
2. **Format code**: `go fmt ./...`

## Testing

The `test.sh` script is the primary way to run tests:

```bash
./test.sh              # Run unit tests (default)
./test.sh --all        # Run both unit and E2E tests
./test.sh --race       # Run with race detector
./test.sh --coverage   # Run with coverage report
./test.sh --verbose    # Run with verbose output
./test.sh --quick      # Fast mode (no race detector)
./test.sh --e2e        # Run E2E tests only
./test.sh --help       # Show all options
```

### E2E Tests Require Docker

E2E tests run in a Docker container to ensure a consistent environment:

```bash
# Install Docker first (if not already installed)
# macOS: brew install --cask docker
# Linux: https://docs.docker.com/engine/install/

# Then run E2E tests
./test.sh --e2e
```

The script automatically:

1. Builds a Docker image from `Dockerfile.e2e`
2. Runs the E2E test suite inside the container
3. Reports results

### When to Run What

| Situation                 | Command            | Time    |
| ------------------------- | ------------------ | ------- |
| Quick check while coding  | `go test ./...`    | ~10s    |
| Before committing         | `./test.sh --all`  | ~2-3min |
| Debugging race conditions | `./test.sh --race` | ~30s    |
| CI/PR validation          | `./test.sh --all`  | ~2-3min |

**Tip:** During active development, run unit tests frequently (`go test ./...`) and let CI handle E2E tests on PRs. Run `./test.sh --all` before pushing.

## Documentation

- [Project Philosophy](docs/PHILOSOPHY.md) - Product principles and design goals
- [API Reference](docs/api.md) - HTTP/WebSocket API contract
- [React Architecture](docs/dev/react.md) - Frontend patterns and conventions
- [CLI Reference](docs/cli.md) - Command-line documentation
- [Web Dashboard](docs/web.md) - Dashboard UX and design system

## Community

- **Issues**: Bug reports and feature requests at [github.com/sergeknystautas/schmux/issues](https://github.com/sergeknystautas/schmux/issues)
- **Discussions**: Questions and general discussion at [github.com/sergeknystautas/schmux/discussions](https://github.com/sergeknystautas/schmux/discussions)

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.
