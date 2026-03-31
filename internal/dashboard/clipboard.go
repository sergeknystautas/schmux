package dashboard

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/remote"
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

	logger.Info("clipboard paste request", "session", req.SessionID[:min(len(req.SessionID), 8)], "remote", sess.IsRemoteSession(), "size", len(imageData))

	if sess.IsRemoteSession() {
		rm := s.session.GetRemoteManager()
		if rm == nil {
			writeJSONError(w, "remote manager not configured", http.StatusInternalServerError)
			return
		}
		conn := rm.GetConnection(sess.RemoteHostID)
		if conn == nil || !conn.IsConnected() {
			logger.Error("remote host not connected", "host_id", sess.RemoteHostID)
			writeJSONError(w, "remote host not connected", http.StatusServiceUnavailable)
			return
		}
		logger.Info("starting remote clipboard paste", "host_id", sess.RemoteHostID, "pane", sess.RemotePaneID)
		if err := remoteClipboardPaste(r.Context(), conn, sess.RemotePaneID, imageData, logger); err != nil {
			logger.Error("remote clipboard paste failed", "err", err)
			writeJSONError(w, "failed to paste image on remote host: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		// Local session: write image to local system clipboard, then send Ctrl+V
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

// remoteClipboardPaste transfers an image to a remote host, sets the remote
// X11 clipboard via xclip, and sends Ctrl+V to the target pane.
// Requires xclip and Xvfb (or another X server) on the remote host.
func remoteClipboardPaste(ctx context.Context, conn *remote.Connection, paneID string, imageData []byte, logger *log.Logger) error {
	// Max 2MB for remote transfer — base64 goes through tmux send-keys
	const maxRemoteImageSize = 2 * 1024 * 1024
	if len(imageData) > maxRemoteImageSize {
		return fmt.Errorf("image too large for remote paste (%d bytes, max %d)", len(imageData), maxRemoteImageSize)
	}

	b64 := base64.StdEncoding.EncodeToString(imageData)
	tmpName := fmt.Sprintf("schmux-clipboard-%s.png", uuid.New().String()[:8])
	tmpPath := "/tmp/" + tmpName

	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Ensure DISPLAY is set in the tmux global environment so all panes
	// (including the agent receiving Ctrl+V) can access the X11 clipboard.
	// This is idempotent — safe to call on every paste.
	client := conn.Client()
	if client != nil {
		if _, err := client.Execute(cmdCtx, "setenv -g DISPLAY :99"); err != nil {
			logger.Warn("failed to set DISPLAY in tmux environment", "err", err)
			// Non-fatal: the agent's shell may already have DISPLAY set
		}
	}

	// Transfer image to remote host via base64
	writeCmd := fmt.Sprintf("printf '%%s' '%s' | base64 -d > %s", b64, tmpPath)
	if _, err := conn.RunCommand(cmdCtx, "/tmp", writeCmd); err != nil {
		return fmt.Errorf("failed to transfer image to remote host: %w", err)
	}

	logger.Info("image transferred to remote host", "path", tmpPath, "size", len(imageData))

	// Set remote X11 clipboard via xclip
	// DISPLAY=:99 is the conventional Xvfb display started during provisioning
	clipCmd := fmt.Sprintf("DISPLAY=:99 xclip -selection clipboard -t image/png -i %s && rm -f %s", tmpPath, tmpPath)
	if _, err := conn.RunCommand(cmdCtx, "/tmp", clipCmd); err != nil {
		// Clean up the temp file on failure
		conn.RunCommand(cmdCtx, "/tmp", "rm -f "+tmpPath)
		return fmt.Errorf("failed to set remote clipboard (is xclip + Xvfb installed?): %w", err)
	}

	// Send Ctrl+V to the remote pane so the agent reads from clipboard
	if err := conn.SendKeys(cmdCtx, paneID, "\x16"); err != nil {
		return fmt.Errorf("failed to send Ctrl+V to remote pane: %w", err)
	}

	return nil
}
