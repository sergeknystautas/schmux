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
	return fmt.Sprintf("git show %s", shellutil.Quote(revision+":"+path))
}

func (g *GitCommandBuilder) FileContent(path string) string {
	// Cap at 2000 lines to avoid overflowing the tmux scrollback buffer
	// when reading file content via RunCommand on remote hosts.
	return fmt.Sprintf("head -2000 %s", shellutil.Quote(path))
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
	for _, ref := range refs {
		args = append(args, shellutil.Quote(ref))
	}
	return strings.Join(args, " ")
}

func (g *GitCommandBuilder) LogRange(refs []string, forkPoint string) string {
	args := []string{
		"git", "log",
		"--format=%H|%h|%s|%an|%aI|%P",
		"--topo-order",
	}
	for _, ref := range refs {
		args = append(args, shellutil.Quote(ref))
	}
	args = append(args, "--not", shellutil.Quote(forkPoint+"^"))
	return strings.Join(args, " ")
}

func (g *GitCommandBuilder) ResolveRef(ref string) string {
	return fmt.Sprintf("git rev-parse --verify %s", shellutil.Quote(ref))
}

func (g *GitCommandBuilder) MergeBase(ref1, ref2 string) string {
	return fmt.Sprintf("git merge-base %s %s", shellutil.Quote(ref1), shellutil.Quote(ref2))
}

func (g *GitCommandBuilder) DefaultBranchRef(branch string) string {
	return "origin/" + branch
}

func (g *GitCommandBuilder) DetectDefaultBranch() string {
	// Try origin/HEAD first, fall back to local HEAD's branch name
	return "git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null | sed 's|refs/remotes/origin/||' || git symbolic-ref HEAD 2>/dev/null | sed 's|refs/heads/||'"
}

func (g *GitCommandBuilder) RevListCount(rangeSpec string) string {
	return fmt.Sprintf("git rev-list --count %s", shellutil.Quote(rangeSpec))
}

func (g *GitCommandBuilder) CurrentBranch() string {
	return "git branch --show-current"
}

func (g *GitCommandBuilder) StatusPorcelain() string {
	return "git status --porcelain"
}

func (g *GitCommandBuilder) RemoteBranchExists(branch string) string {
	return fmt.Sprintf("git ls-remote --heads origin %s", branch)
}

func (g *GitCommandBuilder) NewestTimestamp(rangeSpec string) string {
	return fmt.Sprintf("git log --format=%%aI -1 %s", shellutil.Quote(rangeSpec))
}

func (g *GitCommandBuilder) AddFiles(files []string) string {
	args := []string{"git", "add", "--"}
	for _, f := range files {
		args = append(args, shellutil.Quote(f))
	}
	return strings.Join(args, " ")
}

func (g *GitCommandBuilder) CommitAmendNoEdit() string {
	return "git commit --amend --no-edit"
}

func (g *GitCommandBuilder) DiscardFile(file string) string {
	return fmt.Sprintf("git checkout HEAD -- %s", shellutil.Quote(file))
}

func (g *GitCommandBuilder) DiscardAllTracked() string {
	return "git checkout -- ."
}

func (g *GitCommandBuilder) CleanUntrackedFile(file string) string {
	return fmt.Sprintf("git clean -f -- %s", shellutil.Quote(file))
}

func (g *GitCommandBuilder) CleanAllUntracked() string {
	return "git clean -fd"
}

func (g *GitCommandBuilder) UnstageNewFile(file string) string {
	return fmt.Sprintf("git rm -f --cached -- %s", shellutil.Quote(file))
}

func (g *GitCommandBuilder) Uncommit() string {
	return "git reset HEAD~1"
}

func (g *GitCommandBuilder) CheckIgnore(file string) string {
	return fmt.Sprintf("git check-ignore -q %s", shellutil.Quote(file))
}

func (g *GitCommandBuilder) DiffUnified() string {
	return "git diff HEAD"
}
