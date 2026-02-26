package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/sergeknystautas/schmux/pkg/cli"
)

// InspectCommand implements the inspect command.
type InspectCommand struct {
	client cli.DaemonClient
}

// NewInspectCommand creates a new inspect command.
func NewInspectCommand(client cli.DaemonClient) *InspectCommand {
	return &InspectCommand{client: client}
}

// Run executes the inspect command.
func (cmd *InspectCommand) Run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: schmux inspect <workspace-id> [--json]")
	}
	workspaceID := args[0]

	// Parse flags after workspace ID
	var jsonOutput bool
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--json":
			jsonOutput = true
		default:
			return fmt.Errorf("unknown flag: %s", rest[i])
		}
	}

	if !cmd.client.IsRunning() {
		return fmt.Errorf("daemon is not running. Start it with: schmux start")
	}

	reqURL := cli.GetDefaultURL() + "/api/workspaces/" + workspaceID + "/inspect"

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Get(reqURL)
	if err != nil {
		return fmt.Errorf("failed to inspect workspace: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		WorkspaceID  string   `json:"workspace_id"`
		Repo         string   `json:"repo"`
		Branch       string   `json:"branch"`
		Pushed       bool     `json:"pushed"`
		RemoteBranch string   `json:"remote_branch,omitempty"`
		AheadMain    int      `json:"ahead_main"`
		BehindMain   int      `json:"behind_main"`
		Commits      []string `json:"commits"`
		Uncommitted  []string `json:"uncommitted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	// Human-readable output
	fmt.Printf("%s (%s)\n\n", result.WorkspaceID, result.Repo)
	fmt.Printf("  Branch:  %s\n", result.Branch)
	if result.Pushed {
		fmt.Printf("  Pushed:  yes (%s)\n", result.RemoteBranch)
	} else {
		fmt.Printf("  Pushed:  no\n")
	}
	fmt.Printf("  vs main: +%d commits, -%d behind\n", result.AheadMain, result.BehindMain)

	if len(result.Commits) > 0 {
		fmt.Printf("\n  Commits (not in main):\n")
		for _, c := range result.Commits {
			fmt.Printf("    %s\n", c)
		}
	}

	if len(result.Uncommitted) > 0 {
		fmt.Printf("\n  Uncommitted:\n")
		for _, u := range result.Uncommitted {
			fmt.Printf("    %s\n", u)
		}
	}

	return nil
}
