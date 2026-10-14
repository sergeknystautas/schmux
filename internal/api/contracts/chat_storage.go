package contracts

import "time"

// ChatStorageCleanupResult is the cached result of one cleaner pass. It is
// populated by the daemon-owned cleaner after every scan; the API returns it
// as-is (no rescan on read).
type ChatStorageCleanupResult struct {
	At             time.Time `json:"at"`
	AgeDeleted     int       `json:"age_deleted"`
	BudgetDeleted  int       `json:"budget_deleted"`
	BytesReclaimed int64     `json:"bytes_reclaimed"`
	Errors         int       `json:"errors"`
}

// ChatStorageStatusResponse is the API response for GET /api/chat/storage.
// All counters are zero when no cleaner is installed; last_cleanup is omitted
// until the cleaner has completed at least one pass.
type ChatStorageStatusResponse struct {
	RetentionDays      int                       `json:"retention_days"`
	MaxTotalStorageMB  int                       `json:"max_total_storage_mb"`
	UsedBytes          int64                     `json:"used_bytes"`
	ActiveBytes        int64                     `json:"active_bytes"`
	EndedBytes         int64                     `json:"ended_bytes"`
	SessionDirectories int                       `json:"session_directories"`
	ActiveDirectories  int                       `json:"active_directories"`
	LastCleanup        *ChatStorageCleanupResult `json:"last_cleanup,omitempty"`
}
