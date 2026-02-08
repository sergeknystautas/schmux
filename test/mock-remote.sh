#!/bin/bash
# Mock remote connection for E2E testing
# Simulates remote host by printing fake provisioning output, then executes the command passed to it

set -e

# Simulate provisioning output on stderr (like real remote connections do)
echo "Connecting to remote environment..." >&2
echo "Reserving instance from pool..." >&2
sleep 0.5  # Brief delay to simulate provisioning

# Output hostname (parsed by connection manager)
echo "Establish ControlMaster connection to mock-test-host-$RANDOM.example.com" >&2

# Output UUID (parsed by connection manager)
echo "OD uuid: mock-uuid-$(date +%s)" >&2

# Signal ready
echo "** tmux mode started **" >&2

# Skip the "--" separator if present (first arg)
# The command template passes: <script> -- tmux -CC new-session...
# (No flavor arg is passed to the script - flavor is only in template variables)
if [ "$1" = "--" ]; then
    shift  # Skip --
fi

# Execute the tmux command passed (which will be tmux -CC new-session...)
exec "$@"
