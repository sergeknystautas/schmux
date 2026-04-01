package timelapse

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RecordingInfo holds metadata for a single recording file.
type RecordingInfo struct {
	RecordingID string
	SessionID   string
	StartTime   time.Time
	Duration    float64
	FileSize    int64
	Width       int
	Height      int
	InProgress  bool
	HasExport   bool
	Path        string
}

// ListRecordings returns metadata for all recordings in dir.
func ListRecordings(dir string) ([]RecordingInfo, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}

	var result []RecordingInfo
	for _, path := range matches {
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

	recordingID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	castPath := strings.TrimSuffix(path, ".jsonl") + ".cast"
	_, castErr := os.Stat(castPath)

	info := RecordingInfo{
		RecordingID: recordingID,
		FileSize:    stat.Size(),
		Path:        path,
		InProgress:  true, // assumed until we find an end record
		HasExport:   castErr == nil,
	}

	// Read header and optionally the end record
	ReadRecords(f, func(rec Record) bool {
		switch rec.Type {
		case RecordHeader:
			info.SessionID = rec.SessionID
			info.Width = rec.Width
			info.Height = rec.Height
			if t, err := time.Parse(time.RFC3339, rec.StartTime); err == nil {
				info.StartTime = t
			}
			return true // continue to look for end
		case RecordEnd:
			info.InProgress = false
			if rec.T != nil {
				info.Duration = *rec.T
			}
			return false // done
		case RecordOutput:
			// Track duration from last output as fallback for in-progress
			if rec.T != nil {
				info.Duration = *rec.T
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
			// Also remove cached .cast export
			castPath := strings.TrimSuffix(r.Path, ".jsonl") + ".cast"
			os.Remove(castPath)
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
		castPath := strings.TrimSuffix(oldest.Path, ".jsonl") + ".cast"
		os.Remove(castPath)
		totalBytes -= oldest.FileSize
		remaining = remaining[1:]
	}

	return nil
}
