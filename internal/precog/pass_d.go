package precog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schema"
)

const SchemaPassD = "precog-pass-d"

func init() {
	schema.Register(SchemaPassD, PassDResponse{})
}

// PassDResponse is the response for Pass D (Reality Map).
type PassDResponse struct {
	Clusters       []Cluster  `json:"clusters"`
	Couplings      []Coupling `json:"couplings"`
	Confidence     string     `json:"confidence"`
	ConfidenceNote string     `json:"confidence_note"`
}

// Cluster represents a group of coupled files.
type Cluster struct {
	ID                   string   `json:"id"`
	Type                 string   `json:"type"`
	Name                 string   `json:"name"`
	Members              []string `json:"members"`
	CapabilitiesInvolved []string `json:"capabilities_involved"`
}

// Coupling represents coupling between capabilities.
type Coupling struct {
	CapabilityA string   `json:"capability_a"`
	CapabilityB string   `json:"capability_b"`
	Strength    string   `json:"strength"`
	Evidence    []string `json:"evidence"`
}

const passDPrompt = `You are analyzing the actual coupling in a codebase.

Here are file clusters based on import relationships:
%s

Here are file clusters based on co-change history (files that change together):
%s

Here are the capabilities:
%s

Identify:
1. Clusters that span multiple capabilities (coordination knots)
2. Hidden couplings not obvious from folder structure
3. Cyclic dependencies

Respond in JSON with these fields:
- clusters: array of {id, type, name, members[], capabilities_involved[]}
- couplings: array of {capability_a, capability_b, strength, evidence[]}
- confidence: "high", "medium", or "low"
- confidence_note: brief explanation`

func (a *Analyzer) runPassD(ctx context.Context, rcm *contracts.RCM, files []string) error {
	// Get co-change data
	commits, err := GetRecentCommits(ctx, a.bareDir, 3)
	if err != nil {
		return fmt.Errorf("failed to get commits: %w", err)
	}

	coChangeMatrix := NewCoChangeMatrix(commits)
	topCoChanges := coChangeMatrix.TopCoChanges(20)

	var coChangeStr strings.Builder
	for _, pair := range topCoChanges {
		coChangeStr.WriteString(fmt.Sprintf("%s <-> %s: %s times\n", pair[0], pair[1], pair[2]))
	}

	// Package clustering from imports (simplified)
	packages := GetPackages(files)
	var structuralStr strings.Builder
	for _, pkg := range packages {
		structuralStr.WriteString(fmt.Sprintf("- %s\n", pkg))
	}

	// Capabilities
	var capsStr strings.Builder
	for _, cap := range rcm.Capabilities {
		capsStr.WriteString(fmt.Sprintf("- %s: %s\n", cap.ID, cap.Name))
	}

	prompt := fmt.Sprintf(passDPrompt, structuralStr.String(), coChangeStr.String(), capsStr.String())

	response, err := a.runLLM(ctx, prompt, SchemaPassD)
	if err != nil {
		return fmt.Errorf("LLM call failed: %w", err)
	}

	var passResp PassDResponse
	if err := json.Unmarshal([]byte(oneshot.NormalizeJSONPayload(response)), &passResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Map to RCM
	for _, cluster := range passResp.Clusters {
		rcm.Clusters = append(rcm.Clusters, contracts.RCMCluster{
			ID:                   cluster.ID,
			Type:                 cluster.Type,
			Name:                 cluster.Name,
			Members:              cluster.Members,
			CapabilitiesInvolved: cluster.CapabilitiesInvolved,
		})
	}
	for _, coupling := range passResp.Couplings {
		rcm.Couplings = append(rcm.Couplings, contracts.RCMCoupling{
			CapabilityA: coupling.CapabilityA,
			CapabilityB: coupling.CapabilityB,
			Strength:    coupling.Strength,
			Evidence:    coupling.Evidence,
		})
	}

	rcm.Confidence.Clusters = passResp.Confidence
	rcm.Confidence.ClustersNotes = passResp.ConfidenceNote

	return nil
}
