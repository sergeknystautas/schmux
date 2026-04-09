package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/timelapse"
)

func (s *Server) handleTimelapseList(w http.ResponseWriter, r *http.Request) {
	dir := s.recordingsDir()
	recordings, err := timelapse.ListRecordings(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if recordings == nil {
		recordings = []timelapse.RecordingInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(recordings)
}

func (s *Server) handleTimelapseExport(w http.ResponseWriter, r *http.Request) {
	recordingID := chi.URLParam(r, "recordingId")
	dir := s.recordingsDir()
	recordingPath := filepath.Join(dir, recordingID+".cast")
	compressedPath := filepath.Join(dir, recordingID+".timelapse.cast")

	if _, err := os.Stat(recordingPath); os.IsNotExist(err) {
		http.Error(w, "recording not found", http.StatusNotFound)
		return
	}

	// Check for cached compressed version
	if compInfo, err := os.Stat(compressedPath); err == nil {
		if recInfo, err := os.Stat(recordingPath); err == nil {
			if compInfo.ModTime().After(recInfo.ModTime()) {
				// Cached compressed version is newer — return immediately
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

	// Run compression synchronously — typically completes in seconds
	exp := timelapse.NewExporter(recordingPath, compressedPath, nil)
	if err := exp.Export(); err != nil {
		s.logger.Error("timelapse compression failed", "recording", recordingID, "err", err)
		http.Error(w, "compression failed: "+err.Error(), http.StatusInternalServerError)
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

	// ?type=timelapse downloads the compressed version
	dlType := r.URL.Query().Get("type")
	var castPath, filename string
	if dlType == "timelapse" {
		castPath = filepath.Join(dir, recordingID+".timelapse.cast")
		filename = recordingID + ".timelapse.cast"
	} else {
		castPath = filepath.Join(dir, recordingID+".cast")
		filename = recordingID + ".cast"
	}

	if _, err := os.Stat(castPath); os.IsNotExist(err) {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	http.ServeFile(w, r, castPath)
}

func (s *Server) handleTimelapseDelete(w http.ResponseWriter, r *http.Request) {
	recordingID := chi.URLParam(r, "recordingId")
	dir := s.recordingsDir()

	castPath := filepath.Join(dir, recordingID+".cast")
	compressedPath := filepath.Join(dir, recordingID+".timelapse.cast")

	if _, err := os.Stat(castPath); os.IsNotExist(err) {
		http.Error(w, "recording not found", http.StatusNotFound)
		return
	}

	os.Remove(castPath)
	os.Remove(compressedPath)

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) recordingsDir() string {
	return filepath.Join(schmuxdir.Get(), "recordings")
}
