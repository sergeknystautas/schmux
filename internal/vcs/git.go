package vcs

import (
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

// GitCommandBuilder implements CommandBuilder for git.
type GitCommandBuilder struct{}

func (g *GitCommandBuilder) DiffNumstat() string {
	return "git diff HEAD --numstat --find-renames --diff-filter=ADM"
}

func (g *GitCommandBuilder) ShowFile(path, revision string) string {
	return fmt.Sprintf("git show %s:%s", revision, path)
}

func (g *GitCommandBuilder) FileContent(path string) string {
	return fmt.Sprintf("cat %s", shellutil.Quote(path))
}

func (g *GitCommandBuilder) UntrackedFiles() string {
	return "git ls-files --others --exclude-standard"
}

func (g *GitCommandBuilder) Log(refs []string, maxCount int) string {
	args := []string{
		"git", "log",
		"--format=%H|%h|%s|%an|%aI|%P",
		"--topo-order",
		fmt.Sprintf("--max-count=%d", maxCount),
	}
	args = append(args, refs...)
	return strings.Join(args, " ")
}

func (g *GitCommandBuilder) LogRange(refs []string, forkPoint string) string {
	args := []string{
		"git", "log",
		"--format=%H|%h|%s|%an|%aI|%P",
		"--topo-order",
	}
	args = append(args, refs...)
	args = append(args, "--not", forkPoint+"^")
	return strings.Join(args, " ")
}

func (g *GitCommandBuilder) ResolveRef(ref string) string {
	return fmt.Sprintf("git rev-parse --verify %s", ref)
}

func (g *GitCommandBuilder) MergeBase(ref1, ref2 string) string {
	return fmt.Sprintf("git merge-base %s %s", ref1, ref2)
}

func (g *GitCommandBuilder) DefaultBranchRef(branch string) string {
	return "origin/" + branch
}

func (g *GitCommandBuilder) DetectDefaultBranch() string {
	// Try origin/HEAD first, fall back to local HEAD's branch name
	return "git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null | sed 's|refs/remotes/origin/||' || git symbolic-ref HEAD 2>/dev/null | sed 's|refs/heads/||'"
}

func (g *GitCommandBuilder) RevListCount(rangeSpec string) string {
	return fmt.Sprintf("git rev-list --count %s", rangeSpec)
}
