package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

// handleDiagnosticAppend receives frontend diagnostic files and appends them
// to an existing diagnostic directory created by the WebSocket diagnostic handler.
func (s *Server) handleDiagnosticAppend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DiagDir            string `json:"diagDir"`
		XtermScreen        string `json:"xtermScreen"`
		ScreenDiff         string `json:"screenDiff"`
		RingBufferFrontend string `json:"ringBufferFrontend"`
		GapStats           string `json:"gapStats"`
		CursorXterm        string `json:"cursorXterm"`
		ScrollEvents       string `json:"scrollEvents"`
		ScrollStats        string `json:"scrollStats"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.DiagDir == "" {
		http.Error(w, "diagDir is required", http.StatusBadRequest)
		return
	}
	// Write the frontend files to the diagnostic directory
	os.WriteFile(filepath.Join(req.DiagDir, "screen-xterm.txt"), []byte(req.XtermScreen), 0o644)
	os.WriteFile(filepath.Join(req.DiagDir, "screen-diff.txt"), []byte(req.ScreenDiff), 0o644)
	os.WriteFile(filepath.Join(req.DiagDir, "ringbuffer-frontend.txt"), []byte(req.RingBufferFrontend), 0o644)
	if req.GapStats != "" {
		os.WriteFile(filepath.Join(req.DiagDir, "gap-stats.json"), []byte(req.GapStats), 0o644)
	}
	if req.CursorXterm != "" {
		os.WriteFile(filepath.Join(req.DiagDir, "cursor-xterm.json"), []byte(req.CursorXterm), 0o644)
	}
	if req.ScrollEvents != "" {
		os.WriteFile(filepath.Join(req.DiagDir, "scroll-events.json"), []byte(req.ScrollEvents), 0o644)
	}
	if req.ScrollStats != "" {
		os.WriteFile(filepath.Join(req.DiagDir, "scroll-stats.json"), []byte(req.ScrollStats), 0o644)
	}
	w.WriteHeader(http.StatusOK)
}
