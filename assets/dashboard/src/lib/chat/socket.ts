// WebSocket lifecycle for a chat session: connects to /ws/chat/{id},
// dispatches frames, and reconnects with backoff until closed or gone.
import { transport } from '../transport';
import type { ChatImage, ChatProtocol, ConversationRecord } from './types';

export type ChatSocketStatus = 'connecting' | 'connected' | 'disconnected' | 'gone';

export interface ChatSocketHandlers {
  onHistory(protocol: ChatProtocol, records: ConversationRecord[]): void;
  onRecord(record: ConversationRecord): void;
  onStatus(status: ChatSocketStatus): void;
  onError?(message: string): void;
}

const initialDelayMs = 500;
const maxDelayMs = 5000;

export class ChatSocket {
  private ws: WebSocket | null = null;
  private closedByUser = false;
  private delayMs = initialDelayMs;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(
    private sessionId: string,
    private handlers: ChatSocketHandlers
  ) {}

  connect(): void {
    this.closedByUser = false;
    this.open();
  }

  close(): void {
    this.closedByUser = true;
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.ws?.close();
    this.ws = null;
  }

  send(text: string, images: ChatImage[]): void {
    this.writeFrame({ type: 'send', text, images });
  }

  interrupt(): void {
    this.writeFrame({ type: 'interrupt' });
  }

  permission(
    requestId: string,
    allow: boolean,
    updatedInput?: Record<string, unknown>,
    message?: string
  ): void {
    this.writeFrame({
      type: 'permission',
      request_id: requestId,
      allow,
      ...(updatedInput ? { updated_input: updatedInput } : {}),
      ...(message ? { message } : {}),
    });
  }

  answer(
    requestId: string,
    answers: Record<string, string[]>,
    input: Record<string, unknown>
  ): void {
    this.writeFrame({ type: 'answer', request_id: requestId, answers, input });
  }

  private writeFrame(frame: Record<string, unknown>): void {
    this.ws?.send(JSON.stringify(frame));
  }

  private open(): void {
    this.handlers.onStatus('connecting');
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/chat/${this.sessionId}`;
    const ws = transport.createWebSocket(wsUrl);
    this.ws = ws;
    ws.onopen = () => {
      this.delayMs = initialDelayMs;
      this.handlers.onStatus('connected');
    };
    ws.onmessage = (ev: MessageEvent) => {
      let frame: {
        type?: string;
        protocol?: ChatProtocol;
        records?: ConversationRecord[];
        record?: ConversationRecord;
        message?: string;
      };
      try {
        frame = JSON.parse(String(ev.data));
      } catch {
        return;
      }
      // Fallback to claude-stream-json for older daemons (rolling reload).
      if (frame.type === 'history') {
        this.handlers.onHistory(
          (frame.protocol as ChatProtocol) ?? 'claude-stream-json',
          frame.records ?? []
        );
      } else if (frame.type === 'record' && frame.record) {
        this.handlers.onRecord(frame.record);
      } else if (frame.type === 'error') {
        this.handlers.onError?.(String(frame.message ?? 'error'));
      }
    };
    ws.onclose = () => {
      if (this.closedByUser) return;
      this.handlers.onStatus('disconnected');
      this.scheduleReconnect();
    };
  }

  private scheduleReconnect(): void {
    if (this.closedByUser) return;
    const delay = this.delayMs;
    this.delayMs = Math.min(this.delayMs * 2, maxDelayMs);
    this.reconnectTimer = setTimeout(() => this.open(), delay);
  }
}
