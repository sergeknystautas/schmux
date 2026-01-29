//go:build integration

package branchsuggest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"gopkg.in/yaml.v3"
)

type resultCount struct {
	branch   string
	nickname string
	count    int
}

func TestBranchSuggestion(t *testing.T) {
	// Use pass^k testing (k=3) to reduce variance from nondeterministic LLM responses.
	passRuns := 3
	if raw := strings.TrimSpace(os.Getenv("BRANCHSUGGEST_PASS_K")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("BRANCHSUGGEST_PASS_K must be an integer: %v", err)
		}
		if parsed <= 0 {
			t.Fatalf("BRANCHSUGGEST_PASS_K must be positive, got %d", parsed)
		}
		passRuns = parsed
	}

	// Load config to access models and run targets
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("get home dir: %v", err)
	}
	configPath := filepath.Join(homeDir, ".schmux", "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	// Get target name from config's branch_suggest field
	targetName := cfg.GetBranchSuggestTarget()
	if targetName == "" {
		t.Fatalf("branch_suggest.target not set in config.json")
	}

	cases := loadBranchSuggestManifest(t)

	type summaryRow struct {
		prompt       string
		wantBranch   string
		wantNickname string
		gotResults   []resultCount
	}
	var summaries []summaryRow
	var summariesMu sync.Mutex

	for _, tt := range cases {
		t.Run(tt.ID, func(t *testing.T) {
			t.Parallel()

			// Read prompt
			path := filepath.Join("testdata", tt.Prompt)
			prompt, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read prompt: %v", err)
			}

			if testing.Verbose() {
				t.Logf("Prompt: %s", strings.TrimSpace(string(prompt)))
			}

			resultCounts := make(map[string]resultCount)

			for run := 1; run <= passRuns; run++ {
				result, err := AskForPrompt(context.Background(), cfg, string(prompt))
				if err != nil {
					t.Fatalf("ask branch suggest (run %d/%d): %v", run, passRuns, err)
				}

				// Track results
				key := result.Branch + "|" + result.Nickname
				resultCounts[key] = resultCount{
					branch:   result.Branch,
					nickname: result.Nickname,
					count:    resultCounts[key].count + 1,
				}

				if result.Branch != tt.WantBranch || result.Nickname != tt.WantNickname {
					t.Errorf("run %d/%d: branch=%q nickname=%q, want branch=%q nickname=%q",
						run, passRuns, result.Branch, result.Nickname, tt.WantBranch, tt.WantNickname)
				}

				if testing.Verbose() {
					t.Logf("Result: branch=%s nickname=%s", result.Branch, result.Nickname)
				}
			}

			// Convert map to slice for summary
			var gotResults []resultCount
			for _, rc := range resultCounts {
				gotResults = append(gotResults, rc)
			}
			sort.Slice(gotResults, func(i, j int) bool {
				return gotResults[i].count > gotResults[j].count
			})

			summariesMu.Lock()
			summaries = append(summaries, summaryRow{
				prompt:       tt.Prompt,
				wantBranch:   tt.WantBranch,
				wantNickname: tt.WantNickname,
				gotResults:   gotResults,
			})
			summariesMu.Unlock()
		})
	}

	t.Cleanup(func() {
		summariesMu.Lock()
		defer summariesMu.Unlock()

		if len(summaries) == 0 {
			return
		}

		sort.Slice(summaries, func(i, j int) bool {
			return summaries[i].prompt < summaries[j].prompt
		})

		promptWidth := len("PROMPT")
		wantWidth := len("WANT")
		gotWidth := len("GOT")
		for _, row := range summaries {
			if len(row.prompt) > promptWidth {
				promptWidth = len(row.prompt)
			}
			wantText := fmt.Sprintf("%s (%s)", row.wantBranch, row.wantNickname)
			if len(wantText) > wantWidth {
				wantWidth = len(wantText)
			}
			gotText := formatGotResults(row.gotResults)
			if len(gotText) > gotWidth {
				gotWidth = len(gotText)
			}
		}

		fmt.Println()
		fmt.Println("Branch suggestion summary:")
		fmt.Printf("%-*s  %-*s  %-*s  %s\n", promptWidth, "PROMPT", wantWidth, "WANT", gotWidth, "GOT", "STATUS")
		for _, row := range summaries {
			wantText := fmt.Sprintf("%s (%s)", row.wantBranch, row.wantNickname)
			gotText := formatGotResults(row.gotResults)

			status := "FAIL"
			if len(row.gotResults) == 1 &&
				row.gotResults[0].branch == row.wantBranch &&
				row.gotResults[0].nickname == row.wantNickname &&
				row.gotResults[0].count == passRuns {
				status = "PASS"
			}

			fmt.Printf("%-*s  %-*s  %-*s  %s\n", promptWidth, row.prompt, wantWidth, wantText, gotWidth, gotText, status)
		}
	})
}

type branchSuggestManifest struct {
	Version int                     `yaml:"version"`
	Cases   []branchSuggestTestCase `yaml:"cases"`
}

type branchSuggestTestCase struct {
	ID           string `yaml:"id"`
	Prompt       string `yaml:"prompt"`
	WantBranch   string `yaml:"want_branch"`
	WantNickname string `yaml:"want_nickname"`
	Notes        string `yaml:"notes"`
}

func loadBranchSuggestManifest(t *testing.T) []branchSuggestTestCase {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "manifest.yaml"))
	if err != nil {
		t.Fatalf("read branch suggest manifest: %v", err)
	}

	var manifest branchSuggestManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse branch suggest manifest: %v", err)
	}

	if len(manifest.Cases) == 0 {
		t.Fatalf("branch suggest manifest has no cases")
	}

	for i, tc := range manifest.Cases {
		if strings.TrimSpace(tc.Prompt) == "" {
			t.Fatalf("branch suggest manifest case %d missing prompt", i)
		}
		if strings.TrimSpace(tc.WantBranch) == "" {
			t.Fatalf("branch suggest manifest case %d missing want_branch", i)
		}
		if strings.TrimSpace(tc.WantNickname) == "" {
			t.Fatalf("branch suggest manifest case %d missing want_nickname", i)
		}
	}

	return manifest.Cases
}

func formatGotResults(results []resultCount) string {
	if len(results) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(results))
	for _, r := range results {
		parts = append(parts, fmt.Sprintf("%s (%s) x%d", r.branch, r.nickname, r.count))
	}
	return strings.Join(parts, ", ")
}
