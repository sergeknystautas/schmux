#!/usr/bin/env bash
# Repeat the untagged backend tests under several runtime configurations and
# report tests whose result changes. Each sample is a separate `go test`
# process, so shuffle gets a new seed and process-global state starts clean.
#
# Usage:
#   scripts/determinism.sh
#   scripts/determinism.sh --runs 10
#   scripts/determinism.sh --configs base,shuffle,race
#   scripts/determinism.sh --pkg ./internal/dashboard/...
#
# Results are written to .schmux/determinism/ by default. Exit status is 0 when
# no variation is found, 1 when findings are reported, and 2 when a run could
# not execute or its output could not be analyzed.
set -euo pipefail

RUNS=3
CONFIGS="base,cpu1,cpu8,shuffle,race,minpath"
PKG=""
OUT=".schmux/determinism"

usage_error() {
  echo "$1" >&2
  echo "run '$0 --help' for usage" >&2
  exit 2
}

print_help() {
  cat <<'EOF'
Usage: scripts/determinism.sh [OPTIONS]

Options:
  --runs N              Independent samples per config (default: 3)
  --configs LIST        Comma-separated configs: base,cpu1,cpu8,shuffle,race,minpath
  --pkg PATTERNS        Space-separated Go package patterns
  --out DIRECTORY       Results directory (default: .schmux/determinism)
  -h, --help            Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --runs)
      [[ $# -ge 2 ]] || usage_error "--runs requires a value"
      RUNS="$2"
      shift 2
      ;;
    --configs)
      [[ $# -ge 2 ]] || usage_error "--configs requires a value"
      CONFIGS="$2"
      shift 2
      ;;
    --pkg)
      [[ $# -ge 2 ]] || usage_error "--pkg requires a value"
      PKG="$2"
      shift 2
      ;;
    --out)
      [[ $# -ge 2 ]] || usage_error "--out requires a value"
      OUT="$2"
      shift 2
      ;;
    -h|--help)
      print_help
      exit 0
      ;;
    *) usage_error "unknown flag: $1" ;;
  esac
done

[[ "$RUNS" =~ ^[1-9][0-9]*$ ]] || usage_error "--runs must be a positive integer"
[[ -n "$CONFIGS" ]] || usage_error "--configs must not be empty"
[[ -n "$OUT" && "$OUT" != "/" ]] || usage_error "--out must name a directory other than /"

command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }
GO_BIN="$(command -v go)"

cd "$(dirname "$0")/.."

REQUESTED_CONFIGS=()
IFS=',' read -ra REQUESTED_CONFIGS <<< "$CONFIGS"
for cfg in "${REQUESTED_CONFIGS[@]}"; do
  case "$cfg" in
    base|cpu1|cpu8|shuffle|race|minpath) ;;
    "") usage_error "--configs contains an empty configuration" ;;
    *) usage_error "unknown config: $cfg" ;;
  esac
done

PACKAGE_PATTERNS=()
if [[ -n "$PKG" ]]; then
  read -ra PACKAGE_PATTERNS <<< "$PKG"
else
  PACKAGE_PATTERNS=("./...")
fi

# Resolve patterns before starting. This catches typos instead of turning a
# go-list or setup failure into a false "no variation" result.
PACKAGE_OUTPUT="$("$GO_BIN" list "${PACKAGE_PATTERNS[@]}")" || {
  echo "go list failed; no tests were run" >&2
  exit 2
}
PACKAGES=()
while IFS= read -r pkg; do
  [[ -n "$pkg" ]] || continue
  if [[ -z "$PKG" && "$pkg" == */e2e ]]; then
    continue
  fi
  PACKAGES+=("$pkg")
done <<< "$PACKAGE_OUTPUT"
[[ ${#PACKAGES[@]} -gt 0 ]] || { echo "no packages to test" >&2; exit 2; }

mkdir -p "$OUT"
: > "$OUT/run-errors.tsv"

# Background each invocation in its own process group. Interrupting the harness
# must also stop the compiled test binaries launched by `go test`.
set -m
CHILD_PGID=""

stop_child() {
  if [[ -n "$CHILD_PGID" ]]; then
    kill -TERM "-$CHILD_PGID" 2>/dev/null || true
    sleep 1
    kill -KILL "-$CHILD_PGID" 2>/dev/null || true
  fi
  rm -f "$OUT/RUNNING.pgid"
}
trap 'stop_child; exit 130' INT TERM
trap stop_child EXIT

run_sample() {
  local cfg="$1"
  local run="$2"
  shift 2
  local -a flags=("$@")
  local raw="$OUT/.${cfg}-${run}.jsonl"
  local stderr_file="$OUT/${cfg}-${run}.stderr"
  local rc

  if [[ "$cfg" == "minpath" ]]; then
    env PATH="/usr/bin:/bin:/usr/sbin:/sbin" "$GO_BIN" test -short -json \
      "${flags[@]}" "${PACKAGES[@]}" > "$raw" 2> "$stderr_file" &
  else
    "$GO_BIN" test -short -json \
      "${flags[@]}" "${PACKAGES[@]}" > "$raw" 2> "$stderr_file" &
  fi
  CHILD_PGID=$!
  echo "$CHILD_PGID" > "$OUT/RUNNING.pgid"
  if wait "$CHILD_PGID"; then
    rc=0
  else
    rc=$?
  fi
  CHILD_PGID=""
  rm -f "$OUT/RUNNING.pgid"

  if ! jq -c --arg cfg "$cfg" --argjson run "$run" \
    '. + {_schmux: {config: $cfg, run: $run}}' "$raw" >> "$OUT/$cfg.jsonl"; then
    echo "$cfg run $run produced invalid go test JSON" >&2
    return 2
  fi

  local completed
  completed="$(jq -s '[.[] | select(.Test != null and (.Action == "pass" or .Action == "fail" or .Action == "skip"))] | length' "$raw")"
  printf '    run %-3d exit=%-3d %s completed test results\n' "$run" "$rc" "$completed"

  # A non-zero exit is expected when an individual test fails. Build failures,
  # setup failures, and unexplained non-zero exits are execution errors because
  # they do not provide a test result the verdict logic can classify.
  local test_failures execution_failures
  test_failures="$(jq -s '[.[] | select(.Test != null and .Action == "fail")] | length' "$raw")"
  execution_failures="$(jq -s '
    . as $events
    | [
        $events[]
        | select(.Action == "build-fail" or (.Action == "fail" and .Test == null and .Package != null))
        | . as $failure
        | select(
            .Action == "build-fail"
            or ([
                  $events[]
                  | select(.Package == $failure.Package and .Test != null and .Action == "fail")
                ] | length) == 0
          )
      ]
    | length
  ' "$raw")"

  local interrupted=false
  if [[ "$rc" -ge 128 ]] || jq -e 'select((.Output? // "") == "signal: interrupt\n")' "$raw" >/dev/null; then
    interrupted=true
  fi

  rm -f "$raw"
  local stderr_ref="$stderr_file"
  if [[ ! -s "$stderr_file" ]]; then
    rm -f "$stderr_file"
    stderr_ref="-"
  fi

  if [[ "$execution_failures" -gt 0 || ("$rc" -ne 0 && "$test_failures" -eq 0) ]]; then
    printf '%s\t%s\t%s\t%s\n' "$cfg" "$run" "$rc" "$stderr_ref" >> "$OUT/run-errors.tsv"
  fi

  if [[ "$interrupted" == true ]]; then
    return 130
  fi
  if [[ "$execution_failures" -gt 0 || ("$rc" -ne 0 && "$test_failures" -eq 0) ]]; then
    return 2
  fi
}

echo "determinism: ${#PACKAGES[@]} packages, ${RUNS} independent runs per config (race: 1)"
echo "configs: ${REQUESTED_CONFIGS[*]}"
echo "to stop the active run: kill -TERM -\$(cat $OUT/RUNNING.pgid)"
echo

for cfg in "${REQUESTED_CONFIGS[@]}"; do
  sample_count="$RUNS"
  flags=("-count=1")
  case "$cfg" in
    base) ;;
    cpu1) flags+=("-cpu=1") ;;
    cpu8) flags+=("-cpu=8") ;;
    shuffle) flags+=("-shuffle=on") ;;
    race)
      flags+=("-race")
      sample_count=1
      ;;
    minpath) ;;
  esac

  : > "$OUT/$cfg.jsonl"
  echo "  $cfg"
  for ((run = 1; run <= sample_count; run++)); do
    run_sample "$cfg" "$run" "${flags[@]}" || exit $?
  done
done

# A test is flaky only when it both passes and fails inside the same
# configuration. Variation observed only across configurations is reported
# separately: it may expose a real dependency on the named knob, but it is not
# evidence of stochastic behavior by itself.
for cfg in "${REQUESTED_CONFIGS[@]}"; do
  cat "$OUT/$cfg.jsonl"
done | jq -s -r '
  [
    .[]
    | select(.Test != null and (.Action == "pass" or .Action == "fail" or .Action == "skip"))
    | {cfg: ._schmux.config, pkg: .Package, test: .Test, action: .Action}
  ]
  | group_by([.pkg, .test])
  | map(
      . as $events
      | (group_by(.cfg)
         | map({
             cfg: .[0].cfg,
             pass: ([.[] | select(.action == "pass")] | length),
             fail: ([.[] | select(.action == "fail")] | length),
             skip: ([.[] | select(.action == "skip")] | length)
           })) as $by_config
      | {
          pkg: $events[0].pkg,
          test: $events[0].test,
          pass: ([$events[] | select(.action == "pass")] | length),
          fail: ([$events[] | select(.action == "fail")] | length),
          skip: ([$events[] | select(.action == "skip")] | length),
          failed_in: ([$by_config[] | select(.fail > 0) | .cfg] | unique),
          flaky_in: ([$by_config[] | select(.pass > 0 and .fail > 0) | .cfg] | unique),
          skipped_in: ([$by_config[] | select(.skip > 0) | .cfg] | unique)
        }
    )
  | map(
      . + {
        verdict:
          (if (.flaky_in | length) > 0 then "FLAKY"
           elif .fail > 0 and .pass > 0 then "CONFIG-SENSITIVE"
           elif .fail > 0 then "ALWAYS-FAIL"
           elif .skip > 0 and .pass > 0 then "HOST-GATED"
           else "deterministic"
           end)
      }
    )
  | map(select(.verdict != "deterministic"))
  | sort_by(.verdict, .pkg, .test)
  | .[]
  | [
      .verdict,
      .pass,
      .fail,
      .skip,
      (if (.failed_in | length) == 0 then "-" else (.failed_in | join(",")) end),
      (if (.flaky_in | length) == 0 then "-" else (.flaky_in | join(",")) end),
      (if (.skipped_in | length) == 0 then "-" else (.skipped_in | join(",")) end),
      (.pkg | sub("^github.com/[^/]+/[^/]+/"; "")),
      .test
    ]
  | @tsv
' > "$OUT/verdict.tsv"

print_table() {
  if command -v column >/dev/null; then
    column -t -s $'\t'
  else
    cat
  fi
}

echo
if [[ -s "$OUT/run-errors.tsv" ]]; then
  {
    printf 'CONFIG\tRUN\tEXIT\tSTDERR\n'
    cat "$OUT/run-errors.tsv"
  } | print_table
  echo
  echo "One or more test runs did not execute cleanly."
  echo "Raw results: $OUT/"
  exit 2
fi

if [[ -s "$OUT/verdict.tsv" ]]; then
  {
    printf 'VERDICT\tPASS\tFAIL\tSKIP\tFAILED_IN\tFLAKY_IN\tSKIPPED_IN\tPACKAGE\tTEST\n'
    cat "$OUT/verdict.tsv"
  } | print_table
  echo
  echo "$(wc -l < "$OUT/verdict.tsv" | tr -d ' ') tests need attention."
  echo "Raw results: $OUT/"
  exit 1
fi

echo "No test result varied under the sampled configurations."
echo "Raw results: $OUT/"
