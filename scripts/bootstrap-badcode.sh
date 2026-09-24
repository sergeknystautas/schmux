#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

ROOT="$PWD"
TOOLS_ROOT="$ROOT/.cache/badcode"
TOOLS_BIN="$TOOLS_ROOT/bin"
STAGE=""
NPM_DIRS=(
    assets/dashboard
    tools/test-runner
    tools/dev-runner
    test/scenarios/generated
)

usage() {
    echo "Usage: scripts/bootstrap-badcode.sh [--clean]" >&2
}

clean() {
    rm -rf -- "$TOOLS_ROOT" "$ROOT/vendor"
    for npm_dir in "${NPM_DIRS[@]}"; do
        rm -rf -- "$npm_dir/node_modules"
    done
}

cleanup_stage() {
    if [ -n "$STAGE" ] && [ -d "$STAGE/go/pkg/mod" ]; then
        GOPATH="$STAGE/go" \
            GOMODCACHE="$STAGE/go/pkg/mod" \
            GOFLAGS= \
            GOTOOLCHAIN=local \
            GOTELEMETRY=off \
            go clean -modcache
    fi
    rm -rf -- "$STAGE"
}
trap cleanup_stage EXIT

if [ "${1:-}" = "--clean" ]; then
    clean
elif [ $# -gt 0 ]; then
    usage
    exit 2
fi

mkdir -p "$TOOLS_BIN"
STAGE=$(mktemp -d "${TMPDIR:-/tmp}/schmux-badcode.XXXXXX")

tool_is_current() {
    local name="$1" module="$2" version="$3"
    local binary="$TOOLS_BIN/$name"
    local expected_go active_go actual_version binary_go

    [ -x "$binary" ] || return 1
    [ -f "$TOOLS_ROOT/manifest" ] || return 1
    grep -q "^$name $module $version$" "$TOOLS_ROOT/manifest" || return 1

    expected_go=$(awk '$1 == "go" { print $2; exit }' "$TOOLS_ROOT/manifest")
    active_go=$(go env GOVERSION)
    [ "$expected_go" = "$active_go" ] || return 1

    actual_version=$(go version -m "$binary" \
        | awk -v module="$module" '$1 == "mod" && $2 == module { print $3; exit }')
    [ "$actual_version" = "$version" ] || return 1

    binary_go=$(go version "$binary" | awk '{ print $2 }')
    [ "$binary_go" = "$active_go" ] || return 1
}

install_tool() {
    local name="$1" module="$2" import_path="$3" version="$4"
    if tool_is_current "$name" "$module" "$version"; then
        echo "$name $version: current"
        return
    fi

    echo "Installing $name $version..."
    GOPATH="$STAGE/go" \
        GOMODCACHE="$STAGE/go/pkg/mod" \
        GOBIN="$STAGE/bin" \
        GOFLAGS= \
        GOTOOLCHAIN=local \
        GOTELEMETRY=off \
        go install "$import_path@$version"
    mv -f -- "$STAGE/bin/$name" "$TOOLS_BIN/$name"
}

install_tool deadcode golang.org/x/tools golang.org/x/tools/cmd/deadcode v0.50.0
install_tool staticcheck honnef.co/go/tools honnef.co/go/tools/cmd/staticcheck v0.8.1
install_tool govulncheck golang.org/x/vuln golang.org/x/vuln/cmd/govulncheck v1.8.0

{
    echo "go $(go env GOVERSION)"
    echo "deadcode golang.org/x/tools v0.50.0"
    echo "staticcheck honnef.co/go/tools v0.8.1"
    echo "govulncheck golang.org/x/vuln v1.8.0"
} > "$TOOLS_ROOT/manifest.tmp"
mv -f -- "$TOOLS_ROOT/manifest.tmp" "$TOOLS_ROOT/manifest"

go_hash=$(git hash-object go.mod go.sum)
if [ ! -f vendor/modules.txt ] || ! printf '%s\n' "$go_hash" | cmp -s - "$TOOLS_ROOT/go.stamp"; then
    echo "Creating local Go vendor directory..."
    GOFLAGS= GOTOOLCHAIN=local GOTELEMETRY=off go mod vendor
    printf '%s\n' "$go_hash" > "$TOOLS_ROOT/go.stamp.tmp"
    mv -f -- "$TOOLS_ROOT/go.stamp.tmp" "$TOOLS_ROOT/go.stamp"
else
    echo "Go vendor directory: current"
fi

npm_hashes=$(for npm_dir in "${NPM_DIRS[@]}"; do
    printf '%s ' "$npm_dir"
    git hash-object "$npm_dir/package.json" "$npm_dir/package-lock.json"
done)
npm_current=1
printf '%s\n' "$npm_hashes" | cmp -s - "$TOOLS_ROOT/npm.stamp" || npm_current=0
for npm_dir in "${NPM_DIRS[@]}"; do
    [ -x "$npm_dir/node_modules/.bin/tsc" ] || npm_current=0
done
[ -x assets/dashboard/node_modules/.bin/knip ] || npm_current=0

if [ "$npm_current" -eq 1 ]; then
    echo "TypeScript dependencies: current"
else
    for npm_dir in "${NPM_DIRS[@]}"; do
        echo "Installing $npm_dir dependencies..."
        (cd "$npm_dir" && npm ci --silent --no-audit --no-fund)
    done
    printf '%s\n' "$npm_hashes" > "$TOOLS_ROOT/npm.stamp.tmp"
    mv -f -- "$TOOLS_ROOT/npm.stamp.tmp" "$TOOLS_ROOT/npm.stamp"
fi

echo "PASS: badcode prerequisites are local and ready."
