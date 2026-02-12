package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/sergeknystautas/schmux/pkg/cli"
)

// AnalyzeRepoCommand implements the analyze-repo command.
type AnalyzeRepoCommand struct {
	client cli.DaemonClient
}

// NewAnalyzeRepoCommand creates a new analyze-repo command.
func NewAnalyzeRepoCommand(client cli.DaemonClient) *AnalyzeRepoCommand {
	return &AnalyzeRepoCommand{client: client}
}

// Run executes the analyze-repo command.
func (cmd *AnalyzeRepoCommand) Run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: schmux analyze-repo <repo-name> [--depth N] [--output path]")
	}

	repoName := args[0]

	var (
		depth  int
		output string
	)

	fs := flag.NewFlagSet("analyze-repo", flag.ContinueOnError)
	fs.IntVar(&depth, "depth", 1000, "Max commits to analyze for co-change coupling")
	fs.StringVar(&output, "output", "", "Output file path (default: <repo-workspace-path>/repo-index.json)")

	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if depth <= 0 {
		return fmt.Errorf("depth must be > 0")
	}

	if !cmd.client.IsRunning() {
		return fmt.Errorf("daemon is not running. Start it with: schmux start")
	}

	resp, err := cmd.client.AnalyzeRepo(context.Background(), repoName, depth, output)
	if err != nil {
		return fmt.Errorf("failed to analyze repo: %w", err)
	}

	fmt.Printf("Repository analysis written to %s\n", resp.Output)
	return nil
}
