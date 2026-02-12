#!/bin/bash
# schmux development mode with workspace switching
#
# Loop-based wrapper that:
# 1. Builds the Go binary
# 2. Starts Vite for frontend HMR
# 3. Runs the daemon with --dev-mode
# 4. On exit code 42: reads restart manifest, rebuilds/restarts as needed
# 5. On any other exit code: cleanup and exit
#
# Usage: ./dev.sh

set -euo pipefail

cd "$(dirname "$0")"
SCRIPT_DIR="$(pwd)"

# Colors for output
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

SCHMUX_DIR="${HOME}/.schmux"
DEV_STATE_FILE="${SCHMUX_DIR}/dev-state.json"
DEV_RESTART_FILE="${SCHMUX_DIR}/dev-restart.json"
DEV_BUILD_STATUS_FILE="${SCHMUX_DIR}/dev-build-status.json"
BINARY="./tmp/schmux"
VITE_PID=""

# Read a JSON field using python3 (macOS built-in) with jq fallback
json_field() {
    local file="$1"
    local field="$2"
    if command -v jq >/dev/null 2>&1; then
        jq -r ".$field // empty" "$file" 2>/dev/null
    else
        python3 -c "import json,sys; d=json.load(open('$file')); print(d.get('$field',''))" 2>/dev/null
    fi
}

cleanup() {
    echo ""
    echo -e "${CYAN}[dev]${NC} Shutting down..."

    # Kill Vite
    if [[ -n "$VITE_PID" ]] && kill -0 "$VITE_PID" 2>/dev/null; then
        kill "$VITE_PID" 2>/dev/null || true
        wait "$VITE_PID" 2>/dev/null || true
    fi

    # Clean up dev mode temp files
    rm -f "$DEV_STATE_FILE" "$DEV_RESTART_FILE" "$DEV_BUILD_STATUS_FILE"

    echo -e "${CYAN}[dev]${NC} Development mode stopped."
    exit 0
}

trap cleanup SIGINT SIGTERM

# Check for node_modules
ensure_npm_deps() {
    local dashboard_dir="$1/assets/dashboard"
    if [[ ! -d "$dashboard_dir/node_modules" ]]; then
        echo -e "${YELLOW}[dev]${NC} Installing npm dependencies in ${dashboard_dir}..."
        (cd "$dashboard_dir" && npm install --silent)
    fi
}

# Kill any process listening on a given port
kill_port() {
    local port="$1"
    local pids
    pids=$(lsof -ti :"$port" 2>/dev/null || true)
    if [[ -n "$pids" ]]; then
        echo -e "${YELLOW}[dev]${NC} Killing processes on port ${port}: ${pids}"
        echo "$pids" | xargs kill 2>/dev/null || true
        # Wait for port to be freed
        for i in $(seq 1 30); do
            if ! lsof -ti :"$port" >/dev/null 2>&1; then
                return 0
            fi
            sleep 0.1
        done
        # Force kill if still alive
        pids=$(lsof -ti :"$port" 2>/dev/null || true)
        if [[ -n "$pids" ]]; then
            echo "$pids" | xargs kill -9 2>/dev/null || true
            sleep 0.2
        fi
    fi
}

# Start Vite dev server from a workspace path
start_vite() {
    local workspace_path="$1"
    local dashboard_dir="${workspace_path}/assets/dashboard"

    # Kill existing Vite if running
    if [[ -n "$VITE_PID" ]] && kill -0 "$VITE_PID" 2>/dev/null; then
        kill "$VITE_PID" 2>/dev/null || true
        wait "$VITE_PID" 2>/dev/null || true
        VITE_PID=""
    fi

    # Ensure port 5173 is free (catches orphaned node processes)
    kill_port 5173

    ensure_npm_deps "$workspace_path"

    echo -e "${MAGENTA}[frontend]${NC} Starting Vite from ${dashboard_dir}"
    (cd "$dashboard_dir" && npx vite --port 5173 --strictPort 2>&1 | while IFS= read -r line; do
        echo -e "${MAGENTA}[frontend]${NC} $line"
    done) &
    VITE_PID=$!
}

# Build Go binary from a workspace path
build_binary() {
    local workspace_path="$1"
    local timestamp
    timestamp="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

    echo -e "${CYAN}[dev]${NC} Building from ${workspace_path}..."

    mkdir -p "$(dirname "$BINARY")"

    if (cd "$workspace_path" && go build -o "${SCRIPT_DIR}/${BINARY}" ./cmd/schmux) 2>&1; then
        echo -e "${GREEN}[dev]${NC} Build succeeded"
        # Write build status
        cat > "$DEV_BUILD_STATUS_FILE" <<BEOF
{"success":true,"workspace_path":"${workspace_path}","error":"","at":"${timestamp}"}
BEOF
        return 0
    else
        local err_msg="build failed"
        echo -e "${RED}[dev]${NC} Build failed from ${workspace_path}"
        cat > "$DEV_BUILD_STATUS_FILE" <<BEOF
{"success":false,"workspace_path":"${workspace_path}","error":"${err_msg}","at":"${timestamp}"}
BEOF
        return 1
    fi
}

echo ""
echo "======================================"
echo "  schmux development mode"
echo "======================================"
echo ""

# Stop any existing production daemon
if [[ -f "${SCHMUX_DIR}/daemon.pid" ]]; then
    pid=$(cat "${SCHMUX_DIR}/daemon.pid" 2>/dev/null | tr -d '[:space:]')
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
        echo -e "${YELLOW}[dev]${NC} Stopping existing daemon (PID ${pid})..."
        kill "$pid" 2>/dev/null || true
        # Wait for it to stop
        for i in $(seq 1 50); do
            if ! kill -0 "$pid" 2>/dev/null; then
                break
            fi
            sleep 0.1
        done
    fi
fi

# Current workspace is where this script lives
CURRENT_WORKSPACE="$SCRIPT_DIR"

# Initial build
if ! build_binary "$CURRENT_WORKSPACE"; then
    echo -e "${RED}[dev]${NC} Initial build failed. Fix the errors and try again."
    exit 1
fi

# Start Vite from current workspace
start_vite "$CURRENT_WORKSPACE"

# Give Vite a moment to start
sleep 2

echo ""
echo -e "  ${CYAN}Backend:${NC}   ${BINARY} (loop-based restart)"
echo -e "  ${MAGENTA}Frontend:${NC}  Vite (React HMR) → proxied through backend"
echo -e "  ${GREEN}Dashboard:${NC} http://localhost:7337"
echo ""
echo "Press Ctrl+C to stop"
echo ""
echo "--------------------------------------"
echo ""

# Main loop
while true; do
    # Write dev state
    cat > "$DEV_STATE_FILE" <<SEOF
{"source_workspace":"${CURRENT_WORKSPACE}"}
SEOF

    # Run the daemon
    set +e
    "$BINARY" daemon-run --dev-mode 2>&1 | while IFS= read -r line; do
        echo -e "${CYAN}[backend]${NC}  $line"
    done
    # Capture exit code from the daemon (first process in the pipeline)
    EXIT_CODE=${PIPESTATUS[0]}
    set -e

    if [[ "$EXIT_CODE" -eq 42 ]]; then
        echo ""
        echo -e "${CYAN}[dev]${NC} Dev restart requested (exit code 42)"

        # Read restart manifest
        if [[ ! -f "$DEV_RESTART_FILE" ]]; then
            echo -e "${RED}[dev]${NC} No restart manifest found, restarting with current binary"
            continue
        fi

        RESTART_TYPE=$(json_field "$DEV_RESTART_FILE" "type")
        RESTART_WORKSPACE=$(json_field "$DEV_RESTART_FILE" "workspace_path")

        if [[ -z "$RESTART_WORKSPACE" ]]; then
            echo -e "${RED}[dev]${NC} Invalid restart manifest, restarting with current binary"
            rm -f "$DEV_RESTART_FILE"
            continue
        fi

        echo -e "${CYAN}[dev]${NC} Switching to workspace: ${RESTART_WORKSPACE} (type: ${RESTART_TYPE})"

        # Handle backend rebuild
        if [[ "$RESTART_TYPE" == "backend" || "$RESTART_TYPE" == "both" ]]; then
            if build_binary "$RESTART_WORKSPACE"; then
                CURRENT_WORKSPACE="$RESTART_WORKSPACE"
            else
                echo -e "${YELLOW}[dev]${NC} Build failed, restarting with previous binary"
            fi
        fi

        # Handle frontend switch
        if [[ "$RESTART_TYPE" == "frontend" || "$RESTART_TYPE" == "both" ]]; then
            CURRENT_WORKSPACE="$RESTART_WORKSPACE"
            start_vite "$RESTART_WORKSPACE"
            sleep 1
        fi

        # Clean up manifest
        rm -f "$DEV_RESTART_FILE"

        echo -e "${CYAN}[dev]${NC} Restarting daemon..."
        echo ""
    else
        echo ""
        echo -e "${CYAN}[dev]${NC} Daemon exited with code ${EXIT_CODE}"
        cleanup
    fi
done
