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

const SchemaPassE = "precog-pass-e"

func init() {
	schema.Register(SchemaPassE, PassEResponse{})
}

// PassEResponse is the response for Pass E (Architectural Drift).
type PassEResponse struct {
	DriftFindings  []DriftFinding `json:"drift_findings"`
	Confidence     string         `json:"confidence"`
	ConfidenceNote string         `json:"confidence_note"`
}

// DriftFinding represents architectural drift.
type DriftFinding struct {
	ID                   string   `json:"id"`
	DeclaredBoundary     string   `json:"declared_boundary"`
	ObservedBehavior     string   `json:"observed_behavior"`
	ImpactOnParallelWork string   `json:"impact_on_parallel_work"`
	Anchors              []string `json:"anchors"`
}

const passEPrompt = `You are detecting architectural drift - where reality diverges from intent.

Declared structure (from folders):
%s

Observed couplings:
%s

Cross-boundary dependencies:
%s

For each drift finding:
1. What was the declared boundary?
2. What does observed behavior show?
3. How does this impact parallel development?

Respond in JSON with these fields:
- drift_findings: array of {id, declared_boundary, observed_behavior, impact_on_parallel_work, anchors[]}
- confidence: "high", "medium", or "low"
- confidence_note: brief explanation`

func (a *Analyzer) runPassE(ctx context.Context, rcm *contracts.RCM) error {
	// Build declared structure from folder names
	folders := make(map[string]bool)
	for _, cap := range rcm.Capabilities {
		for _, mod := range cap.Anchors.Modules {
			folders[mod] = true
		}
	}
	var declaredStr strings.Builder
	for folder := range folders {
		declaredStr.WriteString(fmt.Sprintf("- %s\n", folder))
	}

	// Observed couplings
	var couplingsStr strings.Builder
	for _, c := range rcm.Couplings {
		couplingsStr.WriteString(fmt.Sprintf("- %s <-> %s (%s)\n", c.CapabilityA, c.CapabilityB, c.Strength))
	}

	// Cross-boundary from contracts
	var crossBoundaryStr strings.Builder
	for _, contract := range rcm.Contracts {
		if len(contract.UsedByCapabilities) > 1 {
			crossBoundaryStr.WriteString(fmt.Sprintf("- %s used by %v\n", contract.Name, contract.UsedByCapabilities))
		}
	}

	prompt := fmt.Sprintf(passEPrompt, declaredStr.String(), couplingsStr.String(), crossBoundaryStr.String())

	response, err := a.runLLM(ctx, prompt, SchemaPassE)
	if err != nil {
		return fmt.Errorf("LLM call failed: %w", err)
	}

	var passResp PassEResponse
	if err := json.Unmarshal([]byte(oneshot.NormalizeJSONPayload(response)), &passResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Map to RCM
	for _, drift := range passResp.DriftFindings {
		rcm.DriftFindings = append(rcm.DriftFindings, contracts.RCMDriftFinding{
			ID:                   drift.ID,
			DeclaredBoundary:     drift.DeclaredBoundary,
			ObservedBehavior:     drift.ObservedBehavior,
			ImpactOnParallelWork: drift.ImpactOnParallelWork,
			Anchors:              drift.Anchors,
		})
	}

	rcm.Confidence.Drift = passResp.Confidence
	rcm.Confidence.DriftNotes = passResp.ConfidenceNote

	return nil
}
