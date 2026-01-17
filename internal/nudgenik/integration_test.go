//go:build integration

package nudgenik

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sergek/schmux/internal/config"
)

func TestNudgenikClassification(t *testing.T) {
	// Use pass^k testing (k=3) to reduce variance from nondeterministic LLM responses.
	// See: https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents
	const passRuns = 3

	// Load config to access variants and run targets
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	// Get target name from config's nudgenik_target field
	targetName := cfg.GetNudgenikTarget()
	if targetName == "" {
		t.Fatalf("nudgenik_target not set in config.json (set to a variant name like \"claude-opus\")")
	}

	fixtures := []struct {
		capture   string
		wantState string
	}{
		{"claude1.txt", "Needs Feature Clarification"},
		{"claude2.txt", "Needs Authorization"},
		{"claude3.txt", "Needs Feature Clarification"},
		{"claude5.txt", "Completed"},
		{"claude6.txt", "Needs Authorization"},
		{"claude7.txt", "Needs Authorization"},
		{"claude8.txt", "Needs Authorization"},
		{"claude9.txt", "Needs Authorization"},
		{"claude10.txt", "Needs Authorization"},
		{"claude11.txt", "Completed"},
		{"claude12.txt", "Needs Authorization"},
		{"codex1.txt", "Needs User Testing"},
		{"codex2.txt", "Needs Feature Clarification"},
		{"codex4.txt", "Completed"},
		{"codex5.txt", "Completed"},
		{"codex13.txt", "Needs Authorization"},
	}

	for _, tt := range fixtures {
		t.Run(tt.capture, func(t *testing.T) {
			// Read capture
			path := filepath.Join("../tmux/testdata", tt.capture)
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

			for run := 1; run <= passRuns; run++ {
				result, err := AskForExtracted(context.Background(), cfg, extracted)
				if err != nil {
					t.Fatalf("ask nudgenik (run %d/%d): %v", run, passRuns, err)
				}

				// Check state
				if result.State != tt.wantState {
					t.Errorf("run %d/%d state = %q, want %q\nconfidence: %s\nsummary: %s",
						run, passRuns, result.State, tt.wantState, result.Confidence, result.Summary)
				}

				if testing.Verbose() {
					t.Logf("Run %d/%d result: state=%s confidence=%s", run, passRuns, result.State, result.Confidence)
				}
			}
		})
	}
}
