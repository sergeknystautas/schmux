# justfile — schmux developer commands
#
# Requires: nix develop shell (via direnv or `just shell`)
# Run `just` to see all available recipes.

# Show available commands
default:
    @just --list

# --- Nix / Environment ---

# Enter dev shell (if not using direnv)
shell:
    nix develop

# Update nixpkgs pin (may change tool patch versions)
update:
    nix flake update

# Check flake evaluates cleanly
check:
    nix flake check

# Upgrade flake.nix to latest cautomaton-develops template
upgrade:
    #!/usr/bin/env bash
    set -euo pipefail
    url="https://raw.githubusercontent.com/farra/cautomaton-develops/main/template/flake.nix"
    tmp=$(mktemp)
    curl -sL "$url" -o "$tmp"
    if diff -q flake.nix "$tmp" > /dev/null 2>&1; then
        echo "flake.nix is already up to date"
        rm "$tmp"
        exit 0
    fi
    echo "Changes available:"
    echo "=================="
    diff --color=auto -u flake.nix "$tmp" || true
    echo ""
    read -p "Apply update? [y/N] " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        mv "$tmp" flake.nix
        echo "Updated flake.nix"
        echo "Run 'nix flake update' to rebuild with new template"
    else
        rm "$tmp"
        echo "Cancelled"
    fi

# --- Development ---

# First-time setup: check environment, create default config if missing
init:
    #!/usr/bin/env bash
    set -uo pipefail
    red='\033[0;31m' green='\033[0;32m' yellow='\033[0;33m' cyan='\033[0;36m' nc='\033[0m'

    echo ""
    echo "======================================"
    echo "  schmux developer setup"
    echo "======================================"
    echo ""

    # --- Check environment ---
    echo -e "${cyan}Checking environment...${nc}"
    echo ""

    if [ -n "${IN_NIX_SHELL:-}" ]; then
        echo -e "  ${green}nix develop shell active${nc}"
    else
        echo -e "  ${yellow}Not in a nix develop shell${nc}"
        echo -e "  ${yellow}  Run 'just shell' or set up direnv for the full tool set${nc}"
        echo ""
        echo -e "  ${cyan}Checking for tools individually...${nc}"
        echo ""

        # Map deps.toml tool names to binary names
        # (nodejs->node, python3->python3, everything else is identity)
        tools=("go:go" "node:nodejs" "tmux:tmux" "jq:jq" "curl:curl" "python3:python3" "git:git" "lsof:lsof")
        missing=()

        get_version() {
            case "$1" in
                curl)    curl --version 2>/dev/null | head -1 | cut -d' ' -f1-2 ;;
                lsof)    lsof -v 2>&1 | grep -i revision | head -1 | sed 's/.*revision: *//' || echo "found" ;;
                *)       ("$1" --version 2>/dev/null || "$1" -V 2>/dev/null || echo "found") | head -1 ;;
            esac
        }

        for entry in "${tools[@]}"; do
            bin="${entry%%:*}"
            label="${entry##*:}"
            if command -v "$bin" >/dev/null 2>&1; then
                version=$(get_version "$bin")
                echo -e "  ${green}$bin${nc} ($version)"
            else
                echo -e "  ${red}$bin${nc} — not found (deps.toml: $label)"
                missing+=("$bin")
            fi
        done

        if [ ${#missing[@]} -gt 0 ]; then
            echo ""
            echo -e "  ${yellow}Missing tools: ${missing[*]}${nc}"
            echo -e "  ${yellow}Install them manually or use 'just shell' for the nix environment${nc}"
        fi
        echo ""
    fi

    # --- Ensure config ---
    echo -e "${cyan}Checking schmux config...${nc}"
    echo ""

    config="$HOME/.schmux/config.json"
    if [ -f "$config" ]; then
        echo -e "  ${green}Config exists${nc} at $config"
    else
        mkdir -p "$HOME/.schmux"
        echo '{"workspace_path":"","repos":[],"run_targets":[],"quick_launch":[]}' > "$config"
        echo -e "  ${green}Created default config${nc} at $config"
        echo -e "  Finish setup in the dashboard at http://localhost:7337/config"
    fi

    echo ""
    echo -e "${green}Ready.${nc} Run ${cyan}just dev${nc} to start developing."
    echo ""

# Hot-reload dev mode (Go backend + Vite HMR frontend)
dev:
    ./dev.sh

# Build the Go binary (outputs ./schmux)
build:
    go build -o schmux ./cmd/schmux

# Build the React dashboard (NEVER use npm directly)
dashboard:
    go run ./cmd/build-dashboard

# Regenerate TypeScript types from Go API contracts
gen-types:
    go run ./cmd/gen-types

# Start Vite dev server standalone (frontend-only work)
vite:
    cd assets/dashboard && npx vite --port 5173 --strictPort

# Install dashboard npm dependencies
npm-install:
    cd assets/dashboard && npm install

# --- Build & Run ---

# Build everything and start the daemon
run:
    ./run.sh

# Start the daemon (binary must already exist)
start:
    ./schmux start

# Stop the daemon
stop:
    ./schmux stop

# Show daemon status and dashboard URL
status:
    ./schmux status

# Run daemon in foreground (for debugging)
daemon-run:
    ./schmux daemon-run

# --- Testing ---

# Run Go unit tests (default; use test-all for full suite)
test:
    ./test.sh

# Run all tests (unit + E2E + scenarios)
test-all:
    ./test.sh --all

# Run unit tests with race detector
test-race:
    ./test.sh --race

# Run unit tests with coverage report
test-coverage:
    ./test.sh --coverage

# Run E2E tests (requires Docker)
test-e2e:
    ./test.sh --e2e

# Run scenario tests (Playwright, requires Docker)
test-scenarios:
    ./test.sh --scenarios

# Run React dashboard tests only
test-react:
    ./test.sh --react

# Run latency benchmarks (requires tmux)
test-bench:
    ./test.sh --bench

# Run a specific test by pattern
test-run pattern:
    ./test.sh --run '{{pattern}}'

# --- Code Quality ---

# Format all code (Go + TS/JS/CSS/MD/JSON)
format:
    ./format.sh

# Format Go files only
format-go:
    gofmt -w $(find . -name '*.go' -not -path './vendor/*')

# Check that API docs are up to date
check-api-docs:
    ./scripts/check-api-docs.sh

# Typecheck the React dashboard
typecheck:
    cd assets/dashboard && npx tsc --noEmit

# --- Docker / Remote ---

# Start an SSH-enabled Docker container for remote workspace testing
docker-ssh:
    ./scripts/start-docker.sh
