package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/pkg/cli"
)

// BranchesCommand implements the branches command.
type BranchesCommand struct {
	client cli.DaemonClient
}

// NewBranchesCommand creates a new branches command.
func NewBranchesCommand(client cli.DaemonClient) *BranchesCommand {
	return &BranchesCommand{client: client}
}

// Run executes the branches command.
func (cmd *BranchesCommand) Run(args []string) error {
	// Parse flags
	var jsonOutput bool
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			jsonOutput = true
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	if !cmd.client.IsRunning() {
		return fmt.Errorf("daemon is not running. Start it with: schmux start")
	}

	reqURL := cmd.client.BaseURL() + "/api/branches"

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Get(reqURL)
	if err != nil {
		return fmt.Errorf("failed to fetch branches: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(body))
	}

	var entries []struct {
		WorkspaceID   string   `json:"workspace_id"`
		Repo          string   `json:"repo"`
		Branch        string   `json:"branch"`
		AheadMain     int      `json:"ahead_main"`
		BehindMain    int      `json:"behind_main"`
		Pushed        bool     `json:"pushed"`
		Dirty         bool     `json:"dirty"`
		SessionCount  int      `json:"session_count"`
		SessionStates []string `json:"session_states"`
		Error         string   `json:"error,omitempty"`
		Disconnected  bool     `json:"disconnected,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}

	if len(entries) == 0 {
		fmt.Println("No workspaces.")
		return nil
	}

	// Table output
	fmt.Printf("%-18s %-25s %-8s %-12s %-6s %s\n", "Workspace", "Branch", "Main", "Origin", "Dirty", "Sessions")
	fmt.Printf("%-18s %-25s %-8s %-12s %-6s %s\n", "---------", "------", "----", "------", "-----", "--------")
	for _, e := range entries {
		if e.Disconnected {
			fmt.Printf("%-18s %-25s %-8s %-12s %-6s %s\n", truncate(e.WorkspaceID, 18), "(disconnected)", "", "", "", "")
			continue
		}

		mainCol := fmt.Sprintf("+%d -%d", e.AheadMain, e.BehindMain)
		originCol := "not pushed"
		if e.Pushed {
			originCol = "pushed"
		}
		dirtyCol := "no"
		if e.Dirty {
			dirtyCol = "yes"
		}
		sessCol := ""
		if e.SessionCount > 0 {
			sessCol = fmt.Sprintf("%d (%s)", e.SessionCount, strings.Join(e.SessionStates, ", "))
		} else {
			sessCol = "0"
		}

		fmt.Printf("%-18s %-25s %-8s %-12s %-6s %s\n",
			truncate(e.WorkspaceID, 18),
			truncate(e.Branch, 25),
			mainCol,
			originCol,
			dirtyCol,
			sessCol,
		)
	}

	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
