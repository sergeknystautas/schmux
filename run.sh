#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"

echo ""
echo "----- Dashboard ----------------------------------------------------------"
go run ./cmd/build-dashboard

echo ""
echo "----- Backend ----------------------------------------------------------"
go build -o schmux ./cmd/schmux

echo "Stopping any existing daemon..."
./schmux stop 2>/dev/null || true

echo "Starting fresh daemon..."
./schmux start

echo ""
echo "schmux daemon is running."
echo "Open the dashboard URL shown above."
