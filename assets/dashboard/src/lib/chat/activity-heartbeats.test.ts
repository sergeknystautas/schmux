import { describe, expect, it } from 'vitest';
import { applyRecord, emptyConversation, reduceRecords } from './reducer';
import { selectActivity } from './activity-selector';
import type { ConversationRecord } from './types';
import raw from './__fixtures__/claude/activity-heartbeats.jsonl?raw';

const records: ConversationRecord[] = [
  { type: 'user_message', ts: '2026-09-10T02:53:00Z', id: 'u', text: 'Run tests' },
  ...raw
    .trim()
    .split('\n')
    .map((line) => JSON.parse(line) as ConversationRecord),
];

describe('captured command heartbeats', () => {
  it('tracks one actual command across distinct heartbeat event IDs and removes it on completion', () => {
    let c = emptyConversation();
    for (const record of records) {
      c = applyRecord('claude-stream-json', c, record);
      if (record.type !== 'harness') continue;
      expect(c.activity.order).toHaveLength(1);
      expect(c.activity.order.some((key) => key.includes('-heartbeat-'))).toBe(false);
      const rows = selectActivity(c, { kind: 'connected' }, { now: Date.parse(record.ts) }).rows;
      if (record.line.type === 'tool_progress') {
        expect(rows).toMatchObject([
          {
            title: 'Re-run level-authoring gate',
            command: expect.stringContaining('just test-editor'),
            status: 'running',
            durationMs: Number(record.line.elapsed_time_seconds) * 1000,
          },
        ]);
      }
      if (record.line.subtype === 'task_notification') {
        expect(rows).toEqual([]);
        expect(c.activity.operations['claude-task:b912uto5u'].lifecycle).toBe('finished');
      }
    }
  });

  it('replays history then receives a heartbeat and completion without phantom workers', () => {
    let c = reduceRecords('claude-stream-json', records.slice(0, 5));
    for (const record of records.slice(5)) c = applyRecord('claude-stream-json', c, record);
    expect(c.activity.order).toEqual(['claude-task:b912uto5u']);
    expect(
      selectActivity(c, { kind: 'connected' }, { now: Date.parse(records[records.length - 1].ts) })
        .rows
    ).toEqual([]);
  });
});
