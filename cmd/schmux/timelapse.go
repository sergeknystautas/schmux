//go:build !notimelapse

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/timelapse"
)

// TimelapseCommand handles the 'schmux timelapse' subcommand.
type TimelapseCommand struct{}

// NewTimelapseCommand creates a new timelapse command.
func NewTimelapseCommand() *TimelapseCommand {
	return &TimelapseCommand{}
}

// Run executes the timelapse command.
func (c *TimelapseCommand) Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: schmux timelapse <list|export|delete>")
	}

	switch args[0] {
	case "list":
		return c.list()
	case "export":
		if len(args) < 2 {
			return fmt.Errorf("usage: schmux timelapse export <recording-id> [-o output.timelapse.cast]")
		}
		outputPath := ""
		for i := 2; i < len(args); i++ {
			if args[i] == "-o" && i+1 < len(args) {
				outputPath = args[i+1]
				break
			}
		}
		return c.export(args[1], outputPath)
	case "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: schmux timelapse delete <recording-id>")
		}
		return c.delete(args[1])
	default:
		return fmt.Errorf("unknown timelapse command: %s\nUsage: schmux timelapse <list|export|delete>", args[0])
	}
}

func (c *TimelapseCommand) list() error {
	dir := recordingsDir()
	recordings, err := timelapse.ListRecordings(dir)
	if err != nil {
		return fmt.Errorf("failed to list recordings: %w", err)
	}

	if len(recordings) == 0 {
		fmt.Println("No recordings found.")
		return nil
	}

	// Print table header
	fmt.Printf("%-30s  %-12s  %-10s  %-10s  %-10s\n",
		"RECORDING ID", "SESSION", "DURATION", "SIZE", "STATUS")

	for _, r := range recordings {
		status := "complete"
		if r.InProgress {
			status = "in-progress"
		}

		duration := formatDuration(r.Duration)
		size := formatSize(r.FileSize)

		sessionID := r.SessionID
		if len(sessionID) > 12 {
			sessionID = sessionID[:12]
		}

		fmt.Printf("%-30s  %-12s  %-10s  %-10s  %-10s\n",
			r.RecordingID, sessionID, duration, size, status)
	}

	return nil
}

func (c *TimelapseCommand) export(recordingID, outputPath string) error {
	dir := recordingsDir()
	recordingPath := filepath.Join(dir, recordingID+".cast")

	if _, err := os.Stat(recordingPath); os.IsNotExist(err) {
		return fmt.Errorf("recording not found: %s", recordingID)
	}

	if outputPath == "" {
		outputPath = recordingID + ".timelapse.cast"
	}

	fmt.Fprintf(os.Stderr, "Note: recordings may contain sensitive terminal output. Review before sharing.\n")
	fmt.Fprintf(os.Stderr, "Compressing %s -> %s\n", recordingID, outputPath)

	exp := timelapse.NewExporter(recordingPath, outputPath, func(pct float64) {
		fmt.Fprintf(os.Stderr, "\rProgress: %.0f%%", pct*100)
	})

	if err := exp.Export(); err != nil {
		return fmt.Errorf("export failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\rCompression complete: %s\n", outputPath)
	return nil
}

func (c *TimelapseCommand) delete(recordingID string) error {
	dir := recordingsDir()
	castPath := filepath.Join(dir, recordingID+".cast")
	compressedPath := filepath.Join(dir, recordingID+".timelapse.cast")

	if _, err := os.Stat(castPath); os.IsNotExist(err) {
		return fmt.Errorf("recording not found: %s", recordingID)
	}

	os.Remove(castPath)
	os.Remove(compressedPath) // may not exist
	fmt.Printf("Deleted recording: %s\n", recordingID)
	return nil
}

func recordingsDir() string {
	return schmuxdir.RecordingsDir()
}

func formatDuration(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
	)
	switch {
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
