#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RUNNER_DIR="$SCRIPT_DIR/tools/test-runner"

# Download all Go modules on first run (marker tracks go.sum content)
GOMOD_MARKER="$SCRIPT_DIR/.go-mod-cached"
if [ ! -f "$GOMOD_MARKER" ] || ! cmp -s "$SCRIPT_DIR/go.sum" "$GOMOD_MARKER"; then
  echo "Downloading Go modules..."
  (cd "$SCRIPT_DIR" && go mod download)
  cp "$SCRIPT_DIR/go.sum" "$GOMOD_MARKER"
fi

# Auto-install deps if needed
if [ ! -d "$RUNNER_DIR/node_modules" ]; then
  echo "Installing test runner dependencies..."
  (cd "$RUNNER_DIR" && npm install --silent)
fi

# Delegate to TypeScript test runner
exec npx --prefix "$RUNNER_DIR" tsx "$RUNNER_DIR/src/main.ts" "$@"
