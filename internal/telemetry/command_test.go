package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandTelemetry_Track(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "event.json")
	script := filepath.Join(dir, "capture.sh")
	os.WriteFile(script, []byte("#!/bin/sh\ncat > "+outFile+"\n"), 0755)

	ct := NewCommandTelemetry(script, "test-install-id", nil)
	ct.Track("daemon_started", map[string]any{"version": "1.0.0"})

	// Wait for fire-and-forget process to complete
	time.Sleep(500 * time.Millisecond)

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	var result map[string]map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if _, ok := result["int"]["time"]; !ok {
		t.Error("missing int.time")
	}

	normal := result["normal"]
	if normal["event"] != "daemon_started" {
		t.Errorf("event = %v, want daemon_started", normal["event"])
	}
	if normal["installation_id"] != "test-install-id" {
		t.Errorf("installation_id = %v, want test-install-id", normal["installation_id"])
	}
	if normal["version"] != "1.0.0" {
		t.Errorf("version = %v, want 1.0.0", normal["version"])
	}
}

func TestCommandTelemetry_TypeBucketing(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "event.json")
	script := filepath.Join(dir, "capture.sh")
	os.WriteFile(script, []byte("#!/bin/sh\ncat > "+outFile+"\n"), 0755)

	ct := NewCommandTelemetry(script, "test-id", nil)
	ct.Track("test", map[string]any{
		"str_val":   "hello",
		"int_val":   42,
		"float_val": 3.14,
		"bool_val":  true,
	})

	time.Sleep(500 * time.Millisecond)

	data, _ := os.ReadFile(outFile)
	var result map[string]map[string]any
	json.Unmarshal(data, &result)

	if result["normal"]["str_val"] != "hello" {
		t.Errorf("string should go to normal, got %v", result["normal"]["str_val"])
	}
	if result["int"]["int_val"] != float64(42) {
		t.Errorf("int should go to int bucket, got %v", result["int"]["int_val"])
	}
	if result["double"]["float_val"] != 3.14 {
		t.Errorf("float should go to double bucket, got %v", result["double"]["float_val"])
	}
	if result["int"]["bool_val"] != float64(1) {
		t.Errorf("bool true should be int 1, got %v", result["int"]["bool_val"])
	}
}

func TestCommandTelemetry_BadCommand(t *testing.T) {
	ct := NewCommandTelemetry("/nonexistent/command", "test-id", nil)
	ct.Track("test", nil) // should not panic
}

func TestCommandTelemetry_ShutdownIsNoop(t *testing.T) {
	ct := NewCommandTelemetry("echo", "test-id", nil)
	ct.Shutdown() // should not panic or block
}

func TestCommandTelemetry_OmitsEmptyBuckets(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "event.json")
	script := filepath.Join(dir, "capture.sh")
	os.WriteFile(script, []byte("#!/bin/sh\ncat > "+outFile+"\n"), 0755)

	ct := NewCommandTelemetry(script, "test-id", nil)
	ct.Track("test", map[string]any{"key": "val"})

	time.Sleep(500 * time.Millisecond)

	data, _ := os.ReadFile(outFile)
	if strings.Contains(string(data), "double") {
		t.Error("should not include empty double bucket")
	}
}
