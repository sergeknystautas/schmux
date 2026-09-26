import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { ChatSocket } from '../lib/chat/socket';
import type { ChatSocketStatus } from '../lib/chat/socket';
import {
  captureChatLoad,
  clearSessionNavigation,
  getSessionNavigation,
} from '../lib/chat/loadTelemetry';
import type { ChatLoadSample } from '../lib/chat/loadTelemetry';
import type { ChatReductionProfile } from '../lib/chat/loadTelemetry';
import {
  clearCachedConversation,
  getCachedConversation,
  setCachedConversation,
} from '../lib/chat/conversation-cache';
import type { CachedChatConversation } from '../lib/chat/conversation-cache';
import { applyRecord, applyRecords, emptyConversation, resolvesRequest } from '../lib/chat/reducer';
import type { ChatImage, ChatProtocol, Conversation, ConversationRecord } from '../lib/chat/types';

export function useChatSocket(
  sessionId: string | undefined,
  running: boolean,
  onRequestResolved?: (requestId: string) => void,
  chatLoadProfiling = false
): {
  conversation: Conversation;
  status: ChatSocketStatus;
  error: string | null;
  historyLoaded: boolean;
  send(text: string, images: ChatImage[]): void;
  interrupt(): void;
  answerPermission(
    requestId: string,
    allow: boolean,
    updatedInput?: Record<string, unknown>,
    message?: string
  ): void;
  answerQuestion(
    requestId: string,
    answers: Record<string, string[]>,
    input: Record<string, unknown>
  ): void;
  abort(requestId: string): void;
} {
  const [conversation, setConversation] = useState<Conversation>(emptyConversation);
  const [status, setStatus] = useState<ChatSocketStatus>('connecting');
  const [error, setError] = useState<string | null>(null);
  // True once the history frame for the current connection has been applied.
  // Focus restore gates on this: before history, a question card that will
  // exist is indistinguishable from one that is gone.
  const [historyLoaded, setHistoryLoaded] = useState(false);
  const routeStartRef = useRef<{
    sessionId: string | undefined;
    at: number;
    source: ChatLoadSample['start'];
  } | null>(null);
  if (routeStartRef.current?.sessionId !== sessionId) {
    const clickedAt = sessionId ? getSessionNavigation(sessionId) : undefined;
    routeStartRef.current = {
      sessionId,
      at: clickedAt ?? performance.now(),
      source: clickedAt === undefined ? 'view' : 'click',
    };
  }
  const pendingLoadRef = useRef<{
    sample: ChatLoadSample;
    reducedAt: number;
    routeStartedAt: number;
  } | null>(null);
  const socketRef = useRef<ChatSocket | null>(null);
  // Effects run after render. Track which session owns the hook state so a
  // route change cannot render the previous session's conversation first.
  const stateSessionIdRef = useRef(sessionId);
  // Keep the callback in a ref so the socket effect below does not re-run
  // when the caller re-renders.
  const onRequestResolvedRef = useRef(onRequestResolved);
  onRequestResolvedRef.current = onRequestResolved;
  const chatLoadProfilingRef = useRef(chatLoadProfiling);
  chatLoadProfilingRef.current = chatLoadProfiling;
  // The protocol arrives with the history frame and selects the reducer for
  // every record after it.
  const protocolRef = useRef<ChatProtocol>('claude-stream-json');
  // Live records buffer: applied as one setConversation call per animation
  // frame, so a burst of deltas costs one render, not N.
  const pendingRef = useRef<ConversationRecord[]>([]);
  const frameRef = useRef<number | null>(null);

  useLayoutEffect(() => {
    if (!historyLoaded || stateSessionIdRef.current !== sessionId || !pendingLoadRef.current)
      return;
    const pending = pendingLoadRef.current;
    pendingLoadRef.current = null;
    const committedAt = performance.now();
    let secondFrame: number | null = null;
    const firstFrame = requestAnimationFrame(() => {
      secondFrame = requestAnimationFrame(() => {
        const afterPaintAt = performance.now();
        captureChatLoad({
          ...pending.sample,
          commitMs: committedAt - pending.reducedAt,
          afterPaintMs: afterPaintAt - committedAt,
          totalMs: afterPaintAt - pending.routeStartedAt,
        });
      });
    });
    return () => {
      cancelAnimationFrame(firstFrame);
      if (secondFrame !== null) cancelAnimationFrame(secondFrame);
    };
  }, [historyLoaded, sessionId]);

  const schedule = useCallback(() => {
    if (frameRef.current !== null) return;
    if (typeof requestAnimationFrame === 'function') {
      frameRef.current = requestAnimationFrame(flush);
    } else {
      frameRef.current = setTimeout(flush, 16) as unknown as number;
    }
  }, []);

  function flush() {
    frameRef.current = null;
    const batch = pendingRef.current;
    pendingRef.current = [];
    if (batch.length) {
      const protocol = protocolRef.current;
      setConversation((c) => batch.reduce((acc, r) => applyRecord(protocol, acc, r), c));
    }
  }

  useEffect(() => {
    const sessionChanged = stateSessionIdRef.current !== sessionId;
    if (sessionChanged) {
      stateSessionIdRef.current = sessionId;
      setConversation(emptyConversation());
    }
    setError(null);
    setHistoryLoaded(false);
    pendingLoadRef.current = null;
    pendingRef.current = [];
    if (frameRef.current !== null) {
      if (typeof cancelAnimationFrame === 'function') cancelAnimationFrame(frameRef.current);
      else clearTimeout(frameRef.current);
      frameRef.current = null;
    }
    if (!sessionId) {
      setStatus('gone');
      return;
    }
    clearSessionNavigation(sessionId);
    setStatus('connecting');
    // The cache entry whose lastSeq the current connection requested. A delta
    // frame applies to this conversation, the exact state the daemon resumed.
    let requested: CachedChatConversation | undefined;
    const socket = new ChatSocket(
      sessionId,
      {
        onHistory: (protocol, records, timing, metadata) => {
          if (socketRef.current !== socket) return;
          protocolRef.current = protocol;
          // A reconnect reloads history. Discard any buffered live records from
          // the prior connection so they cannot be applied twice.
          pendingRef.current = [];
          if (frameRef.current !== null) {
            if (typeof cancelAnimationFrame === 'function') cancelAnimationFrame(frameRef.current);
            else clearTimeout(frameRef.current);
            frameRef.current = null;
          }
          const resolveStartedAt = performance.now();
          for (const r of records) {
            const rid = resolvesRequest(protocol, r);
            if (rid) onRequestResolvedRef.current?.(rid);
          }
          const resolveFinishedAt = performance.now();
          const reduceStartedAt = performance.now();
          // Queue overlays (no seq) ride after the durable records. The cache
          // keeps durable state only; overlays apply to the rendered copy. An
          // older daemon (rolling reload) sends no lastSeq and no seqs: reduce
          // its full history cold and drop any cache it cannot extend.
          const lastSeq = metadata.lastSeq;
          const sequenced = lastSeq !== undefined;
          const entry = requested;
          const cacheHit =
            sequenced &&
            !metadata.reset &&
            entry?.protocol === protocol &&
            entry.lastSeq === metadata.since;
          const base = cacheHit && entry ? entry.conversation : emptyConversation();
          const durable = sequenced ? records.filter((r) => (r.seq ?? 0) > 0) : records;
          const overlays = sequenced ? records.filter((r) => (r.seq ?? 0) === 0) : [];
          const reduced = chatLoadProfilingRef.current
            ? reduceWithTelemetry(protocol, base, durable)
            : { conversation: applyRecords(protocol, base, durable), profile: undefined };
          if (lastSeq !== undefined) {
            setCachedConversation(sessionId, {
              protocol,
              lastSeq,
              conversation: reduced.conversation,
            });
          } else {
            clearCachedConversation(sessionId);
          }
          setConversation(applyRecords(protocol, reduced.conversation, overlays));
          const reducedAt = performance.now();
          // A reconnect can remain disconnected while the tab is idle. Measure
          // its load from this socket attempt, not the old disconnect event.
          const routeStartedAt =
            routeStartRef.current?.source === 'reconnect'
              ? timing.startedAt
              : (routeStartRef.current?.at ?? timing.startedAt);
          pendingLoadRef.current = {
            sample: {
              sessionId,
              loadId: timing.loadId,
              at: new Date().toISOString(),
              start: routeStartRef.current?.source ?? 'view',
              frameChars: timing.frameChars,
              records: records.length,
              cacheHit,
              since: metadata.since,
              lastSeq,
              durableRecords: durable.length,
              routeToSocketMs: timing.startedAt - routeStartedAt,
              socketOpenMs: timing.openedAt - timing.startedAt,
              historyWaitMs: timing.receivedAt - timing.openedAt,
              parseMs: timing.parsedAt - timing.receivedAt,
              reduceMs: reducedAt - reduceStartedAt,
              ...(chatLoadProfilingRef.current
                ? { resolveMs: resolveFinishedAt - resolveStartedAt }
                : {}),
              ...(reduced.profile ? { reduction: reduced.profile } : {}),
              commitMs: 0,
              afterPaintMs: 0,
              totalMs: 0,
            },
            reducedAt,
            routeStartedAt,
          };
          setHistoryLoaded(true);
          if (!running) {
            socket.close();
            socketRef.current = null;
            setStatus('gone');
          }
        },
        onRecord: (rec) => {
          if (socketRef.current !== socket) return;
          // Resolution is reported immediately rather than with the rAF batch:
          // clearing a saved draft one frame before the card unmounts is harmless.
          const rid = resolvesRequest(protocolRef.current, rec);
          if (rid) onRequestResolvedRef.current?.(rid);
          pendingRef.current.push(rec);
          schedule();
        },
        onStatus: (s) => {
          if (socketRef.current !== socket) return;
          // On reconnect the socket stays the same, so the connection effect
          // does not run. A disconnected transition invalidates the previously
          // loaded history; the next historyLoaded = true arrives with the new
          // history frame after the new socket opens.
          if (s === 'disconnected') {
            routeStartRef.current = { sessionId, at: performance.now(), source: 'reconnect' };
            setHistoryLoaded(false);
          }
          setStatus(s);
        },
        onError: (message) => {
          if (socketRef.current === socket) setError(message);
        },
      },
      () => {
        requested = getCachedConversation(sessionId);
        return requested?.lastSeq;
      }
    );
    socketRef.current = socket;
    socket.connect();
    return () => {
      socket.close();
      socketRef.current = null;
      pendingRef.current = [];
      if (frameRef.current !== null) {
        if (typeof cancelAnimationFrame === 'function') cancelAnimationFrame(frameRef.current);
        else clearTimeout(frameRef.current);
        frameRef.current = null;
      }
    };
  }, [sessionId, running, schedule]);

  const send = useCallback((text: string, images: ChatImage[]) => {
    socketRef.current?.send(text, images);
  }, []);
  const interrupt = useCallback(() => {
    socketRef.current?.interrupt();
  }, []);
  const answerPermission = useCallback(
    (
      requestId: string,
      allow: boolean,
      updatedInput?: Record<string, unknown>,
      message?: string
    ) => {
      socketRef.current?.permission(requestId, allow, updatedInput, message);
    },
    []
  );
  const answerQuestion = useCallback(
    (requestId: string, answers: Record<string, string[]>, input: Record<string, unknown>) => {
      socketRef.current?.answer(requestId, answers, input);
    },
    []
  );
  const abort = useCallback((requestId: string) => {
    socketRef.current?.abort(requestId);
  }, []);

  return {
    conversation: stateSessionIdRef.current === sessionId ? conversation : emptyConversation(),
    status: stateSessionIdRef.current === sessionId ? status : sessionId ? 'connecting' : 'gone',
    error: stateSessionIdRef.current === sessionId ? error : null,
    historyLoaded: stateSessionIdRef.current === sessionId ? historyLoaded : false,
    send,
    interrupt,
    answerPermission,
    answerQuestion,
    abort,
  };
}

function reduceWithTelemetry(
  protocol: ChatProtocol,
  base: Conversation,
  records: ConversationRecord[]
): { conversation: Conversation; profile: ChatReductionProfile } {
  const startedAt = performance.now();
  let conversation = base;
  const categories = new Map<string, ChatReductionProfile['categories'][number]>();
  let measuredMs = 0;
  for (const record of records) {
    const category = chatRecordCategory(record);
    const recordStartedAt = performance.now();
    conversation = applyRecord(protocol, conversation, record);
    const durationMs = performance.now() - recordStartedAt;
    measuredMs += durationMs;
    const bucket = categories.get(category) ?? { category, records: 0, durationMs: 0, maxMs: 0 };
    bucket.records++;
    bucket.durationMs += durationMs;
    bucket.maxMs = Math.max(bucket.maxMs, durationMs);
    categories.set(category, bucket);
  }
  let turns = 0;
  let segments = 0;
  let images = 0;
  for (const item of conversation.items) {
    if (item.kind === 'user') {
      images += item.images.length;
    } else {
      turns++;
      segments += item.segments.length;
      for (const segment of item.segments) {
        if (segment.kind === 'user') images += segment.images.length;
      }
    }
  }
  const profile: ChatReductionProfile = {
    categories: [...categories.values()].sort((a, b) => b.durationMs - a.durationMs),
    probeOverheadMs: Math.max(0, performance.now() - startedAt - measuredMs),
    items: conversation.items.length,
    turns,
    segments,
    images,
    operations: Object.keys(conversation.activity.operations).length,
  };
  return { conversation, profile };
}

function chatRecordCategory(record: ConversationRecord): string {
  if (record.type !== 'harness' && record.type !== 'control') return record.type;
  const line = record.line as Record<string, unknown>;
  const method = typeof line.method === 'string' ? line.method : line.type;
  const item = (line.params as { item?: { type?: unknown } } | undefined)?.item;
  const itemType = typeof item?.type === 'string' ? item.type : line.subtype;
  return [record.type, method, itemType]
    .filter((part): part is string => typeof part === 'string' && part.length > 0)
    .map((part) => part.slice(0, 80))
    .join('/');
}
