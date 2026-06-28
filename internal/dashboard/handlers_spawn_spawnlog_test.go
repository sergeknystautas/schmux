package dashboard

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/spawnlog"
)

func TestWriteSpawnLog_FailedSpawnPersistsPrompt(t *testing.T) {
	schmuxdir.Set(t.TempDir())
	req := SpawnRequest{
		Repo:    "https://example.com/godot.git",
		Branch:  "feature/fmod",
		Targets: map[string]int{"claude": 1},
		Prompt:  "look at ~/Downloads for the fmod specs",
	}
	results := []SessionResult{{Target: "claude", Error: "failed to get workspace: duplicate repo URLs"}}

	writeSpawnLog(logging.New(), req, results)

	path, _ := spawnlog.SourcePath("spawn")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read spawn log: %v", err)
	}
	var rec contracts.SpawnLogRecord
	if err := json.Unmarshal(bytes.TrimSpace(data), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec.Prompt != req.Prompt {
		t.Errorf("prompt = %q, want %q", rec.Prompt, req.Prompt)
	}
	if rec.Status != "failed" {
		t.Errorf("status = %q, want failed", rec.Status)
	}
	if len(rec.Results) != 1 || rec.Results[0].Error == "" {
		t.Errorf("expected one failed result, got %+v", rec.Results)
	}
}
