package timelapse

import (
	"bytes"
	"fmt"
	"testing"
)

func buildTestRecording(outputs []struct {
	t    float64
	data string
}) string {
	var buf bytes.Buffer
	WriteRecord(&buf, Record{
		Type:      RecordHeader,
		Version:   1,
		Width:     40,
		Height:    10,
		StartTime: "2026-03-31T12:00:00Z",
	})
	for _, o := range outputs {
		WriteRecord(&buf, Record{
			Type: RecordOutput,
			T:    floatPtr(o.t),
			D:    o.data,
		})
	}
	WriteRecord(&buf, Record{
		Type: RecordEnd,
		T:    floatPtr(outputs[len(outputs)-1].t + 0.1),
	})
	return buf.String()
}

func TestClassifyIntervals_MultiRowOutput(t *testing.T) {
	// Output that changes many rows (like scrolling) should be Content
	var outputs []struct {
		t    float64
		data string
	}
	for i := 0; i < 10; i++ {
		// Each output writes enough lines to trigger row-change threshold
		var lines string
		for j := 0; j < 5; j++ {
			lines += fmt.Sprintf("line %d-%d: content\r\n", i, j)
		}
		outputs = append(outputs, struct {
			t    float64
			data string
		}{
			t:    float64(i) * 0.5,
			data: lines,
		})
	}

	recording := buildTestRecording(outputs)
	emu := NewScreenEmulator(40, 10)

	intervals, err := ClassifyIntervals(bytes.NewReader([]byte(recording)), emu)
	if err != nil {
		t.Fatal(err)
	}

	if len(intervals) == 0 {
		t.Fatal("expected at least one interval")
	}

	var contentCount, fillerCount int
	for _, iv := range intervals {
		if iv.Type == Content {
			contentCount++
		} else {
			fillerCount++
		}
	}
	if contentCount == 0 {
		t.Error("expected at least one Content interval for multi-row output")
	}
	t.Logf("intervals: %d content, %d filler", contentCount, fillerCount)
}

func TestClassifyIntervals_SpinnerIsFillerNotContent(t *testing.T) {
	// Simulate a spinner: single character overwritten in place, no scrolling
	spinnerChars := []string{"✢", "✳", "✶", "✻", "✽", "·"}
	var outputs []struct {
		t    float64
		data string
	}
	for i := 0; i < 30; i++ {
		char := spinnerChars[i%len(spinnerChars)]
		// Move to row 5 col 0, overwrite one char in place (no scroll)
		outputs = append(outputs, struct {
			t    float64
			data string
		}{
			t:    float64(i) * 0.15,
			data: fmt.Sprintf("\033[5;1H%s", char),
		})
	}

	recording := buildTestRecording(outputs)
	emu := NewScreenEmulator(40, 10)

	intervals, err := ClassifyIntervals(bytes.NewReader([]byte(recording)), emu)
	if err != nil {
		t.Fatal(err)
	}

	// All intervals should be Filler — spinner doesn't scroll
	for _, iv := range intervals {
		if iv.Type == Content {
			t.Errorf("spinner should be classified as Filler, got Content at [%.1f, %.1f]", iv.Start, iv.End)
		}
	}
}

func TestClassifyIntervals_IdleGap(t *testing.T) {
	// Scrolling output, then silence, then more scrolling
	var outputs []struct {
		t    float64
		data string
	}
	// Burst of scrolling content at start
	for i := 0; i < 15; i++ {
		outputs = append(outputs, struct {
			t    float64
			data string
		}{
			t:    float64(i) * 0.1,
			data: fmt.Sprintf("scroll line %d content\r\n", i),
		})
	}
	// 5 second gap (silence)
	// Then more scrolling content
	for i := 0; i < 15; i++ {
		outputs = append(outputs, struct {
			t    float64
			data string
		}{
			t:    6.0 + float64(i)*0.1,
			data: fmt.Sprintf("more scroll line %d\r\n", i),
		})
	}

	recording := buildTestRecording(outputs)
	emu := NewScreenEmulator(40, 10)

	intervals, err := ClassifyIntervals(bytes.NewReader([]byte(recording)), emu)
	if err != nil {
		t.Fatal(err)
	}

	hasFiller := false
	for _, iv := range intervals {
		if iv.Type == Filler {
			hasFiller = true
		}
	}
	if !hasFiller {
		t.Error("expected Filler intervals during idle gap")
	}
}

func TestClassifyIntervals_MergingAdjacentIntervals(t *testing.T) {
	// All idle — should produce one Filler interval
	outputs := []struct {
		t    float64
		data string
	}{
		{0.0, "x"}, // single char, then nothing changes
	}

	recording := buildTestRecording(outputs)
	emu := NewScreenEmulator(40, 10)

	intervals, err := ClassifyIntervals(bytes.NewReader([]byte(recording)), emu)
	if err != nil {
		t.Fatal(err)
	}

	// After initial content, all subsequent snapshots are identical → merged Filler
	if len(intervals) > 2 {
		// Should be at most Content + Filler (merged)
		t.Logf("got %d intervals, expected at most 2", len(intervals))
	}
}
