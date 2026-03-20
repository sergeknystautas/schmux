#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RUNNER_DIR="$SCRIPT_DIR/tools/dev-runner"

# ── Setup (first-run) ────────────────────────────────────────────────────

# Ensure Go module cache is populated so builds/tests work without network
if ! (cd "$SCRIPT_DIR" && go build ./cmd/schmux 2>/dev/null); then
  echo "Downloading Go modules..."
  (cd "$SCRIPT_DIR" && go mod download)
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
