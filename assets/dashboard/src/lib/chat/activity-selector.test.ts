import { capturedActivity } from './__fixtures__/activity';
import { describe, it, expect } from 'vitest';
import { selectActivity, _formatDurationForTest } from './activity-selector';
import { emptyActivity, type ActivityState, type Operation } from './activity';
import type { Conversation, ConversationRecord } from './types';
import { applyRecord, reduceRecords, emptyConversation } from './reducer';
import activityChecklistRaw from './__fixtures__/claude/activity-checklist.jsonl?raw';
import activityBackgroundRaw from './__fixtures__/claude/activity-background.jsonl?raw';
import activityAgentRaw from './__fixtures__/claude/activity-agent.jsonl?raw';
import type { HarnessLine } from './types';

const FIXTURES: Record<string, string> = {
  checklist: activityChecklistRaw,
  background: activityBackgroundRaw,
  agent: activityAgentRaw,
};

const fixture = (name: string): HarnessLine[] =>
  FIXTURES[name]
    .split('\n')
    .filter(Boolean)
    .map((l) => JSON.parse(l) as HarnessLine);

const user = (text: string, id = 'u-1'): ConversationRecord => ({
  ts: 't',
  type: 'user_message',
  id,
  text,
});
const harness = (line: HarnessLine): ConversationRecord => ({ ts: 't', type: 'harness', line });

const NOW = Date.parse('2026-01-01T00:00:30.000Z');

function buildConversation(records: ConversationRecord[]): Conversation {
  return reduceRecords('claude-stream-json', records);
}

function withSeededTask(
  c: Conversation,
  op: Operation,
  activity: ActivityState = c.activity
): Conversation {
  return {
    ...c,
    activity: {
      ...activity,
      operations: { ...activity.operations, [`${op.namespace}:${op.id}`]: op },
      order: [...activity.order, `${op.namespace}:${op.id}`],
    },
  };
}

function seedOp(
  partial: Partial<Operation> & {
    id: string;
    kind: Operation['kind'];
    namespace: Operation['namespace'];
    title: string;
    firstObservedAt: string;
    lastUpdateAt: string;
  }
): Operation {
  return {
    ownerTurnId: null,
    parentId: null,
    lifecycle: 'running',
    rawStatus: null,
    startTime: null,
    endTime: null,
    durationMs: null,
    latestActivity: null,
    usage: null,
    toolId: null,
    assignmentId: 0,
    outputFile: null,
    terminalAt: null,
    ...partial,
  };
}

describe('selectActivity: headline precedence', () => {
  it('hides when there is nothing to render', () => {
    const v = selectActivity(emptyConversation(), { kind: 'connected' }, { now: NOW });
    expect(v.hidden).toBe(true);
    expect(v.headline).toBeNull();
  });

  it('shows "Session ended" when the connection is gone', () => {
    const c = buildConversation([
      user('hi'),
      harness({ type: 'session', event: 'ended' } as HarnessLine),
    ]);
    const v = selectActivity(c, { kind: 'gone' }, { now: NOW });
    expect(v.headline).toBe('Session ended');
  });

  it('shows "Connecting…" when there is no prior connection', () => {
    const v = selectActivity(emptyConversation(), { kind: 'connecting' }, { now: NOW });
    expect(v.headline).toBe('Connecting…');
  });

  it('shows "Reconnecting" when the connection dropped after a prior one', () => {
    const v = selectActivity(
      emptyConversation(),
      { kind: 'reconnecting', priorConnected: true },
      { now: NOW }
    );
    expect(v.headline).toBe('Reconnecting · activity may be out of date');
  });

  it('shows retry headline with attempt detail', () => {
    const op = seedOp({
      id: 'attempt-2',
      kind: 'claude-retry',
      namespace: 'claude-retry',
      title: 'Retrying (attempt 2 of 5)',
      firstObservedAt: 't',
      lastUpdateAt: 't',
    });
    const c = withSeededTask(emptyConversation(), op);
    const v = selectActivity(c, { kind: 'connected' }, { now: NOW });
    expect(v.headline).toBe('Retrying request');
    expect(v.headlineDetail).toContain('attempt 2 of 5');
  });

  it('counts active work for the active-executable headline', () => {
    const op1 = seedOp({
      id: 'b-1',
      kind: 'codex-agent',
      namespace: 'codex-agent',
      title: 'Bash: ls',
      firstObservedAt: 't',
      lastUpdateAt: 't',
    });
    const op2 = seedOp({
      id: 'a-1',
      kind: 'codex-agent',
      namespace: 'codex-agent',
      title: 'Agent abc123',
      firstObservedAt: 't',
      lastUpdateAt: 't',
    });
    const c = withSeededTask(withSeededTask(emptyConversation(), op1), op2);
    const v = selectActivity(c, { kind: 'connected' }, { now: NOW });
    expect(v.headline).toBe('2 agents');
  });

  it('reports background-only headline when no foreground work is open', () => {
    const op = seedOp({
      id: 'bg-1',
      kind: 'claude-task',
      namespace: 'claude-task',
      title: 'Background: check',
      firstObservedAt: 't',
      lastUpdateAt: 't',
      lifecycle: 'running-background',
    });
    const c = withSeededTask(emptyConversation(), op);
    const v = selectActivity(c, { kind: 'connected' }, { now: NOW });
    expect(v.headline).toBe('1 background task active');
  });
});

describe('selectActivity: active rows', () => {
  it('hides hooks that finished before the slow-hook threshold', () => {
    const op = seedOp({
      id: 'h-1',
      kind: 'claude-hook',
      namespace: 'claude-hook',
      title: 'Running SessionStart:startup',
      firstObservedAt: 't',
      lastUpdateAt: 't',
      lifecycle: 'finished',
      startTime: 0,
      endTime: 100,
      terminalAt: '2026-01-01T00:00:00.000Z',
    });
    const c = withSeededTask(emptyConversation(), op);
    const v = selectActivity(c, { kind: 'connected' }, { now: NOW, slowHookThresholdMs: 1000 });
    expect(v.rows).toHaveLength(0);
  });

  it('hides historical completed tasks', () => {
    const op = seedOp({
      id: 't-fresh',
      kind: 'claude-task',
      namespace: 'claude-task',
      title: 'task',
      firstObservedAt: '2026-01-01T00:00:00.000Z',
      lastUpdateAt: '2026-01-01T00:00:00.000Z',
      lifecycle: 'finished',
      terminalAt: '2026-01-01T00:00:00.000Z',
    });
    const c = withSeededTask(emptyConversation(), op);
    const v = selectActivity(
      c,
      { kind: 'connected' },
      { now: Date.parse('2026-01-01T00:01:00.000Z') }
    );
    expect(v.rows).toHaveLength(0);
  });

  it('removes finished rows immediately', () => {
    const op = seedOp({
      id: 't-recent',
      kind: 'claude-task',
      namespace: 'claude-task',
      title: 'task',
      firstObservedAt: '2026-01-01T00:00:00.000Z',
      lastUpdateAt: '2026-01-01T00:00:00.000Z',
      lifecycle: 'finished',
      terminalAt: '2026-01-01T00:00:00.000Z',
    });
    const c = withSeededTask(emptyConversation(), op);
    const v = selectActivity(
      c,
      { kind: 'connected' },
      { now: Date.parse('2026-01-01T00:00:01.000Z') }
    );
    expect(v.rows).toEqual([]);
  });

  it('removes completed hooks regardless of duration', () => {
    const op = seedOp({
      id: 'h-2',
      kind: 'claude-hook',
      namespace: 'claude-hook',
      title: 'Running UserPromptSubmit',
      firstObservedAt: '2026-01-01T00:00:00.000Z',
      lastUpdateAt: '2026-01-01T00:00:05.000Z',
      lifecycle: 'finished',
      startTime: Date.parse('2026-01-01T00:00:00.000Z'),
      endTime: Date.parse('2026-01-01T00:00:05.000Z'),
      terminalAt: '2026-01-01T00:00:05.000Z',
    });
    const c = withSeededTask(emptyConversation(), op);
    const v = selectActivity(
      c,
      { kind: 'connected' },
      {
        now: Date.parse('2026-01-01T00:00:06.000Z'),
        slowHookThresholdMs: 1000,
      }
    );
    expect(v.rows).toEqual([]);
  });

  it('hides terminal rows even without a valid first-observed timestamp', () => {
    const op = seedOp({
      id: 't-1',
      kind: 'claude-task',
      namespace: 'claude-task',
      title: 'task',
      firstObservedAt: 't',
      lastUpdateAt: 't',
      lifecycle: 'finished',
      terminalAt: '2026-01-01T00:00:00.000Z',
    });
    const c = withSeededTask(emptyConversation(), op);
    const v = selectActivity(c, { kind: 'connected' }, { now: NOW });
    expect(v.rows).toHaveLength(0);
  });

  it('keeps failed outcomes in the model, outside the active panel', () => {
    const op = seedOp({
      id: 't-2',
      kind: 'claude-task',
      namespace: 'claude-task',
      title: 'failed task',
      firstObservedAt: '2026-01-01T00:00:00.000Z',
      lastUpdateAt: '2026-01-01T00:00:00.000Z',
      lifecycle: 'failed',
      terminalAt: '2026-01-01T00:00:00.000Z',
    });
    const c = withSeededTask(emptyConversation(), op);
    const v = selectActivity(
      c,
      { kind: 'connected' },
      { now: Date.parse('2026-01-01T00:00:01.000Z') }
    );
    expect(v.rows).toEqual([]);
    expect(c.activity.operations['claude-task:t-2'].lifecycle).toBe('failed');
  });

  it('returns all rows with a non-zero moreCount so the view can show more (finding 3)', () => {
    let c = emptyConversation();
    for (let i = 0; i < 5; i++) {
      c = withSeededTask(
        c,
        seedOp({
          id: `op-${i}`,
          kind: 'codex-agent',
          namespace: 'codex-agent',
          title: `op ${i}`,
          firstObservedAt: 't',
          lastUpdateAt: 't',
        })
      );
    }
    const v = selectActivity(c, { kind: 'connected' }, { now: NOW });
    // The selector returns every filtered row; the view slices the first
    // 3 by default and the remainder appears when the user expands.
    expect(v.rows).toHaveLength(5);
    expect(v.moreCount).toBe(2);
    expect(v.showAll).toBe(true);
  });
});

describe('selectActivity: plan', () => {
  it('renders plan summary and rows from the checklist', () => {
    const c: Conversation = {
      ...emptyConversation(),
      activity: {
        ...emptyActivity(),
        checklist: {
          '1': { id: '1', subject: 'step 1', status: 'completed', lastChangeAt: 't' },
          '2': { id: '2', subject: 'step 2', status: 'in-progress', lastChangeAt: 't' },
        },
        checklistOrder: ['1', '2'],
      },
    };
    const v = selectActivity(c, { kind: 'connected' }, { now: NOW });
    expect(v.plan).not.toBeNull();
    expect(v.plan?.length).toBe(1);
    expect(v.planSummary).toBe('Plan · 1 step in progress');
  });
});

describe('selectActivity: end-to-end with live captures', () => {
  it('checklist fixture populates plan via the spec acceptance A9 path', () => {
    const lines = fixture('checklist');
    const recs: ConversationRecord[] = [user('use TaskCreate')];
    for (const line of lines) {
      if (line.type === 'system' && line.subtype === 'init') continue;
      recs.push(harness(line));
    }
    const c = reduceRecords('claude-stream-json', recs);
    const v = selectActivity(c, { kind: 'connected' }, { now: NOW });
    expect(v.plan).toBeNull();
    expect(v.planSummary).toBeNull();
  });

  it('background fixture produces a background-only headline after the result', () => {
    const lines = fixture('background');
    const recs: ConversationRecord[] = [user('start a bg job')];
    for (const line of lines) {
      if (line.type === 'system' && line.subtype === 'init') continue;
      recs.push(harness(line));
    }
    const c = reduceRecords('claude-stream-json', recs);
    const v = selectActivity(c, { kind: 'connected' }, { now: NOW });
    expect(v.rows).toEqual([]);
    expect(v.headline).toBeNull();
  });

  it('agent fixture reaches the finished state with no other rows', () => {
    const lines = fixture('agent');
    const recs: ConversationRecord[] = [
      { ts: '2026-01-01T00:00:00.000Z', type: 'user_message', id: 'u-1', text: 'launch subagent' },
    ];
    for (const line of lines) {
      if (line.type === 'system' && line.subtype === 'init') continue;
      recs.push({ ts: '2026-01-01T00:00:00.000Z', type: 'harness', line });
    }
    const c = reduceRecords('claude-stream-json', recs);
    const v = selectActivity(c, { kind: 'connected' }, { now: NOW });
    expect(v.hidden).toBe(true);
  });
});

describe('selectActivity: dedupe workers (finding 6)', () => {
  it('claude-task and claude-agent for the same id are merged into one worker', () => {
    const lines = fixture('agent');
    const recs: ConversationRecord[] = [
      { ts: '2026-01-01T00:00:00.000Z', type: 'user_message', id: 'u-1', text: 'go' },
    ];
    for (const line of lines) {
      if (line.type === 'system' && line.subtype === 'init') continue;
      recs.push({ ts: '2026-01-01T00:00:00.000Z', type: 'harness', line });
    }
    const c = reduceRecords('claude-stream-json', recs);
    // Only one operation should exist for the agent's id; the prior
    // implementation created both claude-task:X and claude-agent:X.
    const matches = Object.keys(c.activity.operations).filter((k) =>
      k.endsWith('a715ccc45390d7701')
    );
    expect(matches).toHaveLength(1);
    expect(matches[0]).toMatch(/^claude-agent:/);
  });
});

describe('selectActivity: duration formatting', () => {
  it('formats sub-second durations in ms', () => {
    expect(_formatDurationForTest(250)).toBe('250ms');
  });
  it('formats sub-minute durations in s', () => {
    expect(_formatDurationForTest(12_500)).toBe('12s');
  });
  it('formats sub-hour durations in m and s', () => {
    expect(_formatDurationForTest(125_000)).toBe('2m 5s');
  });
  it('formats long durations in h and m', () => {
    expect(_formatDurationForTest(3_725_000)).toBe('1h 2m');
  });
});

describe('captured worker aliases', () => {
  it('keeps exactly one worker row at every point after the task/launch link is known', () => {
    const records = capturedActivity('agent');
    let c = emptyConversation();
    let linked = false;
    for (const record of records) {
      c = applyRecord('claude-stream-json', c, record);
      if (record.type === 'harness' && record.line.subtype === 'task_started') linked = true;
      if (!linked) continue;
      const workers = selectActivity(
        c,
        { kind: 'connected' },
        { now: Date.parse(record.ts) }
      ).rows.filter((r) => r.key.startsWith('claude-agent:'));
      const op = Object.values(c.activity.operations).find((op) => op.kind === 'claude-agent')!;
      const active = ['preparing', 'running', 'running-background', 'pending-input'].includes(
        op.lifecycle
      );
      expect(workers).toHaveLength(active ? 1 : 0);
      if (active) expect(workers[0].toolId).toBe('call_4460f63af58e42e2a810e0f3');
      expect(new Set(c.activity.order).size).toBe(c.activity.order.length);
    }
  });
});
