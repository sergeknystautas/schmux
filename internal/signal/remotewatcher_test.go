package signal

import (
	"strings"
	"sync"
	"testing"
)

func TestParseSentinelOutput(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{"valid", "__SCHMUX_SIGNAL__completed Done__END__", "completed Done"},
		{"with noise", "junk__SCHMUX_SIGNAL__error Fail__END__trailing", "error Fail"},
		{"no sentinel", "plain text", ""},
		{"no end", "__SCHMUX_SIGNAL__completed", ""},
		{"empty content", "__SCHMUX_SIGNAL____END__", ""},
		{"working", "__SCHMUX_SIGNAL__working__END__", "working"},
		{"end in message", "__SCHMUX_SIGNAL__completed Contains __END__ in text__END__", "completed Contains __END__ in text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSentinelOutput(tt.data)
			if got != tt.want {
				t.Errorf("ParseSentinelOutput(%q) = %q, want %q", tt.data, got, tt.want)
			}
		})
	}
}

func TestRemoteSignalWatcherProcessOutput(t *testing.T) {
	var got []Signal
	var mu sync.Mutex
	w := NewRemoteSignalWatcher("test", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})

	// First signal
	w.ProcessOutput("__SCHMUX_SIGNAL__completed Done__END__\n")
	mu.Lock()
	if len(got) != 1 || got[0].State != "completed" {
		t.Fatalf("expected 1 completed signal, got %v", got)
	}
	mu.Unlock()

	// Duplicate (should be deduplicated)
	w.ProcessOutput("__SCHMUX_SIGNAL__completed Done__END__\n")
	mu.Lock()
	if len(got) != 1 {
		t.Fatalf("expected dedup, got %d signals", len(got))
	}
	mu.Unlock()

	// Different signal
	w.ProcessOutput("__SCHMUX_SIGNAL__needs_input What?__END__\n")
	mu.Lock()
	if len(got) != 2 || got[1].State != "needs_input" {
		t.Fatalf("expected 2 signals, got %v", got)
	}
	mu.Unlock()
}

func TestWatcherScript(t *testing.T) {
	script := WatcherScript("/workspace/.schmux/signal")
	if script == "" {
		t.Fatal("WatcherScript returned empty string")
	}
	if !strings.Contains(script, "__SCHMUX_SIGNAL__") {
		t.Error("script missing sentinel prefix")
	}
	if !strings.Contains(script, "__END__") {
		t.Error("script missing sentinel suffix")
	}
	if !strings.Contains(script, "inotifywait") {
		t.Error("script missing inotifywait")
	}
	if !strings.Contains(script, "sleep 2") {
		t.Error("script missing polling fallback")
	}
}
