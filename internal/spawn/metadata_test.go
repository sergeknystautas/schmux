package spawn

import (
	"testing"
)

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
