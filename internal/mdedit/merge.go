// Package mdedit serves the dashboard's Markdown editor: one Document per open
// file, merging browser saves onto whatever agents have written to disk.
package mdedit

import "github.com/sergi/go-diff/diffmatchpatch"

// Merge applies the change from base to draft onto disk and returns the result
// plus the number of hunks that found no context to attach to. It never
// reports a conflict: overlapping edits land at diff-match-patch's best fuzzy
// match, and a hunk with no match within the default MatchThreshold is
// dropped. When the browser changed nothing, disk is returned as is.
func Merge(base, draft, disk string) (string, int) {
	if base == draft {
		return disk, 0
	}
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(base, draft, false)
	patches := dmp.PatchMake(base, diffs)
	out, applied := dmp.PatchApply(patches, disk)
	dropped := 0
	for _, ok := range applied {
		if !ok {
			dropped++
		}
	}
	return out, dropped
}
