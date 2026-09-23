# Chat Content Links

**Status:** v1 — approved design, 2026-09-23.

## Problem

Agents sometimes invent dashboard URLs for workspace artifacts. In
`bach-godot-003`, chat produced:

```text
https://12540.dashboard.sx:7337/sessions/review/steam-store-automation-spikes.html
```

The file was actually viewable at:

```text
https://12540.dashboard.sx:7337/diff/bach-godot-003/html/review%2Fsteam-store-automation-spikes.html
```

The content-specific route is not something agents should construct. It depends
on extension, workspace ID, URL encoding, viewer availability, and whether the
dashboard opens the file as a tab or a direct route. Public origin also varies
between local HTTP, custom HTTPS, and `dashboard.sx` deployments.

A guessed URL can therefore fail even when the file exists. Conversely, a
renderer that blindly repairs guessed URLs cannot know which workspace file the
agent meant and could turn an ordinary dashboard route into an unintended file
navigation.

## Current State

The navigation core already exists, but two gaps remain:

- `GET /jump/{workspaceId}/{filepath}` validates a local target and redirects
  by content type. Its current validation is filesystem-only and does not yet
  apply the raw-file endpoint's VCS-ignore check.
- `POST /api/workspaces/{workspaceId}/tabs` with `kind: "file"` performs the
  same filesystem validation and returns a tab or direct navigation result.
- Chat Markdown rewrites absolute paths and `file://` URLs under the current
  workspace to `/jump/{workspaceId}/{filepath}`. Its one-pass URI decoding
  already handles spaces and literal percent signs correctly; tests must lock
  that behavior in.
- The raw-file endpoint remains the download path through
  `GET /api/file/{workspaceId}/{filepath}?download=1`.
- Schmux has a managed instruction template and marker-delimited updater, but
  spawn currently provisions it only for adapters whose signaling strategy is
  `instruction_file`. The primary chat harnesses Claude Code and Codex use
  hook-based signaling, so they do not reliably receive this managed block.

The managed block also tells agents how to signal status and register previews,
but not how to cite workspace files in chat. Those gaps invite the invented
`/sessions/review/...` URL above.

## Vocabulary

- **Contract-conformant citation:** a Markdown link emitted by an agent as a
  URI-encoded absolute filesystem path, or the equivalent `file://` URL, for an
  existing file beneath the current workspace.
- **Canonical jump URL:** the authenticated `/jump/{workspaceId}/{filepath}`
  URL used as the stable browser fallback after chat normalization.
- **Raw-file endpoint:** the existing `/api/file/...` endpoint, which serves
  preview bytes or an attachment and remains unrelated to viewer selection.

## Ownership and Scope

This remains a focused navigation-contract change:

- `internal/dashboard` owns one workspace-file target validator. Both `/jump`
  and the workspace tab API call it; neither handler duplicates traversal,
  symlink, regular-file, or VCS-ignore checks.
- `internal/workspace` remains the sole mutator of persisted workspace tabs.
- `assets/dashboard/src/lib/fileNavigation.ts` remains the sole chat-link
  normalizer, and `AssistantTurnView` remains its only chat caller.
- `internal/workspace/ensure.SignalingInstructions` remains the single managed
  instruction template. Spawn provisioning must invoke its updater for every
  builtin harness with an instruction-file configuration, independent of whether
  lifecycle signaling uses hooks.
- Expected implementation touches the shared dashboard validator, the chat-link
  normalizer, the managed instruction updater/spawn call site, focused tests for
  each, and the existing API/web documentation. That scope is required because
  the failure spans agent guidance, URL normalization, server-side validation,
  and navigation; it does not require a new route or viewer.

## Goals

- Every contract-conformant citation to a file in the current workspace reaches
  the correct viewer, regardless of content type.
- Agents never need to know the dashboard origin, workspace ID, or which route
  handles a particular extension.
- File-target selection happens server-side after validation, not in agent
  prose.
- Ordinary clicks integrate with workspace tabs and pending navigation.
- Modified clicks, middle clicks, and copied links remain valid canonical URLs.
- Existing managed instruction files are upgraded in place.

## Non-Goals

- No attempt to retroactively repair arbitrary historical URLs such as
  `/sessions/review/report.html`. Their pathname does not reliably identify the
  intended workspace-relative file.
- No heuristic rewriting of every same-origin URL. Real dashboard routes must
  keep their normal meanings.
- No new download route. Downloads continue through the existing authenticated
  raw-file endpoint with `?download=1`.
- No remote-workspace jump support in v1. The existing local-only limitation
  remains explicit rather than becoming an unvalidated redirect.
- No content sniffing. Extension determines the initial viewer, exactly as the
  current diff and preview routes do.

## Design

### Canonical navigation

The authenticated canonical browser fallback is:

```text
/jump/{workspaceId}/{filepath}
```

Both `{workspaceId}` and `{filepath}` are percent-encoded. Slash characters in
the file path may be encoded as `%2F`; the server decodes the complete target
once and validates the resulting workspace-relative path.

The route accepts only an existing regular file in a local workspace. It rejects:

- malformed workspace IDs and file paths;
- traversal;
- missing workspaces and files;
- directories and other non-regular files;
- targets whose resolved path leaves the workspace;
- targets ignored by the workspace's VCS;
- any symbolic link in the target path, including an interior directory.

VCS-ignore checks use the same workspace-rooted ignore machinery as the
authenticated raw-file endpoint. A failed ignore check is an error, never an
unchecked redirect.

On success it redirects by lowercase extension:

| Extension                                | Destination                        |
| ---------------------------------------- | ---------------------------------- |
| `.md`, `.mdx`                            | Markdown viewer                    |
| `.mmd`                                   | Mermaid viewer                     |
| `.png`, `.jpg`, `.jpeg`, `.webp`, `.gif` | Image viewer                       |
| `.html`                                  | HTML viewer                        |
| all others                               | diff viewer with the file selected |

Audio remains in the fallback diff destination in v1. The raw-file endpoint can
serve supported audio inline, but no dedicated audio viewer/tab kind exists.

### Agent link contract

The schmux-managed instruction block gains a short workspace-file-link rule:

> When linking a workspace file in chat, use the file's absolute path as the
> Markdown link target. Do not construct dashboard, preview, diff, session, or
> download URLs. Schmux rewrites the path to the canonical jump route and
> selects the correct viewer.

For example, an agent in `/workspaces/bach-godot-003` links:

```markdown
[Steam store automation spike](/workspaces/bach-godot-003/review/steam-store-automation-spikes.html)
```

Agents emit the first form as a URI. A literal percent sign in a filename is
therefore written as `%25`; a space may be written literally or as `%20`. The
renderer decodes either form exactly once to recover the filesystem path.

The accepted agent-emitted forms are:

- a URI-encoded absolute filesystem path under the current workspace;
- an equivalent `file://` URL;
- optional trailing `:line` or `:line:column`, which the chat renderer strips.

The renderer canonicalizes these forms to `/jump/...`. It leaves relative links
and every HTTP(S) URL unchanged, including dashboard-origin URLs that already
use `/jump/...`. In particular, it does not strip `/sessions` or another guessed
prefix to guess a file path.

### Chat click behavior

On an ordinary unmodified click of a recognized workspace link:

1. The renderer prevents default browser navigation.
2. It calls the workspace tab API with `kind: "file"`.
3. The server validates the target and chooses tab or direct navigation.
4. The client follows the returned navigation instruction, including pending
   tab navigation over the existing WebSocket state flow.

For modified clicks, middle clicks, or loss of the React handler, the rendered
`href` remains `/jump/...`. The server performs the same target validation and
HTTP redirect.

### Instruction updates

The link rule lives in the same schmux-managed, marker-delimited instruction
block as status signaling and preview registration. When workspace preparation
encounters an existing block, it replaces that block with the current version
while preserving all user and project instructions outside the markers. New
workspaces receive the rule initially.

Provisioning applies regardless of whether the session is terminal or chat.

## Error Handling

Invalid canonical links fail before any redirect:

- `400` for malformed input, traversal syntax, or a remote workspace;
- `403` for an outside target, a VCS-ignored target, a non-regular file, or a
  symbolic link in the path;
- `404` for an unknown workspace or missing file.
- `500` when the VCS ignore check itself fails.

The ordinary-click API uses the same validation and surfaces failures through
the existing chat file-open error dialog. A browser fallback click shows the
JSON error response rather than redirecting to an unvalidated destination.

## Testing

- Backend route tests cover every extension mapping, nested encoded paths, and
  each rejection class above.
- Tab API tests verify server-selected Markdown, Mermaid, and HTML tabs plus
  direct image/other-file navigation.
- Renderer unit tests verify absolute-path and `file://` rewriting, line-suffix
  removal, external-link preservation, and unchanged behavior for guessed
  same-origin URLs. They also cover filenames containing spaces and a literal
  `%2F`: both citation forms encode `%` as `%25` and decode exactly once.
- Chat page tests verify ordinary-click API navigation and modified-click
  fallback to the canonical `href`.
- Managed-instruction tests verify that new files contain the rule and existing
  marked blocks are upgraded without altering surrounding instructions.
- Spawn-provisioning tests cover hook-signaling harnesses with instruction
  files, including Claude Code and Codex, not only `instruction_file` signaling
  targets.

## Acceptance Criteria

- A chat link to `review/steam-store-automation-spikes.html` in the matching
  workspace opens the HTML viewer through the tab API without a browser reload.
- The same input pattern works for Markdown, Mermaid, images, source files, and
  files with spaces or encoded slash-like characters in their names.
- Agent instructions explicitly prohibit constructed dashboard URLs.
- Invalid targets never produce a redirect.
- VCS-ignored targets are rejected before redirect or viewer navigation.
- Dashboard-generated download links continue to use
  `/api/file/...?download=1`.
- Existing jump, tab, chat, and instruction tests pass together with the new
  contract tests.
