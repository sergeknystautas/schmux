#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RUNNER_DIR="$SCRIPT_DIR/tools/test-runner"

# Auto-install deps if needed
if [ ! -d "$RUNNER_DIR/node_modules" ]; then
  echo "Installing test runner dependencies..."
  (cd "$RUNNER_DIR" && npm install --silent)
fi

# Delegate to TypeScript test runner
exec npx --prefix "$RUNNER_DIR" tsx "$RUNNER_DIR/src/main.ts" "$@"
