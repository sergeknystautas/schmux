import { useCallback, useEffect, useReducer, useRef } from 'react';
import { transport } from '../lib/transport';
import {
  reduce,
  initialState,
  TERMINAL_REASONS,
  type DocEvent,
  type DocState,
  type DocStatus,
  type Effect,
} from '../lib/markdownDocument';
import type { MarkdownDocument } from '../lib/types.generated';

// useMarkdownDocument binds the pure document reducer to one WebSocket on
// /ws/markdown/{workspaceId}/{path}. It owns the 500 ms autosave timer and
// reconnection; every decision about what to send lives in the reducer.

export interface MarkdownDocumentHandle {
  draft: string;
  status: DocStatus;
  reason: string | null;
  onEdit: (text: string) => void;
}

const AUTOSAVE_MS = 500;
const RECONNECT_DELAY_MS = 2000;
const MAX_RECONNECT_DELAY_MS = 30000;

interface WebSocketLike {
  onopen: (() => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: ((ev: { code: number; reason: string }) => void) | null;
  onerror: (() => void) | null;
  send(data: string): void;
  close(): void;
}

const newId = () =>
  typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;

export default function useMarkdownDocument(
  workspaceId: string,
  filePath: string
): MarkdownDocumentHandle {
  // The reducer runs synchronously against a ref, then a counter bump
  // re-renders. React's useReducer is not used because it may defer running
  // the reducer until render, which would run effects against stale state.
  const stateRef = useRef<DocState>(initialState);
  const [, rerender] = useReducer((n: number) => n + 1, 0);

  const wsRef = useRef<WebSocketLike | null>(null);
  const timerRef = useRef<number | null>(null);
  const reconnectRef = useRef<number | null>(null);
  const reconnectDelayRef = useRef(RECONNECT_DELAY_MS);
  const stoppedRef = useRef(false);

  const dispatch = useCallback((e: DocEvent) => {
    const [next, effects] = reduce(stateRef.current, e, newId);
    stateRef.current = next;
    rerender();
    runEffects(effects);
  }, []);

  function runEffects(effects: Effect[]) {
    for (const effect of effects) {
      switch (effect.type) {
        case 'send':
          wsRef.current?.send(
            JSON.stringify({ type: 'save', id: effect.id, base: effect.base, draft: effect.draft })
          );
          break;
        case 'armTimer':
          if (timerRef.current !== null) window.clearTimeout(timerRef.current);
          timerRef.current = window.setTimeout(() => {
            timerRef.current = null;
            dispatch({ type: 'timer' });
          }, AUTOSAVE_MS);
          break;
        case 'cancelTimer':
          if (timerRef.current !== null) {
            window.clearTimeout(timerRef.current);
            timerRef.current = null;
          }
          break;
      }
    }
  }

  useEffect(() => {
    // A new workspace/path is a new document. The router reuses this page
    // element between Markdown routes, so the previous file's base, draft,
    // and in-flight save must not survive into this socket.
    stateRef.current = initialState;
    rerender();
    stoppedRef.current = false;
    reconnectDelayRef.current = RECONNECT_DELAY_MS;

    const connect = () => {
      if (stoppedRef.current) return;
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const url = `${protocol}//${window.location.host}/ws/markdown/${workspaceId}/${encodeURIComponent(filePath)}`;
      const ws = transport.createWebSocket(url) as unknown as WebSocketLike;
      wsRef.current = ws;

      ws.onopen = () => {
        if (wsRef.current !== ws) return;
        reconnectDelayRef.current = RECONNECT_DELAY_MS;
        dispatch({ type: 'open' });
      };
      ws.onmessage = (event) => {
        if (wsRef.current !== ws) return;
        let msg: MarkdownDocument;
        try {
          msg = JSON.parse(event.data as string) as MarkdownDocument;
        } catch {
          return;
        }
        if (msg.type !== 'document') return;
        dispatch({ type: 'document', content: msg.content, reply: msg.reply });
      };
      ws.onclose = (event) => {
        if (wsRef.current !== ws) return;
        wsRef.current = null;
        const reason = event.reason || null;
        dispatch({ type: 'close', reason });
        if (stoppedRef.current || (reason && TERMINAL_REASONS.has(reason))) return;
        const jitter = reconnectDelayRef.current * (0.5 + Math.random());
        reconnectRef.current = window.setTimeout(() => {
          reconnectDelayRef.current = Math.min(
            reconnectDelayRef.current * 2,
            MAX_RECONNECT_DELAY_MS
          );
          connect();
        }, jitter);
      };
      ws.onerror = () => {
        // onclose follows; nothing to do here.
      };
    };

    connect();

    return () => {
      stoppedRef.current = true;
      if (timerRef.current !== null) window.clearTimeout(timerRef.current);
      if (reconnectRef.current !== null) window.clearTimeout(reconnectRef.current);
      const ws = wsRef.current;
      wsRef.current = null;
      ws?.close();
    };
  }, [workspaceId, filePath, dispatch]);

  const onEdit = useCallback((text: string) => dispatch({ type: 'edit', text }), [dispatch]);

  const state = stateRef.current;
  return { draft: state.draft, status: state.status, reason: state.reason, onEdit };
}
