#!/usr/bin/env bash
# verify.sh — prove scan.sh against the fixture corpus.
#
# Usage:
#   verify.sh           run all assertions (exit 0 = scanner proven)
#   verify.sh --update  regenerate expected/ from current scan output
#                      (run ONLY when patterns intentionally change, after
#                      confirming fixture contents still match the plan)
set -euo pipefail
cd "$(dirname "$0")"

normalize() { awk -F'\t' '{print $1"|"$2}' | LC_ALL=C sort; }

update=false
if [ "${1:-}" = "--update" ]; then
  update=true
fi

status=0
for group in prohibited excepted; do
  expected="fixtures/expected/$group.txt"
  actual="$(./scan.sh "fixtures/$group"/*.txt | normalize)"

  if $update; then
    printf '%s\n' "$actual" > "$expected"
    echo "updated $expected"
    continue
  fi

  if ! printf '%s\n' "$actual" | diff "$expected" -; then
    status=1
  fi

  # Every fixture must surface at least one candidate. An empty scan of an
  # excepted fixture would mean an exception passing silently.
  for f in "fixtures/$group"/*.txt; do
    if ! printf '%s\n' "$actual" | grep -qF "$f"; then
      echo "FAIL: no candidates surfaced for $f" >&2
      status=1
    fi
  done
done

exit $status
