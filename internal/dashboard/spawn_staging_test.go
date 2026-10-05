package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/attachment"
)

func TestSpawnStagingPutAndResolve(t *testing.T) {
	s := newSpawnStaging(filepath.Join(t.TempDir(), "spawn-attachments"), time.Now)
	a, err := s.Put("notes.txt", strings.NewReader("first"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Put("notes.txt", strings.NewReader("second"))
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID || a.Name != "notes.txt" {
		t.Fatalf("Put returned %+v and %+v", a, b)
	}
	paths, err := s.Resolve([]string{a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"first", "second"} {
		data, err := os.ReadFile(paths[i])
		if err != nil || string(data) != want || filepath.Base(paths[i]) != "notes.txt" {
			t.Fatalf("paths[%d] = %q holds %q (%v), want %q", i, paths[i], data, err, want)
		}
	}
}

// Ids come from the browser; anything that is not a canonical uuid must
// never be joined onto the staging directory.
func TestSpawnStagingResolveRejectsNonCanonicalIDs(t *testing.T) {
	s := newSpawnStaging(filepath.Join(t.TempDir(), "spawn-attachments"), time.Now)
	a, err := s.Put("data.csv", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "..", "../../etc", "{" + a.ID + "}", strings.ToUpper(a.ID), "urn:uuid:" + a.ID, "6ba7b810-9dad-11d1-80b4-00c04fd430c8"} {
		t.Run(id, func(t *testing.T) {
			_, err := s.Resolve([]string{id})
			if err == nil || err.Error() != "attachment no longer available: "+id {
				t.Fatalf("Resolve(%q) err = %v", id, err)
			}
		})
	}
}

func TestSpawnStagingDelete(t *testing.T) {
	s := newSpawnStaging(filepath.Join(t.TempDir(), "spawn-attachments"), time.Now)
	a, err := s.Put("data.csv", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	s.Delete([]string{a.ID, "../not-a-uuid"})
	if _, err := s.Resolve([]string{a.ID}); err == nil {
		t.Fatal("deleted attachment still resolves")
	}
}

func TestSpawnStagingSweepRemovesOnlyOldEntries(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), "spawn-attachments")
	s := newSpawnStaging(dir, func() time.Time { return now })
	old, err := s.Put("old.csv", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := s.Put("fresh.csv", strings.NewReader("y"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, old.ID), now.Add(-25*time.Hour), now.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, fresh.ID), now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.Sweep(24 * time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve([]string{old.ID}); err == nil {
		t.Fatal("25h-old entry survived the sweep")
	}
	if _, err := s.Resolve([]string{fresh.ID}); err != nil {
		t.Fatalf("1h-old entry was swept: %v", err)
	}
}

func TestSpawnStagingSweepToleratesMissingDirectory(t *testing.T) {
	s := newSpawnStaging(filepath.Join(t.TempDir(), "never-created"), time.Now)
	if err := s.Sweep(24 * time.Hour); err != nil {
		t.Fatalf("Sweep on a missing directory: %v", err)
	}
}

// zeroReader yields zero bytes forever; LimitReader bounds it.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func spawnAttachmentRequest(name string, body io.Reader) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/api/spawn-attachments?filename="+url.QueryEscape(name), body)
}

func TestHandleSpawnAttachment(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := newTestSpawnHandlers(s)

	t.Run("stores the body", func(t *testing.T) {
		w := httptest.NewRecorder()
		h.handleSpawnAttachment(w, spawnAttachmentRequest("users.csv", strings.NewReader("id,name\n")))
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		var got contracts.SpawnAttachment
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		paths, err := h.staging.Resolve([]string{got.ID})
		if err != nil || got.Name != "users.csv" {
			t.Fatalf("got %+v, resolve err %v", got, err)
		}
		data, _ := os.ReadFile(paths[0])
		if string(data) != "id,name\n" {
			t.Fatalf("staged bytes = %q", data)
		}
	})

	t.Run("rejects an invalid filename", func(t *testing.T) {
		w := httptest.NewRecorder()
		h.handleSpawnAttachment(w, spawnAttachmentRequest("../escape", strings.NewReader("x")))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid filename") {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects a body over 50 MiB", func(t *testing.T) {
		w := httptest.NewRecorder()
		h.handleSpawnAttachment(w, spawnAttachmentRequest("big.bin", io.LimitReader(zeroReader{}, attachment.MaxSize+1)))
		if w.Code != http.StatusRequestEntityTooLarge || !strings.Contains(w.Body.String(), "file exceeds 50 MiB") {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
	})
}
