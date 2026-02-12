#!/bin/bash
# Build and run schmux daemon in development mode
# Usage: ./run.sh

set -euo pipefail

# Navigate to project root
cd "$(dirname "$0")"

echo ""
echo "----- Dashboard ----------------------------------------------------------"
go run ./cmd/build-dashboard &&

echo ""
echo "----- Backend ----------------------------------------------------------"
go build -o schmux ./cmd/schmux &&

echo "Stopping any existing daemon..."
./schmux stop &&

echo "Starting fresh daemon..."
./schmux start

echo ""
echo "schmux daemon is running."
echo ""

echo "----- Logs ----------------------------------------------------------"
echo ""
tail -f ~/.schmux/daemon-startup.log
