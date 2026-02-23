#!/bin/bash
set -e
# Start SSH server
/usr/sbin/sshd
# Wait for SSH to be ready
sleep 1
# Build test command with optional -run filter
TEST_CMD="/home/e2e/e2e-test -test.v"
if [ -n "${TEST_RUN:-}" ]; then
    TEST_CMD="$TEST_CMD -test.run \"$TEST_RUN\""
fi
if [ -n "${TEST_COUNT:-}" ]; then
    TEST_CMD="$TEST_CMD -test.count $TEST_COUNT"
fi
# Run tests from internal/e2e dir (matching go test cwd for relative paths)
cd /home/e2e/src/internal/e2e
su -c "$TEST_CMD" e2e
