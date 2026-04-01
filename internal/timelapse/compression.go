package timelapse

import (
	"io"
)

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

const snapshotIntervalSec = 0.5 // snapshot every 500ms

// minScrollMatchRatio is the fraction of rows that must match a shifted
// version of the previous snapshot for the interval to be classified as
// Content (scroll detected). Agent TUIs repaint many rows in place for
// spinners/status, but scrolling events shift most rows up by k lines.
const minScrollMatchRatio = 0.4

// ClassifyIntervals reads a recording, replays through the emulator,
// snapshots every 500ms, and returns the compression map.
// Adjacent same-type intervals are merged.
func ClassifyIntervals(recording io.Reader, emulator *ScreenEmulator) ([]Interval, error) {
	var records []Record
	err := ReadRecords(recording, func(rec Record) bool {
		records = append(records, rec)
		return true
	})
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, nil
	}

	// Find the time range of the recording
	var maxT float64
	for _, rec := range records {
		if rec.T != nil && *rec.T > maxT {
			maxT = *rec.T
		}
	}

	if maxT <= 0 {
		return nil, nil
	}

	// Generate snapshots at regular intervals
	type snapshot struct {
		t    float64
		grid [][]rune
	}

	var snapshots []snapshot
	recIdx := 0

	for t := 0.0; t <= maxT; t += snapshotIntervalSec {
		// Replay all records up to time t
		for recIdx < len(records) {
			rec := records[recIdx]
			if rec.T == nil {
				recIdx++
				continue // skip header/end records with no timestamp
			}
			if *rec.T > t {
				break
			}
			switch rec.Type {
			case RecordOutput:
				emulator.Write([]byte(rec.D))
			case RecordResize:
				if rec.Width > 0 && rec.Height > 0 {
					emulator.Resize(rec.Width, rec.Height)
				}
			}
			recIdx++
		}

		grid := emulator.CellText()
		snapshots = append(snapshots, snapshot{t: t, grid: grid})
	}

	if len(snapshots) < 2 {
		return []Interval{{Type: Content, Start: 0, End: maxT}}, nil
	}

	// Compare consecutive snapshots and classify intervals
	var intervals []Interval
	for i := 1; i < len(snapshots); i++ {
		prev := snapshots[i-1]
		curr := snapshots[i]

		iType := Filler
		if detectScroll(prev.grid, curr.grid) {
			iType = Content
		}

		// Merge with previous interval if same type
		if len(intervals) > 0 && intervals[len(intervals)-1].Type == iType {
			intervals[len(intervals)-1].End = curr.t
		} else {
			intervals = append(intervals, Interval{
				Type:  iType,
				Start: prev.t,
				End:   curr.t,
			})
		}
	}

	return intervals, nil
}

// detectScroll checks if the new grid is a scrolled version of the old grid.
// For each shift k (1..maxShift), it counts how many rows in the new grid
// match rows k positions down in the old grid. If enough rows match,
// a scroll event is detected.
func detectScroll(prev, curr [][]rune) bool {
	rows := len(prev)
	if len(curr) < rows {
		rows = len(curr)
	}
	if rows < 3 {
		return false
	}

	const maxShift = 5
	for k := 1; k <= maxShift && k < rows; k++ {
		matches := 0
		compared := 0
		for y := 0; y+k < rows; y++ {
			// Skip blank rows — two empty rows matching isn't scroll evidence
			if rowBlank(prev[y+k]) {
				continue
			}
			// Only count rows that actually MOVED — if curr[y] already
			// equals prev[y] (row unchanged), matching prev[y+k] is just
			// a coincidence of repeated content, not a scroll.
			if rowsEqual(curr[y], prev[y]) {
				continue
			}
			compared++
			if rowsEqual(curr[y], prev[y+k]) {
				matches++
			}
		}
		// Need both a good match ratio AND enough actually-moved rows
		if compared >= 3 && float64(matches)/float64(compared) >= minScrollMatchRatio {
			return true
		}
	}
	return false
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

// rowsEqual returns true if two rows have identical rune content.
func rowsEqual(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// countChangedRows returns the number of rows that have at least one changed cell.
func countChangedRows(a, b [][]rune) int {
	rows := len(a)
	if len(b) < rows {
		rows = len(b)
	}
	changed := 0
	for y := 0; y < rows; y++ {
		cols := len(a[y])
		if len(b[y]) < cols {
			cols = len(b[y])
		}
		for x := 0; x < cols; x++ {
			if a[y][x] != b[y][x] {
				changed++
				break // one changed cell is enough to count this row
			}
		}
	}
	// Extra rows in either grid count as changed
	if len(a) != len(b) {
		changed += abs(len(a) - len(b))
	}
	return changed
}

// countChangedCells returns the number of cells that differ between two grids.
func countChangedCells(a, b [][]rune) int {
	changed := 0
	rows := len(a)
	if len(b) < rows {
		rows = len(b)
	}
	for y := 0; y < rows; y++ {
		cols := len(a[y])
		if len(b[y]) < cols {
			cols = len(b[y])
		}
		for x := 0; x < cols; x++ {
			if a[y][x] != b[y][x] {
				changed++
			}
		}
	}
	// Count extra rows/cols as changes
	if len(a) != len(b) {
		changed += abs(len(a)-len(b)) * maxCols(a, b)
	}
	return changed
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func maxCols(a, b [][]rune) int {
	max := 0
	for _, row := range a {
		if len(row) > max {
			max = len(row)
		}
	}
	for _, row := range b {
		if len(row) > max {
			max = len(row)
		}
	}
	return max
}
