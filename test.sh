#!/usr/bin/env bash

# test.sh - Comprehensive test runner for schmux
# Usage: ./test.sh [OPTIONS]
#
# Options:
#   --unit          Run unit tests only (default)
#   --e2e           Run E2E tests only
#   --scenarios     Run scenario tests only (Playwright)
#   --bench         Run latency benchmarks only (requires tmux)
#   --react         Run React dashboard tests only
#   --all           Run all test suites in parallel (unit, react, E2E, scenarios)
#   --race          Run with race detector
#   --verbose       Run with verbose output
#   --coverage      Run with coverage report
#   --quick         Run without race detector or coverage (fast)
#   --force         Force rebuild Docker images (skip cache)
#   --run PATTERN   Run only tests matching PATTERN (go test -run / playwright --grep)
#   --help          Show this help message

set -e  # Exit on error
set -u  # Treat unset variables as errors
set -o pipefail  # Propagate errors through pipes

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default options
RUN_UNIT=true
RUN_E2E=false
RUN_SCENARIOS=false
RUN_BENCH=false
RUN_REACT=false
RUN_RACE=false
RUN_VERBOSE=false
RUN_COVERAGE=false
FORCE_BUILD=false
RUN_ALL=false
TEST_RUN_PATTERN=""

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --unit)
            RUN_UNIT=true
            RUN_E2E=false
            RUN_SCENARIOS=false
            shift
            ;;
        --e2e)
            RUN_UNIT=false
            RUN_E2E=true
            shift
            ;;
        --scenarios)
            RUN_UNIT=false
            RUN_E2E=false
            RUN_SCENARIOS=true
            shift
            ;;
        --react)
            RUN_UNIT=false
            RUN_E2E=false
            RUN_SCENARIOS=false
            RUN_REACT=true
            shift
            ;;
        --all)
            RUN_UNIT=true
            RUN_E2E=true
            RUN_SCENARIOS=true
            RUN_ALL=true
            shift
            ;;
        --bench)
            RUN_UNIT=false
            RUN_E2E=false
            RUN_SCENARIOS=false
            RUN_BENCH=true
            shift
            ;;
        --race)
            RUN_RACE=true
            shift
            ;;
        --verbose)
            RUN_VERBOSE=true
            shift
            ;;
        --coverage)
            RUN_COVERAGE=true
            shift
            ;;
        --quick)
            RUN_RACE=false
            RUN_COVERAGE=false
            shift
            ;;
        --force)
            FORCE_BUILD=true
            shift
            ;;
        --run)
            if [[ -z "${2:-}" ]]; then
                echo -e "${RED}--run requires a test pattern argument${NC}"
                exit 1
            fi
            TEST_RUN_PATTERN="$2"
            shift 2
            ;;
        --help)
            echo "Usage: ./test.sh [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --unit          Run unit tests only (default)"
            echo "  --e2e           Run E2E tests only"
            echo "  --scenarios     Run scenario tests only (Playwright)"
            echo "  --react         Run React dashboard tests only"
            echo "  --bench         Run latency benchmarks only (requires tmux)"
            echo "  --all           Run all test suites in parallel (unit, react, E2E, scenarios)"
            echo "  --race          Run with race detector"
            echo "  --verbose       Run with verbose output"
            echo "  --coverage      Run with coverage report"
            echo "  --quick         Run without race detector or coverage (fast)"
            echo "  --force         Force rebuild Docker base images (skip cache)"
            echo "  --run PATTERN   Run only tests matching PATTERN (go test -run / playwright --grep)"
            echo "  --help          Show this help message"
            echo ""
            echo "Examples:"
            echo "  ./test.sh                    # Run unit tests"
            echo "  ./test.sh --all              # Run all tests in parallel"
            echo "  ./test.sh --race --verbose   # Run unit tests with race detector and verbose output"
            echo "  ./test.sh --e2e              # Run E2E tests only"
            echo "  ./test.sh --e2e --run TestE2EOverlayCompounding  # Run a single E2E test"
            echo "  ./test.sh --coverage         # Run unit tests with coverage"
            echo "  ./test.sh --scenarios        # Run scenario tests only (Playwright)"
            echo "  ./test.sh --scenarios --run 'dispose'  # Run scenario tests matching 'dispose'"
            echo "  ./test.sh --react            # Run React dashboard tests only"
            echo "  ./test.sh --e2e --force      # Rebuild base image and run E2E tests"
            echo "  ./test.sh --bench            # Run latency benchmarks (requires tmux locally)"
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            echo "Run './test.sh --help' for usage information"
            exit 1
            ;;
    esac
done

# Check and auto-install dependencies
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/scripts/check-deps.sh"

DEPS=("go:brew:go")
if [ "$RUN_E2E" = true ] || [ "$RUN_SCENARIOS" = true ]; then
    DEPS+=("docker:brew-cask:docker")
fi
if [ "$RUN_BENCH" = true ]; then
    DEPS+=("python3:brew:python3")
fi
check_deps "${DEPS[@]}"

# Print header
echo -e "${BLUE}╔═══════════════════════╗${NC}"
echo -e "${BLUE}║   Schmux Test Suite   ║${NC}"
echo -e "${BLUE}╚═══════════════════════╝${NC}"
echo ""

# Track overall status
EXIT_CODE=0

# ─────────────────────────────────────────────────────────────────────────────
# Helper: ensure a base Docker image exists (build if missing or --force)
# Usage: ensure_base_image <image_name> <dockerfile> <label>
# ─────────────────────────────────────────────────────────────────────────────
ensure_base_image() {
    local image_name="$1"
    local dockerfile="$2"
    local label="$3"

    if [ "$FORCE_BUILD" = true ] || ! docker image inspect "$image_name" > /dev/null 2>&1; then
        echo -e "  ${BLUE}🐳 Building ${label} base image...${NC}"
        if [ "$RUN_VERBOSE" = true ]; then
            if ! docker build -f "$dockerfile" -t "$image_name" .; then
                echo -e "  ${RED}❌ Failed to build ${label} base image${NC}"
                return 1
            fi
        else
            if ! docker build -f "$dockerfile" -t "$image_name" . > /dev/null 2>&1; then
                echo -e "  ${RED}❌ Failed to build ${label} base image${NC}"
                return 1
            fi
        fi
        echo -e "  ${GREEN}✅ ${label} base image built${NC}"
    else
        echo -e "  ${GREEN}✅ Reusing cached ${label} base image (use --force to rebuild)${NC}"
    fi
    return 0
}

# ─────────────────────────────────────────────────────────────────────────────
# Local build: cross-compile schmux binary + dashboard for Docker tests
# ─────────────────────────────────────────────────────────────────────────────
LOCAL_BUILD_DONE=false
DASHBOARD_BUILT=false

# Unique suffix for ephemeral Docker images so concurrent runs don't collide
RUN_ID=$$

build_local_artifacts() {
    if [ "$LOCAL_BUILD_DONE" = true ]; then
        return 0
    fi

    echo -e "${YELLOW}▶️  Building local artifacts for Docker tests...${NC}"
    mkdir -p build

    echo -e "  ${BLUE}🔨 Cross-compiling schmux for Linux...${NC}"
    if ! GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/schmux-linux ./cmd/schmux; then
        echo -e "  ${RED}❌ Failed to cross-compile schmux${NC}"
        return 1
    fi
    echo -e "  ${GREEN}✅ Binary built: build/schmux-linux${NC}"

    LOCAL_BUILD_DONE=true
    echo ""
    return 0
}

# ─────────────────────────────────────────────────────────────────────────────
# Suite runner functions
# Each returns 0 on success, 1 on failure. Reads globals, does NOT call exit.
# ─────────────────────────────────────────────────────────────────────────────

run_unit_tests() {
    echo -e "${YELLOW}▶️  Running unit tests...${NC}"

    TEST_ARGS=(go test -short ./...)

    if [ "$RUN_RACE" = true ]; then
        TEST_ARGS+=(-race)
        echo -e "  ${BLUE}🔍 Race detector enabled${NC}"
    fi

    if [ "$RUN_VERBOSE" = true ]; then
        TEST_ARGS+=(-v)
        echo -e "  ${BLUE}📢 Verbose output enabled${NC}"
    fi

    if [ "$RUN_COVERAGE" = true ]; then
        TEST_ARGS+=(-coverprofile=coverage.out -covermode=atomic)
        echo -e "  ${BLUE}📊 Coverage enabled${NC}"
    fi

    if [ -n "$TEST_RUN_PATTERN" ]; then
        TEST_ARGS+=(-run "$TEST_RUN_PATTERN")
        echo -e "  ${BLUE}🎯 Filter: $TEST_RUN_PATTERN${NC}"
    fi

    echo ""

    local output_file
    output_file=$(mktemp)
    if "${TEST_ARGS[@]}" 2>&1 | tee "$output_file"; then
        echo ""
        echo -e "${GREEN}✅ Unit tests passed${NC}"

        if [ "$RUN_COVERAGE" = true ]; then
            echo ""
            echo -e "${YELLOW}▶️  Coverage summary:${NC}"
            go tool cover -func=coverage.out | tail -n 1
            echo ""
            echo -e "  ${BLUE}📄 Full coverage report: coverage.out${NC}"
            echo -e "  ${BLUE}🌐 View HTML report: go tool cover -html=coverage.out${NC}"
        fi
        rm -f "$output_file"
        return 0
    else
        echo ""
        echo -e "${RED}❌ Unit tests failed${NC}"

        local failed_tests
        failed_tests=$(grep '^--- FAIL:' "$output_file" | sed 's/--- FAIL: //;s/ (.*//' | cut -d/ -f1 | sort -u)
        if [ -n "$failed_tests" ]; then
            echo ""
            echo -e "${BLUE}Re-run individually with:${NC}"
            while IFS= read -r test_name; do
                echo -e "  go test -run ${test_name} ./..."
            done <<< "$failed_tests"
        fi

        rm -f "$output_file"
        return 1
    fi
}

run_react_tests() {
    echo -e "${YELLOW}▶️  Running React dashboard tests...${NC}"
    if (cd "$SCRIPT_DIR/assets/dashboard" && { [ -d node_modules ] || npm ci --silent; } && npx vitest run); then
        echo ""
        echo -e "${GREEN}✅ React dashboard tests passed${NC}"
        return 0
    else
        echo ""
        echo -e "${RED}❌ React dashboard tests failed${NC}"
        return 1
    fi
}

run_e2e_tests() {
    echo -e "${YELLOW}▶️  Running E2E tests...${NC}"
    echo ""

    local image_tag="schmux-e2e-${RUN_ID}"

    if ! command -v docker &> /dev/null; then
        echo -e "${RED}❌ Docker is not installed or not in PATH${NC}"
        echo -e "  ${BLUE}💡 E2E tests require Docker${NC}"
        return 1
    fi

    if ! build_local_artifacts; then
        return 1
    fi

    if ! ensure_base_image schmux-e2e-base Dockerfile.e2e-base "E2E"; then
        return 1
    fi

    echo -e "  ${BLUE}🐳 Building E2E test image...${NC}"
    local e2e_image_ready=false
    if [ "$RUN_VERBOSE" = true ]; then
        if docker build -f Dockerfile.e2e -t "$image_tag" .; then
            e2e_image_ready=true
        fi
    else
        if docker build -f Dockerfile.e2e -t "$image_tag" . > /dev/null 2>&1; then
            e2e_image_ready=true
        fi
    fi

    if [ "$e2e_image_ready" != true ]; then
        echo -e "  ${RED}❌ Failed to build E2E test image${NC}"
        return 1
    fi

    echo -e "  ${GREEN}✅ E2E test image built${NC}"
    echo ""
    echo -e "  ${BLUE}🚀 Running E2E tests in container...${NC}"
    if [ -n "$TEST_RUN_PATTERN" ]; then
        echo -e "  ${BLUE}🎯 Filter: $TEST_RUN_PATTERN${NC}"
    fi
    echo ""

    local docker_run_args=(docker run --rm)
    if [ -n "$TEST_RUN_PATTERN" ]; then
        docker_run_args+=(-e "TEST_RUN=$TEST_RUN_PATTERN")
    fi
    docker_run_args+=("$image_tag")

    local e2e_output_file
    e2e_output_file=$(mktemp)
    if "${docker_run_args[@]}" 2>&1 | tee "$e2e_output_file"; then
        echo ""
        echo -e "${GREEN}✅ E2E tests passed${NC}"
        rm -f "$e2e_output_file"
        docker rmi "$image_tag" > /dev/null 2>&1 || true
        return 0
    else
        echo ""
        echo -e "${RED}❌ E2E tests failed${NC}"

        local failed_tests
        failed_tests=$(grep '^--- FAIL:' "$e2e_output_file" | sed 's/--- FAIL: //;s/ (.*//' | cut -d/ -f1 | sort -u)
        if [ -n "$failed_tests" ]; then
            echo ""
            echo -e "${RED}Failed tests:${NC}"
            echo ""
            echo -e "${BLUE}Re-run individually with:${NC}"
            while IFS= read -r test_name; do
                echo -e "  ./test.sh --e2e --run ${test_name}"
            done <<< "$failed_tests"
        fi

        rm -f "$e2e_output_file"
        docker rmi "$image_tag" > /dev/null 2>&1 || true
        return 1
    fi
}

run_scenario_tests() {
    echo -e "${YELLOW}▶️  Running scenario tests...${NC}"
    echo ""

    local image_tag="schmux-scenarios-${RUN_ID}"

    if ! command -v docker &> /dev/null; then
        echo -e "${RED}❌ Docker is not installed or not in PATH${NC}"
        echo -e "  ${BLUE}💡 Scenario tests require Docker${NC}"
        return 1
    fi

    local artifacts_dir="test/scenarios/artifacts"
    rm -rf "$artifacts_dir"
    mkdir -p "$artifacts_dir"

    if ! build_local_artifacts; then
        return 1
    fi

    # Dashboard build (needed for scenarios, not for E2E)
    if [ "${DASHBOARD_BUILT:-false}" != true ]; then
        local dashboard_args=()
        if [ -d "$SCRIPT_DIR/assets/dashboard/node_modules" ]; then
            dashboard_args+=(--skip-install)
        fi
        echo -e "  ${BLUE}🎨 Building dashboard...${NC}"
        if ! go run ./cmd/build-dashboard "${dashboard_args[@]+"${dashboard_args[@]}"}"; then
            echo -e "  ${RED}❌ Failed to build dashboard${NC}"
            return 1
        fi
        echo -e "  ${GREEN}✅ Dashboard built${NC}"
    fi

    if ! ensure_base_image schmux-scenarios-base Dockerfile.scenarios-base "Scenario"; then
        return 1
    fi

    echo -e "  ${BLUE}🐳 Building scenario test image...${NC}"
    local scenario_image_ready=false
    if [ "$RUN_VERBOSE" = true ]; then
        if docker build -f Dockerfile.scenarios -t "$image_tag" .; then
            scenario_image_ready=true
        fi
    else
        if docker build -f Dockerfile.scenarios -t "$image_tag" . > /dev/null 2>&1; then
            scenario_image_ready=true
        fi
    fi

    if [ "$scenario_image_ready" != true ]; then
        echo -e "  ${RED}❌ Failed to build scenario test image${NC}"
        return 1
    fi

    echo -e "  ${GREEN}✅ Scenario test image built${NC}"
    echo ""
    echo -e "  ${BLUE}🎭 Running Playwright scenario tests in container...${NC}"
    if [ -n "$TEST_RUN_PATTERN" ]; then
        echo -e "  ${BLUE}🎯 Filter: $TEST_RUN_PATTERN${NC}"
    fi
    echo ""

    local scenario_run_args=(docker run --rm -v "$(pwd)/$artifacts_dir:/artifacts")
    if [ -n "$TEST_RUN_PATTERN" ]; then
        scenario_run_args+=(-e "TEST_GREP=$TEST_RUN_PATTERN")
    fi
    scenario_run_args+=("$image_tag")

    local scenario_output_file
    scenario_output_file=$(mktemp)
    if "${scenario_run_args[@]}" 2>&1 | tee "$scenario_output_file"; then
        echo ""
        echo -e "${GREEN}✅ Scenario tests passed${NC}"
        rm -f "$scenario_output_file"
        docker rmi "$image_tag" > /dev/null 2>&1 || true
        return 0
    else
        echo ""
        echo -e "${RED}❌ Scenario tests failed${NC}"

        local failed_scenarios
        failed_scenarios=$(sed -n 's/.*\.spec\.ts:[0-9]*:[0-9]* › \([^›─]*\).*/\1/p' "$scenario_output_file" | sed 's/ *$//' | sort -u)
        if [ -n "$failed_scenarios" ]; then
            echo ""
            echo -e "${BLUE}Re-run individually with:${NC}"
            while IFS= read -r scenario_name; do
                echo -e "  ./test.sh --scenarios --run '${scenario_name}'"
            done <<< "$failed_scenarios"
        fi

        echo ""
        echo -e "  ${BLUE}📁 Test artifacts saved to: $artifacts_dir/${NC}"
        if [ -d "$artifacts_dir/playwright-report" ]; then
            echo -e "  ${BLUE}🌐 View HTML report: npx playwright show-report $artifacts_dir/playwright-report${NC}"
        fi
        if [ -d "$artifacts_dir/test-results" ]; then
            echo -e "  ${BLUE}🎬 Videos/screenshots: $artifacts_dir/test-results/${NC}"
        fi

        rm -f "$scenario_output_file"
        docker rmi "$image_tag" > /dev/null 2>&1 || true
        return 1
    fi
}

# ─────────────────────────────────────────────────────────────────────────────
# Parallel execution: run all suites concurrently, collect results
# ─────────────────────────────────────────────────────────────────────────────
run_suites_parallel() {
    local parallel_dir
    parallel_dir=$(mktemp -d)
    local pids=()
    local suites=()

    echo -e "${YELLOW}▶️  Running test suites in parallel...${NC}"
    echo ""

    # Shared build step: cross-compile the binary (needed by E2E + scenarios)
    echo -e "${YELLOW}▶️  Building local artifacts for Docker tests...${NC}"
    mkdir -p build
    echo -e "  ${BLUE}🔨 Cross-compiling schmux for Linux...${NC}"
    if ! GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/schmux-linux ./cmd/schmux; then
        echo -e "  ${RED}❌ Failed to cross-compile schmux${NC}"
        rm -rf "$parallel_dir"
        return 1
    fi
    echo -e "  ${GREEN}✅ Binary built: build/schmux-linux${NC}"
    LOCAL_BUILD_DONE=true
    echo ""

    # Shared npm install (React tests + scenario dashboard build both need it)
    echo -e "  ${BLUE}📦 Installing dashboard dependencies...${NC}"
    if ! (cd "$SCRIPT_DIR/assets/dashboard" && npm ci --silent); then
        echo -e "  ${RED}❌ Failed to install dashboard dependencies${NC}"
        rm -rf "$parallel_dir"
        return 1
    fi
    echo -e "  ${GREEN}✅ Dashboard dependencies installed${NC}"
    echo ""

    # Shared dashboard build (scenarios need dist/, and building during fork
    # corrupts Docker build context for E2E)
    echo -e "  ${BLUE}🎨 Building dashboard...${NC}"
    if ! go run ./cmd/build-dashboard --skip-install; then
        echo -e "  ${RED}❌ Failed to build dashboard${NC}"
        rm -rf "$parallel_dir"
        return 1
    fi
    echo -e "  ${GREEN}✅ Dashboard built${NC}"
    DASHBOARD_BUILT=true
    echo ""

    # Fork: Unit tests
    suites+=("unit")
    (
        set +e
        run_unit_tests
        echo $? > "$parallel_dir/unit.status"
    ) > "$parallel_dir/unit.log" 2>&1 &
    pids+=($!)

    # Fork: React tests
    suites+=("react")
    (
        set +e
        run_react_tests
        echo $? > "$parallel_dir/react.status"
    ) > "$parallel_dir/react.log" 2>&1 &
    pids+=($!)

    # Fork: E2E tests
    suites+=("e2e")
    (
        set +e
        run_e2e_tests
        echo $? > "$parallel_dir/e2e.status"
    ) > "$parallel_dir/e2e.log" 2>&1 &
    pids+=($!)

    # Fork: Scenario tests (includes dashboard build internally)
    suites+=("scenarios")
    (
        set +e
        run_scenario_tests
        echo $? > "$parallel_dir/scenarios.status"
    ) > "$parallel_dir/scenarios.log" 2>&1 &
    pids+=($!)

    echo -e "  ${BLUE}⏳ Waiting for ${#suites[@]} suites: ${suites[*]}${NC}"
    echo ""

    # Wait for all background jobs
    for pid in "${pids[@]}"; do
        wait "$pid" 2>/dev/null || true
    done

    # Collect results and print output
    local any_failed=false
    local suite_labels=("Unit Tests" "React Tests" "E2E Tests" "Scenario Tests")

    for i in "${!suites[@]}"; do
        local suite="${suites[$i]}"
        local label="${suite_labels[$i]}"
        local status_file="$parallel_dir/${suite}.status"
        local log_file="$parallel_dir/${suite}.log"
        local status=1

        if [ -f "$status_file" ]; then
            status=$(cat "$status_file")
        fi

        echo -e "${BLUE}╔════════════════════════════════════════════════╗${NC}"
        if [ "$status" -eq 0 ]; then
            echo -e "${BLUE}║${NC}  ${GREEN}✅ ${label}${NC}$(printf '%*s' $((39 - ${#label})) '')${BLUE}║${NC}"
        else
            echo -e "${BLUE}║${NC}  ${RED}❌ ${label}${NC}$(printf '%*s' $((39 - ${#label})) '')${BLUE}║${NC}"
            any_failed=true
        fi
        echo -e "${BLUE}╚════════════════════════════════════════════════╝${NC}"

        if [ -f "$log_file" ]; then
            cat "$log_file"
        fi
        echo ""
    done

    rm -rf "$parallel_dir"

    if [ "$any_failed" = true ]; then
        return 1
    fi
    return 0
}

# ─────────────────────────────────────────────────────────────────────────────
# Run test suites
# ─────────────────────────────────────────────────────────────────────────────

if [ "$RUN_ALL" = true ]; then
    # Parallel mode: run all suites concurrently
    run_suites_parallel || EXIT_CODE=1
else
    # Serial mode: run requested suites one at a time
    if [ "$RUN_UNIT" = true ]; then
        run_unit_tests || EXIT_CODE=1
        echo ""
    fi

    if [ "$RUN_UNIT" = true ] || [ "$RUN_REACT" = true ]; then
        run_react_tests || EXIT_CODE=1
        echo ""
    fi

    if [ "$RUN_E2E" = true ]; then
        run_e2e_tests || EXIT_CODE=1
        echo ""
    fi

    if [ "$RUN_SCENARIOS" = true ]; then
        run_scenario_tests || EXIT_CODE=1
        echo ""
    fi
fi

# Run benchmarks
if [ "$RUN_BENCH" = true ]; then
    echo -e "${YELLOW}▶️  Running latency benchmarks...${NC}"
    echo ""

    # Check tmux is available
    if ! command -v tmux &> /dev/null; then
        echo -e "${RED}❌ tmux is not installed or not in PATH${NC}"
        echo -e "  ${BLUE}💡 Benchmarks require tmux locally (no Docker)${NC}"
        EXIT_CODE=1
    else
        BENCH_DIR="bench-results/$(date +%Y%m%d-%H%M%S)"
        mkdir -p "$BENCH_DIR"
        export BENCH_OUTPUT_DIR="$(cd "$BENCH_DIR" && pwd)"
        echo -e "  ${BLUE}📁 Output directory: $BENCH_DIR${NC}"
        echo ""

        echo -e "  ${BLUE}📊 Running percentile tests...${NC}"
        echo ""
        if go test -tags bench -run "TestLatency" -timeout 120s ./internal/session/ -v 2>&1 | tee "$BENCH_DIR/percentiles.log"; then
            echo ""
            echo -e "  ${GREEN}✅ Percentile tests passed${NC}"
        else
            echo ""
            echo -e "  ${RED}❌ Percentile tests failed${NC}"
            EXIT_CODE=1
        fi
        echo ""

        echo -e "  ${BLUE}⏱️  Running Go benchmark...${NC}"
        echo ""
        if go test -tags bench -run='^$' -bench "BenchmarkSendInput" -benchtime 5s -timeout 120s ./internal/session/ 2>&1 | tee "$BENCH_DIR/benchmark.log"; then
            echo ""
            echo -e "  ${GREEN}✅ Go benchmark passed${NC}"
        else
            echo ""
            echo -e "  ${RED}❌ Go benchmark failed${NC}"
            EXIT_CODE=1
        fi

        echo ""
        echo -e "  ${BLUE}📁 Results saved to: $BENCH_DIR/${NC}"

        # WebSocket benchmarks (require a running daemon)
        echo ""
        echo -e "  ${BLUE}🌐 Checking for running daemon (WebSocket benchmarks)...${NC}"
        if curl -s --max-time 2 http://localhost:7337/api/healthz > /dev/null 2>&1; then
            echo -e "  ${GREEN}✅ Daemon reachable${NC}"

            # Spawn a temporary cat session for WS benchmarks
            echo -e "  ${BLUE}🔧 Spawning temporary cat session...${NC}"

            # Get the first workspace ID from the sessions list
            WS_ID=$(curl -s http://localhost:7337/api/sessions | python3 -c "
import sys, json
data = json.load(sys.stdin)
if data:
    print(data[0].get('id', ''))
" 2>/dev/null)

            if [ -z "$WS_ID" ]; then
                echo -e "  ${YELLOW}⚠️  No workspaces found — skipping WS benchmarks${NC}"
            else
                # Spawn idle cat session
                SPAWN_RESP=$(curl -s -X POST http://localhost:7337/api/spawn \
                    -H 'Content-Type: application/json' \
                    -d "{\"workspace_id\": \"$WS_ID\", \"command\": \"cat\", \"nickname\": \"ws-bench\"}")

                BENCH_SID=$(echo "$SPAWN_RESP" | python3 -c "
import sys, json
data = json.load(sys.stdin)
if isinstance(data, list) and data:
    print(data[0].get('session_id', ''))
" 2>/dev/null)

                # Spawn stressed session (background output flood + cat)
                SPAWN_STRESSED_RESP=$(curl -s -X POST http://localhost:7337/api/spawn \
                    -H 'Content-Type: application/json' \
                    -d "{\"workspace_id\": \"$WS_ID\", \"command\": \"sh -c 'while true; do seq 1 50; sleep 0.05; done & exec cat'\", \"nickname\": \"ws-bench-stressed\"}")

                BENCH_SID_STRESSED=$(echo "$SPAWN_STRESSED_RESP" | python3 -c "
import sys, json
data = json.load(sys.stdin)
if isinstance(data, list) and data:
    print(data[0].get('session_id', ''))
" 2>/dev/null)

                if [ -z "$BENCH_SID" ]; then
                    echo -e "  ${RED}❌ Failed to spawn cat session for WS benchmarks${NC}"
                    echo -e "  ${BLUE}Response: $SPAWN_RESP${NC}"
                else
                    echo -e "  ${GREEN}✅ Spawned idle session $BENCH_SID${NC}"
                    if [ -n "$BENCH_SID_STRESSED" ]; then
                        echo -e "  ${GREEN}✅ Spawned stressed session $BENCH_SID_STRESSED${NC}"
                    else
                        echo -e "  ${YELLOW}⚠️  Failed to spawn stressed session — stressed WS benchmark will be skipped${NC}"
                    fi
                    # Give the sessions time to start and attach PTY
                    sleep 2

                    export BENCH_SESSION_ID="$BENCH_SID"
                    if [ -n "$BENCH_SID_STRESSED" ]; then
                        export BENCH_SESSION_ID_STRESSED="$BENCH_SID_STRESSED"
                    fi

                    echo ""
                    echo -e "  ${BLUE}📊 Running WebSocket percentile tests...${NC}"
                    echo ""
                    if go test -tags bench -run "TestWSLatency" -timeout 120s ./internal/dashboard/ -v 2>&1 | tee "$BENCH_DIR/ws_percentiles.log"; then
                        echo ""
                        echo -e "  ${GREEN}✅ WebSocket percentile tests passed${NC}"
                    else
                        echo ""
                        echo -e "  ${RED}❌ WebSocket percentile tests failed${NC}"
                        EXIT_CODE=1
                    fi
                    echo ""

                    echo -e "  ${BLUE}⏱️  Running WebSocket Go benchmark...${NC}"
                    echo ""
                    if go test -tags bench -run='^$' -bench "BenchmarkWSEcho" -benchtime 5s -timeout 120s ./internal/dashboard/ 2>&1 | tee "$BENCH_DIR/ws_benchmark.log"; then
                        echo ""
                        echo -e "  ${GREEN}✅ WebSocket Go benchmark passed${NC}"
                    else
                        echo ""
                        echo -e "  ${RED}❌ WebSocket Go benchmark failed${NC}"
                        EXIT_CODE=1
                    fi

                    # Dispose the temporary sessions
                    echo ""
                    echo -e "  ${BLUE}🧹 Disposing temporary sessions...${NC}"
                    curl -s -X POST "http://localhost:7337/api/sessions/$BENCH_SID/dispose" > /dev/null 2>&1
                    if [ -n "$BENCH_SID_STRESSED" ]; then
                        curl -s -X POST "http://localhost:7337/api/sessions/$BENCH_SID_STRESSED/dispose" > /dev/null 2>&1
                    fi
                    echo -e "  ${GREEN}✅ Cleaned up${NC}"
                fi
            fi
        else
            echo -e "  ${YELLOW}⚠️  Daemon not running — skipping WebSocket benchmarks${NC}"
            echo -e "  ${BLUE}💡 Start daemon with './schmux start' to include WS benchmarks${NC}"
        fi

        echo ""
        echo -e "  ${BLUE}📁 All results saved to: $BENCH_DIR/${NC}"
    fi
    echo ""
fi

echo -e     "${BLUE}╔═════════════════════╗${NC}"
if [ $EXIT_CODE -eq 0 ]; then
    echo -e "${BLUE}║  ${GREEN}All tests passed!  ${BLUE}║${NC}"
else
    echo -e "${BLUE}║  ${RED}Some tests failed! ${BLUE}║${NC}"
fi
echo -e     "${BLUE}╚═════════════════════╝${NC}"

exit $EXIT_CODE
