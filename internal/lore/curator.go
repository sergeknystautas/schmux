package lore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Curator reads raw lore entries and instruction files, calls an LLM to produce a merge proposal.
type Curator struct {
	InstructionFiles []string
	Executor         func(ctx context.Context, prompt string, timeout time.Duration) (string, error)
}

// CuratorResponse is the expected JSON output from the curator LLM.
type CuratorResponse struct {
	ProposedFiles    map[string]string `json:"proposed_files"`
	DiffSummary      string            `json:"diff_summary"`
	EntriesUsed      []string          `json:"entries_used"`
	EntriesDiscarded map[string]string `json:"entries_discarded"`
}

// Curate reads raw entries and instruction files, calls the LLM, and returns a Proposal.
// Returns nil if there are no raw entries to curate.
func (c *Curator) Curate(ctx context.Context, repoName, repoDir, lorePath string) (*Proposal, error) {
	// Read raw entries
	entries, err := ReadEntries(lorePath, FilterRaw())
	if err != nil {
		return nil, fmt.Errorf("failed to read lore entries: %w", err)
	}
	if len(entries) == 0 {
		return nil, nil
	}

	// Read instruction files that exist
	instrFiles := make(map[string]string)
	fileHashes := make(map[string]string)
	for _, name := range c.InstructionFiles {
		fullPath := filepath.Join(repoDir, name)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to read %s: %w", name, err)
		}
		instrFiles[name] = string(content)
		hash := sha256.Sum256(content)
		fileHashes[name] = "sha256:" + hex.EncodeToString(hash[:])
	}

	if len(instrFiles) == 0 {
		return nil, fmt.Errorf("no instruction files found in %s", repoDir)
	}

	// Build prompt and call LLM
	prompt := BuildCuratorPrompt(instrFiles, entries)
	response, err := c.Executor(ctx, prompt, 120*time.Second)
	if err != nil {
		return nil, fmt.Errorf("curator LLM call failed: %w", err)
	}

	result, err := ParseCuratorResponse(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse curator response: %w", err)
	}

	// Collect unique source workspaces
	sourceSet := make(map[string]bool)
	for _, e := range entries {
		if e.Workspace != "" {
			sourceSet[e.Workspace] = true
		}
	}
	var sources []string
	for ws := range sourceSet {
		sources = append(sources, ws)
	}

	now := time.Now().UTC()
	proposal := &Proposal{
		ID:               fmt.Sprintf("prop-%s", now.Format("20060102-150405")),
		Repo:             repoName,
		CreatedAt:        now,
		Status:           ProposalPending,
		SourceCount:      len(entries),
		Sources:          sources,
		FileHashes:       fileHashes,
		ProposedFiles:    result.ProposedFiles,
		DiffSummary:      result.DiffSummary,
		EntriesUsed:      result.EntriesUsed,
		EntriesDiscarded: result.EntriesDiscarded,
	}

	return proposal, nil
}

// BuildCuratorPrompt constructs the LLM prompt for curating lore into instruction files.
func BuildCuratorPrompt(instrFiles map[string]string, entries []Entry) string {
	var sb strings.Builder
	sb.WriteString(`You are a curator for a software project's agent instruction files.

You will receive:
1. A list of raw lore entries discovered by AI agents working on this project
2. The current content of all instruction files

Your job is to produce a merge proposal — changes to the instruction files that
incorporate the new lore.

Rules:
- DEDUPLICATE: Collapse similar entries from different agents into one
- FILTER: Discard entries already covered by existing content
- ROUTE: Decide which file(s) each entry belongs in:
  - Universal lore (applies to any agent) → add to ALL instruction files, adapted to each file's style
  - Agent-specific lore → add to that agent's file only
- CATEGORIZE: Place each entry under the appropriate existing section, or propose a new section if none fits
- PRESERVE VOICE: Match the tone, formatting, and style of each file
- NEVER REMOVE existing content — only add or refine
- Output ONLY valid JSON matching the schema below, no markdown fencing

Output schema:
{
  "proposed_files": {"<filename>": "<full proposed content>", ...},
  "diff_summary": "<one-line summary of changes>",
  "entries_used": ["<entry text that was incorporated>", ...],
  "entries_discarded": {"<entry text>": "<reason for discarding>", ...}
}

INSTRUCTION FILES:
`)
	for name, content := range instrFiles {
		fmt.Fprintf(&sb, "\n=== %s ===\n%s\n", name, content)
	}

	sb.WriteString("\nRAW LORE:\n")
	for _, e := range entries {
		fmt.Fprintf(&sb, "- [%s] [%s] [%s] %s\n", e.Agent, e.Type, e.Workspace, e.Text)
	}

	return sb.String()
}

// ParseCuratorResponse parses the LLM JSON response into a CuratorResponse.
func ParseCuratorResponse(response string) (*CuratorResponse, error) {
	// Strip markdown fencing if present
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "```") {
		lines := strings.SplitN(response, "\n", 2)
		if len(lines) > 1 {
			response = lines[1]
		}
		if idx := strings.LastIndex(response, "```"); idx >= 0 {
			response = response[:idx]
		}
		response = strings.TrimSpace(response)
	}

	var result CuratorResponse
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, fmt.Errorf("invalid curator JSON: %w", err)
	}
	return &result, nil
}
