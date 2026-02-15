package difftool

import (
	"strings"
	"testing"
)

func TestUnifiedDiff_NoChange(t *testing.T) {
	result := UnifiedDiff("file.txt", []byte("hello\n"), []byte("hello\n"))
	if result != "" {
		t.Errorf("expected empty diff for identical content, got: %q", result)
	}
}

func TestUnifiedDiff_Addition(t *testing.T) {
	old := []byte("line1\n")
	new := []byte("line1\nline2\n")
	result := UnifiedDiff("file.txt", old, new)
	if !strings.Contains(result, "+line2") {
		t.Errorf("expected diff to contain +line2, got: %q", result)
	}
}

func TestUnifiedDiff_Deletion(t *testing.T) {
	old := []byte("line1\nline2\n")
	new := []byte("line1\n")
	result := UnifiedDiff("file.txt", old, new)
	if !strings.Contains(result, "-line2") {
		t.Errorf("expected diff to contain -line2, got: %q", result)
	}
}

func TestUnifiedDiff_Modification(t *testing.T) {
	old := []byte("line1\nold\nline3\n")
	new := []byte("line1\nnew\nline3\n")
	result := UnifiedDiff("file.txt", old, new)
	if !strings.Contains(result, "-old") || !strings.Contains(result, "+new") {
		t.Errorf("expected diff to contain -old and +new, got: %q", result)
	}
}

func TestUnifiedDiff_BinaryContent(t *testing.T) {
	old := []byte{0x00, 0x01, 0x02}
	new := []byte{0x00, 0x01, 0x03}
	result := UnifiedDiff("file.bin", old, new)
	if !strings.Contains(result, "Binary") {
		t.Errorf("expected binary file message, got: %q", result)
	}
}
