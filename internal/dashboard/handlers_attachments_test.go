package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func attachmentRequest(name string, body io.Reader) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/workspaces/ws-1/attachments?filename="+url.QueryEscape(name), body)
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("workspaceID", "ws-1")
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, ctx))
}

func TestWorkspaceAttachmentPreservesFiles(t *testing.T) {
	for _, vcs := range []string{"git", "sapling"} {
		t.Run(vcs, func(t *testing.T) {
			s, _, st := newTestServer(t)
			ws := state.Workspace{ID: "ws-1", Path: t.TempDir(), VCS: vcs}
			if err := st.AddWorkspace(ws); err != nil {
				t.Fatal(err)
			}
			h := newTestWorkspaceHandlers(s)
			var previous string
			for _, contents := range [][]byte{{0, 255, 1, 13, 10}, []byte("second file")} {
				w := httptest.NewRecorder()
				h.handleWorkspaceAttachment(w, attachmentRequest("résumé data.bin", bytes.NewReader(contents)))
				if w.Code != http.StatusCreated {
					t.Fatalf("upload: %d %s", w.Code, w.Body.String())
				}
				var got struct{ Name, Path string }
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got.Name != "résumé data.bin" || filepath.Base(got.Path) != got.Name {
					t.Fatalf("filename changed: %+v", got)
				}
				wantDir := filepath.Join(ws.Path, ".schmux", "attachments")
				if vcs == "sapling" {
					wantDir = filepath.Join(ws.Path, ".sl", "schmux", "attachments")
				}
				if !strings.HasPrefix(got.Path, wantDir+string(filepath.Separator)) || got.Path == previous {
					t.Fatalf("unexpected attachment path: %q", got.Path)
				}
				data, err := os.ReadFile(got.Path)
				if err != nil || !bytes.Equal(data, contents) {
					t.Fatalf("saved bytes = %v, err = %v; want %v", data, err, contents)
				}
				info, err := os.Stat(got.Path)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0o600 {
					t.Fatalf("file mode = %o", info.Mode().Perm())
				}
				if previous != "" {
					original, err := os.ReadFile(previous)
					if err != nil || !bytes.Equal(original, []byte{0, 255, 1, 13, 10}) {
						t.Fatalf("earlier upload changed: %v, %v", original, err)
					}
				}
				previous = got.Path
			}
		})
	}
}

func TestWorkspaceAttachmentRejectsInvalidNames(t *testing.T) {
	s, _, st := newTestServer(t)
	ws := state.Workspace{ID: "ws-1", Path: t.TempDir()}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", ".", "..", "../escape", "/tmp/escape", `dir\escape`, "line\nbreak", "null\x00byte"} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			newTestWorkspaceHandlers(s).handleWorkspaceAttachment(w, attachmentRequest(name, strings.NewReader("data")))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
		})
	}
	entries, err := os.ReadDir(ws.Path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid request wrote files: %v, %v", entries, err)
	}
}

func TestWorkspaceAttachmentRejectsEscapingSymlink(t *testing.T) {
	s, _, st := newTestServer(t)
	ws := state.Workspace{ID: "ws-1", Path: t.TempDir()}
	outside := t.TempDir()
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(ws.Path, ".schmux")); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	newTestWorkspaceHandlers(s).handleWorkspaceAttachment(w, attachmentRequest("data.csv", strings.NewReader("data")))
	if w.Code < 400 {
		t.Fatalf("escaping symlink accepted: %d %s", w.Code, w.Body.String())
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("wrote outside workspace: %v, %v", entries, err)
	}
}

type interruptedAttachment struct{}

func (interruptedAttachment) Read([]byte) (int, error) {
	return 0, errors.New("connection interrupted")
}

func TestWorkspaceAttachmentCleansFailedTransfers(t *testing.T) {
	for _, tc := range []struct {
		name string
		body io.Reader
		code int
	}{
		{"interrupted", io.MultiReader(strings.NewReader("partial"), interruptedAttachment{}), http.StatusBadRequest},
		{"oversized", io.LimitReader(&zeroAttachmentReader{}, 50*1024*1024+1), http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, st := newTestServer(t)
			ws := state.Workspace{ID: "ws-1", Path: t.TempDir()}
			if err := st.AddWorkspace(ws); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			newTestWorkspaceHandlers(s).handleWorkspaceAttachment(w, attachmentRequest("data.bin", tc.body))
			if w.Code != tc.code {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			entries, err := os.ReadDir(filepath.Join(ws.Path, ".schmux", "attachments"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("partial upload remains: %v, %v", entries, err)
			}
		})
	}
}

type zeroAttachmentReader struct{}

func (*zeroAttachmentReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestWorkspaceAttachmentUnavailableWorkspace(t *testing.T) {
	for _, tc := range []struct {
		name, remote, status string
		missing, locked      bool
		code                 int
	}{
		{name: "missing", missing: true, code: http.StatusNotFound},
		{name: "remote", remote: "host-1", code: http.StatusBadRequest},
		{name: "disposing", status: state.WorkspaceStatusDisposing, code: http.StatusConflict},
		{name: "locked", locked: true, code: http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, st := newTestServer(t)
			ws := state.Workspace{ID: "ws-1", Path: t.TempDir(), RemoteHostID: tc.remote, Status: tc.status}
			if !tc.missing {
				if err := st.AddWorkspace(ws); err != nil {
					t.Fatal(err)
				}
			}
			if tc.locked {
				manager := s.workspace.(*workspace.Manager)
				if !manager.LockWorkspace(ws.ID) {
					t.Fatal("could not lock workspace")
				}
				defer manager.UnlockWorkspace(ws.ID)
			}
			w := httptest.NewRecorder()
			newTestWorkspaceHandlers(s).handleWorkspaceAttachment(w, attachmentRequest("data.csv", strings.NewReader("data")))
			if w.Code != tc.code {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			entries, err := os.ReadDir(ws.Path)
			if err != nil || len(entries) != 0 {
				t.Fatalf("rejected request wrote files: %v, %v", entries, err)
			}
		})
	}
}
