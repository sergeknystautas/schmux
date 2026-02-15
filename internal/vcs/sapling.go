package vcs

import (
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

// SaplingCommandBuilder implements CommandBuilder for Sapling (sl).
type SaplingCommandBuilder struct{}

func (s *SaplingCommandBuilder) DiffNumstat() string {
	return "sl diff --numstat"
}

func (s *SaplingCommandBuilder) ShowFile(path, revision string) string {
	// In sapling, .^ means "parent of working copy", equivalent to git's HEAD
	slRev := revision
	if revision == "HEAD" {
		slRev = ".^"
	}
	return fmt.Sprintf("sl cat -r %s %s", slRev, shellutil.Quote(path))
}

func (s *SaplingCommandBuilder) FileContent(path string) string {
	return fmt.Sprintf("cat %s", shellutil.Quote(path))
}

func (s *SaplingCommandBuilder) UntrackedFiles() string {
	return "sl status --unknown --no-status"
}

func (s *SaplingCommandBuilder) Log(refs []string, maxCount int) string {
	// Sapling log with parseable template
	revset := "ancestors(.)"
	if len(refs) > 1 {
		revset = fmt.Sprintf("ancestors(%s)", strings.Join(refs, "+"))
	} else if len(refs) == 1 && refs[0] != "HEAD" {
		revset = fmt.Sprintf("ancestors(%s)", refs[0])
	}
	return fmt.Sprintf("sl log -T '{node}|{short(node)}|{desc|firstline}|{author|user}|{date|isodate}|{parents}\\n' -r '%s' --limit %d",
		revset, maxCount)
}

func (s *SaplingCommandBuilder) LogRange(refs []string, forkPoint string) string {
	// Commits reachable from refs but not before forkPoint's parents
	refExprs := make([]string, len(refs))
	for i, ref := range refs {
		if ref == "HEAD" {
			refExprs[i] = "."
		} else {
			refExprs[i] = ref
		}
	}
	revset := fmt.Sprintf("(%s)::%s", forkPoint, strings.Join(refExprs, "+"))
	return fmt.Sprintf("sl log -T '{node}|{short(node)}|{desc|firstline}|{author|user}|{date|isodate}|{parents}\\n' -r '%s'", revset)
}

func (s *SaplingCommandBuilder) ResolveRef(ref string) string {
	slRef := ref
	if ref == "HEAD" {
		slRef = "."
	}
	return fmt.Sprintf("sl log -T '{node}' -r '%s' --limit 1", slRef)
}

func (s *SaplingCommandBuilder) MergeBase(ref1, ref2 string) string {
	slRef1, slRef2 := ref1, ref2
	if slRef1 == "HEAD" {
		slRef1 = "."
	}
	return fmt.Sprintf("sl log -T '{node}' -r 'ancestor(%s, %s)' --limit 1", slRef1, slRef2)
}

func (s *SaplingCommandBuilder) DefaultBranchRef(branch string) string {
	return "remote/" + branch
}

func (s *SaplingCommandBuilder) DetectDefaultBranch() string {
	// Sapling: get the default remote bookmark name (e.g., "main"), fall back to "main"
	return "sl config remotenames.selectivepulldefault 2>/dev/null || echo main"
}

func (s *SaplingCommandBuilder) RevListCount(rangeSpec string) string {
	// Convert git range notation to sapling revset
	// "HEAD..origin/main" -> "only(origin/main, .)"
	parts := strings.Split(rangeSpec, "..")
	if len(parts) == 2 {
		exclude, include := parts[0], parts[1]
		if exclude == "HEAD" {
			exclude = "."
		}
		return fmt.Sprintf("sl log -T '.' -r 'only(%s, %s)' | wc -l", include, exclude)
	}
	// Fallback for other range specs
	return fmt.Sprintf("sl log -T '.' -r '%s' | wc -l", rangeSpec)
}
