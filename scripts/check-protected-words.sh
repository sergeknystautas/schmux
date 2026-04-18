#!/usr/bin/env bash
#
# check-protected-words.sh — scan staged additions for protected words.
#
# "Protected" means: terms that may legitimately exist on the local machine
# (e.g., vendor-internal names, internal hostnames, internal tooling) but
# should not land in this open-source repository's commits.
#
# Used by .claude/commands/commit.md (Step 2) to block such commits.
#
# Two patterns files (both optional, both outside the repo):
#
#   ~/.schmux/protected-words      — case-INSENSITIVE patterns. Use for terms
#                                    where case doesn't matter (tool names,
#                                    hostnames, table prefixes, etc.).
#                                    Override path with SCHMUX_PROTECTED_WORDS.
#
#   ~/.schmux/protected-words-cs   — case-SENSITIVE patterns. Use for proper
#                                    nouns (company names, product names) where
#                                    a case-insensitive match would generate
#                                    false positives in generic English text.
#                                    Override path with SCHMUX_PROTECTED_WORDS_CS.
#
# Format of each file: one ERE regex per line. Blank lines and lines starting
# with '#' are comments.
#
# Behavior:
#   - Both files missing  → print "skipped" message, exit 0.
#   - Either file present, no matches → exit 0.
#   - Any matches found   → print file:line:content for each match, exit 1.
#
# Only ADDED lines in the staged diff are checked (lines starting with '+',
# excluding diff headers like '+++').
#

set -eu

CI_PATTERNS_FILE="${SCHMUX_PROTECTED_WORDS:-$HOME/.schmux/protected-words}"
CS_PATTERNS_FILE="${SCHMUX_PROTECTED_WORDS_CS:-$HOME/.schmux/protected-words-cs}"

if [ ! -f "$CI_PATTERNS_FILE" ] && [ ! -f "$CS_PATTERNS_FILE" ]; then
  echo "no protected-words files configured (looked for $CI_PATTERNS_FILE and $CS_PATTERNS_FILE) — check skipped"
  exit 0
fi

# Build effective (filtered) pattern files: strip blank lines and #-comments.
# grep -f treats every line of the patterns file as a pattern; without
# filtering, blanks would match every input line and #-comments would match
# any line containing '#'.
build_effective() {
  local src="$1"
  local dst="$2"
  if [ -f "$src" ]; then
    grep -Ev '^[[:space:]]*(#|$)' "$src" > "$dst" 2>/dev/null || true
  else
    : > "$dst"
  fi
}

CI_EFFECTIVE=$(mktemp)
CS_EFFECTIVE=$(mktemp)
trap 'rm -f "$CI_EFFECTIVE" "$CS_EFFECTIVE"' EXIT

build_effective "$CI_PATTERNS_FILE" "$CI_EFFECTIVE"
build_effective "$CS_PATTERNS_FILE" "$CS_EFFECTIVE"

if [ ! -s "$CI_EFFECTIVE" ] && [ ! -s "$CS_EFFECTIVE" ]; then
  echo "no active patterns in any protected-words file — check skipped"
  exit 0
fi

# Render each ADDED line from the staged diff as "file:line: content". The awk
# script tracks the current file (from "diff --git" headers) and the current
# line number (from @@ hunks).
ADDITIONS=$(git diff --cached -p --src-prefix='' --dst-prefix='' \
  | awk '
    /^diff --git/ { f = $3; next }
    /^@@/         { sub(/^.*\+/, "", $3); l = $3 + 0; next }
    /^\+[^+]/     { print f ":" l ": " substr($0, 2); l++; next }
    /^[ ]/        { l++ }
  ')

# Run case-insensitive and case-sensitive grep passes; collect matches.
MATCHES=""
if [ -s "$CI_EFFECTIVE" ] && [ -n "$ADDITIONS" ]; then
  CI_MATCHES=$(printf '%s\n' "$ADDITIONS" | grep -E -i -f "$CI_EFFECTIVE" || true)
  if [ -n "$CI_MATCHES" ]; then
    MATCHES="$CI_MATCHES"
  fi
fi
if [ -s "$CS_EFFECTIVE" ] && [ -n "$ADDITIONS" ]; then
  CS_MATCHES=$(printf '%s\n' "$ADDITIONS" | grep -E -f "$CS_EFFECTIVE" || true)
  if [ -n "$CS_MATCHES" ]; then
    if [ -n "$MATCHES" ]; then
      MATCHES="$MATCHES
$CS_MATCHES"
    else
      MATCHES="$CS_MATCHES"
    fi
  fi
fi

if [ -n "$MATCHES" ]; then
  # Deduplicate (a line could match patterns in both files).
  MATCHES=$(printf '%s\n' "$MATCHES" | awk '!seen[$0]++')
  echo "FAIL: protected words found in staged additions." >&2
  echo "" >&2
  echo "$MATCHES" >&2
  echo "" >&2
  echo "Sanitize the staged content (replace the term with a generic placeholder)," >&2
  echo "re-stage, and re-invoke /commit. For confirmed false positives, tighten" >&2
  echo "the pattern in $CI_PATTERNS_FILE or $CS_PATTERNS_FILE." >&2
  exit 1
fi

exit 0
