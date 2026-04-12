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

func TestStore_ListExcludesProposedAndDismissed(t *testing.T) {
	s := newTestStore(t)
	repo := "test-repo"

	// Create a pinned entry
	s.Create(repo, contracts.CreateSpawnEntryRequest{
		Name: "pinned", Type: contracts.SpawnEntryCommand, Command: "echo pinned",
	})

	// Add proposed entries
	s.AddProposed(repo, []contracts.SpawnEntry{
		{ID: "prop-1", Name: "proposed", Type: contracts.SpawnEntrySkill, SkillRef: "test"},
	})

	// List should only return pinned
	entries, err := s.List(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Name != "pinned" {
		t.Errorf("expected 'pinned', got %q", entries[0].Name)
	}
}

func TestStore_ListAll(t *testing.T) {
	s := newTestStore(t)
	repo := "test-repo"

	s.Create(repo, contracts.CreateSpawnEntryRequest{
		Name: "pinned", Type: contracts.SpawnEntryCommand, Command: "echo 1",
	})
	s.AddProposed(repo, []contracts.SpawnEntry{
		{ID: "prop-1", Name: "proposed", Type: contracts.SpawnEntrySkill, Source: contracts.SpawnSourceEmerged, State: contracts.SpawnStateProposed},
	})

	entries, err := s.ListAll(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
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

func TestStore_Pin(t *testing.T) {
	s := newTestStore(t)
	repo := "test-repo"

	s.AddProposed(repo, []contracts.SpawnEntry{
		{ID: "prop-1", Name: "proposed skill", Type: contracts.SpawnEntrySkill, Source: contracts.SpawnSourceEmerged, State: contracts.SpawnStateProposed},
	})

	err := s.Pin(repo, "prop-1")
	if err != nil {
		t.Fatal(err)
	}

	got, ok, _ := s.Get(repo, "prop-1")
	if !ok {
		t.Fatal("entry not found after pin")
	}
	if got.State != contracts.SpawnStatePinned {
		t.Errorf("expected state pinned, got %q", got.State)
	}
}

func TestStore_Dismiss(t *testing.T) {
	s := newTestStore(t)
	repo := "test-repo"

	s.AddProposed(repo, []contracts.SpawnEntry{
		{ID: "prop-1", Name: "proposed skill", Type: contracts.SpawnEntrySkill, Source: contracts.SpawnSourceEmerged, State: contracts.SpawnStateProposed},
	})

	err := s.Dismiss(repo, "prop-1")
	if err != nil {
		t.Fatal(err)
	}

	got, ok, _ := s.Get(repo, "prop-1")
	if !ok {
		t.Fatal("entry not found after dismiss")
	}
	if got.State != contracts.SpawnStateDismissed {
		t.Errorf("expected state dismissed, got %q", got.State)
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

func TestStore_AddProposed(t *testing.T) {
	s := newTestStore(t)
	repo := "test-repo"

	proposed := []contracts.SpawnEntry{
		{ID: "p1", Name: "skill-a", Type: contracts.SpawnEntrySkill, SkillRef: "a"},
		{ID: "p2", Name: "skill-b", Type: contracts.SpawnEntrySkill, SkillRef: "b"},
	}
	err := s.AddProposed(repo, proposed)
	if err != nil {
		t.Fatal(err)
	}

	entries, _ := s.ListAll(repo)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.State != contracts.SpawnStateProposed {
			t.Errorf("expected state proposed, got %q for %s", e.State, e.Name)
		}
		if e.Source != contracts.SpawnSourceEmerged {
			t.Errorf("expected source emerged, got %q for %s", e.Source, e.Name)
		}
	}
}

func TestStore_AddProposed_DeduplicatesExisting(t *testing.T) {
	s := newTestStore(t)
	repo := "test-repo"

	// Create a pinned entry
	s.Create(repo, contracts.CreateSpawnEntryRequest{
		Name: "existing-skill", Type: contracts.SpawnEntrySkill,
	})
	// Add a proposed entry
	s.AddProposed(repo, []contracts.SpawnEntry{
		{ID: "p1", Name: "proposed-skill", Type: contracts.SpawnEntrySkill},
	})

	// Try to add entries that overlap with existing pinned and proposed
	err := s.AddProposed(repo, []contracts.SpawnEntry{
		{ID: "p2", Name: "existing-skill", Type: contracts.SpawnEntrySkill}, // duplicate of pinned
		{ID: "p3", Name: "proposed-skill", Type: contracts.SpawnEntrySkill}, // duplicate of proposed
		{ID: "p4", Name: "new-skill", Type: contracts.SpawnEntrySkill},      // new
		{ID: "p5", Name: "new-skill", Type: contracts.SpawnEntrySkill},      // intra-batch duplicate
	})
	if err != nil {
		t.Fatal(err)
	}

	entries, _ := s.ListAll(repo)
	// Should have: existing-skill (pinned), proposed-skill (proposed), new-skill (proposed)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	names := make(map[string]bool)
	for _, e := range entries {
		names[e.Name] = true
	}
	for _, want := range []string{"existing-skill", "proposed-skill", "new-skill"} {
		if !names[want] {
			t.Errorf("expected entry %q to exist", want)
		}
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
