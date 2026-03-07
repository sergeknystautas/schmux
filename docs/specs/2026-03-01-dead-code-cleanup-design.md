# Dead Code Cleanup: Kill Bridge Remnants

## Problem

The model config editor (Feb 27) and kill-bridge refactor (Mar 1) replaced the old run-target bridge with a clean model-first architecture. But remnant code survived: validation functions that check "promptability" at config load time (duplicating runtime checks), vestigial UI wrappers from when multiple optgroups existed, broken tests asserting deleted behavior, and variable names that still reference the old "promptable targets" concept.

## What Gets Deleted

### Go Backend

**`internal/config/run_targets.go`:**

| What                                                | Lines   | Why                                                                                                                             |
| --------------------------------------------------- | ------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `isPromptableTarget()`                              | 134-147 | Duplicates `models.Manager.IsModel()`. Config validation shouldn't check target existence — that's a runtime concern.           |
| `validateRunTargetDependencies()`                   | 121-132 | Only exists to call `isPromptableTarget` on quick launch/nudgenik/compound.                                                     |
| `validateQuickLaunchTargets()`                      | 88-100  | Same — calls `isPromptableTarget` for existence check.                                                                          |
| Promptable validation in `validateQuickLaunch()`    | 47-62   | Remove `targets []RunTarget` parameter and prompt/promptable branching. Keep structural checks (name non-empty, no duplicates). |
| Promptable validation in `validateNudgenikConfig()` | 78-84   | Remove `targets []RunTarget` parameter and promptable checks. Keep structural checks.                                           |
| Promptable validation in `validateCompoundConfig()` | 111-118 | Remove `targets []RunTarget` parameter and promptable checks. Keep structural checks.                                           |

**`internal/config/config.go`:**

| What                                               | Lines | Why                |
| -------------------------------------------------- | ----- | ------------------ |
| `validateRunTargetDependencies()` call             | ~754  | Function deleted.  |
| `c.RunTargets` param to `validateQuickLaunch` etc. | ~751  | Parameter removed. |

**`internal/models/manager.go`:**

| What                                   | Lines   | Why                                                                                                                                       |
| -------------------------------------- | ------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `GetRunTarget` fallback in `IsModel()` | 278-280 | IsModel should only answer "is this a model or builtin tool?" — not "is this a command target?" Callers check command targets separately. **Status: NOT YET REMOVED** — the fallback still exists in `manager.go:290`. |

### Frontend

**`assets/dashboard/src/routes/config/TargetSelect.tsx`:**

| What                                | Why                                                                                                                       |
| ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `<optgroup label="Models">` wrapper | Vestigial from when there were two groups (Detected Tools + Models). Now there's only models — flatten to direct options. |

**`assets/dashboard/src/routes/SpawnPage.tsx`:**

| What                           | Why                                                                   |
| ------------------------------ | --------------------------------------------------------------------- |
| `promptableList` variable name | Rename to `availableModels`. "Promptable" was the old bridge concept. |

### Tests

| File                          | What                                                                                                                                     |
| ----------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `TargetSelect.test.tsx:32-36` | "renders only configured models" — asserts filtering that no longer exists in the component. Fix to match reality.                       |
| `run_targets_test.go`         | Delete `TestValidateRunTargetDependencies` and promptable-related test cases. Update remaining tests for simplified function signatures. |

## What Stays

- **`RunTarget` struct** (name + command) — correct, command-only
- **`run_targets` in config/API** — correct, command targets only
- **`commandTargets` naming in frontend** — correct rename from old concept
- **`isCommand` / `isPromptable` runtime checks** in AppShell.tsx and SessionTabs.tsx — correct pattern
- **`models.Manager.IsModel()`** — stays but loses the `GetRunTarget` fallback
- **`validateRunTargets()`** — stays, validates command targets structurally
- **Config migrations** — `drop_run_target_bridge_fields` and `rewrite_tool_name_targets_to_model_ids` stay (needed for old config files)
