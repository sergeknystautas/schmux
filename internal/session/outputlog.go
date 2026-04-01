package session

import "sync"

// LogEntry is a sequenced output event.
type LogEntry struct {
	Seq  uint64
	Data []byte
}

// OutputLog is a bounded circular buffer of sequenced output events.
// It is safe for concurrent use from multiple goroutines.
type OutputLog struct {
	mu         sync.RWMutex
	entries    []LogEntry
	head       int        // next write position
	size       int        // current number of valid entries
	cap        int        // max entries
	nextSeq    uint64     // next sequence number to assign
	totalBytes int64      // cumulative bytes appended
	notify     *sync.Cond // signals on Append for waiters (e.g., recorder)
}

// NewOutputLog creates an output log with the given capacity (max entries).
// Panics if capacity is zero or negative.
func NewOutputLog(capacity int) *OutputLog {
	if capacity <= 0 {
		panic("outputlog: capacity must be > 0")
	}
	ol := &OutputLog{
		entries: make([]LogEntry, capacity),
		cap:     capacity,
	}
	ol.notify = sync.NewCond(&ol.mu)
	return ol
}

// Append adds data to the log and assigns the next sequence number.
func (l *OutputLog) Append(data []byte) uint64 {
	// Copy the data so the caller can reuse the slice
	copied := make([]byte, len(data))
	copy(copied, data)

	l.mu.Lock()
	seq := l.nextSeq
	l.entries[l.head] = LogEntry{Seq: seq, Data: copied}
	l.head = (l.head + 1) % l.cap
	if l.size < l.cap {
		l.size++
	}
	l.nextSeq++
	l.totalBytes += int64(len(data))
	l.mu.Unlock()
	// Broadcast OUTSIDE the lock to avoid unnecessary contention.
	l.notify.Broadcast()
	return seq
}

// CurrentSeq returns the next sequence number that will be assigned.
// If 3 events have been appended (seq 0,1,2), CurrentSeq returns 3.
func (l *OutputLog) CurrentSeq() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.nextSeq
}

// OldestSeq returns the oldest sequence number still in the log.
// Returns 0 if the log is empty.
func (l *OutputLog) OldestSeq() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.size == 0 {
		return 0
	}
	oldest := (l.head - l.size + l.cap) % l.cap
	return l.entries[oldest].Seq
}

// ReplayFrom returns all entries with seq >= fromSeq.
// Returns nil if fromSeq is too old (evicted from the buffer).
// Returns empty slice if fromSeq >= currentSeq (nothing new).
func (l *OutputLog) ReplayFrom(fromSeq uint64) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.replayFromLocked(fromSeq)
}

// replayFromLocked is the lock-free inner implementation of ReplayFrom.
// Caller must hold at least a read lock.
func (l *OutputLog) replayFromLocked(fromSeq uint64) []LogEntry {
	if l.size == 0 {
		if fromSeq == 0 {
			return []LogEntry{}
		}
		return nil
	}

	oldest := (l.head - l.size + l.cap) % l.cap
	oldestSeq := l.entries[oldest].Seq

	if fromSeq < oldestSeq {
		return nil // requested data has been evicted
	}
	if fromSeq >= l.nextSeq {
		return []LogEntry{} // nothing new
	}

	// Calculate how many entries to skip from oldest
	skip := int(fromSeq - oldestSeq)
	count := l.size - skip
	result := make([]LogEntry, count)
	for i := 0; i < count; i++ {
		idx := (oldest + skip + i) % l.cap
		result[i] = l.entries[idx]
	}
	return result
}

// ReplayAll returns all entries currently in the log.
func (l *OutputLog) ReplayAll() []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.size == 0 {
		return []LogEntry{}
	}
	oldest := (l.head - l.size + l.cap) % l.cap
	return l.replayFromLocked(l.entries[oldest].Seq)
}

// TotalBytes returns the cumulative bytes appended to the log.
func (l *OutputLog) TotalBytes() int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.totalBytes
}

// WaitForNew blocks until new entries are available after afterSeq,
// or until stopCh is closed. Returns true if new data is available,
// false if stopCh was closed.
func (l *OutputLog) WaitForNew(afterSeq uint64, stopCh <-chan struct{}) bool {
	// Fast path: check if new data is already available.
	l.mu.RLock()
	if l.nextSeq > afterSeq {
		l.mu.RUnlock()
		return true
	}
	l.mu.RUnlock()

	// Start a goroutine that closes a done channel when stopCh fires,
	// waking any waiting goroutines.
	done := make(chan struct{})
	go func() {
		select {
		case <-stopCh:
			l.notify.Broadcast()
		case <-done:
		}
	}()
	defer close(done)

	l.notify.L.Lock()
	for l.nextSeq <= afterSeq {
		// Check stopCh before waiting
		select {
		case <-stopCh:
			l.notify.L.Unlock()
			return false
		default:
		}
		l.notify.Wait()
	}
	l.notify.L.Unlock()
	return true
}
