import { describe, expect, it } from 'vitest';
import { applyRecord, emptyConversation, reduceRecords } from './reducer';
import type { ConversationRecord, HarnessLine } from './types';

const protocol = 'codex-app-server';
const failure: ConversationRecord = {
  ts: '2026-11-01T17:51:41Z',
  type: 'harness',
  line: {
    id: 3,
    error: { code: -32600, message: 'thread already has an active writer' },
  } as unknown as HarnessLine,
};
const message: ConversationRecord = {
  ts: '2026-11-01T17:51:51Z',
  type: 'user_message',
  id: 'hi',
  text: 'hi',
};

describe('Codex JSON-RPC failures', () => {
  it('keeps startup failures in the transcript after queued input and replay', () => {
    let c = applyRecord(protocol, emptyConversation(), failure);
    const failed = c.items[0];
    expect(failed).toMatchObject({
      kind: 'assistant',
      end: { state: 'error', text: 'thread already has an active writer' },
    });
    expect(c.phase).toBe('idle');

    c = applyRecord(protocol, c, message);
    c = applyRecord(protocol, c, {
      ts: message.ts,
      type: 'user_message_queue',
      id: message.id,
      queued: true,
    });
    expect(c.items[0]).toEqual(failed);
    expect(reduceRecords(protocol, [failure, message]).items[0]).toEqual(failed);
  });

  it('closes an existing turn when a client request fails', () => {
    const c = reduceRecords(protocol, [message, failure]);
    expect(c.items).toHaveLength(2);
    expect(c.items[1]).toMatchObject({
      kind: 'assistant',
      end: { state: 'error', text: 'thread already has an active writer' },
    });
    expect(c.phase).toBe('idle');
  });
});
