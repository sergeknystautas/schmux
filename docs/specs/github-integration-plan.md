# GitHub Integration Phase 1 — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Establish `gh auth status` as the single gate for GitHub features, fix local repo polling, and add a "Publish to GitHub" flow.

**Architecture:** Run `gh auth status` at daemon startup, store result in memory, broadcast via WebSocket. Frontend gates GitHub UI on this status. Local repos skip remote git checks. Publish flow uses `gh repo create` and updates schmux config.

**Tech Stack:** Go backend, React/TypeScript frontend, `gh` CLI, WebSocket broadcast

---

### Task 1: GitHub status contract and type generation

**Files:**

- Create: `internal/api/contracts/github.go`
- Modify: `assets/dashboard/src/lib/types.generated.ts` (via gen-types)

**Step 1: Create the contract type**

```go
// internal/api/contracts/github.go
package contracts

// GitHubStatus represents the gh CLI authentication state.
type GitHubStatus struct {
	Available bool   `json:"available"`
	Username  string `json:"username,omitempty"`
}
```

**Step 2: Regenerate TypeScript types**

Run: `go run ./cmd/gen-types`
Expected: `types.generated.ts` now contains `GitHubStatus` interface.

**Step 3: Commit**

```
feat(github): add GitHubStatus contract type
```

---

### Task 2: `gh auth status` check in the github package

**Files:**

- Modify: `internal/github/repo.go`
- Create: `internal/github/auth.go`
- Create: `internal/github/auth_test.go`

**Step 1: Write the test**

```go
// internal/github/auth_test.go
package github

import (
	"context"
	"testing"
)

func TestCheckAuth_ParsesUsername(t *testing.T) {
	// Integration test — skipped if gh not available
	status := CheckAuth(context.Background())
	if !status.Available {
		t.Skip("gh not authenticated, skipping")
	}
	if status.Username == "" {
		t.Error("expected non-empty username when authenticated")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/github/ -run TestCheckAuth -v`
Expected: FAIL — `CheckAuth` not defined.

**Step 3: Implement CheckAuth**

```go
// internal/github/auth.go
package github

import (
	"context"
	"os/exec"
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// CheckAuth runs `gh auth status` to determine if the gh CLI is installed and authenticated.
// Returns a GitHubStatus with Available=true and the username if authenticated.
// This is a blocking call that shells out to `gh`.
func CheckAuth(ctx context.Context) contracts.GitHubStatus {
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return contracts.GitHubStatus{}
	}

	cmd := exec.CommandContext(ctx, ghPath, "auth", "status")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return contracts.GitHubStatus{}
	}

	// Parse username from output like "Logged in to github.com account username (..."
	username := parseGhUsername(string(output))
	return contracts.GitHubStatus{
		Available: true,
		Username:  username,
	}
}

// parseGhUsername extracts the username from `gh auth status` output.
func parseGhUsername(output string) string {
	// gh auth status outputs lines like:
	//   "Logged in to github.com account USERNAME (...)"
	// or older versions:
	//   "Logged in to github.com as USERNAME (...)"
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		// Try "account USERNAME" pattern first (newer gh)
		if idx := strings.Index(line, "account "); idx >= 0 {
			rest := line[idx+len("account "):]
			if sp := strings.IndexAny(rest, " ("); sp > 0 {
				return rest[:sp]
			}
			return rest
		}
		// Try "as USERNAME" pattern (older gh)
		if idx := strings.Index(line, " as "); idx >= 0 {
			rest := line[idx+len(" as "):]
			if sp := strings.IndexAny(rest, " ("); sp > 0 {
				return rest[:sp]
			}
			return rest
		}
	}
	return ""
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/github/ -run TestCheckAuth -v`
Expected: PASS (or skip if gh not installed).

**Step 5: Add unit test for parseGhUsername**

```go
func TestParseGhUsername(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "newer format with account",
			output: "github.com\n  ✓ Logged in to github.com account sergeknystautas (keyring)\n  - Active account: true\n",
			want:   "sergeknystautas",
		},
		{
			name:   "older format with as",
			output: "github.com\n  ✓ Logged in to github.com as sergeknystautas (oauth_token)\n",
			want:   "sergeknystautas",
		},
		{
			name:   "empty output",
			output: "",
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseGhUsername(tt.output)
			if got != tt.want {
				t.Errorf("parseGhUsername() = %q, want %q", got, tt.want)
			}
		})
	}
}
```

**Step 6: Run tests**

Run: `go test ./internal/github/ -v`
Expected: PASS

**Step 7: Commit**

```
feat(github): add CheckAuth to detect gh CLI authentication
```

---

### Task 3: Store GitHub status on Daemon and broadcast via WebSocket

**Files:**

- Modify: `internal/daemon/daemon.go` (Daemon struct + Run method)
- Modify: `internal/dashboard/server.go` (Server struct + broadcast method)

**Step 1: Add GitHubStatus to Daemon struct**

In `internal/daemon/daemon.go`, add to the `Daemon` struct (after line 66):

```go
githubStatus contracts.GitHubStatus
```

**Step 2: Call CheckAuth in Run(), before the dashboard server is created**

In `daemon.go` `Run()`, after config/state are loaded but before creating the dashboard server (around line 403), add:

```go
// Check gh CLI authentication
d.githubStatus = github.CheckAuth(d.shutdownCtx)
if d.githubStatus.Available {
	fmt.Printf("[daemon] GitHub authenticated as %s\n", d.githubStatus.Username)
} else {
	fmt.Println("[daemon] GitHub not available (gh CLI not installed or not authenticated)")
}
```

**Step 3: Pass GitHubStatus to dashboard Server**

Add `githubStatus contracts.GitHubStatus` parameter to `dashboard.NewServer()`. Store it on the `Server` struct. This requires modifying:

- `internal/dashboard/server.go`: Add `githubStatus contracts.GitHubStatus` field to `Server`, add parameter to `NewServer()`.
- `internal/daemon/daemon.go`: Pass `d.githubStatus` to `NewServer()` call.

**Step 4: Add broadcast method for GitHub status**

In `internal/dashboard/server.go`, add a method to broadcast GitHub status and include it in `doBroadcast()`:

```go
// In doBroadcast(), after sending the "sessions" payload, also send github status:
ghPayload, err := json.Marshal(map[string]interface{}{
	"type":          "github_status",
	"github_status": s.githubStatus,
})
if err == nil {
	// Send after sessions payload
	for _, conn := range conns {
		conn.WriteMessage(websocket.TextMessage, ghPayload)
	}
}
```

**Step 5: Add GET /api/github/status endpoint**

In `internal/dashboard/server.go` (or a new `handlers_github.go`), add:

```go
func (s *Server) handleGetGitHubStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.githubStatus)
}
```

Register the route in the router setup.

**Step 6: Build and verify**

Run: `go build ./cmd/schmux`
Expected: Compiles cleanly.

**Step 7: Commit**

```
feat(github): check gh auth at daemon startup and broadcast status
```

---

### Task 4: Frontend — consume GitHub status via WebSocket

**Files:**

- Modify: `assets/dashboard/src/hooks/useSessionsWebSocket.ts`
- Modify: `assets/dashboard/src/lib/api.ts`

**Step 1: Add githubStatus state to useSessionsWebSocket**

In `useSessionsWebSocket.ts`, add state:

```typescript
const [githubStatus, setGithubStatus] = useState<GitHubStatus>({ available: false });
```

Add handler in `ws.onmessage` (after the `remote_access_status` handler):

```typescript
} else if (data.type === 'github_status' && data.github_status) {
  setGithubStatus(data.github_status as GitHubStatus);
}
```

Return `githubStatus` from the hook.

**Step 2: Add API function for initial fetch**

In `api.ts`:

```typescript
export async function getGitHubStatus(): Promise<GitHubStatus> {
  const response = await fetch('/api/github/status');
  if (!response.ok) return { available: false };
  return response.json();
}
```

**Step 3: Build dashboard**

Run: `go run ./cmd/build-dashboard`
Expected: Builds cleanly.

**Step 4: Commit**

```
feat(dashboard): consume GitHub status via WebSocket
```

---

### Task 5: Gate the home page GitHub/PR section on status

**Files:**

- Modify: `assets/dashboard/src/routes/HomePage.tsx`

**Step 1: Use githubStatus from the WebSocket hook**

Where `HomePage` consumes `useSessionsWebSocket`, destructure `githubStatus`.

**Step 2: Replace the unconditional PR section**

Replace the current PR section (lines ~554-659) with a gated section:

- When `!githubStatus.available`: Show a compact card with "GitHub: not connected" and a brief note about installing/authenticating `gh` CLI.
- When `githubStatus.available`: Show "Connected as @{username}" status line at the top, then the existing PR list below.

**Step 3: Build and verify**

Run: `go run ./cmd/build-dashboard`
Expected: Builds cleanly.

**Step 4: Commit**

```
feat(dashboard): gate GitHub section on gh auth status
```

---

### Task 6: Fix git polling for local repos

**Files:**

- Modify: `internal/workspace/git.go` (~line 365)
- Modify: `internal/workspace/origin_queries.go` (~line 29)
- Create: `internal/workspace/git_test.go` (or add to existing)

**Step 1: Write the test**

Test that `gitStatus` for a local repo skips remote checks and doesn't error. Create a temporary local git repo (no origin), call `gitStatus`, verify it returns dirty/clean status without errors.

**Step 2: Run test to verify it fails**

Run the test — currently `gitStatus` will attempt `git fetch` and `origin/{defaultBranch}` checks that fail on repos with no remote.

**Step 3: Skip remote checks for local repos**

In `git.go` `gitStatus()` (line 365), the function receives `repoURL`. Add an early check:

```go
isLocal := strings.HasPrefix(repoURL, "local:")
```

Then guard the remote-dependent sections:

- Line 367: `if !isLocal { _ = m.gitFetch(ctx, dir) }`
- Line 388-408: Wrap the ahead/behind `rev-list` block in `if !isLocal { ... }`
- Line 410-445: Wrap the `remoteBranchExists` / `commitsSyncedWithRemote` block in `if !isLocal { ... }`

**Step 4: Skip origin queries for local repos**

In `origin_queries.go` `EnsureOriginQueries()` (line 29), skip repos with `local:` URL:

```go
for _, repo := range m.config.GetRepos() {
	if strings.HasPrefix(repo.URL, "local:") {
		continue
	}
	// ... existing logic
}
```

Same in `FetchOriginQueries()`.

**Step 5: Run tests**

Run: `go test ./internal/workspace/ -v`
Expected: PASS, no more error spam for local repos.

**Step 6: Commit**

```
fix(workspace): skip remote git checks for local repos
```

---

### Task 7: Gate lore auto-PR on GitHub status

**Files:**

- Modify: `internal/dashboard/handlers_lore.go` (~line 262)
- Modify: `internal/dashboard/server.go` (expose getter for githubStatus)

**Step 1: Add a getter for GitHub status on Server**

In `server.go`:

```go
func (s *Server) GitHubAvailable() bool {
	return s.githubStatus.Available
}
```

**Step 2: Gate the auto-PR call**

In `handlers_lore.go` line 262, change:

```go
if s.config.GetLoreAutoPR() {
```

to:

```go
if s.config.GetLoreAutoPR() && s.githubStatus.Available {
```

**Step 3: Build and verify**

Run: `go build ./cmd/schmux`
Expected: Compiles cleanly.

**Step 4: Commit**

```
fix(lore): gate auto-PR on gh auth status
```

---

### Task 8: GitHub links on workspace header

**Files:**

- Modify: `assets/dashboard/src/components/WorkspaceHeader.tsx`
- Modify: `internal/workspace/giturl.go` (add repo URL builder)

**Step 1: Add BuildGitRepoURL to giturl.go**

```go
// BuildGitRepoURL constructs a web URL for the repository itself (not a specific branch).
func BuildGitRepoURL(repoURL string) string {
	if repoURL == "" || strings.HasPrefix(repoURL, "local:") {
		return ""
	}
	cleanRepoURL := strings.TrimSuffix(repoURL, ".git")
	if strings.HasPrefix(cleanRepoURL, "git@") {
		parts := strings.TrimPrefix(cleanRepoURL, "git@")
		colonIdx := strings.Index(parts, ":")
		if colonIdx == -1 {
			return ""
		}
		host := parts[:colonIdx]
		path := parts[colonIdx+1:]
		return fmt.Sprintf("https://%s/%s", host, path)
	}
	u, err := url.Parse(cleanRepoURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return cleanRepoURL
}
```

**Step 2: Add test for BuildGitRepoURL**

```go
func TestBuildGitRepoURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"git@github.com:user/repo.git", "https://github.com/user/repo"},
		{"https://github.com/user/repo.git", "https://github.com/user/repo"},
		{"local:myrepo", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := BuildGitRepoURL(tt.input)
		if got != tt.want {
			t.Errorf("BuildGitRepoURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
```

**Step 3: Expose repo_url in the workspace broadcast**

The workspace response needs to include the repo URL so the frontend can build the link. Check if `repo` is already in the broadcast data — if so, use `BuildGitRepoURL` on the backend side and send it as `repo_web_url`.

**Step 4: Update WorkspaceHeader.tsx**

When `githubStatus.available` and `repo_web_url` is present:

- Add a small GitHub icon/link next to the branch name that links to the repo on GitHub.
- Keep the existing branch link behavior (only when `remoteBranchExists`).

**Step 5: Build and verify**

Run: `go run ./cmd/build-dashboard`
Expected: Builds cleanly.

**Step 6: Commit**

```
feat(dashboard): add GitHub repo link on workspace header
```

---

### Task 9: Publish to GitHub — backend API

**Files:**

- Create: `internal/dashboard/handlers_github.go`
- Modify: `internal/dashboard/server.go` (register routes)

**Step 1: Add the publish endpoint**

```go
// POST /api/github/publish
// Request: { workspace_id: string, name: string, visibility: "private"|"public", owner: string }
// Response: { repo_url: string }
func (s *Server) handlePublishToGitHub(w http.ResponseWriter, r *http.Request) {
	if !s.githubStatus.Available {
		http.Error(w, "GitHub not available", http.StatusBadRequest)
		return
	}
	// Parse request
	// Validate workspace is a local repo
	// Run: gh repo create {owner}/{name} --{visibility} --source={workspace_path} --push
	// On success: update config entry from local:name to the new GitHub URL
	// Set up bare path
	// Return new repo URL
}
```

**Step 2: Add GET /api/github/orgs endpoint**

```go
// GET /api/github/orgs
// Response: { username: string, orgs: []string }
func (s *Server) handleGetGitHubOrgs(w http.ResponseWriter, r *http.Request) {
	// Run: gh api user/orgs --jq '.[].login'
	// Return username + org list
}
```

**Step 3: Register routes**

Add both routes to the router in `server.go`.

**Step 4: Build and verify**

Run: `go build ./cmd/schmux`
Expected: Compiles cleanly.

**Step 5: Commit**

```
feat(github): add publish-to-github and orgs API endpoints
```

---

### Task 10: Publish to GitHub — frontend UI

**Files:**

- Modify: `assets/dashboard/src/components/WorkspaceHeader.tsx`
- Modify: `assets/dashboard/src/lib/api.ts`

**Step 1: Add API functions**

```typescript
export async function getGitHubOrgs(): Promise<{ username: string; orgs: string[] }> {
  const response = await fetch('/api/github/orgs');
  if (!response.ok) return { username: '', orgs: [] };
  return response.json();
}

export async function publishToGitHub(req: {
  workspace_id: string;
  name: string;
  visibility: string;
  owner: string;
}): Promise<{ repo_url: string }> {
  const response = await fetch('/api/github/publish', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(req),
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to publish to GitHub');
  return response.json();
}
```

**Step 2: Add Publish button to WorkspaceHeader**

When `githubStatus.available` and the workspace repo starts with `local:`:

- Show a "Publish to GitHub" button in the header actions area.
- On click, show a small form/modal: repo name (default from workspace), visibility toggle (default private), owner dropdown (fetched from `/api/github/orgs`).
- On submit, call `publishToGitHub()`.
- On success, the WebSocket broadcast will update the workspace with the new repo URL.

**Step 3: Build and verify**

Run: `go run ./cmd/build-dashboard`
Expected: Builds cleanly.

**Step 4: Commit**

```
feat(dashboard): add Publish to GitHub UI for local repos
```

---

### Task 11: Integration test and final verification

**Step 1: Run all tests**

Run: `./test.sh`
Expected: All tests pass.

**Step 2: Manual smoke test**

- Start daemon with `gh` authenticated → verify "Connected as @username" on home page.
- Start daemon without `gh` → verify "GitHub: not connected" message.
- Create a local repo → verify no git polling errors in logs.
- Verify lore auto-PR is silently skipped when `gh` is not available.

**Step 3: Commit spec**

```
docs: add GitHub integration spec and implementation plan
```
