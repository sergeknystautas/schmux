# Chi Router Migration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Replace `http.ServeMux` with `go-chi/chi` so that auth/CORS/CSRF middleware is applied structurally via route groups instead of manually per-handler.

**Architecture:** Chi router groups replace manual `withCORS(withAuthAndCSRF(...))` wrapping. Routes are organized into public, authenticated, and CSRF-protected groups. Sub-routers (`handleWorkspaceRoutes`, `handleLinearSync`, `handleLoreRouter`, etc.) are replaced by explicit chi route trees. Middleware logic is unchanged — only the wrapping convention changes.

**Tech Stack:** Go 1.24, go-chi/chi/v5, existing `net/http` handler signatures.

**Design doc:** `docs/specs/chi-router-migration.md`

---

### Task 1: Add chi dependency and convert middleware signatures

**Files:**

- Modify: `go.mod`
- Modify: `internal/dashboard/auth.go:90-141`
- Modify: `internal/dashboard/server.go:597-628`
- Test: `internal/dashboard/auth_test.go`, `internal/dashboard/server_test.go`

**Step 1: Add chi dependency**

```bash
cd /Users/stefanomaz/code/workspaces/schmux-004 && go get github.com/go-chi/chi/v5
```

**Step 2: Convert `withCORS` to chi middleware signature**

In `server.go`, rename `withCORS` to `corsMiddleware` and change its signature from `func(http.HandlerFunc) http.HandlerFunc` to `func(http.Handler) http.Handler`. The body stays identical except `h(w, r)` becomes `h.ServeHTTP(w, r)`:

```go
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && !s.isAllowedOrigin(origin) {
			logging.Sub(s.logger, "daemon").Info("rejected origin", "origin", origin, "method", r.Method, "path", r.URL.Path)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			if s.config.GetAuthEnabled() || s.requiresAuth() {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

**Step 3: Convert `withAuth` to chi middleware signature**

In `auth.go`, rename `withAuth` to `authMiddleware`. Same pattern — `h(w, r)` becomes `next.ServeHTTP(w, r)`:

```go
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.requiresAuth() {
			next.ServeHTTP(w, r)
			return
		}
		if !s.authEnabled() && s.isTrustedRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		if _, err := s.authenticateRequest(r); err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

**Step 4: Extract CSRF into its own middleware**

Currently CSRF is bundled inside `withAuthAndCSRF`. Split it into a standalone `csrfMiddleware`:

```go
func (s *Server) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if !s.isTrustedRequest(r) && !s.validateCSRF(r) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
```

**Step 5: Keep old wrappers temporarily as shims**

To allow incremental migration, keep the old functions as thin wrappers around the new middleware. These will be deleted in the final task:

```go
// Deprecated: use corsMiddleware via r.Use() instead. Remove after migration.
func (s *Server) withCORS(h http.HandlerFunc) http.HandlerFunc {
	return s.corsMiddleware(http.HandlerFunc(h)).ServeHTTP
}

// Deprecated: use authMiddleware via r.Use() instead. Remove after migration.
func (s *Server) withAuth(h http.HandlerFunc) http.HandlerFunc {
	return s.authMiddleware(http.HandlerFunc(h)).ServeHTTP
}

// Deprecated: use authMiddleware + csrfMiddleware via r.Use() instead. Remove after migration.
func (s *Server) withAuthAndCSRF(h http.HandlerFunc) http.HandlerFunc {
	return s.authMiddleware(s.csrfMiddleware(http.HandlerFunc(h))).ServeHTTP
}
```

**Step 6: Run tests**

```bash
go test ./internal/dashboard/...
```

Expected: All existing tests pass. No behavior change.

**Step 7: Commit**

```
feat(dashboard): add chi dependency and convert middleware to standard signature
```

---

### Task 2: Rewrite route registration with chi router

This is the core task. Replace `http.NewServeMux()` with `chi.NewRouter()` and organize routes into middleware groups.

**Files:**

- Modify: `internal/dashboard/server.go:369-475` (the `mux` setup block in `Start()`)

**Step 1: Replace mux creation and add imports**

Add `"github.com/go-chi/chi/v5"` to imports. Replace the route registration block (lines 369–475) with the chi router structure. Keep the same handler function names for now — handler bodies are unchanged in this task.

```go
r := chi.NewRouter()

// ── Public routes (no auth, no CORS) ─────────────────
r.HandleFunc("/remote-auth", s.handleRemoteAuth)
r.HandleFunc("/auth/login", s.handleAuthLogin)
r.HandleFunc("/auth/callback", s.handleAuthCallback)
r.HandleFunc("/auth/logout", s.handleAuthLogout)

// ── WebSocket routes (inline auth + origin check) ────
r.HandleFunc("/ws/terminal/{id}", s.handleTerminalWebSocket)
r.HandleFunc("/ws/provision/{id}", s.handleProvisionWebSocket)
r.HandleFunc("/ws/dashboard", s.handleDashboardWebSocket)

// ── App shell + static assets ────────────────────────
if s.devProxy {
	viteProxy := createDevProxyHandler("http://localhost:5173")
	r.Handle("/*", viteProxy)
	s.logger.Info("dev-proxy enabled: proxying to Vite", "target", "http://localhost:5173")
} else {
	r.Handle("/assets/*", s.withAuthHandler(
		http.StripPrefix("/assets/", http.FileServer(http.Dir(filepath.Join(s.getDashboardDistPath(), "assets")))),
	))
	r.HandleFunc("/*", s.handleApp)
}

// ── API routes (CORS + Auth by default) ──────────────
r.Route("/api", func(r chi.Router) {
	r.Use(s.corsMiddleware)
	r.Use(s.authMiddleware)

	// -- Read-only endpoints (no CSRF) --
	r.Get("/healthz", s.handleHealthz)
	r.Get("/sessions", s.handleSessions)
	r.Get("/recent-branches", s.handleRecentBranches)
	r.Get("/detect-tools", s.handleDetectTools)
	r.Get("/models", s.handleModels)
	r.Get("/builtin-quick-launch", s.handleBuiltinQuickLaunch)
	r.Get("/commit/prompt", s.handleCommitPrompt)
	r.Get("/diff/{id}", s.handleDiff)
	r.Get("/file/*", s.handleFile)
	r.Get("/overlays", s.handleOverlays)
	r.Get("/prs", s.handlePRs)
	r.Get("/hasNudgenik", s.handleHasNudgenik)
	r.Get("/askNudgenik/{query}", s.handleAskNudgenik)
	r.Get("/auth/me", s.handleAuthMe)
	r.Get("/remote/hosts", s.handleRemoteHosts)
	r.Get("/remote/hosts/connect/stream", s.handleRemoteConnectStream)
	r.Get("/remote/flavor-statuses", s.handleRemoteFlavorStatuses)
	r.Get("/remote-access/status", s.handleRemoteAccessStatus)

	// -- State-changing endpoints (CORS + Auth + CSRF) --
	r.Group(func(r chi.Router) {
		r.Use(s.csrfMiddleware)

		r.Post("/spawn", s.handleSpawnPost)
		r.Post("/update", s.handleUpdate)
		r.Post("/workspaces/scan", s.handleWorkspacesScan)
		r.Post("/suggest-branch", s.handleSuggestBranch)
		r.Post("/prepare-branch-spawn", s.handlePrepareBranchSpawn)
		r.Post("/check-branch-conflict", s.handleCheckBranchConflict)
		r.Post("/recent-branches/refresh", s.handleRecentBranchesRefresh)
		r.Post("/commit/generate", s.handleCommitGenerate)
		r.Post("/overlays/scan", s.handleOverlayScan)
		r.Post("/overlays/add", s.handleOverlayAdd)
		r.Post("/overlays/dismiss-nudge", s.handleDismissNudge)
		r.Post("/prs/refresh", s.handlePRRefresh)
		r.Post("/prs/checkout", s.handlePRCheckout)
		r.Post("/remote/hosts/connect", s.handleRemoteHostConnect)
		r.Post("/remote-access/on", s.handleRemoteAccessOn)
		r.Post("/remote-access/off", s.handleRemoteAccessOff)
		r.Post("/remote-access/set-password", s.handleRemoteAccessSetPassword)
		r.Post("/remote-access/test-notification", s.handleRemoteAccessTestNotification)

		// Sessions
		r.Post("/sessions/{id}/dispose", s.handleDispose)
		r.Put("/sessions-nickname/{id}", s.handleUpdateNickname)

		// Config
		r.Get("/config", s.handleConfigGet)
		r.Put("/config", s.handleConfigUpdate)
		r.Put("/auth/secrets", s.handleAuthSecretsUpdate)
		r.Get("/auth/secrets", s.handleAuthSecretsGet)

		// Models sub-routes
		r.Get("/models/{name}/configured", s.handleModelConfigured)
		r.Post("/models/{name}/secrets", s.handleModelSecretsPost)
		r.Delete("/models/{name}/secrets", s.handleModelSecretsDelete)

		// Diff / file actions
		r.Post("/diff-external/{id}", s.handleDiffExternal)
		r.Post("/open-vscode/{path}", s.handleOpenVSCode)

		// Remote flavors
		r.Get("/config/remote-flavors", s.handleRemoteFlavorsGet)
		r.Post("/config/remote-flavors", s.handleRemoteFlavorsCreate)
		r.Get("/config/remote-flavors/{id}", s.handleRemoteFlavorGet)
		r.Put("/config/remote-flavors/{id}", s.handleRemoteFlavorUpdate)
		r.Delete("/config/remote-flavors/{id}", s.handleRemoteFlavorDelete)

		// Remote hosts
		r.Post("/remote/hosts/{id}/reconnect", s.handleRemoteHostReconnect)
		r.Post("/remote/hosts/{id}/disconnect", s.handleRemoteHostDisconnect)

		// Workspaces sub-routes
		r.Route("/workspaces/{workspaceID}", func(r chi.Router) {
			// Previews
			r.Get("/previews", s.handlePreviewsList)
			r.Post("/previews", s.handlePreviewsCreate)
			r.Delete("/previews/{previewID}", s.handlePreviewsDelete)

			// Git operations
			r.Get("/git-graph", s.handleWorkspaceGitGraph)
			r.Get("/git-commit/{hash}", s.handleWorkspaceGitCommit)
			r.Post("/linear-sync-from-main", s.handleLinearSyncFromMain)
			r.Post("/linear-sync-to-main", s.handleLinearSyncToMain)
			r.Post("/push-to-branch", s.handlePushToBranch)
			r.Post("/linear-sync-resolve-conflict", s.handleLinearSyncResolveConflict)
			r.Delete("/linear-sync-resolve-conflict-state", s.handleDeleteLinearSyncResolveConflictState)
			r.Post("/git-commit-stage", s.handleGitCommitStage)
			r.Post("/git-amend", s.handleGitAmend)
			r.Post("/git-discard", s.handleGitDiscard)
			r.Post("/git-uncommit", s.handleGitUncommit)
			r.Post("/refresh-overlay", s.handleRefreshOverlay)
			r.Post("/dispose", s.handleDisposeWorkspace)
			r.Post("/dispose-all", s.handleDisposeWorkspaceAll)
		})

		// Lore sub-routes
		r.Get("/lore/status", s.handleLoreStatus)
		r.Get("/lore/{repo}/proposals", s.handleLoreProposals)
		r.Get("/lore/{repo}/proposals/{proposalID}", s.handleLoreProposalGet)
		r.Post("/lore/{repo}/proposals/{proposalID}/apply", s.handleLoreApply)
		r.Post("/lore/{repo}/proposals/{proposalID}/dismiss", s.handleLoreDismiss)
		r.Get("/lore/{repo}/entries", s.handleLoreEntries)
		r.Post("/lore/{repo}/curate", s.handleLoreCurate)
	})

	// Dev-mode routes
	if s.devMode {
		r.Get("/dev/status", s.handleDevStatus)
		r.Group(func(r chi.Router) {
			r.Use(s.csrfMiddleware)
			r.Post("/dev/rebuild", s.handleDevRebuild)
			r.Post("/dev/simulate-tunnel", s.handleDevSimulateTunnel)
			r.Post("/dev/simulate-tunnel-stop", s.handleDevSimulateTunnelStop)
			r.Post("/dev/clear-password", s.handleDevClearPassword)
			r.Post("/dev/diagnostic-append", s.handleDiagnosticAppend)
		})
	}
})
```

Set `s.httpServer.Handler = r` (instead of `mux`).

**Step 2: Run tests**

```bash
go test ./internal/dashboard/...
```

Expected: Tests that call handlers directly still pass. Tests that create a full server and hit routes via HTTP may need URL adjustments (e.g., if they relied on trailing-slash redirects).

**Step 3: Commit**

```
feat(dashboard): rewrite route registration with chi router groups
```

**Important notes for this task:**

- Many handler names in the route tree above don't exist yet (e.g., `handleConfigGet`, `handleConfigUpdate`, `handleModelConfigured`). These are created by splitting multi-method handlers in Task 4. Until then, use the existing handler names and `HandleFunc` (which accepts any method) for routes that still need multi-method dispatch.
- The route tree above is the **target state**. During this task, use `r.HandleFunc(...)` for any route whose handler hasn't been split yet. Replace with `r.Get`/`r.Post`/etc. as handlers are split in Task 4.

---

### Task 3: Replace `extractPathSegment` / `strings.TrimPrefix` with `chi.URLParam`

All 14 `extractPathSegment` calls and 10 `strings.TrimPrefix` path-extraction calls become `chi.URLParam(r, "paramName")`. The param names come from the chi route patterns registered in Task 2.

**Files:**

- Modify: `internal/dashboard/handlers.go` (delete `extractPathSegment` function, update `handleUpdateNickname`, `handleAskNudgenik`)
- Modify: `internal/dashboard/handlers_dispose.go` (3 calls)
- Modify: `internal/dashboard/handlers_git.go` (5 calls)
- Modify: `internal/dashboard/handlers_sync.go` (5 calls)
- Modify: `internal/dashboard/handlers_overlay.go` (1 call)
- Modify: `internal/dashboard/handlers_diff.go` (3 calls using `strings.TrimPrefix`)
- Modify: `internal/dashboard/handlers_models.go` (1 call using `strings.TrimPrefix`)
- Modify: `internal/dashboard/handlers_remote.go` (3 calls using `strings.TrimPrefix`)
- Modify: `internal/dashboard/handlers_workspace.go` (path parsing in `handleWorkspacePreviews`)
- Modify: `internal/dashboard/handlers_lore.go` (path parsing in `handleLoreRouter`)

**Step 1: Add chi import to each handler file**

```go
import "github.com/go-chi/chi/v5"
```

**Step 2: Replace each extraction call**

Pattern: `extractPathSegment(r.URL.Path, "/api/workspaces/", "/git-graph")` becomes `chi.URLParam(r, "workspaceID")`.

Example in `handlers_dispose.go`:

```go
// Before:
sessionID := extractPathSegment(r.URL.Path, "/api/sessions/", "/dispose")

// After:
sessionID := chi.URLParam(r, "id")
```

Example in `handlers_lore.go` (sub-router was path-parsing manually):

```go
// Before:
path := strings.TrimPrefix(r.URL.Path, "/api/lore/")
parts := strings.Split(path, "/")
repoName := parts[0]

// After:
repoName := chi.URLParam(r, "repo")
```

**Step 3: Delete `extractPathSegment` from `handlers.go:22-28`**

**Step 4: Delete `parseWorkspacePreviewPath` from `handlers_workspace.go:154-165`**

Replaced by chi params `workspaceID` and `previewID`.

**Step 5: Run tests**

```bash
go test ./internal/dashboard/...
```

Expected: PASS. Existing tests that call handlers directly via `httptest.NewRequest` will need the chi URL params injected. Use `chi.NewRouteContext()`:

```go
rctx := chi.NewRouteContext()
rctx.URLParams.Add("id", "test-session-id")
req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
```

Update any tests that construct requests to handlers expecting URL params.

**Step 6: Commit**

```
refactor(dashboard): replace manual path extraction with chi.URLParam
```

---

### Task 4: Split multi-method handlers

8 handlers currently use `switch r.Method` to handle multiple HTTP methods in one function. Split each into single-method handlers registered via `r.Get`/`r.Post`/`r.Delete`.

**Files:**

- Modify: `internal/dashboard/handlers_config.go` (`handleConfig` → `handleConfigGet` + `handleConfigUpdate`)
- Modify: `internal/dashboard/handlers_config.go` (`handleAuthSecrets` → `handleAuthSecretsGet` + `handleAuthSecretsUpdate`)
- Modify: `internal/dashboard/handlers_remote.go` (`handleRemoteFlavors` → `handleRemoteFlavorsGet` + `handleRemoteFlavorsCreate`)
- Modify: `internal/dashboard/handlers_remote.go` (`handleRemoteFlavor` → `handleRemoteFlavorGet` + `handleRemoteFlavorUpdate` + `handleRemoteFlavorDelete`)
- Modify: `internal/dashboard/handlers_remote.go` (`handleRemoteHostRoute` → `handleRemoteHostReconnect` + `handleRemoteHostDisconnect` — these already exist as separate functions, just need direct registration)
- Modify: `internal/dashboard/handlers_models.go` (`handleModel` → `handleModelConfigured` + `handleModelSecretsPost` + `handleModelSecretsDelete`)
- Modify: `internal/dashboard/handlers_workspace.go` (`handleWorkspacePreviews` → `handlePreviewsList` + `handlePreviewsCreate` + `handlePreviewsDelete`)
- Modify: `internal/dashboard/handlers_remote_auth.go` (`handleRemoteAuth` — this is a public route, split GET/POST)

**Step 1: Split each handler**

For each multi-method handler, extract the method-specific branches into standalone functions. Example for `handleConfig`:

```go
// Before (single function with switch):
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodGet:
        // ... get logic ...
    case http.MethodPost, http.MethodPut:
        // ... update logic ...
    default:
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
    }
}

// After (two functions):
func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
    // ... get logic (copied from GET case) ...
}

func (s *Server) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
    // ... update logic (copied from POST/PUT case) ...
}
```

**Step 2: Update route registration in `server.go`**

Replace any remaining `r.HandleFunc(...)` calls for these handlers with the correct `r.Get`/`r.Post`/`r.Put`/`r.Delete` calls pointing to the new split handler names. This completes the target route tree from Task 2.

**Step 3: Run tests**

```bash
go test ./internal/dashboard/...
```

Update any tests that called the old combined handler directly.

**Step 4: Commit**

```
refactor(dashboard): split multi-method handlers into single-method functions
```

---

### Task 5: Remove manual method checks from single-method handlers

~18 handlers have `if r.Method != http.MethodPost` guards that are now redundant because chi only routes matching methods.

**Files:**

- Modify: `internal/dashboard/handlers.go` (`handleHealthz`, `handleUpdateNickname`)
- Modify: `internal/dashboard/handlers_spawn.go` (`handleSpawnPost`, `handleSuggestBranch`, `handleBuiltinQuickLaunch`, `handleCheckBranchConflict`, `handleRecentBranches`, `handleRecentBranchesRefresh`)
- Modify: `internal/dashboard/handlers_dispose.go` (`handleDispose`, `handleDisposeWorkspace`, `handleDisposeWorkspaceAll`)
- Modify: `internal/dashboard/handlers_sessions.go` (`handleSessions`)
- Modify: `internal/dashboard/handlers_remote_access.go` (all 4 handlers)
- Modify: `internal/dashboard/handlers_remote.go` (`handleRemoteHostConnect`, `handleRemoteHostReconnect`)
- Modify: `internal/dashboard/handlers_sync.go` (remove method checks in individual sync handlers)
- Modify: `internal/dashboard/handlers_git.go` (remove method checks)

**Step 1: Remove method guard boilerplate**

For each handler, delete the method-check block. Example:

```go
// Before:
func (s *Server) handleSpawnPost(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }
    // ... rest of handler ...
}

// After:
func (s *Server) handleSpawnPost(w http.ResponseWriter, r *http.Request) {
    // ... rest of handler ...
}
```

**Step 2: Run tests**

```bash
go test ./internal/dashboard/...
```

Expected: PASS. Any tests that were testing "wrong method returns 405" should be updated to test via the router (chi returns 405 automatically) rather than calling the handler directly.

**Step 3: Commit**

```
refactor(dashboard): remove redundant method checks (chi handles method routing)
```

---

### Task 6: Delete dead sub-routers and old middleware wrappers

The manual sub-routers and shim wrappers from Task 1 are now unused.

**Files:**

- Modify: `internal/dashboard/handlers_workspace.go` (delete `handleWorkspaceRoutes`, `handleWorkspacePreviews` — replaced by split handlers and chi routing)
- Modify: `internal/dashboard/handlers_sync.go` (delete `handleLinearSync` dispatcher — replaced by direct chi routes)
- Modify: `internal/dashboard/handlers_lore.go` (delete `handleLoreRouter` dispatcher — replaced by direct chi routes)
- Modify: `internal/dashboard/handlers_models.go` (delete `handleModel` dispatcher — replaced by split handlers)
- Modify: `internal/dashboard/handlers_remote.go` (delete `handleRemoteHostRoute` dispatcher — replaced by direct chi routes)
- Modify: `internal/dashboard/server.go` (delete old `withCORS` shim)
- Modify: `internal/dashboard/auth.go` (delete old `withAuth`, `withAuthAndCSRF`, `withAuthHandler` shims)

**Step 1: Delete each dead function**

Search for all references before deleting to confirm they're unused. The compiler will catch any missed references.

**Step 2: Run tests**

```bash
go test ./internal/dashboard/...
```

Expected: PASS. Compilation confirms nothing references the deleted functions.

**Step 3: Commit**

```
refactor(dashboard): delete dead sub-routers and old middleware wrappers
```

---

### Task 7: Full integration test pass

**Step 1: Run the full test suite**

```bash
./test.sh --all
```

This runs unit tests, E2E tests, and scenario tests (Playwright). The scenario tests exercise real HTTP routes end-to-end, which validates that the chi routing matches the previous `ServeMux` behavior.

**Step 2: Manual smoke test**

```bash
go build ./cmd/schmux && ./schmux daemon-run
```

Open `http://localhost:7337` and verify:

- Dashboard loads
- Sessions list loads (`GET /api/sessions`)
- Spawn form works (`POST /api/spawn`)
- WebSocket terminal connects (`/ws/terminal/{id}`)
- Config page loads and saves (`GET/PUT /api/config`)

**Step 3: Commit (if any fixes were needed)**

```
fix(dashboard): address integration issues from chi migration
```

---

## Task dependency order

```
Task 1 (middleware signatures)
  └─► Task 2 (route registration)
        ├─► Task 3 (URL params)
        └─► Task 4 (split handlers)
              └─► Task 5 (remove method checks)
                    └─► Task 6 (delete dead code)
                          └─► Task 7 (integration test)
```

Tasks 3 and 4 can be done in parallel after Task 2. Tasks 5-7 are sequential.
