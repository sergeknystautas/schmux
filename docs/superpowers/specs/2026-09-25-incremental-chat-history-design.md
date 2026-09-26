# Incremental Chat History

**Date:** 2026-09-25
**Status:** Proposed design, not yet implemented

## Problem

Every chat WebSocket connection rereads the full conversation log, marshals the full history, sends the full history, and reduces the full history from an empty conversation. This happens even when the client already displayed the same conversation moments earlier.

The diagnostics show both costs:

- `schmux-005-deb2fa48` loaded the same 10,192-record, 12.7 MB history six times.
- Browser reduction averaged 1.73 s and peaked at 2.12 s.
- The daemon still transmitted the full 12.7 MB frame every time.
- Repeated loads had identical record counts and frame sizes.

Saving only the normalized conversation would remove the repeated reducer work but would still transmit tens or hundreds of megabytes across repeated connects. The wire protocol must become incremental too.

## Goals

1. Send only records appended since the client’s last observed sequence.
2. Reuse the browser’s normalized `Conversation` across reconnects.
3. Keep the raw conversation log as the durable source of truth.
4. Avoid a second Go reducer.

## Non-Goals

- **No IndexedDB.** The cache lives in browser memory for the SPA lifetime.
- **No server-side transcript reducer.** The existing TypeScript reducer remains authoritative for rendering.
- **No generic pagination API.** This is a reconnect delta, not arbitrary page browsing.

## Design

### 1. Sequence numbers

Assign each durable conversation record a monotonically increasing delivery sequence:

- Existing logs get sequence numbers assigned in order during each read.
- Newly appended records get the next sequence after the log append succeeds.
- Live-only records and held-message overlays do not advance the durable sequence.

The sequence is part of the record payload on the wire only. It is not persisted in `conversation.jsonl` and needs no sidecar index.

### 2. Client cache

Keep a module-level map in the dashboard:

```ts
interface CachedChatConversation {
  protocol: ChatProtocol;
  lastSeq: number;
  conversation: Conversation;
}
```

The cache is keyed by `sessionId`. It survives SPA navigation and WebSocket reconnects, and dies on page reload.

When a history frame arrives:

1. If no cache entry exists, reduce the full history and store it with the final sequence.
2. If a cache entry exists, send `?since={lastSeq}` on reconnect.
3. Apply only returned records to the cached conversation.
4. Update `lastSeq` after the batch is reduced.

Live records continue to update the rendered conversation immediately, but do not mutate the cached durable snapshot. A reconnect first restores that snapshot and then applies the returned history delta; this deliberately drops stale live-only streaming state, matching today’s full-history replay semantics.

### 3. Wire protocol

The daemon accepts `since` on `/ws/chat/{id}`:

- No `since`: full history, cold start.
- `since=N`: records with sequence greater than `N`.

The response frame remains `type: "history"` and gains:

- `since`: the requested sequence
- `last_seq`: the newest sequence in the response

If the daemon cannot satisfy a delta request, it returns the full history and marks `reset: true`.

### 4. Daemon behavior

On connect, the daemon still reads the log to reconstruct current state, but it emits only the requested suffix. This fixes transfer size immediately and keeps the existing durability model unchanged.

If profiling later shows log reads are the next bottleneck, add a sidecar offset index then. Do not add it speculatively.

## Telemetry

Add to browser load samples:

- `cache_hit`
- `since`
- `last_seq`
- durable delta record count

Keep the existing frame, parse, reduce, and commit timings.

## Testing

Vitest:

- Cold load reduces and caches.
- Reconnect with `since` applies only new records.
- Only history frames advance `lastSeq`.
- Live records, durable or live-only, do not mutate the durable cache.
- Different session ids use separate cache entries.

Go:

- Sequence assignment is monotonic.
- `since` slicing is exclusive and gapless.
- Invalid or future `since` triggers a full reset.

## Success Criteria

For a reconnect with no new records:

- zero durable history records transmitted,
- no durable-record reducer work,
- no full 12.7 MB frame.

For a reconnect with a small delta:

- transfer and reduction scale with appended records, not total history length.
