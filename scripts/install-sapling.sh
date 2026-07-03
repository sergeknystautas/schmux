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

# Resolve the latest release tag from the releases/latest web redirect. This
# avoids api.github.com: its unauthenticated endpoint is rate-limited to 60
# requests/hour per IP, and shared GitHub Actions runner IPs routinely exhaust
# that budget (HTTP 403), which breaks the base image build. Release assets
# follow a stable naming convention (sapling-<tag>-linux-<arch>.tar.xz), so the
# tag alone is enough to construct the download URL.
REDIRECT=$(curl -fsSL -o /dev/null -w "%{url_effective}" \
  https://github.com/facebook/sapling/releases/latest)
SL_TAG="${REDIRECT##*/}"
if [ -z "$SL_TAG" ]; then
  echo "ERROR: Could not resolve latest sapling release tag" >&2
  exit 1
fi

SL_URL="https://github.com/facebook/sapling/releases/download/${SL_TAG}/sapling-${SL_TAG}-linux-${SL_ARCH}.tar.xz"

echo "Installing sapling ($SL_ARCH, $SL_TAG) from: $SL_URL"
curl -fsSL -o /tmp/sapling.tar.xz "$SL_URL"
mkdir -p /opt/sapling
tar -xJf /tmp/sapling.tar.xz -C /opt/sapling
ln -sf /opt/sapling/sl /usr/local/bin/sl
rm /tmp/sapling.tar.xz

sl version
