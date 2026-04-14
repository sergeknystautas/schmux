//go:build !norepofeed

package repofeed

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

const dismissedFileName = "repofeed-dismissed.json"

// DismissedStore tracks which completed intents a user has dismissed from their incoming feed.
type DismissedStore struct {
	mu        sync.RWMutex
	dismissed map[string]bool // hashed IDs
	path      string
}

// NewDismissedStore creates a new DismissedStore, loading from disk if available.
func NewDismissedStore() *DismissedStore {
	ds := &DismissedStore{
		dismissed: make(map[string]bool),
		path:      filepath.Join(schmuxdir.Get(), dismissedFileName),
	}
	ds.load()
	return ds
}

// Dismiss marks an intent as dismissed.
func (ds *DismissedStore) Dismiss(developer, workspaceID string) {
	key := dismissKey(developer, workspaceID)
	ds.mu.Lock()
	ds.dismissed[key] = true
	ds.mu.Unlock()
	ds.save()
}

// IsDismissed checks if an intent has been dismissed.
func (ds *DismissedStore) IsDismissed(developer, workspaceID string) bool {
	key := dismissKey(developer, workspaceID)
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.dismissed[key]
}

func dismissKey(developer, workspaceID string) string {
	h := sha256.Sum256([]byte(developer + ":" + workspaceID))
	return fmt.Sprintf("%x", h[:8])
}

type dismissedFile struct {
	Dismissed []string `json:"dismissed"`
}

func (ds *DismissedStore) load() {
	data, err := os.ReadFile(ds.path)
	if err != nil {
		return
	}
	var f dismissedFile
	if err := json.Unmarshal(data, &f); err != nil {
		return
	}
	for _, key := range f.Dismissed {
		ds.dismissed[key] = true
	}
}

func (ds *DismissedStore) save() {
	ds.mu.RLock()
	keys := make([]string, 0, len(ds.dismissed))
	for k := range ds.dismissed {
		keys = append(keys, k)
	}
	ds.mu.RUnlock()

	data, err := json.MarshalIndent(dismissedFile{Dismissed: keys}, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(ds.path, data, 0644)
}
