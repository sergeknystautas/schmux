#!/bin/bash
set -euo pipefail

report_result() {
    local rc=$?
    trap - EXIT
    if [ "$rc" -eq 0 ]; then
        echo "FORMAT_RESULT=PASS"
    else
        echo "FORMAT_RESULT=FAIL exit_code=$rc" >&2
    fi
    exit "$rc"
}
trap report_result EXIT

cd "$(dirname "$0")"

# Auto-install pre-commit hook if missing or outdated
install_hook() {
    HOOKS_DIR=$(git rev-parse --git-common-dir)/hooks
    HOOK_SRC="scripts/git-hooks/pre-commit"
    HOOK_DST="$HOOKS_DIR/pre-commit"

    if [ ! -f "$HOOK_DST" ] || ! cmp -s "$HOOK_SRC" "$HOOK_DST"; then
        cp "$HOOK_SRC" "$HOOK_DST"
        chmod +x "$HOOK_DST"
        echo "Pre-commit hook installed."
    fi
}

install_hook

# Ensure prettier is installed
if ! (cd assets/dashboard && npm list prettier >/dev/null 2>&1); then
    echo "Installing prettier..."
    (cd assets/dashboard && npm install --save-dev prettier)
fi

echo "Formatting Go files..."
find . -name '*.go' -not -path './vendor/*' -not -path './.cache/*' -print0 | xargs -0 gofmt -w

echo "Formatting TS/JS/CSS/MD/JSON files..."
(cd assets/dashboard && npx prettier --write "../.." --ignore-path "../../.prettierignore")

echo "Done!"
