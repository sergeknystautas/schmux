package precog

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

const commitHeaderPrefix = "__SCHMUX_COMMIT__"

// CoChangeAnalysis captures co-change and authorship derived from git history.
type CoChangeAnalysis struct {
	Coupling    map[string][]CoupledFile
	FileAuthors map[string][]string
}

type commitChange struct {
	Author string
	Files  []string
}

// AnalyzeCoChange parses git history and computes pairwise co-change strengths.
func AnalyzeCoChange(ctx context.Context, repoPath string, historyDepth int) (*CoChangeAnalysis, error) {
	commits, err := collectCommitChanges(ctx, repoPath, historyDepth)
	if err != nil {
		return nil, err
	}

	fileCommitCount := map[string]int{}
	pairCount := map[string]map[string]int{}
	fileAuthors := map[string]map[string]bool{}

	for _, commit := range commits {
		if len(commit.Files) == 0 {
			continue
		}
		for _, file := range commit.Files {
			fileCommitCount[file]++
			if _, ok := fileAuthors[file]; !ok {
				fileAuthors[file] = map[string]bool{}
			}
			if commit.Author != "" {
				fileAuthors[file][commit.Author] = true
			}
		}

		for i := 0; i < len(commit.Files); i++ {
			for j := i + 1; j < len(commit.Files); j++ {
				a, b := normalizePair(commit.Files[i], commit.Files[j])
				if _, ok := pairCount[a]; !ok {
					pairCount[a] = map[string]int{}
				}
				pairCount[a][b]++
			}
		}
	}

	coupling := map[string][]CoupledFile{}
	for a, neighbors := range pairCount {
		for b, count := range neighbors {
			denominator := minInt(fileCommitCount[a], fileCommitCount[b])
			if denominator <= 0 {
				continue
			}
			strength := float64(count) / float64(denominator)
			coupling[a] = append(coupling[a], CoupledFile{
				Path:     b,
				Strength: strength,
			})
			coupling[b] = append(coupling[b], CoupledFile{
				Path:     a,
				Strength: strength,
			})
		}
	}

	for file := range coupling {
		sort.Slice(coupling[file], func(i, j int) bool {
			if coupling[file][i].Strength == coupling[file][j].Strength {
				return coupling[file][i].Path < coupling[file][j].Path
			}
			return coupling[file][i].Strength > coupling[file][j].Strength
		})
	}

	authors := map[string][]string{}
	for file, set := range fileAuthors {
		for author := range set {
			authors[file] = append(authors[file], author)
		}
		sort.Strings(authors[file])
	}

	return &CoChangeAnalysis{
		Coupling:    coupling,
		FileAuthors: authors,
	}, nil
}

func collectCommitChanges(ctx context.Context, repoPath string, historyDepth int) ([]commitChange, error) {
	if historyDepth <= 0 {
		historyDepth = defaultHistoryDepth
	}

	cmd := exec.CommandContext(ctx, "git", "log",
		"--name-only",
		"--format="+commitHeaderPrefix+"%an",
		fmt.Sprintf("--max-count=%d", historyDepth),
	)
	cmd.Dir = repoPath

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	var (
		commits []commitChange
		current commitChange
	)

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, commitHeaderPrefix) {
			if len(current.Files) > 0 {
				current.Files = uniqueStrings(current.Files)
				sort.Strings(current.Files)
				commits = append(commits, current)
			}
			current = commitChange{
				Author: strings.TrimPrefix(line, commitHeaderPrefix),
				Files:  []string{},
			}
			continue
		}
		current.Files = append(current.Files, line)
	}

	if len(current.Files) > 0 {
		current.Files = uniqueStrings(current.Files)
		sort.Strings(current.Files)
		commits = append(commits, current)
	}

	return commits, nil
}

func normalizePair(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		result = append(result, v)
	}
	return result
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
