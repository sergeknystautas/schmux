#!/bin/bash
set -e

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
gofmt -w $(find . -name '*.go' -not -path './vendor/*')

echo "Formatting TS/JS/CSS/MD/JSON files..."
(cd assets/dashboard && npx prettier --write "../.." --ignore-path "../../.prettierignore")

echo "Done!"
