//go:build !notimelapse

package timelapse

import (
	"fmt"
	"os"
)

const (
	// scrollBeatDuration is the pause inserted before each scroll event
	// so it's visible during playback.
	scrollBeatDuration = 0.3
	// fillerEventDuration is the timestamp advance for non-scroll events
	// that change enough cells to be considered meaningful (e.g. in-place
	// content updates that don't scroll).
	fillerEventDuration = 0.001
	// cosmeticCellThreshold is the maximum number of changed cells for an
	// event to be classified as cosmetic. Cosmetic events (spinner animations,
	// timer updates, status line refreshes) get zero timestamp advance,
	// collapsing long idle/thinking periods to near-zero playback time.
	// Calibrated from real Claude Code recordings where 96%+ of filler
	// events change ≤10 cells (spinners: 1-3, timers: 2-5, status: 5-10).
	cosmeticCellThreshold = 10
)

// Exporter converts a full timelapse recording (.cast) to a compressed .cast file.
// All events are preserved (maintaining correct terminal state) but timestamps
// are rewritten: scroll moments get a visible pause, everything else is near-instant.
type Exporter struct {
	recordingPath string
	outputPath    string
	progressFn    func(pct float64)
}

// NewExporter creates an exporter for a recording.
func NewExporter(recordingPath, outputPath string, progressFn func(float64)) *Exporter {
	return &Exporter{
		recordingPath: recordingPath,
		outputPath:    outputPath,
		progressFn:    progressFn,
	}
}

// Export runs the single-pass time-compression pipeline.
func (e *Exporter) Export() error {
	header, err := e.readHeader()
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}

	width, height := header.Width, header.Height
	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}

	// Read all events
	e.reportProgress(0.1)
	f, err := os.Open(e.recordingPath)
	if err != nil {
		return fmt.Errorf("open recording: %w", err)
	}
	defer f.Close()

	var records []Record
	ReadCastEvents(f, func(rec Record) bool {
		records = append(records, rec)
		return true
	})

	e.reportProgress(0.3)

	// Create output
	outFile, err := os.OpenFile(e.outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer outFile.Close()

	cw, err := NewCastWriter(outFile, CastHeader{
		Width:  width,
		Height: height,
		Title:  header.RecordingID,
	})
	if err != nil {
		return fmt.Errorf("create cast writer: %w", err)
	}

	// Single-pass: feed each event to the emulator, check for scroll,
	// emit all events with compressed timestamps
	emu := NewScreenEmulator(width, height)
	prevGrid := emu.CellGrid(width, height)
	var compressedT float64
	totalEvents := 0

	for _, rec := range records {
		if rec.Type != RecordOutput {
			continue
		}
		totalEvents++
	}

	eventIdx := 0
	for _, rec := range records {
		if rec.Type == RecordResize {
			if rec.Width > 0 && rec.Height > 0 {
				emu.Resize(rec.Width, rec.Height)
				width, height = rec.Width, rec.Height
				prevGrid = emu.CellGrid(width, height)
				// Emit resize event as "r" type so the player can call term.resize()
				cw.WriteResize(compressedT, rec.Width, rec.Height)
			}
			continue
		}

		if rec.Type != RecordOutput {
			continue
		}
		eventIdx++

		// Feed to emulator
		emu.Write([]byte(rec.D))

		// Snapshot and classify: scroll > filler > cosmetic
		currGrid := emu.CellGrid(width, height)
		scrolled := detectScrollGrid(prevGrid, currGrid, width, height)

		if scrolled {
			compressedT += scrollBeatDuration
		} else if countChangedCells(prevGrid, currGrid, width, height) > cosmeticCellThreshold {
			compressedT += fillerEventDuration
		}
		// cosmetic events (≤threshold cells changed): no timestamp advance

		// Emit ALL events with compressed timestamp
		cw.WriteEvent(compressedT, rec.D)
		prevGrid = currGrid

		// Report progress
		if totalEvents > 0 && eventIdx%100 == 0 {
			e.reportProgress(0.3 + 0.7*float64(eventIdx)/float64(totalEvents))
		}
	}

	e.reportProgress(1.0)
	return nil
}

func (e *Exporter) readHeader() (Record, error) {
	f, err := os.Open(e.recordingPath)
	if err != nil {
		return Record{}, err
	}
	defer f.Close()

	var header Record
	ReadCastEvents(f, func(rec Record) bool {
		if rec.Type == RecordHeader {
			header = rec
			return false
		}
		return true
	})
	return header, nil
}

func (e *Exporter) reportProgress(pct float64) {
	if e.progressFn != nil {
		e.progressFn(pct)
	}
}

// detectScrollGrid checks if currGrid is a scrolled version of prevGrid.
func detectScrollGrid(prev, curr [][]rune, width, height int) bool {
	rows := height
	if len(prev) < rows {
		rows = len(prev)
	}
	if len(curr) < rows {
		rows = len(curr)
	}
	if rows < 3 {
		return false
	}

	for k := 1; k <= 5 && k < rows; k++ {
		matches := 0
		compared := 0
		for y := 0; y+k < rows; y++ {
			if rowBlank(prev[y+k]) {
				continue
			}
			if rowsEqualSlice(curr[y], prev[y]) {
				continue
			}
			compared++
			if rowsEqualSlice(curr[y], prev[y+k]) {
				matches++
			}
		}
		if compared >= 3 && float64(matches)/float64(compared) >= 0.4 {
			return true
		}
	}
	return false
}

func rowsEqualSlice(a, b []rune) bool {
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
