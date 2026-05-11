#!/bin/bash
set -euo pipefail

if [ "${SCHMUX_INSTALL_TEST:-}" != "1" ]; then
    echo "Error: verify-install.sh must run inside a container." >&2
    echo "Use ./release/test-install.sh instead." >&2
    exit 1
fi

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

pass() { echo -e "${GREEN}PASS: $1${NC}"; }
fail() { echo -e "${RED}FAIL: $1${NC}"; exit 1; }

echo "=== Step 1: Run installer from GitHub ==="
INSTALL_OUTPUT=$(curl -fsSL https://raw.githubusercontent.com/sergeknystautas/schmux/main/install.sh | bash 2>&1) && INSTALL_OK=true || INSTALL_OK=false
echo "$INSTALL_OUTPUT"

if [ "$INSTALL_OK" = false ]; then
    echo ""
    echo "=== Diagnosing installer failure ==="
    RELEASE_JSON=$(curl -fsSL "https://api.github.com/repos/sergeknystautas/schmux/releases/latest" 2>/dev/null) || true
    if [ -n "$RELEASE_JSON" ]; then
        TAG=$(echo "$RELEASE_JSON" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"\([^"]*\)".*/\1/')
        ASSET_COUNT=$(echo "$RELEASE_JSON" | grep -c '"browser_download_url"' || true)
        echo "Latest release: ${TAG:-unknown}"
        echo "Downloadable assets: ${ASSET_COUNT}"
        if [ "$ASSET_COUNT" -eq 0 ] 2>/dev/null; then
            echo "DIAGNOSIS: Release ${TAG} has no assets. The release workflow likely failed before uploading binaries."
        else
            ASSET_NAMES=$(echo "$RELEASE_JSON" | grep '"name"' | sed 's/.*"name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/  - \1/' | grep -v "^  - $" | head -10)
            echo "Assets found:"
            echo "$ASSET_NAMES"
        fi
    fi
    fail "installer exited non-zero"
fi
pass "installer completed"

echo ""
echo "=== Step 2: Verify schmux --version ==="
VERSION_OUTPUT=$(schmux --version 2>&1) || fail "schmux --version exited non-zero"
echo "$VERSION_OUTPUT"
echo "$VERSION_OUTPUT" | grep -qE 'v?[0-9]+\.[0-9]+\.[0-9]+' \
    || fail "version output does not contain a version string"
pass "schmux --version"

echo ""
echo "=== Step 3: Verify dashboard assets ==="
[ -f "$HOME/.schmux/dashboard/index.html" ] \
    || fail "~/.schmux/dashboard/index.html not found"
pass "dashboard assets installed"

echo ""
echo "=== Step 4: Verify daemon starts ==="
schmux start || fail "schmux start exited non-zero"
pass "schmux start"

echo ""
echo "=== Step 5: Verify daemon status ==="
schmux status || fail "schmux status exited non-zero"
pass "schmux status"

echo ""
echo "=== Step 6: Stop daemon ==="
schmux stop || fail "schmux stop exited non-zero"
pass "schmux stop"

echo ""
echo -e "${GREEN}All checks passed.${NC}"
