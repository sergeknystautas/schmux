#!/bin/bash
# run-workspace.sh - Build and run schmux daemon in development mode
# Usage: ./scripts/run-workspace.sh

set -euo pipefail

# Navigate to project root (assumes script is in scripts/ subdir)
cd "$(dirname "$0")/.."

echo "Building dashboard..."
go run ./cmd/build-dashboard &&

echo "Building backend..."
go build -o schmux ./cmd/schmux &&

echo "Stopping any existing daemon..."
./schmux stop &&

echo "Starting daemon..."
./schmux start

echo ""
echo "Daemon is running. Logs:"
echo "--------------------"
tail -f ~/.schmux/daemon-startup.log
