# Spawn Image Attachments Design

**Date:** 2026-02-26
**Branch:** feature/spawn-image-attachments

## Problem

Users want to provide visual context (screenshots, mockups, diagrams) when spawning AI agent sessions. Currently, images can only be pasted into running terminal sessions via the clipboard paste feature. There's no way to include images as part of the initial spawn prompt.

## Solution

Add image attachment support to the spawn flow. Users paste images from their clipboard while on the SpawnPage. Images are sent as base64 data in the SpawnRequest, written to the workspace filesystem, and their absolute paths are appended to the prompt so the spawned agent can reference them.

## Design

### Frontend: Paste Capture & State

**Paste handling on SpawnPage:**

- `paste` event listener on the SpawnPage intercepts clipboard images
- Extract image blobs from `ClipboardEvent.clipboardData.items` (filter `image/*`)
- Convert to base64 using the same pattern as `terminalStream.ts:pasteImageToSession`
- Store in SpawnDraft as `imageAttachments: string[]` (max 5)
- If at max capacity, silently ignore additional pastes

**Attachment list below PromptTextarea:**

- Simple text entries: "Image 1 x", "Image 2 x", etc.
- Each entry has a remove button
- No thumbnails or image previews — just labels

**SpawnDraft changes:**

```typescript
interface SpawnDraft {
  // ...existing fields...
  imageAttachments?: string[]; // base64-encoded PNGs
}
```

Images persist in session storage as part of the draft (survives page navigation).

### API: SpawnRequest Changes

**TypeScript (types.ts):**

```typescript
export interface SpawnRequest {
  // ...existing fields...
  image_attachments?: string[]; // base64-encoded PNGs, max 5
}
```

**Go (handlers_spawn.go):**

```go
type SpawnRequest struct {
    // ...existing fields...
    ImageAttachments []string `json:"image_attachments,omitempty"`
}
```

**Validation rules:**

- Max 5 items; reject with 400 if more
- Reject if combined with `resume: true`
- Reject if combined with `command` (raw command mode)
- Reject if combined with `remote_flavor_id` (files not accessible on remote)
- Spawn handler body limit bumped to accommodate base64 payloads (~50MB)

### Backend: File Writing & Prompt Modification

**Flow in `handleSpawnPost`:**

1. Validate image attachments against edge cases above
2. After workspace is resolved (need workspace path), decode each base64 string
3. Write files to `{workspace}/.schmux/attachments/img-{uuid}.png`
4. Append image paths to the prompt before calling `session.Spawn()`

**Prompt appendage format:**

```
<original prompt text>

Image attachments:
Image #1: /absolute/path/.schmux/attachments/img-abc123.png
Image #2: /absolute/path/.schmux/attachments/img-def456.png
```

**Key decisions:**

- Prompt modification happens in the handler, not the session manager
- `SpawnOptions` does not change — the session manager receives a normal prompt string
- Attachment files live in `.schmux/attachments/` which is git-excluded via `.schmux/`
- No active cleanup — files persist for workspace lifetime

### Edge Cases

| Case                     | Behavior                                                       |
| ------------------------ | -------------------------------------------------------------- |
| Multi-target spawn       | All agents share the same attachment files and modified prompt |
| Resume mode              | Reject — no prompt allowed, so no attachments                  |
| Command-based spawn      | Reject — images don't apply to raw shell commands              |
| Remote spawn             | Reject — local files not accessible on remote host             |
| Workspace already exists | Write attachments to existing `.schmux/attachments/` dir       |
| Max images exceeded      | Frontend: silently ignore. Backend: 400 error.                 |

### Files to Modify

**Frontend:**

- `assets/dashboard/src/routes/SpawnPage.tsx` — paste handler, attachment list UI, draft state
- `assets/dashboard/src/lib/types.ts` — `SpawnRequest.image_attachments`
- `assets/dashboard/src/lib/api.ts` — pass image_attachments in spawn call

**Backend:**

- `internal/dashboard/handlers_spawn.go` — `SpawnRequest` struct, validation, file writing, prompt modification
- `internal/dashboard/server.go` — bump body limit for spawn endpoint (if separate from global)

**No changes needed:**

- `internal/session/manager.go` — receives modified prompt, unaware of images
- `internal/api/contracts/` — SpawnRequest is not in contracts (defined locally in handler)
