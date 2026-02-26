package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/sergeknystautas/schmux/pkg/cli"
)

// EventsCommand implements the events command.
type EventsCommand struct {
	client cli.DaemonClient
}

// NewEventsCommand creates a new events command.
func NewEventsCommand(client cli.DaemonClient) *EventsCommand {
	return &EventsCommand{client: client}
}

// Run executes the events command.
func (cmd *EventsCommand) Run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: schmux events <session-id> [--type T] [--last N] [--json]")
	}
	sessionID := args[0]

	// Parse flags after session ID
	var typeFilter string
	var lastN int
	var jsonOutput bool
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--type":
			if i+1 >= len(rest) {
				return fmt.Errorf("flag --type requires a value")
			}
			typeFilter = rest[i+1]
			i++
		case "--last":
			if i+1 >= len(rest) {
				return fmt.Errorf("flag --last requires a value")
			}
			if _, err := fmt.Sscanf(rest[i+1], "%d", &lastN); err != nil {
				return fmt.Errorf("invalid --last value: %s", rest[i+1])
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

	// Build URL with query params
	params := url.Values{}
	if typeFilter != "" {
		params.Set("type", typeFilter)
	}
	if lastN > 0 {
		params.Set("last", fmt.Sprintf("%d", lastN))
	}

	reqURL := cli.GetDefaultURL() + "/api/sessions/" + sessionID + "/events"
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Get(reqURL)
	if err != nil {
		return fmt.Errorf("failed to fetch events: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(body))
	}

	var events []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(events)
	}

	// Human-readable output
	if len(events) == 0 {
		fmt.Printf("%s: no events\n", sessionID)
		return nil
	}

	fmt.Printf("%s events:\n\n", sessionID)
	for _, raw := range events {
		var evt struct {
			Ts       string `json:"ts"`
			Type     string `json:"type"`
			State    string `json:"state"`
			Message  string `json:"message"`
			Intent   string `json:"intent"`
			Source   string `json:"source"`
			Tool     string `json:"tool"`
			Error    string `json:"error"`
			Category string `json:"category"`
			Text     string `json:"text"`
		}
		if err := json.Unmarshal(raw, &evt); err != nil {
			continue
		}

		// Extract short time from timestamp
		ts := evt.Ts
		if t, err := time.Parse(time.RFC3339, evt.Ts); err == nil {
			ts = t.Local().Format("15:04:05")
		}

		switch evt.Type {
		case "status":
			source := ""
			if evt.Source == "floor-manager" {
				source = "[from FM] "
			}
			detail := evt.Message
			if detail == "" {
				detail = evt.Intent
			}
			fmt.Printf("  %s  %-8s %-14s %s%q\n", ts, evt.Type, evt.State, source, detail)
		case "failure":
			fmt.Printf("  %s  %-8s %-14s tool=%s error=%q category=%s\n", ts, evt.Type, evt.Category, evt.Tool, evt.Error, evt.Category)
		case "reflection", "friction":
			fmt.Printf("  %s  %-8s %q\n", ts, evt.Type, evt.Text)
		default:
			fmt.Printf("  %s  %s\n", ts, string(raw))
		}
	}

	return nil
}
