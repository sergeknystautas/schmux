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
