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

const SchemaPassA = "precog-pass-a"

func init() {
	schema.Register(SchemaPassA, PassAResponse{})
}

// PassAResponse is the response for Pass A (Inventory & Entry Points).
type PassAResponse struct {
	SystemType        string       `json:"system_type"`
	RuntimeComponents []Component  `json:"runtime_components"`
	Entrypoints       []Entrypoint `json:"entrypoints"`
	Confidence        string       `json:"confidence"`
	ConfidenceNote    string       `json:"confidence_note"`
}

// Component represents a runtime component.
type Component struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Anchors []string `json:"anchors"`
}

// Entrypoint represents an entry point.
type Entrypoint struct {
	Type   string `json:"type"`
	Anchor string `json:"anchor"`
	Notes  string `json:"notes,omitempty"`
}

const passAPrompt = `You are analyzing a software repository to understand its structure.

Here is the file tree:
%s

Here are the detected entry points:
%s

Here is the content of key files:
%s

Classify this system:
1. What type of system is this? (daemon, CLI tool, web app, library, etc.)
2. What are the runtime components? (services, workers, UIs, databases)
3. What are the entry points for each component?

Respond in JSON with these fields:
- system_type: string describing the system
- runtime_components: array of {name, type, anchors[]}
- entrypoints: array of {type, anchor, notes}
- confidence: "high", "medium", or "low"
- confidence_note: brief explanation`

func (a *Analyzer) runPassA(ctx context.Context, rcm *contracts.RCM, files []string) error {
	// Build file tree (truncated for prompt size)
	fileTree := buildFileTree(files, 200)

	// Find entry points
	entryPoints := FindEntryPoints(files)
	entryPointsStr := strings.Join(entryPoints, "\n")

	// Read key files (main.go, package.json, etc.)
	var keyFilesContent strings.Builder
	keyFiles := []string{"main.go", "cmd/schmux/main.go", "package.json", "go.mod", "README.md", "CLAUDE.md"}
	for _, kf := range keyFiles {
		content, err := lore.ReadFileFromRepo(ctx, a.bareDir, kf)
		if err == nil {
			// Truncate large files
			if len(content) > 2000 {
				content = content[:2000] + "\n... (truncated)"
			}
			keyFilesContent.WriteString(fmt.Sprintf("\n--- %s ---\n%s\n", kf, content))
		}
	}

	prompt := fmt.Sprintf(passAPrompt, fileTree, entryPointsStr, keyFilesContent.String())

	response, err := a.runLLM(ctx, prompt, SchemaPassA)
	if err != nil {
		return fmt.Errorf("LLM call failed: %w", err)
	}

	var passResp PassAResponse
	if err := json.Unmarshal([]byte(oneshot.NormalizeJSONPayload(response)), &passResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Map to RCM
	rcm.RepoSummary.SystemType = passResp.SystemType
	for _, comp := range passResp.RuntimeComponents {
		rcm.RuntimeComponents = append(rcm.RuntimeComponents, contracts.RCMComponent{
			Name:    comp.Name,
			Type:    comp.Type,
			Anchors: comp.Anchors,
		})
	}
	for _, ep := range passResp.Entrypoints {
		rcm.Entrypoints = append(rcm.Entrypoints, contracts.RCMEntrypoint{
			Type:   ep.Type,
			Anchor: ep.Anchor,
			Notes:  ep.Notes,
		})
	}

	return nil
}
