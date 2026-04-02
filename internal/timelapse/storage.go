package timelapse

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RecordingInfo holds metadata for a single recording file.
type RecordingInfo struct {
	RecordingID   string
	SessionID     string
	StartTime     time.Time
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

	// Sort by start time, newest first
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartTime.After(result[j].StartTime)
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

	// Derive SessionID from recording ID: "<sessionID>-<unixTimestamp>"
	sessionID := recordingID
	if lastDash := strings.LastIndex(recordingID, "-"); lastDash > 0 {
		sessionID = recordingID[:lastDash]
	}

	info := RecordingInfo{
		RecordingID:   recordingID,
		SessionID:     sessionID,
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

// PruneRecordings deletes recordings older than retentionDays
// and evicts oldest-first when totalBytes exceeds maxTotalBytes.
func PruneRecordings(dir string, retentionDays int, maxTotalBytes int64) error {
	recordings, err := ListRecordings(dir)
	if err != nil {
		return err
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	// Age-based pruning
	var remaining []RecordingInfo
	for _, r := range recordings {
		if !r.StartTime.IsZero() && r.StartTime.Before(cutoff) && !r.InProgress {
			os.Remove(r.Path)
			// Also remove compressed .timelapse.cast
			compressedPath := strings.TrimSuffix(r.Path, ".cast") + ".timelapse.cast"
			os.Remove(compressedPath)
		} else {
			remaining = append(remaining, r)
		}
	}

	// Size-based eviction (oldest first)
	// Sort by start time ascending (oldest first) for eviction
	sort.Slice(remaining, func(i, j int) bool {
		return remaining[i].StartTime.Before(remaining[j].StartTime)
	})

	var totalBytes int64
	for _, r := range remaining {
		totalBytes += r.FileSize
	}

	for totalBytes > maxTotalBytes && len(remaining) > 0 {
		oldest := remaining[0]
		if oldest.InProgress {
			break // don't evict in-progress recordings
		}
		os.Remove(oldest.Path)
		compressedPath := strings.TrimSuffix(oldest.Path, ".cast") + ".timelapse.cast"
		os.Remove(compressedPath)
		totalBytes -= oldest.FileSize
		remaining = remaining[1:]
	}

	return nil
}
