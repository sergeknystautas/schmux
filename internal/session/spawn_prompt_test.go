package session

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/state"
)

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

func TestAppendImagePathsToPrompt(t *testing.T) {
	prompt := "Build a login page"
	paths := []string{"/ws/.schmux/attachments/img-abc.png", "/ws/.schmux/attachments/img-def.png"}
	result := appendImagePathsToPrompt(prompt, paths)

	if !strings.HasPrefix(result, "Build a login page") {
		t.Error("original prompt should be preserved")
	}
	if !strings.Contains(result, "Image #1: /ws/.schmux/attachments/img-abc.png") {
		t.Error("missing image #1")
	}
	if !strings.Contains(result, "Image #2: /ws/.schmux/attachments/img-def.png") {
		t.Error("missing image #2")
	}
}

func TestAppendImagePathsToPrompt_Empty(t *testing.T) {
	prompt := "Build a login page"
	result := appendImagePathsToPrompt(prompt, nil)
	if result != prompt {
		t.Errorf("expected unmodified prompt, got %q", result)
	}
}

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
