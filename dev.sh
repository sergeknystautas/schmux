#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RUNNER_DIR="$SCRIPT_DIR/tools/dev-runner"

# ── Setup (first-run) ────────────────────────────────────────────────────

# Download all Go modules on first run (marker tracks go.sum content)
GOMOD_MARKER="$SCRIPT_DIR/.go-mod-cached"
if [ ! -f "$GOMOD_MARKER" ] || ! cmp -s "$SCRIPT_DIR/go.sum" "$GOMOD_MARKER"; then
  echo "Downloading Go modules..."
  (cd "$SCRIPT_DIR" && go mod download)
  cp "$SCRIPT_DIR/go.sum" "$GOMOD_MARKER"
fi

# Ensure React dashboard is built (embedded in the Go binary)
if [ ! -f "$SCRIPT_DIR/assets/dashboard/dist/index.html" ]; then
  echo "Building dashboard..."
  (cd "$SCRIPT_DIR" && go run ./cmd/build-dashboard)
fi

# Auto-install dev runner deps if needed
if [ ! -d "$RUNNER_DIR/node_modules" ]; then
  echo "Installing dev runner dependencies..."
  (cd "$RUNNER_DIR" && npm install --silent)
fi

# ── Build & Run ───────────────────────────────────────────────────────────

echo "Building schmux CLI..."
(cd "$SCRIPT_DIR" && go build ./cmd/schmux)

# Delegate to TypeScript dev runner
exec npx --prefix "$RUNNER_DIR" tsx "$RUNNER_DIR/src/main.tsx" "$@"
