package contracts

import (
	"encoding/json"
	"testing"
)

func TestSpawnLogRecord_JSONRoundTrip(t *testing.T) {
	rec := SpawnLogRecord{
		TS:      "2026-06-27T15:24:06Z",
		Repo:    "https://example.com/x.git",
		Branch:  "feat/x",
		Targets: map[string]int{"claude": 1},
		Prompt:  "find the fmod specs",
		Status:  "failed",
		Results: []SpawnLogResult{{Target: "claude", Error: "boom"}},
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got SpawnLogRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Prompt != rec.Prompt || got.Status != "failed" || len(got.Results) != 1 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}
