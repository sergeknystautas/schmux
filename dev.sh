#!/bin/bash
# schmux development mode with hot-reload
# Runs Go backend (air) and React frontend (Vite) concurrently
#
# Usage: ./dev.sh

set -euo pipefail

cd "$(dirname "$0")"

# Ensure Go binaries are in PATH (go install puts them in $HOME/go/bin)
export PATH="${GOPATH:-$HOME/go}/bin:$PATH"

# Colors for output prefixes
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Prefix functions for output
prefix_backend() {
    while IFS= read -r line; do
        echo -e "${CYAN}[backend]${NC}  $line"
    done
}

prefix_frontend() {
    while IFS= read -r line; do
        echo -e "${MAGENTA}[frontend]${NC} $line"
    done
}

# Check if a command exists
check_command() {
    command -v "$1" >/dev/null 2>&1
}

# Prompt for yes/no with default
prompt_yn() {
    local prompt="$1"
    local default="${2:-y}"

    if [[ "$default" == "y" ]]; then
        prompt="$prompt [Y/n] "
    else
        prompt="$prompt [y/N] "
    fi

    read -r -p "$prompt" response
    response="${response:-$default}"
    [[ "$response" =~ ^[Yy]$ ]]
}

echo ""
echo "======================================"
echo "  schmux development mode"
echo "======================================"
echo ""

# Check for air
AIR_BIN=""
if check_command air; then
    AIR_BIN="$(command -v air)"
elif [[ -x "${GOPATH:-$HOME/go}/bin/air" ]]; then
    AIR_BIN="${GOPATH:-$HOME/go}/bin/air"
fi

if [[ -z "$AIR_BIN" ]]; then
    echo -e "${YELLOW}air not found.${NC} Air is used for Go hot-reload."
    echo ""
    if prompt_yn "Install air via 'go install github.com/air-verse/air@latest'?"; then
        echo "Installing air..."
        go install github.com/air-verse/air@latest
        AIR_BIN="${GOPATH:-$HOME/go}/bin/air"
        echo -e "${CYAN}air installed successfully${NC}"
        echo ""
    else
        echo "Cannot continue without air. Please install it manually:"
        echo "  go install github.com/air-verse/air@latest"
        exit 1
    fi
fi

# Check for node_modules
if [[ ! -d "assets/dashboard/node_modules" ]]; then
    echo -e "${YELLOW}node_modules not found.${NC} npm dependencies are needed for the React dashboard."
    echo ""
    if prompt_yn "Run 'npm install' in assets/dashboard?"; then
        echo "Installing npm dependencies..."
        (cd assets/dashboard && npm install)
        echo -e "${MAGENTA}npm dependencies installed successfully${NC}"
        echo ""
    else
        echo "Cannot continue without npm dependencies. Please install them manually:"
        echo "  cd assets/dashboard && npm install"
        exit 1
    fi
fi

# Stop any existing daemon
if ./schmux status >/dev/null 2>&1; then
    echo "Stopping existing daemon..."
    ./schmux stop 2>/dev/null || true
    echo ""
fi

echo "Starting development servers..."
echo ""
echo -e "  ${CYAN}Backend:${NC}   air (Go hot-reload) → http://localhost:7337"
echo -e "  ${MAGENTA}Frontend:${NC}  Vite (React HMR) → proxied through backend"
echo ""
echo "Press Ctrl+C to stop both servers"
echo ""
echo "--------------------------------------"
echo ""

# Track child PIDs for cleanup
BACKEND_PID=""
FRONTEND_PID=""

cleanup() {
    echo ""
    echo "Shutting down..."

    # Kill child processes
    if [[ -n "$BACKEND_PID" ]] && kill -0 "$BACKEND_PID" 2>/dev/null; then
        kill "$BACKEND_PID" 2>/dev/null || true
    fi
    if [[ -n "$FRONTEND_PID" ]] && kill -0 "$FRONTEND_PID" 2>/dev/null; then
        kill "$FRONTEND_PID" 2>/dev/null || true
    fi

    # Wait for them to exit
    wait 2>/dev/null || true

    echo "Development servers stopped."
    exit 0
}

trap cleanup SIGINT SIGTERM

# Start Vite dev server in background
(cd assets/dashboard && npm run dev 2>&1) | prefix_frontend &
FRONTEND_PID=$!

# Give Vite a moment to start
sleep 1

# Start air (Go hot-reload) in background
(
    "$AIR_BIN" < /dev/null 2>&1
) | prefix_backend &
BACKEND_PID=$!

# Wait for either process to exit
# Note: wait -n waits for any single job; we use wait to wait for all
wait $FRONTEND_PID $BACKEND_PID 2>/dev/null || true

# If we get here, one of the processes exited unexpectedly
echo -e "${RED}One of the development servers exited unexpectedly${NC}"
cleanup
