import { describe, expect, it } from 'vitest';
import capture from './__fixtures__/codex/activity-live.jsonl?raw';
import { applyRecord, emptyConversation } from './reducer';
import { selectActivity } from './activity-selector';
import type { AssistantTurn, Conversation, ConversationRecord, HarnessLine } from './types';

const parent = '01a0871a-b6eb-7273-b266-19181f7206e2';
const child = '01a0871a-d6bd-7072-9c19-dc938a1deaf5';
const launch = 'call_45TAmIjgCduUBnFCTCrF7esR';
const records: ConversationRecord[] = capture
  .trim()
  .split('\n')
  .map((raw) => {
    const line = JSON.parse(raw) as HarnessLine;
    return { type: 'harness', line, ts: new Date(line.emittedAtMs as number).toISOString() };
  });
function replayUntil(stop: (line: HarnessLine) => boolean): Conversation {
  let c = applyRecord('codex-app-server', emptyConversation(), {
    type: 'user_message',
    id: 'probe',
    ts: records[0].ts,
    text: 'Run the arithmetic probe',
  });
  for (const record of records) {
    c = applyRecord('codex-app-server', c, record);
    if (record.type === 'harness' && stop(record.line)) break;
  }
  return c;
}
const params = (line: HarnessLine) =>
  line.params as {
    threadId?: string;
    item?: { type: string; id: string; kind?: string };
    turn?: { id: string };
  };
const turn = (c: Conversation) =>
  c.items.find((item) => item.kind === 'assistant') as AssistantTurn;

describe('Codex 0.153.4 live activity capture', () => {
  it('reopens the captured child on an explicit new turn without reopening the parent', () => {
    const finished = replayUntil(() => false);
    // Synthetic follow-up using the captured child-turn shape; not a live follow-up capture.
    const started = records.find(
      (r) =>
        r.type === 'harness' &&
        r.line.method === 'turn/started' &&
        params(r.line).threadId === child
    )!;
    if (started.type !== 'harness') throw new Error('Missing child start');
    const now = Date.parse(records[records.length - 1].ts) + 1000;
    const c = applyRecord('codex-app-server', finished, {
      ...started,
      ts: new Date(now).toISOString(),
      line: {
        ...started.line,
        emittedAtMs: now,
        params: {
          threadId: child,
          turn: { id: 'synthetic-follow-up', status: 'inProgress', items: [] },
        },
      },
    });
    expect(c.activity.operations[`codex-agent:${child}`]).toMatchObject({
      lifecycle: 'running',
      assignmentId: 2,
      startTime: now,
      terminalAt: null,
    });
    expect(turn(c)).toBe(turn(finished));
    expect(c.phase).toBe('idle');
  });

  it('keeps the child running when its launch item completes', () => {
    const c = replayUntil(
      (line) => line.method === 'item/completed' && params(line).item?.id === launch
    );
    const op = c.activity.operations[`codex-agent:${child}`];
    expect(op).toMatchObject({
      lifecycle: 'running',
      title: '/root/calculate_17_19',
      assignmentId: 1,
      toolId: launch,
    });
    expect(
      turn(c).segments.filter((segment) => segment.kind === 'tool' && segment.id === launch)
    ).toHaveLength(1);
    const view = selectActivity(c, { kind: 'connected' }, { now: Date.parse(op.lastUpdateAt) });
    expect(view.rows.filter((row) => row.kind === 'codex-agent')).toMatchObject([
      { status: 'running', toolId: launch },
    ]);
  });

  it('finishes one child without closing or inserting child prose into the parent turn', () => {
    const c = replayUntil(
      (line) => line.method === 'turn/completed' && params(line).threadId === child
    );
    expect(turn(c).end).toBeNull();
    expect(c.phase).toBe('running');
    expect(c.activity.operations[`codex-agent:${child}`]).toMatchObject({
      lifecycle: 'finished',
      latestActivity: '323',
      assignmentId: 1,
    });
    expect(
      turn(c).segments.some((segment) => segment.kind === 'prose' && segment.text === '323')
    ).toBe(false);
  });

  it('retains the parent final answer, one child identity, and millisecond hook clocks', () => {
    const c = replayUntil(() => false);
    expect(turn(c).end).toMatchObject({ state: 'done' });
    expect(
      turn(c).segments.some(
        (segment) => segment.kind === 'prose' && segment.text.includes('23 × 29 = 667')
      )
    ).toBe(true);
    expect(
      Object.values(c.activity.operations).filter((op) => op.kind === 'codex-agent')
    ).toMatchObject([{ id: child, lifecycle: 'finished', latestActivity: '323', assignmentId: 1 }]);
    expect(
      c.activity.operations['codex-hook:session-start:2:/home/probe/.codex/hooks.json']
    ).toMatchObject({
      lifecycle: 'finished',
      startTime: 1788973071000,
      endTime: 1788973071000,
      durationMs: 7,
    });
    const parentCompletion = records.find(
      (r) =>
        r.type === 'harness' &&
        r.line.method === 'turn/completed' &&
        params(r.line).threadId === parent
    )!;
    expect(
      selectActivity(c, { kind: 'connected' }, { now: Date.parse(parentCompletion.ts) + 4000 }).rows
    ).toHaveLength(0);
  });
});
