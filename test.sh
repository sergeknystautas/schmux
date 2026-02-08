#!/usr/bin/env bash

# test.sh - Comprehensive test runner for schmux
# Usage: ./test.sh [OPTIONS]
#
# Options:
#   --unit          Run unit tests only (default)
#   --e2e           Run E2E tests only
#   --all           Run both unit and E2E tests
#   --race          Run with race detector
#   --verbose       Run with verbose output
#   --coverage      Run with coverage report
#   --quick         Run without race detector or coverage (fast)
#   --help          Show this help message

set -e  # Exit on error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default options
RUN_UNIT=true
RUN_E2E=false
RUN_RACE=false
RUN_VERBOSE=false
RUN_COVERAGE=false

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --unit)
            RUN_UNIT=true
            RUN_E2E=false
            shift
            ;;
        --e2e)
            RUN_UNIT=false
            RUN_E2E=true
            shift
            ;;
        --all)
            RUN_UNIT=true
            RUN_E2E=true
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
        --help)
            echo "Usage: ./test.sh [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --unit          Run unit tests only (default)"
            echo "  --e2e           Run E2E tests only"
            echo "  --all           Run both unit and E2E tests"
            echo "  --race          Run with race detector"
            echo "  --verbose       Run with verbose output"
            echo "  --coverage      Run with coverage report"
            echo "  --quick         Run without race detector or coverage (fast)"
            echo "  --help          Show this help message"
            echo ""
            echo "Examples:"
            echo "  ./test.sh                    # Run unit tests"
            echo "  ./test.sh --all              # Run all tests (unit + E2E)"
            echo "  ./test.sh --race --verbose   # Run unit tests with race detector and verbose output"
            echo "  ./test.sh --e2e              # Run E2E tests only"
            echo "  ./test.sh --coverage         # Run unit tests with coverage"
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            echo "Run './test.sh --help' for usage information"
            exit 1
            ;;
    esac
done

# Print header
echo -e "${BLUE}╔════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║${NC}  🧪 Schmux Test Suite                           ${BLUE}║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════╝${NC}"
echo ""

# Track overall status
EXIT_CODE=0

# Run unit tests
if [ "$RUN_UNIT" = true ]; then
    echo -e "${YELLOW}▶️  Running unit tests...${NC}"
    echo ""

    # Build test command
    TEST_CMD="go test ./..."

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
        echo -e "  ${BLUE}🐳 Building E2E Docker image...${NC}"
        if docker build -f Dockerfile.e2e -t schmux-e2e . > /dev/null 2>&1; then
            echo -e "  ${GREEN}✅ Docker image built${NC}"
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
        else
            echo -e "  ${RED}❌ Failed to build Docker image${NC}"
            EXIT_CODE=1
        fi
    fi
    echo ""
fi

# Print summary
echo -e "${BLUE}╔════════════════════════════════════════════════╗${NC}"
if [ $EXIT_CODE -eq 0 ]; then
    echo -e "${BLUE}║${NC}  ${GREEN}🎉 All tests passed!${NC}                          ${BLUE}║${NC}"
else
    echo -e "${BLUE}║${NC}  ${RED}💥 Some tests failed${NC}                         ${BLUE}║${NC}"
fi
echo -e "${BLUE}╚════════════════════════════════════════════════╝${NC}"

exit $EXIT_CODE
