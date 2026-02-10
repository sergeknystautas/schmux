# Contributing to schmux

## Prerequisites

### Runtime Dependencies

These are needed to run schmux (whether installed via script or built from source):

- **tmux** - Required for session management
  - macOS: `brew install tmux`
  - Linux: `apt install tmux` or equivalent
- **git** - For workspace management

### Development Dependencies

These are additionally needed to build schmux from source:

- **Go 1.21+** - [Download](https://go.dev/dl/)
  - macOS: `brew install go`
  - Linux: See [official install guide](https://go.dev/doc/install)
- **Node.js 18+ & npm** - For building the React dashboard
  - macOS: `brew install node`
  - Linux: `apt install nodejs npm` or use [nvm](https://github.com/nvm-sh/nvm)

### Verify Your Setup

```bash
# Check versions
go version      # go1.21+ required
node --version  # v18+ required
npm --version
tmux -V
git --version
```

## Building from Source

```bash
# Clone the repository
git clone https://github.com/sergeknystautas/schmux.git
cd schmux

# Build dashboard (npm install + vite build) and Go binary
go run ./cmd/build-dashboard

# The ./schmux binary is now ready
./schmux version
```

### Build Options

```bash
# Full build with tests
go run ./cmd/build-dashboard -test -build

# Skip npm install (if node_modules exists)
go run ./cmd/build-dashboard -skip-install

# Just build Go binary (if dashboard already built)
go build ./cmd/schmux
```

### What Gets Built

1. **Dashboard assets** (`assets/dashboard/dist/`) - React app built with Vite
2. **Go binary** (`./schmux`) - CLI and daemon

The binary serves dashboard assets from `./assets/dashboard/dist/` during development, or from `~/.schmux/dashboard/` for installed versions.

## Quick Start

### 1. Create Configuration

Create `~/.schmux/config.json`:

```json
{
  "workspace_path": "~/dev/schmux-workspaces",
  "repos": [
    {
      "name": "myproject",
      "url": "git@github.com:user/myproject.git"
    }
  ],
  "run_targets": [
    {
      "name": "codex",
      "type": "promptable",
      "command": "codex"
    },
    {
      "name": "claude",
      "type": "promptable",
      "command": "claude"
    }
  ]
}
```

### 2. Start the Daemon

```bash
./schmux start
```

Output: `schmux daemon started`

### 3. Check Status

```bash
./schmux status
```

Output:

```
schmux daemon is running
Dashboard: http://localhost:7337
```

### 4. Open the Dashboard

Open http://localhost:7337 in your browser.

From there you can:

- **Spawn sessions** - Select repo, enter branch/prompt, choose agents
- **View sessions** - See all running sessions with attach commands
- **View terminal** - Watch real-time output from each session

### 5. Attach to a Session

To attach directly from your terminal:

```bash
tmux attach -t <session-name>
```

Press `Ctrl+B d` to detach without ending the session.

## Development Workflow

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests for a specific package
go test ./internal/session

# Run tests with coverage
go test -cover ./...
```

### Project Structure

```
schmux/
├── cmd/schmux/main.go          # CLI entry point
├── internal/
│   ├── config/                 # Configuration loading
│   ├── state/                  # State persistence (JSON)
│   ├── tmux/                   # tmux integration
│   ├── workspace/              # Workspace & git management
│   ├── session/                # Session lifecycle
│   ├── daemon/                 # Daemon process management
│   └── dashboard/              # Web dashboard (server + handlers)
├── assets/dashboard/           # Frontend assets
│   ├── index.html             # Session list view
│   ├── spawn.html             # Spawn form
│   ├── terminal.html          # Terminal view
│   ├── styles.css             # Styles
│   └── app.ts                # TypeScript
├── README.md                   # Project overview
└── docs/                       # Documentation
```

### Making Changes

1. **Create a branch** for your work:

   ```bash
   git checkout -b feature/my-feature
   ```

2. **Make your changes** and update tests

3. **Run tests** to ensure nothing breaks:

   ```bash
   go test ./...
   ```

4. **Compile-check** to verify everything builds:

   ```bash
   go build ./...
   ```

5. **Build the runnable binary** (so you’re testing the code you just changed):

   ```bash
   # Produces ./schmux in the repo root
   go build ./cmd/schmux

   # Alternative: update the schmux on your PATH
   # go install ./cmd/schmux
   ```

6. **Test manually** if applicable:

   ```bash
   # Start daemon
   ./schmux start

   # Spawn a test session via dashboard
   # Open http://localhost:7337

   # Stop daemon when done
   ./schmux stop
   ```

7. **Commit** your changes:

   ```bash
   git add -A
   git commit -m "Description of changes"
   ```

8. **Push** and create a pull request

## Configuration

### Config File Location

`~/.schmux/config.json` - Created manually, contains:

- **workspace_path**: Directory for cloned repositories
- **repos**: List of git repositories to work with
- **run_targets**: Available AI agents and commands (name + type + command)

### State File Location

`~/.schmux/state.json` - Auto-generated, contains:

- **workspaces**: Managed workspace directories
- **sessions**: Active session information

**Note:** Local development testing can use a `.schmux/` directory in the repo root (this is gitignored).

## CLI Commands

| Command         | Description                          |
| --------------- | ------------------------------------ |
| `schmux start`  | Start daemon in background           |
| `schmux stop`   | Stop daemon                          |
| `schmux status` | Show daemon status and dashboard URL |

## Web Dashboard

- **URL:** http://localhost:7337 (or `https://<public_base_url>` when auth is enabled)
- **Security:** Localhost only by default. Optional GitHub auth requires HTTPS.
- **Features:**
  - Spawn sessions across multiple agents
  - View real-time terminal output
  - Copy attach commands
  - Dispose completed sessions

### HTTPS & GitHub Auth

When auth is enabled, schmux serves HTTPS directly using the configured certificate paths.

**Recommended setup** - Use the interactive wizard:

```bash
./schmux auth github
```

This walks you through:

1. Choosing a hostname (e.g., `schmux.local`)
2. Auto-generating TLS certificates via mkcert (or providing your own)
3. Creating a GitHub OAuth App with the exact values to copy
4. Configuring network access and session TTL

Certificates are stored in `~/.schmux/tls/` and config is written to `~/.schmux/config.json` and `~/.schmux/secrets.json`.

**Manual setup** (if needed):

```bash
# Install mkcert and local CA once
brew install mkcert
mkcert -install

# Create a local cert for schmux.local
mkcert -cert-file ~/.schmux/tls/schmux.local.pem -key-file ~/.schmux/tls/schmux.local-key.pem schmux.local
```

Then configure in `config.json`:

```json
{
  "network": {
    "bind_address": "127.0.0.1",
    "port": 7337,
    "public_base_url": "https://schmux.local:7337",
    "tls": {
      "cert_path": "~/.schmux/tls/schmux.local.pem",
      "key_path": "~/.schmux/tls/schmux.local-key.pem"
    }
  },
  "access_control": {
    "enabled": true,
    "provider": "github",
    "session_ttl_minutes": 1440
  }
}
```

And GitHub credentials in `secrets.json`:

```json
{
  "auth": {
    "github": {
      "client_id": "your-client-id",
      "client_secret": "your-client-secret"
    }
  }
}
```

## Troubleshooting

### Port Already in Use

If you see "bind: address already in use":

```bash
# Find what's using port 7337
lsof -i :7337  # macOS
netstat -tulpn | grep 7337  # Linux

# Kill the process or change the port in internal/dashboard/server.go
```

### Tmux Session Issues

List all schmux sessions:

```bash
tmux list-sessions | grep schmux
```

Kill a stuck session:

```bash
tmux kill-session -t <session-name>
```

### Daemon Won't Stop

Force kill:

```bash
# Check PID
cat ~/.schmux/daemon.pid

# Kill it
kill <pid>

# Or force kill
kill -9 <pid>

# Remove stale PID file
rm ~/.schmux/daemon.pid
```

### Clean State

To start fresh:

```bash
# Stop daemon first
./schmux stop

# Remove state
rm ~/.schmux/state.json

# Optionally clean workspaces (careful!)
rm -rf ~/dev/schmux-workspaces/*
```

## Documentation Conventions

### Shell Commands

When documenting shell commands in markdown, **include inline `#` comments** within the code block:

```bash
# This is preferred - context stays with the command
go run ./cmd/build-dashboard  # Builds dashboard (npm install + vite build)
go test ./...                 # Run all tests
./schmux start                # Start daemon in background
```

**Why?** Inline comments provide context that AI agents and readers need directly alongside the command, which is especially valuable when commands are copied or referenced programmatically. The comments are valid shell syntax and don't break execution.

This convention is used by other high-visibility projects like:

- [openai/openai-codex](https://github.com/openai/openai-codex)
- [bead-community/bead](https://github.com/bead-community/bead)

For longer explanations, use comment-only lines before the command:

```bash
# Run all unit tests with verbose output
# This is useful when debugging test failures
go test -v ./...
```

## Code Style

- Follow standard Go conventions
- Use `gofmt` for formatting
- Add tests for new functionality
- Update this guide for new workflows

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.
