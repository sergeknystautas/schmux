package signal

import (
	"fmt"
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

func TestRemoteSignalWatcherConcurrent(t *testing.T) {
	var mu sync.Mutex
	var got []Signal
	w := NewRemoteSignalWatcher("test", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Alternate between different signal states to trigger dedup logic
			state := "completed"
			if i%2 == 0 {
				state = "needs_input"
			}
			data := fmt.Sprintf("__SCHMUX_SIGNAL__%s msg-%d__END__\n", state, i)
			w.ProcessOutput(data)
		}(i)
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	// At minimum we should get at least 1 signal (first call always fires).
	// The exact count depends on goroutine scheduling.
	if len(got) == 0 {
		t.Fatal("expected at least 1 signal from concurrent calls")
	}
}

func TestWatcherScriptInjection(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "dollar sign", path: "/workspace/$HOME/signal"},
		{name: "backtick", path: "/workspace/`whoami`/signal"},
		{name: "single quote", path: "/workspace/it's/signal"},
		{name: "semicolon", path: "/workspace/a;rm -rf /;b/signal"},
		{name: "combined", path: "/workspace/$`';/signal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := WatcherScript(tt.path)

			// The path must be single-quoted in the script to prevent injection
			quoted := "'" + strings.ReplaceAll(tt.path, "'", "'\\''") + "'"
			if !strings.Contains(script, "STATUS_FILE="+quoted) {
				t.Errorf("WatcherScript(%q) does not properly quote the path\nScript: %s", tt.path, script)
			}
		})
	}
}
