# Markdown Editor

**Date:** 2026-09-23
**Status:** Design approved in conversation; not yet implemented.

Supersedes the 2026-09-22 draft of the same name. That draft's claims about
this codebase were checked against this worktree; where this document differs
from it, the difference is deliberate and listed under "Changes from the prior
draft".

## Problem

The dashboard renders `.md` and `.mdx` files at `/diff/{workspaceId}/md/{filepath}`
but the page is read-only. Making a small documentation change while an agent
works in the same workspace means leaving the dashboard.

Today `MarkdownPreviewPage.tsx` fetches the file over `GET /api/file/{workspaceId}/{path}`,
renders it with `react-markdown`, rewrites relative image URLs to that endpoint,
and refetches when the workspace's aggregate VCS counters change. Those counters
are not a file watch: an agent can rewrite the open file without moving them.
There is no file-write API.

## Goals

1. The local Markdown page becomes an editor with Write, Preview, and Both views.
2. Edits autosave. No Save button.
3. Agent edits to the open file appear in the editor without a reload.
4. Concurrent browser and agent edits are merged on the server into one file.
   The user never sees a conflict state.
5. Download and relative images keep working.
6. Editor code stays out of the initial dashboard bundle.

## Non-goals

- WYSIWYG editing.
- Editing remote-workspace files. They stay on the read-only path.
- Creating, deleting, renaming, or moving files.
- Editing anything other than `.md` and `.mdx`.
- Conflict markers, conflict UI, or manual conflict resolution.
- Browser-side recovery storage.
- Cross-process file locking. See "Known limitations".
- Coordinating with the workspace manager's sync lock, branch changes, or
  workspace reuse. Agents rewrite files under the editor constantly; sync and
  preparation are just more of that, and the merge path handles all of it the
  same way. The editor issues no git commands.
- Persisting the chosen view across page opens.

## Changes from the prior draft

| Prior draft                                          | This design                                  | Why                                                                                                        |
| ---------------------------------------------------- | -------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `github.com/epiclabs-io/diff3`, pinned to a commit   | `github.com/sergi/go-diff` v1.4.0            | The diff3 module has no tags and no adoption. go-diff is tagged, MIT, ~2,000 stars.                        |
| Line-level diff3 with conflict markers               | Character-level patch, always written        | Overlapping edits merge instead of stopping the user.                                                      |
| Six server message types plus client revision fields | One message each way                         | The server has one thing to say: the document.                                                             |
| Conflict state, recovery storage, `file_busy` retry  | Removed                                      | No conflict state exists, so nothing needs recovering or retrying.                                         |
| Silent on cursor behavior                            | Cursor restored after each incoming document | ByteMD's React wrapper calls CodeMirror `setValue`, which moves the cursor to the top. Verified in source. |
| Silent on toolbar accessibility                      | Adapter adds role, tabindex, aria-label      | ByteMD's toolbar icons are plain `div`s. Verified in the built package.                                    |

## Dependencies

### Editor: ByteMD

`bytemd@1.22.0`, `@bytemd/react@1.22.0`, `@bytemd/plugin-gfm@1.22.0`. All MIT.
Verified against the npm registry: 1.22.0 is the latest release, published
February 2025. The editor is a Svelte 3 component wrapped in a thin React
component; it bundles CodeMirror 5 and a unified 10 processing stack separate
from the dashboard's `react-markdown` 10 / unified 11 stack. Its default
processor runs `rehype-sanitize` with the GitHub schema before any plugin's
rehype step, so a plugin that rewrites `img src` runs on sanitized output.

A throwaway Vite 7 / React 19 project built the editor without errors. The
editor chunk was 651 KB minified, 202 KB gzipped, plus 3.5 KB gzipped CSS.
Production license scan: 117 MIT packages and one BSD-3-Clause package.
Implementation must repeat the scan against the committed lockfile.

Vite bundles the packages. No CDN, no hosted service.

### Merge: go-diff

`github.com/sergi/go-diff` v1.4.0, MIT, the Go port of diff-match-patch. Used
for `DiffMain`, `PatchMake`, and `PatchApply`. `go.mod` and `go.sum` change;
`vendor/` is gitignored and regenerated locally with `go mod vendor` before
`./badcode.sh`, which runs with `-mod=vendor`.

## Architecture

```text
ByteMD adapter ──edit──▶ reducer ──save──▶ WebSocket ──▶ handler ──▶ Document
      ▲                     ▲                                          │
      └── replace text ─────┴────────── document ◀─────────────────────┤
                                                                       │ read / patch / atomic write
                                                            parent-dir fsnotify ──▶ workspace file
```

### Server

Package `internal/mdedit` holds `Hub`, `Document`, and `merge`. The message
structs live in `internal/api/contracts/markdown.go` and are regenerated into
`types.generated.ts` with `go run ./cmd/gen-types`. The word "document" here
means one open Markdown file; it is unrelated to schmux sessions.

**Hub.** Owned by the dashboard `Server`. Maps an absolute file path to one
`Document`. `Subscribe` creates it on first use; `Unsubscribe` tears it down on
last use; `Close` stops every document at shutdown and waits for their
goroutines.

**Document.** One per open file. Owns the parent-directory `fsnotify` watch,
the set of subscribed connections, a mutex, and `lastRevision`, the SHA-256 of
the bytes it most recently read or wrote. The document is the only writer to
its connections, so each connection receives one ordered stream. The mutex
orders the daemon's own goroutines and nothing else; agents, editors, and the
daemon's git commands write the file outside it.

Creation order: register the directory watch, then read the file, check size
and encoding, hash, set `lastRevision`, send the document. Any write after the
watch exists is either in that first read or arrives as an event.

**Watcher loop.** One goroutine per document. Events for other basenames are
dropped. A burst is coalesced with a short timer whose clock is injected.
Then, under the mutex: if the file is gone, close every connection with reason
`deleted`; otherwise hash it, drop the event if the hash equals `lastRevision`
(the echo of the document's own write), else set `lastRevision` and send the
document to every subscriber. The directory rather than the file is watched
because atomic saves replace the inode.

**Handler.** One route, `/ws/markdown/{workspaceId}/*`, registered alongside
the other `/ws/` routes with inline auth. Before upgrade: the authentication
block from `handleTerminalWebSocket`; workspace ID and path parsed as in
`handleFile`; `validateWorkspaceFileTarget` from `handlers_file_jump.go`;
extension `.md` or `.mdx`, case-insensitive; `fileMatchesVCSIgnore` false.
Upgrade uses `checkWSOrigin` and a dedicated read limit of 4 MiB in place of
the shared 64 KB limit, which is not widened. After upgrade the handler
subscribes, reads `save` messages in a loop, hands each to the document, and
unsubscribes on close. A frame it cannot parse is reported to the document,
which closes that connection with `bad_request`; the handler itself never
writes to the socket.

**Merge.** A pure function `merge(base, draft, disk string) (out string, dropped int)`:

1. `DiffMain(base, draft, checklines=false)` gives a character-level diff.
2. `PatchMake(base, diffs)` turns it into patches with context.
3. `PatchApply(patches, disk)` applies them to the current file with
   diff-match-patch's context matching, so a patch still lands when the agent
   changed text above or below it.

The function returns the patched text and the number of hunks that could not
be placed. It never returns a conflict. Where both sides changed the same
characters, diff-match-patch applies the browser's change at the best fuzzy
match of its context, provided the match scores within the library's default
`MatchThreshold`. A hunk with no match within that threshold is dropped; the
caller logs the count so the condition is observable.

The expected shape of concurrency: the browser produces small character-level
edits from typing, while agents rewrite whole chunks with tools like `sed -i`,
which writes a new file and renames it over the old one. The document merges
both in memory and the result flows both ways: to disk through the atomic
write, and to the browser as the next document message. A browser edit inside
a region the agent rewrote has no surviving context; the agent's version of
that region stands, the edit is the dropped hunk above, and the browser shows
the agent's text at once. Verified with a throwaway program against go-diff
v1.4.0: an edit on one line and an agent edit elsewhere on the same line
produced one line containing both.

**Save, under the document mutex.**

0. If the save's `id` was already applied, send the current document with
   `reply` set to that id and stop.
1. Re-run `validateWorkspaceFileTarget`. On failure close with reason `invalid_path`.
2. Read disk and its permission bits.
3. If `SHA-256(disk) == SHA-256(base)`, output is the draft. Otherwise output is
   `merge(base, draft, disk)`.
4. Write output with `fileutil.AtomicWriteFile` using the mode from step 2.
5. Set `lastRevision` to the output's hash and record the save's `id`.
6. Send the document to every subscriber, with `reply` set to the id on the
   requester's copy only.

### Protocol

JSON text frames, snake_case fields.

Client to server, one type:

```json
{
  "type": "save",
  "id": "<client-generated, unique per save>",
  "base": "<text the draft was edited from>",
  "draft": "<editor text>"
}
```

Server to client, one type:

```json
{
  "type": "document",
  "content": "<whole file>",
  "revision": "sha256:<hex>",
  "reply": "<id of the save this answers>"
}
```

`reply` is present only on the message that answers the receiving
connection's pending save and carries that save's `id`. Every other document
message omits it. `revision` is the hash of `content`'s exact bytes.

The `id` makes a save idempotent across a dropped socket. The document keeps
the ids it has applied, in memory, bounded to the last 64. A save arriving
with a known id is not applied again; the document answers with its current
content and `reply` set to that id. Without this, a reply lost after the
write and a reconnect-and-resave would apply the same patch twice; verified
with go-diff v1.4.0, which turned `The big cat` into `The big big cat`. A
daemon crash between the rename and the reply still loses the ids; that
window is microseconds and is accepted.

Everything else is a socket close with a reason string: `deleted`, `not_utf8`,
`too_large`, `invalid_path`, `write_failed`, `watcher_error`, `bad_request`.
Invalid JSON, unknown types, or a `base` or `draft` over 1 MiB after decoding
close with `bad_request`.

### Browser

**Reducer.** A pure function `(state, event) → (state, effects)` in
`lib/markdownDocument.ts`. State has four fields: `base`, `draft`, `inFlight`
(the id, base, and draft of the one outstanding save, or `null`), and `status`
(`connecting`, `saved`, `saving`, `error` with reason). Effects: `send(id,
base, draft)`, `replace(text)`, `armTimer`, `cancelTimer`. Dirty means `draft !==
base`; it is never inferred from an edit event, because ByteMD fires its change
event on programmatic replacement too.

Rules:

- **edit(text):** set `draft`. If dirty and nothing in flight, `armTimer`.
- **timer:** `send(id, base, draft)` with a fresh id; record id, base, and
  draft as `inFlight`; status `saving`.
- **document with reply not matching `inFlight.id`:** treat as a document
  without reply.
- **document with reply, no typing since send** (`draft === inFlight.draft`):
  `base` and `draft` become `content`; `replace(content)`; clear `inFlight`;
  status `saved`.
- **document with reply, typed since send:** `base` becomes `inFlight.draft`,
  because the new keystrokes were typed on top of it; `send` at once with a
  fresh id and record it as the new `inFlight`.
- **document without reply, clean, nothing in flight:** adopt as `base` and
  `draft`; `replace(content)`.
- **document without reply, dirty, nothing in flight:** `cancelTimer`;
  `send` now with a fresh id.
- **document without reply, save in flight:** ignore. The server's read either
  included it, or the watcher sends it again after the reply, when the client
  is clean.
- **open (reconnect):** the first message is a document. Clean: adopt. Dirty
  with a save still recorded in `inFlight`: resend it with the same id, so a
  save that committed before the socket dropped is not applied twice. Dirty
  otherwise: `send` with a fresh id. Either way the server merges against
  current disk.
- **close(reason):** status `error(reason)`; `cancelTimer`; keep `draft` and
  `base`. `deleted`, `write_failed`, `watcher_error`, and a close with no
  reason reconnect. `too_large`, `not_utf8`, `invalid_path`, and
  `bad_request` are terminal: no reconnect. The last reason stays displayed
  while reconnecting, since a failed upgrade exposes no reason to the browser.

Autosave delay is 500 ms from the last edit. One save in flight per tab.

**Hook.** `useMarkdownDocument(workspaceId, path)` runs the reducer against a
socket from `transport.createWebSocket`, a timer, and reconnection with the
same exponential backoff and jitter as `useSessionsWebSocket` (cap 30 s). It
never fetches content over HTTP.

**Adapter.** `components/markdown/MarkdownEditor.tsx` renders
`@bytemd/react`'s `Editor` with `mode="auto"`, the GFM plugin, and one
schmux plugin that:

- rewrites relative `img src` to `/api/file/...` in its `rehype` step, which
  ByteMD runs after sanitization;
- captures the CodeMirror instance in `editorEffect` and subscribes to its
  `beforeChange` and `change` events. For a change whose origin is `setValue`,
  which is how ByteMD applies an incoming `value` prop, `beforeChange` records
  the cursor offset and scroll and `change` restores them, shifting the offset
  by the length difference when the cursor sat past the common prefix of old
  and new text. Typing never has that origin, so it is untouched;
- in the same `editorEffect`, sets `role="button"`, `tabindex="0"`, and an
  `aria-label` from each toolbar icon's tooltip text, and maps Enter and Space
  to click.

`mode="auto"` is ByteMD's own behavior: side-by-side above 800 px of container
width with its Write-only and Preview-only toggles, tabs below. No schmux code
switches views.

**Page.** `MarkdownPreviewPage` stays route-lazy and becomes a controller:
local workspace → `MarkdownEditor` with the hook; remote workspace, or a socket
closed with `too_large` or `not_utf8` → the existing `react-markdown` viewer,
extracted into `MarkdownViewer.tsx` unchanged. The header keeps Back, the
filename, Download, and a status word: `Saving…`, `Saved`, or the close
reason. The VCS-counter refetch is removed for the editor path and kept for
the viewer.

**Styles.** `styles/markdownEditor.module.css` holds layout and every ByteMD
override, mapping its editor, CodeMirror surface, preview, toolbar, and tooltip
colors to schmux tokens under `[data-theme='light']` and `[data-theme='dark']`.
No bare `button`, `input`, `select`, or `textarea` selectors. Schmux-owned
controls use `.btn` variants. ByteMD's own stylesheet is imported inside the
lazy chunk.

## Safety

Every connection and every save establishes: workspace exists and has no
`RemoteHostID`; path is relative, inside the workspace, matches on-disk casing,
and contains no symlink at any component; target is a regular file with
extension `.md` or `.mdx`; target is not VCS-ignored. The shared
`validateWorkspaceFileTarget` includes the ignore check, so every save spawns
one `check-ignore` subprocess, bounded by the validator's 5 s timeout. The
per-save validator is bound to the server's lifetime context, not the
request that opened the document, because the document outlives whichever
tab opened it first. The initial read rejects files over 1 MiB or not
valid UTF-8; the socket closes with `too_large` or `not_utf8` and the page
falls back to the viewer.

The feature edits existing files only. A missing or symlinked target is never
created or followed. Logs carry workspace ID, relative path, revision, byte
count, and dropped-hunk count, never document content.

## Known limitations

Between the document's read of disk and its rename, an unrelated process can
write the file, and that write is overwritten. Advisory locks would only ever
be held by schmux, macOS has no mandatory locks, and rename-based saves replace
the inode. The window is milliseconds; `docs/api.md` states it.

A browser hunk whose surrounding context no longer exists anywhere in the file
cannot be placed and is dropped. The daemon logs the count. This is the price
of never showing a conflict.

## Testing

All tests follow `docs/testing.md`. Timers use injected clocks (rule 3,
example 2). Watcher events are injected. No sleeps.

**Go, `internal/mdedit`:** table-driven tests for `merge` (non-overlapping,
same-line different-position, identical edits, overlapping edits, context
shifted by agent insertions above, empty base, dropped-hunk count); document
tests for watch-before-read ordering, in-place write, atomic rename, echo
suppression by hash, delete closing subscribers with `deleted`, second
subscriber receiving the first's save, last-unsubscribe cleanup, `Close`
waiting for goroutines.

**Go, `internal/dashboard`:** pre-upgrade rejection for each validation rule
(auth, origin, traversal, case, symlink at every position, ignored, extension,
remote, directory, too large, not UTF-8), dedicated read limit, oversized
field rejection, `reply` set only for the requester.

**Frontend:** reducer tests for every rule above, pure. Hook tests with the
existing `MockWebSocket` pattern and fake timers: 500 ms debounce, one in
flight, typing during a save, clean and dirty incoming documents, reconnect
saving a dirty draft, close reasons. Adapter tests: cursor restored after
replacement, toolbar icons have accessible names and respond to Enter, image
rewriting, sanitization of a script tag. Page tests: local uses the editor,
remote uses the viewer, `too_large` falls back to the viewer.

**Scenario, `test/scenarios/markdown-editor.md`:** open a local Markdown file;
type and confirm the disk file changed; change a different line on disk and
confirm the editor shows it; change the same line on both sides and confirm
one file containing both edits.

**Evidence before completion:** `go run ./cmd/build-dashboard` output showing
ByteMD in a separate chunk; `./test.sh`; `./badcode.sh`; a production license
scan of the committed lockfile; the style guide's seven-point rubric run by a
human in both themes.

## Documentation

- `docs/api.md`: the route, auth, limits, both message types, close reasons,
  merge semantics, both known limitations.
- `docs/web.md`: editor views, autosave, live agent updates, read-only
  fallbacks.
- `docs/react.md`: the lazy chunk, adapter boundary, reducer and hook, pinned
  dependencies and licenses.

## Acceptance criteria

1. A local Markdown page opens side-by-side and switches views with ByteMD's controls.
2. Edits autosave after 500 ms and typing during a save loses nothing.
3. Content, saves, and agent updates use only the Markdown WebSocket.
4. A clean page shows an agent edit without reload, cursor in place.
5. Browser and agent edits to different parts of the file both land.
6. Browser and agent edits to the same line both land; no conflict UI exists.
7. Every safety rule holds on connect and on save.
8. Remote Markdown and oversized or non-UTF-8 files render read-only.
9. Images, Download, GFM, sanitization, both themes, and keyboard access work.
10. ByteMD is in a lazy chunk; licenses pass; `./test.sh` and `./badcode.sh` pass.
