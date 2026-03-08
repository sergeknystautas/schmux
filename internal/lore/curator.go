package lore

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sergeknystautas/schmux/internal/schema"
)

func init() {
	schema.Register(schema.LabelLoreCurator, ExtractionResponse{})
}

// ExtractionResponse is the expected JSON output from the extraction curator LLM.
type ExtractionResponse struct {
	Rules            []ExtractedRule `json:"rules"`
	DiscardedEntries []string        `json:"discarded_entries"`
}

// ExtractedRule is a discrete rule extracted by the curator LLM.
type ExtractedRule struct {
	Text           string   `json:"text"`
	Category       string   `json:"category"`
	SuggestedLayer string   `json:"suggested_layer"`
	SourceEntries  []string `json:"source_entries"`
}

// BuildExtractionPrompt constructs the LLM prompt for extracting discrete rules from friction data.
// Extraction is blind — it does NOT include instruction files.
// existingRules lists rules already extracted in pending proposals; the LLM is told not to
// re-extract semantically equivalent rules.
// dismissedRules lists rules the user previously rejected; the LLM is told not to re-propose them.
func BuildExtractionPrompt(entries []Entry, existingRules []string, dismissedRules []string) string {
	var sb strings.Builder
	sb.WriteString(`You are a rule extractor for a multi-agent software development environment.

You will receive failure records and friction reflections from agent work sessions.
Your job is to extract discrete, actionable rules that prevent future agents from
repeating these mistakes.

Rules for extraction:
- SYNTHESIZE: Turn failure patterns into actionable rules
  (e.g., 5 "npm run build" failures → "Always use go run ./cmd/build-dashboard")
- DEDUPLICATE: Multiple agents hitting the same wall → one rule
- FILTER: Discard one-off failures that don't indicate systemic issues
  (e.g., a single typo in a file path is not rule-worthy)
- Write rules as imperatives: "Use X, not Y" / "Always run X before Y"
- Each rule must be self-contained — understandable without the original failure context
- Assign each rule a category (e.g., "build", "testing", "environment", "workflow")
- Assign a suggested_layer:
  - "repo_public": rules specific to this repository, safe to commit (e.g., build commands, project structure)
  - "repo_private": rules specific to this repository but private (e.g., internal tooling, personal preferences)
  - "cross_repo_private": rules that apply across all repositories (e.g., agent behavior, environment setup)
- Output ONLY valid JSON matching the schema below, no markdown fencing

Output schema:
{
  "rules": [
    {
      "text": "Always use go run ./cmd/build-dashboard instead of npm run build",
      "category": "build",
      "suggested_layer": "repo_public",
      "source_entries": ["<timestamp or entry key that led to this rule>"]
    }
  ],
  "discarded_entries": ["<timestamp or entry key of discarded entries>"]
}

`)

	// Include existing pending rules so the LLM avoids re-extracting them
	if len(existingRules) > 0 {
		sb.WriteString("ALREADY EXTRACTED RULES (pending review — do NOT re-extract these or semantically equivalent rules):\n")
		for _, r := range existingRules {
			fmt.Fprintf(&sb, "- %s\n", r)
		}
		sb.WriteString("\n")
	}

	// Include dismissed rules so the LLM avoids re-proposing them
	if len(dismissedRules) > 0 {
		sb.WriteString("PREVIOUSLY REJECTED RULES (user dismissed these — do NOT re-propose them or semantically equivalent rules):\n")
		for _, r := range dismissedRules {
			fmt.Fprintf(&sb, "- %s\n", r)
		}
		sb.WriteString("\n")
	}

	// Separate entries by type and deduplicate
	var failures, reflections []Entry
	failureSeen := make(map[string]bool)
	reflectionSeen := make(map[string]bool)
	for _, e := range entries {
		if e.Type == "failure" {
			key := fmt.Sprintf("%s|%s|%s|%s", e.Tool, e.Category, e.InputSummary, e.ErrorSummary)
			if failureSeen[key] {
				continue
			}
			failureSeen[key] = true
			failures = append(failures, e)
		} else {
			text := strings.TrimSpace(e.Text)
			if text == "" || strings.EqualFold(text, "none") {
				continue
			}
			if reflectionSeen[text] {
				continue
			}
			reflectionSeen[text] = true
			reflections = append(reflections, e)
		}
	}

	if len(failures) > 0 {
		sb.WriteString("FAILURE RECORDS:\n")
		for _, e := range failures {
			fmt.Fprintf(&sb, "- [%s] [%s] [%s] [%s] command: %q → error: %q\n",
				e.Agent, e.Tool, e.Category, e.Workspace, e.InputSummary, e.ErrorSummary)
		}
	}

	if len(reflections) > 0 {
		sb.WriteString("\nFRICTION REFLECTIONS:\n")
		for _, e := range reflections {
			fmt.Fprintf(&sb, "- [%s] [%s] [%s] %s\n", e.Agent, e.Type, e.Workspace, e.Text)
		}
	}

	return sb.String()
}

// ParseExtractionResponse parses the LLM JSON response into an ExtractionResponse.
func ParseExtractionResponse(response string) (*ExtractionResponse, error) {
	response = strings.TrimSpace(response)
	// Try parsing directly first — avoids corrupting JSON that contains
	// backticks in string values (e.g., code snippets in source_entries).
	var result ExtractionResponse
	if err := json.Unmarshal([]byte(response), &result); err == nil {
		return &result, nil
	}
	// Fallback: strip markdown code fences and retry.
	stripped := stripFencing(response)
	if err := json.Unmarshal([]byte(stripped), &result); err != nil {
		return nil, fmt.Errorf("invalid extraction JSON: %w", err)
	}
	return &result, nil
}

// stripFencing removes markdown code fences from an LLM response, handling
// both leading and embedded fences.
func stripFencing(response string) string {
	response = strings.TrimSpace(response)
	fenceStart := strings.Index(response, "```")
	if fenceStart >= 0 {
		afterFence := response[fenceStart:]
		firstNewline := strings.Index(afterFence, "\n")
		if firstNewline >= 0 {
			afterFence = afterFence[firstNewline+1:]
		}
		lastFence := strings.LastIndex(afterFence, "\n```")
		if lastFence >= 0 {
			response = afterFence[:lastFence]
		} else {
			response = afterFence
		}
	}
	return response
}

// ReadFileFromRepo reads a file from HEAD in a git repo (works with bare repos).
func ReadFileFromRepo(ctx context.Context, repoDir, relPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "show", "HEAD:"+relPath)
	cmd.Dir = repoDir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git show HEAD:%s failed: %w", relPath, err)
	}
	return string(output), nil
}
