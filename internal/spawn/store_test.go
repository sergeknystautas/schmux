package spawn

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir())
}

func TestStore_ListEmpty(t *testing.T) {
	s := newTestStore(t)
	entries, err := s.List("test-repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty list, got %d entries", len(entries))
	}
}

func TestStore_CreateAndGet(t *testing.T) {
	s := newTestStore(t)
	entry, err := s.Create("test-repo", contracts.CreateSpawnEntryRequest{
		Name:   "test action",
		Type:   contracts.SpawnEntryAgent,
		Prompt: "do something",
		Target: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if entry.ID == "" {
		t.Error("expected non-empty ID")
	}
	if entry.Name != "test action" {
		t.Errorf("expected name 'test action', got %q", entry.Name)
	}
	if entry.Source != contracts.SpawnSourceManual {
		t.Errorf("expected source manual, got %q", entry.Source)
	}
	if entry.State != contracts.SpawnStatePinned {
		t.Errorf("expected state pinned, got %q", entry.State)
	}

	// Get by ID
	got, ok, err := s.Get("test-repo", entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected to find entry")
	}
	if got.Name != "test action" {
		t.Errorf("expected name 'test action', got %q", got.Name)
	}

	// Get missing
	_, ok, err = s.Get("test-repo", "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected not found")
	}
}

func TestStore_ListReturnsPinnedSortedByUseCount(t *testing.T) {
	s := newTestStore(t)
	repo := "test-repo"

	// Create entries with different use counts
	e1, _ := s.Create(repo, contracts.CreateSpawnEntryRequest{
		Name: "low use", Type: contracts.SpawnEntryCommand, Command: "echo 1",
	})
	e2, _ := s.Create(repo, contracts.CreateSpawnEntryRequest{
		Name: "high use", Type: contracts.SpawnEntryCommand, Command: "echo 2",
	})

	// Record uses to set ordering
	s.RecordUse(repo, e2.ID)
	s.RecordUse(repo, e2.ID)
	s.RecordUse(repo, e2.ID)
	s.RecordUse(repo, e1.ID)

	entries, err := s.List(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Name != "high use" {
		t.Errorf("expected first entry to be 'high use', got %q", entries[0].Name)
	}
	if entries[0].UseCount != 3 {
		t.Errorf("expected use_count 3, got %d", entries[0].UseCount)
	}
	if entries[1].Name != "low use" {
		t.Errorf("expected second entry to be 'low use', got %q", entries[1].Name)
	}
}

func TestStore_Update(t *testing.T) {
	s := newTestStore(t)
	repo := "test-repo"

	entry, _ := s.Create(repo, contracts.CreateSpawnEntryRequest{
		Name: "original", Type: contracts.SpawnEntryCommand, Command: "echo old",
	})

	newName := "updated"
	newCmd := "echo new"
	err := s.Update(repo, entry.ID, contracts.UpdateSpawnEntryRequest{
		Name:    &newName,
		Command: &newCmd,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, ok, _ := s.Get(repo, entry.ID)
	if !ok {
		t.Fatal("entry not found after update")
	}
	if got.Name != "updated" {
		t.Errorf("expected name 'updated', got %q", got.Name)
	}
	if got.Command != "echo new" {
		t.Errorf("expected command 'echo new', got %q", got.Command)
	}
}

func TestStore_Delete(t *testing.T) {
	s := newTestStore(t)
	repo := "test-repo"

	entry, _ := s.Create(repo, contracts.CreateSpawnEntryRequest{
		Name: "to delete", Type: contracts.SpawnEntryCommand, Command: "echo bye",
	})

	err := s.Delete(repo, entry.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, ok, _ := s.Get(repo, entry.ID)
	if ok {
		t.Error("expected entry to be deleted")
	}
}

func TestStore_RecordUse(t *testing.T) {
	s := newTestStore(t)
	repo := "test-repo"

	entry, _ := s.Create(repo, contracts.CreateSpawnEntryRequest{
		Name: "test", Type: contracts.SpawnEntryCommand, Command: "echo hi",
	})

	err := s.RecordUse(repo, entry.ID)
	if err != nil {
		t.Fatal(err)
	}

	got, ok, _ := s.Get(repo, entry.ID)
	if !ok {
		t.Fatal("entry not found")
	}
	if got.UseCount != 1 {
		t.Errorf("expected use_count 1, got %d", got.UseCount)
	}
	if got.LastUsed == nil {
		t.Error("expected last_used to be set")
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	repo := "test-repo"

	// Create and populate store
	s1 := NewStore(dir)
	s1.Create(repo, contracts.CreateSpawnEntryRequest{
		Name: "persistent", Type: contracts.SpawnEntryCommand, Command: "echo persist",
	})

	// Create new store from same directory
	s2 := NewStore(dir)
	entries, err := s2.List(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry from loaded store, got %d", len(entries))
	}
	if entries[0].Name != "persistent" {
		t.Errorf("expected 'persistent', got %q", entries[0].Name)
	}
}

func TestStore_AtomicWrite(t *testing.T) {
	s := newTestStore(t)
	repo := "test-repo"

	s.Create(repo, contracts.CreateSpawnEntryRequest{
		Name: "test", Type: contracts.SpawnEntryCommand, Command: "echo hi",
	})

	// Verify no temp files remain
	dir := filepath.Join(s.baseDir, repo)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("found leftover temp file: %s", e.Name())
		}
	}
}

func TestStore_LoadDeduplicatesExisting(t *testing.T) {
	dir := t.TempDir()
	repo := "test-repo"

	// Write a spawn-entries.json with duplicates directly.
	repoDir := filepath.Join(dir, repo)
	os.MkdirAll(repoDir, 0755)
	data := `[
		{"id":"p1","name":"skill-a","type":"skill","source":"emerged","state":"proposed","use_count":0},
		{"id":"p2","name":"skill-a","type":"skill","source":"emerged","state":"proposed","use_count":0},
		{"id":"p3","name":"skill-b","type":"skill","source":"emerged","state":"proposed","use_count":0}
	]`
	os.WriteFile(filepath.Join(repoDir, "spawn-entries.json"), []byte(data), 0644)

	s := NewStore(dir)
	entries, err := s.ListAll(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries after load-time dedup, got %d", len(entries))
	}
	if entries[0].Name != "skill-a" || entries[1].Name != "skill-b" {
		t.Errorf("unexpected entry names: %q, %q", entries[0].Name, entries[1].Name)
	}

	// Verify persisted: reload from disk
	s2 := NewStore(dir)
	entries2, _ := s2.ListAll(repo)
	if len(entries2) != 2 {
		t.Fatalf("expected 2 entries after reload, got %d", len(entries2))
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s := newTestStore(t)
	repo := "test-repo"

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	// 10 concurrent creates
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := s.Create(repo, contracts.CreateSpawnEntryRequest{
				Name:    "concurrent",
				Type:    contracts.SpawnEntryCommand,
				Command: "echo concurrent",
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}

	// 10 concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.List(repo)
			if err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent operation failed: %v", err)
	}

	// Verify all 10 entries were created
	entries, _ := s.List(repo)
	if len(entries) != 10 {
		t.Errorf("expected 10 entries, got %d", len(entries))
	}
}
