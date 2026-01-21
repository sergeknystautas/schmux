#!/bin/bash
set -euo pipefail

# schmux installer
# Usage: curl -fsSL https://raw.githubusercontent.com/sergeknystautas/schmux/main/install.sh | bash

REPO="sergeknystautas/schmux"
GITHUB_API="https://api.github.com/repos/${REPO}/releases/latest"
GITHUB_RELEASES="https://github.com/${REPO}/releases/download"

# Colors for output (disable if not a terminal)
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    NC='\033[0m' # No Color
else
    RED=''
    GREEN=''
    YELLOW=''
    NC=''
fi

error() {
    echo -e "${RED}Error: $1${NC}" >&2
    exit 1
}

info() {
    echo -e "${GREEN}$1${NC}"
}

warn() {
    echo -e "${YELLOW}$1${NC}"
}

# Check for required commands
command -v curl >/dev/null 2>&1 || error "curl is required but not installed"
command -v tar >/dev/null 2>&1 || error "tar is required but not installed"

# Detect platform
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) error "Unsupported architecture: $ARCH (supported: x86_64, arm64)" ;;
esac

case "$OS" in
    darwin|linux) ;;
    *) error "Unsupported OS: $OS (supported: macOS, Linux)" ;;
esac

# Get latest version - try jq first, fall back to grep/sed
info "Fetching latest version..."

RELEASE_JSON=$(curl -fsSL --max-time 30 "$GITHUB_API") || error "Failed to fetch release info from GitHub"

if command -v jq >/dev/null 2>&1; then
    LATEST=$(echo "$RELEASE_JSON" | jq -r '.tag_name // empty' | sed 's/^v//')
else
    LATEST=$(echo "$RELEASE_JSON" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"v\{0,1\}\([^"]*\)".*/\1/')
fi

if [ -z "$LATEST" ]; then
    error "Could not determine latest version"
fi

info "Installing schmux v${LATEST} for ${OS}/${ARCH}..."

# Create temp directory with proper cleanup
TMP_DIR=$(mktemp -d)
cleanup() {
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

# Download checksums first
CHECKSUMS_URL="${GITHUB_RELEASES}/v${LATEST}/checksums.txt"
info "Downloading checksums..."
curl -fsSL --max-time 30 -o "${TMP_DIR}/checksums.txt" "$CHECKSUMS_URL" || error "Failed to download checksums"

# Download binary
BINARY_NAME="schmux-${OS}-${ARCH}"
BINARY_URL="${GITHUB_RELEASES}/v${LATEST}/${BINARY_NAME}"
info "Downloading binary..."
curl -fsSL --max-time 120 -o "${TMP_DIR}/schmux" "$BINARY_URL" || error "Failed to download binary"

# Verify binary checksum
EXPECTED_HASH=$(grep "${BINARY_NAME}$" "${TMP_DIR}/checksums.txt" | awk '{print $1}')
if [ -z "$EXPECTED_HASH" ]; then
    error "No checksum found for ${BINARY_NAME}"
fi

if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL_HASH=$(sha256sum "${TMP_DIR}/schmux" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
    ACTUAL_HASH=$(shasum -a 256 "${TMP_DIR}/schmux" | awk '{print $1}')
else
    warn "Neither sha256sum nor shasum found - skipping checksum verification"
    ACTUAL_HASH="$EXPECTED_HASH"
fi

if [ "$ACTUAL_HASH" != "$EXPECTED_HASH" ]; then
    error "Checksum mismatch for binary: expected ${EXPECTED_HASH}, got ${ACTUAL_HASH}"
fi
info "Binary checksum verified."

# Download dashboard assets
ASSETS_URL="${GITHUB_RELEASES}/v${LATEST}/dashboard-assets.tar.gz"
info "Downloading dashboard assets..."
curl -fsSL --max-time 120 -o "${TMP_DIR}/dashboard-assets.tar.gz" "$ASSETS_URL" || error "Failed to download dashboard assets"

# Verify assets checksum
EXPECTED_ASSETS_HASH=$(grep "dashboard-assets.tar.gz$" "${TMP_DIR}/checksums.txt" | awk '{print $1}')
if [ -z "$EXPECTED_ASSETS_HASH" ]; then
    error "No checksum found for dashboard-assets.tar.gz"
fi

if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL_ASSETS_HASH=$(sha256sum "${TMP_DIR}/dashboard-assets.tar.gz" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
    ACTUAL_ASSETS_HASH=$(shasum -a 256 "${TMP_DIR}/dashboard-assets.tar.gz" | awk '{print $1}')
else
    ACTUAL_ASSETS_HASH="$EXPECTED_ASSETS_HASH"
fi

if [ "$ACTUAL_ASSETS_HASH" != "$EXPECTED_ASSETS_HASH" ]; then
    error "Checksum mismatch for assets: expected ${EXPECTED_ASSETS_HASH}, got ${ACTUAL_ASSETS_HASH}"
fi
info "Assets checksum verified."

# Install binary
chmod +x "${TMP_DIR}/schmux"
INSTALL_DIR="${HOME}/.local/bin"
mkdir -p "$INSTALL_DIR"
mv "${TMP_DIR}/schmux" "$INSTALL_DIR/schmux"

# Install dashboard assets
ASSETS_DIR="${HOME}/.schmux/dashboard"
mkdir -p "$ASSETS_DIR"
rm -rf "$ASSETS_DIR"/*
tar -xzf "${TMP_DIR}/dashboard-assets.tar.gz" -C "$ASSETS_DIR"

# Write version marker
echo "$LATEST" > "${ASSETS_DIR}/.version"

echo ""
info "Successfully installed schmux v${LATEST}!"
echo ""
echo "Binary: ${INSTALL_DIR}/schmux"
echo "Assets: ${ASSETS_DIR}"
echo ""

# Check if install dir is in PATH
case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
        warn "Add ${INSTALL_DIR} to your PATH:"
        echo ""
        echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
        echo ""
        echo "Add this line to your ~/.bashrc, ~/.zshrc, or shell config file."
        echo ""
        ;;
esac

echo "Get started:"
echo "  schmux start    # Start the daemon"
echo "  schmux status   # Check status and get dashboard URL"
