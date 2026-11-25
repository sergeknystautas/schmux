package dashboard

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/logging"
)

// clipboardPasteRequest is the JSON body for POST /api/clipboard-paste.
type clipboardPasteRequest struct {
	SessionID string `json:"sessionId"`
	ImageB64  string `json:"imageBase64"` // base64-encoded PNG
}

// handleClipboardPaste receives an image from the browser, writes it to the
// system clipboard, and sends Ctrl+V (0x16) to the tmux session so the
// terminal app picks up the image from the clipboard.
func (s *Server) handleClipboardPaste(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Limit request body to 10MB to prevent unbounded memory allocation.
	// Clipboard images are typically small; 10MB is generous.
	const maxClipboardBodySize = 10 * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxClipboardBodySize)

	var req clipboardPasteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.SessionID == "" || req.ImageB64 == "" {
		writeJSONError(w, "sessionId and imageBase64 are required", http.StatusBadRequest)
		return
	}

	// Decode image
	imageData, err := base64.StdEncoding.DecodeString(req.ImageB64)
	if err != nil {
		writeJSONError(w, "invalid base64 image data", http.StatusBadRequest)
		return
	}

	logger := logging.Sub(s.logger, "clipboard")

	// Send Ctrl+V to the session
	sess, ok := s.state.GetSession(req.SessionID)
	if !ok {
		writeJSONError(w, "session not found", http.StatusNotFound)
		return
	}

	logger.Info("clipboard paste request", "session", req.SessionID[:min(len(req.SessionID), 8)], "size", len(imageData))

	if sess.Fence {
		// Fenced session: the agent inside the sandbox can't read the
		// macOS clipboard (fence denies mach-lookup com.apple.pasteboard.1), so
		// the clipboard+Ctrl+V route can't work. Write the image to a
		// fence-readable temp file and type its path into the agent instead.
		// Claude Code reads image files from a path.
		result, err := s.fencedClipboardPaste(req.SessionID, imageData, logger)
		if err != nil {
			logger.Error("fenced clipboard paste failed", "err", err)
			writeJSONError(w, "failed to paste image: "+err.Error(), http.StatusInternalServerError)
			return
		}
		logger.Info("clipboard image pasted", "session", req.SessionID[:min(len(req.SessionID), 8)], "size", len(imageData), "method", result.Method)
		writeJSON(w, map[string]string{
			"status":    "ok",
			"method":    result.Method,
			"file_path": result.FilePath,
		})
		return
	} else {
		// Unfenced session: write image to local system clipboard, then send Ctrl+V
		tmpFile, err := os.CreateTemp("", "schmux-clipboard-*.png")
		if err != nil {
			logger.Error("failed to create temp file", "err", err)
			writeJSONError(w, "failed to process image", http.StatusInternalServerError)
			return
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)

		if _, err := tmpFile.Write(imageData); err != nil {
			tmpFile.Close()
			logger.Error("failed to write temp file", "err", err)
			writeJSONError(w, "failed to process image", http.StatusInternalServerError)
			return
		}
		tmpFile.Close()

		if err := setClipboardImageFunc(tmpPath); err != nil {
			logger.Error("failed to set clipboard", "err", err)
			writeJSONError(w, "failed to set clipboard: "+err.Error(), http.StatusInternalServerError)
			return
		}

		tracker, err := s.session.GetTracker(req.SessionID)
		if err != nil {
			writeJSONError(w, "session tracker not found", http.StatusNotFound)
			return
		}
		if _, err := tracker.SendInput("\x16"); err != nil {
			logger.Error("failed to send ctrl+v to session", "err", err)
			writeJSONError(w, "failed to send input", http.StatusInternalServerError)
			return
		}
	}

	logger.Info("clipboard image pasted", "session", req.SessionID[:8], "size", len(imageData))
	writeJSON(w, map[string]string{"status": "ok"})
}

// setClipboardImageFunc is the function used to write images to the system
// clipboard. It's a variable so tests can replace it.
var setClipboardImageFunc = setClipboardImage

// setClipboardImage writes a PNG image to the macOS system clipboard.
func setClipboardImage(pngPath string) error {
	absPath, err := filepath.Abs(pngPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Use osascript to set clipboard to image data
	script := fmt.Sprintf(`set the clipboard to (read (POSIX file %q) as «class PNGf»)`, absPath)
	cmd := exec.Command("osascript", "-e", script)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("osascript failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// fencedClipboardPaste writes the image to a fence-readable temp file and types
// its path into the agent's input. Fenced sessions deny clipboard access, so the
// clipboard+Ctrl+V route used for unfenced local sessions can't work. /tmp is
// readable inside the fence and not redirected, so the agent can open the file.
// Typing is best-effort: the file is written and its path returned even if the
// keystroke send fails, so the path is never lost.
func (s *Server) fencedClipboardPaste(sessionID string, imageData []byte, logger *log.Logger) (*clipboardPasteResult, error) {
	tmpPath := filepath.Join("/tmp", fmt.Sprintf("schmux-clipboard-%s.png", uuid.New().String()[:8]))
	if err := os.WriteFile(tmpPath, imageData, 0o600); err != nil {
		return nil, fmt.Errorf("writing image file: %w", err)
	}
	tracker, err := s.session.GetTracker(sessionID)
	if err != nil {
		logger.Warn("fenced clipboard paste: tracker unavailable, returning file path only", "err", err)
		return &clipboardPasteResult{Method: "file", FilePath: tmpPath}, nil
	}
	if _, err := tracker.SendInput("Image: " + tmpPath); err != nil {
		logger.Warn("fenced clipboard paste: failed to type file path", "err", err)
	}
	logger.Info("fenced clipboard paste via file path", "path", tmpPath)
	return &clipboardPasteResult{Method: "file", FilePath: tmpPath}, nil
}

// clipboardPasteResult contains the result of a clipboard paste that used the
// file fallback — fenced sessions have no pasteboard access.
type clipboardPasteResult struct {
	// FilePath is the path to the written image file (set when file fallback is used).
	FilePath string `json:"file_path,omitempty"`
	// Method is "clipboard" or "file" depending on which approach succeeded.
	Method string `json:"method"`
}
