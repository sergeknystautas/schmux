package directhttp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"
)

func TestProbeOllama_ReturnsModelList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"models":[{"name":"llama3.2:latest"},{"name":"qwen2.5-coder:7b"}]}`)
	}))
	defer server.Close()

	ids, err := ProbeOllama(server.URL, 2*time.Second)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	sort.Strings(ids)
	want := []string{"llama3.2:latest", "qwen2.5-coder:7b"}
	if len(ids) != 2 || ids[0] != want[0] || ids[1] != want[1] {
		t.Errorf("got %v, want %v", ids, want)
	}
}

func TestProbeOllama_ErrorReturnsEmpty(t *testing.T) {
	ids, err := ProbeOllama("http://127.0.0.1:1/", 100*time.Millisecond)
	if err == nil {
		t.Error("expected error")
	}
	if len(ids) != 0 {
		t.Errorf("expected empty slice on error, got %v", ids)
	}
}

func TestOllamaRegistry_UpdateAndSnapshot(t *testing.T) {
	reg := newOllamaRegistry()
	reg.Update([]string{"m1", "m2"})
	got := reg.Snapshot()
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestStartOllamaProbeLoop_CallsRefreshImmediatelyAndOnTick(t *testing.T) {
	var calls int
	fakeProbe := func() { calls++ }
	stop := make(chan struct{})
	go LoopOllamaProbe(10*time.Millisecond, fakeProbe, stop)
	time.Sleep(35 * time.Millisecond)
	close(stop)
	if calls < 3 {
		t.Errorf("expected >=3 calls, got %d", calls)
	}
}
