package emergence

import (
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

func TestMetadataStore_SaveAndLoad(t *testing.T) {
	s := NewMetadataStore(t.TempDir())
	repo := "test-repo"
	now := time.Now().Truncate(time.Second)

	meta := contracts.EmergenceMetadata{
		SkillName:     "code-review",
		Confidence:    0.85,
		EvidenceCount: 5,
		Evidence:      []string{"review PR #123", "review PR #456"},
		EmergedAt:     now,
		LastCurated:   now,
	}

	if err := s.Save(repo, meta); err != nil {
		t.Fatal(err)
	}

	got, ok, err := s.Get(repo, "code-review")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected to find metadata")
	}
	if got.Confidence != 0.85 {
		t.Errorf("expected confidence 0.85, got %f", got.Confidence)
	}
	if got.EvidenceCount != 5 {
		t.Errorf("expected evidence_count 5, got %d", got.EvidenceCount)
	}
	if len(got.Evidence) != 2 {
		t.Errorf("expected 2 evidence items, got %d", len(got.Evidence))
	}
}

func TestMetadataStore_GetMissing(t *testing.T) {
	s := NewMetadataStore(t.TempDir())

	_, ok, err := s.Get("test-repo", "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected not found")
	}
}

func TestMetadataStore_ListAll(t *testing.T) {
	s := NewMetadataStore(t.TempDir())
	repo := "test-repo"
	now := time.Now().Truncate(time.Second)

	s.Save(repo, contracts.EmergenceMetadata{
		SkillName: "skill-a", Confidence: 0.9, EmergedAt: now, LastCurated: now,
	})
	s.Save(repo, contracts.EmergenceMetadata{
		SkillName: "skill-b", Confidence: 0.7, EmergedAt: now, LastCurated: now,
	})

	all, err := s.ListAll(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 metadata entries, got %d", len(all))
	}
}

func TestMetadataStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	repo := "test-repo"
	now := time.Now().Truncate(time.Second)

	s1 := NewMetadataStore(dir)
	s1.Save(repo, contracts.EmergenceMetadata{
		SkillName: "persistent", Confidence: 0.95, EmergedAt: now, LastCurated: now,
	})

	s2 := NewMetadataStore(dir)
	got, ok, err := s2.Get(repo, "persistent")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected to find metadata in new store instance")
	}
	if got.Confidence != 0.95 {
		t.Errorf("expected confidence 0.95, got %f", got.Confidence)
	}
}

func TestMetadataStore_UpdateExisting(t *testing.T) {
	s := NewMetadataStore(t.TempDir())
	repo := "test-repo"
	now := time.Now().Truncate(time.Second)

	s.Save(repo, contracts.EmergenceMetadata{
		SkillName: "evolving", Confidence: 0.5, EvidenceCount: 3, EmergedAt: now, LastCurated: now,
	})

	later := now.Add(time.Hour)
	s.Save(repo, contracts.EmergenceMetadata{
		SkillName: "evolving", Confidence: 0.9, EvidenceCount: 8, EmergedAt: now, LastCurated: later,
	})

	got, ok, _ := s.Get(repo, "evolving")
	if !ok {
		t.Fatal("expected to find metadata")
	}
	if got.Confidence != 0.9 {
		t.Errorf("expected updated confidence 0.9, got %f", got.Confidence)
	}
	if got.EvidenceCount != 8 {
		t.Errorf("expected updated evidence_count 8, got %d", got.EvidenceCount)
	}

	// Should still have only one entry
	all, _ := s.ListAll(repo)
	if len(all) != 1 {
		t.Errorf("expected 1 entry after update, got %d", len(all))
	}
}
