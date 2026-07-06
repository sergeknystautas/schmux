#!/usr/bin/env bash
# Install fence (binary + its Linux runtime deps) from the latest GitHub release,
# for CI. TestFenceAnalyze_Success (internal/dashboard) spawns a real fenced pane,
# so CI needs fence installed and runnable just like a dev box does — that is the
# fence parity this gives.
# Mirrors scripts/install-sapling.sh: resolves the tag via the releases/latest
# web redirect to avoid api.github.com's unauthenticated rate limit. Unlike that
# script (which runs as root inside a Docker build), this runs directly on the
# ubuntu-latest runner as the unprivileged user, so curl/tar/ca-certificates are
# already present and /usr/local/bin needs sudo.
set -euo pipefail

# Fence's sandbox has mandatory Linux runtime deps beyond the binary itself:
# bubblewrap (sandboxing) and socat (network bridging), per fence's README.
# Without them `fence -m` aborts before the pane is alive ("bwrap/socat ... not
# found"), which surfaces as "failed to get pane PID" and is exactly what made
# TestFenceAnalyze_Success fail even with the binary installed. bpftrace is
# optional (filesystem-violation visibility under -m) and the test does not need
# it. apt-get install is a no-op if the runner already ships these.
sudo apt-get update -qq
sudo apt-get install -y --no-install-recommends socat bubblewrap

# Map system architecture to the fence asset name.
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  F_ARCH="x86_64" ;;
  aarch64) F_ARCH="arm64" ;;
  *)       echo "ERROR: unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

# Resolve the latest release tag from the releases/latest web redirect. This
# avoids api.github.com: its unauthenticated endpoint is rate-limited to 60
# requests/hour per IP, and shared GitHub Actions runner IPs routinely exhaust
# that budget (HTTP 403). Assets follow fence_<ver>_Linux_<arch>.tar.gz with the
# binary at the tarball root (confirmed against v0.1.62), so the tag alone builds
# the download URL.
REDIRECT=$(curl -fsSL -o /dev/null -w "%{url_effective}" \
  https://github.com/fencesandbox/fence/releases/latest)
F_TAG="${REDIRECT##*/}"
if [ -z "$F_TAG" ]; then
  echo "ERROR: could not resolve latest fence release tag" >&2
  exit 1
fi
F_VER="${F_TAG#v}"
F_URL="https://github.com/fencesandbox/fence/releases/download/${F_TAG}/fence_${F_VER}_Linux_${F_ARCH}.tar.gz"

echo "Installing fence ($F_ARCH, $F_TAG) from: $F_URL"
curl -fsSL -o /tmp/fence.tar.gz "$F_URL"
mkdir -p /tmp/fence-extract
tar -xzf /tmp/fence.tar.gz -C /tmp/fence-extract
sudo install -m 0755 /tmp/fence-extract/fence /usr/local/bin/fence
rm -rf /tmp/fence-extract /tmp/fence.tar.gz

fence --version
