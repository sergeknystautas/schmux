# Spawn Attachments

**Date:** 2026-10-05
**Status:** Proposed design, not yet implemented

## Goal

Attaching files when starting work behaves exactly like attaching files in a
chat conversation. In every local spawn mode (fresh, existing workspace, and
create-branch) the user can attach any image type and any file through the
picker, paste, or drag-and-drop; sees the same chips, upload state, and errors
as the chat composer; keeps attachments in the spawn draft; and every spawned
session receives the files inside its own workspace.

## Current State

- `components/chat/Composer.tsx` classifies each selected file. `image/*` files
  are read client-side as `{media_type, data}` and sent inline. Other files
  upload immediately through `POST /api/workspaces/{id}/attachments`, which
  returns `{name, path}`. On send, Composer appends:

  ```text
  File attachments:
  /abs/path/to/file
  ```

  Composer disables Send while uploading, reports `filename: message` errors,
  renders thumbnail and file-name chips, and persists both kinds in its draft.
  `ChatView.tsx` provides a full-view drop target that calls
  `Composer.attachFiles`.

- `routes/SpawnPage.tsx` accepts images only, through a document-level paste
  listener, stores them as bare base64 strings, renders `Image N` chips, and
  sends them as `image_attachments: string[]`. There is no Attach button, no
  drop target, and no generic-file support.
- `image_attachments` carries no media type. The terminal path writes every
  image as `.png` (`session/manager.go` `writeImageAttachments`), and the chat
  path labels every image `image/png`. Write failures are skipped silently.
- The workspace upload endpoint requires an existing workspace. Fresh and
  create-branch spawns resolve their workspace during the spawn request, and
  each target of a fresh spawn may resolve a different workspace, so that
  endpoint cannot serve spawn.
- The dashboard is the only client of `image_attachments`.

## Non-Goals

- **Attach in terminal sessions.** Matching terminal sessions to this attachment
  workflow is the next project. This spec covers the spawn form and the chat
  composer only.
- **Remote spawns.** Attachments stay unsupported for remote spawns and remote
  workspaces.
- **Attachments on resume or command spawns.** Both keep rejecting attachments.

## Design

### Staging endpoint

`POST /api/spawn-attachments?filename=<name>` accepts a raw body and stores it
at `~/.schmux/spawn-attachments/<uuid>/<name>`. It returns
`201 {id, name}`, where `id` is the uuid. (A `/api/spawn/attachments` path
would sit beside the existing `/api/spawn/{repo}/…` routes and shadow a repo
named `attachments`.)

Filename validation, the 50 MiB limit, `os.Root`-anchored writes, and the
`.upload`-then-rename publish step are shared with the workspace endpoint
(`handlers_attachments.go`) through one extracted package, `internal/attachment`,
rather than copied. Both endpoints return `400 invalid filename`,
`413 file exceeds 50 MiB`, `400 file upload failed` for a broken body, and
`500 cannot save attachment` for any storage failure (the workspace endpoint's
five distinct 500 messages collapse into this one; the detail stays in the
daemon log).

Every upload gets its own uuid directory, so uploading the same file from two
spawn forms produces two independent staged files.

One small type in `internal/dashboard` (a staging store) owns this directory:
`Put` (used by the endpoint), `Resolve(ids) → staged paths` and `Delete(ids)`
(used by the spawn handler), and `Sweep(maxAge)` (called at daemon start). No
other code reads or writes `~/.schmux/spawn-attachments/`.

### Spawn request

In `internal/api/contracts/spawn_request.go`:

- `image_attachments: string[]` is replaced by `images: [{media_type, data}]`,
  the same shape as `chat.Image` and the dashboard's `ChatImage`. The maximum
  stays 5.
- New `file_attachments: string[]` holds staging ids.
- Both fields are rejected with `resume`, `command`, and `remote_profile_id`,
  using the existing `image_attachments` rules. A command target (a
  non-promptable run target) in a request with attachments fails with the
  per-target error `attachments are not allowed for command targets`, matching
  the existing per-target prompt rule.
- Before spawning any target, the handler resolves every staging id through the
  staging store. A missing id fails the whole request with
  `400 "attachment no longer available: <id>"`. Nothing is spawned.
- The handler passes the resolved staged paths to `session.SpawnOptions` as
  `FileAttachments []string`, and `images` as `Images []chat.Image` (replacing
  `ImageAttachments []string`). The session manager never sees staging ids.

### Delivery into each workspace

In `session.Manager.Spawn`, after the workspace resolves and before the prompt
is delivered:

- **Files:** each staged file is copied into
  `<SchmuxDataDir>/attachments/<uuid>/<name>` using the same `os.Root`-anchored
  write. The prompt gets the chat composer's suffix, built by one Go helper:
  `"\n\nFile attachments:\n" + paths joined by "\n"` (no leading blank lines
  when the prompt is empty). Terminal and chat targets receive identical text.
  This format now has two writers, the Go helper for spawn and
  `withFileAttachments` for the chat composer, because spawn paths are known
  only on the daemon and chat paths only in the browser. A Go test and a Vitest
  test each pin the exact output against the same literal so the two cannot
  drift.
- **Images, terminal targets:** written as
  `<SchmuxDataDir>/attachments/<uuid>/img-<id>.<ext>` through the same shared
  write, with the extension from the chat package's existing mapping
  (`attachmentExt` in `internal/chat/bridge.go`, exported), so spawn and chat
  name image files identically. The existing `Image attachments:` suffix is
  unchanged and follows the `File attachments:` block, the same order a chat
  message records.
- **Images, chat targets:** sent inline to the chat runtime with their real
  `media_type`.

The copy happens once per spawned session. Each session's prompt references
only paths inside its own workspace. Three targets in one workspace get three
copies, matching how images already behave.

### Staging lifecycle

- When at least one target in a request succeeds, the handler deletes the
  request's staged directories. This mirrors the form: the in-flight store
  clears the spawn draft on any success (`spawn-inflight.ts`), so no draft can
  reference those ids afterward.
- When the request is rejected or every target fails, staged files remain; the
  form keeps its draft, so the user can retry with the same chips.
- On daemon start, staged directories older than 24 hours are removed. Spawn
  drafts live in per-tab sessionStorage, so older entries are orphans.

### Shared frontend attachment logic

Composer's and ChatView's attachment logic moves into shared units, following
the dashboard's layout (`hooks/`, `components/`, `lib/`):

- `hooks/useAttachments.ts`: `useAttachments<F>({ upload, initialImages?,
initialFiles?, maxImages?, disabled? })` owns `images`, `files`,
  `attaching`, and `error`, plus `attachFiles(files)`, `removeImage`,
  `removeFile`, `restore(images, files)`, and `clear`. Classification, base64 reading,
  the client-side 50 MiB check, and `filename: message` errors match today's
  Composer behavior. When `maxImages` is reached, further images are rejected
  with an error rather than silently dropped.
- `components/AttachmentChips.tsx`: thumbnail chips, file-name chips with
  remove buttons, the `Attaching…` status, and the error banner, using
  Composer's existing markup and styles (moved from `chat.module.css`).
- `hooks/useFileDrop.ts` and `components/FileDropOverlay.tsx`: ChatView's
  drag-depth tracking and "Drop files to attach" overlay (styles moved from
  `chat.module.css`).
- `lib/attachments.ts`: `withFileAttachments(text, paths)` composes the
  `File attachments:` suffix for chat.

Composer uses the hook with
`upload = (f) => uploadWorkspaceAttachment(workspaceId, f)`, and ChatView uses
`useFileDrop`. Their behavior and existing tests are unchanged.

### Spawn form

SpawnPage uses the same hook with `upload = uploadSpawnAttachment` (new
`lib/api.ts` function for the staging endpoint) and `maxImages: 5`:

- An **Attach** button and a hidden multi-file input on the prompt card,
  matching the composer's control.
- The existing document-level paste listener routes all pasted files, not just
  images, through `attachFiles`.
- The spawn form becomes a drop target through `useFileDrop` and
  `FileDropOverlay`, the same units ChatView uses.
- `AttachmentChips` replaces the `Image N` chips.
- Spawn (button and ⌘↩) is disabled while an upload is in flight.
- Attach and drop are disabled when the form is disabled or in remote mode,
  with the tooltip "Attachments aren't supported for remote spawns".
- Attachments are never silently omitted (they can already exist when the user
  switches mode): `validateForm` rejects remote submissions while attachments
  are present, and the slash-command spawns (`/resume`, command targets,
  `/quick`) refuse to start while attachments are present. Each shows a toast
  naming the conflict.

Two differences from chat are deliberate. Spawn caps images at 5 because the
spawn request carries them inline under its 50 MiB body limit; chat sends each
message separately and has no cap. Spawn keeps its existing document-level
paste listener because the spawn form has several inputs and pasting an image
anywhere on the form already works today; chat's paste is on its single
textarea.

### Draft and submit

`SpawnDraft.imageAttachments: string[]` is replaced by `images?: ChatImage[]`
and `files?: {id, name}[]`. Restoring a draft restores both chip kinds without
re-uploading. Drafts that carry only the old `imageAttachments` field are
ignored for attachments.

The request sends `images` and `file_attachments: files.map((f) => f.id)`. The
existing `onSuccess` (called when at least one target succeeds) clears both
collections. When every target fails, the in-flight store keeps the draft, as
it does today.

## Error Handling

- **Upload failure** (size, filename, daemon error): `filename: message` in the
  banner, no chip, prompt and other attachments untouched.
- **Image limit reached:** `filename: maximum 5 images` in the banner.
- **Staging id missing at spawn** (expired or removed): request fails with 400,
  nothing spawns, draft is preserved; the user removes the stale chip.
- **Copy or image write fails during a spawn:** that target fails with the
  error in its `SessionResult` rather than starting without its attachments.
  This replaces the current silent skip in `writeImageAttachments`.
- **Resume, command, quick launch, or remote with attachments:** blocked
  client-side; resume, command, remote, and command targets are rejected
  server-side.

## Testing

Go:

- Staging endpoint stores the body and returns `{id, name}`; rejects invalid
  filenames and bodies over 50 MiB. The shared helper keeps the workspace
  endpoint's existing traversal and symlink-escape tests passing.
- Startup sweep removes staged directories older than 24 hours and keeps newer
  ones.
- Spawn handler: an unknown staging id returns 400 and spawns nothing;
  `images` and `file_attachments` are rejected with resume, command, and
  remote, and per-target for command targets; staged files are deleted when any
  target succeeds and retained when every target fails.
- Session manager: a staged file is copied into the workspace and the prompt
  ends with the `File attachments:` suffix for both terminal and chat targets;
  terminal image extensions follow `media_type`; chat targets receive the real
  `media_type`; a copy failure fails the target.

Vitest:

- `useAttachments`: image/file classification, `attaching` state, error format,
  the `maxImages` cap.
- Composer's existing tests pass unchanged.
- SpawnPage: picker, paste, and drop all reach `attachFiles`; Spawn is disabled
  while uploading; the request carries `images` and `file_attachments`; a
  restored draft shows both chip kinds; success clears them; Attach is disabled
  in remote mode; resume, command, and remote are blocked while attachments
  exist.
- `withFileAttachments` output matches the literal pinned by the Go suffix
  test.

Scenario (Playwright):

- On a fresh spawn, attach a CSV through the picker and a JPEG by drop; the CSV
  uploads to the real staging endpoint with its original bytes; both chips
  survive a reload; the submitted spawn request carries the staged id and the
  JPEG with `image/jpeg`. The scenario image has no promptable agent, so the
  spawn response is a controlled fixture; delivery into workspaces is covered
  by the Go tests above.

## Documentation

`docs/api.md`: add the staging endpoint; replace `image_attachments` with
`images` and `file_attachments` in the spawn request section.
`docs/dashboard-ui.md`: update the spawn form's attachment description.

## Acceptance Criteria

- A fresh spawn, a spawn into workspace-001, and a create-branch spawn can each
  attach `users.csv` and a JPEG. Each spawned session's prompt references a copy
  of `users.csv` inside its own workspace, and the JPEG keeps its media type.
- Spawn and chat produce the same `File attachments:` prompt text.
- Spawn cannot be submitted while an upload is in flight.
- Attachments survive navigation through the spawn draft and are cleared only
  after a fully successful spawn.
- No spawn silently drops an attachment: unsupported modes are blocked, and
  delivery failures fail the target.
