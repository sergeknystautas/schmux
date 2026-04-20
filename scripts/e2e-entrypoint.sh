#!/bin/bash
set -e
# Start SSH server
/usr/sbin/sshd
# Wait for SSH to be ready
sleep 1
# Build test command with optional -run filter.
# Use a generous test timeout — under CPU/IO contention from parallel Docker
# containers, individual tests (especially dispose-all) can take several
# minutes. The default 10m timeout is too short for worst-case runs.
# Cap test parallelism: each test starts a full daemon (3 fsnotify watchers)
# plus 1 watcher per session. The host kernel's fs.inotify.max_user_instances
# defaults to 128 and is *shared* across all containers running as the same
# UID, so 3 parallel docker repeats × parallelism × 5 watchers must stay under
# 128. Hard cap at 6 per container (3 × 6 × ~5 = 90, comfortable headroom).
# Profiled on 2-CPU Docker: p=4 → 144s/0 flaky, p=48 → 29s/1 flaky.
DEFAULT_PARALLEL=$(( $(nproc) * 2 ))
if [ "$DEFAULT_PARALLEL" -gt 6 ]; then
    DEFAULT_PARALLEL=6
fi
PARALLEL="${TEST_PARALLEL:-$DEFAULT_PARALLEL}"
TEST_CMD="/home/e2e/e2e-test -test.v -test.timeout 30m -test.parallel $PARALLEL"
if [ -n "${TEST_RUN:-}" ]; then
    TEST_CMD="$TEST_CMD -test.run \"$TEST_RUN\""
fi
if [ -n "${TEST_COUNT:-}" ]; then
    TEST_CMD="$TEST_CMD -test.count $TEST_COUNT"
fi
# Run tests from internal/e2e dir (matching go test cwd for relative paths)
cd /home/e2e/src/internal/e2e
su -c "$TEST_CMD" e2e
