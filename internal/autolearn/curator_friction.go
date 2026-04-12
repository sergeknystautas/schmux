//go:build !noautolearn

package autolearn

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sergeknystautas/schmux/internal/schema"
)

func init() {
	schema.Register(schema.LabelAutolearnFriction, FrictionCuratorResponse{})
}

// FrictionCuratorResponse is the expected JSON output from the friction curator LLM.
type FrictionCuratorResponse struct {
	Learnings        []Learning `json:"learnings"`
	DiscardedEntries []string   `json:"discarded_entries"`
}

// BuildFrictionPrompt constructs the LLM prompt for extracting learnings from friction data.
// existingTitles lists learning titles already extracted in pending batches; the LLM is told
// not to re-extract semantically equivalent learnings.
// dismissedTitles lists learning titles the user previously rejected; the LLM is told not to
// re-propose them.
func BuildFrictionPrompt(entries []Entry, existingTitles []string, dismissedTitles []string) string {
	var sb strings.Builder
	sb.WriteString(`You are a learning extractor for a multi-agent software development environment.

You will receive failure records and friction reflections from agent work sessions.
Your job is to extract discrete, actionable learnings that prevent future agents from
repeating these mistakes.

Each learning has a "kind" — either "rule" or "skill":
- "rule": a concise imperative instruction (e.g., "Always use go run ./cmd/build-dashboard")
- "skill": a recurring multi-step procedure worth automating (e.g., "How to deploy a preview server")

You may output skills if you notice recurring patterns worth automating.

Guidelines for extraction:
- SYNTHESIZE: Turn failure patterns into actionable learnings
  (e.g., 5 "npm run build" failures -> "Always use go run ./cmd/build-dashboard")
- DEDUPLICATE: Multiple agents hitting the same wall -> one learning
- FILTER: Discard one-off failures that don't indicate systemic issues
  (e.g., a single typo in a file path is not learning-worthy)
- Write rule titles as imperatives: "Use X, not Y" / "Always run X before Y"
- Write skill titles as "How to" phrases: "How to set up the dev environment"
- Each learning must be self-contained — understandable without the original failure context
- Assign each learning a category (e.g., "build", "testing", "environment", "workflow")
- Assign a suggested_layer:
  - "repo_public": learnings specific to this repository, safe to commit (e.g., build commands, project structure)
  - "repo_private": learnings specific to this repository but private (e.g., internal tooling, personal preferences)
  - "cross_repo_private": learnings that apply across all repositories (e.g., agent behavior, environment setup)
- Output ONLY valid JSON matching the schema below, no markdown fencing

Output schema:
{
  "learnings": [
    {
      "kind": "rule",
      "title": "Always use go run ./cmd/build-dashboard instead of npm run build",
      "category": "build",
      "suggested_layer": "repo_public",
      "sources": [
        {
          "type": "failure|reflection|friction",
          "text": "reflection or friction text (omit for failures)",
          "input_summary": "what was attempted (for failures only)",
          "error_summary": "what went wrong (for failures only)",
          "tool": "tool name if applicable (omit if none)"
        }
      ],
      "rule": {}
    },
    {
      "kind": "skill",
      "title": "How to set up the dev environment",
      "category": "environment",
      "suggested_layer": "repo_public",
      "sources": [
        {
          "type": "friction",
          "text": "spent 20 minutes figuring out dev setup"
        }
      ],
      "skill": {
        "triggers": ["setting up dev environment", "first time setup"],
        "procedure": "Step-by-step procedure here"
      }
    }
  ],
  "discarded_entries": ["<timestamp or entry key of discarded entries>"]
}

`)

	// Include existing pending titles so the LLM avoids re-extracting them
	if len(existingTitles) > 0 {
		sb.WriteString("ALREADY EXTRACTED LEARNINGS (pending review — do NOT re-extract these or semantically equivalent learnings):\n")
		for _, t := range existingTitles {
			fmt.Fprintf(&sb, "- %s\n", t)
		}
		sb.WriteString("\n")
	}

	// Include dismissed titles so the LLM avoids re-proposing them
	if len(dismissedTitles) > 0 {
		sb.WriteString("PREVIOUSLY REJECTED LEARNINGS (user dismissed these — do NOT re-propose them or semantically equivalent learnings):\n")
		for _, t := range dismissedTitles {
			fmt.Fprintf(&sb, "- %s\n", t)
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
			fmt.Fprintf(&sb, "- [%s] [%s] [%s] [%s] command: %q -> error: %q\n",
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

// ParseFrictionResponse parses the LLM JSON response into a FrictionCuratorResponse.
func ParseFrictionResponse(response string) (*FrictionCuratorResponse, error) {
	response = strings.TrimSpace(response)
	// Try parsing directly first — avoids corrupting JSON that contains
	// backticks in string values (e.g., code snippets in sources).
	var result FrictionCuratorResponse
	if err := json.Unmarshal([]byte(response), &result); err == nil {
		return &result, nil
	}
	// Fallback: strip markdown code fences and retry.
	stripped := stripFencing(response)
	if err := json.Unmarshal([]byte(stripped), &result); err == nil {
		return &result, nil
	}
	// Fallback: extract outermost JSON object from prose-wrapped response.
	if start := strings.Index(response, "{"); start >= 0 {
		if end := strings.LastIndex(response, "}"); end > start {
			if err := json.Unmarshal([]byte(response[start:end+1]), &result); err == nil {
				return &result, nil
			}
		}
	}
	return nil, fmt.Errorf("invalid friction JSON: no valid JSON object found in response")
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
