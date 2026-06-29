// Package oneshotlog persists oneshot LLM calls as JSONL records under
// ~/.schmux/logs/oneshot.jsonl. It is the write side of the Logs page's
// "oneshot" source; the source registry lives in package spawnlog.
package oneshotlog

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

// Path returns the absolute path of the oneshot log file. Single source of
// truth — spawnlog.SourcePath("oneshot") references this.
func Path() string {
	return filepath.Join(schmuxdir.LogsDir(), "oneshot.jsonl")
}

// Append writes one record as a JSON line to oneshot.jsonl. Best effort — a
// logging failure must never affect the oneshot call's result.
func Append(rec contracts.OneshotLogRecord) error {
	path := Path()
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
