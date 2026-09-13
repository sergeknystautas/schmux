package chat

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

// Paths are the files of one chat session, all inside its own directory
// (schmuxdir.ChatSessionDir). The bridge files are transport only; nothing
// reads them for display.
type Paths struct {
	Dir          string
	Input        string // in.jsonl — appended by the daemon, tailed into claude's stdin
	Output       string // out.jsonl — claude's stdout, tailed by the daemon
	Errors       string // err.txt
	TailPID      string // tail.pid — so the pane can kill tail when claude exits
	Conversation string // conversation.jsonl — the conversation record
}

// PathsFor returns the file paths inside a session directory.
func PathsFor(sessionDir string) Paths {
	return Paths{
		Dir:          sessionDir,
		Input:        filepath.Join(sessionDir, "in.jsonl"),
		Output:       filepath.Join(sessionDir, "out.jsonl"),
		Errors:       filepath.Join(sessionDir, "err.txt"),
		TailPID:      filepath.Join(sessionDir, "tail.pid"),
		Conversation: ConversationPath(sessionDir),
	}
}

// Ensure creates the directory and empty bridge files without truncating
// existing ones.
func (p Paths) Ensure() error {
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return fmt.Errorf("chat: create bridge dir: %w", err)
	}
	for _, f := range []string{p.Input, p.Output, p.Errors} {
		fh, err := os.OpenFile(f, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("chat: create %s: %w", filepath.Base(f), err)
		}
		fh.Close()
	}
	return nil
}

// PipelineCommand wraps the claude command in the file bridge. tail runs as a
// background job whose pid is recorded, so when claude exits the shell kills
// tail and the pane closes; a plain `tail | claude` would leave the pane
// alive until the next input write (see the spec's bridge section).
func PipelineCommand(claudeCommand string, p Paths) string {
	return fmt.Sprintf("{ tail -n +1 -f %s & echo $! > %s; } | %s >> %s 2>> %s; kill $(cat %s) 2>/dev/null",
		shellutil.QuoteIfNeeded(p.Input), shellutil.QuoteIfNeeded(p.TailPID), claudeCommand,
		shellutil.QuoteIfNeeded(p.Output), shellutil.QuoteIfNeeded(p.Errors), shellutil.QuoteIfNeeded(p.TailPID))
}

// attachmentExt maps a media type to a file extension; unknown types get png,
// the dominant paste format.
func attachmentExt(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return "jpg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	default:
		return "png"
	}
}

// PersistAttachment writes an image's decoded bytes into dir — /tmp in
// production, the same place the terminal clipboard flow writes
// (fencedClipboardPaste) — as schmux-chat-<uuid8>.<ext>, mode 0600, and
// returns the file path.
func PersistAttachment(dir string, img Image) (string, error) {
	data, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil {
		return "", fmt.Errorf("chat: decode image: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("chat: create attachment dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("schmux-chat-%s.%s", uuid.New().String()[:8], attachmentExt(img.MediaType)))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("chat: write attachment: %w", err)
	}
	return path, nil
}

// AppendInput appends one stream-json line to the input file.
func AppendInput(p Paths, line []byte) error {
	f, err := os.OpenFile(p.Input, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(append([]byte{}, line...), '\n'))
	return err
}
