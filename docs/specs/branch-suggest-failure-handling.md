# Spec: Branch Suggestion Failure Handling

## Problem

When branch name suggestion fails (timeout, API error, invalid response), the spawn page silently falls back to the repo's default branch (typically `main`) and proceeds to spawn. This is bad: the user ends up with sessions on `main` without realizing the suggestion failed, and has no opportunity to provide their own branch name.

Additionally, the 15-second timeout is too short given the prompt changes.

## Changes

### 1. Increase timeout (backend)

**File:** `internal/branchsuggest/branchsuggest.go`

Change `branchSuggestTimeout` from `15 * time.Second` to `30 * time.Second`.

### 2. Add hidden branch input row (frontend)

**File:** `assets/dashboard/src/routes/SpawnPage.tsx`

Add state:
```ts
const [showBranchInput, setShowBranchInput] = useState(false);
```

Add a branch input row in the grid (lines ~898-937), below the Repository row, inside the same `mode === 'fresh'` conditional. The row is only rendered when `showBranchInput` is true. Layout matches the repo row: label "Branch" in the left column, text input in the right column. Placeholder: `"e.g. feature/my-branch"`.

The input is bound to the existing `branch` state and `setBranch`.

**When branch suggestion is disabled** (`!branchSuggestTarget`): always show the branch input row (`showBranchInput` starts `true`). The user must provide a branch name — there's no auto-generation available. `validateForm` rejects empty branch in this case.

### 3. Surface the actual error reason in the toast (frontend)

**File:** `assets/dashboard/src/routes/SpawnPage.tsx`

The `generateBranchName` callback currently catches errors and returns `null`, discarding the error message. Change it to return the error message so the caller can display it:

```ts
const generateBranchName = useCallback(async (promptText: string): Promise<{ result: SuggestBranchResponse | null; error: string | null }> => {
```

The backend already returns distinct error messages (e.g. "Failed to generate branch suggestion: oneshot execute: context deadline exceeded" for timeout, or "...invalid branch name" for parse failures). Parse the JSON `error` field from the response body in the catch block and return it.

In `handleEngage`, use the returned error string in the toast rather than a generic message.

### 4. On suggestion failure: abort, reveal input (frontend)

**File:** `assets/dashboard/src/routes/SpawnPage.tsx`

In `handleEngage`, when branch suggestion fails (returns no result):

- Set `showBranchInput` to `true`
- Toast the error from the API (e.g. `"Branch suggestion failed: context deadline exceeded. Please enter a branch name."`)
- Reset `engagePhase` to `'idle'`
- Return early (do NOT proceed to spawn)

### 5. On retry: respect user-provided branch (frontend)

**File:** `assets/dashboard/src/routes/SpawnPage.tsx`

In `handleEngage`, in the fresh mode block, before calling branch suggestion:

- If `branch.trim()` is non-empty, use it as `actualBranch` directly. Set `actualNickname = nickname`. Skip suggestion entirely.
- If `branch.trim()` is empty, proceed with suggestion as before (no logic change to the suggestion call itself).

This means: if the user fills in a branch name after failure, it's used as-is. If they leave it blank and click Engage again, it retries the suggestion. The backend handles naming conflicts as it already does during workspace/branch creation.

### 6. Validation

**No client-side branch name regex validation.** The backend validates branch names during workspace creation and returns errors. The spawn error path already displays these to the user. Adding a frontend regex would create a second source of truth that can drift from the backend pattern.

### 7. Validate branch required when suggestion disabled

**File:** `assets/dashboard/src/routes/SpawnPage.tsx`

In `validateForm`, when `mode === 'fresh'` and `!branchSuggestTarget` (suggestion not configured), require `branch.trim()` to be non-empty. Toast: `"Please enter a branch name"`.

## Non-changes

- Nickname: stays blank on failure. No special handling needed.
- `getDefaultBranch()` fallback: removed from the suggestion-failure path. Only used when not in fresh mode.
- No changes to the branch suggestion API, parsing, or backend handler (other than timeout).
- No changes to session storage draft persistence (branch is not part of the draft today and doesn't need to be).
- No doc updates needed — `docs/web.md` doesn't document the branch suggestion flow at this level of detail.
