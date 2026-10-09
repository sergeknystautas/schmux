#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

FAILED=0
SECTION=0

TOOLS_ROOT="$PWD/.cache/badcode"
TOOLS_BIN="$TOOLS_ROOT/bin"
DEADCODE="$TOOLS_BIN/deadcode"
STATICCHECK="$TOOLS_BIN/staticcheck"
GOVULNCHECK="$TOOLS_BIN/govulncheck"
# GOCACHE is inherited, not overridden, so analysis reuses the same build cache
# as regular builds (fence's golang preset cache, or Go's default).
export GOFLAGS=-mod=vendor
export GOTOOLCHAIN=local
export GOPROXY=off
export GOTELEMETRY=off

section() {
    SECTION=$((SECTION + 1))
    echo ""
    echo "=== [$SECTION] $1 ==="
}

# --- Local prerequisites ---

PREFLIGHT_FAILED=0

missing_local() {
    echo "FAIL: $1" >&2
    PREFLIGHT_FAILED=1
}

require_tool() {
    local name="$1" module="$2" version="$3"
    local binary="$TOOLS_BIN/$name"
    local expected_go active_go actual_version binary_go

    if [ ! -x "$binary" ]; then
        missing_local "missing local $name $version"
        return
    fi
    if ! grep -q "^$name $module $version$" "$TOOLS_ROOT/manifest" 2>/dev/null; then
        missing_local "$name is not pinned to $version"
        return
    fi

    expected_go=$(awk '$1 == "go" { print $2; exit }' "$TOOLS_ROOT/manifest")
    active_go=$(go env GOVERSION)
    if [ "$expected_go" != "$active_go" ]; then
        missing_local "$name was built with $expected_go, active Go is $active_go"
        return
    fi

    actual_version=$(go version -m "$binary" \
        | awk -v module="$module" '$1 == "mod" && $2 == module { print $3; exit }')
    if [ "$actual_version" != "$version" ]; then
        missing_local "$name is $actual_version, expected $version"
        return
    fi

    binary_go=$(go version "$binary" | awk '{ print $2 }')
    if [ "$binary_go" != "$active_go" ]; then
        missing_local "$name was built with $binary_go, active Go is $active_go"
    fi
}

require_tool deadcode golang.org/x/tools v0.50.0
require_tool staticcheck honnef.co/go/tools v0.8.1
require_tool govulncheck golang.org/x/vuln v1.8.0

for command_name in go npm; do
    command -v "$command_name" &>/dev/null || missing_local "missing command: $command_name"
done
[ -f vendor/modules.txt ] || missing_local "missing local vendor/modules.txt"
# Stamps are written by bootstrap-badcode.sh; a mismatch means go.mod/go.sum or
# a package lock changed since vendor/ and node_modules were provisioned.
if ! git hash-object go.mod go.sum | cmp -s - "$TOOLS_ROOT/go.stamp"; then
    missing_local "vendor/ is stale: go.mod or go.sum changed since bootstrap"
fi
[ -x assets/dashboard/node_modules/.bin/knip ] || missing_local "missing local knip"
NPM_HASHES=""
for ts_dir in assets/dashboard tools/test-runner tools/dev-runner test/scenarios/generated; do
    if [ ! -x "$ts_dir/node_modules/.bin/tsc" ]; then
        missing_local "missing local tsc in $ts_dir"
    fi
    NPM_HASHES+="$ts_dir $(git hash-object "$ts_dir/package.json" "$ts_dir/package-lock.json")"$'\n'
done
if ! printf '%s' "$NPM_HASHES" | cmp -s - "$TOOLS_ROOT/npm.stamp"; then
    missing_local "node_modules is stale: a package.json or package-lock.json changed since bootstrap"
fi

if [ "$PREFLIGHT_FAILED" -ne 0 ]; then
    echo ""
    echo "Run ./scripts/bootstrap-badcode.sh to provision badcode." >&2
    exit 127
fi

# --- Go: deadcode (unreachable functions) ---

section "Go unreachable functions (deadcode)"
# -tags e2e: include E2E test callers so helpers aren't flagged
# Filter out: e2e test infrastructure, test utility mocks, ForTest helpers.
# internal/benchutil: called only from bench-tagged _test.go files, which
# deadcode cannot see without -test (they were wrongly deleted as dead on
# 2026-04-10 and restored on 2026-09-11).
DEADCODE_OUT=$("$DEADCODE" -tags e2e ./... 2>&1 \
    | grep -v "internal/e2e/" \
    | grep -v "testutil.go" \
    | grep -v "ForTest" \
    | grep -v "internal/benchutil/" \
) || true
if [ -n "$DEADCODE_OUT" ]; then
    echo "$DEADCODE_OUT"
    FAILED=1
else
    echo "PASS"
fi

# --- Go: staticcheck (bugs, performance, simplifications, unused) ---

# Failure signature after a Go upgrade: "export data version N is greater than
# maximum supported version M" — the tool binary was built against an older
# golang.org/x/tools than the toolchain's export format. Fix: delete the stale
# binary (rm .cache/badcode/bin/staticcheck) and re-run
# scripts/bootstrap-badcode.sh; bootstrap rebuilds with an x/tools override.
section "Go static analysis (staticcheck)"
STATIC_OUT=$("$STATICCHECK" ./... 2>&1) || true
if [ -n "$STATIC_OUT" ]; then
    echo "$STATIC_OUT"
    FAILED=1
else
    echo "PASS"
fi

# --- Go: govulncheck (known vulnerabilities) ---

# Failure signature: findings say "Standard library", "Found in: ...@goX.Y.Z",
# "Fixed in: ...@goX.Y.Z+1". The code is not vulnerable — the local Go
# toolchain is a patch release behind, and GOTOOLCHAIN=local (above) pins it.
# Fix: `brew upgrade go` then `scripts/bootstrap-badcode.sh` (tool binaries
# are version-checked against the active Go, so they must be rebuilt).
# Do NOT "fix" this by editing code, vendoring, or silencing the check.
#
# In a fenced schmux session: go.dev, dl.google.com, and storage.googleapis.com
# are NOT allowlisted, so `brew upgrade` / toolchain downloads fail with 403 —
# run the upgrade in an unfenced terminal. A 403 from curl/go here is the
# fence allowlist (.schmux/config.json), not a broken proxy or network.
section "Go dependency vulnerabilities (govulncheck)"
VULN_RC=0
VULN_OUT=$("$GOVULNCHECK" ./... 2>&1) || VULN_RC=$?
if echo "$VULN_OUT" | grep -q "^Vulnerability"; then
    echo "$VULN_OUT"
    echo "FAIL: vulnerabilities found"
    FAILED=1
elif [ "$VULN_RC" -ne 0 ]; then
    # govulncheck itself failed (e.g. could not fetch https://vuln.go.dev) —
    # a clean scan we couldn't run is not a PASS
    echo "$VULN_OUT"
    echo "FAIL: govulncheck exited $VULN_RC without findings"
    FAILED=1
else
    echo "PASS"
fi

# --- TypeScript: knip (unused files, exports, deps) ---

section "TypeScript unused code (knip)"
KNIP_OUT=$(cd assets/dashboard && ./node_modules/.bin/knip --no-exit-code 2>&1 | grep -v "^Configuration hints" | grep -v "knip.json") || true
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
        TSC_OUT=$(cd "$ts_dir" && ./node_modules/.bin/tsc --noEmit 2>&1) || true
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

# --- Vendor-locked guards (custom shell scripts) ---

section "Vendor-locked: direct bind-address writes (check-direct-bind-writes.sh)"
BIND_OUT=$(./scripts/check-direct-bind-writes.sh 2>&1) || {
    echo "$BIND_OUT"
    FAILED=1
}
if [ -z "${BIND_OUT:-}" ]; then
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
