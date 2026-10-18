package chat

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

// ChatStorageGrace is the fixed grace period that protects newly disposed
// chat session directories. It is intentionally not configurable: it is a
// correctness guard, not a user preference.
const ChatStorageGrace = time.Hour

// StorageLogger is the narrow logger surface the cleaner uses. Matches
// charmbracelet/log's Info and Warn signatures so callers can pass a
// *log.Logger directly.
type StorageLogger interface {
	Info(msg interface{}, args ...interface{})
	Warn(msg interface{}, args ...interface{})
}

// StorageCleanerOptions wires dynamic config and the current time into the
// cleaner. Closures read live values so config edits made between passes take
// effect on the next pass.
type StorageCleanerOptions struct {
	Root           string
	ActiveSessions func() map[string]struct{}
	RetentionDays  func() int
	MaxBytes       func() int64
	Now            func() time.Time
	Log            StorageLogger
}

// StorageCleanupResult is one completed pass.
type StorageCleanupResult struct {
	At             time.Time `json:"at"`
	AgeDeleted     int       `json:"age_deleted"`
	BudgetDeleted  int       `json:"budget_deleted"`
	BytesReclaimed int64     `json:"bytes_reclaimed"`
	Errors         int       `json:"errors"`
}

// StorageStatus is the dashboard snapshot: live config plus the most recent
// scan, with the cached cleanup result attached.
type StorageStatus struct {
	RetentionDays      int                   `json:"retention_days"`
	MaxTotalStorageMB  int                   `json:"max_total_storage_mb"`
	UsedBytes          int64                 `json:"used_bytes"`
	ActiveBytes        int64                 `json:"active_bytes"`
	EndedBytes         int64                 `json:"ended_bytes"`
	SessionDirectories int                   `json:"session_directories"`
	ActiveDirectories  int                   `json:"active_directories"`
	LastCleanup        *StorageCleanupResult `json:"last_cleanup,omitempty"`
}

// StorageCleaner bounds chat storage usage. Each RunOnce pass scans the chat
// root, deletes candidates past retention, then deletes oldest grace-eligible
// candidates while over budget. The cached status is replaced after every
// pass, including zero-op and error passes.
type StorageCleaner struct {
	opts struct {
		StorageCleanerOptions
		// maxBytes is the test seam that lets the bytes cap be adjusted
		// in-place for ordering tests without rebuilding the cleaner.
		maxBytes func() int64
	}

	mu     sync.RWMutex
	status StorageStatus
}

// NewStorageCleaner validates options and returns a cleaner. The bytes cap
// is sourced from opts.MaxBytes (kept on opts.maxBytes for tests).
func NewStorageCleaner(opts StorageCleanerOptions) *StorageCleaner {
	c := &StorageCleaner{}
	c.opts.StorageCleanerOptions = opts
	if c.opts.ActiveSessions == nil {
		c.opts.ActiveSessions = func() map[string]struct{} { return nil }
	}
	if c.opts.RetentionDays == nil {
		c.opts.RetentionDays = func() int { return 7 }
	}
	if c.opts.MaxBytes == nil {
		c.opts.MaxBytes = func() int64 { return int64(4096) * 1024 * 1024 }
	}
	if c.opts.Now == nil {
		c.opts.Now = time.Now
	}
	c.opts.maxBytes = c.opts.MaxBytes
	return c
}

// Status returns the cached snapshot under the cleaner's mutex. Safe for
// concurrent readers.
func (c *StorageCleaner) Status() StorageStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// recognizedFiles is the set of filenames the cleaner is allowed to see
// inside a session directory. Anything else is treated as an error.
var recognizedFiles = map[string]struct{}{
	"conversation.jsonl": {},
	"in.jsonl":           {},
	"out.jsonl":          {},
	"err.txt":            {},
	"tail.pid":           {},
}

// candidate is a single session directory the cleaner is considering.
type candidate struct {
	path            string
	sessionID       string
	workspaceID     string
	modTime         time.Time
	bytes           int64 // logical size of recognized files in this dir
	active          bool
	recognized      []string // sorted, recognized filenames present
	recognizedBytes map[string]int64
}

// RemoveBridgeFiles deletes the four transport files for one chat session:
// Input, Output, Errors, TailPID. Conversation is preserved. Missing files
// are treated as success. The containing directory is not removed.
func RemoveBridgeFiles(p Paths) error {
	for _, f := range []string{p.Input, p.Output, p.Errors, p.TailPID} {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("chat: remove %s: %w", filepath.Base(f), err)
		}
	}
	return nil
}

// DeleteConversations removes the chat conversation directories for the named
// session IDs under one workspace, returning the number removed. A workspace
// or session ID that contains a path separator is refused; missing entries
// are not an error. Use this when terminating a flight whose session list is
// known, so other sessions stored under the same workspace ID are not lost.
func DeleteConversations(workspaceID string, sessionIDs []string) (deleted int, err error) {
	if err := validateConfinedID(workspaceID, "workspace id"); err != nil {
		return 0, err
	}
	for _, sid := range sessionIDs {
		if err := validateConfinedID(sid, "session id"); err != nil {
			return 0, err
		}
	}
	for _, sid := range sessionIDs {
		dir := schmuxdir.ChatSessionDir(workspaceID, sid)
		if _, statErr := os.Stat(dir); errors.Is(statErr, os.ErrNotExist) {
			continue
		} else if statErr != nil {
			return deleted, fmt.Errorf("chat: stat %s: %w", dir, statErr)
		}
		if err := os.RemoveAll(dir); err != nil {
			return deleted, fmt.Errorf("chat: remove %s: %w", dir, err)
		}
		deleted++
	}
	return deleted, nil
}

// validateConfinedID rejects IDs that are empty, equal to "." or "..", or
// that contain a path separator. The check prevents traversal outside the
// chat storage root.
func validateConfinedID(id, kind string) error {
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id {
		return fmt.Errorf("chat: invalid %s %q", kind, id)
	}
	return nil
}

// RunOnce performs a single scan-and-cleanup pass. Returns the status
// snapshot. The cached snapshot is replaced even on error.
func (c *StorageCleaner) RunOnce(ctx context.Context) StorageStatus {
	log := c.opts.Log
	if log == nil {
		log = silentLoggerNoop{}
	}
	now := c.opts.Now()
	if now.IsZero() {
		now = time.Now()
	}
	retention := c.opts.RetentionDays()
	maxBytes := c.opts.maxBytes()
	active := c.opts.ActiveSessions()
	if active == nil {
		active = map[string]struct{}{}
	}

	candidates, activeBytes, errs := c.scan(ctx, active)
	totalBytes, measureErrs := regularBytes(c.opts.Root)
	errs += measureErrs

	// Pass one: age deletion.
	var ageDeleted int
	var reclaimed int64
	cutoff := now.Add(-ChatStorageGrace - time.Duration(retention)*24*time.Hour)
	for _, cand := range candidates {
		if cand.active {
			continue
		}
		if !cand.modTime.Before(cutoff) {
			continue
		}
		if err := ctx.Err(); err != nil {
			break
		}
		size := cand.bytes
		if err := os.RemoveAll(cand.path); err != nil {
			errs++
			log.Warn("chat-storage: age delete failed", "session", cand.sessionID, "err", err)
			continue
		}
		ageDeleted++
		reclaimed += size
		totalBytes -= size
	}

	// Recompute candidates still on disk after the age pass; preserve age
	// by reading the original modTime we already recorded, since the
	// directory is gone. For the survivors we can re-scan.
	survivors := c.rescanSurvivors(candidates, active)

	// Pass two: budget deletion using only grace-eligible candidates.
	var budgetDeleted int
	if totalBytes > maxBytes {
		graceCutoff := now.Add(-ChatStorageGrace)
		eligible := make([]candidate, 0, len(survivors))
		for _, cand := range survivors {
			if cand.active {
				continue
			}
			if !cand.modTime.Before(graceCutoff) {
				continue
			}
			eligible = append(eligible, cand)
		}
		sort.Slice(eligible, func(i, j int) bool {
			return eligible[i].modTime.Before(eligible[j].modTime)
		})
		for _, cand := range eligible {
			if err := ctx.Err(); err != nil {
				break
			}
			if totalBytes <= maxBytes {
				break
			}
			size := cand.bytes
			if err := os.RemoveAll(cand.path); err != nil {
				errs++
				log.Warn("chat-storage: budget delete failed", "session", cand.sessionID, "err", err)
				continue
			}
			budgetDeleted++
			reclaimed += size
			totalBytes -= size
		}
		if totalBytes > maxBytes {
			log.Warn("chat-storage: over budget after grace-eligible deletions",
				"used_bytes", totalBytes, "max_bytes", maxBytes)
			errs++ // over-budget state is an error condition worth counting
		}
	}

	// Tidy empty workspace directories.
	c.removeEmptyWorkspaces(ctx)

	result := StorageCleanupResult{
		At:             now,
		AgeDeleted:     ageDeleted,
		BudgetDeleted:  budgetDeleted,
		BytesReclaimed: reclaimed,
		Errors:         errs,
	}
	if log != nil {
		log.Info("chat-storage: pass complete",
			"age_deleted", ageDeleted,
			"budget_deleted", budgetDeleted,
			"bytes_reclaimed", reclaimed,
			"errors", errs)
	}

	status := StorageStatus{
		RetentionDays:      retention,
		MaxTotalStorageMB:  int(maxBytes / (1024 * 1024)),
		UsedBytes:          totalBytes,
		ActiveBytes:        activeBytes,
		EndedBytes:         totalBytes - activeBytes,
		SessionDirectories: len(candidates),
		ActiveDirectories:  countActive(candidates),
		LastCleanup:        &result,
	}
	c.mu.Lock()
	c.status = status
	c.mu.Unlock()
	return status
}

// scan walks the chat root and returns deletable candidates plus active-byte
// accounting. Global byte accounting is handled by regularBytes so malformed
// directories still count toward the budget.
func (c *StorageCleaner) scan(ctx context.Context, active map[string]struct{}) ([]candidate, int64, int) {
	var (
		cands       []candidate
		activeBytes int64
		errs        int
		log         = c.opts.Log
	)
	if log == nil {
		log = silentLoggerNoop{}
	}
	if c.opts.Root == "" {
		return nil, 0, 0
	}
	workspaces, err := os.ReadDir(c.opts.Root)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			errs++
			log.Warn("chat-storage: read root failed", "err", err)
		}
		return nil, 0, errs
	}
	for _, ws := range workspaces {
		if err := ctx.Err(); err != nil {
			break
		}
		if !ws.IsDir() {
			continue
		}
		wsPath := filepath.Join(c.opts.Root, ws.Name())
		// Lstat on workspace dir so we never follow symlinks at this level.
		if _, lerr := os.Lstat(wsPath); lerr != nil {
			continue
		}
		sessions, err := os.ReadDir(wsPath)
		if err != nil {
			errs++
			log.Warn("chat-storage: read workspace failed", "workspace", ws.Name(), "err", err)
			continue
		}
		for _, sess := range sessions {
			if err := ctx.Err(); err != nil {
				break
			}
			if !sess.IsDir() {
				continue
			}
			sessPath := filepath.Join(wsPath, sess.Name())
			info, lerr := os.Lstat(sessPath)
			if lerr != nil {
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 {
				continue // ignore symlinks
			}
			cand, ok := c.buildCandidate(sessPath, ws.Name(), sess.Name(), active)
			if !ok {
				errs++
				if invalidBytes, _ := regularBytes(sessPath); invalidBytes > 0 {
					if _, isActive := active[sess.Name()]; isActive {
						activeBytes += invalidBytes
					}
				}
				continue
			}
			cands = append(cands, cand)
			if cand.active {
				activeBytes += cand.bytes
			}
		}
	}
	return cands, activeBytes, errs
}

// regularBytes sums logical sizes of regular files without following symlinked
// directories. Storage accounting deliberately includes malformed session
// directories even though the cleaner never deletes them.
func regularBytes(root string) (int64, int) {
	if root == "" {
		return 0, 0
	}
	var total int64
	var errs int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			errs++
			return nil
		}
		if d.Type().IsRegular() {
			info, infoErr := d.Info()
			if infoErr != nil {
				errs++
				return nil
			}
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return total, 0
		}
		errs++
	}
	return total, errs
}

// buildCandidate reads a session directory and returns a candidate. ok=false
// means the directory is invalid (unrecognized entries, missing regular
// files, or symlink) and the caller should record an error.
func (c *StorageCleaner) buildCandidate(sessPath, workspaceID, sessionID string, active map[string]struct{}) (candidate, bool) {
	dirInfo, err := os.Lstat(sessPath)
	if err != nil {
		return candidate{}, false
	}
	if !dirInfo.IsDir() {
		return candidate{}, false
	}
	entries, err := os.ReadDir(sessPath)
	if err != nil {
		return candidate{}, false
	}
	hasRecognized := false
	latest := dirInfo.ModTime()
	var (
		recognizedNames []string
		recognizedBytes = map[string]int64{}
	)
	for _, e := range entries {
		if e.IsDir() {
			return candidate{}, false
		}
		if _, ok := recognizedFiles[e.Name()]; !ok {
			return candidate{}, false
		}
		full := filepath.Join(sessPath, e.Name())
		info, err := os.Lstat(full)
		if err != nil {
			return candidate{}, false
		}
		if !info.Mode().IsRegular() {
			return candidate{}, false
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return candidate{}, false
		}
		hasRecognized = true
		recognizedNames = append(recognizedNames, e.Name())
		recognizedBytes[e.Name()] = info.Size()
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	if !hasRecognized {
		return candidate{}, false
	}
	sort.Strings(recognizedNames)
	var total int64
	for _, n := range recognizedNames {
		total += recognizedBytes[n]
	}
	_, isActive := active[sessionID]
	return candidate{
		path:            sessPath,
		sessionID:       sessionID,
		workspaceID:     workspaceID,
		modTime:         latest,
		bytes:           total,
		active:          isActive,
		recognized:      recognizedNames,
		recognizedBytes: recognizedBytes,
	}, true
}

// rescanSurvivors rebuilds the candidate list for directories that survived
// the age pass. This avoids trying to delete paths that are already gone.
func (c *StorageCleaner) rescanSurvivors(ageDeleted []candidate, active map[string]struct{}) []candidate {
	_ = ageDeleted
	survivors, _, _ := c.scan(context.Background(), active)
	return survivors
}

func countActive(cands []candidate) int {
	n := 0
	for _, c := range cands {
		if c.active {
			n++
		}
	}
	return n
}

// removeEmptyWorkspaces deletes immediate workspace directories under root
// that have no session directories left.
func (c *StorageCleaner) removeEmptyWorkspaces(ctx context.Context) {
	if c.opts.Root == "" {
		return
	}
	workspaces, err := os.ReadDir(c.opts.Root)
	if err != nil {
		return
	}
	for _, ws := range workspaces {
		if err := ctx.Err(); err != nil {
			return
		}
		if !ws.IsDir() {
			continue
		}
		wsPath := filepath.Join(c.opts.Root, ws.Name())
		entries, err := os.ReadDir(wsPath)
		if err != nil {
			continue
		}
		if len(entries) == 0 {
			_ = os.Remove(wsPath)
		}
	}
}

type silentLoggerNoop struct{}

func (silentLoggerNoop) Info(interface{}, ...interface{}) {}
func (silentLoggerNoop) Warn(interface{}, ...interface{}) {}
