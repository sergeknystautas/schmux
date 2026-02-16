package precog

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/lore"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schema"
)

const SchemaPassC = "precog-pass-c"

func init() {
	schema.Register(SchemaPassC, PassCResponse{})
}

// PassCResponse is the response for Pass C (Coordination Surfaces).
type PassCResponse struct {
	Contracts      []Contract `json:"contracts"`
	Confidence     string     `json:"confidence"`
	ConfidenceNote string     `json:"confidence_note"`
}

// Contract represents a coordination surface.
type Contract struct {
	ID                 string   `json:"id"`
	Type               string   `json:"type"`
	Name               string   `json:"name"`
	Anchor             string   `json:"anchor"`
	UsedByCapabilities []string `json:"used_by_capabilities"`
	FanIn              int      `json:"fan_in"`
	Notes              string   `json:"notes,omitempty"`
}

const passCPrompt = `You are identifying coordination surfaces in a codebase - the contracts that force teams to coordinate.

Here are the most-imported files (high fan-in):
%s

Here are shared types used across multiple packages:
%s

Here are the capabilities identified:
%s

For each coordination surface, identify:
1. What type is it? (api_schema, shared_model, db_schema, event, config, auth_policy, library)
2. Which capabilities use it?
3. What is its coordination pressure? (how many things depend on it)

Respond in JSON with these fields:
- contracts: array of {id, type, name, anchor, used_by_capabilities[], fan_in, notes}
- confidence: "high", "medium", or "low"
- confidence_note: brief explanation`

func (a *Analyzer) runPassC(ctx context.Context, rcm *contracts.RCM, files []string) error {
	// TODO: Fan-in analysis is Go-only. Should handle TypeScript imports too.

	// Compute fan-in for Go imports
	var importInfos []ImportInfo
	for _, file := range files {
		if strings.HasSuffix(file, ".go") && !strings.HasSuffix(file, "_test.go") {
			content, err := lore.ReadFileFromRepo(ctx, a.bareDir, file)
			if err == nil {
				imports := ExtractGoImports(content)
				importInfos = append(importInfos, ImportInfo{File: file, Imports: imports})
			}
		}
	}

	// Find local import prefix from go.mod
	localPrefix := ""
	gomod, err := lore.ReadFileFromRepo(ctx, a.bareDir, "go.mod")
	if err == nil {
		for _, line := range strings.Split(gomod, "\n") {
			if strings.HasPrefix(line, "module ") {
				localPrefix = strings.TrimPrefix(line, "module ")
				localPrefix = strings.TrimSpace(localPrefix)
				break
			}
		}
	}

	fanIn := ComputeFanIn(importInfos, localPrefix)

	// Sort by fan-in
	type fanInEntry struct {
		path  string
		count int
	}
	var entries []fanInEntry
	for path, count := range fanIn {
		entries = append(entries, fanInEntry{path, count})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].count > entries[j].count
	})

	var highFanInStr strings.Builder
	for i, e := range entries {
		if i >= 20 {
			break
		}
		highFanInStr.WriteString(fmt.Sprintf("%s: %d imports\n", e.path, e.count))
	}

	// Capabilities from previous pass
	var capsStr strings.Builder
	for _, cap := range rcm.Capabilities {
		capsStr.WriteString(fmt.Sprintf("- %s: %s\n", cap.ID, cap.Description))
	}

	prompt := fmt.Sprintf(passCPrompt, highFanInStr.String(), "Shared types analysis pending", capsStr.String())

	response, err := a.runLLM(ctx, prompt, SchemaPassC)
	if err != nil {
		return fmt.Errorf("LLM call failed: %w", err)
	}

	var passResp PassCResponse
	if err := json.Unmarshal([]byte(oneshot.NormalizeJSONPayload(response)), &passResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Map to RCM
	for _, contract := range passResp.Contracts {
		rcm.Contracts = append(rcm.Contracts, contracts.RCMContract{
			ID:                 contract.ID,
			Type:               contract.Type,
			Name:               contract.Name,
			Anchor:             contract.Anchor,
			UsedByCapabilities: contract.UsedByCapabilities,
			FanIn:              contract.FanIn,
			Notes:              contract.Notes,
		})
	}

	rcm.Confidence.Contracts = passResp.Confidence
	rcm.Confidence.ContractsNotes = passResp.ConfidenceNote

	return nil
}
