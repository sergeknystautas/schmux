package style

import "testing"

func TestManagerList(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	styles, err := mgr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(styles) != 0 {
		t.Errorf("expected empty list, got %d", len(styles))
	}
}

func TestManagerCreateAndGet(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	s := &Style{
		ID: "test-style", Name: "Test", Icon: "T",
		Tagline: "A test", Prompt: "Be helpful.", BuiltIn: false,
	}
	if err := mgr.Create(s); err != nil {
		t.Fatalf("create error: %v", err)
	}
	got, err := mgr.Get("test-style")
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	if got.Name != "Test" || got.Prompt != "Be helpful." || got.Tagline != "A test" {
		t.Errorf("unexpected style: %+v", got)
	}
}

func TestManagerCreateDuplicate(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	s := &Style{ID: "dup", Name: "Dup", Icon: "D", Tagline: "Dup"}
	if err := mgr.Create(s); err != nil {
		t.Fatalf("first create error: %v", err)
	}
	if err := mgr.Create(s); err == nil {
		t.Fatal("expected error on duplicate create")
	}
}

func TestManagerUpdate(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	s := &Style{ID: "upd", Name: "Original", Icon: "O", Tagline: "T"}
	mgr.Create(s)
	s.Name = "Updated"
	if err := mgr.Update(s); err != nil {
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
	s := &Style{ID: "del", Name: "Del", Icon: "X", Tagline: "T"}
	mgr.Create(s)
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
		t.Fatal("expected error for missing style")
	}
}

func TestEnsureBuiltins(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	if err := mgr.EnsureBuiltins(); err != nil {
		t.Fatalf("ensure error: %v", err)
	}
	styles, err := mgr.List()
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(styles) != 25 {
		t.Errorf("expected 25 built-in styles, got %d", len(styles))
	}
	for _, s := range styles {
		if !s.BuiltIn {
			t.Errorf("style %q should be built_in", s.ID)
		}
	}
}

func TestEnsureBuiltinsSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureBuiltins()
	s, _ := mgr.Get("pirate")
	s.Prompt = "Custom prompt."
	mgr.Update(s)
	mgr.EnsureBuiltins()
	got, _ := mgr.Get("pirate")
	if got.Prompt != "Custom prompt." {
		t.Error("EnsureBuiltins overwrote user-modified style")
	}
}

func TestResetBuiltIn(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureBuiltins()
	s, _ := mgr.Get("pirate")
	original := s.Prompt
	s.Prompt = "Modified prompt."
	mgr.Update(s)
	if err := mgr.ResetBuiltIn("pirate"); err != nil {
		t.Fatalf("reset error: %v", err)
	}
	got, _ := mgr.Get("pirate")
	if got.Prompt != original {
		t.Errorf("reset did not restore original prompt")
	}
}

func TestResetBuiltInNotBuiltIn(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	err := mgr.ResetBuiltIn("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-built-in style")
	}
}
