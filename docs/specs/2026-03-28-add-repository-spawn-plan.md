# Add Repository via Spawn — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to paste a git URL in the spawn wizard to clone and register a new repo in one step.

**Architecture:** The spawn handler (`handlers_spawn.go`) detects git URLs in `req.Repo`, generates a name, registers the repo in config, and lets the existing spawn flow handle cloning. The frontend renames the option, updates the placeholder, adds helper text, and sends raw URLs for git repos (no `local:` prefix).

**Tech Stack:** Go (handler + name generation), TypeScript/React (SpawnPage UI), Vitest (frontend tests)

**Spec:** `docs/specs/2026-03-28-add-repository-spawn-design.md`

---

### File Map

- **Create:** `internal/dashboard/repo_name.go` — standalone `repoNameFromURL` function + `isGitURL` helper
- **Create:** `internal/dashboard/repo_name_test.go` — unit tests for name generation and URL detection
- **Modify:** `internal/dashboard/handlers_spawn.go` — add URL detection + registration before spawn
- **Modify:** `internal/dashboard/handlers_spawn_test.go` — integration test for URL registration flow
- **Modify:** `assets/dashboard/src/routes/SpawnPage.tsx` — UI text changes + URL handling on submit

---

### Task 1: Name Generation Function

**Files:**

- Create: `internal/dashboard/repo_name.go`
- Create: `internal/dashboard/repo_name_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/dashboard/repo_name_test.go
package dashboard

import "testing"

func TestIsGitURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://github.com/user/repo.git", true},
		{"http://github.com/user/repo.git", true},
		{"git@github.com:user/repo.git", true},
		{"ssh://git@github.com/user/repo.git", true},
		{"git://github.com/user/repo.git", true},
		{"https://gitlab.com/user/repo", true},
		{"my-project", false},
		{"local:my-project", false},
		{"", false},
		{"https://", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isGitURL(tt.input); got != tt.want {
				t.Errorf("isGitURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestRepoNameFromURL(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		existingNames []string
		want          string
	}{
		{
			name:          "basic HTTPS URL",
			url:           "https://github.com/anthropics/claude-code.git",
			existingNames: nil,
			want:          "claude-code",
		},
		{
			name:          "SSH URL",
			url:           "git@github.com:anthropics/claude-code.git",
			existingNames: nil,
			want:          "claude-code",
		},
		{
			name:          "no .git suffix",
			url:           "https://github.com/anthropics/claude-code",
			existingNames: nil,
			want:          "claude-code",
		},
		{
			name:          "collision adds owner prefix",
			url:           "https://github.com/bob/claude-code.git",
			existingNames: []string{"claude-code"},
			want:          "bob-claude-code",
		},
		{
			name:          "owner truncated to 6 chars",
			url:           "https://github.com/very-long-org-name/utils.git",
			existingNames: []string{"utils"},
			want:          "very-l-utils",
		},
		{
			name:          "owner prefix also collides, numeric suffix",
			url:           "https://github.com/alice/claude-code.git",
			existingNames: []string{"claude-code", "alice-claude-code"},
			want:          "alice-claude-code-2",
		},
		{
			name:          "numeric suffix increments",
			url:           "https://github.com/alice/claude-code.git",
			existingNames: []string{"claude-code", "alice-claude-code", "alice-claude-code-2"},
			want:          "alice-claude-code-3",
		},
		{
			name:          "no owner in URL, straight to numeric suffix",
			url:           "https://example.com/repo.git",
			existingNames: []string{"repo"},
			want:          "repo-2",
		},
		{
			name:          "uppercase in URL lowercased",
			url:           "https://github.com/Owner/MyRepo.git",
			existingNames: nil,
			want:          "myrepo",
		},
		{
			name:          "SSH with colon separator",
			url:           "git@gitlab.com:myorg/my-project.git",
			existingNames: nil,
			want:          "my-project",
		},
		{
			name:          "owner exactly 6 chars, no truncation",
			url:           "https://github.com/abcdef/utils.git",
			existingNames: []string{"utils"},
			want:          "abcdef-utils",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repoNameFromURL(tt.url, tt.existingNames)
			if got != tt.want {
				t.Errorf("repoNameFromURL(%q, %v) = %q, want %q", tt.url, tt.existingNames, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/dashboard/ -run "TestIsGitURL|TestRepoNameFromURL" -v`
Expected: FAIL — `isGitURL` and `repoNameFromURL` not defined

- [ ] **Step 3: Write the implementation**

```go
// internal/dashboard/repo_name.go
package dashboard

import (
	"fmt"
	"strings"
)

// isGitURL returns true if the input looks like a git remote URL.
func isGitURL(s string) bool {
	for _, prefix := range []string{"https://", "http://", "ssh://", "git://"} {
		if strings.HasPrefix(s, prefix) {
			// Must have a path component after the host
			rest := s[len(prefix):]
			return strings.Contains(rest, "/")
		}
	}
	if strings.HasPrefix(s, "git@") {
		return strings.Contains(s, ":")
	}
	return false
}

// repoNameFromURL generates a human-readable repo name from a git URL.
// It uses the last path segment (stripped of .git), lowercased.
// On collision with existingNames, it prepends the owner (truncated to 6 chars).
// If that still collides, it appends a numeric suffix (-2, -3, etc.).
func repoNameFromURL(url string, existingNames []string) string {
	repo, owner := extractRepoAndOwner(url)

	nameSet := make(map[string]bool, len(existingNames))
	for _, n := range existingNames {
		nameSet[n] = true
	}

	// Try repo name alone
	candidate := repo
	if !nameSet[candidate] {
		return candidate
	}

	// Try owner-repo (if owner available)
	if owner != "" {
		if len(owner) > 6 {
			owner = owner[:6]
		}
		candidate = owner + "-" + repo
		if !nameSet[candidate] {
			return candidate
		}
	}

	// Numeric suffix
	for i := 2; ; i++ {
		suffixed := fmt.Sprintf("%s-%d", candidate, i)
		if !nameSet[suffixed] {
			return suffixed
		}
	}
}

// extractRepoAndOwner parses a git URL and returns (repo, owner), both lowercased.
// For "git@github.com:anthropics/claude-code.git" → ("claude-code", "anthropics").
// For URLs with only one path segment, owner is empty.
func extractRepoAndOwner(url string) (string, string) {
	// Normalize SSH-style URLs: git@host:path → path
	path := url
	if idx := strings.Index(path, "://"); idx >= 0 {
		path = path[idx+3:]
	}
	if strings.HasPrefix(path, "git@") {
		if idx := strings.Index(path, ":"); idx >= 0 {
			path = path[idx+1:]
		}
	}
	// Remove host for http/ssh URLs
	if idx := strings.Index(path, "/"); idx >= 0 {
		path = path[idx+1:]
	}

	// Strip .git suffix
	path = strings.TrimSuffix(path, ".git")

	// Split into segments
	segments := strings.Split(path, "/")

	var repo, owner string
	if len(segments) >= 2 {
		repo = segments[len(segments)-1]
		owner = segments[len(segments)-2]
	} else if len(segments) == 1 {
		repo = segments[0]
	}

	return strings.ToLower(repo), strings.ToLower(owner)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/dashboard/ -run "TestIsGitURL|TestRepoNameFromURL" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
feat(spawn): add repo name generation from git URL

New standalone functions isGitURL and repoNameFromURL for detecting
git URLs and generating human-readable repo names with collision
handling (owner prefix, numeric suffix).
```

---

### Task 2: Handler URL Detection and Registration

**Files:**

- Modify: `internal/dashboard/handlers_spawn.go:96-107` (validation section)
- Modify: `internal/dashboard/handlers_spawn_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/dashboard/handlers_spawn_test.go`:

```go
func TestHandleSpawnPost_GitURLRegistersRepo(t *testing.T) {
	server, cfg, _ := newTestServer(t)

	// Verify no repos exist initially
	if len(cfg.Repos) != 0 {
		t.Fatalf("expected 0 repos, got %d", len(cfg.Repos))
	}

	// Send a spawn request with a git URL as the repo
	// This will fail at the spawn stage (no real git repo), but the repo
	// should be registered in config before that happens.
	body := SpawnRequest{
		Repo:    "https://github.com/anthropics/claude-code.git",
		Branch:  "main",
		Targets: map[string]int{"command": 1},
		Prompt:  "hello",
	}
	postSpawnJSON(t, server.handleSpawnPost, body)

	// The repo should now be registered in config regardless of spawn outcome
	_, found := cfg.FindRepoByURL("https://github.com/anthropics/claude-code.git")
	if !found {
		t.Error("expected git URL to be registered in config after spawn request")
	}
}

func TestHandleSpawnPost_GitURLExistingRepoSkipsRegistration(t *testing.T) {
	server, cfg, _ := newTestServer(t)

	// Pre-register the repo
	cfg.Repos = append(cfg.Repos, config.Repo{
		Name:     "claude-code",
		URL:      "https://github.com/anthropics/claude-code.git",
		BarePath: "claude-code.git",
	})

	body := SpawnRequest{
		Repo:    "https://github.com/anthropics/claude-code.git",
		Branch:  "main",
		Targets: map[string]int{"command": 1},
		Prompt:  "hello",
	}
	postSpawnJSON(t, server.handleSpawnPost, body)

	// Should still be exactly 1 repo — no duplicate
	count := 0
	for _, r := range cfg.Repos {
		if r.URL == "https://github.com/anthropics/claude-code.git" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 repo entry, got %d", count)
	}
}

func TestHandleSpawnPost_GitURLGeneratesCorrectName(t *testing.T) {
	server, cfg, _ := newTestServer(t)

	body := SpawnRequest{
		Repo:    "https://github.com/anthropics/claude-code.git",
		Branch:  "main",
		Targets: map[string]int{"command": 1},
		Prompt:  "hello",
	}
	postSpawnJSON(t, server.handleSpawnPost, body)

	repo, found := cfg.FindRepoByURL("https://github.com/anthropics/claude-code.git")
	if !found {
		t.Fatal("repo not registered")
	}
	if repo.Name != "claude-code" {
		t.Errorf("repo name = %q, want %q", repo.Name, "claude-code")
	}
	if repo.BarePath != "claude-code.git" {
		t.Errorf("repo bare path = %q, want %q", repo.BarePath, "claude-code.git")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/dashboard/ -run "TestHandleSpawnPost_GitURL" -v`
Expected: FAIL — repos not registered (handler doesn't detect URLs yet)

- [ ] **Step 3: Write the handler changes**

In `internal/dashboard/handlers_spawn.go`, add the URL detection and registration block after the validation section (after line 107, before the branch conflict check at line 150). Insert between the `// Validate request` block and `// Server-side branch conflict check`:

```go
	// Detect git URL in repo field and register if new
	if req.Repo != "" && isGitURL(req.Repo) {
		if _, found := s.config.FindRepoByURL(req.Repo); !found {
			// Collect existing repo names for collision detection
			existingNames := make([]string, 0, len(s.config.Repos))
			for _, r := range s.config.Repos {
				existingNames = append(existingNames, r.Name)
			}
			name := repoNameFromURL(req.Repo, existingNames)
			s.config.Repos = append(s.config.Repos, config.Repo{
				Name:     name,
				URL:      req.Repo,
				BarePath: name + ".git",
			})
			if err := s.config.Save(); err != nil {
				writeJSONError(w, fmt.Sprintf("failed to register repo: %v", err), http.StatusInternalServerError)
				return
			}
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/dashboard/ -run "TestHandleSpawnPost_GitURL" -v`
Expected: PASS

- [ ] **Step 5: Run full handler tests**

Run: `go test ./internal/dashboard/ -run "TestHandleSpawnPost" -v`
Expected: PASS (no regressions)

- [ ] **Step 6: Commit**

```
feat(spawn): register new git URLs in config during spawn

When the spawn handler receives a git URL that isn't already in config,
it generates a name and registers the repo before proceeding with the
normal spawn flow.
```

---

### Task 3: Frontend UI Changes

**Files:**

- Modify: `assets/dashboard/src/routes/SpawnPage.tsx`

- [ ] **Step 1: Update dropdown option text (two instances)**

Change line 1136:

```tsx
<option value="__new__">+ Add Repository</option>
```

Change line 1247:

```tsx
<option value="__new__">+ Add Repository</option>
```

- [ ] **Step 2: Update input placeholder (two instances)**

Change line 1146:

```tsx
placeholder = 'Name or git URL';
```

Change line 1258:

```tsx
placeholder = 'Name or git URL';
```

- [ ] **Step 3: Add helper text below each input**

After the input at line 1148 (single-agent mode), inside the `{repo === '__new__' && (` block, add a helper text paragraph after the `<input>`:

```tsx
<p className="text-secondary text-sm mt-xs">
  Enter a project name to init a new one locally, or paste a git URL to clone an existing
  repository.
</p>
```

After the input at line 1260 (multi-agent mode), inside the `<div className="mt-sm">` block, add the same helper text after the `<input>`:

```tsx
<p className="text-secondary text-sm mt-xs">
  Enter a project name to init a new one locally, or paste a git URL to clone an existing
  repository.
</p>
```

- [ ] **Step 4: Update submit logic to send raw URL for git repos**

There are three places that compute `actualRepo` from the `__new__` selection. Each currently does:

```tsx
const actualRepo = repo === '__new__' ? `local:${newRepoName.trim()}` : repo;
```

These are at lines 664, 716, and 762. Update each to detect git URLs:

```tsx
const actualRepo =
  repo === '__new__'
    ? /^(https?:\/\/|git@|ssh:\/\/|git:\/\/)/.test(newRepoName.trim())
      ? newRepoName.trim()
      : `local:${newRepoName.trim()}`
    : repo;
```

- [ ] **Step 5: Add frontend idempotency check**

In the `validateForm` callback (around line 559), after the empty-name check for `__new__`, add a short-circuit that switches to an existing repo if the URL matches:

```tsx
if (repo === '__new__' && !newRepoName.trim()) {
  toastError('Please enter a repository name');
  return false;
}
// Short-circuit: if URL matches an existing repo, switch to it
if (repo === '__new__' && /^(https?:\/\/|git@|ssh:\/\/|git:\/\/)/.test(newRepoName.trim())) {
  const matchingRepo = repos.find((r) => r.url === newRepoName.trim());
  if (matchingRepo) {
    setRepo(matchingRepo.url);
    setNewRepoName('');
    return false; // Don't submit — let user see the switch
  }
}
```

- [ ] **Step 6: Run tests**

Run: `./test.sh --quick`
Expected: PASS

- [ ] **Step 7: Commit**

```
feat(spawn): update UI to accept git URLs in add repository flow

Rename "+ Create New Repository" to "+ Add Repository", update placeholder
to "Name or git URL", add helper text, and send raw URL (no local: prefix)
when input matches a git URL pattern.
```

---

### Task 4: Full Integration Test

- [ ] **Step 1: Run the full test suite**

Run: `./test.sh`
Expected: PASS

- [ ] **Step 2: Verify no regressions in existing spawn flow**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS

- [ ] **Step 3: Verify no regressions in workspace manager**

Run: `go test ./internal/workspace/ -v`
Expected: PASS

- [ ] **Step 4: Final commit (if any formatting changes needed)**

Run: `./format.sh`

If files changed, commit:

```
style: format code
```
