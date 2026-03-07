package persona

import (
	"testing"
)

func TestManagerList(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	personas, err := mgr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(personas) != 0 {
		t.Errorf("expected empty list, got %d", len(personas))
	}
}

func TestManagerCreateAndGet(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	p := &Persona{
		ID: "test-persona", Name: "Test", Icon: "T",
		Color: "#fff", Prompt: "Be helpful.", BuiltIn: false,
	}
	if err := mgr.Create(p); err != nil {
		t.Fatalf("create error: %v", err)
	}

	got, err := mgr.Get("test-persona")
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	if got.Name != "Test" || got.Prompt != "Be helpful." {
		t.Errorf("unexpected persona: %+v", got)
	}
}

func TestManagerCreateDuplicate(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	p := &Persona{ID: "dup", Name: "Dup", Icon: "D", Color: "#000"}
	if err := mgr.Create(p); err != nil {
		t.Fatalf("first create error: %v", err)
	}
	if err := mgr.Create(p); err == nil {
		t.Fatal("expected error on duplicate create")
	}
}

func TestManagerUpdate(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	p := &Persona{ID: "upd", Name: "Original", Icon: "O", Color: "#000"}
	mgr.Create(p)

	p.Name = "Updated"
	if err := mgr.Update(p); err != nil {
		t.Fatalf("update error: %v", err)
	}
	got, _ := mgr.Get("upd")
	if got.Name != "Updated" {
		t.Errorf("Name = %q, want %q", got.Name, "Updated")
	}
}

func TestManagerDelete(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	p := &Persona{ID: "del", Name: "Del", Icon: "X", Color: "#000"}
	mgr.Create(p)

	if err := mgr.Delete("del"); err != nil {
		t.Fatalf("delete error: %v", err)
	}
	if _, err := mgr.Get("del"); err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestManagerGetNotFound(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	_, err := mgr.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing persona")
	}
}

func TestEnsureBuiltins(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	if err := mgr.EnsureBuiltins(); err != nil {
		t.Fatalf("ensure error: %v", err)
	}

	personas, err := mgr.List()
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(personas) != 11 {
		t.Errorf("expected 11 built-in personas, got %d", len(personas))
	}

	// Verify all are marked built_in
	for _, p := range personas {
		if !p.BuiltIn {
			t.Errorf("persona %q should be built_in", p.ID)
		}
	}
}

func TestEnsureBuiltinsSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	// First ensure
	mgr.EnsureBuiltins()

	// Modify one
	p, _ := mgr.Get("security-auditor")
	p.Prompt = "Custom prompt."
	mgr.Update(p)

	// Second ensure should not overwrite
	mgr.EnsureBuiltins()

	got, _ := mgr.Get("security-auditor")
	if got.Prompt != "Custom prompt." {
		t.Error("EnsureBuiltins overwrote user-modified persona")
	}
}

func TestResetBuiltIn(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	mgr.EnsureBuiltins()

	// Modify a built-in
	p, _ := mgr.Get("security-auditor")
	original := p.Prompt
	p.Prompt = "Modified prompt."
	mgr.Update(p)

	// Reset should restore original
	if err := mgr.ResetBuiltIn("security-auditor"); err != nil {
		t.Fatalf("reset error: %v", err)
	}

	got, _ := mgr.Get("security-auditor")
	if got.Prompt != original {
		t.Errorf("reset did not restore original prompt")
	}
}

func TestResetBuiltInNotBuiltIn(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	err := mgr.ResetBuiltIn("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-built-in persona")
	}
}
