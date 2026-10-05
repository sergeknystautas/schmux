package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/attachment"
)

// spawnStaging owns ~/.schmux/spawn-attachments: files uploaded from the
// spawn form before the workspace that will receive them exists. Each upload
// lives in its own <uuid>/<name> directory; the uuid is the staging id.
type spawnStaging struct {
	dir string
	now func() time.Time
}

func newSpawnStaging(dir string, now func() time.Time) *spawnStaging {
	return &spawnStaging{dir: dir, now: now}
}

// Put stores one upload and returns its staging id.
func (s *spawnStaging) Put(name string, src io.Reader) (contracts.SpawnAttachment, error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return contracts.SpawnAttachment{}, fmt.Errorf("create staging directory: %w", err)
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return contracts.SpawnAttachment{}, fmt.Errorf("open staging directory: %w", err)
	}
	defer root.Close()
	rel, err := attachment.Save(root, ".", name, src)
	if err != nil {
		return contracts.SpawnAttachment{}, err
	}
	return contracts.SpawnAttachment{ID: filepath.Dir(rel), Name: name}, nil
}

// Resolve returns the staged file path for each id, in order. Any id that is
// not a canonical uuid or no longer exists fails the whole call.
func (s *spawnStaging) Resolve(ids []string) ([]string, error) {
	paths := make([]string, 0, len(ids))
	for _, id := range ids {
		p, ok := s.path(id)
		if !ok {
			return nil, fmt.Errorf("attachment no longer available: %s", id)
		}
		paths = append(paths, p)
	}
	return paths, nil
}

// Delete removes the given uploads. Unknown or malformed ids are ignored.
func (s *spawnStaging) Delete(ids []string) {
	for _, id := range ids {
		if canonicalID(id) {
			_ = os.RemoveAll(filepath.Join(s.dir, id))
		}
	}
}

// Sweep removes uploads older than maxAge. Spawn drafts live in per-tab
// sessionStorage, so anything that old has no form left to submit it.
func (s *spawnStaging) Sweep(maxAge time.Duration) error {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	cutoff := s.now().Add(-maxAge)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !e.IsDir() || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (s *spawnStaging) path(id string) (string, bool) {
	if !canonicalID(id) {
		return "", false
	}
	entries, err := os.ReadDir(filepath.Join(s.dir, id))
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if e.Type().IsRegular() && e.Name() != ".upload" {
			return filepath.Join(s.dir, id, e.Name()), true
		}
	}
	return "", false
}

// canonicalID accepts only the lowercase hyphenated form uuid.NewString makes.
func canonicalID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.String() == id
}

// releaseStaged deletes a request's staged uploads once any target has
// started. The form clears its draft on any success (spawn-inflight.ts), so
// nothing can reference them again; when every target fails the draft
// survives and so do the uploads.
func (h *SpawnHandlers) releaseStaged(ids []string, results []SessionResult) {
	for _, r := range results {
		if r.Error == "" {
			h.staging.Delete(ids)
			return
		}
	}
}

// handleSpawnAttachment stages one raw file body for the spawn form's Attach
// action, before the spawn's workspace exists.
func (h *SpawnHandlers) handleSpawnAttachment(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("filename")
	if !attachment.ValidName(name) {
		writeJSONError(w, "invalid filename", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, attachment.MaxSize)
	defer r.Body.Close()
	staged, err := h.staging.Put(name, r.Body)
	if err != nil {
		writeAttachmentSaveError(w, h.logger, "spawn-staging", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(staged)
}
