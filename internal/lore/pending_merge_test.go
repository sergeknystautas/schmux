package lore

import (
	"os"
	"testing"
	"time"
)

func TestPendingMergeStore_SaveAndGet(t *testing.T) {
	dir := t.TempDir()
	store := NewPendingMergeStore(dir, nil)

	pm := &PendingMerge{
		Repo:           "schmux",
		Status:         PendingMergeStatusReady,
		BaseSHA:        "abc123",
		RuleIDs:        []string{"r1", "r2"},
		ProposalIDs:    []string{"prop-001", "prop-002"},
		MergedContent:  "merged instructions",
		CurrentContent: "current instructions",
		Summary:        "Added 2 rules",
		CreatedAt:      time.Now().UTC(),
	}

	if err := store.Save(pm); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := store.Get("schmux")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if loaded.Repo != "schmux" {
		t.Errorf("expected repo schmux, got %s", loaded.Repo)
	}
	if loaded.Status != PendingMergeStatusReady {
		t.Errorf("expected status ready, got %s", loaded.Status)
	}
	if loaded.BaseSHA != "abc123" {
		t.Errorf("expected base SHA abc123, got %s", loaded.BaseSHA)
	}
	if len(loaded.RuleIDs) != 2 {
		t.Errorf("expected 2 rule IDs, got %d", len(loaded.RuleIDs))
	}
	if len(loaded.ProposalIDs) != 2 {
		t.Errorf("expected 2 proposal IDs, got %d", len(loaded.ProposalIDs))
	}
	if loaded.MergedContent != "merged instructions" {
		t.Errorf("expected merged content, got %s", loaded.MergedContent)
	}
	if loaded.CurrentContent != "current instructions" {
		t.Errorf("expected current content, got %s", loaded.CurrentContent)
	}
	if loaded.Summary != "Added 2 rules" {
		t.Errorf("expected summary, got %s", loaded.Summary)
	}
	if loaded.EditedContent != nil {
		t.Errorf("expected nil edited content, got %v", loaded.EditedContent)
	}
}

func TestPendingMergeStore_GetNotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewPendingMergeStore(dir, nil)

	_, err := store.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent repo")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected not-exist error, got %v", err)
	}
}

func TestPendingMergeStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store := NewPendingMergeStore(dir, nil)

	pm := &PendingMerge{
		Repo:      "schmux",
		Status:    PendingMergeStatusReady,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Save(pm); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Verify it exists.
	if _, err := store.Get("schmux"); err != nil {
		t.Fatalf("get before delete failed: %v", err)
	}

	if err := store.Delete("schmux"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err := store.Get("schmux")
	if err == nil {
		t.Fatal("expected error after delete")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected not-exist error after delete, got %v", err)
	}
}

func TestPendingMergeStore_UpdateEdited(t *testing.T) {
	dir := t.TempDir()
	store := NewPendingMergeStore(dir, nil)

	pm := &PendingMerge{
		Repo:          "schmux",
		Status:        PendingMergeStatusReady,
		MergedContent: "original merged",
		CreatedAt:     time.Now().UTC(),
	}
	if err := store.Save(pm); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Before edit, EffectiveContent returns MergedContent.
	loaded, _ := store.Get("schmux")
	if loaded.EffectiveContent() != "original merged" {
		t.Errorf("expected effective content to be merged content, got %s", loaded.EffectiveContent())
	}

	// Update edited content.
	if err := store.UpdateEditedContent("schmux", "user edited version"); err != nil {
		t.Fatalf("update edited content failed: %v", err)
	}

	loaded, _ = store.Get("schmux")
	if loaded.EditedContent == nil {
		t.Fatal("expected non-nil edited content")
	}
	if *loaded.EditedContent != "user edited version" {
		t.Errorf("expected edited content, got %s", *loaded.EditedContent)
	}
	if loaded.EffectiveContent() != "user edited version" {
		t.Errorf("expected effective content to be edited content, got %s", loaded.EffectiveContent())
	}
}

func TestPendingMergeStore_Expired(t *testing.T) {
	dir := t.TempDir()
	store := NewPendingMergeStore(dir, nil)

	// Create a pending merge with a CreatedAt 25 hours ago.
	pm := &PendingMerge{
		Repo:      "schmux",
		Status:    PendingMergeStatusReady,
		CreatedAt: time.Now().UTC().Add(-25 * time.Hour),
	}
	if err := store.Save(pm); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := store.Get("schmux")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !loaded.IsExpired() {
		t.Error("expected pending merge to be expired")
	}

	// A fresh one should not be expired.
	fresh := &PendingMerge{
		Repo:      "fresh-repo",
		Status:    PendingMergeStatusReady,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Save(fresh); err != nil {
		t.Fatalf("save fresh failed: %v", err)
	}
	loadedFresh, _ := store.Get("fresh-repo")
	if loadedFresh.IsExpired() {
		t.Error("expected fresh pending merge to not be expired")
	}
}

func TestPendingMergeStore_InvalidateIfContainsRule(t *testing.T) {
	dir := t.TempDir()
	store := NewPendingMergeStore(dir, nil)

	pm := &PendingMerge{
		Repo:      "schmux",
		Status:    PendingMergeStatusReady,
		RuleIDs:   []string{"r1", "r2", "r3"},
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Save(pm); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Invalidating with a non-matching rule should keep it.
	if err := store.InvalidateIfContainsRule("schmux", "r99"); err != nil {
		t.Fatalf("invalidate non-matching failed: %v", err)
	}
	if _, err := store.Get("schmux"); err != nil {
		t.Fatal("expected pending merge to still exist after non-matching invalidation")
	}

	// Invalidating with a matching rule should delete it.
	if err := store.InvalidateIfContainsRule("schmux", "r2"); err != nil {
		t.Fatalf("invalidate matching failed: %v", err)
	}
	_, err := store.Get("schmux")
	if err == nil {
		t.Fatal("expected pending merge to be deleted after matching invalidation")
	}

	// Invalidating a nonexistent repo should not error.
	if err := store.InvalidateIfContainsRule("nonexistent", "r1"); err != nil {
		t.Fatalf("invalidate nonexistent repo should not error: %v", err)
	}
}
