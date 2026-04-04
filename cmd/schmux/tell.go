package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sergeknystautas/schmux/pkg/cli"
)

// TellCommand implements the tell command.
type TellCommand struct {
	client cli.DaemonClient
}

// NewTellCommand creates a new tell command.
func NewTellCommand(client cli.DaemonClient) *TellCommand {
	return &TellCommand{client: client}
}

// Run executes the tell command.
func (cmd *TellCommand) Run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: schmux tell <session-id> -m \"message\"")
	}
	sessionID := args[0]

	// Parse flags after session ID
	var messageFlag string
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "-m", "--message":
			if i+1 >= len(rest) {
				return fmt.Errorf("flag %s requires a value", rest[i])
			}
			messageFlag = rest[i+1]
			i++
		default:
			return fmt.Errorf("unknown flag: %s", rest[i])
		}
	}

	if messageFlag == "" {
		return fmt.Errorf("required flag -m (--message) not provided")
	}

	if !cmd.client.IsRunning() {
		return fmt.Errorf("daemon is not running. Start it with: schmux start")
	}

	// POST to /api/sessions/{id}/tell
	body, _ := json.Marshal(map[string]string{"message": messageFlag})
	url := cmd.client.BaseURL() + "/api/sessions/" + sessionID + "/tell"

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(respBody))
	}

	fmt.Printf("Message sent to session %s.\n", sessionID)
	return nil
}
