package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sergeknystautas/schmux/pkg/cli"
)

// SpawnCommand implements the spawn command.
type SpawnCommand struct {
	client cli.DaemonClient
}

// NewSpawnCommand creates a new spawn command.
func NewSpawnCommand(client cli.DaemonClient) *SpawnCommand {
	return &SpawnCommand{client: client}
}

// Run executes the spawn command.
func (cmd *SpawnCommand) Run(args []string) error {
	var (
		targetFlag    string
		promptFlag    string
		workspaceFlag string
		repoFlag      string
		branchFlag    string
		nicknameFlag  string
		jsonOutput    bool
	)

	fs := flag.NewFlagSet("spawn", flag.ExitOnError)
	fs.StringVar(&targetFlag, "t", "", "Run target name (required)")
	fs.StringVar(&targetFlag, "target", "", "Run target name (required)")
	fs.StringVar(&promptFlag, "p", "", "Prompt for promptable targets")
	fs.StringVar(&promptFlag, "prompt", "", "Prompt for promptable targets")
	fs.StringVar(&workspaceFlag, "w", "", "Workspace path (e.g., . or ~/ws/myproject-001)")
	fs.StringVar(&workspaceFlag, "workspace", "", "Workspace path (e.g., . or ~/ws/myproject-001)")
	fs.StringVar(&repoFlag, "r", "", "Repo name from config (for new workspace)")
	fs.StringVar(&repoFlag, "repo", "", "Repo name from config (for new workspace)")
	fs.StringVar(&branchFlag, "b", "main", "Git branch")
	fs.StringVar(&branchFlag, "branch", "main", "Git branch")
	fs.StringVar(&nicknameFlag, "n", "", "Optional session nickname")
	fs.StringVar(&nicknameFlag, "nickname", "", "Optional session nickname")
	fs.BoolVar(&jsonOutput, "json", false, "JSON output")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Validate required flags
	if targetFlag == "" {
		return fmt.Errorf("required flag -t (--target) not provided")
	}

	// Check if daemon is running
	if !cmd.client.IsRunning() {
		return fmt.Errorf("daemon is not running. Start it with: schmux start")
	}

	// Get config to validate target/repo
	cfg, err := cmd.client.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	// Determine workspace/repo
	workspaceID := ""
	repoURL := ""

	if workspaceFlag != "" {
		// Workspace explicitly specified
		workspaceID, err = cmd.resolveWorkspace(workspaceFlag, cfg)
		if err != nil {
			return err
		}
	} else if repoFlag != "" {
		// Repo explicitly specified
		repo, found := cmd.findRepo(repoFlag, cfg)
		if !found {
			return fmt.Errorf("repo not found in config: %s", repoFlag)
		}
		repoURL = repo.URL
	} else {
		// Try to auto-detect current directory as workspace
		workspaceID, repoURL, err = cmd.autoDetectWorkspace(cfg)
		if err != nil {
			return fmt.Errorf("please specify -w (--workspace) or -r (--repo): %w", err)
		}
	}

	if target, found := cmd.findRunTarget(targetFlag, cfg); found {
		if target.Type == "command" && promptFlag != "" {
			return fmt.Errorf("prompt (-p/--prompt) is not allowed for command targets")
		}
		if target.Type == "promptable" && promptFlag == "" {
			return fmt.Errorf("prompt (-p/--prompt) is required for promptable targets")
		}
	}

	// Build spawn request
	req := cli.SpawnRequest{
		Repo:        repoURL,
		Branch:      branchFlag,
		Prompt:      promptFlag,
		Nickname:    nicknameFlag,
		WorkspaceID: workspaceID,
		Targets:     map[string]int{targetFlag: 1},
	}

	results, err := cmd.client.Spawn(context.Background(), req)
	if err != nil {
		return fmt.Errorf("spawn failed: %w", err)
	}

	// Output results
	if jsonOutput {
		return cmd.outputJSON(results)
	}
	workspaceOrRepo := workspaceID
	if workspaceOrRepo == "" {
		workspaceOrRepo = repoFlag
	}
	return cmd.outputHuman(results, workspaceOrRepo)
}

// resolveWorkspace resolves a workspace path to a workspace ID.
func (cmd *SpawnCommand) resolveWorkspace(path string, cfg *cli.Config) (string, error) {
	// Expand ~
	if strings.HasPrefix(path, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to expand ~: %w", err)
		}
		path = filepath.Join(homeDir, path[1:])
	}

	// Make absolute
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("failed to resolve path: %w", err)
		}
		path = abs
	}

	// Scan workspaces to ensure this one is tracked
	if _, err := cmd.client.ScanWorkspaces(context.Background()); err != nil {
		return "", fmt.Errorf("failed to scan workspaces: %w", err)
	}

	// Get all workspaces and find matching path
	workspaces, err := cmd.client.GetWorkspaces()
	if err != nil {
		return "", fmt.Errorf("failed to get workspaces: %w", err)
	}

	for _, ws := range workspaces {
		if ws.Path == path {
			return ws.ID, nil
		}
	}

	return "", fmt.Errorf("not a valid workspace: %s", path)
}

// autoDetectWorkspace tries to detect if the current directory is a workspace.
func (cmd *SpawnCommand) autoDetectWorkspace(cfg *cli.Config) (workspaceID, repoURL string, err error) {
	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Check if current directory is a workspace
	workspaces, err := cmd.client.GetWorkspaces()
	if err != nil {
		return "", "", fmt.Errorf("failed to get workspaces: %w", err)
	}

	for _, ws := range workspaces {
		if ws.Path == cwd {
			return ws.ID, "", nil
		}
	}

	return "", "", fmt.Errorf("not in a workspace directory")
}

// findRunTarget finds a run target by name in config.
func (cmd *SpawnCommand) findRunTarget(name string, cfg *cli.Config) (*cli.RunTarget, bool) {
	for _, target := range cfg.RunTargets {
		if target.Name == name {
			return &target, true
		}
	}
	return nil, false
}

// findRepo finds a repo by name in config.
func (cmd *SpawnCommand) findRepo(name string, cfg *cli.Config) (*cli.Repo, bool) {
	for _, repo := range cfg.Repos {
		if repo.Name == name {
			return &repo, true
		}
	}
	return nil, false
}

// outputHuman outputs results in human-readable format.
func (cmd *SpawnCommand) outputHuman(results []cli.SpawnResult, workspaceOrRepo string) error {
	fmt.Println("Spawn results:")
	for _, result := range results {
		if result.Error != "" {
			fmt.Printf("  [%s] Error: %s\n", result.Target, result.Error)
		} else {
			fmt.Printf("  [%s] Session: %s\n", result.Target, result.SessionID)
			fmt.Printf("        Workspace: %s\n", result.WorkspaceID)
			fmt.Printf("        Attach: schmux attach %s\n", result.SessionID)
		}
	}
	return nil
}

// outputJSON outputs results in JSON format.
func (cmd *SpawnCommand) outputJSON(results []cli.SpawnResult) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(results)
}
