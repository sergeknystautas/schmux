# Remote Workspace Support

This package implements remote workspace support for schmux.

## Architecture

Schmux connects to remote development hosts using `tmux -CC` (control mode) over a persistent connection.

### Connection Mechanism

The implementation uses a remote connection command with tmux control mode:

```bash
<remote-connect-cmd> <flavor> tmux -CC new-session -A -s schmux
```

- **Flavor**: The remote environment type (e.g., `gpu:ml-large`, `docker:devenv`).
- **Persistent Connection**: Uses persistent terminal technology to survive network interruptions.
- **Control Mode**: Allows schmux to drive the remote tmux instance programmatically (create windows, send keys, capture output) without parsing escape codes.
- **Transport-agnostic**: The user's connect command handles transport (SSH, Docker, etc.). Schmux appends the tmux invocation automatically. Any transport-specific separators (like `--` for SSH) should be included in the user's connect command template.

### Key Components

- **Manager (`manager.go`)**: High-level connection lifecycle management (connect, reconnect, disconnect).
- **Connection (`connection.go`)**: Wraps the remote connection process and manages the control mode client.
- **Control Mode Client (`controlmode/`)**: Implements the tmux control mode protocol to interact with the remote tmux session.

## Prerequisites

The host machine running schmux must have the appropriate remote connection tooling installed and authenticated.
