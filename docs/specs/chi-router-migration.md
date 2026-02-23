# Chi Router Migration: Auth-by-Default Middleware

## Problem

Auth/CORS/CSRF protection is applied per-route via manual wrapping:

```go
mux.HandleFunc("/api/spawn", s.withCORS(s.withAuthAndCSRF(s.handleSpawnPost)))
```

This is repeated ~50 times in `server.go`. Every new route requires the developer to remember the correct wrapper combination. Forgetting it silently leaves the route unprotected. There's no compile-time or test-time safety net — only code review catches the omission.

## Solution

Replace `http.ServeMux` with [`go-chi/chi`](https://github.com/go-chi/chi) and organize routes into middleware groups where auth is structural (inherited by group membership) rather than manual (per-handler wrapping).

## Why chi

Chi is a lightweight router (~1,500 LOC) that implements `http.Handler` and uses the standard `http.HandlerFunc` signature. It adds two capabilities the stdlib lacks:

1. **Middleware groups** — middleware applied to a group automatically covers every route in it.
2. **Method routing + URL params** — `r.Get(...)`, `r.Post(...)`, and `{id}` path parameters, eliminating manual `r.Method` checks and `extractPathSegment` calls.

Chi has no dependencies beyond the stdlib. It's the most widely used Go router outside `net/http`.

## Design

### Route structure

```go
r := chi.NewRouter()

// ── Public routes (no auth) ──────────────────────────
r.HandleFunc("/remote-auth", s.handleRemoteAuth)
r.HandleFunc("/auth/login", s.handleAuthLogin)
r.HandleFunc("/auth/callback", s.handleAuthCallback)
r.HandleFunc("/auth/logout", s.handleAuthLogout)

// ── WebSocket routes (inline auth + origin check) ────
r.HandleFunc("/ws/terminal/{id}", s.handleTerminalWebSocket)
r.HandleFunc("/ws/provision/{id}", s.handleProvisionWebSocket)
r.HandleFunc("/ws/dashboard", s.handleDashboardWebSocket)

// ── App shell + static assets ────────────────────────
// (handleApp does its own redirect-to-login logic)
if s.devProxy {
    r.Handle("/*", viteProxy)
} else {
    r.Handle("/assets/*", s.withAuthHandler(staticFileServer))
    r.HandleFunc("/*", s.handleApp)
}

// ── API routes (auth-by-default) ─────────────────────
r.Route("/api", func(r chi.Router) {
    r.Use(s.corsMiddleware)
    r.Use(s.authMiddleware)

    // Read-only endpoints (CORS + Auth only)
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
    r.Get("/remote/hosts", s.handleRemoteHosts)
    r.Get("/remote/hosts/connect/stream", s.handleRemoteConnectStream)
    r.Get("/remote/flavor-statuses", s.handleRemoteFlavorStatuses)
    r.Get("/remote-access/status", s.handleRemoteAccessStatus)

    // State-changing endpoints (CORS + Auth + CSRF)
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

        // Routes with path params and/or multiple methods
        r.Route("/sessions/{id}", func(r chi.Router) {
            r.Delete("/dispose", s.handleSessionDispose)
        })
        r.Put("/sessions-nickname/{id}", s.handleUpdateNickname)
        r.Route("/workspaces/{id}", func(r chi.Router) {
            r.Delete("/dispose", s.handleWorkspaceDispose)
            r.Get("/git-graph", s.handleGitGraph)
            r.Post("/linear-sync-from-main", s.handleLinearSyncFromMain)
            // ... other workspace sub-routes
        })
        r.Route("/config", func(r chi.Router) {
            r.Get("/", s.handleConfigGet)
            r.Put("/", s.handleConfigUpdate)
            r.Get("/remote-flavors", s.handleRemoteFlavors)
            r.Route("/remote-flavors/{id}", func(r chi.Router) {
                r.Put("/", s.handleRemoteFlavorUpdate)
                r.Delete("/", s.handleRemoteFlavorDelete)
            })
        })
        r.Put("/auth/secrets", s.handleAuthSecrets)
        r.Put("/models/{id}", s.handleModel)
        r.Post("/diff-external/{id}", s.handleDiffExternal)
        r.Post("/open-vscode/{path}", s.handleOpenVSCode)
        r.HandleFunc("/lore/*", s.handleLoreRouter)
        r.HandleFunc("/remote/hosts/{id}", s.handleRemoteHostRoute)
    })

    // Dev-mode routes (conditionally registered)
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

### Middleware refactoring

The existing `withCORS`, `withAuth`, and `withAuthAndCSRF` functions become chi-compatible middleware (signature: `func(http.Handler) http.Handler`). The logic stays identical — only the wrapping convention changes.

```go
// Before: wraps http.HandlerFunc, returns http.HandlerFunc
func (s *Server) withAuth(h http.HandlerFunc) http.HandlerFunc { ... }

// After: wraps http.Handler, returns http.Handler (chi middleware signature)
func (s *Server) authMiddleware(next http.Handler) http.Handler { ... }
```

The `withAuthAndCSRF` function splits into two independent middleware — `authMiddleware` and `csrfMiddleware` — composed via `r.Use()` at the group level. This is cleaner than the current nesting where CSRF is bundled inside auth.

The old `withAuth`, `withCORS`, `withAuthAndCSRF` wrapper functions are deleted. `withAuthHandler` is also deleted since chi middleware works with `http.Handler` natively.

### Handler cleanup

With chi handling method dispatch and path parameters:

- **Remove manual method checks.** The ~18 handlers with `if r.Method != http.MethodPost` guards no longer need them — chi rejects wrong methods with 405 automatically.
- **Remove `extractPathSegment`.** The ~11 call sites become `chi.URLParam(r, "id")`.
- **Remove `isValidResourceID` at extraction sites.** Move validation into a shared chi middleware or keep it where it is — either way, the path param extraction gets simpler.
- **Split multi-method handlers.** Handlers like `handleConfig` (GET + PUT in a switch) become `handleConfigGet` and `handleConfigUpdate`, registered separately. This makes each handler single-purpose.

### WebSocket routes stay unchanged

WebSocket endpoints (`/ws/terminal/`, `/ws/provision/`, `/ws/dashboard`) continue to do inline authentication. WebSocket upgrades don't use CORS headers, and the auth check must happen before the upgrade. Chi's `{id}` parameter extraction still applies (replacing `extractPathSegment`), but the inline auth pattern is correct and doesn't change.

## What changes

| Aspect | Before | After |
|--------|--------|-------|
| New API route | Write handler, wrap with `withCORS(withAuthAndCSRF(...))`, register | Write handler, add `r.Post(...)` inside the right group |
| Forgetting auth | Silent — route is unprotected | Impossible — group middleware applies automatically |
| Method validation | Manual `if r.Method !=` in each handler | Chi returns 405 automatically |
| Path parameters | `extractPathSegment(r.URL.Path, "/api/sessions/", "/dispose")` | `chi.URLParam(r, "id")` |
| Middleware signature | `func(http.HandlerFunc) http.HandlerFunc` (custom) | `func(http.Handler) http.Handler` (standard) |
| CORS + Auth + CSRF | Single combined wrapper per route | Composed via `r.Use()` at group level |
| Route table readability | Flat list, security level hidden in wrapper | Indented groups, security level visible from structure |

## What doesn't change

- **Auth logic.** The `requiresAuth()`, `authenticateRequest()`, `isTrustedRequest()`, `isAllowedOrigin()`, and `validateCSRF()` functions are untouched.
- **Handler business logic.** Only the method-check and path-extraction boilerplate is removed.
- **WebSocket auth.** Stays inline in the WS handlers.
- **Frontend.** No API contract changes — same paths, same methods, same headers.
- **Cookie/session management.** Unchanged.

## New dependency

```
github.com/go-chi/chi/v5
```

Chi v5 requires Go 1.14+ (schmux is on 1.24). It has zero transitive dependencies. The module is actively maintained and widely used (~18k GitHub stars).

## Migration strategy

1. Add `chi/v5` dependency.
2. Convert the three middleware functions to chi signature.
3. Rewrite route registration in `Server.Start()` using the group structure above.
4. Remove manual method checks from handlers (one handler at a time, testable independently).
5. Replace `extractPathSegment` calls with `chi.URLParam`.
6. Split multi-method handlers (e.g., `handleConfig` → `handleConfigGet` + `handleConfigUpdate`).
7. Delete `withCORS`, `withAuth`, `withAuthAndCSRF`, `withAuthHandler`, and `extractPathSegment`.

Steps 2-3 are the core change. Steps 4-7 are cleanup that can happen incrementally.

## Risks

- **Route ordering.** Chi uses first-match routing. The current `ServeMux` uses longest-prefix-match. Routes like `/api/workspaces/scan` vs `/api/workspaces/{id}` need to be registered with the more specific pattern first, or use chi's sub-routing to disambiguate. This is the most likely source of subtle bugs during migration and needs careful testing.
- **Trailing slash behavior.** `ServeMux` auto-redirects `/api/sessions` to `/api/sessions/` for prefix patterns. Chi doesn't. Some frontend calls may need adjustment if they rely on trailing-slash redirects (though chi has a `middleware.RedirectSlashes` option).
- **Wildcard differences.** `ServeMux` pattern `/api/workspaces/` matches any path starting with that prefix. Chi requires explicit `/*` or `/{param}`. Handlers that parse sub-paths manually (like `handleWorkspaceRoutes`) need their sub-paths registered explicitly.
