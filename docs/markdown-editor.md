# Markdown Editor

## What it does

The local-workspace Markdown page at `/diff/{workspaceId}/md/{filepath}` is an autosaving ByteMD editor. Browser edits and agent writes to the same `.md`/`.mdx` file merge on the server into one file, with no Save button and no conflict state; agent writes appear in the open editor without a reload. Remote workspaces and files the editor refuses fall back to the read-only `react-markdown` viewer.

## Key files

| File                                                             | Purpose                                                                                             |
| ---------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| `internal/mdedit/hub.go`                                         | `Hub`: one `Document` per absolute path; created on first subscribe, closed on last unsubscribe     |
| `internal/mdedit/document.go`                                    | `Document`: watcher loop, echo suppression by SHA-256, `Save`, applied-id dedupe, close reasons     |
| `internal/mdedit/merge.go`                                       | `Merge(base, draft, disk)`: character-level diff-match-patch, returns dropped-hunk count            |
| `internal/mdedit/fsnotify.go`                                    | Parent-directory `fsnotify` source                                                                  |
| `internal/dashboard/websocket_markdown.go`                       | `/ws/markdown/*` handler: auth and path rules before upgrade, 4 MiB read limit, 10 s write timeout  |
| `internal/dashboard/validation.go`                               | `splitWorkspaceFileParam` (shared with `/api/file`), `isMarkdownPath`                               |
| `internal/dashboard/handlers_file_jump.go`                       | `validateWorkspaceFileTarget`: containment, casing, symlinks, regular file, local-only, VCS-ignore  |
| `internal/api/contracts/markdown.go`                             | `MarkdownSave` and `MarkdownDocument` wire types                                                    |
| `assets/dashboard/src/lib/markdownDocument.ts`                   | Pure reducer: `base`, `draft`, one `inFlight` save, status, terminal close reasons                  |
| `assets/dashboard/src/hooks/useMarkdownDocument.ts`              | Binds the reducer to the socket, the 500 ms autosave timer, and reconnect backoff                   |
| `assets/dashboard/src/components/markdown/MarkdownEditor.tsx`    | ByteMD `Editor` wrapper; tab mode, `Cmd-\` Write/Preview toggle                                     |
| `assets/dashboard/src/components/markdown/bytemdSchmuxPlugin.ts` | Image URL rewriting, cursor restore on replacement, toolbar ARIA, per-file tab/cursor/scroll memory |
| `assets/dashboard/src/components/markdown/MarkdownViewer.tsx`    | Read-only `react-markdown` viewer                                                                   |
| `assets/dashboard/src/routes/MarkdownPreviewPage.tsx`            | Controller: editor for local, viewer for remote and `too_large`/`not_utf8`                          |
| `assets/dashboard/src/styles/markdownEditor.module.css`          | Layout and every ByteMD color override mapped to schmux tokens                                      |
| `test/scenarios/markdown-editor.md`                              | End-to-end scenario: type, external rewrite, same-line merge, tab and cursor memory                 |

The HTTP/WebSocket contract (messages, close reasons, limits) is in `docs/api.md` under `WS /ws/markdown/{workspaceId}/{filepath}`.

## Architecture decisions

- **Merge, never conflict.** A save carries `base` (what the draft was edited from) and `draft`. If disk still hashes to `base`, the draft is written; otherwise `Merge` patches base→draft onto disk with `github.com/sergi/go-diff`, whose fuzzy context matching lands a browser hunk even when the agent changed text around it (`internal/mdedit/document.go:392`, `internal/mdedit/merge.go:12`). Line-level diff3 with conflict markers was rejected because it would stop the user on every overlapping edit; the trade-off is that a browser hunk with no surviving context is silently dropped.
- **One message type each way.** The server only ever says "here is the whole document" (`MarkdownDocument`); everything else is a WebSocket close with a reason string (`internal/mdedit/document.go:26`). This replaced an earlier design with six server message types, conflict state, and a retry protocol.
- **One `Document` per file, and it is the only writer to its sockets.** The hub keys documents by absolute path (`internal/mdedit/hub.go:26`). The handler never writes; unparseable frames go through `Document.Reject` so each socket sees one ordered stream (`internal/dashboard/websocket_markdown.go:121`). The document mutex orders only the daemon's goroutines — agents and git write the file outside it.
- **Watch the parent directory, not the file.** Atomic saves (schmux's own, `sed -i`, most editors) replace the inode, so a file watch would go silent (`internal/mdedit/fsnotify.go:7`). Events for other basenames are dropped and bursts coalesce on a 50 ms timer (`internal/mdedit/document.go:123`).
- **Watch before read.** The first subscriber registers the watch before the first read, so every write is either in that read or arrives as an event (`internal/mdedit/document.go:167`).
- **Echo suppression by content hash.** `lastRevision` is the SHA-256 of the bytes last read or written; a watcher reread with that hash is dropped as the document's own write (`internal/mdedit/document.go:317`).
- **Idempotent saves.** Each save carries a client-generated `id`; the document remembers the last 64 applied ids, and a repeat is answered with current disk plus `reply` instead of being re-applied (`internal/mdedit/document.go:360`). Without this, a reply lost after the write plus a reconnect-and-resend applies the patch twice (`The big cat` → `The big big cat`). The reducer resends the outstanding save with the same id on reconnect (`assets/dashboard/src/lib/markdownDocument.ts:97`).
- **Dirty is `draft !== base`, never "an edit event fired"**, because ByteMD fires its change event on programmatic replacement too (`assets/dashboard/src/lib/markdownDocument.ts:53`). One save in flight per tab; keystrokes typed during a save are rebased onto the sent draft and sent on the reply (`assets/dashboard/src/lib/markdownDocument.ts:117`).
- **Per-save validation uses the server's lifetime context**, not the opening request's: the hub keeps the first subscriber's closure for the document's life and that request's context is cancelled when its handler returns (`internal/dashboard/websocket_markdown.go:94`).
- **ByteMD is route-lazy.** Only `MarkdownPreviewPage` imports it (`assets/dashboard/src/App.tsx:29`), so its CodeMirror 5 / unified 10 stack stays out of the initial bundle. Versions are pinned exactly to 1.22.0 in `assets/dashboard/package.json`.

## Gotchas

- **Implemented differently from the design spec — view mode.** The spec called for ByteMD `mode="auto"` (side-by-side above 800 px) and no persisted view. The code defaults to `mode="tab"`, opens in Preview, remembers the Write/Preview choice per workspace file in `localStorage`, and toggles with `Cmd-\` (`assets/dashboard/src/components/markdown/MarkdownEditor.tsx:27`, `:91`). Cursor offset and preview scroll are also persisted per file (`assets/dashboard/src/components/markdown/bytemdSchmuxPlugin.ts:25`). The plugin moves ByteMD's toolbar into the page's file header (`bytemdSchmuxPlugin.ts:225`).
- **Implemented differently — no `replace` effect.** The spec's reducer emitted `replace(text)`; the code has no such effect. The editor's `value` prop is the reducer's `draft`, and ByteMD calls CodeMirror `setValue` only when the prop differs from the editor text (`assets/dashboard/src/components/markdown/MarkdownEditor.tsx:17`). The reducer also tracks `awaitingFirst` to treat the first post-connect document as the server snapshot (`assets/dashboard/src/lib/markdownDocument.ts:21`).
- **`setValue` moves the cursor to the top.** The plugin records cursor and scroll in CodeMirror's `beforeChange` for origin `setValue` and restores them in `change`, mapping the offset across the changed region (`bytemdSchmuxPlugin.ts:243`, `mapCursorOffset` at `:84`). Typing never has that origin.
- **ByteMD toolbar items are plain `div`s.** The plugin adds `role`, `tabindex`, and `aria-label` (icons from their tippy tooltip text, tabs as `role="tab"` with `aria-selected`) and maps Enter/Space to click, re-applying via `MutationObserver` (`bytemdSchmuxPlugin.ts:108`).
- **Image rewriting runs in ByteMD's `rehype` step, after its sanitizer.** It refuses paths that climb above the workspace root, unlike the viewer's shared `resolveRelativePath` (`bytemdSchmuxPlugin.ts:51`).
- **Dropped hunks are logged at Debug level only** (`dropped_hunks` in `internal/mdedit/document.go:414`); at the default level they are invisible. Logs never include document content.
- **A save that changes nothing is not written.** When the merged output equals disk the file is left untouched (`internal/mdedit/document.go:404`), so no watcher event follows.
- **Writes to sockets carry a 10 s deadline** because the document writes while holding its mutex; a stalled tab would otherwise block every other subscriber, the watcher, and shutdown. A timed-out write closes that socket with `write_failed` (`internal/dashboard/websocket_markdown.go:21`, `internal/mdedit/document.go:231`).
- **Terminal vs. reconnecting closes.** `too_large`, `not_utf8`, `invalid_path`, `bad_request` stop reconnection (`assets/dashboard/src/lib/markdownDocument.ts:37`); only `too_large` and `not_utf8` switch the page to the viewer (`assets/dashboard/src/routes/MarkdownPreviewPage.tsx:16`). A failed upgrade exposes no close reason to the browser, so the last reason stays displayed while reconnecting.
- **Known race:** a write by another process between the document's read and its rename is overwritten. There is no cross-process lock; macOS has no mandatory locks and rename-based saves replace the inode anyway.

## Common modification patterns

- **Add a close reason:** add the constant in `internal/mdedit/document.go`, decide whether it is terminal in `TERMINAL_REASONS` (`assets/dashboard/src/lib/markdownDocument.ts`) and whether it falls back to the viewer (`VIEWER_FALLBACK_REASONS` in `MarkdownPreviewPage.tsx`), and list it in `docs/api.md`.
- **Change the wire messages:** edit `internal/api/contracts/markdown.go`, run `go run ./cmd/gen-types` (both types are registered in `cmd/gen-types/main.go`), then update the hook and `docs/api.md`.
- **Change save/merge rules:** server side in `Document.Save` and `Merge` with tests in `internal/mdedit/document_test.go` and `merge_test.go`; client side in the pure `reduce` with tests in `assets/dashboard/src/lib/markdownDocument.test.ts`.
- **Change what files are editable:** `isMarkdownPath` in `internal/dashboard/validation.go`; path safety rules live in the shared `validateWorkspaceFileTarget`, which also gates `/jump` and the file tab API.
- **Add editor behavior (toolbar, keyboard, rendering):** extend `schmuxPlugin` in `bytemdSchmuxPlugin.ts` rather than reaching into ByteMD from the page; restyle ByteMD only in `markdownEditor.module.css` using tokens from `docs/dashboard-style-guide.md`.
