#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RUNNER_DIR="$SCRIPT_DIR/tools/test-runner"

# Ensure Go module cache is populated so builds/tests work without network
if ! (cd "$SCRIPT_DIR" && go build ./cmd/schmux 2>/dev/null); then
  echo "Downloading Go modules..."
  (cd "$SCRIPT_DIR" && go mod download)
fi

# Auto-install deps if needed
if [ ! -d "$RUNNER_DIR/node_modules" ]; then
  echo "Installing test runner dependencies..."
  (cd "$RUNNER_DIR" && npm install --silent)
fi

# Delegate to TypeScript test runner
exec npx --prefix "$RUNNER_DIR" tsx "$RUNNER_DIR/src/main.ts" "$@"
