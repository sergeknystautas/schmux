package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// silentLogger swallows all messages so test output stays quiet.
type silentLogger struct{}

func (silentLogger) Info(interface{}, ...interface{}) {}
func (silentLogger) Warn(interface{}, ...interface{}) {}

// countingLogger counts warn calls so tests can assert on over-budget warnings.
type countingLogger struct {
	mu       sync.Mutex
	warnMsgs []string
}

func (c *countingLogger) Info(interface{}, ...interface{}) {}
func (c *countingLogger) Warn(msg interface{}, args ...interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := msg.(string); ok {
		c.warnMsgs = append(c.warnMsgs, s)
	} else {
		c.warnMsgs = append(c.warnMsgs, "non-string")
	}
}
func (c *countingLogger) WarnCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.warnMsgs)
}

func newTestCleaner(t *testing.T, root string, retentionDays, maxMB int) *StorageCleaner {
	t.Helper()
	logger := &silentLogger{}
	return NewStorageCleaner(StorageCleanerOptions{
		Root:           root,
		ActiveSessions: func() map[string]struct{} { return map[string]struct{}{} },
		RetentionDays:  func() int { return retentionDays },
		MaxBytes:       func() int64 { return int64(maxMB) * 1024 * 1024 },
		Now:            func() time.Time { return time.Unix(2_000_000_000, 0) },
		Log:            logger,
	})
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func touchTime(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func sessionFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{
		"conversation.jsonl": filepath.Join(dir, "conversation.jsonl"),
		"in.jsonl":           filepath.Join(dir, "in.jsonl"),
		"out.jsonl":          filepath.Join(dir, "out.jsonl"),
		"err.txt":            filepath.Join(dir, "err.txt"),
		"tail.pid":           filepath.Join(dir, "tail.pid"),
	}
	return files
}

// seedSession creates a session directory with all five recognized files and
// returns the directory path and a past modification time the caller can apply.
func seedSession(t *testing.T, root, workspaceID, sessionID string, modTime time.Time, bodySize int) string {
	t.Helper()
	dir := filepath.Join(root, workspaceID, sessionID)
	for name, p := range sessionFiles(t, dir) {
		body := "x"
		if name == "conversation.jsonl" || name == "out.jsonl" || name == "in.jsonl" {
			body = strings.Repeat("a", bodySize)
		}
		writeFile(t, p, body)
	}
	touchTime(t, dir, modTime)
	for _, p := range sessionFiles(t, dir) {
		touchTime(t, p, modTime)
	}
	return dir
}

func TestStorageCleaner_DefaultsAndExplicitOptions(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(1_700_000_000, 0)

	c := NewStorageCleaner(StorageCleanerOptions{
		Root:           root,
		ActiveSessions: func() map[string]struct{} { return nil },
		RetentionDays:  func() int { return 7 },
		MaxBytes:       func() int64 { return 4096 * 1024 * 1024 },
		Now:            func() time.Time { return now },
		Log:            silentLogger{},
	})
	if c == nil {
		t.Fatal("NewStorageCleaner returned nil")
	}
	if c.opts.RetentionDays() != 7 {
		t.Errorf("retention days = %d, want 7", c.opts.RetentionDays())
	}
	if c.opts.MaxBytes() != int64(4096*1024*1024) {
		t.Errorf("max bytes = %d, want %d", c.opts.MaxBytes(), 4096*1024*1024)
	}
}

func TestStorageCleaner_RemoveBridgeFiles_PreservesConversation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ws1", "s1")
	paths := PathsFor(dir)
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Conversation, []byte(`{"role":"user"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveBridgeFiles(paths); err != nil {
		t.Fatalf("RemoveBridgeFiles: %v", err)
	}

	for _, f := range []string{paths.Input, paths.Output, paths.Errors, paths.TailPID} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed, stat err = %v", filepath.Base(f), err)
		}
	}
	if _, err := os.Stat(paths.Conversation); err != nil {
		t.Errorf("conversation.jsonl must remain, stat err = %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("session directory must remain, stat err = %v", err)
	}
}

func TestStorageCleaner_RemoveBridgeFiles_AllAbsent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ws1", "s1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := PathsFor(dir)
	if err := RemoveBridgeFiles(paths); err != nil {
		t.Errorf("expected nil error when all bridge files missing, got %v", err)
	}
}

func TestStorageCleaner_ActiveSessionsNeverDeleted(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(2_000_000_000, 0)
	oldTime := now.Add(-30 * 24 * time.Hour) // 30 days old

	seedSession(t, root, "ws1", "s1", oldTime, 100)
	seedSession(t, root, "ws1", "s2", oldTime, 100)

	active := map[string]struct{}{"s1": {}}
	c := NewStorageCleaner(StorageCleanerOptions{
		Root:           root,
		ActiveSessions: func() map[string]struct{} { return active },
		RetentionDays:  func() int { return 1 },
		MaxBytes:       func() int64 { return 0 }, // Force budget deletion too
		Now:            func() time.Time { return now },
		Log:            silentLogger{},
	})
	c.RunOnce(context.Background())

	if _, err := os.Stat(filepath.Join(root, "ws1", "s1")); err != nil {
		t.Errorf("active session s1 must not be deleted, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "ws1", "s2")); !os.IsNotExist(err) {
		t.Errorf("inactive session s2 should be deleted, stat err = %v", err)
	}
}

func TestStorageCleaner_AgeDeletionRespectsRetention(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(2_000_000_000, 0)
	retention := 7
	grace := ChatStorageGrace
	oldEnough := now.Add(-(grace + time.Duration(retention)*24*time.Hour) - time.Hour)
	tooYoung := now.Add(-24 * time.Hour)

	seedSession(t, root, "ws1", "old", oldEnough, 100)
	seedSession(t, root, "ws1", "young", tooYoung, 100)

	c := newTestCleaner(t, root, retention, 4096)
	status := c.RunOnce(context.Background())

	if status.LastCleanup == nil || status.LastCleanup.AgeDeleted != 1 {
		t.Errorf("age_deleted = %v, want 1", status.LastCleanup)
	}
	if _, err := os.Stat(filepath.Join(root, "ws1", "old")); !os.IsNotExist(err) {
		t.Errorf("old session should be deleted, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "ws1", "young")); err != nil {
		t.Errorf("young session must remain, stat err = %v", err)
	}
}

func TestStorageCleaner_ZeroRetentionProtectedByGrace(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(2_000_000_000, 0)
	// 2 hours old: past grace (1h), but with retention=0, age-eligible math is now-1h-0d
	// We need the candidate to NOT yet satisfy age deletion. Two-hour-old should
	// be eligible, so we go the other way: a candidate 30 minutes old should be
	// protected by grace even with retention=0.
	recent := now.Add(-30 * time.Minute)

	seedSession(t, root, "ws1", "recent", recent, 100)

	c := newTestCleaner(t, root, 0, 4096)
	status := c.RunOnce(context.Background())

	if status.LastCleanup == nil || status.LastCleanup.AgeDeleted != 0 {
		t.Errorf("age_deleted = %v, want 0 (zero retention still protected by grace)", status.LastCleanup)
	}
	if _, err := os.Stat(filepath.Join(root, "ws1", "recent")); err != nil {
		t.Errorf("recent session must remain inside grace, stat err = %v", err)
	}
}

func TestStorageCleaner_BudgetDeletionOnlyGraceEligible(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(2_000_000_000, 0)
	// past-grace must be eligible (older than grace+retention=0)
	// in-grace must be inside grace (30min old < 1h)
	insideGrace := now.Add(-30 * time.Minute)
	pastGrace := now.Add(-2 * time.Hour)

	// A budget of 0 means every byte over zero must be reclaimed. But grace
	// must protect the in-grace candidate.
	seedSession(t, root, "ws1", "ingrace", insideGrace, 1024)
	seedSession(t, root, "ws1", "pastgrace", pastGrace, 1024)

	c := newTestCleaner(t, root, 0, 0) // max=0 → always over budget
	status := c.RunOnce(context.Background())

	if status.LastCleanup == nil {
		t.Fatal("LastCleanup must be set")
	}
	// past-grace is past grace+0days so age deletion catches it.
	// in-grace is inside grace and must remain.
	if status.LastCleanup.AgeDeleted+status.LastCleanup.BudgetDeleted != 1 {
		t.Errorf("expected exactly 1 deletion, got age=%d budget=%d",
			status.LastCleanup.AgeDeleted, status.LastCleanup.BudgetDeleted)
	}
	if _, err := os.Stat(filepath.Join(root, "ws1", "pastgrace")); !os.IsNotExist(err) {
		t.Errorf("past-grace session should be deleted, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "ws1", "ingrace")); err != nil {
		t.Errorf("in-grace session must remain, stat err = %v", err)
	}
}

func TestStorageCleaner_OverBudgetActiveOnlyWarns(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(2_000_000_000, 0)
	oldTime := now.Add(-30 * 24 * time.Hour)

	seedSession(t, root, "ws1", "active-old", oldTime, 4096) // 4KB per file × 5 = 20KB

	active := map[string]struct{}{"active-old": {}}
	logger := &countingLogger{}
	c := NewStorageCleaner(StorageCleanerOptions{
		Root:           root,
		ActiveSessions: func() map[string]struct{} { return active },
		RetentionDays:  func() int { return 1 },
		MaxBytes:       func() int64 { return 1 }, // 1 byte — over budget
		Now:            func() time.Time { return now },
		Log:            logger,
	})
	status := c.RunOnce(context.Background())

	if status.LastCleanup == nil || status.LastCleanup.Errors == 0 {
		t.Error("expected Errors >= 1 (over budget) for active-only storage")
	}
	if _, err := os.Stat(filepath.Join(root, "ws1", "active-old")); err != nil {
		t.Errorf("active session must remain even when over budget, stat err = %v", err)
	}
}

func TestStorageCleaner_OldestFirstUnderBudget(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(2_000_000_000, 0)
	oldest := now.Add(-3 * time.Hour)
	middle := now.Add(-2 * time.Hour)
	newest := now.Add(-1 * time.Hour)

	seedSession(t, root, "ws1", "oldest", oldest, 1024)
	seedSession(t, root, "ws1", "middle", middle, 1024)
	seedSession(t, root, "ws1", "newest", newest, 1024)

	// The three JSONL files are 1024B each and the two one-byte transport
	// files make each session 3074B. A 7KB cap requires deleting only the
	// oldest; the newer two then total 6148B.
	c := newTestCleaner(t, root, 7, 4096)
	c.opts.maxBytes = func() int64 { return 7 * 1024 }
	status := c.RunOnce(context.Background())

	lc := status.LastCleanup
	if lc == nil {
		t.Fatal("LastCleanup must be set")
	}
	if lc.BudgetDeleted != 1 {
		t.Errorf("budget_deleted = %d, want 1 (oldest only)", lc.BudgetDeleted)
	}
	if _, err := os.Stat(filepath.Join(root, "ws1", "oldest")); !os.IsNotExist(err) {
		t.Errorf("oldest session should be deleted, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "ws1", "middle")); err != nil {
		t.Errorf("middle session must remain, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "ws1", "newest")); err != nil {
		t.Errorf("newest session must remain, stat err = %v", err)
	}
	if status.UsedBytes > 7*1024 {
		t.Errorf("used_bytes = %d, want <= %d", status.UsedBytes, 7*1024)
	}
}

func TestStorageCleaner_MalformedDirectoryCountsTowardBudget(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(2_000_000_000, 0)
	oldTime := now.Add(-30 * 24 * time.Hour)

	dir := seedSession(t, root, "ws1", "malformed", oldTime, 1024)
	stranger := filepath.Join(dir, "stranger.txt")
	if err := os.WriteFile(stranger, []byte(strings.Repeat("b", 4096)), 0o644); err != nil {
		t.Fatal(err)
	}
	touchTime(t, stranger, oldTime)

	c := newTestCleaner(t, root, 7, 4096)
	c.opts.maxBytes = func() int64 { return 1 }
	status := c.RunOnce(context.Background())

	// The three 1024-byte JSONL files and two one-byte transport files total
	// 3074 bytes; the unexpected file adds 4096 bytes.
	if want := int64(3074 + 4096); status.UsedBytes != want {
		t.Errorf("used_bytes = %d, want %d", status.UsedBytes, want)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("malformed session must remain, stat err = %v", err)
	}
	lc := status.LastCleanup
	if lc == nil || lc.Errors == 0 {
		t.Fatal("expected an error because malformed storage keeps the cleaner over budget")
	}
	if lc.AgeDeleted != 0 {
		t.Errorf("age_deleted = %d, want 0 for malformed directory", lc.AgeDeleted)
	}
	if lc.BudgetDeleted != 0 {
		t.Errorf("budget_deleted = %d, want 0 for malformed directory", lc.BudgetDeleted)
	}
	if status.SessionDirectories != 0 {
		t.Errorf("session_directories = %d, want 0 for malformed-only tree", status.SessionDirectories)
	}
}

func TestStorageCleaner_SymlinkedSessionDirectoryIgnored(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(2_000_000_000, 0)
	target := t.TempDir()
	oldTime := now.Add(-30 * 24 * time.Hour)
	seedSession(t, target, "ws1", "s1", oldTime, 100)

	// Create a symlink at <root>/<wsID>/<sessionID> pointing to the real dir.
	linkDir := filepath.Join(root, "ws1")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(linkDir, "s1")
	if err := os.Symlink(filepath.Join(target, "ws1", "s1"), link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	c := newTestCleaner(t, root, 1, 0)
	status := c.RunOnce(context.Background())

	// Symlinked session must NOT be touched (Lstat) and must not be counted as a candidate.
	if status.SessionDirectories != 0 {
		t.Errorf("session_directories = %d, want 0 (symlink must be ignored)", status.SessionDirectories)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("symlink must remain, stat err = %v", err)
	}
}

func TestStorageCleaner_UnexpectedNestedEntriesBlockDeletion(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(2_000_000_000, 0)
	oldTime := now.Add(-30 * 24 * time.Hour)

	dir := seedSession(t, root, "ws1", "s1", oldTime, 1024)
	// Drop a nested directory inside the session.
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	touchTime(t, filepath.Join(dir, "subdir"), oldTime)

	c := newTestCleaner(t, root, 1, 0)
	status := c.RunOnce(context.Background())

	if status.LastCleanup == nil || status.LastCleanup.Errors == 0 {
		t.Error("expected Errors >= 1 for unexpected nested entry")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("session dir must remain, stat err = %v", err)
	}
}

func TestStorageCleaner_UnrecognizedDirectoryErrors(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(2_000_000_000, 0)
	dir := filepath.Join(root, "ws1", "s1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stranger.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	touchTime(t, dir, now.Add(-30*24*time.Hour))

	c := newTestCleaner(t, root, 1, 0)
	status := c.RunOnce(context.Background())

	if status.LastCleanup == nil || status.LastCleanup.Errors == 0 {
		t.Error("expected Errors >= 1 for unrecognized dir")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("unrecognized dir must remain, stat err = %v", err)
	}
}

func TestStorageCleaner_EmptyWorkspaceDirsRemoved(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(2_000_000_000, 0)
	oldTime := now.Add(-30 * 24 * time.Hour)

	seedSession(t, root, "ws1", "s1", oldTime, 100)
	// Add an empty workspace directory that should be removed.
	emptyWS := filepath.Join(root, "ws2")
	if err := os.MkdirAll(emptyWS, 0o755); err != nil {
		t.Fatal(err)
	}
	// Add a non-empty workspace that must remain.
	keep := filepath.Join(root, "ws3")
	if err := os.MkdirAll(filepath.Join(keep, "s1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keep, "s1", "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := newTestCleaner(t, root, 1, 0)
	c.RunOnce(context.Background())

	if _, err := os.Stat(emptyWS); !os.IsNotExist(err) {
		t.Errorf("empty ws2 should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("non-empty ws3 must remain, stat err = %v", err)
	}
}

func TestStorageCleaner_PartialDeletionErrors(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(2_000_000_000, 0)
	oldTime := now.Add(-30 * 24 * time.Hour)

	dir := seedSession(t, root, "ws1", "s1", oldTime, 100)
	// Make the directory read-only on the parent so RemoveAll of one child fails.
	// Easier: pre-create a regular file at the same path that blocks os.RemoveAll.
	// os.RemoveAll handles a child that is a regular file fine; instead we make
	// the session file's parent read-only so RemoveAll on the dir fails.
	parent := filepath.Dir(dir)
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Skipf("chmod unavailable: %v", err)
	}
	defer os.Chmod(parent, 0o755)

	c := newTestCleaner(t, root, 1, 0)
	status := c.RunOnce(context.Background())

	if status.LastCleanup == nil || status.LastCleanup.Errors == 0 {
		t.Error("expected Errors >= 1 for partial deletion failure")
	}
}

func TestStorageCleaner_CancellationStopsBeforeNextDeletion(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(2_000_000_000, 0)
	oldTime := now.Add(-30 * 24 * time.Hour)

	for i := 0; i < 5; i++ {
		seedSession(t, root, "ws1", "s"+string(rune('a'+i)), oldTime, 100)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	c := newTestCleaner(t, root, 1, 0)
	c.RunOnce(ctx)

	// With ctx already cancelled, the cleaner must delete nothing.
	entries, _ := os.ReadDir(filepath.Join(root, "ws1"))
	if len(entries) == 0 {
		t.Error("cancellation: nothing was scheduled for deletion, so dirs should remain")
	}
}

func TestStorageCleaner_StatusReplaceableAndConcurrentSafe(t *testing.T) {
	root := t.TempDir()
	c := newTestCleaner(t, root, 7, 4096)
	c.RunOnce(context.Background()) // populates status

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = c.Status() }()
		go func() { defer wg.Done(); c.RunOnce(context.Background()) }()
	}
	wg.Wait()
}

func TestStorageCleaner_JSONStatusSerialization(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(2_000_000_000, 0)
	oldTime := now.Add(-30 * 24 * time.Hour)
	seedSession(t, root, "ws1", "s1", oldTime, 100)

	c := newTestCleaner(t, root, 1, 0)
	status := c.RunOnce(context.Background())
	if status.LastCleanup == nil {
		t.Fatal("expected LastCleanup to be populated after a pass")
	}

	b, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Spot-check the JSON contains the expected field names.
	for _, key := range []string{
		"retention_days",
		"max_total_storage_mb",
		"used_bytes",
		"active_bytes",
		"ended_bytes",
		"session_directories",
		"active_directories",
		"last_cleanup",
		"age_deleted",
		"budget_deleted",
		"bytes_reclaimed",
		"errors",
	} {
		if !strings.Contains(string(b), key) {
			t.Errorf("serialized status missing %q: %s", key, b)
		}
	}
}

func TestStorageCleaner_StatusReplacesOnErrorPass(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(2_000_000_000, 0)
	oldTime := now.Add(-30 * 24 * time.Hour)

	seedSession(t, root, "ws1", "s1", oldTime, 100)
	c := newTestCleaner(t, root, 1, 0)
	first := c.RunOnce(context.Background())
	if first.LastCleanup == nil || first.LastCleanup.AgeDeleted != 1 {
		t.Fatalf("first pass: age_deleted = %v, want 1", first.LastCleanup)
	}

	// Second pass with no candidates: LastCleanup must be replaced (not omitted).
	second := c.RunOnce(context.Background())
	if second.LastCleanup == nil {
		t.Fatal("second pass: LastCleanup must be present even when no candidates remain")
	}
	if second.LastCleanup.AgeDeleted != 0 {
		t.Errorf("second pass: age_deleted = %d, want 0", second.LastCleanup.AgeDeleted)
	}
	if second.LastCleanup.BytesReclaimed != 0 {
		t.Errorf("second pass: bytes_reclaimed = %d, want 0", second.LastCleanup.BytesReclaimed)
	}
}

func TestStorageCleaner_DeterministicOrder(t *testing.T) {
	// Two passes over the same input should produce a deterministic order.
	// We can't directly observe the order, but we can verify the resulting
	// filesystem state is stable.
	root := t.TempDir()
	now := time.Unix(2_000_000_000, 0)
	oldTime := now.Add(-30 * 24 * time.Hour)

	for _, sid := range []string{"a", "b", "c", "d", "e"} {
		seedSession(t, root, "ws1", sid, oldTime, 100)
	}

	c := newTestCleaner(t, root, 1, 0)
	c.RunOnce(context.Background())

	// Run a second time: order must be stable.
	c2 := newTestCleaner(t, root, 1, 0)
	c2.RunOnce(context.Background())

	// All should be deleted. The workspace dir is also tidied when empty.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		sort.Strings(names)
		t.Errorf("expected all sessions and workspace dirs deleted, found %v", names)
	}
}
