package spawnlog

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func TestAppendRoundTrip(t *testing.T) {
	schmuxdir.Set(t.TempDir())
	rec := contracts.SpawnLogRecord{TS: "t", Prompt: "p", Status: "ok"}
	if err := Append(rec); err != nil {
		t.Fatalf("append: %v", err)
	}
	path, _ := SourcePath("spawn")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got contracts.SpawnLogRecord
	if err := json.Unmarshal(bytes.TrimSpace(data), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Prompt != "p" {
		t.Errorf("prompt = %q, want p", got.Prompt)
	}
}

func TestSourcePath(t *testing.T) {
	if _, ok := SourcePath("spawn"); !ok {
		t.Error("spawn should be a known source")
	}
	if _, ok := SourcePath("nope"); ok {
		t.Error("nope should be unknown")
	}
}

func TestDeriveStatus(t *testing.T) {
	cases := []struct {
		name string
		in   []contracts.SpawnLogResult
		want string
	}{
		{"empty", nil, "failed"},
		{"all ok", []contracts.SpawnLogResult{{SessionID: "a"}}, "ok"},
		{"all failed", []contracts.SpawnLogResult{{Error: "x"}}, "failed"},
		{"mixed", []contracts.SpawnLogResult{{SessionID: "a"}, {Error: "x"}}, "partial"},
	}
	for _, c := range cases {
		if got := DeriveStatus(c.in); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}
