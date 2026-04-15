//go:build !notimelapse

package timelapse

// IntervalType classifies a time interval in the recording.
type IntervalType int

const (
	Content IntervalType = iota
	Filler
)

// Interval represents a classified time range in the recording.
type Interval struct {
	Type  IntervalType
	Start float64
	End   float64
}

// rowBlank returns true if a row contains only spaces.
func rowBlank(row []rune) bool {
	for _, r := range row {
		if r != ' ' && r != 0 {
			return false
		}
	}
	return true
}

// countChangedCells returns the number of cells that differ between two grids.
func countChangedCells(prev, curr [][]rune, width, height int) int {
	changed := 0
	for y := 0; y < height && y < len(prev) && y < len(curr); y++ {
		for x := 0; x < width && x < len(prev[y]) && x < len(curr[y]); x++ {
			if prev[y][x] != curr[y][x] {
				changed++
			}
		}
	}
	return changed
}
