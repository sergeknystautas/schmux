//go:build !noautolearn

package autolearn

import (
	"testing"
	"time"

	"github.com/charmbracelet/log"
)

func newTestStore(t *testing.T) *BatchStore {
	t.Helper()
	return NewBatchStore(t.TempDir(), log.Default())
}

func makeBatch(id, repo string, createdAt time.Time, learnings ...Learning) *Batch {
	return &Batch{
		ID:        id,
		Repo:      repo,
		CreatedAt: createdAt,
		Status:    BatchPending,
		Learnings: learnings,
	}
}

func makeLearning(id string, status LearningStatus, title string) Learning {
	return Learning{
		ID:     id,
		Kind:   KindRule,
		Status: status,
		Title:  title,
	}
}

func TestBatchStore(t *testing.T) {
	t.Run("Save and Get round-trip", func(t *testing.T) {
		store := newTestStore(t)
		b := makeBatch("batch-1", "myrepo", time.Now(),
			makeLearning("l1", StatusPending, "Use gofmt"),
		)
		if err := store.Save(b); err != nil {
			t.Fatalf("Save: %v", err)
		}
		got, err := store.Get("myrepo", "batch-1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.ID != "batch-1" {
			t.Errorf("ID = %q, want %q", got.ID, "batch-1")
		}
		if got.Repo != "myrepo" {
			t.Errorf("Repo = %q, want %q", got.Repo, "myrepo")
		}
		if len(got.Learnings) != 1 {
			t.Fatalf("len(Learnings) = %d, want 1", len(got.Learnings))
		}
		if got.Learnings[0].Title != "Use gofmt" {
			t.Errorf("Learning.Title = %q, want %q", got.Learnings[0].Title, "Use gofmt")
		}
	})

	t.Run("List returns batches sorted by CreatedAt descending", func(t *testing.T) {
		store := newTestStore(t)
		now := time.Now()
		b1 := makeBatch("old", "myrepo", now.Add(-2*time.Hour))
		b2 := makeBatch("mid", "myrepo", now.Add(-1*time.Hour))
		b3 := makeBatch("new", "myrepo", now)
		for _, b := range []*Batch{b1, b3, b2} { // save out of order
			if err := store.Save(b); err != nil {
				t.Fatalf("Save: %v", err)
			}
		}
		list, err := store.List("myrepo")
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(list) != 3 {
			t.Fatalf("len(List) = %d, want 3", len(list))
		}
		want := []string{"new", "mid", "old"}
		for i, b := range list {
			if b.ID != want[i] {
				t.Errorf("list[%d].ID = %q, want %q", i, b.ID, want[i])
			}
		}
	})

	t.Run("UpdateStatus changes batch status", func(t *testing.T) {
		store := newTestStore(t)
		b := makeBatch("batch-1", "myrepo", time.Now())
		if err := store.Save(b); err != nil {
			t.Fatalf("Save: %v", err)
		}
		if err := store.UpdateStatus("myrepo", "batch-1", BatchApplied); err != nil {
			t.Fatalf("UpdateStatus: %v", err)
		}
		got, err := store.Get("myrepo", "batch-1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status != BatchApplied {
			t.Errorf("Status = %q, want %q", got.Status, BatchApplied)
		}
	})

	t.Run("UpdateLearning changes learning fields", func(t *testing.T) {
		store := newTestStore(t)
		b := makeBatch("batch-1", "myrepo", time.Now(),
			makeLearning("l1", StatusPending, "Original title"),
		)
		if err := store.Save(b); err != nil {
			t.Fatalf("Save: %v", err)
		}

		newStatus := StatusApproved
		newTitle := "Updated title"
		chosenLayer := LayerRepoPrivate
		err := store.UpdateLearning("myrepo", "batch-1", "l1", LearningUpdate{
			Status:      &newStatus,
			Title:       &newTitle,
			ChosenLayer: &chosenLayer,
		})
		if err != nil {
			t.Fatalf("UpdateLearning: %v", err)
		}

		got, err := store.Get("myrepo", "batch-1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		l := got.Learnings[0]
		if l.Status != StatusApproved {
			t.Errorf("Status = %q, want %q", l.Status, StatusApproved)
		}
		if l.Title != "Updated title" {
			t.Errorf("Title = %q, want %q", l.Title, "Updated title")
		}
		if l.ChosenLayer == nil || *l.ChosenLayer != LayerRepoPrivate {
			t.Errorf("ChosenLayer = %v, want %q", l.ChosenLayer, LayerRepoPrivate)
		}
	})

	t.Run("PendingLearningTitles returns only pending titles", func(t *testing.T) {
		store := newTestStore(t)
		b := makeBatch("batch-1", "myrepo", time.Now(),
			makeLearning("l1", StatusPending, "Pending one"),
			makeLearning("l2", StatusApproved, "Approved one"),
			makeLearning("l3", StatusPending, "Pending two"),
			makeLearning("l4", StatusDismissed, "Dismissed one"),
		)
		if err := store.Save(b); err != nil {
			t.Fatalf("Save: %v", err)
		}
		titles := store.PendingLearningTitles("myrepo")
		if len(titles) != 2 {
			t.Fatalf("len(titles) = %d, want 2", len(titles))
		}
		want := map[string]bool{"Pending one": true, "Pending two": true}
		for _, title := range titles {
			if !want[title] {
				t.Errorf("unexpected pending title: %q", title)
			}
		}
	})

	t.Run("DismissedLearningTitles returns only dismissed titles", func(t *testing.T) {
		store := newTestStore(t)
		b := makeBatch("batch-1", "myrepo", time.Now(),
			makeLearning("l1", StatusPending, "Pending one"),
			makeLearning("l2", StatusDismissed, "Dismissed one"),
			makeLearning("l3", StatusDismissed, "Dismissed two"),
		)
		if err := store.Save(b); err != nil {
			t.Fatalf("Save: %v", err)
		}
		titles := store.DismissedLearningTitles("myrepo")
		if len(titles) != 2 {
			t.Fatalf("len(titles) = %d, want 2", len(titles))
		}
		want := map[string]bool{"Dismissed one": true, "Dismissed two": true}
		for _, title := range titles {
			if !want[title] {
				t.Errorf("unexpected dismissed title: %q", title)
			}
		}
	})

	t.Run("Get returns error for nonexistent batch", func(t *testing.T) {
		store := newTestStore(t)
		_, err := store.Get("myrepo", "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent batch, got nil")
		}
	})
}
