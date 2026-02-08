#!/bin/bash
# start-docker.sh - Start an SSH-enabled Docker container for testing schmux remote workspaces
# Usage: ./scripts/start-docker.sh
#
# This script:
#   1. Builds and starts an SSH-enabled Docker container (schmux-ssh)
#   2. Sets up SSH key-based auth so schmux daemon can connect non-interactively
#   3. Appends a remote_flavors entry to ~/.schmux/config.json

set -euo pipefail

CONTAINER_NAME="schmux-ssh"
SSH_PORT=2222
SSH_KEY_PATH="$HOME/.schmux/docker-ssh-key"
CONFIG_PATH="$HOME/.schmux/config.json"
FLAVOR_ID="docker_ssh"

# --- Prerequisites ---

check_prereqs() {
    local missing=()
    command -v docker >/dev/null 2>&1 || missing+=("docker")
    command -v jq >/dev/null 2>&1 || missing+=("jq")

    if [ ${#missing[@]} -gt 0 ]; then
        echo "ERROR: Missing required tools: ${missing[*]}"
        echo "Install them and try again."
        exit 1
    fi

    if [ ! -f "$CONFIG_PATH" ]; then
        echo "ERROR: Config file not found at $CONFIG_PATH"
        echo "Run 'schmux init' first to create a config."
        exit 1
    fi
}

# --- Container setup ---

start_container() {
    # Always start fresh — remove any existing container
    if docker inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        echo "Removing existing container '$CONTAINER_NAME'..."
        docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1
    fi

    echo "Creating container '$CONTAINER_NAME'..."
    docker run -d \
        --name "$CONTAINER_NAME" \
        -p "${SSH_PORT}:22" \
        ubuntu:24.04 \
        bash -c '
            apt-get update &&
            apt-get install -y openssh-server tmux git curl &&
            mkdir -p /run/sshd /root/.ssh &&
            chmod 700 /root/.ssh &&
            echo "PermitRootLogin yes" >> /etc/ssh/sshd_config &&
            /usr/sbin/sshd -D
        '

    # Stream container logs in the background so the user can see progress
    docker logs -f "$CONTAINER_NAME" 2>&1 &
    local logs_pid=$!

    # Wait for sshd to be ready (apt-get install takes a while on first run)
    echo "Waiting for sshd..."
    local retries=120
    while ! docker exec "$CONTAINER_NAME" pgrep -x sshd >/dev/null 2>&1; do
        # Check if container exited (e.g. apt-get failed)
        local cstate
        cstate=$(docker inspect -f '{{.State.Status}}' "$CONTAINER_NAME" 2>/dev/null | tr -d '[:space:]')
        if [ "$cstate" = "exited" ]; then
            kill "$logs_pid" 2>/dev/null || true
            echo ""
            echo "ERROR: Container exited unexpectedly. Logs:"
            docker logs --tail 20 "$CONTAINER_NAME"
            exit 1
        fi

        retries=$((retries - 1))
        if [ "$retries" -le 0 ]; then
            kill "$logs_pid" 2>/dev/null || true
            echo ""
            echo "ERROR: sshd failed to start in container. Check logs with:"
            echo "  docker logs $CONTAINER_NAME"
            exit 1
        fi
        sleep 1
    done

    # Stop tailing logs
    kill "$logs_pid" 2>/dev/null || true
    wait "$logs_pid" 2>/dev/null || true
    echo ""
    echo "Container is running with sshd."
}

# --- SSH key auth ---

setup_ssh_key() {
    if [ ! -f "$SSH_KEY_PATH" ]; then
        echo "Generating SSH keypair at $SSH_KEY_PATH..."
        ssh-keygen -t ed25519 -f "$SSH_KEY_PATH" -N "" -C "schmux-docker"
    else
        echo "SSH keypair already exists at $SSH_KEY_PATH."
    fi

    echo "Injecting public key into container..."
    local pubkey
    pubkey=$(cat "${SSH_KEY_PATH}.pub")
    docker exec "$CONTAINER_NAME" bash -c "
        mkdir -p /root/.ssh &&
        chmod 700 /root/.ssh &&
        echo '$pubkey' > /root/.ssh/authorized_keys &&
        chmod 600 /root/.ssh/authorized_keys
    "

    echo "Verifying SSH connectivity..."
    if ssh -i "$SSH_KEY_PATH" -o StrictHostKeyChecking=no -o ConnectTimeout=5 -p "$SSH_PORT" root@localhost echo "SSH connection successful" 2>/dev/null; then
        echo "SSH auth verified."
    else
        echo "ERROR: SSH connection failed. Check that port $SSH_PORT is not in use by another process."
        exit 1
    fi
}

# --- Config modification ---

update_config() {
    # Check if flavor already exists
    local existing
    existing=$(jq --arg id "$FLAVOR_ID" '.remote_flavors // [] | map(select(.id == $id)) | length' "$CONFIG_PATH")

    if [ "$existing" -gt 0 ]; then
        echo "Remote flavor '$FLAVOR_ID' already exists in config. Skipping."
        return
    fi

    echo "Adding remote flavor '$FLAVOR_ID' to $CONFIG_PATH..."

    local flavor
    flavor=$(cat <<EOF
{
    "id": "docker_ssh",
    "flavor": "root@localhost -p 2222",
    "display_name": "Docker SSH (Local)",
    "vcs": "git",
    "workspace_path": "/root/workspace",
    "connect_command": "ssh -tt -i ~/.schmux/docker-ssh-key -o StrictHostKeyChecking=no root@localhost -p 2222 --"
}
EOF
)

    local tmp
    tmp=$(mktemp)
    jq --argjson flavor "$flavor" '
        .remote_flavors = ((.remote_flavors // []) + [$flavor])
    ' "$CONFIG_PATH" > "$tmp" && mv "$tmp" "$CONFIG_PATH"

    echo "Config updated."
}

# --- Main ---

main() {
    echo "=== schmux Docker SSH setup ==="
    echo ""

    check_prereqs
    start_container
    setup_ssh_key
    update_config

    echo ""
    echo "=== Setup complete ==="
    echo ""
    echo "Container:  $CONTAINER_NAME (port $SSH_PORT)"
    echo "SSH key:    $SSH_KEY_PATH"
    echo "Flavor ID:  $FLAVOR_ID"
    echo ""
    echo "Test manually:"
    echo "  ssh -i $SSH_KEY_PATH -o StrictHostKeyChecking=no -p $SSH_PORT root@localhost"
}

main
