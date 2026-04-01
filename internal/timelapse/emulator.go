package timelapse

import (
	"fmt"
	"strings"

	"github.com/hinshun/vt10x"
)

// ScreenEmulator wraps vt10x to provide terminal screen state inspection.
type ScreenEmulator struct {
	term vt10x.Terminal
	cols int
	rows int
}

// NewScreenEmulator creates a terminal emulator with the given dimensions.
func NewScreenEmulator(cols, rows int) *ScreenEmulator {
	term := vt10x.New(vt10x.WithSize(cols, rows))
	return &ScreenEmulator{
		term: term,
		cols: cols,
		rows: rows,
	}
}

// Write feeds terminal output bytes to the emulator.
func (e *ScreenEmulator) Write(data []byte) {
	e.term.Write(data)
}

// Resize changes the terminal dimensions.
func (e *ScreenEmulator) Resize(cols, rows int) {
	e.cols = cols
	e.rows = rows
	e.term.Resize(cols, rows)
}

// CellText returns the screen grid as a 2D rune array (text only, no attributes).
// Used for classification diffing.
func (e *ScreenEmulator) CellText() [][]rune {
	grid := make([][]rune, e.rows)
	for y := 0; y < e.rows; y++ {
		row := make([]rune, e.cols)
		for x := 0; x < e.cols; x++ {
			g := e.term.Cell(x, y)
			if g.Char == 0 {
				row[x] = ' '
			} else {
				row[x] = g.Char
			}
		}
		grid[y] = row
	}
	return grid
}

// ScreenText returns the visible screen content as a string (rows joined by newlines).
func (e *ScreenEmulator) ScreenText() string {
	grid := e.CellText()
	lines := make([]string, len(grid))
	for i, row := range grid {
		lines[i] = strings.TrimRight(string(row), " ")
	}
	return strings.Join(lines, "\n")
}

// RenderKeyframe returns ANSI sequences that reproduce the current screen state.
// Used to replace filler intervals in the export: clear screen + full redraw.
func (e *ScreenEmulator) RenderKeyframe() string {
	var buf strings.Builder
	buf.WriteString("\033[2J\033[H")

	for y := 0; y < e.rows; y++ {
		if y > 0 {
			buf.WriteString("\r\n")
		}
		for x := 0; x < e.cols; x++ {
			g := e.term.Cell(x, y)
			if g.Char == 0 {
				buf.WriteByte(' ')
			} else {
				buf.WriteRune(g.Char)
			}
		}
	}

	cursor := e.term.Cursor()
	buf.WriteString(fmt.Sprintf("\033[%d;%dH", cursor.Y+1, cursor.X+1))

	return buf.String()
}

// Size returns the current terminal dimensions.
func (e *ScreenEmulator) Size() (cols, rows int) {
	return e.cols, e.rows
}

// Reset clears all terminal state.
func (e *ScreenEmulator) Reset() {
	e.term = vt10x.New(vt10x.WithSize(e.cols, e.rows))
}
