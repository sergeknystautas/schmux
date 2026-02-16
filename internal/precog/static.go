package precog

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// FileInfo holds information about a file in the repository.
type FileInfo struct {
	Path      string
	Extension string
	Lines     int
}

// CommitInfo holds information about a commit.
type CommitInfo struct {
	Hash    string
	Subject string
	Files   []string
}

// ListFiles returns all files in the repository at HEAD.
func ListFiles(ctx context.Context, bareDir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-tree", "-r", "--name-only", "HEAD")
	cmd.Dir = bareDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-tree failed: %w", err)
	}

	var files []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			files = append(files, line)
		}
	}
	return files, scanner.Err()
}

// GetHeadCommit returns the HEAD commit hash.
func GetHeadCommit(ctx context.Context, bareDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = bareDir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// GetCommitLog returns commit information for the last n commits.
func GetCommitLog(ctx context.Context, bareDir string, limit int) ([]CommitInfo, error) {
	cmd := exec.CommandContext(ctx, "git", "log",
		fmt.Sprintf("-n%d", limit),
		"--name-only",
		"--pretty=format:COMMIT:%H|%s")
	cmd.Dir = bareDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	var commits []CommitInfo
	var current *CommitInfo

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "COMMIT:") {
			// Save previous commit if exists
			if current != nil {
				commits = append(commits, *current)
			}
			// Parse new commit
			parts := strings.SplitN(strings.TrimPrefix(line, "COMMIT:"), "|", 2)
			current = &CommitInfo{Hash: parts[0]}
			if len(parts) > 1 {
				current.Subject = parts[1]
			}
		} else if current != nil && strings.TrimSpace(line) != "" {
			current.Files = append(current.Files, strings.TrimSpace(line))
		}
	}
	if current != nil {
		commits = append(commits, *current)
	}

	return commits, scanner.Err()
}

// GetRecentCommits returns commits from the last n months.
func GetRecentCommits(ctx context.Context, bareDir string, months int) ([]CommitInfo, error) {
	cmd := exec.CommandContext(ctx, "git", "log",
		fmt.Sprintf("--since=%d months ago", months),
		"--name-only",
		"--pretty=format:COMMIT:%H|%s")
	cmd.Dir = bareDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	var commits []CommitInfo
	var current *CommitInfo

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "COMMIT:") {
			if current != nil {
				commits = append(commits, *current)
			}
			parts := strings.SplitN(strings.TrimPrefix(line, "COMMIT:"), "|", 2)
			current = &CommitInfo{Hash: parts[0]}
			if len(parts) > 1 {
				current.Subject = parts[1]
			}
		} else if current != nil && strings.TrimSpace(line) != "" {
			current.Files = append(current.Files, strings.TrimSpace(line))
		}
	}
	if current != nil {
		commits = append(commits, *current)
	}

	return commits, scanner.Err()
}

// CountLinesByExtension counts lines of code by file extension.
func CountLinesByExtension(ctx context.Context, bareDir string, files []string) (map[string]int, int, error) {
	counts := make(map[string]int)
	total := 0

	for _, file := range files {
		ext := filepath.Ext(file)
		if ext == "" {
			ext = "(no extension)"
		}

		// Count lines using git show
		cmd := exec.CommandContext(ctx, "git", "show", "HEAD:"+file)
		cmd.Dir = bareDir
		output, err := cmd.Output()
		if err != nil {
			// Skip files that can't be read (binary, etc)
			continue
		}

		lines := strings.Count(string(output), "\n")
		counts[ext] += lines
		total += lines
	}

	return counts, total, nil
}

// LanguageStats holds language statistics.
type LanguageStats struct {
	Extension string
	Lines     int
}

// GetTopLanguages returns the top n languages by line count.
func GetTopLanguages(counts map[string]int, n int) []LanguageStats {
	var stats []LanguageStats
	for ext, lines := range counts {
		stats = append(stats, LanguageStats{Extension: ext, Lines: lines})
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Lines > stats[j].Lines
	})
	if len(stats) > n {
		stats = stats[:n]
	}
	return stats
}

// FindEntryPoints looks for common entry point patterns in file paths.
func FindEntryPoints(files []string) []string {
	var entryPoints []string
	seen := make(map[string]bool)

	for _, file := range files {
		// Go main packages
		if strings.HasPrefix(file, "cmd/") && strings.HasSuffix(file, ".go") {
			dir := filepath.Dir(file)
			if !seen[dir] {
				entryPoints = append(entryPoints, dir)
				seen[dir] = true
			}
		}
		// Root main.go
		if file == "main.go" && !seen["main.go"] {
			entryPoints = append(entryPoints, "main.go")
			seen["main.go"] = true
		}
		// Package.json (Node.js)
		if file == "package.json" && !seen["package.json"] {
			entryPoints = append(entryPoints, "package.json")
			seen["package.json"] = true
		}
		// Python entry points
		if file == "__main__.py" || file == "main.py" || file == "app.py" {
			if !seen[file] {
				entryPoints = append(entryPoints, file)
				seen[file] = true
			}
		}
	}

	return entryPoints
}

// GetPackages extracts Go package paths from files.
func GetPackages(files []string) []string {
	packages := make(map[string]bool)
	for _, file := range files {
		if strings.HasSuffix(file, ".go") && !strings.HasSuffix(file, "_test.go") {
			pkg := filepath.Dir(file)
			packages[pkg] = true
		}
	}

	var result []string
	for pkg := range packages {
		result = append(result, pkg)
	}
	sort.Strings(result)
	return result
}

// ImportInfo holds import relationship info.
type ImportInfo struct {
	File    string
	Imports []string
}

// ExtractGoImports extracts imports from a Go file content.
//
// TODO: This is a hacky string-based parser. If precog shows promise:
// - Replace with go/parser for proper Go import extraction
// - Add ExtractTSImports for TypeScript (parse import/require statements)
// - Use primaryLanguages from repo summary to decide which extractors to run
func ExtractGoImports(content string) []string {
	var imports []string
	inImportBlock := false

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "import (") {
			inImportBlock = true
			continue
		}
		if inImportBlock && line == ")" {
			inImportBlock = false
			continue
		}
		if inImportBlock {
			// Parse import line
			imp := strings.Trim(line, `"`)
			// Handle aliased imports: alias "path"
			if idx := strings.Index(line, `"`); idx > 0 {
				imp = strings.Trim(line[idx:], `"`)
			}
			if imp != "" && !strings.HasPrefix(imp, "//") {
				imports = append(imports, imp)
			}
		}
		if strings.HasPrefix(line, `import "`) {
			imp := strings.TrimPrefix(line, `import "`)
			imp = strings.TrimSuffix(imp, `"`)
			imports = append(imports, imp)
		}
	}

	return imports
}

// ComputeFanIn computes how many files import each package.
func ComputeFanIn(importInfos []ImportInfo, localPrefix string) map[string]int {
	fanIn := make(map[string]int)
	for _, info := range importInfos {
		for _, imp := range info.Imports {
			// Only count local imports
			if strings.HasPrefix(imp, localPrefix) {
				fanIn[imp]++
			}
		}
	}
	return fanIn
}

// CoChangeMatrix tracks how often files change together.
type CoChangeMatrix struct {
	pairs map[string]map[string]int
}

// NewCoChangeMatrix creates a new co-change matrix from commits.
func NewCoChangeMatrix(commits []CommitInfo) *CoChangeMatrix {
	m := &CoChangeMatrix{pairs: make(map[string]map[string]int)}

	for _, commit := range commits {
		// For each pair of files in the commit
		for i, f1 := range commit.Files {
			for j := i + 1; j < len(commit.Files); j++ {
				f2 := commit.Files[j]
				m.increment(f1, f2)
			}
		}
	}

	return m
}

func (m *CoChangeMatrix) increment(f1, f2 string) {
	// Ensure consistent ordering
	if f1 > f2 {
		f1, f2 = f2, f1
	}
	if m.pairs[f1] == nil {
		m.pairs[f1] = make(map[string]int)
	}
	m.pairs[f1][f2]++
}

// TopCoChanges returns the top n file pairs that change together.
func (m *CoChangeMatrix) TopCoChanges(n int) [][3]string {
	type pair struct {
		f1, f2 string
		count  int
	}
	var pairs []pair
	for f1, inner := range m.pairs {
		for f2, count := range inner {
			pairs = append(pairs, pair{f1, f2, count})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].count > pairs[j].count
	})
	if len(pairs) > n {
		pairs = pairs[:n]
	}

	var result [][3]string
	for _, p := range pairs {
		result = append(result, [3]string{p.f1, p.f2, strconv.Itoa(p.count)})
	}
	return result
}

// ChurnByDirectory computes the number of commits touching each directory.
func ChurnByDirectory(commits []CommitInfo) map[string]int {
	churn := make(map[string]int)
	for _, commit := range commits {
		seen := make(map[string]bool)
		for _, file := range commit.Files {
			dir := filepath.Dir(file)
			if !seen[dir] {
				churn[dir]++
				seen[dir] = true
			}
		}
	}
	return churn
}
