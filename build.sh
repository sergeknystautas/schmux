#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"

echo ""
echo "----- Dashboard ----------------------------------------------------------"
go run ./cmd/build-dashboard

echo ""
echo "----- Backend ------------------------------------------------------------"
go build -o schmux ./cmd/schmux

echo ""
echo "Build complete: ./schmux"
