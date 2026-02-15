#!/usr/bin/env bash

# test.sh - Comprehensive test runner for schmux
# Usage: ./test.sh [OPTIONS]
#
# Options:
#   --unit          Run unit tests only (default)
#   --e2e           Run E2E tests only
#   --scenarios     Run scenario tests only (Playwright)
#   --bench         Run latency benchmarks only (requires tmux)
#   --all           Run unit, E2E, and scenario tests
#   --race          Run with race detector
#   --verbose       Run with verbose output
#   --coverage      Run with coverage report
#   --quick         Run without race detector or coverage (fast)
#   --force         Force rebuild Docker images (skip cache)
#   --help          Show this help message

set -e  # Exit on error
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
RUN_RACE=false
RUN_VERBOSE=false
RUN_COVERAGE=false
FORCE_BUILD=false

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
        --all)
            RUN_UNIT=true
            RUN_E2E=true
            RUN_SCENARIOS=true
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
        --help)
            echo "Usage: ./test.sh [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --unit          Run unit tests only (default)"
            echo "  --e2e           Run E2E tests only"
            echo "  --scenarios     Run scenario tests only (Playwright)"
            echo "  --bench         Run latency benchmarks only (requires tmux)"
            echo "  --all           Run unit, E2E, and scenario tests"
            echo "  --race          Run with race detector"
            echo "  --verbose       Run with verbose output"
            echo "  --coverage      Run with coverage report"
            echo "  --quick         Run without race detector or coverage (fast)"
            echo "  --force         Force rebuild Docker base images (skip cache)"
            echo "  --help          Show this help message"
            echo ""
            echo "Examples:"
            echo "  ./test.sh                    # Run unit tests"
            echo "  ./test.sh --all              # Run all tests (unit + E2E + scenarios)"
            echo "  ./test.sh --race --verbose   # Run unit tests with race detector and verbose output"
            echo "  ./test.sh --e2e              # Run E2E tests only"
            echo "  ./test.sh --coverage         # Run unit tests with coverage"
            echo "  ./test.sh --scenarios        # Run scenario tests only (Playwright)"
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
source "$SCRIPT_DIR/scripts/check-deps.sh"

DEPS=("go:brew:go")
if [ "$RUN_E2E" = true ] || [ "$RUN_SCENARIOS" = true ]; then
    DEPS+=("docker:brew-cask:docker")
fi
check_deps "${DEPS[@]}"

# Print header
echo -e "${BLUE}╔════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║${NC}  🧪 Schmux Test Suite                          ${BLUE}║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════╝${NC}"
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

build_local_artifacts() {
    if [ "$LOCAL_BUILD_DONE" = true ]; then
        return 0
    fi

    echo -e "${YELLOW}▶️  Building local artifacts for Docker tests...${NC}"
    mkdir -p build

    # Cross-compile schmux for linux/amd64
    echo -e "  ${BLUE}🔨 Cross-compiling schmux for Linux...${NC}"
    if ! GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/schmux-linux ./cmd/schmux; then
        echo -e "  ${RED}❌ Failed to cross-compile schmux${NC}"
        return 1
    fi
    echo -e "  ${GREEN}✅ Binary built: build/schmux-linux${NC}"

    # Build dashboard if scenarios are requested
    if [ "$RUN_SCENARIOS" = true ]; then
        echo -e "  ${BLUE}🎨 Building dashboard...${NC}"
        if ! go run ./cmd/build-dashboard; then
            echo -e "  ${RED}❌ Failed to build dashboard${NC}"
            return 1
        fi
        echo -e "  ${GREEN}✅ Dashboard built${NC}"
    fi

    LOCAL_BUILD_DONE=true
    echo ""
    return 0
}

# Run unit tests
if [ "$RUN_UNIT" = true ]; then
    echo -e "${YELLOW}▶️  Running unit tests...${NC}"

    # Build test command
    TEST_CMD="go test -short ./..."

    if [ "$RUN_RACE" = true ]; then
        TEST_CMD="$TEST_CMD -race"
        echo -e "  ${BLUE}🔍 Race detector enabled${NC}"
    fi

    if [ "$RUN_VERBOSE" = true ]; then
        TEST_CMD="$TEST_CMD -v"
        echo -e "  ${BLUE}📢 Verbose output enabled${NC}"
    fi

    if [ "$RUN_COVERAGE" = true ]; then
        TEST_CMD="$TEST_CMD -coverprofile=coverage.out -covermode=atomic"
        echo -e "  ${BLUE}📊 Coverage enabled${NC}"
    fi

    echo ""

    # Run tests
    if eval $TEST_CMD; then
        echo ""
        echo -e "${GREEN}✅ Unit tests passed${NC}"

        # Show coverage if enabled
        if [ "$RUN_COVERAGE" = true ]; then
            echo ""
            echo -e "${YELLOW}▶️  Coverage summary:${NC}"
            go tool cover -func=coverage.out | tail -n 1
            echo ""
            echo -e "  ${BLUE}📄 Full coverage report: coverage.out${NC}"
            echo -e "  ${BLUE}🌐 View HTML report: go tool cover -html=coverage.out${NC}"
        fi
    else
        echo ""
        echo -e "${RED}❌ Unit tests failed${NC}"
        EXIT_CODE=1
    fi

    # Run React dashboard unit tests
    echo ""
    echo -e "${YELLOW}▶️  Running React dashboard tests...${NC}"
    if (cd assets/dashboard && npm ci --silent && npx vitest run); then
        echo ""
        echo -e "${GREEN}✅ React dashboard tests passed${NC}"
    else
        echo ""
        echo -e "${RED}❌ React dashboard tests failed${NC}"
        EXIT_CODE=1
    fi

    echo ""
fi

# Run E2E tests
if [ "$RUN_E2E" = true ]; then
    echo -e "${YELLOW}▶️  Running E2E tests...${NC}"
    echo ""

    # Check if Docker is available
    if ! command -v docker &> /dev/null; then
        echo -e "${RED}❌ Docker is not installed or not in PATH${NC}"
        echo -e "  ${BLUE}💡 E2E tests require Docker${NC}"
        EXIT_CODE=1
    else
        # Build local artifacts (cross-compiled binary)
        if build_local_artifacts; then
            # Ensure base image exists
            if ensure_base_image schmux-e2e-base Dockerfile.e2e-base "E2E"; then
                # Always rebuild thin image (fast, just COPY ops)
                echo -e "  ${BLUE}🐳 Building E2E test image...${NC}"
                E2E_IMAGE_READY=false
                if [ "$RUN_VERBOSE" = true ]; then
                    if docker build -f Dockerfile.e2e -t schmux-e2e .; then
                        E2E_IMAGE_READY=true
                    fi
                else
                    if docker build -f Dockerfile.e2e -t schmux-e2e . > /dev/null 2>&1; then
                        E2E_IMAGE_READY=true
                    fi
                fi

                if [ "$E2E_IMAGE_READY" = true ]; then
                    echo -e "  ${GREEN}✅ E2E test image built${NC}"
                    echo ""
                    echo -e "  ${BLUE}🚀 Running E2E tests in container...${NC}"
                    echo ""

                    if docker run --rm schmux-e2e; then
                        echo ""
                        echo -e "${GREEN}✅ E2E tests passed${NC}"
                    else
                        echo ""
                        echo -e "${RED}❌ E2E tests failed${NC}"
                        EXIT_CODE=1
                    fi

                    # Clean up ephemeral thin image
                    docker rmi schmux-e2e > /dev/null 2>&1 || true
                else
                    echo -e "  ${RED}❌ Failed to build E2E test image${NC}"
                    EXIT_CODE=1
                fi
            else
                EXIT_CODE=1
            fi
        else
            EXIT_CODE=1
        fi
    fi
    echo ""
fi

# Run scenario tests
if [ "$RUN_SCENARIOS" = true ]; then
    echo -e "${YELLOW}▶️  Running scenario tests...${NC}"
    echo ""

    # Check if Docker is available
    if ! command -v docker &> /dev/null; then
        echo -e "${RED}❌ Docker is not installed or not in PATH${NC}"
        echo -e "  ${BLUE}💡 Scenario tests require Docker${NC}"
        EXIT_CODE=1
    else
        ARTIFACTS_DIR="test/scenarios/artifacts"
        rm -rf "$ARTIFACTS_DIR"
        mkdir -p "$ARTIFACTS_DIR"

        # Build local artifacts (cross-compiled binary + dashboard)
        if build_local_artifacts; then
            # Ensure base image exists
            if ensure_base_image schmux-scenarios-base Dockerfile.scenarios-base "Scenario"; then
                # Always rebuild thin image (fast, just COPY ops)
                echo -e "  ${BLUE}🐳 Building scenario test image...${NC}"
                SCENARIO_IMAGE_READY=false
                if [ "$RUN_VERBOSE" = true ]; then
                    if docker build -f Dockerfile.scenarios -t schmux-scenarios .; then
                        SCENARIO_IMAGE_READY=true
                    fi
                else
                    if docker build -f Dockerfile.scenarios -t schmux-scenarios . > /dev/null 2>&1; then
                        SCENARIO_IMAGE_READY=true
                    fi
                fi

                if [ "$SCENARIO_IMAGE_READY" = true ]; then
                    echo -e "  ${GREEN}✅ Scenario test image built${NC}"
                    echo ""
                    echo -e "  ${BLUE}🎭 Running Playwright scenario tests in container...${NC}"
                    echo ""

                    if docker run --rm -v "$(pwd)/$ARTIFACTS_DIR:/artifacts" schmux-scenarios; then
                        echo ""
                        echo -e "${GREEN}✅ Scenario tests passed${NC}"
                    else
                        echo ""
                        echo -e "${RED}❌ Scenario tests failed${NC}"
                        echo -e "  ${BLUE}📁 Test artifacts saved to: $ARTIFACTS_DIR/${NC}"
                        if [ -d "$ARTIFACTS_DIR/playwright-report" ]; then
                            echo -e "  ${BLUE}🌐 View HTML report: npx playwright show-report $ARTIFACTS_DIR/playwright-report${NC}"
                        fi
                        if [ -d "$ARTIFACTS_DIR/test-results" ]; then
                            echo -e "  ${BLUE}🎬 Videos/screenshots: $ARTIFACTS_DIR/test-results/${NC}"
                        fi
                        EXIT_CODE=1
                    fi

                    # Clean up ephemeral thin image
                    docker rmi schmux-scenarios > /dev/null 2>&1 || true
                else
                    echo -e "  ${RED}❌ Failed to build scenario test image${NC}"
                    EXIT_CODE=1
                fi
            else
                EXIT_CODE=1
            fi
        else
            EXIT_CODE=1
        fi
    fi
    echo ""
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
        if go test -run "TestLatency" -timeout 120s ./internal/session/ -v 2>&1 | tee "$BENCH_DIR/percentiles.log"; then
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
        if go test -run='^$' -bench "BenchmarkSendInput" -benchtime 5s -timeout 120s ./internal/session/ 2>&1 | tee "$BENCH_DIR/benchmark.log"; then
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
                    if go test -run "TestWSLatency" -timeout 120s ./internal/dashboard/ -v 2>&1 | tee "$BENCH_DIR/ws_percentiles.log"; then
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
                    if go test -run='^$' -bench "BenchmarkWSEcho" -benchtime 5s -timeout 120s ./internal/dashboard/ 2>&1 | tee "$BENCH_DIR/ws_benchmark.log"; then
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

# Print summary
echo -e "${BLUE}╔════════════════════════════════════════════════╗${NC}"
if [ $EXIT_CODE -eq 0 ]; then
    echo -e "${BLUE}║${NC}  ${GREEN}🎉 All tests passed!${NC}                          ${BLUE}║${NC}"
else
    echo -e "${BLUE}║${NC}  ${RED}💥 Some tests failed${NC}                          ${BLUE}║${NC}"
fi
echo -e "${BLUE}╚════════════════════════════════════════════════╝${NC}"

exit $EXIT_CODE
