// Package spawnlog persists spawn attempts as JSONL records under
// ~/.schmux/logs/ and exposes the source registry the Logs page reads.
package spawnlog

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/oneshotlog"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

// SourcePath maps a log-source name to its absolute file path and reports
// whether the source is known. Sources: "spawn", "oneshot".
func SourcePath(name string) (string, bool) {
	switch name {
	case "spawn":
		return filepath.Join(schmuxdir.LogsDir(), "spawn.jsonl"), true
	case "oneshot":
		return oneshotlog.Path(), true
	default:
		return "", false
	}
}

// Append writes one record as a JSON line to spawn.jsonl. Best effort — the
// caller logs and continues on error; it must never fail a spawn.
func Append(rec contracts.SpawnLogRecord) error {
	path, _ := SourcePath("spawn")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// DeriveStatus returns "ok" if every result succeeded, "failed" if every result
// errored (or there are none), and "partial" if mixed.
func DeriveStatus(results []contracts.SpawnLogResult) string {
	if len(results) == 0 {
		return "failed"
	}
	ok, failed := 0, 0
	for _, r := range results {
		if r.Error == "" {
			ok++
		} else {
			failed++
		}
	}
	switch {
	case failed == 0:
		return "ok"
	case ok == 0:
		return "failed"
	default:
		return "partial"
	}
}
