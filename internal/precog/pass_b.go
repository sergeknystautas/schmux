package precog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/lore"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schema"
)

const SchemaPassB = "precog-pass-b"

func init() {
	schema.Register(SchemaPassB, PassBResponse{})
}

// PassBResponse is the response for Pass B (Capability Mining).
type PassBResponse struct {
	Capabilities   []Capability `json:"capabilities"`
	Confidence     string       `json:"confidence"`
	ConfidenceNote string       `json:"confidence_note"`
}

// Capability represents a system capability.
type Capability struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Keywords    []string          `json:"keywords"`
	Anchors     CapabilityAnchors `json:"anchors"`
}

// CapabilityAnchors holds anchors for a capability.
type CapabilityAnchors struct {
	Entrypoints []string `json:"entrypoints"`
	Modules     []string `json:"modules"`
	Schema      []string `json:"schema"`
	Symbols     []string `json:"symbols"`
}

const passBPrompt = `You are identifying the capabilities of a software system.

Here are the packages and their structure:
%s

Here are the API routes/handlers (if detected):
%s

Here are key type definitions:
%s

Identify 8-20 capabilities. Each capability is a coherent domain function the system provides.

Avoid generic buckets like "utils", "common", "shared", "misc".

Respond in JSON with these fields:
- capabilities: array of {id, name, description, keywords[], anchors{entrypoints[], modules[], schema[], symbols[]}}
- confidence: "high", "medium", or "low"
- confidence_note: brief explanation`

func (a *Analyzer) runPassB(ctx context.Context, rcm *contracts.RCM, files []string) error {
	// TODO: This pass is Go-centric. Should use rcm.RepoSummary.PrimaryLanguages to:
	// - For Go: extract packages, find handlers, parse types with go/parser
	// - For TypeScript: find src/, parse package.json, look for routes in express/next patterns
	// - For Python: find __init__.py patterns, Flask/Django routes
	// Currently we detect languages but then ignore them and assume Go.

	// Get package structure
	packages := GetPackages(files)
	packagesStr := strings.Join(packages, "\n")

	// Try to find route patterns
	routesStr := "No routes detected"
	// Look for handler files
	for _, file := range files {
		if strings.Contains(file, "handler") || strings.Contains(file, "routes") {
			content, err := lore.ReadFileFromRepo(ctx, a.bareDir, file)
			if err == nil && len(content) < 5000 {
				routesStr = fmt.Sprintf("--- %s ---\n%s", file, content)
				break
			}
		}
	}

	// Get key types
	typesStr := "Type definitions from key files"

	prompt := fmt.Sprintf(passBPrompt, packagesStr, routesStr, typesStr)

	response, err := a.runLLM(ctx, prompt, SchemaPassB)
	if err != nil {
		return fmt.Errorf("LLM call failed: %w", err)
	}

	var passResp PassBResponse
	if err := json.Unmarshal([]byte(oneshot.NormalizeJSONPayload(response)), &passResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Map to RCM
	for _, cap := range passResp.Capabilities {
		rcm.Capabilities = append(rcm.Capabilities, contracts.RCMCapability{
			ID:          cap.ID,
			Name:        cap.Name,
			Description: cap.Description,
			Keywords:    cap.Keywords,
			Anchors: contracts.RCMCapabilityAnchors{
				Entrypoints: cap.Anchors.Entrypoints,
				Modules:     cap.Anchors.Modules,
				Schema:      cap.Anchors.Schema,
				Symbols:     cap.Anchors.Symbols,
			},
		})
	}

	// Update confidence
	rcm.Confidence.Capabilities = passResp.Confidence
	rcm.Confidence.CapabilitiesNotes = passResp.ConfidenceNote

	return nil
}
