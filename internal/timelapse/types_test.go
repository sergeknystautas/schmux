package timelapse

import (
	"strings"
	"testing"
)

func TestReadCastEvents_HeaderAndOutputs(t *testing.T) {
	input := `{"version":2,"width":80,"height":24,"timestamp":1743393600,"env":{"TERM":"xterm-256color"}}
[0.000000,"o","hello"]
[0.500000,"o","world"]
`
	var records []Record
	err := ReadCastEvents(strings.NewReader(input), func(rec Record) bool {
		records = append(records, rec)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	if records[0].Type != RecordHeader {
		t.Errorf("record 0 type = %q, want header", records[0].Type)
	}
	if records[0].Width != 80 || records[0].Height != 24 {
		t.Errorf("header dims = %dx%d, want 80x24", records[0].Width, records[0].Height)
	}
	if records[0].Version != 2 {
		t.Errorf("header version = %d, want 2", records[0].Version)
	}
	if records[1].Type != RecordOutput {
		t.Errorf("record 1 type = %q, want output", records[1].Type)
	}
	if records[1].D != "hello" {
		t.Errorf("record 1 data = %q, want hello", records[1].D)
	}
	if records[1].T == nil || *records[1].T != 0.0 {
		t.Errorf("record 1 T = %v, want 0.0", records[1].T)
	}
	if records[2].D != "world" {
		t.Errorf("record 2 data = %q, want world", records[2].D)
	}
	if records[2].T == nil || *records[2].T != 0.5 {
		t.Errorf("record 2 T = %v, want 0.5", records[2].T)
	}
}

func TestReadCastEvents_ResizeEvent(t *testing.T) {
	input := `{"version":2,"width":80,"height":24}
[1.500000,"r","120x40"]
`
	var records []Record
	err := ReadCastEvents(strings.NewReader(input), func(rec Record) bool {
		records = append(records, rec)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[1].Type != RecordResize {
		t.Errorf("record 1 type = %q, want resize", records[1].Type)
	}
	if records[1].Width != 120 || records[1].Height != 40 {
		t.Errorf("resize = %dx%d, want 120x40", records[1].Width, records[1].Height)
	}
	if records[1].T == nil || *records[1].T != 1.5 {
		t.Errorf("record 1 T = %v, want 1.5", records[1].T)
	}
}

func TestReadCastEvents_StopEarly(t *testing.T) {
	input := `{"version":2,"width":80,"height":24}
[0.000000,"o","a"]
[1.000000,"o","b"]
[2.000000,"o","c"]
`
	var count int
	err := ReadCastEvents(strings.NewReader(input), func(rec Record) bool {
		count++
		return count < 2 // stop after header + first output
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestReadCastEvents_EscapedData(t *testing.T) {
	// Data with special characters that are JSON-escaped
	input := `{"version":2,"width":80,"height":24}
[0.000000,"o","hello\nworld\ttab"]
`
	var records []Record
	err := ReadCastEvents(strings.NewReader(input), func(rec Record) bool {
		records = append(records, rec)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	// JSON unmarshal should decode the escapes
	if records[1].D != "hello\nworld\ttab" {
		t.Errorf("data = %q, want %q", records[1].D, "hello\nworld\ttab")
	}
}

func TestRecordStruct_DIsString(t *testing.T) {
	// Verify that Record.D is a string type (can hold arbitrary bytes as Go string)
	rec := Record{
		Type: RecordOutput,
		T:    func() *float64 { v := 0.0; return &v }(),
		D:    "hello\x00world", // arbitrary bytes in Go string
	}
	if rec.D != "hello\x00world" {
		t.Errorf("D = %q, want hello\\x00world", rec.D)
	}
}
