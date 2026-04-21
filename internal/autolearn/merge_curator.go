//go:build !noautolearn

package autolearn

import (
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/internal/schema"
)

func init() {
	schema.Register(schema.LabelAutolearnMerge, MergeCuratorResponse{})
}

// MergeCuratorResponse is the parsed JSON response from the merge curator LLM.
// Replaces the former XML-tagged MergeResponse; aligns with the oneshot
// schema-everywhere rule.
type MergeCuratorResponse struct {
	Summary       string   `json:"summary" required:"true"`
	MergedContent string   `json:"merged_content" required:"true"`
	_             struct{} `additionalProperties:"false"`
}

// BuildMergePrompt constructs the LLM prompt for merging approved rules into
// an existing instruction file. The LLM receives the current file content and
// the list of learnings (filtered to rules by the caller) to merge, and returns
// a JSON object with the updated file content and a one-line summary.
func BuildMergePrompt(currentContent string, learnings []Learning) string {
	var sb strings.Builder
	sb.WriteString(`You are a merge curator for a software project's agent instruction files.

You will receive:
1. The current content of an instruction file
2. A list of approved rules to merge into the file

Your job is to produce the updated file content with the rules integrated.

Rules:
- PRESERVE VOICE: Match tone, formatting, and style of the existing file
- CATEGORIZE: Place rules under appropriate existing sections, or create new sections if needed
- NEVER REMOVE existing content — only add or refine
- DEDUPLICATE: If a rule is already covered by existing content, skip it
- NATURAL: Rules should read as natural parts of the document, not appended items

Output a JSON object with exactly two fields:
- "summary": one-line string describing what changed
- "merged_content": the full updated file content as a string (newlines, backticks, and code blocks are allowed inside the string — the JSON layer handles escaping)

Do not include any fencing or commentary outside the JSON object.

CURRENT FILE CONTENT:
`)
	sb.WriteString(currentContent)
	sb.WriteString("\n\nRULES TO MERGE:\n")
	for i, l := range learnings {
		fmt.Fprintf(&sb, "%d. [%s] %s\n", i+1, l.Category, l.Title)
	}
	return sb.String()
}
