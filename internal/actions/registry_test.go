package actions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	return NewRegistry(t.TempDir(), "test-repo")
}

func TestNewRegistry_LoadEmpty(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.Load(); err != nil {
		t.Fatalf("Load on empty dir: %v", err)
	}
	if got := r.List(contracts.ActionStatePinned); len(got) != 0 {
		t.Errorf("expected no pinned actions, got %d", len(got))
	}
}

func TestCreateAndGet(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}

	action, err := r.Create(contracts.CreateActionRequest{
		Name:     "Fix lint",
		Type:     contracts.ActionTypeAgent,
		Template: "Fix lint errors in {{path}}",
		Target:   "sonnet",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if action.ID == "" {
		t.Error("expected non-empty ID")
	}
	if action.Name != "Fix lint" {
		t.Errorf("name = %q, want %q", action.Name, "Fix lint")
	}
	if action.State != contracts.ActionStatePinned {
		t.Errorf("state = %q, want pinned", action.State)
	}
	if action.Source != contracts.ActionSourceManual {
		t.Errorf("source = %q, want manual", action.Source)
	}
	if action.Confidence != 1.0 {
		t.Errorf("confidence = %v, want 1.0", action.Confidence)
	}

	// Get by ID.
	got, ok := r.Get(action.ID)
	if !ok {
		t.Fatal("Get: not found")
	}
	if got.Name != "Fix lint" {
		t.Errorf("Get name = %q, want %q", got.Name, "Fix lint")
	}

	// Get missing ID.
	_, ok = r.Get("nonexistent")
	if ok {
		t.Error("expected Get to return false for missing ID")
	}
}

func TestList(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	r.Create(contracts.CreateActionRequest{Name: "A", Type: contracts.ActionTypeAgent})
	r.Create(contracts.CreateActionRequest{Name: "B", Type: contracts.ActionTypeAgent})

	pinned := r.List(contracts.ActionStatePinned)
	if len(pinned) != 2 {
		t.Errorf("expected 2 pinned, got %d", len(pinned))
	}

	proposed := r.List(contracts.ActionStateProposed)
	if len(proposed) != 0 {
		t.Errorf("expected 0 proposed, got %d", len(proposed))
	}
}

func TestPinAndDismiss(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	// Add a proposed action.
	r.AddProposed([]contracts.Action{
		{Name: "Emerging", Type: contracts.ActionTypeAgent, Confidence: 0.8},
	})

	proposed := r.List(contracts.ActionStateProposed)
	if len(proposed) != 1 {
		t.Fatalf("expected 1 proposed, got %d", len(proposed))
	}
	id := proposed[0].ID

	// Pin it.
	if err := r.Pin(id); err != nil {
		t.Fatalf("Pin: %v", err)
	}
	action, _ := r.Get(id)
	if action.State != contracts.ActionStatePinned {
		t.Errorf("state after pin = %q, want pinned", action.State)
	}
	if action.PinnedAt == nil {
		t.Error("pinned_at should be set")
	}

	// Pin again should fail (already pinned).
	if err := r.Pin(id); err == nil {
		t.Error("expected error pinning already-pinned action")
	}

	// Dismiss it.
	if err := r.Dismiss(id); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	action, _ = r.Get(id)
	if action.State != contracts.ActionStateDismissed {
		t.Errorf("state after dismiss = %q, want dismissed", action.State)
	}
}

func TestUpdate(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	action, _ := r.Create(contracts.CreateActionRequest{
		Name:     "Original",
		Type:     contracts.ActionTypeAgent,
		Template: "original template",
		Target:   "sonnet",
	})

	newName := "Updated"
	newTemplate := "updated template"
	if err := r.Update(action.ID, contracts.UpdateActionRequest{
		Name:     &newName,
		Template: &newTemplate,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _ := r.Get(action.ID)
	if got.Name != "Updated" {
		t.Errorf("name = %q, want %q", got.Name, "Updated")
	}
	if got.Template != "updated template" {
		t.Errorf("template = %q, want %q", got.Template, "updated template")
	}
	// Unchanged field should remain.
	if got.Target != "sonnet" {
		t.Errorf("target = %q, want %q", got.Target, "sonnet")
	}

	// Update missing action.
	if err := r.Update("nonexistent", contracts.UpdateActionRequest{Name: &newName}); err == nil {
		t.Error("expected error updating nonexistent action")
	}
}

func TestDelete(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	action, _ := r.Create(contracts.CreateActionRequest{Name: "ToDelete", Type: contracts.ActionTypeAgent})

	if err := r.Delete(action.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, ok := r.Get(action.ID)
	if ok {
		t.Error("expected action to be deleted")
	}

	// Delete missing.
	if err := r.Delete("nonexistent"); err == nil {
		t.Error("expected error deleting nonexistent action")
	}
}

func TestRecordUse(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	action, _ := r.Create(contracts.CreateActionRequest{Name: "Tracked", Type: contracts.ActionTypeAgent})

	if err := r.RecordUse(action.ID, false); err != nil {
		t.Fatalf("RecordUse: %v", err)
	}
	got, _ := r.Get(action.ID)
	if got.UseCount != 1 {
		t.Errorf("use_count = %d, want 1", got.UseCount)
	}
	if got.EditCount != 0 {
		t.Errorf("edit_count = %d, want 0", got.EditCount)
	}
	if got.LastUsed == nil {
		t.Error("last_used should be set")
	}

	// Use with edit.
	if err := r.RecordUse(action.ID, true); err != nil {
		t.Fatalf("RecordUse(edited): %v", err)
	}
	got, _ = r.Get(action.ID)
	if got.UseCount != 2 {
		t.Errorf("use_count = %d, want 2", got.UseCount)
	}
	if got.EditCount != 1 {
		t.Errorf("edit_count = %d, want 1", got.EditCount)
	}
}

func TestApplyDecay(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	action, _ := r.Create(contracts.CreateActionRequest{Name: "Stale", Type: contracts.ActionTypeAgent})

	// Manually set pinned_at to 60 days ago.
	r.mu.Lock()
	sixtyDaysAgo := time.Now().Add(-60 * 24 * time.Hour)
	for i := range r.actions {
		if r.actions[i].ID == action.ID {
			r.actions[i].PinnedAt = &sixtyDaysAgo
			r.actions[i].LastUsed = nil
		}
	}
	r.save()
	r.mu.Unlock()

	// Reload should apply decay.
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}

	got, _ := r.Get(action.ID)
	// 60 days = 2 periods of 30 days => decay = 0.2, new confidence = 0.8
	if got.Confidence != 0.8 {
		t.Errorf("confidence after 60-day decay = %v, want 0.8", got.Confidence)
	}
	if got.State != contracts.ActionStatePinned {
		t.Errorf("state = %q, should still be pinned (confidence >= 0.3)", got.State)
	}
}

func TestApplyDecay_AutoDismiss(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	action, _ := r.Create(contracts.CreateActionRequest{Name: "VeryStale", Type: contracts.ActionTypeAgent})

	// Set pinned_at to 300 days ago.
	r.mu.Lock()
	longAgo := time.Now().Add(-300 * 24 * time.Hour)
	for i := range r.actions {
		if r.actions[i].ID == action.ID {
			r.actions[i].PinnedAt = &longAgo
			r.actions[i].LastUsed = nil
		}
	}
	r.save()
	r.mu.Unlock()

	r.Load()

	got, _ := r.Get(action.ID)
	// 300 days = 10 periods => decay = 1.0, confidence = 0.0 < 0.3 => auto-dismissed
	if got.State != contracts.ActionStateDismissed {
		t.Errorf("state = %q, want dismissed (auto-decay)", got.State)
	}
}

func TestMigrateQuickLaunch(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	prompt := "Fix all lint errors"
	presets := []contracts.QuickLaunch{
		{Name: "Lint Fix", Target: "sonnet", Prompt: &prompt},
		{Name: "Build", Command: "go build ./..."},
	}

	count, err := r.MigrateQuickLaunch(presets)
	if err != nil {
		t.Fatalf("MigrateQuickLaunch: %v", err)
	}
	if count != 2 {
		t.Errorf("migrated %d, want 2", count)
	}

	pinned := r.List(contracts.ActionStatePinned)
	if len(pinned) != 2 {
		t.Fatalf("expected 2 pinned, got %d", len(pinned))
	}

	// Check agent action.
	var agent, cmd contracts.Action
	for _, a := range pinned {
		if a.Type == contracts.ActionTypeAgent {
			agent = a
		} else {
			cmd = a
		}
	}
	if agent.Template != "Fix all lint errors" {
		t.Errorf("agent template = %q", agent.Template)
	}
	if agent.Target != "sonnet" {
		t.Errorf("agent target = %q", agent.Target)
	}
	if agent.Source != contracts.ActionSourceMigrated {
		t.Errorf("agent source = %q", agent.Source)
	}
	if cmd.Command != "go build ./..." {
		t.Errorf("cmd command = %q", cmd.Command)
	}
	if cmd.Type != contracts.ActionTypeCommand {
		t.Errorf("cmd type = %q", cmd.Type)
	}
}

func TestMigrateQuickLaunch_Idempotent(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	prompt := "test"
	presets := []contracts.QuickLaunch{
		{Name: "Test", Prompt: &prompt},
	}

	count1, _ := r.MigrateQuickLaunch(presets)
	count2, _ := r.MigrateQuickLaunch(presets)

	if count1 != 1 {
		t.Errorf("first migration = %d, want 1", count1)
	}
	if count2 != 0 {
		t.Errorf("second migration = %d, want 0 (idempotent)", count2)
	}
}

func TestRemoveMigrated(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	// Add a migrated action via MigrateQuickLaunch.
	prompt := "run tests"
	_, _ = r.MigrateQuickLaunch([]contracts.QuickLaunch{
		{Name: "Run Tests", Target: "agent", Prompt: &prompt},
	})

	// Add a non-migrated action manually.
	_, _ = r.Create(contracts.CreateActionRequest{Name: "Manual Action", Type: contracts.ActionTypeAgent})

	// Should have 2 actions total.
	all := r.List(contracts.ActionStatePinned)
	if len(all) != 2 {
		t.Fatalf("expected 2 pinned actions before cleanup, got %d", len(all))
	}

	// Remove migrated.
	removed, err := r.RemoveMigrated()
	if err != nil {
		t.Fatalf("RemoveMigrated: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	// Only the manual action should remain.
	remaining := r.List(contracts.ActionStatePinned)
	if len(remaining) != 1 {
		t.Fatalf("expected 1 pinned action after cleanup, got %d", len(remaining))
	}
	if remaining[0].Name != "Manual Action" {
		t.Errorf("remaining action = %q, want %q", remaining[0].Name, "Manual Action")
	}
}

func TestRemoveMigrated_NoOp(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	// Add only non-migrated actions.
	_, _ = r.Create(contracts.CreateActionRequest{Name: "Manual", Type: contracts.ActionTypeAgent})

	removed, err := r.RemoveMigrated()
	if err != nil {
		t.Fatalf("RemoveMigrated: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0 (no migrated actions)", removed)
	}
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()

	// Create and populate registry.
	r1 := NewRegistry(dir, "test-repo")
	r1.Load()
	r1.Create(contracts.CreateActionRequest{Name: "Persistent", Type: contracts.ActionTypeAgent})

	// Load a second registry from the same directory.
	r2 := NewRegistry(dir, "test-repo")
	if err := r2.Load(); err != nil {
		t.Fatal(err)
	}

	pinned := r2.List(contracts.ActionStatePinned)
	if len(pinned) != 1 {
		t.Fatalf("expected 1 pinned after reload, got %d", len(pinned))
	}
	if pinned[0].Name != "Persistent" {
		t.Errorf("name = %q, want %q", pinned[0].Name, "Persistent")
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	// 10 concurrent creates.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := r.Create(contracts.CreateActionRequest{
				Name: "Concurrent",
				Type: contracts.ActionTypeAgent,
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}

	// 10 concurrent reads.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.List(contracts.ActionStatePinned)
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}

	pinned := r.List(contracts.ActionStatePinned)
	if len(pinned) != 10 {
		t.Errorf("expected 10 pinned actions, got %d", len(pinned))
	}
}

func TestAddProposed(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	actions := []contracts.Action{
		{Name: "Proposed 1", Type: contracts.ActionTypeAgent, Confidence: 0.7},
		{Name: "Proposed 2", Type: contracts.ActionTypeAgent, Confidence: 0.5},
	}
	if err := r.AddProposed(actions); err != nil {
		t.Fatalf("AddProposed: %v", err)
	}

	proposed := r.List(contracts.ActionStateProposed)
	if len(proposed) != 2 {
		t.Fatalf("expected 2 proposed, got %d", len(proposed))
	}
	for _, p := range proposed {
		if p.ID == "" {
			t.Error("proposed action should have generated ID")
		}
		if p.Source != contracts.ActionSourceEmerged {
			t.Errorf("source = %q, want emerged", p.Source)
		}
		if p.ProposedAt == nil {
			t.Error("proposed_at should be set")
		}
	}
}

func TestAtomicFileWrite(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	r.Create(contracts.CreateActionRequest{Name: "Atomic", Type: contracts.ActionTypeAgent})

	// Verify the file exists and is valid JSON.
	data, err := os.ReadFile(r.filePath())
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	var actions []contracts.Action
	if err := json.Unmarshal(data, &actions); err != nil {
		t.Fatalf("parse file: %v", err)
	}
	if len(actions) != 1 {
		t.Errorf("file has %d actions, want 1", len(actions))
	}

	// No temp files should remain.
	dir := filepath.Dir(r.filePath())
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("stale temp file: %s", e.Name())
		}
	}
}

func TestRecordUse_BoostsConfidence(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	// Add a proposed action at 0.5 confidence, then pin it.
	r.AddProposed([]contracts.Action{
		{Name: "Low Conf", Type: contracts.ActionTypeAgent, Confidence: 0.5},
	})
	proposed := r.List(contracts.ActionStateProposed)
	if len(proposed) != 1 {
		t.Fatalf("expected 1 proposed, got %d", len(proposed))
	}
	id := proposed[0].ID
	r.Pin(id)

	// Record use — should boost confidence by 0.1
	if err := r.RecordUse(id, false); err != nil {
		t.Fatalf("RecordUse: %v", err)
	}
	got, _ := r.Get(id)
	if got.Confidence != 0.6 {
		t.Errorf("confidence after 1 use = %v, want 0.6", got.Confidence)
	}

	// Record use again
	r.RecordUse(id, false)
	got, _ = r.Get(id)
	if got.Confidence != 0.7 {
		t.Errorf("confidence after 2 uses = %v, want 0.7", got.Confidence)
	}
}

func TestRecordUse_ConfidenceCappedAt1(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	action, _ := r.Create(contracts.CreateActionRequest{Name: "Full Conf", Type: contracts.ActionTypeAgent})

	// confidence starts at 1.0, using should not exceed 1.0
	r.RecordUse(action.ID, false)
	got, _ := r.Get(action.ID)
	if got.Confidence != 1.0 {
		t.Errorf("confidence = %v, want 1.0 (capped)", got.Confidence)
	}
}

func TestMaybeApplyDecay_TriggersAfterOneHour(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	action, _ := r.Create(contracts.CreateActionRequest{Name: "Stale2", Type: contracts.ActionTypeAgent})

	// Manually set pinned_at to 60 days ago.
	r.mu.Lock()
	sixtyDaysAgo := time.Now().Add(-60 * 24 * time.Hour)
	for i := range r.actions {
		if r.actions[i].ID == action.ID {
			r.actions[i].PinnedAt = &sixtyDaysAgo
			r.actions[i].LastUsed = nil
		}
	}
	// Set lastDecayCheck to 2 hours ago so maybeApplyDecay triggers.
	r.lastDecayCheck = time.Now().Add(-2 * time.Hour)
	r.save()
	r.mu.Unlock()

	// List() should trigger maybeApplyDecay, applying decay.
	pinned := r.List(contracts.ActionStatePinned)

	// Verify the action got decayed.
	var found bool
	for _, a := range pinned {
		if a.ID == action.ID {
			found = true
			if a.Confidence != 0.8 {
				t.Errorf("confidence = %v, want 0.8 after runtime decay", a.Confidence)
			}
		}
	}
	if !found {
		// Maybe auto-dismissed — check dismissed
		got, ok := r.Get(action.ID)
		if !ok {
			t.Fatal("action not found after decay")
		}
		if got.Confidence >= 1.0 {
			t.Errorf("confidence = %v, expected decay to have run", got.Confidence)
		}
	}
}

func TestMaybeApplyDecay_SkipsWhenRecent(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	action, _ := r.Create(contracts.CreateActionRequest{Name: "Fresh", Type: contracts.ActionTypeAgent})

	// Set pinned_at to 60 days ago but lastDecayCheck is recent (just now from Load).
	r.mu.Lock()
	sixtyDaysAgo := time.Now().Add(-60 * 24 * time.Hour)
	for i := range r.actions {
		if r.actions[i].ID == action.ID {
			r.actions[i].PinnedAt = &sixtyDaysAgo
			r.actions[i].LastUsed = nil
		}
	}
	r.save()
	r.mu.Unlock()

	// List() should NOT trigger decay because lastDecayCheck is recent.
	pinned := r.List(contracts.ActionStatePinned)
	for _, a := range pinned {
		if a.ID == action.ID {
			if a.Confidence != 1.0 {
				t.Errorf("confidence = %v, want 1.0 (decay should not have run)", a.Confidence)
			}
		}
	}
}
