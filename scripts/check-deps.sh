#!/bin/bash
# Shared dependency checker for schmux scripts.
# Source this file and call check_deps with dependency specs.
#
# Usage:
#   source scripts/check-deps.sh
#   check_deps "go:brew:go" "node:brew:node" "tmux:brew:tmux"
#
# Dependency spec format: "command:method:package"
#   command   — the binary name to look for in PATH
#   method    — "brew" or "brew-cask"
#   package   — the Homebrew formula/cask name
#
# For npm packages in a project directory:
#   check_npm_deps "/path/to/dir-with-package.json"

check_deps() {
    local missing_cmds=()
    local brew_pkgs=()
    local brew_cask_pkgs=()
    local install_lines=()

    for dep_spec in "$@"; do
        IFS=':' read -r cmd method pkg <<< "$dep_spec"
        if ! command -v "$cmd" >/dev/null 2>&1; then
            missing_cmds+=("$cmd")
            if [[ "$method" == "brew-cask" ]]; then
                brew_cask_pkgs+=("$pkg")
                install_lines+=("  brew install --cask $pkg")
            else
                brew_pkgs+=("$pkg")
                install_lines+=("  brew install $pkg")
            fi
        fi
    done

    if [[ ${#missing_cmds[@]} -eq 0 ]]; then
        return 0
    fi

    echo ""
    echo -e "\033[1;33mMissing dependencies: ${missing_cmds[*]}\033[0m"
    echo ""

    # Need Homebrew to auto-install
    if ! command -v brew >/dev/null 2>&1; then
        echo "Homebrew is not installed. Please install it first:"
        echo '  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"'
        echo ""
        echo "Then re-run this script."
        exit 1
    fi

    echo "The following will be installed via Homebrew:"
    for line in "${install_lines[@]}"; do
        echo "$line"
    done
    echo ""

    read -p "Install now? [y/N] " answer
    if [[ "$answer" != "y" && "$answer" != "Y" ]]; then
        echo "Aborting. Please install the dependencies manually and re-run."
        exit 1
    fi

    echo ""
    for pkg in "${brew_pkgs[@]}"; do
        echo -e "\033[0;36mInstalling ${pkg}...\033[0m"
        brew install "$pkg"
    done
    for pkg in "${brew_cask_pkgs[@]}"; do
        echo -e "\033[0;36mInstalling ${pkg} (cask)...\033[0m"
        brew install --cask "$pkg"
    done

    # Post-install hint for Docker Desktop
    for pkg in "${brew_cask_pkgs[@]}"; do
        if [[ "$pkg" == "docker" ]]; then
            echo ""
            echo -e "\033[1;33mNote: Docker Desktop was installed. You may need to open it once"
            echo -e "before the 'docker' CLI is available.\033[0m"
        fi
    done

    echo ""
}

# Check that npm packages are installed in a given directory.
# If node_modules is missing or stale (package.json newer), prompt to install.
#
# Usage: check_npm_deps "/path/to/dir-with-package.json"
check_npm_deps() {
    local dir="$1"

    if [[ ! -f "$dir/package.json" ]]; then
        return 0
    fi

    # Already installed and up-to-date
    if [[ -d "$dir/node_modules" ]] && [[ "$dir/node_modules" -nt "$dir/package.json" ]]; then
        return 0
    fi

    if ! command -v npm >/dev/null 2>&1; then
        echo -e "\033[0;31mnpm is not installed. Cannot install node packages.\033[0m"
        exit 1
    fi

    local reason="missing"
    if [[ -d "$dir/node_modules" ]]; then
        reason="out of date"
    fi

    echo ""
    echo -e "\033[1;33mNode packages ${reason} in ${dir}\033[0m"
    echo ""
    echo "Will run:"
    echo "  npm install (in $dir)"
    echo ""

    read -p "Install now? [y/N] " answer
    if [[ "$answer" != "y" && "$answer" != "Y" ]]; then
        echo "Aborting. Run 'npm install' in $dir manually and re-run."
        exit 1
    fi

    echo ""
    echo -e "\033[0;36mInstalling npm packages...\033[0m"
    (cd "$dir" && npm install)
    echo ""
}
