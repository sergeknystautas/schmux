VERDICT: NEEDS_REVISION

## Summary Assessment

The plan is structurally sound and correctly mirrors the persona subsystem patterns, but has two critical issues: it uses `git commit` directly instead of the mandated `/commit` command (a CLAUDE.md hard requirement), and it entirely omits updating `docs/api.md` for the new `/api/styles` endpoints (which will cause CI to fail via `scripts/check-api-docs.sh`). Several steps also lack the TDD cycle for non-trivial code.

## Critical Issues (must fix)

### 1. Every commit step uses `git commit` directly -- CLAUDE.md forbids this

CLAUDE.md line 113 states: **"ALWAYS use `/commit` to create commits. NEVER run `git commit` directly."** The `/commit` command runs `./test.sh`, checks `docs/api.md`, and requires a self-assessment. Every commit step in the plan (1e, 2e, 3c, 4c, 5c, 6d, 7f, 8c, 9b, 10b, 11b, 12c, 13e, 14b, 15e, 16e) must use `/commit` instead of `git commit`.

This is not cosmetic -- `/commit` runs `./test.sh` before committing, which catches compilation errors and test failures early. Without it, broken intermediate commits can accumulate.

### 2. Missing `docs/api.md` update for new API endpoints

The plan adds five REST endpoints (`GET/POST /api/styles`, `GET/PUT/DELETE /api/styles/{id}`), modifies the `SpawnRequest` struct, and adds `comm_styles` to `ConfigUpdateRequest`/`ConfigResponse`. CI runs `scripts/check-api-docs.sh` which checks that any changes to `internal/dashboard/`, `internal/config/`, `internal/state/`, or `internal/session/` are accompanied by a `docs/api.md` update. The plan touches all four of those directories but never mentions `docs/api.md`.

The `/commit` command will also catch this, but the plan should include an explicit step to update `docs/api.md` with the new style endpoints, the `style_id` spawn parameter, and the `comm_styles` config field.

### 3. Step 4 has no tests for the CRUD handlers

Step 4 creates `handlers_styles.go` with five handler functions and only verifies compilation (`go build`). The existing `handlers_personas.go` is a direct analog, and while it also lacks dedicated handler tests, that is not a reason to repeat the omission. Handler tests are especially important here because:

- The `handleCreateStyle` handler has validation logic (reserved IDs "create" and "none", slug format regex, required fields) that is not exercised by any test.
- The `handleDeleteStyle` handler has branching behavior (reset vs. delete) based on `BuiltIn` flag.
- The plan's `validStyleID` regex differs from persona's `validPersonaID` regex only in variable name -- this is correct but should be verified by tests.

Add handler-level tests similar to the pattern in `handlers_spawn_test.go` or `handlers_workspace_test.go`.

### 4. Step 5 is incomplete -- no implementation detail for config update handler wiring

Step 5 says "Wire up the update handler in `handleConfigUpdate` to apply `CommStyles` when non-nil" but provides no code for this. Looking at `handlers_config.go`, this handler is a 500+ line function with field-by-field update application. The plan needs to specify:

- The exact code block to add (similar to the `EnabledModels` pattern at line 697-699):
  ```go
  if req.CommStyles != nil {
      cfg.CommStyles = *req.CommStyles
  }
  ```
- Where in the handler to add it.
- The corresponding line in `handleGetConfig` that returns `CommStyles` in the response.

Without this, the config page UI (Step 14) will have no working backend to save to.

### 5. Step 5 also omits `ConfigResponse` field for `comm_styles`

The plan says to add `CommStyles` to `ConfigResponse` in contracts, but does not show the corresponding code in `handleGetConfig` (in `handlers_config.go`) that populates this field from `s.config.GetCommStyles()`. Without this, the frontend cannot read the current comm_styles defaults.

## Suggestions (nice to have)

### 1. Step sizes are mostly appropriate but Step 2 is large

Step 2 creates `manager.go`, `builtins.go`, and 25 YAML files. Writing 25 YAML files with unique, quality prompts is easily 15-20 minutes of work by itself -- well beyond the 2-5 minute target. Consider splitting: one step for the manager + builtins infrastructure with 2-3 sample styles, and a separate step for the remaining 22-23 styles.

### 2. Step 7 line number references are inaccurate

The plan references specific line numbers (e.g., "~line 44" for `SpawnRequest`, "~line 295", "~line 818", "~line 829") but these are approximate. For example:

- `SpawnRequest.PersonaID` is at line 43, not 44 -- close enough.
- `formatPersonaPrompt` is at line 818-827 -- correct.
- The persona resolution block is at line 295-303 -- correct.

These are approximately right and will serve as guidance, but the implementer should search by symbol name rather than relying on line numbers.

### 3. No frontend tests for style pages (Steps 9, 11)

Steps 9 and 11 create `StyleForm.tsx`, `StylesListPage.tsx`, `StyleCreatePage.tsx`, and `StyleEditPage.tsx` without any test files. The persona pages have a test (`PersonasListPage.test.tsx`). Parity would suggest at least a basic render test for the styles list page.

### 4. Step 13 should reset `selectedStyleId` when navigating between spawn modes

The plan adds `selectedStyleId` state but does not reset it when switching between fresh/workspace modes or when changing the selected agent. If a user selects a style, then switches to a different agent type, the stale `selectedStyleId` persists. The persona dropdown has the same limitation (pre-existing), so this is not blocking, but worth noting.

### 5. Step 15 modifies `SpawnRemote` signature -- callers need updating

The plan refactors `SpawnRemote` from positional parameters to an options struct. The current call site is at `handlers_spawn.go:353`. But the plan should verify there are no other callers of `SpawnRemote` (e.g., in CLI code or tests). A search shows it is also called from `cmd/schmux/spawn.go` potentially. If there are other callers, they all need updating.

### 6. The `"none"` reserved ID check in `handleCreateStyle` is good but should also block update-to-"none"

The create handler rejects `id: "none"` but since IDs are immutable (set at create time), this is sufficient. However, worth confirming no path allows renaming a style's ID to "none" via update -- the current `StyleUpdateRequest` does not include an `ID` field, so this is already safe.

### 7. Step 7d renames persona file path but does not address backward compatibility

The plan renames `.schmux/persona-{sessionID}.md` to `.schmux/system-prompt-{sessionID}.md`. Any running sessions that were spawned with the old filename will have stale references. Since this path is only written at spawn time and the file is consumed immediately by the agent, there is no backward compatibility issue -- but the plan should note this explicitly to avoid confusion.

## Verified Claims (things I confirmed are correct)

1. **Module path is correct.** `github.com/sergeknystautas/schmux` matches the import paths used in the plan's code.

2. **Persona package structure matches.** `internal/persona/` has exactly `parse.go`, `parse_test.go`, `manager.go`, `manager_test.go`, `builtins.go`, and `builtins/*.yaml` -- the style package mirrors this correctly.

3. **`internal/style/` does not exist yet.** Confirmed via glob -- the directory is new.

4. **`personaManager` field and initialization pattern are accurate.** Line 215 has the field, line 340-343 initializes it -- the plan's instructions to add `styleManager` in the same locations are correct.

5. **Persona routes are at line 703-708.** The plan's instruction to add style routes "after persona routes" is accurately placed.

6. **`SpawnRemote` current signature matches.** It takes 6 positional string arguments at line 390, confirming the plan's refactoring scope.

7. **`ResolveTargetToTool()` exists and works.** Confirmed at `models/manager.go:384-387`.

8. **`maxBodySize` constant exists.** Defined at `handlers.go:175` as `1 << 20`. The plan's handlers correctly reference it.

9. **`chi.URLParam` usage is correct.** The persona handlers use the same pattern for extracting URL params.

10. **`SpawnRequest` struct at lines 28-45 matches.** The plan correctly identifies `PersonaID` at line 43 and proposes adding `StyleID` after it.

11. **Session state has `PersonaID` at line 326.** The plan correctly proposes adding `StyleID` after it.

12. **`formatPersonaPrompt` at lines 818-827 matches.** The function signature and body match the plan's description of what to rename/extend.

13. **Spawn handler test file exists.** `SpawnPage.agent-select.test.tsx` lines 320-324 assert persona-select inside agent-repo-row, confirming the test update described in Step 13d.

14. **Gen-types `rootTypes` array matches.** The persona entries are at lines 35-37, confirming the plan's insertion point for style types.

15. **`ToolsSection.tsx` `menuItems` array exists at line 88.** The Personas entry is at line 117-119. The plan's instruction to add "Comm Styles" after it is correctly placed.

16. **Frontend API client patterns match.** `apiFetch`, `parseErrorResponse`, and `csrfHeaders` are all available and used consistently in the existing persona API functions.

17. **ConfigUpdateRequest and ConfigResponse exist** at the expected locations in `internal/api/contracts/config.go`. Adding `CommStyles` fields follows the established pattern (e.g., `EnabledModels`).

18. **Both remote session creation paths** (queued/provisioning at line 502-515 and immediate at line 606-618) need `PersonaID`/`StyleID` added. The plan only explicitly shows the immediate path -- the queued path also needs updating but may inherit from the same options struct.
