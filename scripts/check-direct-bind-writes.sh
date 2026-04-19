#!/usr/bin/env bash
# check-direct-bind-writes.sh — scan for direct
# cfg.Network.BindAddress = "0.0.0.0" writes outside _test.go files.
# Each such site must be gated by !vendorlocked OR by another tag that
# vendorlocked builds always set (nogithub, notunnel, nodashboardsx).
# See docs/security.md "Vendor-locked builds".

set -eu

# Find candidate files (non-test .go files) with direct writes.
hits=$(git grep -lE 'Network\.BindAddress *= *"0\.0\.0\.0"' -- '*.go' ':!*_test.go' || true)

if [ -z "$hits" ]; then
    exit 0
fi

# Filter out files whose build constraint header excludes vendorlocked
# builds via !nogithub, !nodashboardsx, !notunnel, or !vendorlocked.
unguarded=""
while IFS= read -r file; do
    [ -z "$file" ] && continue
    # Inspect the build constraint lines (top of file, before package decl).
    # A file is considered guarded if any //go:build line in the header
    # contains one of the recognized exclusion tags.
    header=$(awk '
        /^package / { exit }
        /^\/\/go:build/ { print }
    ' "$file")
    if echo "$header" | grep -qE '!(nogithub|nodashboardsx|notunnel|vendorlocked)\b'; then
        continue
    fi
    unguarded="$unguarded$file"$'\n'
done <<<"$hits"

unguarded=$(printf '%s' "$unguarded" | sed '/^$/d')

if [ -z "$unguarded" ]; then
    exit 0
fi

echo "Found direct cfg.Network.BindAddress = \"0.0.0.0\" writes outside test files:"
while IFS= read -r file; do
    [ -z "$file" ] && continue
    git grep -nE 'Network\.BindAddress *= *"0\.0\.0\.0"' -- "$file"
done <<<"$unguarded"
echo
echo "Each site must be gated by !vendorlocked OR by another tag that"
echo "vendorlocked builds always set (nogithub, notunnel, nodashboardsx)."
echo "See docs/security.md 'Vendor-locked builds'."
exit 1
