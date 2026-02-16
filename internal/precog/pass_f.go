package precog

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schema"
)

const SchemaPassF = "precog-pass-f"

func init() {
	schema.Register(SchemaPassF, PassFResponse{})
}

// PassFResponse is the response for Pass F (Change Gravity & Trajectory).
type PassFResponse struct {
	Gravity        []Gravity    `json:"gravity"`
	Trajectory     []Trajectory `json:"trajectory"`
	Confidence     string       `json:"confidence"`
	ConfidenceNote string       `json:"confidence_note"`
}

// Gravity represents a region attracting work.
type Gravity struct {
	Region      string   `json:"region"`
	Type        string   `json:"type"`
	Signals     []string `json:"signals"`
	Implication string   `json:"implication"`
}

// Trajectory represents evolution direction.
type Trajectory struct {
	Direction  string   `json:"direction"`
	Evidence   []string `json:"evidence"`
	Confidence string   `json:"confidence"`
}

const passFPrompt = `You are predicting where future development effort will concentrate.

Churn by capability (commits in last 3 months):
%s

Churn by directory:
%s

Recently added code patterns:
%s

For each gravity zone:
1. What region is attracting work?
2. What signals indicate this?
3. What does this imply for coordination?

For trajectory:
1. What direction is the system evolving?
2. Where will parallel work likely collide?

Respond in JSON with these fields:
- gravity: array of {region, type, signals[], implication}
- trajectory: array of {direction, evidence[], confidence}
- confidence: "high", "medium", or "low"
- confidence_note: brief explanation`

func (a *Analyzer) runPassF(ctx context.Context, rcm *contracts.RCM) error {
	// Get recent commits for churn analysis
	commits, err := GetRecentCommits(ctx, a.bareDir, 3)
	if err != nil {
		return fmt.Errorf("failed to get commits: %w", err)
	}

	// Churn by directory
	churnByDir := ChurnByDirectory(commits)
	type churnEntry struct {
		dir   string
		count int
	}
	var churnEntries []churnEntry
	for dir, count := range churnByDir {
		churnEntries = append(churnEntries, churnEntry{dir, count})
	}
	sort.Slice(churnEntries, func(i, j int) bool {
		return churnEntries[i].count > churnEntries[j].count
	})

	var churnStr strings.Builder
	for i, e := range churnEntries {
		if i >= 15 {
			break
		}
		churnStr.WriteString(fmt.Sprintf("- %s: %d commits\n", e.dir, e.count))
	}

	// Map capabilities to their modules for capability churn
	capChurn := make(map[string]int)
	for _, cap := range rcm.Capabilities {
		for _, mod := range cap.Anchors.Modules {
			if count, ok := churnByDir[strings.TrimSuffix(mod, "/")]; ok {
				capChurn[cap.ID] += count
			}
		}
	}

	var capChurnStr strings.Builder
	for capID, count := range capChurn {
		capChurnStr.WriteString(fmt.Sprintf("- %s: %d commits\n", capID, count))
	}

	prompt := fmt.Sprintf(passFPrompt, capChurnStr.String(), churnStr.String(), "Recent additions analysis pending")

	response, err := a.runLLM(ctx, prompt, SchemaPassF)
	if err != nil {
		return fmt.Errorf("LLM call failed: %w", err)
	}

	var passResp PassFResponse
	if err := json.Unmarshal([]byte(oneshot.NormalizeJSONPayload(response)), &passResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Map to RCM
	for _, g := range passResp.Gravity {
		rcm.Gravity = append(rcm.Gravity, contracts.RCMGravity{
			Region:      g.Region,
			Type:        g.Type,
			Signals:     g.Signals,
			Implication: g.Implication,
		})
	}
	for _, t := range passResp.Trajectory {
		rcm.Trajectory = append(rcm.Trajectory, contracts.RCMTrajectory{
			Direction:  t.Direction,
			Evidence:   t.Evidence,
			Confidence: t.Confidence,
		})
	}

	rcm.Confidence.Trajectory = passResp.Confidence
	rcm.Confidence.TrajectoryNotes = passResp.ConfidenceNote

	return nil
}
