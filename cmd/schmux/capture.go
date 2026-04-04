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

// CaptureCommand implements the capture command.
type CaptureCommand struct {
	client cli.DaemonClient
}

// NewCaptureCommand creates a new capture command.
func NewCaptureCommand(client cli.DaemonClient) *CaptureCommand {
	return &CaptureCommand{client: client}
}

// Run executes the capture command.
func (cmd *CaptureCommand) Run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: schmux capture <session-id> [--lines N] [--json]")
	}
	sessionID := args[0]

	// Parse flags after session ID
	linesFlag := 50
	var jsonOutput bool
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--lines":
			if i+1 >= len(rest) {
				return fmt.Errorf("flag --lines requires a value")
			}
			if _, err := fmt.Sscanf(rest[i+1], "%d", &linesFlag); err != nil {
				return fmt.Errorf("invalid --lines value: %s", rest[i+1])
			}
			i++
		case "--json":
			jsonOutput = true
		default:
			return fmt.Errorf("unknown flag: %s", rest[i])
		}
	}

	if !cmd.client.IsRunning() {
		return fmt.Errorf("daemon is not running. Start it with: schmux start")
	}

	reqURL := fmt.Sprintf("%s/api/sessions/%s/capture?lines=%d", cmd.client.BaseURL(), sessionID, linesFlag)

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Get(reqURL)
	if err != nil {
		return fmt.Errorf("failed to capture output: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		SessionID string `json:"session_id"`
		Lines     int    `json:"lines"`
		Output    string `json:"output"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Print(result.Output)
	return nil
}
