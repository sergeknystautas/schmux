#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"

echo ""
echo "----- Dashboard ----------------------------------------------------------"
go run ./cmd/build-dashboard

echo ""
echo "----- npm audit ----------------------------------------------------------"
cd assets/dashboard
audit_output=$(npm audit 2>&1) || true
if echo "$audit_output" | grep -q "found 0 vulnerabilities"; then
    echo "No vulnerabilities found."
else
    echo "$audit_output"
    echo ""
    echo "Fixing vulnerabilities..."
    npm audit fix
fi
cd ../..

echo ""
echo "----- Backend ------------------------------------------------------------"
go build -o schmux ./cmd/schmux

echo ""
echo "Build complete: ./schmux"
