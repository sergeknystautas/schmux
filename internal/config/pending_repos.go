package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// pendingReposFileName is the file the repo migration writes to and the repo
// manager reads from. It sits beside the config file.
const pendingReposFileName = "pending-repos.jsonl"

// PendingReposPath returns the path of the pending-repos file beside the
// config file at configPath.
func PendingReposPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), pendingReposFileName)
}

// appendPendingRepos appends repo entries to the pending file beside the
// config file, one compacted JSON line per entry.
//
// Every append starts with a newline, so a line cut short by a crash cannot
// swallow the next one; blank lines are skipped by the reader.
func appendPendingRepos(configPath string, entries []json.RawMessage) error {
	if len(entries) == 0 {
		return nil
	}

	// On a first run the defaults are applied before the directory exists.
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	f, err := os.OpenFile(PendingReposPath(configPath), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open pending repos file: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	buf.WriteString("\n")
	for _, entry := range entries {
		var compacted bytes.Buffer
		if err := json.Compact(&compacted, entry); err != nil {
			return fmt.Errorf("failed to compact repo entry: %w", err)
		}
		buf.Write(compacted.Bytes())
		buf.WriteString("\n")
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("failed to write pending repos: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync pending repos: %w", err)
	}
	return f.Close()
}

// backupConfig copies the config file, byte for byte, to a timestamped backup
// beside it. The backup is created exclusively, so one is never overwritten.
func backupConfig(configPath string, now time.Time) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}
	stamp := now.UTC().Format("2006-01-02T15-04-05.000000000")
	backupPath := configPath + "." + stamp + ".bak"

	f, err := os.OpenFile(backupPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create config backup: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("failed to write config backup: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync config backup: %w", err)
	}
	return f.Close()
}

// moveReposToPending moves the repo entries out of config.json and into the
// pending file. The daemon reads that file through the repo manager once the
// state store exists, which is later than a config load.
var moveReposToPending = Migration{
	Name: "move_repos_to_pending",
	Detect: func(raw map[string]json.RawMessage, cfg *Config) bool {
		_, ok := raw["repos"]
		return ok
	},
	Apply: func(raw map[string]json.RawMessage, cfg *Config) error {
		return applyMoveReposToPending(raw, cfg)
	},
}

// applyMoveReposToPending decodes the repos list, backs the config file up,
// and appends the entries to the pending file, in that order.
func applyMoveReposToPending(raw map[string]json.RawMessage, cfg *Config) error {
	rawRepos, ok := raw["repos"]
	if !ok {
		return nil
	}
	trimmed := bytes.TrimSpace(rawRepos)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}

	var entries []json.RawMessage
	if err := json.Unmarshal(trimmed, &entries); err != nil {
		return fmt.Errorf("repos is not a list: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}

	configPath := cfg.FilePath()
	if err := backupConfig(configPath, time.Now()); err != nil {
		return err
	}
	return appendPendingRepos(configPath, entries)
}
