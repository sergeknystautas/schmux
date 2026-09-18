package chat

import (
	"os"
	"testing"
)

func TestRuntime_UsageCallbackReceivesHarnessRecords(t *testing.T) {
	rt, paths := newTestRuntime(t)
	records := make(chan Record, 1)
	rt.SetUsageCallback(func(record Record) {
		select {
		case records <- record:
		default:
		}
	})
	rt.Start()

	output, err := os.OpenFile(paths.Output, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := output.WriteString(`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{"five_hour":{"utilization":0.27}}}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}

	record := recv(t, records)
	if record.Type != RecordHarness {
		t.Fatalf("callback record type = %q", record.Type)
	}
	if record.Ts == "" || len(record.Line) == 0 {
		t.Fatalf("callback record = %+v", record)
	}
}
