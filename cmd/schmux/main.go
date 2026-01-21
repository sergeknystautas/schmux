package main

import (
	"fmt"
	"os"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/daemon"
	"github.com/sergeknystautas/schmux/internal/update"
	"github.com/sergeknystautas/schmux/internal/version"
	"github.com/sergeknystautas/schmux/pkg/cli"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "start", "daemon-run":
		// Shared setup for both start and daemon-run
		configOk, err := config.EnsureExists()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking config: %v\n", err)
			os.Exit(1)
		}
		if !configOk {
			// User declined to create config
			os.Exit(1)
		}

		if err := daemon.ValidateReadyToRun(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Diverge here: background vs inline
		if command == "start" {
			if err := daemon.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("schmux daemon started")
		} else { // daemon-run
			background := false
			args := os.Args[2:]
			for _, arg := range args {
				if arg == "--background" {
					background = true
					break
				}
			}
			if err := daemon.Run(background); err != nil {
				fmt.Fprintf(os.Stderr, "Daemon error: %v\n", err)
				os.Exit(1)
			}
		}

	case "stop":
		if err := daemon.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("schmux daemon stopped")

	case "status":
		running, url, _, err := daemon.Status()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if running {
			fmt.Println("schmux daemon is running")
			fmt.Printf("Dashboard: %s\n", url)
		} else {
			fmt.Println("schmux daemon is not running")
			os.Exit(1)
		}

	case "version", "-v", "--version":
		fmt.Printf("schmux v%s\n", version.Version)

	case "update":
		if err := update.Update(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "help", "-h", "--help":
		printUsage()

	case "spawn":
		client := cli.NewDaemonClient(cli.GetDefaultURL())
		cmd := NewSpawnCommand(client)
		if err := cmd.Run(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "list":
		client := cli.NewDaemonClient(cli.GetDefaultURL())
		cmd := NewListCommand(client)
		if err := cmd.Run(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "attach":
		client := cli.NewDaemonClient(cli.GetDefaultURL())
		cmd := NewAttachCommand(client)
		if err := cmd.Run(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "dispose":
		client := cli.NewDaemonClient(cli.GetDefaultURL())
		cmd := NewDisposeCommand(client)
		if err := cmd.Run(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "refresh-overlay":
		client := cli.NewDaemonClient(cli.GetDefaultURL())
		cmd := NewRefreshOverlayCommand(client)
		if err := cmd.Run(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("schmux - Smart Cognitive Hub on tmux")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  schmux <command>")
	fmt.Println()
	fmt.Println("Daemon Commands:")
	fmt.Println("  start       Start the daemon in background")
	fmt.Println("  stop        Stop the daemon")
	fmt.Println("  status      Show daemon status and dashboard URL")
	fmt.Println("  daemon-run  Run the daemon in foreground (for debugging)")
	fmt.Println()
	fmt.Println("Session Commands:")
	fmt.Println("  spawn           Spawn a new session")
	fmt.Println("  list            List sessions")
	fmt.Println("  attach          Attach to a session")
	fmt.Println("  dispose         Dispose a session")
	fmt.Println()
	fmt.Println("Workspace Commands:")
	fmt.Println("  refresh-overlay Refresh overlay files for a workspace")
	fmt.Println()
	fmt.Println("Other:")
	fmt.Println("  version     Show version")
	fmt.Println("  update      Update schmux to the latest version")
	fmt.Println("  help        Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  schmux start                        # Start the daemon")
	fmt.Println("  schmux spawn -a claude -p \"fix bug\"  # Spawn in current workspace")
	fmt.Println("  schmux list                         # List all sessions")
	fmt.Println("  schmux attach <session-id>           # Attach to a session")
	fmt.Println("  schmux refresh-overlay <workspace>   # Refresh overlay files")
}
