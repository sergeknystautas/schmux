#!/bin/bash
set -e

# Git requires user config for schmux repo operations
git config --global user.email "test@schmux.dev"
git config --global user.name "Schmux Test"

# Create minimal config so the daemon can start
mkdir -p ~/.schmux
cat > ~/.schmux/config.json <<'CONFIG'
{
  "workspace_path": "/tmp/schmux-test-workspaces",
  "source_code_management": "git",
  "repos": [],
  "run_targets": [],
  "terminal": {
    "width": 120,
    "height": 40,
    "seed_lines": 100
  }
}
CONFIG
mkdir -p /tmp/schmux-test-workspaces

# Start the schmux daemon in the background
cd /app
schmux daemon-run &
DAEMON_PID=$!

# Wait for the daemon to become healthy
echo "Waiting for schmux daemon to become healthy..."
for i in $(seq 1 30); do
    if curl -sf http://localhost:7337/api/healthz > /dev/null 2>&1; then
        echo "Daemon is healthy (attempt $i)"
        break
    fi
    if [ "$i" -eq 30 ]; then
        echo "ERROR: Daemon failed to become healthy after 30 attempts"
        kill $DAEMON_PID 2>/dev/null || true
        exit 1
    fi
    sleep 1
done

# Run Playwright scenario tests
cd /app/test/scenarios/generated
npx playwright test
TEST_EXIT=$?

# Clean up
kill $DAEMON_PID 2>/dev/null || true
exit $TEST_EXIT
