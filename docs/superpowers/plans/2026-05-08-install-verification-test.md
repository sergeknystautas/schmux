# Install Verification Test Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A standalone Docker-based test that verifies the schmux installer works against real GitHub releases, plus a CI workflow that runs it automatically after each release.

**Architecture:** A minimal Debian Docker container curls the real installer from GitHub, runs it, and verifies the binary, dashboard assets, and daemon all work. A root-level script (`release/test-install.sh`) handles Docker build/run. A GitHub Actions workflow triggers it after releases.

**Tech Stack:** Docker, bash, GitHub Actions `workflow_run` trigger

---

### Task 1: Move `docs/release-strategy.md` to `release/`

**Files:**

- Move: `docs/release-strategy.md` → `release/release-strategy.md`
- Modify: `docs/README.md:17`

- [ ] **Step 1: Create the `release/` directory and move the file**

```bash
mkdir -p release
git mv docs/release-strategy.md release/release-strategy.md
```

- [ ] **Step 2: Update the link in `docs/README.md`**

Change line 17 from:

```markdown
| [release-strategy.md](release-strategy.md) | Release process |
```

to:

```markdown
| [release-strategy.md](../release/release-strategy.md) | Release process |
```

- [ ] **Step 3: Verify no other files reference `docs/release-strategy.md`**

```bash
grep -rn "docs/release-strategy" . --include="*.md" --include="*.go" --include="*.yml" | grep -v .git/
```

Expected: no output.

---

### Task 2: Create `release/Dockerfile.install-test`

**Files:**

- Create: `release/Dockerfile.install-test`

- [ ] **Step 1: Write the Dockerfile**

```dockerfile
# Install Test Image for schmux
# Simulates a fresh user machine — no Go, no Node, just standard CLI tools.
# Verifies the real GitHub installer works end-to-end.
#
# Build: docker build -f release/Dockerfile.install-test -t schmux-install-test .
# Run:   docker run --rm schmux-install-test

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    tar \
    tmux \
    bash \
    git \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -u 1000 testuser
USER testuser
ENV PATH="/home/testuser/.local/bin:${PATH}"

COPY release/verify-install.sh /home/testuser/verify-install.sh

ENTRYPOINT ["bash", "/home/testuser/verify-install.sh"]
```

- [ ] **Step 2: Verify it parses**

```bash
docker build --check -f release/Dockerfile.install-test .
```

Expected: no syntax errors. (If `--check` is not supported on the local Docker version, skip this step — the full build in Task 4 will catch errors.)

---

### Task 3: Create `release/verify-install.sh`

This is the script that runs inside the container.

**Files:**

- Create: `release/verify-install.sh`

- [ ] **Step 1: Write the verification script**

```bash
#!/bin/bash
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

pass() { echo -e "${GREEN}PASS: $1${NC}"; }
fail() { echo -e "${RED}FAIL: $1${NC}"; exit 1; }

echo "=== Step 1: Run installer from GitHub ==="
curl -fsSL https://raw.githubusercontent.com/sergeknystautas/schmux/main/install.sh | bash \
    || fail "installer exited non-zero"
pass "installer completed"

echo ""
echo "=== Step 2: Verify schmux --version ==="
VERSION_OUTPUT=$(schmux --version 2>&1) || fail "schmux --version exited non-zero"
echo "$VERSION_OUTPUT"
echo "$VERSION_OUTPUT" | grep -qE 'v?[0-9]+\.[0-9]+\.[0-9]+' \
    || fail "version output does not contain a version string"
pass "schmux --version"

echo ""
echo "=== Step 3: Verify dashboard assets ==="
[ -f "$HOME/.schmux/dashboard/index.html" ] \
    || fail "~/.schmux/dashboard/index.html not found"
pass "dashboard assets installed"

echo ""
echo "=== Step 4: Verify daemon starts ==="
schmux start || fail "schmux start exited non-zero"
pass "schmux start"

echo ""
echo "=== Step 5: Verify daemon status ==="
schmux status || fail "schmux status exited non-zero"
pass "schmux status"

echo ""
echo "=== Step 6: Stop daemon ==="
schmux stop || fail "schmux stop exited non-zero"
pass "schmux stop"

echo ""
echo -e "${GREEN}All checks passed.${NC}"
```

- [ ] **Step 2: Make it executable**

```bash
chmod +x release/verify-install.sh
```

---

### Task 4: Create `release/test-install.sh`

**Files:**

- Create: `release/test-install.sh`

- [ ] **Step 1: Write the test runner script**

```bash
#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "Building install test image..."
docker build -f "$REPO_ROOT/release/Dockerfile.install-test" -t schmux-install-test "$REPO_ROOT"

echo "Running install test..."
docker run --rm schmux-install-test
```

- [ ] **Step 2: Make it executable**

```bash
chmod +x release/test-install.sh
```

- [ ] **Step 3: Run it**

```bash
./release/test-install.sh
```

Expected: The container builds, curls the installer from GitHub, installs schmux, and all six verification steps pass. If the latest release has no assets (as is currently the case with v1.2.1), the test will fail at Step 1 with the installer's "Failed to download checksums" error — this is the correct behavior, confirming the test catches the broken release.

---

### Task 5: Create `.github/workflows/verify-release.yml`

**Files:**

- Create: `.github/workflows/verify-release.yml`

- [ ] **Step 1: Write the workflow**

```yaml
name: Verify Release

on:
  workflow_run:
    workflows: ['Release']
    types:
      - completed

permissions:
  contents: read

jobs:
  verify-install:
    name: Verify Install
    runs-on: ubuntu-latest
    if: ${{ github.event.workflow_run.conclusion == 'success' }}
    steps:
      - uses: actions/checkout@v4

      - name: Run install test
        run: ./release/test-install.sh
```

- [ ] **Step 2: Verify the workflow name matches**

The `workflows: ["Release"]` filter must match the `name:` field in `.github/workflows/release.yml`.

```bash
head -1 .github/workflows/release.yml
```

Expected: `name: Release`
