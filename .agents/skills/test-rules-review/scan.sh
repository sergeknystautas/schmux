#!/usr/bin/env bash
# scan.sh — mechanical scan layer for the test-rules-review skill. Read-only.
#
# Modes:
#   scan.sh --net [--changed [BASE]]   print the scoped test-file list
#   scan.sh [--index] <file>...        scan the given files (working-tree content)
#   scan.sh [--index]                  scan a file list from stdin (index content with --index)
#
# Scan output: <check>\t<file>:<line>\t<matched text>, deduplicated, LC_ALL=C sorted.
# Exit code is 0 for scans; findings are data, not failure.
set -euo pipefail

NET_INCLUDE='_test\.go$|\.test\.[tj]sx?$|\.spec\.[tj]sx?$|(^|/)test\.sh$|(^|/)test/.*\.sh$|(^|/)scripts/determinism\.sh$|(^|/)test/scenarios/generated/[^/]+\.ts$'
NET_EXCLUDE='(^|/)(vendor|node_modules|dist)/|^\.agents/|^\.claude/'

# name|ERE. No \b (BSD grep): boundaries are (^|[^A-Za-z0-9_]) / ([^A-Za-z0-9_]|$).
CHECKS=(
  'fixed-delay|time\.Sleep\('
  'fixed-delay|(^|[^A-Za-z0-9_])(sleep|usleep)([^A-Za-z0-9_]|$)'
  'fixed-delay|setTimeout[[:space:]]*\('
  'fixed-delay|waitForTimeout[[:space:]]*\('
  'polling|setInterval[[:space:]]*\('
  'polling|(^|[^A-Za-z0-9_])poll(ing|ed)?([^A-Za-z0-9_]|$)'
  'polling|waitFor[A-Z]'
  'assertion-retry|(^|[^A-Za-z0-9_])retr(y|ies)([^A-Za-z0-9_]|$)'
  'assertion-retry|(^|[^A-Za-z0-9_])(attempts?|maxRetries|maxAttempts)([^A-Za-z0-9_]|$)'
  'assertion-retry|(^|[^A-Za-z0-9_])Eventually\('
  'perf-threshold|(^|[^A-Za-z0-9_])p(50|95|99)([^A-Za-z0-9_]|$)'
  'perf-threshold|(^|[^A-Za-z0-9_])(latency|median)([^A-Za-z0-9_]|$)'
  'perf-threshold|(Date|performance)\.now\(\)'
  'fixed-port|:7337([^0-9]|$)'
  'fixed-port|(localhost|127\.0\.0\.1):[0-9]+'
  'fixed-port|net\.Listen\([^)]*,[[:space:]]*":[0-9]+'
  'ambient-state|os\.UserHomeDir\(\)|os\.Getenv\([[:space:]]*"HOME"'
  'ambient-state|~/\.schmux|\.gitconfig'
  'cleanup|os\.Setenv\('
  'cleanup|os\.Chdir\('
  'cleanup|os\.MkdirTemp\('
  'cleanup|exec\.Command(New)?\('
  'exception/deadline-timer|time\.After\('
  'exception/fake-clock|useFakeTimers|advanceTimersByTime|(fake|mock)[Cc]lock|clock\.Mock'
  'exception/framework-auto-wait|expect\.poll\(|findBy[A-Z]|(^|[^A-Za-z0-9_])waitFor\('
)

net_filter() {
  grep -E "$NET_INCLUDE" | grep -vE "$NET_EXCLUDE" | while IFS= read -r f; do
    if [ -f "$f" ]; then
      printf '%s\n' "$f"
    fi
  done
}

check_exists() {
  # $1 = file. In --index mode existence = present in the index.
  if [ "$mode_index" = 1 ]; then
    if git cat-file -e ":$1" 2>/dev/null; then
      return 0
    fi
  else
    if [ -f "$1" ]; then
      return 0
    fi
  fi
  echo "scan.sh: skipped (not found): $1" >&2
  return 1
}

scan_one() {
  # $1 = file. Prints one line per candidate.
  local file="$1" spec name re hits line ln txt
  for spec in "${CHECKS[@]}"; do
    name="${spec%%|*}"
    re="${spec#*|}"
    if [ "$mode_index" = 1 ]; then
      hits="$(git show ":$file" | grep -nE "$re" || true)"
    else
      hits="$(grep -nE "$re" "$file" || true)"
    fi
    if [ -z "$hits" ]; then
      continue
    fi
    while IFS= read -r line; do
      ln="${line%%:*}"
      txt="${line#*:}"
      printf '%s\t%s\t%s\n' "$name" "$file:$ln" "$txt"
    done <<< "$hits"
  done
}

run_scan() {
  local f
  if [ $# -gt 0 ]; then
    for f in "$@"; do
      if check_exists "$f"; then
        scan_one "$f"
      fi
    done
  else
    while IFS= read -r f; do
      if [ -z "$f" ]; then
        continue
      fi
      if check_exists "$f"; then
        scan_one "$f"
      fi
    done
  fi
}

mode_net=0
mode_changed=0
mode_index=0
while [ $# -gt 0 ]; do
  case "$1" in
    --net) mode_net=1 ;;
    --changed) mode_changed=1 ;;
    --index) mode_index=1 ;;
    *) break ;;
  esac
  shift
done

if [ "$mode_net" = 1 ]; then
  if [ "$mode_changed" = 1 ]; then
    base="${1:-}"
    if [ -z "$base" ]; then
      base="$(git merge-base HEAD main)"
    fi
    { git diff --name-only "$base"; git diff --name-only; git diff --name-only --cached; } \
      | sort -u | net_filter
  else
    git ls-files | net_filter
  fi
  exit 0
fi

run_scan "$@" | LC_ALL=C sort -u
