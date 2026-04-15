#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

FAILED=0
SECTION=0

# Ensure GOPATH/bin is on PATH for freshly-installed tools
export PATH="${GOPATH:-$HOME/go}/bin:$PATH"

section() {
    SECTION=$((SECTION + 1))
    echo ""
    echo "=== [$SECTION] $1 ==="
}

# --- Auto-install prerequisites ---

if ! command -v deadcode &>/dev/null; then
    echo "Installing deadcode..."
    go install golang.org/x/tools/cmd/deadcode@latest
fi

if ! command -v staticcheck &>/dev/null; then
    echo "Installing staticcheck..."
    go install honnef.co/go/tools/cmd/staticcheck@latest
fi

if ! command -v govulncheck &>/dev/null; then
    echo "Installing govulncheck..."
    go install golang.org/x/vuln/cmd/govulncheck@latest
fi

if ! (cd assets/dashboard && npm list knip >/dev/null 2>&1); then
    echo "Installing knip..."
    (cd assets/dashboard && npm install --save-dev knip --silent)
fi

for ts_dir in tools/test-runner tools/dev-runner test/scenarios/generated; do
    if [ -f "$ts_dir/package.json" ] && [ ! -d "$ts_dir/node_modules" ]; then
        echo "Installing $ts_dir dependencies..."
        (cd "$ts_dir" && npm install --silent)
    fi
done

# --- Go: deadcode (unreachable functions) ---

section "Go unreachable functions (deadcode)"
# -tags e2e: include E2E test callers so helpers aren't flagged
# Filter out: e2e test infrastructure, test utility mocks, ForTest helpers
DEADCODE_OUT=$(deadcode -tags e2e ./... 2>&1 \
    | grep -v "internal/e2e/" \
    | grep -v "testutil.go" \
    | grep -v "ForTest" \
) || true
if [ -n "$DEADCODE_OUT" ]; then
    echo "$DEADCODE_OUT"
    FAILED=1
else
    echo "PASS"
fi

# --- Go: staticcheck (bugs, performance, simplifications, unused) ---

section "Go static analysis (staticcheck)"
STATIC_OUT=$(staticcheck ./... 2>&1) || true
if [ -n "$STATIC_OUT" ]; then
    echo "$STATIC_OUT"
    FAILED=1
else
    echo "PASS"
fi

# --- Go: govulncheck (known vulnerabilities — informational) ---

section "Go dependency vulnerabilities (govulncheck)"
VULN_OUT=$(govulncheck ./... 2>&1) || true
if echo "$VULN_OUT" | grep -q "^Vulnerability"; then
    echo "$VULN_OUT"
    FAILED=1
else
    echo "PASS"
fi

# --- TypeScript: knip (unused files, exports, deps) ---

section "TypeScript unused code (knip)"
KNIP_OUT=$(cd assets/dashboard && npx knip --no-exit-code 2>&1 | grep -v "^Configuration hints" | grep -v "knip.json") || true
if [ -n "$KNIP_OUT" ]; then
    echo "$KNIP_OUT"
    FAILED=1
else
    echo "PASS"
fi

# --- npm: known vulnerabilities in dependencies ---

section "npm dependency vulnerabilities (npm audit)"
NPM_FAILED=0
for pkg_dir in assets/dashboard tools/test-runner tools/dev-runner; do
    if [ -f "$pkg_dir/package.json" ]; then
        AUDIT_OUT=$(cd "$pkg_dir" && npm audit --omit=dev 2>&1) || true
        if echo "$AUDIT_OUT" | grep -q "found 0 vulnerabilities"; then
            echo "$pkg_dir: ok"
        else
            echo "--- $pkg_dir ---"
            echo "$AUDIT_OUT"
            NPM_FAILED=1
        fi
    fi
done
if [ $NPM_FAILED -ne 0 ]; then
    FAILED=1
else
    echo "PASS"
fi

# --- TypeScript: strict type checking ---

section "TypeScript type errors (tsc)"
TSC_FAILED=0
for ts_dir in assets/dashboard tools/test-runner tools/dev-runner test/scenarios/generated; do
    if [ -f "$ts_dir/tsconfig.json" ]; then
        TSC_OUT=$(cd "$ts_dir" && npx tsc --noEmit 2>&1) || true
        if [ -n "$TSC_OUT" ]; then
            echo "--- $ts_dir ---"
            echo "$TSC_OUT"
            TSC_FAILED=1
        else
            echo "$ts_dir: ok"
        fi
    fi
done
if [ $TSC_FAILED -ne 0 ]; then
    FAILED=1
else
    echo "PASS"
fi

# --- Summary ---

echo ""
echo "================================"
if [ $FAILED -ne 0 ]; then
    echo "FAIL: Issues detected. Fix them before committing."
    exit 1
else
    echo "PASS: No issues found."
    exit 0
fi
