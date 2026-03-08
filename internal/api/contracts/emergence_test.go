package contracts

import (
	"encoding/json"
	"testing"
)

func TestSpawnEntryJSON(t *testing.T) {
	entry := SpawnEntry{
		ID:       "test-1",
		Name:     "Test entry",
		Type:     SpawnEntrySkill,
		Source:   SpawnSourceEmerged,
		State:    SpawnStatePinned,
		SkillRef: "code-review",
		UseCount: 5,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SpawnEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != entry.ID || decoded.SkillRef != entry.SkillRef {
		t.Errorf("round-trip mismatch: got %+v", decoded)
	}
}
