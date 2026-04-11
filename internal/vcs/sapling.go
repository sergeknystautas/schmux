package vcs

import (
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

// parseRangeToRevset converts git-style range notation "A..B" to Sapling revset
// operands (exclude, include). Returns ("", rangeSpec) if not in A..B format.
// Maps "HEAD" to "." (Sapling's working copy parent, equivalent to git HEAD).
func parseRangeToRevset(rangeSpec string) (exclude, include string) {
	parts := strings.Split(rangeSpec, "..")
	if len(parts) != 2 {
		return "", rangeSpec
	}
	exclude, include = parts[0], parts[1]
	if exclude == "HEAD" {
		exclude = "."
	}
	return exclude, include
}

// SaplingCommandBuilder implements CommandBuilder for Sapling (sl).
// All sl commands that produce output use --pager never to prevent
// the pager from blocking when run non-interactively via RunCommand.
type SaplingCommandBuilder struct{}

func (s *SaplingCommandBuilder) DiffNumstat() string {
	// Sapling doesn't support --numstat. Use sl status to get the file list
	// and format as numstat-compatible output (0\t0\tfilename). The actual
	// line counts are unknown but buildDiffResponse fetches content anyway.
	return "sl status --no-status -mad | while IFS= read -r f; do printf '0\\t0\\t%s\\n' \"$f\"; done"
}

func (s *SaplingCommandBuilder) ShowFile(path, revision string) string {
	// In Sapling, "." is the working copy parent — equivalent to git's HEAD.
	slRev := revision
	if revision == "HEAD" {
		slRev = "."
	}
	return fmt.Sprintf("sl cat --pager never -r %s %s | head -2000", shellutil.Quote(slRev), shellutil.Quote(path))
}

func (s *SaplingCommandBuilder) FileContent(path string) string {
	return fmt.Sprintf("head -2000 %s", shellutil.Quote(path))
}

func (s *SaplingCommandBuilder) UntrackedFiles() string {
	return "sl status --unknown --no-status"
}

func (s *SaplingCommandBuilder) Log(refs []string, maxCount int) string {
	// Sapling log with parseable template.
	// Use {p1node} {p2node} instead of {parents} — the {parents} keyword outputs
	// "rev:shorthash" format, not full hex hashes like git's %P.
	tmpl := "'{node}|{short(node)}|{desc|firstline}|{author|user}|{date|isodate}|{p1node} {p2node}\\n'"

	// Fast path: single-HEAD log. `sl log --limit N` without a revset walks
	// backwards from `.` efficiently — O(N), not O(total_commits).
	// The slow `last(ancestors(.), N)` revset computes the full ancestor set
	// first, which is catastrophic on large monorepos (fbsource, etc.).
	if len(refs) <= 1 && (len(refs) == 0 || refs[0] == "HEAD") {
		return fmt.Sprintf("sl log --pager never -T %s --limit %d", tmpl, maxCount)
	}

	// Multi-ref or non-HEAD: need a revset to combine ancestry.
	// Use last(revset, N) instead of --limit N because ancestors() returns
	// commits in ascending order — --limit would take the oldest N.
	var revset string
	if len(refs) > 1 {
		revset = fmt.Sprintf("ancestors(%s)", strings.Join(refs, "+"))
	} else {
		revset = fmt.Sprintf("ancestors(%s)", refs[0])
	}
	limitedRevset := fmt.Sprintf("last(%s, %d)", revset, maxCount)
	return fmt.Sprintf("sl log --pager never -T %s -r %s", tmpl, shellutil.Quote(limitedRevset))
}

func (s *SaplingCommandBuilder) LogRange(refs []string, forkPoint string) string {
	// Commits reachable from refs but not before forkPoint's parents.
	// Use {p1node} {p2node} instead of {parents} — see Log() for explanation.
	refExprs := make([]string, len(refs))
	for i, ref := range refs {
		if ref == "HEAD" {
			refExprs[i] = "."
		} else {
			refExprs[i] = ref
		}
	}
	revset := fmt.Sprintf("(%s)::%s", forkPoint, strings.Join(refExprs, "+"))
	limitedRevset := fmt.Sprintf("last(%s, 5000)", revset)
	return fmt.Sprintf("sl log --pager never -T '{node}|{short(node)}|{desc|firstline}|{author|user}|{date|isodate}|{p1node} {p2node}\\n' -r %s", shellutil.Quote(limitedRevset))
}

func (s *SaplingCommandBuilder) ResolveRef(ref string) string {
	slRef := ref
	if ref == "HEAD" {
		slRef = "."
	}
	return fmt.Sprintf("sl log --pager never -T '{node}' -r %s --limit 1", shellutil.Quote(slRef))
}

func (s *SaplingCommandBuilder) MergeBase(ref1, ref2 string) string {
	slRef1, slRef2 := ref1, ref2
	if slRef1 == "HEAD" {
		slRef1 = "."
	}
	revset := fmt.Sprintf("ancestor(%s, %s)", slRef1, slRef2)
	return fmt.Sprintf("sl log --pager never -T '{node}' -r %s --limit 1", shellutil.Quote(revset))
}

func (s *SaplingCommandBuilder) DefaultBranchRef(branch string) string {
	return "remote/" + branch
}

func (s *SaplingCommandBuilder) DetectDefaultBranch() string {
	// Sapling: get the default remote bookmark name (e.g., "main"), fall back to "main".
	// selectivepulldefault can be a comma-separated list (e.g., "master, fbcode/warm") —
	// take only the first entry and trim whitespace.
	return "sl config remotenames.selectivepulldefault 2>/dev/null | cut -d',' -f1 | tr -d ' ' || echo main"
}

func (s *SaplingCommandBuilder) RevListCount(rangeSpec string) string {
	exclude, include := parseRangeToRevset(rangeSpec)
	if exclude != "" {
		revset := fmt.Sprintf("only(%s, %s)", include, exclude)
		return fmt.Sprintf("sl log --pager never -T '.' -r %s | wc -l", shellutil.Quote(revset))
	}
	return fmt.Sprintf("sl log --pager never -T '.' -r %s | wc -l", shellutil.Quote(rangeSpec))
}

func (s *SaplingCommandBuilder) CurrentBranch() string {
	return "sl whereami"
}

func (s *SaplingCommandBuilder) StatusPorcelain() string {
	return "sl status"
}

func (s *SaplingCommandBuilder) RemoteBranchExists(branch string) string {
	return fmt.Sprintf("sl log --pager never -r 'remote(%s)' --limit 1 -T '{node}' 2>/dev/null", branch)
}

func (s *SaplingCommandBuilder) NewestTimestamp(rangeSpec string) string {
	exclude, include := parseRangeToRevset(rangeSpec)
	if exclude != "" {
		revset := fmt.Sprintf("last(only(%s, %s))", include, exclude)
		return fmt.Sprintf("sl log --pager never -T '{date|isodate}\\n' -r %s --limit 1", shellutil.Quote(revset))
	}
	return fmt.Sprintf("sl log --pager never -T '{date|isodate}\\n' -r %s --limit 1", shellutil.Quote(rangeSpec))
}

func (s *SaplingCommandBuilder) AddFiles(files []string) string {
	args := []string{"sl", "add"}
	for _, f := range files {
		args = append(args, shellutil.Quote(f))
	}
	return strings.Join(args, " ")
}

func (s *SaplingCommandBuilder) CommitAmendNoEdit() string {
	return "sl amend"
}

func (s *SaplingCommandBuilder) DiscardFile(file string) string {
	return fmt.Sprintf("sl revert %s", shellutil.Quote(file))
}

func (s *SaplingCommandBuilder) DiscardAllTracked() string {
	return "sl revert --all"
}

func (s *SaplingCommandBuilder) CleanUntrackedFile(file string) string {
	return fmt.Sprintf("rm -f %s", shellutil.Quote(file))
}

func (s *SaplingCommandBuilder) CleanAllUntracked() string {
	return "sl purge --all"
}

func (s *SaplingCommandBuilder) UnstageNewFile(file string) string {
	return fmt.Sprintf("sl forget %s", shellutil.Quote(file))
}

func (s *SaplingCommandBuilder) Uncommit() string {
	return "sl uncommit"
}

func (s *SaplingCommandBuilder) CheckIgnore(file string) string {
	// Sapling: check if file appears in ignored status output.
	// Exit 0 if ignored (grep matches), non-zero otherwise.
	return fmt.Sprintf("sl status -i %s 2>/dev/null | grep -q .", shellutil.Quote(file))
}

func (s *SaplingCommandBuilder) DiffUnified() string {
	return "sl diff --pager never"
}
