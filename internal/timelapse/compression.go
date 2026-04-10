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
