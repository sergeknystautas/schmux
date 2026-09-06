import { useCallback, useEffect, useRef, useState } from 'react';
import { ChatSocket } from '../lib/chat/socket';
import type { ChatSocketStatus } from '../lib/chat/socket';
import { applyRecord, emptyConversation, reduceRecords } from '../lib/chat/reducer';
import type { ChatImage, ChatProtocol, Conversation, ConversationRecord } from '../lib/chat/types';

export function useChatSocket(
  sessionId: string | undefined,
  running: boolean
): {
  conversation: Conversation;
  status: ChatSocketStatus;
  error: string | null;
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
  const socketRef = useRef<ChatSocket | null>(null);
  // The protocol arrives with the history frame and selects the reducer for
  // every record after it.
  const protocolRef = useRef<ChatProtocol>('claude-stream-json');
  // Live records buffer: applied as one setConversation call per animation
  // frame, so a burst of deltas costs one render, not N.
  const pendingRef = useRef<ConversationRecord[]>([]);
  const frameRef = useRef<number | null>(null);

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
    if (!sessionId || !running) {
      setStatus('gone');
      return;
    }
    setStatus('connecting');
    setError(null);
    setConversation(emptyConversation());
    pendingRef.current = [];
    if (frameRef.current !== null) {
      if (typeof cancelAnimationFrame === 'function') cancelAnimationFrame(frameRef.current);
      else clearTimeout(frameRef.current);
      frameRef.current = null;
    }
    const socket = new ChatSocket(sessionId, {
      onHistory: (protocol, records) => {
        protocolRef.current = protocol;
        // A reconnect reloads history. Discard any buffered live records from
        // the prior connection so they cannot be applied twice.
        pendingRef.current = [];
        if (frameRef.current !== null) {
          if (typeof cancelAnimationFrame === 'function') cancelAnimationFrame(frameRef.current);
          else clearTimeout(frameRef.current);
          frameRef.current = null;
        }
        setConversation(reduceRecords(protocol, records));
      },
      onRecord: (rec) => {
        pendingRef.current.push(rec);
        schedule();
      },
      onStatus: setStatus,
      onError: setError,
    });
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

  return { conversation, status, error, send, interrupt, answerPermission, answerQuestion, abort };
}
