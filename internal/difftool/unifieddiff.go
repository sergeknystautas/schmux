package difftool

import (
	"bytes"
	"fmt"
	"strings"
)

// UnifiedDiff produces a unified diff string from two byte slices.
// Returns empty string if content is identical.
// Returns a "Binary files differ" message for binary content.
func UnifiedDiff(filename string, oldContent, newContent []byte) string {
	if bytes.Equal(oldContent, newContent) {
		return ""
	}

	// Detect binary content (contains null bytes)
	if isBinaryContent(oldContent) || isBinaryContent(newContent) {
		return fmt.Sprintf("Binary file %s has changed", filename)
	}

	oldLines := splitLines(string(oldContent))
	newLines := splitLines(string(newContent))

	return formatUnifiedDiff(filename, oldLines, newLines)
}

func isBinaryContent(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// Remove trailing empty string from final newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func formatUnifiedDiff(filename string, oldLines, newLines []string) string {
	lcs := computeLCS(oldLines, newLines)

	var hunks []hunk
	var current *hunk
	oi, ni, li := 0, 0, 0

	for oi < len(oldLines) || ni < len(newLines) {
		if li < len(lcs) && oi < len(oldLines) && ni < len(newLines) &&
			oldLines[oi] == lcs[li] && newLines[ni] == lcs[li] {
			if current != nil {
				current.lines = append(current.lines, " "+oldLines[oi])
				current.oldCount++
				current.newCount++
			}
			oi++
			ni++
			li++
		} else if li < len(lcs) && oi < len(oldLines) && oldLines[oi] != lcs[li] {
			if current == nil {
				current = &hunk{oldStart: oi + 1, newStart: ni + 1}
			}
			current.lines = append(current.lines, "-"+oldLines[oi])
			current.oldCount++
			oi++
		} else if oi >= len(oldLines) || (li < len(lcs) && ni < len(newLines) && newLines[ni] != lcs[li]) {
			if current == nil {
				current = &hunk{oldStart: oi + 1, newStart: ni + 1}
			}
			current.lines = append(current.lines, "+"+newLines[ni])
			current.newCount++
			ni++
		} else if li >= len(lcs) {
			if current == nil {
				current = &hunk{oldStart: oi + 1, newStart: ni + 1}
			}
			if oi < len(oldLines) {
				current.lines = append(current.lines, "-"+oldLines[oi])
				current.oldCount++
				oi++
			}
			if ni < len(newLines) {
				current.lines = append(current.lines, "+"+newLines[ni])
				current.newCount++
				ni++
			}
		}

		if current != nil && countTrailingContext(current.lines) >= 3 {
			hunks = append(hunks, *current)
			current = nil
		}
	}
	if current != nil {
		hunks = append(hunks, *current)
	}

	if len(hunks) == 0 {
		return ""
	}

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("--- a/%s\n", filename))
	buf.WriteString(fmt.Sprintf("+++ b/%s\n", filename))
	for _, h := range hunks {
		buf.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", h.oldStart, h.oldCount, h.newStart, h.newCount))
		for _, line := range h.lines {
			buf.WriteString(line)
			buf.WriteString("\n")
		}
	}
	return buf.String()
}

type hunk struct {
	oldStart, oldCount int
	newStart, newCount int
	lines              []string
}

func countTrailingContext(lines []string) int {
	count := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], " ") {
			count++
		} else {
			break
		}
	}
	return count
}

func computeLCS(a, b []string) []string {
	// Guard against excessive memory usage: O(m*n) matrix
	// For two 10K-line files this would be ~800MB
	const maxCells = 10_000_000 // ~80MB limit
	if int64(len(a)+1)*int64(len(b)+1) > maxCells {
		// Fall back to returning empty LCS (full diff)
		return nil
	}

	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] > dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	result := make([]string, 0, dp[m][n])
	i, j := m, n
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			result = append(result, a[i-1])
			i--
			j--
		} else if dp[i-1][j] > dp[i][j-1] {
			i--
		} else {
			j--
		}
	}
	for l, r := 0, len(result)-1; l < r; l, r = l+1, r-1 {
		result[l], result[r] = result[r], result[l]
	}
	return result
}
