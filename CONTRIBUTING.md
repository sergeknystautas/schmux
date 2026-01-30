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

## Pre-Commit Requirements

Before committing changes, you MUST run:

1. **Run unit tests**: `go test ./...`
2. **Run E2E tests**: `docker build -f Dockerfile.e2e -t schmux-e2e . && docker run --rm schmux-e2e`
3. **Format code**: `go fmt ./...`

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
