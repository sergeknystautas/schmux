# Spawn Attachments Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The spawn form attaches images and files exactly like the chat composer, in every local spawn mode, and every spawned session receives its own copy inside its own workspace.

**Architecture:** Generic files upload immediately to a daemon staging area (`POST /api/spawn-attachments`) and the spawn request references them by id; images travel inline with their media type, the same shape chat uses. After each spawn resolves its workspace, the session manager copies staged files and (for terminal targets) image bytes into the workspace through one shared `os.Root`-anchored writer and appends the chat composer's `File attachments:` block. The frontend extracts Composer's and ChatView's attachment logic into shared hooks and components that both the chat composer and the spawn form use.

**Tech Stack:** Go (chi, `os.Root`, `github.com/google/uuid`), React + TypeScript (Vitest, React Testing Library), Playwright scenarios.

**Spec:** `docs/superpowers/specs/2026-10-04-spawn-attachments-design.md`

## Global Constraints

- No task commits. The user owns git and commits with `/commit` when they choose. Never run `git commit`, `git stash`, or `--no-verify`.
- Build the dashboard only with `go run ./cmd/build-dashboard`; never `npm`/`vite` directly.
- Run tests only through `./test.sh` (`--quick` while iterating; full `./test.sh` before completion). Go unit tests may also run with `go test ./internal/<pkg>/...`.
- Never edit `assets/dashboard/src/lib/types.generated.ts`; change `internal/api/contracts/*.go` and run `go run ./cmd/gen-types`.
- Any change under `internal/dashboard/` or `internal/session/` updates `docs/api.md` in the same task.
- Per-file upload limit: 50 MiB (`50 << 20`). Spawn image limit: 5.
- Staging directory: `~/.schmux/spawn-attachments/<uuid>/<name>` (via `schmuxdir.Get()`). Sweep age: 24 hours, at daemon start.
- Workspace copies: `<SchmuxDataDirRelative(vcs)>/attachments/<uuid>/<name>`, mode 0600, directories 0700.
- Prompt block, byte-exact: `File attachments:\n<path>\n<path>`, preceded by `\n\n` only when the prompt is non-empty. The `Image attachments:` block (terminal only) follows it.
- Error strings, exact: `invalid filename`, `file exceeds 50 MiB`, `file upload failed`, `cannot save attachment`, `attachment no longer available: <id>`, `attachments are not allowed for command targets`, `maximum 5 image attachments allowed`, `cannot use attachments with resume mode`, `cannot use attachments with command mode`, `attachments are not supported for remote spawns`, `image attachments must have an image media type`. Client-side: `<filename>: maximum 5 images`, `Attachments aren't supported for remote spawns`.
- Dashboard CSS follows `docs/dashboard-style-guide.md`: tokens only, no hardcoded colors.
- Tests follow `docs/testing.md` (rubric rules cited by number). No sleeps; inject clocks.

## Review Focus

1. **A staging id that is not a canonical uuid** (`../../etc`, `{uuid}`, uppercase, empty) must resolve as "no longer available", never touch a path outside the staging directory. Test in Task 2.
2. **Two attachments with the same filename in one spawn** (two `notes.txt` from different folders) must both be delivered at distinct paths. Test in Task 4.
3. **A spawn whose staged file was swept** (draft restored a day later) must fail the whole request with 400 and keep the form's chips and draft. Tests in Task 4 (server) and Task 7 (form).
4. **Switching a fresh spawn to a remote host after attaching** must block submission with a toast instead of dropping the attachments. Test in Task 7.
5. **Pasting plain text into the prompt** must still paste text; the document-level paste listener only takes over when the clipboard carries files. Test in Task 7.

---

## File Structure

Backend:

- Create `internal/attachment/attachment.go` — `MaxSize`, `ErrReceive`, `ValidName`, `Save`, `AppendFileList`. The single owner of the rooted write and the prompt block format.
- Create `internal/attachment/attachment_test.go`.
- Modify `internal/dashboard/handlers_attachments.go` — workspace endpoint uses `attachment`; adds `writeAttachmentSaveError` shared by both endpoints.
- Create `internal/dashboard/spawn_staging.go` — `spawnStaging` store (`Put`, `Resolve`, `Delete`, `Sweep`) and `handleSpawnAttachment`.
- Create `internal/dashboard/spawn_staging_test.go`.
- Modify `internal/dashboard/server.go` — construct + sweep staging; route `/spawn-attachments`; pass staging to `SpawnHandlers`.
- Modify `internal/dashboard/handlers_spawn.go` — `staging` field; validate `images`/`file_attachments`; resolve ids; per-target rule; release staged files on success.
- Modify `internal/dashboard/api_contract_test.go` — `newTestSpawnHandlers` gets staging; image tests use `Images`; new attachment tests.
- Modify `internal/api/contracts/attachments.go` — `SpawnAttachment`, `SpawnImage`.
- Modify `internal/api/contracts/spawn_request.go` — `Images`, `FileAttachments` replace `ImageAttachments`.
- Create `internal/session/spawn_attachments.go` — `deliverAttachments`.
- Modify `internal/session/manager.go` — `SpawnOptions.Images`/`FileAttachments`; call `deliverAttachments`; chat send uses real media types; remove `writeImageAttachments`.
- Modify `internal/session/spawn_prompt_test.go` — replace `writeImageAttachments` tests with `deliverAttachments` tests.
- Modify `internal/chat/bridge.go` — export `AttachmentExt`.

Frontend (`assets/dashboard/src/`):

- Create `lib/attachments.ts` + `lib/attachments.test.ts` — `withFileAttachments`.
- Create `hooks/useAttachments.ts` + `hooks/useAttachments.test.tsx`.
- Create `components/AttachmentChips.tsx` + `components/AttachmentChips.module.css` (chip styles moved from `chat.module.css`).
- Create `hooks/useFileDrop.ts`, `components/FileDropOverlay.tsx` + `components/FileDropOverlay.module.css` (overlay styles moved from `chat.module.css`).
- Modify `components/chat/Composer.tsx`, `components/chat/ChatView.tsx`, `components/chat/chat.module.css`.
- Modify `lib/api.ts` — `uploadSpawnAttachment`.
- Modify `lib/types.ts` — manual `SpawnRequest` gets `images`/`file_attachments`.
- Modify `lib/spawn-draft.ts` — `images`, `files`.
- Modify `routes/SpawnPage.tsx`; modify `styles/global.css` (one `.spawn-attachments` rule).
- Create `routes/SpawnPage.attachments.test.tsx`.

Docs and scenarios:

- Modify `docs/api.md`, `docs/dashboard-ui.md`.
- Create `test/scenarios/spawn-file-attachments.md` + generated `test/scenarios/generated/spawn-file-attachments.spec.ts`.

---

### Task 1: Shared rooted attachment writer

**Files:**

- Create: `internal/attachment/attachment.go`
- Create: `internal/attachment/attachment_test.go`
- Modify: `internal/dashboard/handlers_attachments.go` (whole handler body after the workspace checks)
- Modify: `docs/api.md` (workspace attachments section, "Errors" list near line 2237)

**Interfaces:**

- Produces:
  - `attachment.MaxSize int64 = 50 << 20`
  - `attachment.ErrReceive error`
  - `attachment.ValidName(name string) bool`
  - `attachment.Save(root *os.Root, parent, name string, src io.Reader) (rel string, err error)` — `rel` is `parent/<uuid>/name`, relative to `root`.
  - `attachment.AppendFileList(prompt string, paths []string) string`
  - `dashboard.writeAttachmentSaveError(w http.ResponseWriter, logger *log.Logger, owner string, err error)`

- [ ] **Step 1: Write the failing tests**

`internal/attachment/attachment_test.go`:

```go
package attachment

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
)

func TestValidName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"data.csv", true},
		{"résumé data.bin", true},
		{"", false},
		{".", false},
		{"..", false},
		{"../escape", false},
		{"/tmp/escape", false},
		{`dir\escape`, false},
		{"line\nbreak", false},
		{"null\x00byte", false},
		{strings.Repeat("a", 256), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidName(tt.name); got != tt.want {
				t.Fatalf("ValidName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestSaveWritesEachFileInItsOwnDirectory(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	first, err := Save(root, filepath.Join(".schmux", "attachments"), "notes.txt", strings.NewReader("one"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Save(root, filepath.Join(".schmux", "attachments"), "notes.txt", strings.NewReader("two"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("same name produced the same path %q", first)
	}
	for rel, want := range map[string]string{first: "one", second: "two"} {
		if filepath.Base(rel) != "notes.txt" || !strings.HasPrefix(rel, filepath.Join(".schmux", "attachments")+string(filepath.Separator)) {
			t.Fatalf("unexpected relative path %q", rel)
		}
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil || string(data) != want {
			t.Fatalf("%s = %q, %v; want %q", rel, data, err, want)
		}
		info, err := os.Stat(filepath.Join(dir, rel))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, %v; want 0600", rel, info.Mode().Perm(), err)
		}
	}
}

func TestSaveRemovesItsDirectoryWhenTheSourceFails(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	_, err = Save(root, "staged", "data.csv", iotest.ErrReader(errors.New("connection reset")))
	if !errors.Is(err, ErrReceive) {
		t.Fatalf("err = %v, want ErrReceive", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "staged"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed save left %v (err %v)", entries, err)
	}
}

func TestSaveRejectsInvalidName(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := Save(root, ".", "../escape", bytes.NewReader(nil)); err == nil {
		t.Fatal("Save accepted ../escape")
	}
}

func TestSaveRejectsEscapingSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, ".schmux")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	if _, err := Save(root, filepath.Join(".schmux", "attachments"), "data.csv", strings.NewReader("x")); err == nil {
		t.Fatal("Save followed a symlink out of the root")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("wrote outside root: %v, %v", entries, err)
	}
}

// The literal below is pinned identically in
// assets/dashboard/src/lib/attachments.test.ts so the Go and TypeScript
// writers of this block cannot drift.
func TestAppendFileList(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		paths  []string
		want   string
	}{
		{"with prompt", "do it", []string{"/a/b.csv", "/c/d.txt"}, "do it\n\nFile attachments:\n/a/b.csv\n/c/d.txt"},
		{"empty prompt", "", []string{"/a/b.csv"}, "File attachments:\n/a/b.csv"},
		{"no paths", "do it", nil, "do it"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AppendFileList(tt.prompt, tt.paths); got != tt.want {
				t.Fatalf("AppendFileList() = %q, want %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/attachment/...`
Expected: FAIL to compile — `undefined: ValidName`, `Save`, `ErrReceive`, `AppendFileList`.

- [ ] **Step 3: Implement the package**

`internal/attachment/attachment.go`:

```go
// Package attachment stores user-supplied files under an os.Root, so a
// symlink inside the root can never redirect a write outside it, and formats
// the prompt block that points an agent at them.
package attachment

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

// MaxSize is the largest file an attachment endpoint accepts.
const MaxSize = 50 << 20

// ErrReceive marks a failure while reading or copying the source, as opposed
// to preparing storage. The underlying error (for example *http.MaxBytesError)
// stays in the chain.
var ErrReceive = errors.New("attachment: receive failed")

// ValidName reports whether name is usable as a single path element.
func ValidName(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 255 &&
		!strings.ContainsAny(name, `/\`) && !strings.ContainsFunc(name, unicode.IsControl)
}

// Save streams src into parent/<uuid>/name under root and returns that path
// relative to root. The file is published (renamed from a temporary name) only
// after src is fully written; on any failure the uuid directory is removed.
func Save(root *os.Root, parent, name string, src io.Reader) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("attachment: invalid filename %q", name)
	}
	if err := root.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("attachment: create %s: %w", parent, err)
	}
	dir := filepath.Join(parent, uuid.NewString())
	if err := root.Mkdir(dir, 0o700); err != nil {
		return "", fmt.Errorf("attachment: create %s: %w", dir, err)
	}
	saved := false
	defer func() {
		if !saved {
			_ = root.RemoveAll(dir)
		}
	}()
	sub, err := root.OpenRoot(dir)
	if err != nil {
		return "", fmt.Errorf("attachment: open %s: %w", dir, err)
	}
	defer sub.Close()
	f, err := sub.OpenFile(".upload", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("attachment: create upload: %w", err)
	}
	_, copyErr := io.Copy(f, src)
	closeErr := f.Close()
	if copyErr != nil {
		return "", fmt.Errorf("%w: %w", ErrReceive, copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("attachment: close upload: %w", closeErr)
	}
	if err := sub.Rename(".upload", name); err != nil {
		return "", fmt.Errorf("attachment: publish %s: %w", name, err)
	}
	saved = true
	return filepath.Join(dir, name), nil
}

// AppendFileList appends the "File attachments:" block the chat composer
// writes (withFileAttachments in assets/dashboard/src/lib/attachments.ts).
func AppendFileList(prompt string, paths []string) string {
	if len(paths) == 0 {
		return prompt
	}
	block := "File attachments:\n" + strings.Join(paths, "\n")
	if prompt == "" {
		return block
	}
	return prompt + "\n\n" + block
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/attachment/...`
Expected: PASS.

- [ ] **Step 5: Move the workspace endpoint onto the package**

In `internal/dashboard/handlers_attachments.go`, delete `const maxWorkspaceAttachmentSize` and replace everything from `name := r.URL.Query().Get("filename")` to the end of the handler with:

```go
	name := r.URL.Query().Get("filename")
	if !attachment.ValidName(name) {
		writeJSONError(w, "invalid filename", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, attachment.MaxSize)
	defer r.Body.Close()

	// Anchor every filesystem operation to the workspace. String-prefix path
	// checks alone would allow .schmux symlinks to redirect writes outside it.
	root, err := os.OpenRoot(ws.Path)
	if err != nil {
		h.logger.Warn("open attachment workspace", "workspace", ws.ID, "err", err)
		writeJSONError(w, "cannot open workspace", http.StatusInternalServerError)
		return
	}
	defer root.Close()
	rel, err := attachment.Save(root, filepath.Join(state.SchmuxDataDirRelative(ws.VCS), "attachments"), name, r.Body)
	if err != nil {
		writeAttachmentSaveError(w, h.logger, ws.ID, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(contracts.WorkspaceAttachment{
		Name: name,
		Path: filepath.Join(ws.Path, rel),
	})
}

// writeAttachmentSaveError maps an attachment.Save failure to the responses
// both attachment endpoints share. owner names the workspace or staging area.
func writeAttachmentSaveError(w http.ResponseWriter, logger *log.Logger, owner string, err error) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		writeJSONError(w, "file exceeds 50 MiB", http.StatusRequestEntityTooLarge)
	case errors.Is(err, attachment.ErrReceive):
		logger.Warn("receive attachment", "owner", owner, "err", err)
		writeJSONError(w, "file upload failed", http.StatusBadRequest)
	default:
		logger.Warn("save attachment", "owner", owner, "err", err)
		writeJSONError(w, "cannot save attachment", http.StatusInternalServerError)
	}
}
```

Fix imports: add `github.com/sergeknystautas/schmux/internal/attachment` and the logger package the file's siblings use for `*log.Logger` (copy the `log` import line from `handlers_spawn.go`); drop `io`, `strings`, `unicode`, and `uuid` if now unused.

- [ ] **Step 6: Run the dashboard attachment tests**

Run: `go test ./internal/dashboard/ -run 'TestWorkspaceAttachment'`
Expected: PASS (existing preserve, invalid-name, and symlink tests unchanged).

- [ ] **Step 7: Update `docs/api.md`**

In the `POST /api/workspaces/{workspaceID}/attachments` "Errors" list, state that `413` is returned for files over 50 MiB and that every storage failure returns `500 cannot save attachment` (details in the daemon log).

---

### Task 2: Spawn staging store and endpoint

**Files:**

- Modify: `internal/api/contracts/attachments.go`
- Create: `internal/dashboard/spawn_staging.go`
- Create: `internal/dashboard/spawn_staging_test.go`
- Modify: `internal/dashboard/server.go` (Server struct near line 289; `NewServer` after the `usageManager` block near line 420; `SpawnHandlers` literal near line 881; CSRF route group near line 980)
- Modify: `internal/dashboard/handlers_spawn.go:40-61` (`SpawnHandlers` struct)
- Modify: `internal/dashboard/api_contract_test.go:122` (`newTestSpawnHandlers`)
- Modify: `docs/api.md` (new section after the workspace attachments section)

**Interfaces:**

- Consumes: `attachment.Save`, `attachment.ValidName`, `attachment.MaxSize`, `writeAttachmentSaveError` (Task 1).
- Produces:
  - `contracts.SpawnAttachment{ID string "json:id"; Name string "json:name"}`
  - `newSpawnStaging(dir string, now func() time.Time) *spawnStaging`
  - `(*spawnStaging).Put(name string, src io.Reader) (contracts.SpawnAttachment, error)`
  - `(*spawnStaging).Resolve(ids []string) ([]string, error)` — absolute staged file paths, in order; error text `attachment no longer available: <id>`.
  - `(*spawnStaging).Delete(ids []string)`
  - `(*spawnStaging).Sweep(maxAge time.Duration) error`
  - `SpawnHandlers.staging *spawnStaging`, `Server.spawnStaging *spawnStaging`
  - Route `POST /api/spawn-attachments?filename=<name>` → `201 {id, name}`

- [ ] **Step 1: Add the contract type**

Append to `internal/api/contracts/attachments.go`:

```go
// SpawnAttachment is a file staged for a spawn request before its workspace
// exists. The spawn request references it by ID.
type SpawnAttachment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
```

Run: `go run ./cmd/gen-types`
Expected: `types.generated.ts` gains `SpawnAttachment`.

- [ ] **Step 2: Write the failing tests**

`internal/dashboard/spawn_staging_test.go`:

```go
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

	"github.com/sergeknystautas/schmux/internal/attachment"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
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
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/dashboard/ -run 'TestSpawnStaging|TestHandleSpawnAttachment'`
Expected: FAIL to compile — `undefined: newSpawnStaging`, `h.staging undefined`.

- [ ] **Step 4: Implement the store and endpoint**

`internal/dashboard/spawn_staging.go`:

```go
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
```

The test case `6ba7b810-9dad-11d1-80b4-00c04fd430c8` is a canonical uuid that was never staged; it must fail through the "no such directory" branch.

- [ ] **Step 5: Wire it into the server**

In `internal/dashboard/handlers_spawn.go`, add to `SpawnHandlers` after `spawnStore`:

```go
	staging        *spawnStaging
```

In `internal/dashboard/server.go`:

- Add the field `spawnStaging *spawnStaging` next to `spawnStore *spawn.Store`.
- In `NewServer`, after `s.usageManager.Load()`:

```go
	s.spawnStaging = newSpawnStaging(filepath.Join(schmuxdir.Get(), "spawn-attachments"), time.Now)
	if err := s.spawnStaging.Sweep(24 * time.Hour); err != nil {
		logger.Warn("sweep spawn attachments", "err", err)
	}
```

- In the `&SpawnHandlers{...}` literal, add `staging: s.spawnStaging,`.
- In the CSRF route group, after `r.Post("/spawn", spawnH.handleSpawnPost)`:

```go
			r.Post("/spawn-attachments", spawnH.handleSpawnAttachment)
```

In `internal/dashboard/api_contract_test.go` `newTestSpawnHandlers`, add `staging: s.spawnStaging,`.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/dashboard/ -run 'TestSpawnStaging|TestHandleSpawnAttachment|TestWorkspaceAttachment'`
Expected: PASS.

- [ ] **Step 7: Document the endpoint in `docs/api.md`**

Add after the workspace attachments section:

```markdown
### POST /api/spawn-attachments

Stages one file for the spawn form's **Attach** action, before the spawn's workspace exists. Uses the standard API authentication and CSRF middleware. Same request shape and limits as `POST /api/workspaces/{workspaceID}/attachments`: `filename` query parameter, raw body, 50 MiB cap.

The daemon stores the file at `~/.schmux/spawn-attachments/<id>/<filename>` (mode 0600) and returns `201 Created`, `{"id":"<uuid>","name":"data.csv"}`. A spawn request references it through `file_attachments`. Staged files are deleted when a spawn that references them starts at least one session, and any staged file older than 24 hours is removed when the daemon starts.

Errors: `400 invalid filename`, `400 file upload failed`, `413 file exceeds 50 MiB`, `500 cannot save attachment`.
```

---

### Task 3: Images carry their media type end to end

**Files:**

- Modify: `internal/chat/bridge.go:63-88` (export `AttachmentExt`)
- Modify: `internal/api/contracts/attachments.go`, `internal/api/contracts/spawn_request.go:22`
- Create: `internal/session/spawn_attachments.go`
- Modify: `internal/session/manager.go` (`SpawnOptions` near line 963; image block near lines 1109-1121; chat send near lines 1306-1312; delete `writeImageAttachments` at lines 1011-1034)
- Modify: `internal/session/spawn_prompt_test.go` (replace the two `TestWriteImageAttachments*` tests)
- Modify: `internal/dashboard/handlers_spawn.go` (image validation near line 275; `session.SpawnOptions` literal near line 554)
- Modify: `internal/dashboard/api_contract_test.go:283-378` (image subtests)
- Modify: `assets/dashboard/src/lib/types.ts:176`, `assets/dashboard/src/routes/SpawnPage.tsx:845`
- Modify: `docs/api.md:521,544`

**Interfaces:**

- Consumes: `attachment.Save` (Task 1).
- Produces:
  - `chat.AttachmentExt(mediaType string) string`
  - `contracts.SpawnImage{MediaType string "json:media_type"; Data string "json:data"}`
  - `contracts.SpawnRequest.Images []SpawnImage "json:images,omitempty"` (replaces `ImageAttachments`)
  - `session.SpawnOptions.Images []chat.Image` (replaces `ImageAttachments`)
  - `session.deliverAttachments(w *state.Workspace, isChat bool, prompt string, files []string, images []chat.Image) (string, error)` — `files` is unused until Task 4.

- [ ] **Step 1: Export the chat extension helper**

In `internal/chat/bridge.go`, rename `attachmentExt` to `AttachmentExt` (definition, its doc comment, and the call in `PersistAttachment`). Run `grep -rn "attachmentExt" internal/` and update any remaining callers.

- [ ] **Step 2: Write the failing tests**

Replace `TestWriteImageAttachments` and `TestWriteImageAttachments_InvalidBase64Skipped` in `internal/session/spawn_prompt_test.go` with:

```go
func TestDeliverAttachments_TerminalImagesKeepTheirMediaType(t *testing.T) {
	w := &state.Workspace{Path: t.TempDir(), VCS: "git"}
	png := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	jpg := base64.StdEncoding.EncodeToString([]byte("jpg-bytes"))

	prompt, err := deliverAttachments(w, false, "look", nil, []chat.Image{
		{MediaType: "image/png", Data: png},
		{MediaType: "image/jpeg", Data: jpg},
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(prompt, "\n")
	if lines[0] != "look" || lines[2] != "Image attachments:" || len(lines) != 5 {
		t.Fatalf("prompt = %q", prompt)
	}
	for i, want := range []struct{ ext, data string }{{".png", "png-bytes"}, {".jpg", "jpg-bytes"}} {
		path := strings.TrimPrefix(lines[3+i], fmt.Sprintf("Image #%d: ", i+1))
		if filepath.Ext(path) != want.ext || !strings.HasPrefix(path, filepath.Join(w.Path, ".schmux", "attachments")+string(filepath.Separator)) {
			t.Fatalf("image %d path = %q", i+1, path)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want.data {
			t.Fatalf("image %d bytes = %q, %v", i+1, data, err)
		}
	}
}

func TestDeliverAttachments_ChatLeavesImagesInline(t *testing.T) {
	w := &state.Workspace{Path: t.TempDir(), VCS: "git"}
	img := chat.Image{MediaType: "image/webp", Data: base64.StdEncoding.EncodeToString([]byte("x"))}
	prompt, err := deliverAttachments(w, true, "look", nil, []chat.Image{img})
	if err != nil || prompt != "look" {
		t.Fatalf("prompt = %q, err = %v", prompt, err)
	}
	if _, err := os.Stat(filepath.Join(w.Path, ".schmux", "attachments")); !os.IsNotExist(err) {
		t.Fatalf("chat spawn wrote image files: %v", err)
	}
}

func TestDeliverAttachments_InvalidImageFailsTheSpawn(t *testing.T) {
	w := &state.Workspace{Path: t.TempDir(), VCS: "git"}
	_, err := deliverAttachments(w, false, "look", nil, []chat.Image{{MediaType: "image/png", Data: "!!!invalid!!!"}})
	if err == nil {
		t.Fatal("invalid base64 was skipped instead of failing the spawn")
	}
}
```

Add imports as needed (`encoding/base64`, `fmt`, `os`, `path/filepath`, `strings`, `internal/chat`, `internal/state`).

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/session/ -run TestDeliverAttachments`
Expected: FAIL to compile — `undefined: deliverAttachments`.

- [ ] **Step 4: Implement `deliverAttachments` (images only)**

`internal/session/spawn_attachments.go`:

```go
package session

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/attachment"
	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/state"
)

// deliverAttachments writes a spawn's attachments into its workspace and
// returns the prompt that points the agent at them. Every call writes its own
// copies, so each session's paths are inside its own workspace. Chat sessions
// carry images inline in the protocol message, so only terminal sessions get
// image files. Any write failure fails the spawn rather than starting the
// agent without its attachments.
func deliverAttachments(w *state.Workspace, isChat bool, prompt string, files []string, images []chat.Image) (string, error) {
	writeImages := !isChat && len(images) > 0
	if len(files) == 0 && !writeImages {
		return prompt, nil
	}
	root, err := os.OpenRoot(w.Path)
	if err != nil {
		return "", fmt.Errorf("open workspace: %w", err)
	}
	defer root.Close()
	parent := filepath.Join(state.SchmuxDataDirRelative(w.VCS), "attachments")

	if writeImages {
		paths := make([]string, 0, len(images))
		for i, img := range images {
			data, err := base64.StdEncoding.DecodeString(img.Data)
			if err != nil {
				return "", fmt.Errorf("decode image %d: %w", i+1, err)
			}
			name := fmt.Sprintf("img-%s.%s", uuid.NewString()[:8], chat.AttachmentExt(img.MediaType))
			rel, err := attachment.Save(root, parent, name, bytes.NewReader(data))
			if err != nil {
				return "", fmt.Errorf("write image %d: %w", i+1, err)
			}
			paths = append(paths, filepath.Join(w.Path, rel))
		}
		prompt = appendImagePathsToPrompt(prompt, paths)
	}
	return prompt, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/session/ -run 'TestDeliverAttachments|TestAppendImagePathsToPrompt'`
Expected: PASS.

- [ ] **Step 6: Switch `SpawnOptions` and `Spawn` to `[]chat.Image`**

In `internal/session/manager.go`:

- In `SpawnOptions`, replace `ImageAttachments []string // base64-encoded PNGs ...` with:

```go
	Images           []chat.Image // inline images; written into the workspace for terminal spawns
```

- Delete `writeImageAttachments` (keep `appendImagePathsToPrompt`).
- Replace the block starting `// Write image attachments to workspace and append paths to prompt.` through its closing `}` with:

```go
	// Deliver attachments into this workspace and point the prompt at them.
	prompt, err := deliverAttachments(w, isChat, opts.Prompt, nil, opts.Images)
	if err != nil {
		return nil, fmt.Errorf("failed to deliver attachments: %w", err)
	}
	opts.Prompt = prompt
```

- Replace the chat send block with:

```go
	if isChat && (strings.TrimSpace(opts.Prompt) != "" || len(opts.Images) > 0) {
		if rt := m.ensureChatRuntime(sess.ID); rt != nil {
			if _, err := rt.Send(opts.Prompt, opts.Images); err != nil {
				m.logger.Warn("failed to send initial chat message", "session", sess.ID, "err", err)
			}
		}
	}
```

Remove imports that become unused (`encoding/base64` if nothing else uses it).

- [ ] **Step 7: Switch the contract and handler**

Append to `internal/api/contracts/attachments.go`:

```go
// SpawnImage is an inline image attached to a spawn prompt, the same shape
// the chat composer sends.
type SpawnImage struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"` // base64
}
```

In `internal/api/contracts/spawn_request.go`, replace the `ImageAttachments` field with:

```go
	Images           []SpawnImage   `json:"images,omitempty"`            // inline images with media type, max 5
```

In `internal/dashboard/handlers_spawn.go`, replace the `// Validate image attachments` block with:

```go
	// Validate attachments
	if len(req.Images) > 0 {
		if len(req.Images) > 5 {
			writeJSONError(w, "maximum 5 image attachments allowed", http.StatusBadRequest)
			return
		}
		for _, img := range req.Images {
			if !strings.HasPrefix(img.MediaType, "image/") {
				writeJSONError(w, "image attachments must have an image media type", http.StatusBadRequest)
				return
			}
		}
		if req.Resume {
			writeJSONError(w, "cannot use attachments with resume mode", http.StatusBadRequest)
			return
		}
		if req.Command != "" {
			writeJSONError(w, "cannot use attachments with command mode", http.StatusBadRequest)
			return
		}
		if req.RemoteProfileID != "" {
			writeJSONError(w, "attachments are not supported for remote spawns", http.StatusBadRequest)
			return
		}
	}
	images := make([]chat.Image, len(req.Images))
	for i, img := range req.Images {
		images[i] = chat.Image{MediaType: img.MediaType, Data: img.Data}
	}
```

and in the `session.SpawnOptions{...}` literal replace `ImageAttachments: req.ImageAttachments,` with `Images: images,`. Add the `internal/chat` import.

In `internal/dashboard/api_contract_test.go`, in each image subtest replace `ImageAttachments: []string{...}` with `Images: []contracts.SpawnImage{...}` using `{MediaType: "image/png", Data: "iVBORw0KGgo="}` per element (keep the 6- and 5-element counts). Add one subtest:

```go
	t.Run("non-image media type rejected", func(t *testing.T) {
		body, _ := json.Marshal(SpawnRequest{
			Repo:    "https://example.com/repo.git",
			Branch:  "main",
			Targets: map[string]int{"claude": 1},
			Images:  []contracts.SpawnImage{{MediaType: "text/plain", Data: "eA=="}},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		spawnH.handleSpawnPost(rr, req)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "image media type") {
			t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
		}
	})
```

- [ ] **Step 8: Keep the frontend compiling**

Run `go run ./cmd/gen-types`. In `assets/dashboard/src/lib/types.ts`, replace `image_attachments?: string[];` with:

```ts
  images?: SpawnImage[]; // inline images with media type, max 5
```

and add `SpawnImage` to the `import type { ... } from './types.generated'` list at the top of the file.

In `SpawnPage.tsx` replace `image_attachments: imageAttachments.length > 0 ? imageAttachments : undefined,` with the interim mapping below (Task 7 replaces it; today's paste listener already labels every image PNG, so behavior is unchanged):

```ts
      images:
        imageAttachments.length > 0
          ? imageAttachments.map((data) => ({ media_type: 'image/png', data }))
          : undefined,
```

- [ ] **Step 9: Run the affected suites**

Run: `go build ./... && go test ./internal/session/ ./internal/dashboard/ ./internal/chat/ && ./test.sh --quick`
Expected: PASS.

- [ ] **Step 10: Update `docs/api.md`**

Replace the `image_attachments` example line with `"images": [{"media_type": "image/png", "data": "<base64>"}],` and replace its bullet with:

```markdown
- `images` is optional. Up to 5 inline images, each `{media_type, data}` with an `image/*` media type and base64 data. Terminal sessions get each image written to `{workspace}/.schmux/attachments/<id>/img-<id>.<ext>` (`.sl/schmux/attachments/` for Sapling; extension from the media type) with an `Image attachments:` block appended to the prompt. Chat sessions receive the images inline in their first message. A write failure fails that target. Cannot be used with `resume`, `command`, or `remote_profile_id`, and command targets in the same request fail with `attachments are not allowed for command targets`.
```

---

### Task 4: Staged files reach every spawned workspace

**Files:**

- Modify: `internal/api/contracts/spawn_request.go`
- Modify: `internal/session/spawn_attachments.go`, `internal/session/manager.go` (`SpawnOptions`, the `deliverAttachments` call)
- Modify: `internal/session/spawn_prompt_test.go`
- Modify: `internal/dashboard/handlers_spawn.go` (validation block, target loop near line 456, result tail near line 582)
- Modify: `internal/dashboard/api_contract_test.go`
- Modify: `docs/api.md`

**Interfaces:**

- Consumes: `attachment.Save`, `attachment.AppendFileList` (Task 1); `spawnStaging.Resolve`, `spawnStaging.Delete`, `spawnStaging.Put` (Task 2); `deliverAttachments`, `images` (Task 3).
- Produces:
  - `contracts.SpawnRequest.FileAttachments []string "json:file_attachments,omitempty"`
  - `session.SpawnOptions.FileAttachments []string` — absolute staged paths
  - `(*SpawnHandlers).releaseStaged(ids []string, results []SessionResult)`

- [ ] **Step 1: Write the failing session tests**

Append to `internal/session/spawn_prompt_test.go`:

```go
func stageFile(t *testing.T, name, contents string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "staged-"+name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDeliverAttachments_FilesGetTheChatBlockForEveryKind(t *testing.T) {
	for _, tt := range []struct {
		vcs    string
		isChat bool
		subdir string
	}{
		{"git", false, filepath.Join(".schmux", "attachments")},
		{"git", true, filepath.Join(".schmux", "attachments")},
		{"sapling", false, filepath.Join(".sl", "schmux", "attachments")},
	} {
		t.Run(fmt.Sprintf("%s chat=%v", tt.vcs, tt.isChat), func(t *testing.T) {
			w := &state.Workspace{Path: t.TempDir(), VCS: tt.vcs}
			// Same basename from two folders: both must arrive, at distinct paths.
			a := stageFile(t, "notes.txt", "from a")
			b := stageFile(t, "notes.txt", "from b")

			prompt, err := deliverAttachments(w, tt.isChat, "build it", []string{a, b}, nil)
			if err != nil {
				t.Fatal(err)
			}
			head, list, ok := strings.Cut(prompt, "\n\nFile attachments:\n")
			if !ok || head != "build it" {
				t.Fatalf("prompt = %q", prompt)
			}
			paths := strings.Split(list, "\n")
			if len(paths) != 2 || paths[0] == paths[1] {
				t.Fatalf("paths = %q", paths)
			}
			for i, want := range []string{"from a", "from b"} {
				if !strings.HasPrefix(paths[i], filepath.Join(w.Path, tt.subdir)+string(filepath.Separator)) || filepath.Base(paths[i]) != "notes.txt" {
					t.Fatalf("path %d = %q", i, paths[i])
				}
				data, err := os.ReadFile(paths[i])
				if err != nil || string(data) != want {
					t.Fatalf("path %d holds %q, %v", i, data, err)
				}
			}
		})
	}
}

func TestDeliverAttachments_FileBlockPrecedesImageBlock(t *testing.T) {
	w := &state.Workspace{Path: t.TempDir(), VCS: "git"}
	f := stageFile(t, "data.csv", "x")
	img := chat.Image{MediaType: "image/png", Data: base64.StdEncoding.EncodeToString([]byte("p"))}
	prompt, err := deliverAttachments(w, false, "", []string{f}, []chat.Image{img})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(prompt, "File attachments:\n") || !strings.Contains(prompt, "\n\nImage attachments:\nImage #1: ") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestDeliverAttachments_MissingStagedFileFailsTheSpawn(t *testing.T) {
	w := &state.Workspace{Path: t.TempDir(), VCS: "git"}
	_, err := deliverAttachments(w, false, "x", []string{filepath.Join(t.TempDir(), "gone.csv")}, nil)
	if err == nil {
		t.Fatal("missing staged file did not fail the spawn")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/session/ -run TestDeliverAttachments`
Expected: FAIL — files are ignored, so `File attachments:` is missing and the missing-file case returns no error.

- [ ] **Step 3: Copy files in `deliverAttachments`**

In `internal/session/spawn_attachments.go`, insert before `if writeImages {`:

```go
	if len(files) > 0 {
		paths := make([]string, 0, len(files))
		for _, staged := range files {
			rel, err := copyStaged(root, parent, staged)
			if err != nil {
				return "", err
			}
			paths = append(paths, filepath.Join(w.Path, rel))
		}
		prompt = attachment.AppendFileList(prompt, paths)
	}
```

and add:

```go
// copyStaged copies one daemon-staged file into the workspace under parent,
// keeping its original filename.
func copyStaged(root *os.Root, parent, staged string) (string, error) {
	f, err := os.Open(staged)
	if err != nil {
		return "", fmt.Errorf("open staged attachment: %w", err)
	}
	defer f.Close()
	rel, err := attachment.Save(root, parent, filepath.Base(staged), f)
	if err != nil {
		return "", fmt.Errorf("copy %s: %w", filepath.Base(staged), err)
	}
	return rel, nil
}
```

In `internal/session/manager.go`, add to `SpawnOptions` after `Images`:

```go
	FileAttachments  []string     // absolute staged file paths, copied into the workspace during spawn
```

and change the call to `deliverAttachments(w, isChat, opts.Prompt, opts.FileAttachments, opts.Images)`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/session/ -run TestDeliverAttachments`
Expected: PASS.

- [ ] **Step 5: Write the failing handler tests**

Add a new test function to `internal/dashboard/api_contract_test.go`:

```go
func TestAPIContract_SpawnFileAttachments(t *testing.T) {
	// newTestServer configures one run target, "command" — a non-promptable
	// command target.
	server, _, _ := newTestServer(t)
	spawnH := newTestSpawnHandlers(server)

	stage := func(t *testing.T) string {
		t.Helper()
		a, err := spawnH.staging.Put("users.csv", strings.NewReader("id\n"))
		if err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	post := func(req SpawnRequest) *httptest.ResponseRecorder {
		body, _ := json.Marshal(req)
		rr := httptest.NewRecorder()
		spawnH.handleSpawnPost(rr, httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(body)))
		return rr
	}

	t.Run("unknown staging id fails the whole request", func(t *testing.T) {
		rr := post(SpawnRequest{
			Repo: "https://example.com/repo.git", Branch: "main",
			Targets:         map[string]int{"claude": 1},
			FileAttachments: []string{"6ba7b810-9dad-11d1-80b4-00c04fd430c8"},
		})
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "attachment no longer available: 6ba7b810-9dad-11d1-80b4-00c04fd430c8") {
			t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
		}
	})

	for _, tt := range []struct {
		name string
		req  SpawnRequest
		want string
	}{
		{"resume", SpawnRequest{Repo: "https://example.com/repo.git", Branch: "main", Targets: map[string]int{"claude": 1}, Resume: true}, "cannot use attachments with resume mode"},
		{"command", SpawnRequest{Repo: "https://example.com/repo.git", Branch: "main", Command: "echo hi"}, "cannot use attachments with command mode"},
		{"remote", SpawnRequest{RemoteProfileID: "p", RemoteFlavor: "f", Targets: map[string]int{"claude": 1}}, "attachments are not supported for remote spawns"},
	} {
		t.Run(tt.name+" rejected", func(t *testing.T) {
			tt.req.FileAttachments = []string{stage(t)}
			rr := post(tt.req)
			if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), tt.want) {
				t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
			}
		})
	}

	t.Run("command target fails per target and keeps the staged file", func(t *testing.T) {
		id := stage(t)
		rr := post(SpawnRequest{
			Repo: "https://example.com/repo.git", Branch: "main",
			Targets:         map[string]int{"command": 1},
			FileAttachments: []string{id},
		})
		var results []SessionResult
		if err := json.Unmarshal(rr.Body.Bytes(), &results); err != nil {
			t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
		}
		if len(results) != 1 || results[0].Error != "attachments are not allowed for command targets" {
			t.Fatalf("results = %+v", results)
		}
		if _, err := spawnH.staging.Resolve([]string{id}); err != nil {
			t.Fatalf("staged file deleted after every target failed: %v", err)
		}
	})
}

func TestReleaseStaged(t *testing.T) {
	server, _, _ := newTestServer(t)
	h := newTestSpawnHandlers(server)
	for _, tt := range []struct {
		name    string
		results []SessionResult
		kept    bool
	}{
		{"every target failed", []SessionResult{{Error: "boom"}, {Error: "boom"}}, true},
		{"no results", nil, true},
		{"one target succeeded", []SessionResult{{SessionID: "s1"}, {Error: "boom"}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, err := h.staging.Put("data.csv", strings.NewReader("x"))
			if err != nil {
				t.Fatal(err)
			}
			h.releaseStaged([]string{a.ID}, tt.results)
			_, err = h.staging.Resolve([]string{a.ID})
			if kept := err == nil; kept != tt.kept {
				t.Fatalf("kept = %v, want %v", kept, tt.kept)
			}
		})
	}
}
```

- [ ] **Step 6: Run tests to verify they fail**

Run: `go test ./internal/dashboard/ -run 'TestAPIContract_SpawnFileAttachments|TestReleaseStaged'`
Expected: FAIL to compile — `unknown field FileAttachments`, `undefined: releaseStaged`.

- [ ] **Step 7: Implement the request field and handler rules**

In `internal/api/contracts/spawn_request.go`, after `Images`:

```go
	FileAttachments  []string       `json:"file_attachments,omitempty"`  // staging ids from POST /api/spawn-attachments
```

In `handleSpawnPost`, change the validation guard from `if len(req.Images) > 0 {` to `if len(req.Images) > 0 || len(req.FileAttachments) > 0 {`, keeping the `len(req.Images) > 5` and media-type checks inside it. After the `images` conversion loop add:

```go
	stagedFiles, err := h.staging.Resolve(req.FileAttachments)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	hasAttachments := len(images) > 0 || len(stagedFiles) > 0
```

(If `err` is already declared in that scope, use `=` or a distinct name so it compiles.)

In the target loop, after the existing `!promptable && strings.TrimSpace(req.Prompt) != ""` block:

```go
		if !promptable && hasAttachments {
			results = append(results, SessionResult{
				Target: targetName,
				Error:  "attachments are not allowed for command targets",
			})
			continue
		}
```

In the local `session.SpawnOptions{...}` literal add `FileAttachments: stagedFiles,`.

After `writeSpawnLog(h.logger, req, results)` add:

```go
	h.releaseStaged(req.FileAttachments, results)
```

and add the method:

```go
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
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go run ./cmd/gen-types && go test ./internal/dashboard/ ./internal/session/`
Expected: PASS.

- [ ] **Step 9: Update `docs/api.md`**

Add `"file_attachments": ["<staging id>"],` to the spawn request example and this bullet after `images`:

```markdown
- `file_attachments` is optional. Staging ids returned by `POST /api/spawn-attachments`. The request fails with `400 attachment no longer available: <id>` before anything spawns if any id is unknown. Each spawned session copies every file into `{workspace}/.schmux/attachments/<id>/<filename>` (`.sl/schmux/…` for Sapling) and its prompt ends with `File attachments:` followed by one absolute path per line — the same block the chat composer sends — ahead of any `Image attachments:` block. A copy failure fails that target. Staged files are deleted once at least one target starts. Same mode restrictions as `images`.
```

---

### Task 5: Shared attachment hook and chips; Composer uses them

**Files:**

- Create: `assets/dashboard/src/lib/attachments.ts`, `assets/dashboard/src/lib/attachments.test.ts`
- Create: `assets/dashboard/src/hooks/useAttachments.ts`, `assets/dashboard/src/hooks/useAttachments.test.tsx`
- Create: `assets/dashboard/src/components/AttachmentChips.tsx`, `assets/dashboard/src/components/AttachmentChips.module.css`
- Modify: `assets/dashboard/src/components/chat/Composer.tsx`
- Modify: `assets/dashboard/src/components/chat/chat.module.css:352-380` (remove `.chips`, `.chip`, `.chip img`, `.fileName`)

**Interfaces:**

- Produces:
  - `withFileAttachments(text: string, paths: string[]): string`
  - `MAX_ATTACHMENT_BYTES = 50 * 1024 * 1024`
  - `useAttachments<F>(options: UseAttachmentsOptions<F>): Attachments<F>` where
    `UseAttachmentsOptions<F> = { upload(file: File): Promise<F>; initialImages?: ChatImage[]; initialFiles?: F[]; maxImages?: number; disabled?: boolean }` and
    `Attachments<F> = { images: ChatImage[]; files: F[]; attaching: boolean; error: string | null; attachFiles(files: Iterable<File>): Promise<void>; removeImage(index: number): void; removeFile(index: number): void; restore(images: ChatImage[], files: F[]): void; clear(): void }`.
    `attachFiles`, `removeImage`, `removeFile`, `restore`, `clear` are referentially stable except `attachFiles`, which changes with `disabled`/`attaching`.
  - `<AttachmentChips images files attaching error onRemoveImage onRemoveFile testIdPrefix disabled? />` with `files: { name: string; title?: string }[]`; test ids `${testIdPrefix}-image-chip`, `${testIdPrefix}-file-chip`.

- [ ] **Step 1: Write the failing tests**

`assets/dashboard/src/lib/attachments.test.ts`:

```ts
import { describe, it, expect } from 'vitest';
import { withFileAttachments } from './attachments';

// The first literal is pinned identically in internal/attachment
// TestAppendFileList so the Go and TypeScript writers cannot drift.
describe('withFileAttachments', () => {
  it('appends the block after the text', () => {
    expect(withFileAttachments('do it', ['/a/b.csv', '/c/d.txt'])).toBe(
      'do it\n\nFile attachments:\n/a/b.csv\n/c/d.txt'
    );
  });
  it('omits the blank lines when there is no text', () => {
    expect(withFileAttachments('', ['/a/b.csv'])).toBe('File attachments:\n/a/b.csv');
  });
  it('returns the text unchanged without paths', () => {
    expect(withFileAttachments('do it', [])).toBe('do it');
  });
});
```

`assets/dashboard/src/hooks/useAttachments.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { useAttachments } from './useAttachments';

vi.mock('../lib/api', () => ({
  getErrorMessage: (err: unknown, fallback: string) =>
    err instanceof Error ? err.message : fallback,
}));

const png = (name: string) => new File(['p'], name, { type: 'image/png' });
const csv = (name: string) => new File(['c'], name, { type: 'text/csv' });

describe('useAttachments', () => {
  it('reads images inline with their media type and uploads other files', async () => {
    const upload = vi.fn(async (f: File) => ({ id: `id-${f.name}`, name: f.name }));
    const { result } = renderHook(() => useAttachments({ upload }));
    const jpeg = new File(['j'], 'photo.jpg', { type: 'image/jpeg' });

    await act(() => result.current.attachFiles([jpeg, csv('users.csv')]));

    expect(result.current.images).toEqual([{ media_type: 'image/jpeg', data: 'ag==' }]);
    expect(result.current.files).toEqual([{ id: 'id-users.csv', name: 'users.csv' }]);
    expect(upload).toHaveBeenCalledTimes(1);
    expect(result.current.error).toBeNull();
  });

  it('reports attaching until the upload settles', async () => {
    let finish!: (v: { id: string; name: string }) => void;
    const upload = vi.fn(() => new Promise<{ id: string; name: string }>((r) => (finish = r)));
    const { result } = renderHook(() => useAttachments({ upload }));

    let pending!: Promise<void>;
    act(() => {
      pending = result.current.attachFiles([csv('a.csv')]);
    });
    await waitFor(() => expect(result.current.attaching).toBe(true));
    await act(async () => {
      finish({ id: 'a', name: 'a.csv' });
      await pending;
    });
    expect(result.current.attaching).toBe(false);
  });

  it('reports a failed upload as filename: message and adds no file', async () => {
    const upload = vi.fn(async () => {
      throw new Error('file exceeds 50 MiB');
    });
    const { result } = renderHook(() => useAttachments({ upload }));
    await act(() => result.current.attachFiles([csv('big.csv')]));
    expect(result.current.files).toEqual([]);
    expect(result.current.error).toBe('big.csv: file exceeds 50 MiB');
  });

  it('caps images at maxImages and names the rejected file', async () => {
    const { result } = renderHook(() => useAttachments({ upload: vi.fn(), maxImages: 2 }));
    await act(() => result.current.attachFiles([png('1.png'), png('2.png'), png('3.png')]));
    expect(result.current.images).toHaveLength(2);
    expect(result.current.error).toBe('3.png: maximum 2 images');
  });

  it('ignores attach requests while disabled', async () => {
    const upload = vi.fn();
    const { result } = renderHook(() => useAttachments({ upload, disabled: true }));
    await act(() => result.current.attachFiles([csv('a.csv'), png('b.png')]));
    expect(upload).not.toHaveBeenCalled();
    expect(result.current.images).toEqual([]);
  });

  it('restores, removes, and clears', () => {
    const { result } = renderHook(() =>
      useAttachments<{ id: string; name: string }>({ upload: vi.fn() })
    );
    act(() =>
      result.current.restore(
        [{ media_type: 'image/png', data: 'AA==' }],
        [
          { id: '1', name: 'a.csv' },
          { id: '2', name: 'b.csv' },
        ]
      )
    );
    act(() => result.current.removeFile(0));
    expect(result.current.files).toEqual([{ id: '2', name: 'b.csv' }]);
    act(() => result.current.removeImage(0));
    expect(result.current.images).toEqual([]);
    act(() => result.current.clear());
    expect(result.current.files).toEqual([]);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `./test.sh --quick`
Expected: FAIL — cannot resolve `./attachments` and `./useAttachments`.

- [ ] **Step 3: Implement `lib/attachments.ts`**

```ts
// Shared attachment helpers for the chat composer and the spawn form.

/** Largest file either attachment endpoint accepts. */
export const MAX_ATTACHMENT_BYTES = 50 * 1024 * 1024;

/**
 * Appends the "File attachments:" block that points an agent at uploaded
 * files. The daemon writes the same block for spawn prompts
 * (AppendFileList in internal/attachment/attachment.go).
 */
export function withFileAttachments(text: string, paths: string[]): string {
  if (paths.length === 0) return text;
  return `${text ? `${text}\n\n` : ''}File attachments:\n${paths.join('\n')}`;
}
```

- [ ] **Step 4: Implement `hooks/useAttachments.ts`**

```ts
import { useCallback, useRef, useState } from 'react';
import type { ChatImage } from '../lib/chat/types';
import { getErrorMessage } from '../lib/api';
import { MAX_ATTACHMENT_BYTES } from '../lib/attachments';

export interface UseAttachmentsOptions<F> {
  /** Stores one non-image file and returns its record. */
  upload(file: File): Promise<F>;
  initialImages?: ChatImage[];
  initialFiles?: F[];
  /** Images beyond this count are rejected with an error. */
  maxImages?: number;
  disabled?: boolean;
}

export interface Attachments<F> {
  images: ChatImage[];
  files: F[];
  attaching: boolean;
  error: string | null;
  attachFiles(files: Iterable<File>): Promise<void>;
  removeImage(index: number): void;
  removeFile(index: number): void;
  restore(images: ChatImage[], files: F[]): void;
  clear(): void;
}

function readFileAsImage(file: File): Promise<ChatImage> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const url = String(reader.result);
      const base64 = url.slice(url.indexOf(',') + 1);
      resolve({ media_type: file.type || 'image/png', data: base64 });
    };
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

/**
 * Attachment state shared by the chat composer and the spawn form: images are
 * read inline with their media type, other files go through `upload`.
 */
export function useAttachments<F>({
  upload,
  initialImages,
  initialFiles,
  maxImages,
  disabled = false,
}: UseAttachmentsOptions<F>): Attachments<F> {
  const [images, setImages] = useState<ChatImage[]>(initialImages ?? []);
  const [files, setFiles] = useState<F[]>(initialFiles ?? []);
  const [attaching, setAttaching] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const uploadRef = useRef(upload);
  uploadRef.current = upload;
  const imageCountRef = useRef(images.length);
  imageCountRef.current = images.length;

  const attachFiles = useCallback(
    async (selectedFiles: Iterable<File>) => {
      if (disabled || attaching) return;
      // Snapshot the selection before the caller clears a native file input.
      const selection = Array.from(selectedFiles);
      setAttaching(true);
      setError(null);
      let imageCount = imageCountRef.current;
      try {
        for (const file of selection) {
          try {
            if (file.type.startsWith('image/')) {
              if (maxImages !== undefined && imageCount >= maxImages) {
                throw new Error(`maximum ${maxImages} images`);
              }
              imageCount += 1;
              const img = await readFileAsImage(file);
              setImages((prev) => [...prev, img]);
            } else {
              if (file.size > MAX_ATTACHMENT_BYTES) throw new Error('File exceeds 50 MiB');
              const record = await uploadRef.current(file);
              setFiles((prev) => [...prev, record]);
            }
          } catch (err) {
            setError(`${file.name}: ${getErrorMessage(err, 'Failed to attach file')}`);
          }
        }
      } finally {
        setAttaching(false);
      }
    },
    [attaching, disabled, maxImages]
  );

  const removeImage = useCallback(
    (index: number) => setImages((prev) => prev.filter((_, i) => i !== index)),
    []
  );
  const removeFile = useCallback(
    (index: number) => setFiles((prev) => prev.filter((_, i) => i !== index)),
    []
  );
  const restore = useCallback((nextImages: ChatImage[], nextFiles: F[]) => {
    setImages(nextImages);
    setFiles(nextFiles);
  }, []);
  const clear = useCallback(() => {
    setImages([]);
    setFiles([]);
    setError(null);
  }, []);

  return {
    images,
    files,
    attaching,
    error,
    attachFiles,
    removeImage,
    removeFile,
    restore,
    clear,
  };
}
```

- [ ] **Step 5: Implement `AttachmentChips`**

`assets/dashboard/src/components/AttachmentChips.module.css` — move the four rules verbatim from `chat.module.css` (and delete them there):

```css
.chips {
  display: flex;
  gap: var(--spacing-xs);
  flex-wrap: wrap;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-xs);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  padding: var(--spacing-xxs) var(--spacing-xs);
  font-size: 0.75rem;
}

.chip img {
  width: 28px;
  height: 28px;
  object-fit: cover;
  border-radius: var(--radius-sm);
}

.fileName {
  max-width: 32ch;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
```

`assets/dashboard/src/components/AttachmentChips.tsx`:

```tsx
import styles from './AttachmentChips.module.css';
import type { ChatImage } from '../lib/chat/types';

interface AttachmentChipsProps {
  images: ChatImage[];
  files: { name: string; title?: string }[];
  attaching: boolean;
  error: string | null;
  onRemoveImage(index: number): void;
  onRemoveFile(index: number): void;
  /** Prefix for chip test ids, e.g. "chat" → chat-image-chip. */
  testIdPrefix: string;
  disabled?: boolean;
}

/** Image thumbnails, file-name chips, upload status, and upload errors. */
export default function AttachmentChips({
  images,
  files,
  attaching,
  error,
  onRemoveImage,
  onRemoveFile,
  testIdPrefix,
  disabled = false,
}: AttachmentChipsProps) {
  return (
    <>
      {(images.length > 0 || files.length > 0) && (
        <div className={styles.chips}>
          {images.map((img, i) => (
            <span className={styles.chip} key={i} data-testid={`${testIdPrefix}-image-chip`}>
              <img src={`data:${img.media_type};base64,${img.data}`} alt="attachment" />
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                aria-label="Remove image"
                disabled={disabled}
                onClick={() => onRemoveImage(i)}
              >
                ×
              </button>
            </span>
          ))}
          {files.map((file, i) => (
            <span
              className={styles.chip}
              key={`${i}-${file.title ?? file.name}`}
              data-testid={`${testIdPrefix}-file-chip`}
            >
              <span className={styles.fileName} title={file.title ?? file.name}>
                {file.name}
              </span>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                aria-label={`Remove ${file.name}`}
                disabled={disabled}
                onClick={() => onRemoveFile(i)}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}
      {attaching && (
        <span className="text-muted" role="status">
          Attaching…
        </span>
      )}
      {error && (
        <div className="error-banner" role="alert">
          {error}
        </div>
      )}
    </>
  );
}
```

- [ ] **Step 6: Move Composer onto the shared units**

In `Composer.tsx`:

- Delete `readFileAsImage`, the `images`/`files`/`attaching`/`attachmentError` state, and the `attachFiles` callback.
- Add:

```tsx
import AttachmentChips from '../AttachmentChips';
import { useAttachments } from '../../hooks/useAttachments';
import { withFileAttachments } from '../../lib/attachments';
```

and inside the component, after `value` state:

```tsx
const upload = useCallback(
  (file: File) => {
    if (!workspaceId) return Promise.reject(new Error('Workspace is unavailable'));
    return uploadWorkspaceAttachment(workspaceId, file);
  },
  [workspaceId]
);
const {
  images,
  files,
  attaching,
  error: attachmentError,
  attachFiles,
  removeImage,
  removeFile,
  clear: clearAttachments,
} = useAttachments<WorkspaceAttachment>({
  upload,
  initialImages: initialDraft?.images,
  initialFiles: initialDraft?.files,
  disabled,
});
```

- Replace `submit` with:

```tsx
const submit = () => {
  if (disabled || attaching) return;
  const text = withFileAttachments(
    value,
    files.map((file) => file.path)
  );
  if (text.trim() === '' && images.length === 0) return;
  onSend(text, images);
  setValue('');
  clearAttachments();
  textareaRef.current?.focus();
};
```

- Replace the chips, `Attaching…` span, and error banner JSX with:

```tsx
<AttachmentChips
  images={images}
  files={files.map((file) => ({ name: file.name, title: file.path }))}
  attaching={attaching}
  error={attachmentError}
  onRemoveImage={removeImage}
  onRemoveFile={removeFile}
  testIdPrefix="chat"
/>
```

- Leave the imperative handle, paste handler, Attach/Send buttons, and file input calling `attachFiles` as they are. Drop now-unused imports (`getErrorMessage`, `ChatImage` if unused).

- [ ] **Step 7: Run tests to verify they pass**

Run: `./test.sh --quick`
Expected: PASS — new tests pass; `Composer.test.tsx` and `ChatView.test.tsx` pass unchanged.

---

### Task 6: Shared file drop target; ChatView uses it

**Files:**

- Create: `assets/dashboard/src/hooks/useFileDrop.ts`
- Create: `assets/dashboard/src/components/FileDropOverlay.tsx`, `assets/dashboard/src/components/FileDropOverlay.module.css`
- Modify: `assets/dashboard/src/components/chat/ChatView.tsx:97-170`
- Modify: `assets/dashboard/src/components/chat/chat.module.css:21-44` (remove `.fileDropOverlay`, `.fileDropPrompt`)

**Interfaces:**

- Produces:
  - `useFileDrop({ available: boolean; onFiles(files: File[]): void }): { showOverlay: boolean; dragging: boolean; cancel(): void; handlers: { onDragEnter; onDragOver; onDragLeave; onDragEnd; onDrop } }`
  - `<FileDropOverlay testId={string} />`; `dropZoneClassName: string` (sets `position: relative` on the drop target).

- [ ] **Step 1: Implement the hook and overlay (behavior-preserving extraction)**

`assets/dashboard/src/hooks/useFileDrop.ts`:

```ts
import { useCallback, useRef, useState } from 'react';
import type React from 'react';

const hasFiles = (types: readonly string[]) => Array.from(types).includes('Files');

/**
 * File drag-and-drop for a region: counts nested enter/leave pairs so moving
 * across child elements never flickers the overlay, and leaves text drops
 * native.
 */
export function useFileDrop({
  available,
  onFiles,
}: {
  available: boolean;
  onFiles(files: File[]): void;
}) {
  const [dragging, setDragging] = useState(false);
  const depthRef = useRef(0);
  const cancel = useCallback(() => {
    depthRef.current = 0;
    setDragging(false);
  }, []);

  const handlers = {
    onDragEnter: (event: React.DragEvent) => {
      if (!hasFiles(event.dataTransfer.types)) return;
      event.preventDefault();
      depthRef.current += 1;
      setDragging(true);
    },
    onDragOver: (event: React.DragEvent) => {
      if (!hasFiles(event.dataTransfer.types)) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = available ? 'copy' : 'none';
    },
    onDragLeave: (event: React.DragEvent) => {
      if (!hasFiles(event.dataTransfer.types)) return;
      depthRef.current = Math.max(0, depthRef.current - 1);
      if (depthRef.current === 0) setDragging(false);
    },
    onDragEnd: cancel,
    onDrop: (event: React.DragEvent) => {
      if (!hasFiles(event.dataTransfer.types)) return;
      event.preventDefault();
      const files = Array.from(event.dataTransfer.files);
      cancel();
      if (available) onFiles(files);
    },
  };

  return { dragging, showOverlay: dragging && available, cancel, handlers };
}
```

`assets/dashboard/src/components/FileDropOverlay.module.css` — `.zone` plus the two rules moved verbatim from `chat.module.css`:

```css
.zone {
  position: relative;
}

.fileDropOverlay {
  position: absolute;
  inset: var(--spacing-sm);
  z-index: 20;
  border: 2px dashed var(--color-accent);
  border-radius: var(--radius-md);
  pointer-events: none;
}

.fileDropPrompt {
  position: absolute;
  top: 50%;
  left: 50%;
  width: max-content;
  max-width: calc(100% - var(--spacing-xl));
  transform: translate(-50%, -50%);
  padding: var(--spacing-sm) var(--spacing-lg);
  color: var(--color-accent);
  background: var(--color-surface-elevated);
  border: 1px solid var(--color-accent);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-md);
  text-align: center;
}
```

`assets/dashboard/src/components/FileDropOverlay.tsx`:

```tsx
import styles from './FileDropOverlay.module.css';

/** Positions a drop target so FileDropOverlay can frame it. */
export const dropZoneClassName = styles.zone;

/** Outline and prompt shown while files are dragged over a drop target. */
export default function FileDropOverlay({ testId }: { testId: string }) {
  return (
    <div className={styles.fileDropOverlay} data-testid={testId}>
      <div className={styles.fileDropPrompt} role="status">
        Drop files to attach
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Move ChatView onto them**

In `ChatView.tsx`:

- Delete `fileDragInside` state, `fileDragDepthRef`, `hasFiles`, and `clearFileDrag`.
- Add after `attachmentAvailable` state:

```tsx
const fileDrop = useFileDrop({
  available: attachmentAvailable,
  onFiles: (files) => localComposerRef.current?.attachFiles(files),
});
```

- Replace the five drag props on the root `<div>` with `{...fileDrop.handlers}`.
- In `onKeyDown`, replace `fileDragInside` with `fileDrop.dragging` and `clearFileDrag()` with `fileDrop.cancel()`.
- Replace the overlay JSX with `{fileDrop.showOverlay ? <FileDropOverlay testId="chat-file-drop-overlay" /> : null}`.
- `.chat` already has `position: relative`; do not add `dropZoneClassName` to ChatView.
- Import `useFileDrop` from `../../hooks/useFileDrop` and `FileDropOverlay` from `../FileDropOverlay`.

- [ ] **Step 3: Run tests**

Run: `./test.sh --quick`
Expected: PASS — the five ChatView drag/drop tests (`keeps one pane-wide drop target…`, `drops files over transcript content…`, `updates an active drag when Attach becomes unavailable`, `leaves text drops native`, `Escape cancels file-drag feedback…`) pass unchanged; they are this extraction's regression guard.

---

### Task 7: Spawn form attaches like chat

**Files:**

- Modify: `assets/dashboard/src/lib/api.ts` (after `uploadWorkspaceAttachment`, near line 531)
- Modify: `assets/dashboard/src/lib/types.ts` (manual `SpawnRequest`)
- Modify: `assets/dashboard/src/lib/spawn-draft.ts`
- Modify: `assets/dashboard/src/routes/SpawnPage.tsx`
- Modify: `assets/dashboard/src/styles/global.css` (after `.spawn-option input[type='checkbox']`, near line 2736)
- Create: `assets/dashboard/src/routes/SpawnPage.attachments.test.tsx`
- Modify: `docs/dashboard-ui.md:5,23,43,44,52,54`

**Interfaces:**

- Consumes: `useAttachments`, `AttachmentChips`, `useFileDrop`, `FileDropOverlay`, `dropZoneClassName` (Tasks 5–6); `SpawnAttachment`, `SpawnImage` generated types (Tasks 2–3); `POST /api/spawn-attachments`.
- Produces:
  - `uploadSpawnAttachment(file: File): Promise<SpawnAttachment>`
  - `SpawnDraft.images?: ChatImage[]`, `SpawnDraft.files?: SpawnAttachment[]` (replace `imageAttachments`)
  - `SpawnRequest.file_attachments?: string[]`
  - Test ids: `spawn-attach`, `spawn-file-input`, `spawn-image-chip`, `spawn-file-chip`, `spawn-file-drop-overlay`, `spawn-drop-zone`.

- [ ] **Step 1: Add the API client, types, and draft fields**

`lib/api.ts`, after `uploadWorkspaceAttachment`:

```ts
export async function uploadSpawnAttachment(file: File): Promise<SpawnAttachment> {
  const response = await apiFetch(
    `/api/spawn-attachments?filename=${encodeURIComponent(file.name)}`,
    {
      method: 'POST',
      headers: { ...csrfHeaders(), 'Content-Type': 'application/octet-stream' },
      body: file,
    }
  );
  if (!response.ok) await parseErrorResponse(response, 'Failed to upload file');
  return response.json();
}
```

(Import `SpawnAttachment` from `./types.generated` alongside `WorkspaceAttachment`.)

`lib/types.ts` manual `SpawnRequest`, after `images`:

```ts
  file_attachments?: string[]; // staging ids from POST /api/spawn-attachments
```

`lib/spawn-draft.ts`: replace `imageAttachments?: string[]; // base64-encoded PNGs` with

```ts
  images?: ChatImage[]; // inline images with media type
  files?: SpawnAttachment[]; // staged uploads, referenced by id
```

and add `import type { ChatImage } from './chat/types';` and `import type { SpawnAttachment } from './types.generated';`.

- [ ] **Step 2: Write the failing tests**

`assets/dashboard/src/routes/SpawnPage.attachments.test.tsx` — copy the mock block, `chatRunners`, `chatModels`, `renderSpawnPage`, `selectRepoAndBranch`, `selectAgent`, and `engage` verbatim from `SpawnPage.chat.test.tsx` (lines 1–163), then apply these changes to the copy:

```tsx
// In the '../lib/api' mock add:
const mockUploadSpawnAttachment = vi.fn<(file: File) => Promise<{ id: string; name: string }>>();
//   uploadSpawnAttachment: (file: File) => mockUploadSpawnAttachment(file),
// and make getErrorMessage return err.message for Error values:
//   getErrorMessage: (err: unknown, fallback: string) => (err instanceof Error ? err.message : fallback),

// Replace the ToastProvider mock so toasts are observable:
const mockToastError = vi.fn();
vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ show: vi.fn(), success: vi.fn(), error: mockToastError }),
}));

// Replace the RemoteHostSelector mock with one that can switch to remote:
vi.mock('../components/RemoteHostSelector', () => ({
  default: (props: { onChange: (v: unknown) => void }) => (
    <button
      type="button"
      data-testid="go-remote"
      onClick={() => props.onChange({ type: 'remote', profileId: 'p1', profile: {}, flavor: 'f1' })}
    >
      Remote
    </button>
  ),
}));
```

Then the tests:

```tsx
const csv = (name = 'users.csv') => new File(['id\n'], name, { type: 'text/csv' });
const jpeg = (name = 'photo.jpg') => new File(['j'], name, { type: 'image/jpeg' });

function filesTransfer(files: File[]) {
  return { types: ['Files'], files, dropEffect: 'none' };
}

describe('SpawnPage attachments', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    sessionStorage.clear();
    resetSpawnInflightForTests();
    const cfg = makeConfig({ runners: chatRunners(), models: chatModels() });
    configContextValue = cfg;
    workspacesContextValue = [];
    mockGetConfig.mockResolvedValue(cfg);
    mockGetPersonas.mockResolvedValue({ personas: [] });
    mockGetStyles.mockResolvedValue({ styles: [] });
    mockSpawnSessions.mockResolvedValue([{ session_id: 'sess-1', workspace_id: 'ws-1' }]);
    mockUploadSpawnAttachment.mockImplementation(async (f) => ({
      id: `id-${f.name}`,
      name: f.name,
    }));
  });

  it('sends picked files by staging id and images with their media type', async () => {
    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await userEvent.upload(screen.getByTestId('spawn-file-input'), [csv(), jpeg()]);
    expect(await screen.findByTestId('spawn-file-chip')).toHaveTextContent('users.csv');
    expect(await screen.findByTestId('spawn-image-chip')).toBeInTheDocument();

    const req = await engage();
    expect(req.file_attachments).toEqual(['id-users.csv']);
    expect(req.images).toEqual([{ media_type: 'image/jpeg', data: 'ag==' }]);
  });

  it('disables Spawn until the upload finishes', async () => {
    let finish!: (v: { id: string; name: string }) => void;
    mockUploadSpawnAttachment.mockImplementation(() => new Promise((r) => (finish = r)));
    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await userEvent.upload(screen.getByTestId('spawn-file-input'), csv());
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeDisabled());
    finish({ id: 'id-users.csv', name: 'users.csv' });
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeEnabled());
  });

  it('attaches pasted files and leaves plain-text paste alone', async () => {
    renderSpawnPage();
    await selectRepoAndBranch();
    const textPaste = new Event('paste', { bubbles: true, cancelable: true });
    Object.assign(textPaste, { clipboardData: { files: [], items: [] } });
    document.dispatchEvent(textPaste);
    expect(textPaste.defaultPrevented).toBe(false);

    const filePaste = new Event('paste', { bubbles: true, cancelable: true });
    Object.assign(filePaste, { clipboardData: { files: [csv('pasted.csv')], items: [] } });
    document.dispatchEvent(filePaste);
    expect(filePaste.defaultPrevented).toBe(true);
    expect(await screen.findByTestId('spawn-file-chip')).toHaveTextContent('pasted.csv');
  });

  it('attaches dropped files and shows the shared overlay while dragging', async () => {
    renderSpawnPage();
    await selectRepoAndBranch();
    const zone = screen.getByTestId('spawn-drop-zone');
    fireEvent.dragEnter(zone, { dataTransfer: filesTransfer([csv('dropped.csv')]) });
    expect(screen.getByTestId('spawn-file-drop-overlay')).toBeInTheDocument();
    fireEvent.drop(zone, { dataTransfer: filesTransfer([csv('dropped.csv')]) });
    expect(screen.queryByTestId('spawn-file-drop-overlay')).not.toBeInTheDocument();
    expect(await screen.findByTestId('spawn-file-chip')).toHaveTextContent('dropped.csv');
  });

  it('restores both chip kinds from the draft without uploading again', async () => {
    sessionStorage.setItem(
      'spawn-draft-fresh',
      JSON.stringify({
        prompt: 'go',
        targetCounts: {},
        modelSelectionMode: 'single',
        images: [{ media_type: 'image/png', data: 'AA==' }],
        files: [{ id: 'id-restored.csv', name: 'restored.csv' }],
      })
    );
    renderSpawnPage();
    expect(await screen.findByTestId('spawn-file-chip')).toHaveTextContent('restored.csv');
    expect(screen.getByTestId('spawn-image-chip')).toBeInTheDocument();
    expect(mockUploadSpawnAttachment).not.toHaveBeenCalled();
  });

  it('clears attachments after a successful spawn', async () => {
    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await userEvent.upload(screen.getByTestId('spawn-file-input'), csv());
    await screen.findByTestId('spawn-file-chip');
    await engage();
    await waitFor(() => expect(screen.queryByTestId('spawn-file-chip')).not.toBeInTheDocument());
  });

  it('keeps attachments when every target fails', async () => {
    mockSpawnSessions.mockResolvedValue([
      { error: 'attachment no longer available: id-users.csv' },
    ]);
    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await userEvent.upload(screen.getByTestId('spawn-file-input'), csv());
    await screen.findByTestId('spawn-file-chip');
    await engage();
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeEnabled());
    expect(screen.getByTestId('spawn-file-chip')).toHaveTextContent('users.csv');
  });

  it('blocks a remote spawn while attachments are present', async () => {
    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await userEvent.upload(screen.getByTestId('spawn-file-input'), csv());
    await screen.findByTestId('spawn-file-chip');
    fireEvent.click(screen.getByTestId('go-remote'));
    expect(screen.getByTestId('spawn-attach')).toBeDisabled();
    fireEvent.click(screen.getByTestId('spawn-submit'));
    expect(mockToastError).toHaveBeenCalledWith("Attachments aren't supported for remote spawns");
    expect(mockSpawnSessions).not.toHaveBeenCalled();
  });

  it('blocks /resume while attachments are present', async () => {
    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await userEvent.upload(screen.getByTestId('spawn-file-input'), csv());
    await screen.findByTestId('spawn-file-chip');
    fireEvent.click(screen.getByTestId('trigger-resume'));
    expect(mockToastError).toHaveBeenCalledWith('Remove attachments to run /resume');
    expect(mockSpawnSessions).not.toHaveBeenCalled();
  });

  it('caps images at five and names the rejected file', async () => {
    renderSpawnPage();
    await selectRepoAndBranch();
    const six = [1, 2, 3, 4, 5, 6].map((n) => jpeg(`${n}.jpg`));
    await userEvent.upload(screen.getByTestId('spawn-file-input'), six);
    expect(await screen.findByRole('alert')).toHaveTextContent('6.jpg: maximum 5 images');
    expect(screen.getAllByTestId('spawn-image-chip')).toHaveLength(5);
  });
});
```

`spawn-submit` click in the remote test: Spawn is still enabled (only uploads disable it), so `handleEngage` runs `validateForm`, which raises the toast.

- [ ] **Step 3: Run tests to verify they fail**

Run: `./test.sh --quick`
Expected: FAIL — `spawn-file-input` not found.

- [ ] **Step 4: Wire the hook into SpawnPage**

Imports:

```tsx
import {
  getConfig,
  getErrorMessage,
  getPersonas,
  getStyles,
  uploadSpawnAttachment,
} from '../lib/api';
import AttachmentChips from '../components/AttachmentChips';
import FileDropOverlay, { dropZoneClassName } from '../components/FileDropOverlay';
import { useAttachments } from '../hooks/useAttachments';
import { useFileDrop } from '../hooks/useFileDrop';
```

and add `SpawnAttachment` to the `../lib/types.generated` type import.

Delete `const [imageAttachments, setImageAttachments] = useState<string[]>([]);` (line 149). Right after `const formDisabled = inflight !== undefined;` (line 209) add:

```tsx
// Attachments: the same hook and chips as the chat composer. Remote spawns
// have no local workspace to receive files.
const isRemoteSpawn = environment.type === 'remote';
const attachmentsBlocked = formDisabled || isRemoteSpawn;
const attachments = useAttachments<SpawnAttachment>({
  upload: uploadSpawnAttachment,
  maxImages: 5,
  disabled: attachmentsBlocked,
});
const { attachFiles, restore: restoreAttachments, clear: clearAttachments } = attachments;
const hasAttachments = attachments.images.length > 0 || attachments.files.length > 0;
const fileInputRef = useRef<HTMLInputElement>(null);
const fileDrop = useFileDrop({
  available: !attachmentsBlocked && !attachments.attaching,
  onFiles: (files) => void attachFiles(files),
});
```

Draft restore (replace the `// imageAttachments: draft → default` block near line 412):

```tsx
// attachments: draft → default (applies to all modes)
if (draft?.images || draft?.files) {
  restoreAttachments(draft.images ?? [], draft.files ?? []);
}
```

and add `restoreAttachments` to that effect's dependency array.

Draft save (replace the `imageAttachments` lines near line 537):

```tsx
if (attachments.images.length > 0) draft.images = attachments.images;
if (attachments.files.length > 0) draft.files = attachments.files;
```

and in its dependency array replace `imageAttachments` with `attachments.images, attachments.files`.

`validateForm`: before `if (totalPromptableCount === 0)` add

```tsx
if (isRemote && hasAttachments) {
  toastError("Attachments aren't supported for remote spawns");
  return false;
}
```

and add `hasAttachments` to its dependency array.

`handleSlashCommandSelect`: after `if (formDisabled) return;` add

```tsx
// Slash-command spawns (/resume, command targets, /quick) carry no
// attachments; never start one while attachments would be dropped.
if (hasAttachments || attachments.attaching) {
  toastError(`Remove attachments to run ${command}`);
  return;
}
```

and add `hasAttachments, attachments.attaching` to its dependency array.

`handleEngage`: change the first line to `if (formDisabled || attachments.attaching) return;`. In the request replace the interim `images:` mapping from Task 3 with:

```tsx
      images: attachments.images.length > 0 ? attachments.images : undefined,
      file_attachments:
        attachments.files.length > 0 ? attachments.files.map((f) => f.id) : undefined,
```

replace `setImageAttachments([]);` in `onSuccess` with `clearAttachments();`, and in the dependency array replace `imageAttachments` with `attachments.images, attachments.files, attachments.attaching, clearAttachments`.

Paste: replace the whole `// Handle paste events for image attachments` effect with:

```tsx
// Paste files anywhere on the form, the way Attach does. Text-only pastes
// keep their native behavior.
useEffect(() => {
  const handlePaste = (e: ClipboardEvent) => {
    const files = e.clipboardData?.files;
    if (!files || files.length === 0 || attachmentsBlocked) return;
    e.preventDefault();
    void attachFiles(files);
  };
  document.addEventListener('paste', handlePaste);
  return () => document.removeEventListener('paste', handlePaste);
}, [attachmentsBlocked, attachFiles]);
```

- [ ] **Step 5: Render the controls**

Change `<div className="spawn-content" data-tour="spawn-form">` to:

```tsx
      <div
        className={`spawn-content ${dropZoneClassName}`}
        data-tour="spawn-form"
        data-testid="spawn-drop-zone"
        {...fileDrop.handlers}
      >
        {fileDrop.showOverlay ? <FileDropOverlay testId="spawn-file-drop-overlay" /> : null}
```

Replace the whole `{imageAttachments.length > 0 && ( … )}` block after `<PromptTextarea … />` with:

```tsx
<div className="spawn-attachments">
  <div className="spawn-attachments__chips">
    <AttachmentChips
      images={attachments.images}
      files={attachments.files}
      attaching={attachments.attaching}
      error={attachments.error}
      onRemoveImage={attachments.removeImage}
      onRemoveFile={attachments.removeFile}
      testIdPrefix="spawn"
      disabled={formDisabled}
    />
  </div>
  {isRemoteSpawn ? (
    <Tooltip content="Attachments aren't supported for remote spawns">
      <button
        type="button"
        className="btn btn--secondary btn--sm"
        disabled
        data-testid="spawn-attach"
      >
        Attach
      </button>
    </Tooltip>
  ) : (
    <button
      type="button"
      className="btn btn--secondary btn--sm"
      disabled={attachmentsBlocked || attachments.attaching}
      onClick={() => fileInputRef.current?.click()}
      data-testid="spawn-attach"
    >
      Attach
    </button>
  )}
  <input
    ref={fileInputRef}
    type="file"
    multiple
    hidden
    data-testid="spawn-file-input"
    onChange={(e) => {
      if (e.target.files) void attachFiles(e.target.files);
      e.target.value = '';
    }}
  />
</div>
```

Change the submit button's `disabled={formDisabled}` to `disabled={formDisabled || attachments.attaching}`.

`styles/global.css`, after `.spawn-option input[type='checkbox'] { … }`:

```css
/* Spawn page prompt attachments: chips beside the Attach control */
.spawn-attachments {
  display: flex;
  align-items: flex-start;
  gap: var(--spacing-sm);
  padding: var(--spacing-sm) var(--spacing-md);
  border-top: 1px solid var(--color-border);
}

.spawn-attachments__chips {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--spacing-xs);
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `./test.sh --quick`
Expected: PASS — the new file passes and every existing `SpawnPage.*.test.tsx` passes.

- [ ] **Step 7: Style check**

Invoke the `schmux-dashboard-style-check` skill on the spawn prompt card, `AttachmentChips`, and `FileDropOverlay`. Fix any finding it reports. Ask the user to look at the spawn form in light and dark themes through `./dev.sh` (their running process; do not start or restart it yourself).

- [ ] **Step 8: Update `docs/dashboard-ui.md`**

- Line 5: replace "image attachment support for spawn prompts" with "image and file attachments for spawn prompts (shared with the chat composer)".
- Line 23: replace "image paste handling" with "attachments (Attach, paste, drop) via `useAttachments`".
- Replace the bullets at lines 43, 44, 52, 54 with:

```markdown
- **Spawn attachments match the chat composer.** `useAttachments`, `AttachmentChips`, `useFileDrop`, and `FileDropOverlay` are shared by `Composer`/`ChatView` and `SpawnPage`. Images travel inline as `images: [{media_type, data}]`; other files upload immediately to `POST /api/spawn-attachments` and the request carries their staging ids in `file_attachments`. The daemon copies them into each spawned workspace and appends the chat composer's `File attachments:` block.
- **Attachments are never silently dropped.** Attach and drop are disabled for remote spawns; remote submissions, `/resume`, command targets, and `/quick` are refused with a toast while attachments are present. Spawn is disabled while an upload is in flight.
- **Max 5 images per spawn**, enforced in the form (with a `name: maximum 5 images` error) and by the daemon (400). The spawn endpoint keeps its 50MB body limit for inline images.
- **SpawnDraft persists `images` and staged `files` in sessionStorage**, keyed by workspace ID; restoring a draft restores both chip kinds without uploading again. The draft clears when at least one target starts, and the daemon deletes the staged files at the same moment.
```

---

### Task 8: Scenario regression

**Files:**

- Create: `test/scenarios/spawn-file-attachments.md`
- Create (generated): `test/scenarios/generated/spawn-file-attachments.spec.ts`

**Interfaces:**

- Consumes: the full feature (Tasks 1–7), real `POST /api/spawn-attachments`.

- [ ] **Step 1: Write the scenario**

`test/scenarios/spawn-file-attachments.md`:

```markdown
# Attach files when spawning

A user starting a fresh spawn attaches a CSV with the picker and a JPEG by
dropping it on the form, reloads the page, and submits. The spawn request
references the uploaded CSV and carries the JPEG with its real media type.

## Preconditions

- The isolated scenario daemon serves the real dashboard with one local git repository configured.
- The scenario image has no promptable agent, so `POST /api/spawn` is a controlled fixture that records the request and returns one successful session. `POST /api/spawn-attachments` is the real daemon endpoint.

## Verifications

- Attach opens a picker; choosing `users.csv` shows a `users.csv` chip.
- The upload reaches `POST /api/spawn-attachments` with the original filename and exact bytes, and the daemon answers 201 with an id.
- Dragging files over the spawn form shows the "Drop files to attach" outline; dropping `photo.jpg` shows an image thumbnail chip.
- Reloading the page restores both chips without uploading the CSV again.
- Submitting sends `file_attachments` equal to the id the daemon returned and `images` with one entry whose `media_type` is `image/jpeg`.
- After the successful spawn, both chips are gone.
```

- [ ] **Step 2: Generate the Playwright test**

Invoke the `generate-scenario-tests` skill for `test/scenarios/spawn-file-attachments.md`. Review the generated spec against `docs/testing.md` rules 1, 5, and 7: waits are locator assertions or `page.waitForRequest`, never timeouts.

- [ ] **Step 3: Run it**

Run: `./test.sh --scenarios --run "spawn-file-attachments"`
Expected: PASS. On failure, report the Playwright output and the artifact path under `test/scenarios/artifacts/` (rule 12); do not add retries or waits.

---

### Task 9: Completion gates

**Files:** none new.

- [ ] **Step 1: Format and full suite**

Run: `./format.sh && ./test.sh`
Expected: all suites PASS. Paste the summary. A skipped suite is missing evidence (rule 11), not a pass.

- [ ] **Step 2: Static analysis**

Run: `./badcode.sh`
Expected: PASS (deadcode will flag anything left behind, such as an unused `writeImageAttachments`).

- [ ] **Step 3: Test rubric review**

Invoke the `test-rules-review` skill over the changed tests. A violations verdict blocks completion; fix and re-run.

- [ ] **Step 4: API docs check**

Run: `scripts/check-api-docs.sh`
Expected: PASS.

- [ ] **Step 5: Hand back**

Report to the user with evidence from Steps 1–4. Ask whether to run `/finalize` to fold the spec into the subsystem guides and delete this plan and the spec (the user's definition of done requires both deleted). The user commits with `/commit`.
