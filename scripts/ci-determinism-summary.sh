#!/usr/bin/env bash
# Classify determinism-workflow artifacts into findings vs incomplete
# execution. Reads the <producer>.exit-code.txt convention plus verdict.tsv
# and <suite>-repeat.json written by the producer jobs.
#
# Usage: ci-determinism-summary.sh <artifacts-dir> <selected-suite>
#   <selected-suite>: all | backend | frontend | docker-suites | detector-contract
#
# Exit 0 = every selected producer clean/verified; 1 = anything needing
# attention (findings or incomplete execution); 2 = usage error.
set -euo pipefail

[[ $# -eq 2 ]] || { echo "usage: $0 <artifacts-dir> <selected-suite>" >&2; exit 2; }
ART="$1"
SELECTED="$2"
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }
[[ -d "$ART" ]] || { echo "artifacts dir not found: $ART" >&2; exit 2; }

# Which artifact each selection expects. `backend` covers both backend jobs.
expected_for() {
  case "$SELECTED" in
    all) echo "determinism-detector-contract determinism-backend determinism-race determinism-frontend determinism-docker-suites" ;;
    backend) echo "determinism-detector-contract:skip determinism-backend determinism-race determinism-frontend:skip determinism-docker-suites:skip" ;;
    frontend) echo "determinism-detector-contract:skip determinism-backend:skip determinism-race:skip determinism-frontend determinism-docker-suites:skip" ;;
    docker-suites) echo "determinism-detector-contract:skip determinism-backend:skip determinism-race:skip determinism-frontend:skip determinism-docker-suites" ;;
    detector-contract) echo "determinism-detector-contract determinism-backend:skip determinism-race:skip determinism-frontend:skip determinism-docker-suites:skip" ;;
    *) echo "unknown suite: $SELECTED" >&2; exit 2 ;;
  esac
}

attention=0

# classify <artifact-name> <skipped?> — prints one markdown table row.
classify() {
  local art="$1" skipped="$2"
  if [[ "$skipped" == "skip" ]]; then
    echo "| $art | skipped | not selected for this run |"
    return
  fi
  local dir="$ART/$art"
  if [[ ! -d "$dir" ]]; then
    echo "| $art | INCOMPLETE | artifact missing — producer job cancelled or failed before upload |"
    attention=$((attention + 1))
    return
  fi
  local any_code=0
  while IFS= read -r -d '' code_file; do
    local producer code
    producer="$(basename "$code_file" .exit-code.txt)"
    code="$(cat "$code_file")"
    local outcome="clean" detail=""
    case "$code" in
      0) outcome="clean" ;;
      1)
        outcome="FINDINGS"
        detail="$(describe_findings "$art")"
        ;;
      *) outcome="INCOMPLETE" ; detail="exit $code — execution error, evidence not clean" ;;
    esac
    if [[ "$outcome" != "clean" ]]; then attention=$((attention + 1)); fi
    echo "| $art/$producer | $outcome | ${detail:-exit $code} |"
    [[ "$code" != "0" ]] && any_code=1
  done < <(find "$dir" -name '*.exit-code.txt' -print0 | sort -z)
  if [[ "$any_code" == 0 ]] && ! find "$dir" -name '*.exit-code.txt' | grep -q .; then
    echo "| $art | INCOMPLETE | no exit-code file captured"
    attention=$((attention + 1))
  fi
}

describe_findings() {
  local art="$1"
  if [[ -s "$ART/$art/verdict.tsv" ]]; then
    awk -F'\t' 'NR>1 { counts[$1]++ } END { out=""; for (v in counts) out = out (out==""?"":", ") v " x" counts[v]; print out }' "$ART/$art/verdict.tsv"
  else
    local reports json
    reports="$(find "$ART/$art" -name '*-repeat.json' 2>/dev/null || true)"
    for f in $reports; do
      json="$(jq -r 'if (.findings | length) > 0 then "flaky x\(.findings | length)" else (if .incompleteEvidence then "incomplete evidence" else empty end) end' "$f" 2>/dev/null || true)"
      [[ -n "$json" ]] && echo "$f: $json"
    done
  fi
}

echo "## Determinism run classification"
echo
echo "| Producer | Outcome | Detail |"
echo "| --- | --- | --- |"
for entry in $(expected_for); do
  classify "${entry%%:*}" "${entry##*:}"
done
echo
if [[ "$attention" -gt 0 ]]; then
  echo "**$attention producer result(s) need attention** — findings and incomplete execution both fail this run; see the per-job summaries and artifacts."
  exit 1
fi
echo "All selected producers clean."
exit 0