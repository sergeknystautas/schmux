package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/timelapse"
)

func (s *Server) handleTimelapseList(w http.ResponseWriter, r *http.Request) {
	dir := s.recordingsDir()
	recordings, err := timelapse.ListRecordings(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(recordings)
}

func (s *Server) handleTimelapseExport(w http.ResponseWriter, r *http.Request) {
	recordingID := chi.URLParam(r, "recordingId")
	dir := s.recordingsDir()
	recordingPath := filepath.Join(dir, recordingID+".jsonl")
	outputPath := filepath.Join(dir, recordingID+".cast")

	if _, err := os.Stat(recordingPath); os.IsNotExist(err) {
		http.Error(w, "recording not found", http.StatusNotFound)
		return
	}

	// Check for cached export
	if castInfo, err := os.Stat(outputPath); err == nil {
		if recInfo, err := os.Stat(recordingPath); err == nil {
			if castInfo.ModTime().After(recInfo.ModTime()) {
				// Cached export is newer — return immediately
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]string{
					"exportId":    recordingID,
					"recordingId": recordingID,
					"status":      "cached",
				})
				return
			}
		}
	}

	// Run export synchronously — typically completes in seconds
	exp := timelapse.NewExporter(recordingPath, outputPath, nil)
	if err := exp.Export(); err != nil {
		s.logger.Error("timelapse export failed", "recording", recordingID, "err", err)
		http.Error(w, "export failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"exportId":    recordingID,
		"recordingId": recordingID,
		"status":      "complete",
	})
}

func (s *Server) handleTimelapseDownload(w http.ResponseWriter, r *http.Request) {
	recordingID := chi.URLParam(r, "recordingId")
	dir := s.recordingsDir()
	castPath := filepath.Join(dir, recordingID+".cast")

	if _, err := os.Stat(castPath); os.IsNotExist(err) {
		http.Error(w, "export not found — run export first", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+recordingID+".cast")
	http.ServeFile(w, r, castPath)
}

func (s *Server) handleTimelapseDelete(w http.ResponseWriter, r *http.Request) {
	recordingID := chi.URLParam(r, "recordingId")
	dir := s.recordingsDir()

	jsonlPath := filepath.Join(dir, recordingID+".jsonl")
	castPath := filepath.Join(dir, recordingID+".cast")

	if _, err := os.Stat(jsonlPath); os.IsNotExist(err) {
		http.Error(w, "recording not found", http.StatusNotFound)
		return
	}

	os.Remove(jsonlPath)
	os.Remove(castPath)

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) recordingsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".schmux", "recordings")
}
