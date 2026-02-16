package precog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/oneshot"
)

// Analyzer runs the RCM analysis pipeline.
type Analyzer struct {
	config   *config.Config
	bareDir  string
	repoName string
}

// NewAnalyzer creates a new RCM analyzer.
func NewAnalyzer(cfg *config.Config, repoName string) (*Analyzer, error) {
	repo, found := cfg.FindRepo(repoName)
	if !found {
		return nil, fmt.Errorf("repo not found: %s", repoName)
	}
	if repo.BarePath == "" {
		return nil, fmt.Errorf("repo %s has no bare_path configured", repoName)
	}

	bareDir := filepath.Join(cfg.GetQueryRepoPath(), repo.BarePath)
	if _, err := os.Stat(bareDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("bare repo not found at %s", bareDir)
	}

	return &Analyzer{
		config:   cfg,
		bareDir:  bareDir,
		repoName: repoName,
	}, nil
}

// JobMeta holds metadata about an analysis job.
type JobMeta struct {
	JobID       string  `json:"job_id"`
	Status      string  `json:"status"` // "running", "completed", "failed"
	CurrentPass string  `json:"current_pass,omitempty"`
	StartedAt   string  `json:"started_at"`
	CompletedAt *string `json:"completed_at,omitempty"`
	CommitHash  string  `json:"commit_hash,omitempty"`
	Error       *string `json:"error,omitempty"`
}

// Run executes the full RCM analysis pipeline.
func (a *Analyzer) Run(ctx context.Context, jobID string, updateMeta func(JobMeta)) (*contracts.RCM, error) {
	startedAt := time.Now().UTC().Format(time.RFC3339)

	meta := JobMeta{
		JobID:     jobID,
		Status:    "running",
		StartedAt: startedAt,
	}
	updateMeta(meta)

	// Get HEAD commit
	commitHash, err := GetHeadCommit(ctx, a.bareDir)
	if err != nil {
		return nil, a.failJob(meta, updateMeta, fmt.Errorf("failed to get HEAD commit: %w", err))
	}
	meta.CommitHash = commitHash
	updateMeta(meta)

	// List all files
	files, err := ListFiles(ctx, a.bareDir)
	if err != nil {
		return nil, a.failJob(meta, updateMeta, fmt.Errorf("failed to list files: %w", err))
	}

	// Count lines by extension
	lineCounts, totalLines, err := CountLinesByExtension(ctx, a.bareDir, files)
	if err != nil {
		// Non-fatal, continue with zero counts
		lineCounts = make(map[string]int)
		totalLines = 0
	}

	// Get top languages
	topLangs := GetTopLanguages(lineCounts, 5)
	var primaryLanguages []string
	for _, lang := range topLangs {
		primaryLanguages = append(primaryLanguages, strings.TrimPrefix(lang.Extension, "."))
	}

	// Initialize RCM
	rcm := &contracts.RCM{
		RepoSummary: contracts.RCMRepoSummary{
			Name:             a.repoName,
			AnalyzedAt:       startedAt,
			CommitHash:       commitHash,
			PrimaryLanguages: primaryLanguages,
			LinesOfCode:      totalLines,
		},
	}

	// Run Pass A
	meta.CurrentPass = "A"
	updateMeta(meta)
	if err := a.runPassA(ctx, rcm, files); err != nil {
		// Log but continue - partial results are better than none
		fmt.Fprintf(os.Stderr, "[precog] Pass A failed: %v\n", err)
	}

	// Run Pass B
	meta.CurrentPass = "B"
	updateMeta(meta)
	if err := a.runPassB(ctx, rcm, files); err != nil {
		fmt.Fprintf(os.Stderr, "[precog] Pass B failed: %v\n", err)
	}

	// Run Pass C
	meta.CurrentPass = "C"
	updateMeta(meta)
	if err := a.runPassC(ctx, rcm, files); err != nil {
		fmt.Fprintf(os.Stderr, "[precog] Pass C failed: %v\n", err)
	}

	// Run Pass D
	meta.CurrentPass = "D"
	updateMeta(meta)
	if err := a.runPassD(ctx, rcm, files); err != nil {
		fmt.Fprintf(os.Stderr, "[precog] Pass D failed: %v\n", err)
	}

	// Run Pass E
	meta.CurrentPass = "E"
	updateMeta(meta)
	if err := a.runPassE(ctx, rcm); err != nil {
		fmt.Fprintf(os.Stderr, "[precog] Pass E failed: %v\n", err)
	}

	// Run Pass F
	meta.CurrentPass = "F"
	updateMeta(meta)
	if err := a.runPassF(ctx, rcm); err != nil {
		fmt.Fprintf(os.Stderr, "[precog] Pass F failed: %v\n", err)
	}

	// Mark complete
	completedAt := time.Now().UTC().Format(time.RFC3339)
	meta.Status = "completed"
	meta.CompletedAt = &completedAt
	meta.CurrentPass = ""
	updateMeta(meta)

	return rcm, nil
}

func (a *Analyzer) failJob(meta JobMeta, updateMeta func(JobMeta), err error) error {
	errStr := err.Error()
	meta.Status = "failed"
	meta.Error = &errStr
	updateMeta(meta)
	return err
}

func (a *Analyzer) runLLM(ctx context.Context, prompt, schemaLabel string) (string, error) {
	target := a.config.GetPrecogTarget()
	if target == "" {
		return "", fmt.Errorf("precog is disabled: set precog.target in config")
	}
	timeout := time.Duration(a.config.GetPrecogTimeout()) * time.Second

	return oneshot.ExecuteTarget(ctx, a.config, target, prompt, schemaLabel, timeout, a.bareDir)
}

func buildFileTree(files []string, maxLines int) string {
	if len(files) <= maxLines {
		return strings.Join(files, "\n")
	}

	// Group by top-level directory
	dirs := make(map[string][]string)
	for _, f := range files {
		parts := strings.SplitN(f, "/", 2)
		dir := parts[0]
		dirs[dir] = append(dirs[dir], f)
	}

	var result strings.Builder
	lineCount := 0
	for dir, dirFiles := range dirs {
		if lineCount >= maxLines {
			result.WriteString(fmt.Sprintf("\n... and %d more files", len(files)-lineCount))
			break
		}
		result.WriteString(fmt.Sprintf("\n%s/ (%d files)\n", dir, len(dirFiles)))
		lineCount++
		for i, f := range dirFiles {
			if i >= 10 || lineCount >= maxLines {
				if i < len(dirFiles) {
					result.WriteString(fmt.Sprintf("  ... and %d more\n", len(dirFiles)-i))
					lineCount++
				}
				break
			}
			result.WriteString(fmt.Sprintf("  %s\n", f))
			lineCount++
		}
	}
	return result.String()
}

// GetPrecogDir returns the precog output directory path.
func GetPrecogDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".schmux", "precog"), nil
}

// EnsurePrecogDir creates the precog directory if it doesn't exist.
func EnsurePrecogDir() error {
	dir, err := GetPrecogDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0755)
}

// SaveRCM saves an RCM to the precog directory.
func SaveRCM(repoName string, rcm *contracts.RCM) error {
	if err := EnsurePrecogDir(); err != nil {
		return err
	}
	dir, err := GetPrecogDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, repoName+".json")
	data, err := json.MarshalIndent(rcm, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadRCM loads an RCM from the precog directory.
func LoadRCM(repoName string) (*contracts.RCM, error) {
	dir, err := GetPrecogDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, repoName+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rcm contracts.RCM
	if err := json.Unmarshal(data, &rcm); err != nil {
		return nil, err
	}
	return &rcm, nil
}

// SaveJobMeta saves job metadata.
func SaveJobMeta(repoName string, meta JobMeta) error {
	if err := EnsurePrecogDir(); err != nil {
		return err
	}
	dir, err := GetPrecogDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, repoName+".meta.json")
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadJobMeta loads job metadata.
func LoadJobMeta(repoName string) (*JobMeta, error) {
	dir, err := GetPrecogDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, repoName+".meta.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta JobMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// RunPass executes a single pass and updates the RCM.
// Valid pass IDs: "a", "b", "c", "d", "e", "f" (case-insensitive).
func (a *Analyzer) RunPass(ctx context.Context, passID string) (*contracts.RCM, error) {
	passID = strings.ToUpper(passID)

	// Load existing RCM or create empty one
	rcm, err := LoadRCM(a.repoName)
	if err != nil {
		// No existing RCM, create minimal one
		rcm = &contracts.RCM{
			RepoSummary: contracts.RCMRepoSummary{
				Name: a.repoName,
			},
		}
	}

	// Get files for passes that need them
	var files []string
	if passID == "A" || passID == "B" || passID == "C" || passID == "D" {
		files, err = ListFiles(ctx, a.bareDir)
		if err != nil {
			return nil, fmt.Errorf("failed to list files: %w", err)
		}
	}

	// Run the specified pass
	switch passID {
	case "A":
		err = a.runPassA(ctx, rcm, files)
	case "B":
		err = a.runPassB(ctx, rcm, files)
	case "C":
		err = a.runPassC(ctx, rcm, files)
	case "D":
		err = a.runPassD(ctx, rcm, files)
	case "E":
		err = a.runPassE(ctx, rcm)
	case "F":
		err = a.runPassF(ctx, rcm)
	default:
		return nil, fmt.Errorf("unknown pass ID: %s (valid: a, b, c, d, e, f)", passID)
	}

	if err != nil {
		return nil, fmt.Errorf("pass %s failed: %w", passID, err)
	}

	return rcm, nil
}
