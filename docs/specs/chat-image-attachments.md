# Chat Image Attachments

**Status:** v1 — initial draft.

## Problem

Pasting an image into a chat session delivers the bytes but the agent often
never sees the picture. Both composer entry points — clipboard paste and the
Attach file picker — converge at `attachFiles` → `Runtime.Send`, so both are
affected.

**Delivery works.** The Composer base64-encodes the image, the chat socket
carries it, and `Runtime.Send` writes a user message whose content holds an
inline base64 image block. The harness accepts it.

**The model doesn't receive pixels.** Chat sessions behind the GLM relay
(model glm-5.3) have every inline image re-hosted on the relay's object
storage and handed to the model as a signed, expiring URL. To look at it the
model must call the relay's `analyze_image` tool with that URL — a second,
lossy hop. Observed in `~/.schmux/chat/schmux-006/schmux-006-d1d7dfae/`
(2026-09-12): a mangled URL → `400 图片输入格式/解析错误`, and a second image
the model never viewed at all, then argued with the user about what had
"really" been pasted.

**Terminal paste doesn't have this problem** because it never sends pixels
through the model channel: `POST /api/clipboard-paste` writes
`/tmp/schmux-clipboard-<id>.png` and hands the agent the path
(`fencedClipboardPaste`, `internal/dashboard/clipboard.go:191`); terminal
spawns write `.schmux/attachments/img-*.png` and append the paths to the
prompt. The agent opens a local file with its own tools — no relay URL, no
expiry, no model discretion.

Chat paste must follow the same contract: **the image lands in the tmp
directory and the agent gets the path.**

## Goals

- An attached chat image — paste, file picker, or spawn-time initial
  message — is persisted to `/tmp` as `schmux-chat-<uuid8>.<ext>` (mode 0600),
  the same directory and pattern family the terminal clipboard flow uses, and
  the agent receives the path in the message text.
- Works on every backend — including relays that degrade inline images to
  URLs — and for both chat protocols (Claude stream-json, Codex app-server).
- Native-vision backends keep the inline base64 block; the file is an
  addition, never a replacement.
- One write site (`Runtime.Send`); every later encode (held-message flush,
  Codex `Rebuild`, Restart seeding) derives from the stored record.
- A persist failure degrades to today's behavior (inline base64 only), never
  blocks the send.

## Non-goals

- **Terminal flows** — already hand the agent files; unchanged.
- **Attachment lifetime/GC** — /tmp is managed by the OS, exactly like the
  terminal clipboard files. No cleanup code.
- **UI changes** — chips, bubbles, paste handling unchanged.

## Design

### Where files are written

`/tmp` — the terminal clipboard flow's location (`fencedClipboardPaste`).
It satisfies every constraint:

- **The established place** — `schmux-clipboard-*` already lives there for
  terminal paste; `schmux-chat-*` joins it. Same vocabulary, same lifetime.
- **Fence-readable** — /tmp is readable inside the fence and not redirected
  (the fenced clipboard paste relies on exactly this).
- **No workspace churn** — nothing written into the worktree, no git-exclude
  interaction.

Naming: `schmux-chat-<uuid8>.<ext>` — the `chat-` middle marks the origin;
the extension derives from the image's `media_type` (`image/png`→`png`,
`image/jpeg`→`jpg`, `image/gif`→`gif`, `image/webp`→`webp`, else `png`),
because chat pastes are any `image/*`. Mode `0600`, matching both terminal
flows.

### Who writes them

`Runtime.Send` (`internal/chat/runtime.go`) — the single entry point for the
WebSocket `send` frame and the spawn-time initial message. `NewRuntime` gains
the directory as a constructor argument; `ensureChatRuntime` passes `/tmp`.

Send, before taking the runtime mutex:

1. Copy the caller's image slice (never mutate it); clear any inbound
   `Image.Path` — the daemon assigns paths, clients never do.
2. Persist each image via `PersistAttachment` (base64 decode → MkdirAll →
   write `0600`); set `Image.Path` on success. A failure logs a warning and
   leaves that image pathless; the send proceeds.
3. Append the `user_message` record (source of truth) and encode — the text
   suffix below derives from `Images[].Path` at encode time, never stored in
   `Text`.

Held messages (`flushHeldLocked`) and Codex `Rebuild` only re-encode from
records whose `Path` fields are already set — no path is computed twice.

### The harness line

Both protocols keep the user's text untouched and append the exact suffix
format terminal spawns use (`appendImagePathsToPrompt`):

```
<text>

Image attachments:
Image #1: /tmp/schmux-chat-ab12cd34.png
```

Numbering counts only images that got a path, in paste order.

- **Claude** (`UserMessageLine`): suffix on the single text block that
  precedes the image blocks. Base64 blocks unchanged.
- **Codex** (`UserMessage`): suffix on the text input item. `data:` URL image
  inputs unchanged.

### Record and wire format

`chat.Image` gains `Path string \`json:"path,omitempty"\``— daemon-assigned
at send time, server→client only. The`user_message`record and`history`/`record`frames carry it automatically; the client`send`frame is
unchanged. The dashboard's`ChatImage`gains`path?: string`; nothing reads
it yet.

### Durability

/tmp is ephemeral by design — same as terminal clipboard files. The record's
base64 remains the durable copy: after a reboot the paths dangle, the
dashboard still renders chips from base64, and the agent is not running
against stale paths in practice. Within a session's life the files persist.

## Testing

- `UserMessageLine` (Claude) and Codex `UserMessage`: images with paths →
  text ends with the suffix, inline blocks unchanged; images without paths →
  byte-identical to today.
- `PersistAttachment`: writes decoded bytes, `schmux-chat-` name, `0600`,
  correct extension per media type; bad base64 and unwritable dir fail.
- `Runtime.Send`: files written into the injected dir, `Path` set on the
  record, caller's slice not mutated, inbound path overwritten; persist
  failure → send succeeds, no path, no suffix, inline block stays.
- Held path: Codex not-addressable send → held; `flushHeldLocked` (triggered
  from the output tail loop) encodes the suffix from the record's paths.
- Session wiring: `ensureChatRuntime` → path lives in `/tmp`.
- Record JSON round-trip includes `path`; omit-empty when unset.

## Documentation

- `docs/api.md` — `path` in the `user_message` example; server-assigned,
  omitted on failure.
- `docs/chat-sessions.md` — at `/finalize`, not during implementation.

## Alternatives considered

- **`.schmux/attachments/` in the workspace** (the terminal-spawn location):
  fence-readable and durable, but chat paste is a clipboard event, not a
  workspace artifact — it belongs in the tmp directory with the other
  clipboard files, and workspace copies churn the tree. Rejected for
  symmetry with the flow the user pointed at.
- **Session dir under `~/.schmux/chat/…`**: fenced chat agents cannot read
  it (by design — launch artifacts live there).
- **Path-only (drop inline base64):** forces a tool call on native-vision
  backends — a regression for them.
- **Detect the relay and branch:** no reliable signal; the dual encode costs
  nothing.
