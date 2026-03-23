#!/usr/bin/env bash
# Install sapling (sl) from the latest GitHub release.
# Used by Dockerfile.e2e-base and Dockerfile.scenarios-base.
# Supports both x64 and arm64 architectures.
# Fails hard if the install doesn't work (sl version check at the end).
set -euo pipefail

apt-get update -qq
apt-get install -y --no-install-recommends jq xz-utils ca-certificates curl
rm -rf /var/lib/apt/lists/*

# Map system architecture to sapling asset name
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  SL_ARCH="x64" ;;
  aarch64) SL_ARCH="arm64" ;;
  *)       echo "ERROR: Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

SL_URL=$(curl -fsSL https://api.github.com/repos/facebook/sapling/releases/latest \
  | jq -r ".assets[] | select(.name | test(\"linux-${SL_ARCH}\\\\.tar\\\\.xz$\")) | .browser_download_url")

if [ -z "$SL_URL" ]; then
  echo "ERROR: Could not find linux-${SL_ARCH} sapling release asset" >&2
  exit 1
fi

echo "Installing sapling ($SL_ARCH) from: $SL_URL"
curl -fsSL -o /tmp/sapling.tar.xz "$SL_URL"
mkdir -p /opt/sapling
tar -xJf /tmp/sapling.tar.xz -C /opt/sapling
ln -sf /opt/sapling/sl /usr/local/bin/sl
rm /tmp/sapling.tar.xz

sl version
