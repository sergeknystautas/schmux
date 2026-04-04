//go:build !norepofeed

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

// RepofeedCommand implements the repofeed command.
type RepofeedCommand struct {
	client cli.DaemonClient
}

// NewRepofeedCommand creates a new repofeed command.
func NewRepofeedCommand(client cli.DaemonClient) *RepofeedCommand {
	return &RepofeedCommand{client: client}
}

// Run executes the repofeed command.
func (cmd *RepofeedCommand) Run(args []string) error {
	var jsonOutput bool
	var repoFilter string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			jsonOutput = true
		case "--repo":
			if i+1 < len(args) {
				i++
				repoFilter = args[i]
			} else {
				return fmt.Errorf("--repo requires a value")
			}
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	if !cmd.client.IsRunning() {
		return fmt.Errorf("daemon is not running. Start it with: schmux start")
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}

	if repoFilter != "" {
		return cmd.showRepo(httpClient, repoFilter, jsonOutput)
	}
	return cmd.showList(httpClient, jsonOutput)
}

func (cmd *RepofeedCommand) showList(httpClient *http.Client, jsonOutput bool) error {
	reqURL := cmd.client.BaseURL() + "/api/repofeed"
	resp, err := httpClient.Get(reqURL)
	if err != nil {
		return fmt.Errorf("failed to fetch repofeed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(body))
	}

	var data struct {
		Repos []struct {
			Name          string `json:"name"`
			Slug          string `json:"slug"`
			ActiveIntents int    `json:"active_intents"`
			LandedCount   int    `json:"landed_count"`
		} `json:"repos"`
		LastFetch string `json:"last_fetch,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	}

	if len(data.Repos) == 0 {
		fmt.Println("No repofeed data. Enable repofeed in config to start publishing.")
		return nil
	}

	for _, repo := range data.Repos {
		fmt.Printf("%s: %d active intent(s)\n", repo.Name, repo.ActiveIntents)
	}
	return nil
}

func (cmd *RepofeedCommand) showRepo(httpClient *http.Client, slug string, jsonOutput bool) error {
	reqURL := cmd.client.BaseURL() + "/api/repofeed/" + slug
	resp, err := httpClient.Get(reqURL)
	if err != nil {
		return fmt.Errorf("failed to fetch repofeed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(body))
	}

	var data struct {
		Name    string `json:"name"`
		Slug    string `json:"slug"`
		Intents []struct {
			Developer    string   `json:"developer"`
			DisplayName  string   `json:"display_name"`
			Intent       string   `json:"intent"`
			Status       string   `json:"status"`
			Started      string   `json:"started"`
			Branches     []string `json:"branches"`
			SessionCount int      `json:"session_count"`
			Agents       []string `json:"agents"`
		} `json:"intents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	}

	if len(data.Intents) == 0 {
		fmt.Printf("%s: no active intents\n", data.Name)
		return nil
	}

	fmt.Printf("%s:\n", data.Name)
	for _, intent := range data.Intents {
		statusDot := "○"
		if intent.Status == "active" {
			statusDot = "●"
		}
		name := intent.DisplayName
		if name == "" {
			name = intent.Developer
		}
		fmt.Printf("  %s %s — %s", statusDot, name, intent.Intent)
		if len(intent.Branches) > 0 {
			fmt.Printf(" [%s]", intent.Branches[0])
		}
		fmt.Println()
	}
	return nil
}
