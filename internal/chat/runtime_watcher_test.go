package chat

import (
	"os"
	"testing"
)

func TestRuntime_WatchesOutputCreatedAfterSetup(t *testing.T) {
	p := PathsFor(t.TempDir())
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p.Output); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime("watch", mustProto(t), p, "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	sub, err := rt.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	rt.Start()
	f, err := os.OpenFile(p.Output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// Waiting for each delivered record ensures later appends exercise the
	// watcher after the startup drain, rather than just that initial read.
	for _, line := range []string{`{"type":"system","subtype":"init","session_id":"watch"}`, `{"type":"result","subtype":"success"}`} {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatal(err)
		}
		if got := recv(t, sub.Live); got.Type != RecordHarness || string(got.Line) != line {
			t.Fatalf("delivered %+v, want harness line %s", got, line)
		}
	}
}

func TestRuntime_WatcherCompletesPartialLine(t *testing.T) {
	rt, p := newTestRuntime(t)
	f, err := os.OpenFile(p.Output, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	const partial = `{"type":"result","subtype":`
	if _, err := f.WriteString(partial); err != nil {
		t.Fatal(err)
	}
	// Explicitly finish an attempted drain of the partial line before
	// completing it, so no timing assumption determines this boundary.
	rt.drain()
	sub, err := rt.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	rt.Start()
	if _, err := f.WriteString("\"success\"}\n"); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, sub.Live); got.Type != RecordHarness || string(got.Line) != partial+`"success"}` {
		t.Fatalf("completed line = %+v", got)
	}
	rt.Stop()
	records, err := rt.log.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || string(records[0].Line) != partial+`"success"}` {
		t.Fatalf("durable records = %+v; want the complete line exactly once", records)
	}
}
