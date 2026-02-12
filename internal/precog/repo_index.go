package precog

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const defaultHistoryDepth = 1000

// RepoIndex is the Phase 1 repository analysis output.
type RepoIndex struct {
	RepoPath   string                   `json:"repo_path"`
	AnalyzedAt time.Time                `json:"analyzed_at"`
	Files      map[string]FileInfo      `json:"files"`
	Coupling   map[string][]CoupledFile `json:"coupling"`
	Packages   []Package                `json:"packages"`
}

// FileInfo describes an indexed file.
type FileInfo struct {
	Path    string   `json:"path"`
	Package string   `json:"package"`
	Authors []string `json:"authors"`
}

// CoupledFile is a file with historical co-change strength to another file.
type CoupledFile struct {
	Path     string  `json:"path"`
	Strength float64 `json:"strength"`
}

// Package describes a structural package/module bucket.
type Package struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	Files      []string `json:"files"`
	ConfigFile string   `json:"config_file,omitempty"`
}

// RepoIndexer produces a RepoIndex for a git repository.
type RepoIndexer struct {
	HistoryDepth int
}

// NewRepoIndexer creates a RepoIndexer with the provided history depth.
func NewRepoIndexer(historyDepth int) *RepoIndexer {
	if historyDepth <= 0 {
		historyDepth = defaultHistoryDepth
	}
	return &RepoIndexer{HistoryDepth: historyDepth}
}

// Analyze generates a repository index for repoPath.
func (r *RepoIndexer) Analyze(ctx context.Context, repoPath string) (*RepoIndex, error) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("resolve repo path: %w", err)
	}

	if err := ensureGitRepo(ctx, absPath); err != nil {
		return nil, err
	}

	trackedFiles, err := listTrackedFiles(ctx, absPath)
	if err != nil {
		return nil, err
	}

	coChange, err := AnalyzeCoChange(ctx, absPath, r.HistoryDepth)
	if err != nil {
		return nil, err
	}

	packages, filePackages := AnalyzeDirectoryStructure(absPath, trackedFiles)
	trackedSet := make(map[string]bool, len(trackedFiles))
	for _, f := range trackedFiles {
		trackedSet[f] = true
	}

	files := make(map[string]FileInfo, len(trackedFiles))
	for _, f := range trackedFiles {
		files[f] = FileInfo{
			Path:    f,
			Package: filePackages[f],
			Authors: coChange.FileAuthors[f],
		}
	}

	filteredCoupling := make(map[string][]CoupledFile, len(coChange.Coupling))
	for file, neighbors := range coChange.Coupling {
		if !trackedSet[file] {
			continue
		}
		for _, n := range neighbors {
			if !trackedSet[n.Path] {
				continue
			}
			filteredCoupling[file] = append(filteredCoupling[file], n)
		}
		if len(filteredCoupling[file]) == 0 {
			delete(filteredCoupling, file)
		}
	}

	for _, f := range trackedFiles {
		if _, ok := filteredCoupling[f]; !ok {
			filteredCoupling[f] = []CoupledFile{}
		}
	}

	return &RepoIndex{
		RepoPath:   absPath,
		AnalyzedAt: time.Now().UTC(),
		Files:      files,
		Coupling:   filteredCoupling,
		Packages:   packages,
	}, nil
}

func ensureGitRepo(ctx context.Context, repoPath string) error {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-dir")
	cmd.Dir = repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("not a git repository: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func listTrackedFiles(ctx context.Context, repoPath string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w", err)
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		files = append(files, filepath.ToSlash(line))
	}
	sort.Strings(files)
	return files, nil
}
