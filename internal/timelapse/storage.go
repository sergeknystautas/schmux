//go:build !notimelapse

package timelapse

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RecordingInfo holds metadata for a single recording file.
type RecordingInfo struct {
	RecordingID   string
	SessionID     string
	StartTime     time.Time
	ModTime       time.Time
	Duration      float64
	FileSize      int64
	Width         int
	Height        int
	InProgress    bool
	HasCompressed bool
	Path          string
}

// ListRecordings returns metadata for all recordings in dir.
// It looks for .cast files that are NOT compressed (no .timelapse.cast suffix).
func ListRecordings(dir string) ([]RecordingInfo, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.cast"))
	if err != nil {
		return nil, err
	}

	var result []RecordingInfo
	for _, path := range matches {
		// Skip compressed timelapse files
		if strings.HasSuffix(path, ".timelapse.cast") {
			continue
		}
		info, err := parseRecordingInfo(path)
		if err != nil {
			continue // skip malformed files
		}
		result = append(result, info)
	}

	// Sort by start time (from cast header), newest first; fall back to ModTime
	sort.Slice(result, func(i, j int) bool {
		ti, tj := result[i].StartTime, result[j].StartTime
		if ti.IsZero() || tj.IsZero() {
			return result[i].ModTime.After(result[j].ModTime)
		}
		return ti.After(tj)
	})

	return result, nil
}

func parseRecordingInfo(path string) (RecordingInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return RecordingInfo{}, err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return RecordingInfo{}, err
	}

	recordingID := strings.TrimSuffix(filepath.Base(path), ".cast")
	compressedPath := strings.TrimSuffix(path, ".cast") + ".timelapse.cast"
	_, compressedErr := os.Stat(compressedPath)

	// Derive SessionID from recording ID.
	// New format: recordingID == sessionID (e.g. "schmux-002-abc12345")
	// Legacy format: "<sessionID>-<unixTimestamp>" — strip the numeric suffix.
	sessionID := recordingID
	if lastDash := strings.LastIndex(recordingID, "-"); lastDash > 0 {
		suffix := recordingID[lastDash+1:]
		if _, err := strconv.ParseInt(suffix, 10, 64); err == nil && len(suffix) >= 10 {
			// Looks like a unix timestamp — strip it to recover the session ID.
			sessionID = recordingID[:lastDash]
		}
	}

	info := RecordingInfo{
		RecordingID:   recordingID,
		SessionID:     sessionID,
		ModTime:       stat.ModTime(),
		FileSize:      stat.Size(),
		Path:          path,
		InProgress:    true, // assumed until we see events with timestamps
		HasCompressed: compressedErr == nil,
	}

	// Read the asciicast v2 header (first line is a JSON object)
	// and scan events to determine duration.
	ReadCastEvents(f, func(rec Record) bool {
		switch rec.Type {
		case RecordHeader:
			info.Width = rec.Width
			info.Height = rec.Height
			// StartTime may be a Unix timestamp string from the cast header
			if rec.StartTime != "" {
				// Try as unix timestamp first (recorder writes epoch seconds)
				if ts, err := json.Number(rec.StartTime).Int64(); err == nil && ts > 0 {
					info.StartTime = time.Unix(ts, 0)
				}
			}
			return true // continue to scan events
		case RecordOutput:
			if rec.T != nil {
				info.Duration = *rec.T
				info.InProgress = false // has at least one event
			}
			return true
		default:
			return true
		}
	})

	return info, nil
}
