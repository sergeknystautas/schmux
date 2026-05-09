#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "Building install test image..."
docker build -f "$REPO_ROOT/release/Dockerfile.install-test" -t schmux-install-test "$REPO_ROOT"

echo "Running install test..."
docker run --rm schmux-install-test
