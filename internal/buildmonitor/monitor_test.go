//go:build !nobuildmonitor

package buildmonitor

import (
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/github"
)

func TestStatus(t *testing.T) {
	repo := gh("acme", "app")
	// helper to build an enabled monitor with one repo's metadata
	newMon := func(hasWorkflows bool, repoErr string) *Monitor {
		m := NewMonitor(time.Now, "")
		m.setEnabled()
		m.setRepoMeta(repo, hasWorkflows, repoErr)
		return m
	}

	t.Run("disabled → absent", func(t *testing.T) {
		m := NewMonitor(time.Now, "")
		m.setRepoMeta(repo, true, "")
		m.recordCommit(repo, "sha1", StatusSuccess, "https://run", true)
		if _, _, ok := m.Status(repo, "sha1"); ok {
			t.Fatal("want absent while disabled")
		}
	})

	t.Run("empty sha → absent", func(t *testing.T) {
		m := newMon(true, "")
		if _, _, ok := m.Status(repo, ""); ok {
			t.Fatal("want absent")
		}
	})

	t.Run("unrecorded commit + active workflows → queued (first push)", func(t *testing.T) {
		m := newMon(true, "")
		st, _, ok := m.Status(repo, "newsha")
		if !ok || st != StatusQueued {
			t.Fatalf("got (%q,%v), want (queued,true)", st, ok)
		}
	})

	t.Run("unrecorded commit + repo without workflows → absent", func(t *testing.T) {
		m := newMon(false, "")
		if _, _, ok := m.Status(repo, "s"); ok {
			t.Fatal("want absent")
		}
	})

	t.Run("unrecorded commit + unknown repo → absent", func(t *testing.T) {
		m := NewMonitor(time.Now, "")
		m.setEnabled()
		if _, _, ok := m.Status(repo, "s"); ok {
			t.Fatal("want absent")
		}
	})

	t.Run("recorded commit → its status", func(t *testing.T) {
		m := newMon(true, "")
		m.recordCommit(repo, "sha1", StatusSuccess, "https://run", true)
		st, url, ok := m.Status(repo, "sha1")
		if !ok || st != StatusSuccess || url != "https://run" {
			t.Fatalf("got (%q,%q,%v)", st, url, ok)
		}
	})

	t.Run("head moved → queued (post-push gap)", func(t *testing.T) {
		m := newMon(true, "")
		m.recordCommit(repo, "old", StatusSuccess, "https://run", true)
		st, _, ok := m.Status(repo, "new")
		if !ok || st != StatusQueued {
			t.Fatalf("got (%q,%v), want (queued,true)", st, ok)
		}
	})

	t.Run("repo error → absent", func(t *testing.T) {
		m := newMon(true, "unauthorized")
		m.recordCommit(repo, "sha1", StatusSuccess, "https://run", true)
		if _, _, ok := m.Status(repo, "sha1"); ok {
			t.Fatal("want absent")
		}
	})

	t.Run("fork commit lives under the fork repo", func(t *testing.T) {
		m := NewMonitor(time.Now, "")
		m.setEnabled()
		m.setRepoMeta(gh("acme", "app"), true, "")
		m.setRepoMeta(gh("fork", "app"), true, "")
		m.recordCommit(gh("fork", "app"), "sha1", StatusFailure, "https://forkrun", true)
		st, url, ok := m.Status(gh("fork", "app"), "sha1")
		if !ok || st != StatusFailure || url != "https://forkrun" {
			t.Fatalf("got (%q,%q,%v)", st, url, ok)
		}
		// The same SHA under the base repo must not inherit the fork's status.
		st, _, ok = m.Status(repo, "sha1")
		if !ok || st != StatusQueued {
			t.Fatalf("base repo got (%q,%v), want (queued,true)", st, ok)
		}
	})
}

func TestSetDisabled(t *testing.T) {
	repo := gh("acme", "app")
	m := NewMonitor(time.Now, "")
	m.setEnabled()
	m.setRepoMeta(repo, true, "")
	m.recordCommit(repo, "sha1", StatusSuccess, "u", true)

	if !m.setDisabled() {
		t.Fatal("setDisabled should report held data")
	}
	if _, _, ok := m.Status(repo, "sha1"); ok {
		t.Fatal("want absent after disable")
	}
	if m.setDisabled() {
		t.Fatal("second setDisabled should report nothing held")
	}
}

func TestPruneExcept(t *testing.T) {
	repo := gh("acme", "app")
	m := NewMonitor(time.Now, "")
	m.setEnabled()
	m.setRepoMeta(repo, true, "")
	m.recordCommit(repo, "sha1", StatusSuccess, "u", true)
	m.recordCommit(repo, "sha2", StatusFailure, "u2", true)

	m.pruneExcept(map[commitKey]bool{{owner: "acme", repo: "app", sha: "sha1"}: true})

	if _, _, ok := m.Status(repo, "sha1"); !ok {
		t.Fatal("referenced commit should survive pruning")
	}
	m.mu.Lock()
	_, present := m.commits[commitKey{owner: "acme", repo: "app", sha: "sha2"}]
	m.mu.Unlock()
	if present {
		t.Fatal("unreferenced commit should be pruned")
	}
}

func TestHydrate(t *testing.T) {
	repo := gh("acme", "app")

	t.Run("snapshot with successful head → success", func(t *testing.T) {
		m := NewMonitor(time.Now, "")
		m.Hydrate(map[string]*UnitState{
			"r": {
				Repo: "acme/app", RepoName: "acme/app", Branch: "main", HeadSHA: "h1",
				CheckedAt: "2026-08-20T10:00:00Z",
				Workflows: []WorkflowState{
					{WorkflowID: 1, RunID: 7, Status: "completed", Conclusion: "success", HeadSHA: "h1", HTMLURL: "u"},
				},
			},
		})
		st, url, ok := m.Status(repo, "h1")
		if !ok || st != StatusSuccess || url != "u" {
			t.Fatalf("got (%q,%q,%v), want (success,u,true)", st, url, ok)
		}
	})

	t.Run("snapshot with no runs → head queued, unknown commits queued", func(t *testing.T) {
		m := NewMonitor(time.Now, "")
		m.Hydrate(map[string]*UnitState{
			"r": {Repo: "acme/app", Branch: "main", HeadSHA: "h1",
				Workflows: []WorkflowState{{WorkflowID: 1, Name: "CI"}}},
		})
		if st, _, ok := m.Status(repo, "h1"); !ok || st != StatusQueued {
			t.Fatalf("head got (%q,%v), want (queued,true)", st, ok)
		}
		// Repo metadata seeded: an unrecorded commit in an active repo → queued.
		if st, _, ok := m.Status(repo, "unseen"); !ok || st != StatusQueued {
			t.Fatalf("unknown commit got (%q,%v), want (queued,true)", st, ok)
		}
	})

	t.Run("snapshot with failing head → failure", func(t *testing.T) {
		m := NewMonitor(time.Now, "")
		m.Hydrate(map[string]*UnitState{
			"r": {Repo: "acme/app", Branch: "main", HeadSHA: "h1",
				Workflows: []WorkflowState{
					{WorkflowID: 1, RunID: 7, Status: "completed", Conclusion: "success", HeadSHA: "h1"},
					{WorkflowID: 2, RunID: 8, Status: "completed", Conclusion: "timed_out", HeadSHA: "h1"},
				}},
		})
		if st, _, ok := m.Status(repo, "h1"); !ok || st != StatusFailure {
			t.Fatalf("got (%q,%v), want (failure,true)", st, ok)
		}
	})

	t.Run("repo with no workflows → absent", func(t *testing.T) {
		m := NewMonitor(time.Now, "")
		m.Hydrate(map[string]*UnitState{
			"r": {Repo: "acme/app", Branch: "main", HeadSHA: "h1"},
		})
		if _, _, ok := m.Status(repo, "h1"); ok {
			t.Fatal("want absent for repo without workflows")
		}
	})
}

func gh(owner, repo string) github.RepoInfo { return github.RepoInfo{Owner: owner, Repo: repo} }
