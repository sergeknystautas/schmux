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

	// Write image to temp file
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

	// Write image to system clipboard
	if err := setClipboardImageFunc(tmpPath); err != nil {
		logger.Error("failed to set clipboard", "err", err)
		writeJSONError(w, "failed to set clipboard: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Send Ctrl+V to the session
	sess, ok := s.state.GetSession(req.SessionID)
	if !ok {
		writeJSONError(w, "session not found", http.StatusNotFound)
		return
	}

	if sess.IsRemoteSession() {
		rm := s.session.GetRemoteManager()
		if rm == nil {
			writeJSONError(w, "remote manager not configured", http.StatusInternalServerError)
			return
		}
		conn := rm.GetConnection(sess.RemoteHostID)
		if conn == nil || !conn.IsConnected() {
			writeJSONError(w, "remote host not connected", http.StatusServiceUnavailable)
			return
		}
		if err := conn.SendKeys(r.Context(), sess.RemotePaneID, "\x16"); err != nil {
			logger.Error("failed to send ctrl+v to remote session", "err", err)
			writeJSONError(w, "failed to send input", http.StatusInternalServerError)
			return
		}
	} else {
		tracker, err := s.session.GetTracker(req.SessionID)
		if err != nil {
			writeJSONError(w, "session tracker not found", http.StatusNotFound)
			return
		}
		if err := tracker.SendInput("\x16"); err != nil {
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
