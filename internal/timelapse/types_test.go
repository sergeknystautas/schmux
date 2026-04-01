package timelapse

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRecordSerialization_Header(t *testing.T) {
	rec := Record{
		Type:        RecordHeader,
		Version:     1,
		RecordingID: "abc123",
		SessionID:   "s1",
		Width:       80,
		Height:      24,
		StartTime:   "2026-03-31T12:00:00Z",
	}
	var buf bytes.Buffer
	if err := WriteRecord(&buf, rec); err != nil {
		t.Fatal(err)
	}

	// Verify NDJSON (ends with newline)
	line := buf.String()
	if !strings.HasSuffix(line, "\n") {
		t.Error("record should end with newline")
	}

	// Roundtrip
	var got Record
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != RecordHeader || got.Version != 1 || got.RecordingID != "abc123" {
		t.Errorf("header roundtrip failed: %+v", got)
	}
	// T should be nil for header
	if got.T != nil {
		t.Errorf("header T should be nil, got %v", *got.T)
	}
}

func TestRecordSerialization_OutputAtT0(t *testing.T) {
	// Critical test: t=0.0 must NOT be omitted by omitempty
	rec := Record{
		Type: RecordOutput,
		T:    floatPtr(0.0),
		Seq:  0,
		D:    "hello",
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}

	// Verify "t":0 is present in the JSON
	if !strings.Contains(string(data), `"t":0`) {
		t.Errorf("t=0.0 was omitted from JSON: %s", data)
	}

	// Roundtrip
	var got Record
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.T == nil {
		t.Fatal("T should not be nil after roundtrip")
	}
	if *got.T != 0.0 {
		t.Errorf("T = %f, want 0.0", *got.T)
	}
}

func TestRecordSerialization_Gap(t *testing.T) {
	snapshot := "$ cursor here"
	rec := Record{
		Type:     RecordGap,
		T:        floatPtr(5.2),
		Reason:   "buffer_overrun",
		LostSeqs: [2]uint64{10, 20},
		Snapshot: &snapshot,
	}
	var buf bytes.Buffer
	if err := WriteRecord(&buf, rec); err != nil {
		t.Fatal(err)
	}

	var got Record
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Reason != "buffer_overrun" {
		t.Errorf("Reason = %q, want buffer_overrun", got.Reason)
	}
	if got.LostSeqs != [2]uint64{10, 20} {
		t.Errorf("LostSeqs = %v, want [10 20]", got.LostSeqs)
	}
	if got.Snapshot == nil || *got.Snapshot != "$ cursor here" {
		t.Errorf("Snapshot = %v, want '$ cursor here'", got.Snapshot)
	}
}

func TestRecordSerialization_Resize(t *testing.T) {
	rec := Record{
		Type:   RecordResize,
		T:      floatPtr(1.5),
		Width:  120,
		Height: 40,
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var got Record
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Width != 120 || got.Height != 40 {
		t.Errorf("resize = %dx%d, want 120x40", got.Width, got.Height)
	}
}

func TestRecordSerialization_End(t *testing.T) {
	rec := Record{
		Type: RecordEnd,
		T:    floatPtr(300.5),
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var got Record
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != RecordEnd || got.T == nil || *got.T != 300.5 {
		t.Errorf("end roundtrip: %+v", got)
	}
}

func TestReadRecords(t *testing.T) {
	input := `{"type":"header","version":1,"recordingId":"r1","sessionId":"s1","width":80,"height":24,"startTime":"2026-03-31T12:00:00Z"}
{"type":"output","t":0,"seq":0,"d":"hello"}
{"type":"output","t":0.5,"seq":1,"d":"world"}
{"type":"end","t":1.0}
`
	var records []Record
	err := ReadRecords(strings.NewReader(input), func(rec Record) bool {
		records = append(records, rec)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 4 {
		t.Fatalf("expected 4 records, got %d", len(records))
	}
	if records[0].Type != RecordHeader {
		t.Errorf("record 0 type = %q, want header", records[0].Type)
	}
	if records[1].D != "hello" {
		t.Errorf("record 1 data = %q, want hello", records[1].D)
	}
	if records[3].Type != RecordEnd {
		t.Errorf("record 3 type = %q, want end", records[3].Type)
	}
}

func TestReadRecords_StopEarly(t *testing.T) {
	input := `{"type":"output","t":0,"seq":0,"d":"a"}
{"type":"output","t":1,"seq":1,"d":"b"}
{"type":"output","t":2,"seq":2,"d":"c"}
`
	var count int
	err := ReadRecords(strings.NewReader(input), func(rec Record) bool {
		count++
		return count < 2 // stop after first
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}
