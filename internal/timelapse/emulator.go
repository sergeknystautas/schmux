package timelapse

import (
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

// CellGrid returns a fresh 2D rune grid with the given dimensions.
// Useful when the caller tracks width/height separately from the emulator.
func (e *ScreenEmulator) CellGrid(width, height int) [][]rune {
	grid := make([][]rune, height)
	for y := 0; y < height; y++ {
		row := make([]rune, width)
		for x := 0; x < width; x++ {
			if x < e.cols && y < e.rows {
				g := e.term.Cell(x, y)
				if g.Char == 0 {
					row[x] = ' '
				} else {
					row[x] = g.Char
				}
			} else {
				row[x] = ' '
			}
		}
		grid[y] = row
	}
	return grid
}
