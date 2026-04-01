package timelapse

import (
	"strings"
	"testing"
)

func TestEmulator_WriteAndCellText(t *testing.T) {
	emu := NewScreenEmulator(10, 3)
	emu.Write([]byte("hello"))

	grid := emu.CellText()
	if len(grid) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(grid))
	}
	if len(grid[0]) != 10 {
		t.Fatalf("expected 10 cols, got %d", len(grid[0]))
	}

	row0 := string(grid[0])
	if !strings.HasPrefix(row0, "hello") {
		t.Errorf("row 0 = %q, want prefix 'hello'", row0)
	}
}

func TestEmulator_ScreenText(t *testing.T) {
	emu := NewScreenEmulator(20, 3)
	emu.Write([]byte("line1\r\nline2\r\nline3"))

	text := emu.ScreenText()
	lines := strings.Split(text, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "line1" {
		t.Errorf("line 0 = %q, want 'line1'", lines[0])
	}
	if lines[1] != "line2" {
		t.Errorf("line 1 = %q, want 'line2'", lines[1])
	}
}

func TestEmulator_Resize(t *testing.T) {
	emu := NewScreenEmulator(80, 24)
	emu.Resize(120, 40)

	cols, rows := emu.Size()
	if cols != 120 || rows != 40 {
		t.Errorf("size = %dx%d, want 120x40", cols, rows)
	}

	grid := emu.CellText()
	if len(grid) != 40 {
		t.Errorf("grid rows = %d, want 40", len(grid))
	}
}

func TestEmulator_RenderKeyframe(t *testing.T) {
	emu := NewScreenEmulator(10, 3)
	emu.Write([]byte("ABC"))

	kf := emu.RenderKeyframe()
	// Should start with clear screen
	if !strings.HasPrefix(kf, "\033[2J\033[H") {
		t.Error("keyframe should start with clear screen sequence")
	}
	// Should contain the text
	if !strings.Contains(kf, "ABC") {
		t.Error("keyframe should contain 'ABC'")
	}
	// Should end with cursor position restore
	if !strings.Contains(kf, "\033[") {
		t.Error("keyframe should contain cursor position escape")
	}
}

func TestEmulator_ClearScreen(t *testing.T) {
	emu := NewScreenEmulator(20, 5)
	emu.Write([]byte("before clear"))

	// ED2 (clear entire screen) is commonly used by TUI apps
	emu.Write([]byte("\033[2J\033[Hafter clear"))

	text := emu.ScreenText()
	if !strings.Contains(text, "after clear") {
		t.Error("screen should show content after clear")
	}
	if strings.Contains(text, "before clear") {
		t.Error("screen should not show content from before clear")
	}
}

func TestEmulator_Reset(t *testing.T) {
	emu := NewScreenEmulator(20, 3)
	emu.Write([]byte("before reset"))
	emu.Reset()

	text := emu.ScreenText()
	if strings.Contains(text, "before") {
		t.Error("reset should clear all content")
	}
}
