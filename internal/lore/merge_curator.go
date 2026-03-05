package lore

import (
	"fmt"
	"strings"
)

// MergeResponse holds the parsed output from the merge curator LLM.
type MergeResponse struct {
	MergedContent string
	Summary       string
}

// BuildMergePrompt constructs the LLM prompt for merging approved rules into
// an existing instruction file. The LLM receives the current file content and
// the list of rules to merge, and returns the updated file content.
//
// The output uses XML-style delimiters instead of JSON because the merged
// content is a full markdown file that may contain code blocks, backticks,
// and other characters that are difficult to escape correctly in JSON.
func BuildMergePrompt(currentContent string, rules []Rule) string {
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

Output format — use EXACTLY this structure with the XML tags on their own lines:

<SUMMARY>one-line summary of what changed</SUMMARY>
<MERGED>
full updated file content here
</MERGED>

CURRENT FILE CONTENT:
`)
	sb.WriteString(currentContent)
	sb.WriteString("\n\nRULES TO MERGE:\n")
	for i, r := range rules {
		fmt.Fprintf(&sb, "%d. [%s] %s\n", i+1, r.Category, r.Text)
	}
	return sb.String()
}

// ParseMergeResponse parses the LLM response using XML-style delimiters.
func ParseMergeResponse(response string) (*MergeResponse, error) {
	response = strings.TrimSpace(response)

	summary, err := extractTag(response, "SUMMARY")
	if err != nil {
		return nil, fmt.Errorf("missing SUMMARY tag: %w", err)
	}

	merged, err := extractTag(response, "MERGED")
	if err != nil {
		return nil, fmt.Errorf("missing MERGED tag: %w", err)
	}

	return &MergeResponse{
		MergedContent: merged,
		Summary:       strings.TrimSpace(summary),
	}, nil
}

// extractTag extracts content between <TAG> and </TAG> delimiters.
func extractTag(s, tag string) (string, error) {
	open := "<" + tag + ">"
	close := "</" + tag + ">"

	start := strings.Index(s, open)
	if start < 0 {
		return "", fmt.Errorf("opening <%s> not found", tag)
	}
	content := s[start+len(open):]

	end := strings.LastIndex(content, close)
	if end < 0 {
		return "", fmt.Errorf("closing </%s> not found", tag)
	}

	result := content[:end]
	// Trim a single leading newline if present (the tag format puts content on the next line)
	result = strings.TrimPrefix(result, "\n")
	// Trim a single trailing newline if present
	result = strings.TrimSuffix(result, "\n")
	return result, nil
}
