# Contributing to schmux

## Prerequisites

- **Go 1.21+** - [Download](https://go.dev/dl/)
- **tmux** - Required for session management
  - macOS: `brew install tmux`
  - Linux: `apt install tmux` or equivalent
- **git** - For workspace management

## Building from Source

```bash
# Clone the repository
git clone https://github.com/sergek/schmux.git
cd schmux

# Download dependencies
go mod download

# Build the binary
go build ./cmd/schmux

# (Optional) Install to your PATH
go install ./cmd/schmux
```

The `schmux` binary will be created in the current directory.

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
  "agents": [
    {
      "name": "codex",
      "command": "codex"
    },
    {
      "name": "claude",
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
│   └── app.js                 # JavaScript
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
- **agents**: Available AI agents (name + command)

### State File Location

`~/.schmux/state.json` - Auto-generated, contains:
- **workspaces**: Managed workspace directories
- **sessions**: Active session information

**Note:** Local development testing can use a `.schmux/` directory in the repo root (this is gitignored).

## CLI Commands

| Command | Description |
|---------|-------------|
| `schmux start` | Start daemon in background |
| `schmux stop` | Stop daemon |
| `schmux status` | Show daemon status and dashboard URL |

## Web Dashboard

- **URL:** http://localhost:7337
- **Security:** Localhost only, no authentication (as per v0.5 spec)
- **Features:**
  - Spawn sessions across multiple agents
  - View real-time terminal output
  - Copy attach commands
  - Dispose completed sessions

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

## Code Style

- Follow standard Go conventions
- Use `gofmt` for formatting
- Add tests for new functionality
- Update this guide for new workflows

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.
