package main

import (
	"fmt"
	"os"

	"github.com/sergek/schmux/internal/daemon"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "start":
		if err := daemon.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("schmux daemon started")

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

	case "daemon-run":
		// This is the entry point for the daemon process
		if err := daemon.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Daemon error: %v\n", err)
			os.Exit(1)
		}

	case "help", "-h", "--help":
		printUsage()

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
	fmt.Println("Commands:")
	fmt.Println("  start       Start the daemon in background")
	fmt.Println("  stop        Stop the daemon")
	fmt.Println("  status      Show daemon status and dashboard URL")
	fmt.Println("  daemon-run  Run the daemon in foreground (for debugging)")
	fmt.Println("  help        Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  schmux start       # Start the daemon")
	fmt.Println("  schmux status      # Check if daemon is running")
	fmt.Println("  schmux stop        # Stop the daemon")
	fmt.Println("  schmux daemon-run  # Run in foreground to see debug output")
}
