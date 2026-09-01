package logstream

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// receiveLine reads one line from the channel with a generous ceiling (used as
// a failure timeout only — not for waiting on success).
func receiveLine(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for line")
		return ""
	}
}

// expectNoLine asserts the channel remains empty for a short window.
func expectNoLine(t *testing.T, ch <-chan string) {
	t.Helper()
	select {
	case got := <-ch:
		t.Fatalf("unexpected line: %q", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func writeTestLog(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func appendTestLog(t *testing.T, path, contents string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(contents); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func TestStream_OwnsBothDirectionsFromOneSnapshot(t *testing.T) {
	path := writeTestLog(t, "old-1\nold-2\nold-3\n")
	appends := make(chan string, 4)
	s, err := New(path, 2, func(line []byte) { appends <- string(line) }, func(err error) {
		t.Errorf("unexpected stream error: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.mu.Lock()
	snapshot := s.historyBefore
	if s.liveOffset != snapshot {
		s.mu.Unlock()
		t.Fatalf("offsets start at %d and %d", snapshot, s.liveOffset)
	}
	s.mu.Unlock()

	appendTestLog(t, path, "live-1\n")
	if got := receiveLine(t, appends); got != "live-1" {
		t.Fatalf("append = %q", got)
	}

	page, err := s.LoadOlder()
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, page.Lines, "old-3", "old-2")

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.historyBefore >= snapshot {
		t.Fatalf("history boundary did not move backward (was %d, now %d)", snapshot, s.historyBefore)
	}
	if s.liveOffset <= snapshot {
		t.Fatalf("live offset did not move forward (was %d, now %d)", snapshot, s.liveOffset)
	}
}

func TestStream_AppendAfterSnapshotIsNotReturnedAsHistory(t *testing.T) {
	path := writeTestLog(t, "old\n")
	appends := make(chan string, 4)
	s, err := New(path, 5, func(line []byte) { appends <- string(line) }, func(err error) {
		t.Errorf("unexpected stream error: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	appendTestLog(t, path, "new\n")
	if got := receiveLine(t, appends); got != "new" {
		t.Fatalf("append = %q", got)
	}

	page, err := s.LoadOlder()
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, page.Lines, "old")
}

func TestStream_MissingFileStartsEmptyThenTailsCreation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "later.log")
	appends := make(chan string, 4)
	s, err := New(path, 5, func(line []byte) { appends <- string(line) }, func(err error) {
		t.Errorf("unexpected stream error: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	page, err := s.LoadOlder()
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Lines) != 0 {
		t.Fatalf("expected empty page, got %q", page.Lines)
	}

	if err := os.WriteFile(path, []byte("created\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := receiveLine(t, appends); got != "created" {
		t.Fatalf("append = %q", got)
	}
}

func TestStream_HistoryFailureDoesNotAdvanceBoundary(t *testing.T) {
	path := writeTestLog(t, "old-1\nold-2\nold-3\n")
	appends := make(chan string, 4)
	s, err := New(path, 2, func(line []byte) { appends <- string(line) }, func(err error) {
		// History errors are expected in this test.
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// First LoadOlder should succeed.
	first, err := s.LoadOlder()
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, first.Lines, "old-3", "old-2")

	s.mu.Lock()
	beforeFail := s.historyBefore
	s.mu.Unlock()

	// Replace the file with a directory so the next read fails.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadOlder(); err == nil {
		t.Fatal("expected error reading history from directory")
	}

	s.mu.Lock()
	if s.historyBefore != beforeFail {
		s.mu.Unlock()
		t.Fatalf("boundary changed after failure: was %d, now %d", beforeFail, s.historyBefore)
	}
	s.mu.Unlock()

	// Restore the file and Retry should return the oldest remaining line.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	retry, err := s.LoadOlder()
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, retry.Lines, "old-1")
}

func TestStream_CloseStopsCallbacks(t *testing.T) {
	path := writeTestLog(t, "old\n")
	appends := make(chan string, 4)
	s, err := New(path, 5, func(line []byte) { appends <- string(line) }, func(err error) {
		t.Errorf("unexpected stream error: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Subsequent appends must NOT deliver lines.
	appendTestLog(t, path, "ignored\n")
	expectNoLine(t, appends)
}

func TestStream_UnexpectedWatcherCloseReportsError(t *testing.T) {
	path := writeTestLog(t, "old\n")
	errorsCh := make(chan error, 1)
	s, err := New(path, 5, func([]byte) {}, func(err error) { errorsCh <- err })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Close only the watcher, leaving the Stream context active. The run loop
	// must report the unexpected watcher shutdown rather than silently leaving
	// the client marked Live.
	if err := s.fsw.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errorsCh:
		if err == nil {
			t.Fatal("watcher shutdown reported nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for watcher shutdown error")
	}
}

func TestStream_RejectsBadPageSize(t *testing.T) {
	path := writeTestLog(t, "x\n")
	_, err := New(path, 0, func(line []byte) {}, func(err error) {})
	if err == nil {
		t.Fatal("expected error for pageSize <= 0")
	}
}

func TestStream_PagedHistoryExhaustsAfterAllPagesLoaded(t *testing.T) {
	// Build a 250-line file. With pageSize=100 we should need 3 calls to drain.
	var b []byte
	for i := 0; i < 250; i++ {
		b = append(b, []byte("line-")...)
		// pad number
		num := []byte{byte('0' + i/100), byte('0' + (i/10)%10), byte('0' + i%10)}
		b = append(b, num...)
		b = append(b, '\n')
	}
	path := writeTestLog(t, string(b))

	s, err := New(path, 100, func(line []byte) {}, func(err error) {
		t.Errorf("unexpected stream error: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	page1, err := s.LoadOlder()
	if err != nil {
		t.Fatal(err)
	}
	if len(page1.Lines) != 100 {
		t.Fatalf("page1 size = %d, want 100", len(page1.Lines))
	}
	if !page1.HasMore {
		t.Fatal("page1 should report older history")
	}
	// Newest first: page1[0] should be "line-249".
	if string(page1.Lines[0]) != "line-249" {
		t.Fatalf("page1[0] = %q, want line-249", page1.Lines[0])
	}

	page2, err := s.LoadOlder()
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Lines) != 100 {
		t.Fatalf("page2 size = %d, want 100", len(page2.Lines))
	}
	if !page2.HasMore {
		t.Fatal("page2 should report older history")
	}
	if string(page2.Lines[99]) != "line-050" {
		t.Fatalf("page2[99] = %q, want line-050", page2.Lines[99])
	}

	page3, err := s.LoadOlder()
	if err != nil {
		t.Fatal(err)
	}
	if len(page3.Lines) != 50 {
		t.Fatalf("page3 size = %d, want 50", len(page3.Lines))
	}
	if page3.HasMore {
		t.Fatal("page3 should exhaust history")
	}
	if string(page3.Lines[49]) != "line-000" {
		t.Fatalf("page3[49] = %q, want line-000", page3.Lines[49])
	}
}

// TestStream_DoesNotInterleaveHistoryAndLiveLines asserts that an append
// that arrives after the snapshot is delivered as a live record and is NOT
// also returned by LoadOlder (which only walks the snapshot's "before"
// boundary).
func TestStream_DoesNotInterleaveHistoryAndLiveLines(t *testing.T) {
	path := writeTestLog(t, "")
	appends := make(chan string, 4)
	s, err := New(path, 5, func(line []byte) {
		appends <- string(line)
	}, func(err error) {
		t.Errorf("unexpected stream error: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	appendTestLog(t, path, "post-snap\n")
	if got := receiveLine(t, appends); got != "post-snap" {
		t.Fatalf("append = %q", got)
	}

	page, err := s.LoadOlder()
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Lines) != 0 {
		t.Fatalf("history page must be empty; got %q", page.Lines)
	}
}
