package usage

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestPlanQuotaPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	m := NewManager(path, nil)
	m.Observe("anthropic", capturedReport(t, "claude/permission-allow.jsonl", ParseClaudePlanUsage))
	m.Observe("openai", capturedReport(t, "codex/stream.out.jsonl", ParseCodexPlanUsage))
	restored := NewManager(path, nil)
	restored.Load()
	got := restored.Snapshot()
	if len(got) != 2 || !reflect.DeepEqual(got, m.Snapshot()) {
		t.Fatalf("restored = %+v; stored = %+v", got, m.Snapshot())
	}
	for _, p := range got {
		if _, err := time.Parse(time.RFC3339Nano, p.UpdatedAt); err != nil {
			t.Fatalf("receipt time %q: %v", p.UpdatedAt, err)
		}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"input_tokens", "output_tokens", "reports", "session", "thread", "cumulative", "days", "cost_usd"} {
		if bytes.Contains(body, []byte(`"`+unwanted+`"`)) {
			t.Fatalf("unwanted %s: %s", unwanted, body)
		}
	}
}

func TestLatestQuotaReplacesWithoutSummingOrKeepingOldWindows(t *testing.T) {
	m := NewManager("", nil)
	m.Observe("anthropic", capturedReport(t, "claude/permission-allow.jsonl", ParseClaudePlanUsage))
	newer, ok := ParseClaudePlanUsage([]byte(`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{"five_hour":{"utilization":0.02,"resetsAt":1788624600}}}}`))
	if !ok {
		t.Fatal("quota rejected")
	}
	m.Observe("anthropic", newer)
	got := m.Snapshot()
	if len(got) != 1 || len(got[0].Windows) != 1 || *got[0].Windows[0].UsedPercent != 2 || got[0].Windows[0].ResetsAt != 1788624600 {
		t.Fatalf("latest quota = %+v", got)
	}
	// Callers cannot mutate the persisted manager state through shared pointers.
	*got[0].Windows[0].UsedPercent = 99
	if *m.Snapshot()[0].Windows[0].UsedPercent != 2 {
		t.Fatal("snapshot aliases store")
	}
}

func TestPersistenceFailurePreservesPreviousQuota(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(filepath.Join(dir, "usage.json"), nil)
	old := capturedReport(t, "claude/permission-allow.jsonl", ParseClaudePlanUsage)
	m.Observe("anthropic", old)
	before := m.Snapshot()
	m.dataPath = filepath.Join(dir, "missing", "usage.json")
	m.Observe("anthropic", capturedReport(t, "codex/stream.out.jsonl", ParseCodexPlanUsage))
	if !reflect.DeepEqual(m.Snapshot(), before) {
		t.Fatalf("failed write changed snapshot: %+v", m.Snapshot())
	}
}

func TestLoadRejectsTokenAccountingState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	if err := os.WriteFile(path, []byte(`{"providers":{"anthropic":{"input_tokens":10,"output_tokens":4}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(path, nil)
	m.Load()
	if len(m.Snapshot()) != 0 {
		t.Fatalf("loaded token accounting: %+v", m.Snapshot())
	}
	// The next real report replaces obsolete state; there is no migration.
	m.Observe("anthropic", capturedReport(t, "claude/permission-allow.jsonl", ParseClaudePlanUsage))
	restored := NewManager(path, nil)
	restored.Load()
	if len(restored.Snapshot()) != 1 {
		t.Fatalf("new quota not persisted: %+v", restored.Snapshot())
	}
}
