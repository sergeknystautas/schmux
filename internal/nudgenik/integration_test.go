//go:build integration

package nudgenik

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sort"
	"sync"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"gopkg.in/yaml.v3"
)

func TestNudgenikClassification(t *testing.T) {
	// Use pass^k testing (k=3) to reduce variance from nondeterministic LLM responses.
	// See: https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents
	passRuns := 3
	if raw := strings.TrimSpace(os.Getenv("NUDGENIK_PASS_K")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("NUDGENIK_PASS_K must be an integer: %v", err)
		}
		if parsed <= 0 {
			t.Fatalf("NUDGENIK_PASS_K must be positive, got %d", parsed)
		}
		passRuns = parsed
	}

	// Load config to access variants and run targets
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("get home dir: %v", err)
	}
	configPath := filepath.Join(homeDir, ".schmux", "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	// Get target name from config's nudgenik_target field
	targetName := cfg.GetNudgenikTarget()
	if targetName == "" {
		t.Fatalf("nudgenik_target not set in config.json (set to a variant name like \"claude-opus\")")
	}

	cases := loadNudgenikManifest(t)

	type summaryRow struct {
		capture   string
		wantState string
		gotStates map[string]int
	}
	var summaries []summaryRow
	var summariesMu sync.Mutex

	for _, tt := range cases {
		t.Run(tt.Capture, func(t *testing.T) {
			t.Parallel()
			// Read capture
			path := filepath.Join("../tmux/testdata", tt.Capture)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read capture: %v", err)
			}

			extracted, err := ExtractLatestFromCapture(string(data))
			if err != nil {
				t.Fatalf("extract content: %v", err)
			}

			if testing.Verbose() {
				extractedLines := strings.Split(extracted, "\n")
				preview := strings.Join(extractedLines[:min(5, len(extractedLines))], "\n")
				if len(extractedLines) > 5 {
					preview += "\n... (" + strconv.Itoa(len(extractedLines)-5) + " more lines)"
				}
				t.Logf("Extracted content preview:\n%s", preview)
			}

			gotStates := make(map[string]int)
			for run := 1; run <= passRuns; run++ {
				result, err := AskForExtracted(context.Background(), cfg, extracted)
				if err != nil {
					t.Fatalf("ask nudgenik (run %d/%d): %v", run, passRuns, err)
				}

				// Check state
				gotStates[result.State]++
				if result.State != tt.WantState {
					t.Errorf("run %d/%d state = %q, want %q\nconfidence: %s\nsummary: %s",
						run, passRuns, result.State, tt.WantState, result.Confidence, result.Summary)
				}

				if testing.Verbose() {
					t.Logf("Result: state=%s confidence=%s", result.State, result.Confidence)
				}
			}

			summariesMu.Lock()
			summaries = append(summaries, summaryRow{
				capture:   tt.Capture,
				wantState: tt.WantState,
				gotStates: gotStates,
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
			return summaries[i].capture < summaries[j].capture
		})

		fileWidth := len("FILE")
		wantWidth := len("WANT")
		gotWidth := len("GOT")
		for _, row := range summaries {
			if len(row.capture) > fileWidth {
				fileWidth = len(row.capture)
			}
			if len(row.wantState) > wantWidth {
				wantWidth = len(row.wantState)
			}
			gotText := formatGotStates(row.gotStates)
			if len(gotText) > gotWidth {
				gotWidth = len(gotText)
			}
		}

		fmt.Println()
		fmt.Println("Nudgenik classification summary:")
		fmt.Printf("%-*s  %-*s  %-*s  %s\n", fileWidth, "FILE", wantWidth, "WANT", gotWidth, "GOT", "STATUS")
		for _, row := range summaries {
			gotText := formatGotStates(row.gotStates)
			status := "FAIL"
			if row.gotStates[row.wantState] == passRuns && len(row.gotStates) == 1 {
				status = "PASS"
			}
			fmt.Printf("%-*s  %-*s  %-*s  %s\n", fileWidth, row.capture, wantWidth, row.wantState, gotWidth, gotText, status)
		}
	})
}

type nudgenikManifest struct {
	Version int                  `yaml:"version"`
	Cases   []nudgenikTestCase   `yaml:"cases"`
}

type nudgenikTestCase struct {
	ID        string `yaml:"id"`
	Capture   string `yaml:"capture"`
	WantState string `yaml:"want_state"`
	Notes     string `yaml:"notes"`
}

func loadNudgenikManifest(t *testing.T) []nudgenikTestCase {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "tmux", "testdata", "manifest.yaml"))
	if err != nil {
		t.Fatalf("read nudgenik manifest: %v", err)
	}

	var manifest nudgenikManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse nudgenik manifest: %v", err)
	}

	if len(manifest.Cases) == 0 {
		t.Fatalf("nudgenik manifest has no cases")
	}

	for i, tc := range manifest.Cases {
		if strings.TrimSpace(tc.Capture) == "" {
			t.Fatalf("nudgenik manifest case %d missing capture", i)
		}
		if strings.TrimSpace(tc.WantState) == "" {
			t.Fatalf("nudgenik manifest case %d missing want_state", i)
		}
	}

	return manifest.Cases
}

func formatGotStates(gotStates map[string]int) string {
	if len(gotStates) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(gotStates))
	for state := range gotStates {
		keys = append(keys, state)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, state := range keys {
		parts = append(parts, fmt.Sprintf("%s x%d", state, gotStates[state]))
	}
	return strings.Join(parts, ", ")
}
