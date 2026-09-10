import { describe, it, expect } from 'vitest';
import { applyRecord as applyAny, reduceRecords as reduceAny, emptyConversation } from './reducer';
import { claudeResolvedRequestId } from './claude';
import type {
  AssistantTurn,
  Conversation,
  ConversationRecord,
  HarnessLine,
  PendingSegment,
  ToolSegment,
  UserMessage,
} from './types';
import questionRawFixture from './__fixtures__/claude/question.jsonl?raw';

const applyRecord = (c: Conversation, r: ConversationRecord) =>
  applyAny('claude-stream-json', c, r);
const reduceRecords = (records: ConversationRecord[]) => reduceAny('claude-stream-json', records);
import permissionAllowRaw from './__fixtures__/claude/permission-allow.jsonl?raw';
import permissionDenyRaw from './__fixtures__/claude/permission-deny.jsonl?raw';
import interruptTextRaw from './__fixtures__/claude/interrupt-text.jsonl?raw';
import questionRaw from './__fixtures__/claude/question.jsonl?raw';
import queuedRaw from './__fixtures__/claude/queued.jsonl?raw';
import queuedInterruptRaw from './__fixtures__/claude/queued-interrupt.jsonl?raw';
import interruptPendingRaw from './__fixtures__/claude/interrupt-pending.jsonl?raw';
import multiselectRaw from './__fixtures__/claude/multiselect.jsonl?raw';
import subagentRaw from './__fixtures__/claude/subagent.jsonl?raw';
import activityChecklistRaw from './__fixtures__/claude/activity-checklist.jsonl?raw';
import activityBackgroundRaw from './__fixtures__/claude/activity-background.jsonl?raw';
import activityAgentRaw from './__fixtures__/claude/activity-agent.jsonl?raw';

const FIXTURES: Record<string, string> = {
  'permission-allow': permissionAllowRaw,
  'permission-deny': permissionDenyRaw,
  'interrupt-text': interruptTextRaw,
  question: questionRaw,
  queued: queuedRaw,
  'queued-interrupt': queuedInterruptRaw,
  'interrupt-pending': interruptPendingRaw,
  multiselect: multiselectRaw,
  subagent: subagentRaw,
  'activity-checklist': activityChecklistRaw,
  'activity-background': activityBackgroundRaw,
  'activity-agent': activityAgentRaw,
};

const fixture = (name: string): HarnessLine[] =>
  FIXTURES[name]
    .split('\n')
    .filter(Boolean)
    .map((l) => JSON.parse(l) as HarnessLine);

const user = (text: string, id = 'u-' + text.length): ConversationRecord => ({
  ts: 't',
  type: 'user_message',
  id,
  text,
});
const harness = (line: HarnessLine): ConversationRecord => ({ ts: 't', type: 'harness', line });
const control = (line: HarnessLine): ConversationRecord => ({ ts: 't', type: 'control', line });

/** Replays a probe slice as the daemon would have recorded it: our control
 *  record precedes each echoed control_response; interrupts precede int-* acks. */
function replay(
  userText: string,
  lines: HarnessLine[],
  extraUsers: Record<string, string> = {}
): ConversationRecord[] {
  const out: ConversationRecord[] = [user(userText)];
  for (const line of lines) {
    if (line.type === 'control_response') {
      const rid = (line.response as { request_id: string }).request_id;
      if (rid.startsWith('int-')) {
        out.push(
          control({ type: 'control_request', request_id: rid, request: { subtype: 'interrupt' } })
        );
      } else {
        out.push(control(line));
      }
    }
    if (line.type === 'user' && line.isReplay === true) {
      const text = String((line.message as { content: unknown }).content);
      if (extraUsers[text]) out.push(user(text, extraUsers[text]));
    }
    out.push(harness(line));
  }
  return out;
}

const lastTurn = (c: ReturnType<typeof reduceRecords>): AssistantTurn =>
  [...c.items].reverse().find((i) => i.kind === 'assistant') as AssistantTurn;

describe('reducer: user messages', () => {
  it('a user_message opens a user-initiated turn', () => {
    const c = applyRecord(emptyConversation(), user('hi'));
    expect(c.items[0]).toMatchObject({ kind: 'user', text: 'hi', queued: false });
    expect(c.items[1]).toMatchObject({ kind: 'assistant', end: null });
    expect(c.phase).toBe('running');
  });
  it('a task notification opens an assistant-initiated background turn', () => {
    const c = reduceRecords([
      harness({
        type: 'system',
        subtype: 'task_notification',
        status: 'completed',
        task_id: 'build-monitor',
      }),
      harness({ type: 'system', subtype: 'status', status: 'requesting' }),
      harness({
        type: 'assistant',
        message: { content: [{ type: 'text', text: 'The monitored build passed.' }] },
      }),
      harness({
        type: 'result',
        is_error: false,
        result: 'The monitored build passed.',
        origin: { kind: 'task-notification' },
      }),
    ]);

    expect(c.items.filter((i) => i.kind === 'user')).toHaveLength(0);
    expect(c.items.filter((i) => i.kind === 'assistant')).toHaveLength(1);
    expect(lastTurn(c)).toMatchObject({
      segments: [{ kind: 'prose', text: 'The monitored build passed.', streaming: false }],
      end: { state: 'done' },
    });
    expect(c.phase).toBe('idle');
    expect(c.activity.operations['claude-task:build-monitor']).toMatchObject({
      lifecycle: 'finished',
      terminalAt: 't',
    });
    expect(c.activity.order).toEqual(['claude-task:build-monitor']);
  });
  it('a child task notification updates activity without opening a parent turn', () => {
    const c = reduceRecords([
      harness({
        type: 'system',
        subtype: 'task_notification',
        parent_tool_use_id: 'agent-launch',
        task_id: 'child-build',
        status: 'completed',
      }),
    ]);
    expect(c.items).toEqual([]);
    expect(c.phase).toBe('idle');
    expect(c.activity.operations['claude-task:child-build'].lifecycle).toBe('finished');
  });
  it('harness user records never become user items', () => {
    const c = reduceRecords(replay('Run it', fixture('permission-allow')));
    expect(c.items.filter((i) => i.kind === 'user')).toHaveLength(1);
  });
  it('a message sent while a turn is open is queued until its replay', () => {
    const lines = fixture('queued');
    const recs = replay(
      'Without tools, write a numbered list of 40 fruits, one per line, with a one-sentence description each.',
      lines,
      {
        'Without tools reply with exactly: QUEUED-OK': 'u-q',
      }
    );
    // Move the queued user_message to right after the first text delta so it is sent mid-turn.
    const qi = recs.findIndex((r) => r.type === 'user_message' && r.id === 'u-q');
    const [q] = recs.splice(qi, 1);
    const firstDelta = recs.findIndex(
      (r) => r.type === 'harness' && r.line.type === 'stream_event'
    );
    recs.splice(firstDelta + 1, 0, q);
    let c = emptyConversation();
    let sawQueued = false;
    for (const r of recs) {
      c = applyRecord(c, r);
      const um = c.items.find((i) => i.kind === 'user' && (i as UserMessage).id === 'u-q') as
        UserMessage | undefined;
      if (um?.queued) sawQueued = true;
    }
    expect(sawQueued).toBe(true);
    const um = c.items.find(
      (i) => i.kind === 'user' && (i as UserMessage).id === 'u-q'
    ) as UserMessage;
    expect(um.queued).toBe(false);
    const turns = c.items.filter((i) => i.kind === 'assistant') as AssistantTurn[];
    expect(turns).toHaveLength(2);
    expect(turns[1].end).toEqual({ state: 'done' });
    expect(c.phase).toBe('idle');
  });
});

describe('reducer: tools and permissions', () => {
  it('tool call goes preparing → running → done and the card is removed on our answer', () => {
    const recs = replay('Run it', fixture('permission-allow'));
    let c = emptyConversation();
    const states: string[] = [];
    let sawPending = false;
    for (const r of recs) {
      c = applyRecord(c, r);
      const t = lastTurn(c);
      const tool = t?.segments.find((s) => s.kind === 'tool') as ToolSegment | undefined;
      if (tool && states[states.length - 1] !== tool.state) states.push(tool.state);
      if (t?.segments.some((s) => s.kind === 'pending')) sawPending = true;
    }
    expect(states).toEqual(['preparing', 'running', 'done']);
    expect(sawPending).toBe(true);
    const t = lastTurn(c);
    expect(t.segments.some((s) => s.kind === 'pending')).toBe(false);
    const tool = t.segments.find((s) => s.kind === 'tool') as ToolSegment;
    expect(tool.name).toBe('Bash');
    expect(tool.result).toBe('42');
    expect(t.end).toEqual({ state: 'done' });
  });
  it('a denied permission marks the tool as error with the deny message', () => {
    const c = reduceRecords(replay('Run it', fixture('permission-deny')));
    const tool = lastTurn(c).segments.find((s) => s.kind === 'tool') as ToolSegment;
    expect(tool.state).toBe('error');
    expect(tool.result).toContain('denied');
  });
  it('the pending card sits directly after its tool row', () => {
    const recs = replay('Run it', fixture('permission-allow'));
    let c = emptyConversation();
    for (const r of recs) {
      c = applyRecord(c, r);
      const segs = lastTurn(c)?.segments ?? [];
      const pi = segs.findIndex((s) => s.kind === 'pending');
      if (pi >= 0) expect(segs[pi - 1]).toMatchObject({ kind: 'tool', name: 'Bash' });
    }
  });
});

describe('reducer: questions', () => {
  it('AskUserQuestion becomes a question card and is removed on answer', () => {
    const recs = replay('Ask me', fixture('question'));
    let c = emptyConversation();
    let card: unknown;
    for (const r of recs) {
      c = applyRecord(c, r);
      const p = lastTurn(c)?.segments.find((s) => s.kind === 'pending');
      if (p) card = p;
    }
    expect(card).toMatchObject({ toolName: 'AskUserQuestion' });
    expect((card as { questions: unknown[] }).questions).toHaveLength(1);
    expect(lastTurn(c).segments.some((s) => s.kind === 'pending')).toBe(false);
    expect(lastTurn(c).end).toEqual({ state: 'done' });
  });
  it('claude questions are keyed by their text', () => {
    const recs = questionRawFixture
      .split('\n')
      .filter(Boolean)
      .map((l) => JSON.parse(l) as HarnessLine);
    const out: ConversationRecord[] = [{ ts: 't', type: 'user_message', id: 'u0', text: 'ask' }];
    for (const l of recs) {
      if (l.type === 'control_response') out.push({ ts: 't', type: 'control', line: l });
      out.push({ ts: 't', type: 'harness', line: l });
    }
    let c = emptyConversation();
    let pending: PendingSegment | undefined;
    for (const r of out) {
      c = applyRecord(c, r);
      pending = lastTurn(c).segments.find((s) => s.kind === 'pending') as PendingSegment;
    }
    expect(pending?.questions?.[0].id).toBe(pending?.questions?.[0].question);
  });
  it('multi-select fixture parses options', () => {
    const recs = replay('Toppings', fixture('multiselect'));
    let c = emptyConversation();
    let card: { questions: { multiSelect?: boolean; options: unknown[] }[] } | undefined;
    for (const r of recs) {
      c = applyRecord(c, r);
      const p = lastTurn(c)?.segments.find((s) => s.kind === 'pending') as typeof card;
      if (p) card = p;
    }
    expect(card?.questions[0].multiSelect).toBe(true);
    expect(card?.questions[0].options).toHaveLength(3);
  });
});

describe('reducer: prose, thinking, stop, errors', () => {
  it('text deltas stream and the durable block replaces them', () => {
    const recs = replay('List', fixture('queued'));
    let c = emptyConversation();
    let sawStreaming = false;
    for (const r of recs) {
      c = applyRecord(c, r);
      const p = lastTurn(c)?.segments.find((s) => s.kind === 'prose');
      if (p && (p as { streaming: boolean }).streaming) sawStreaming = true;
    }
    expect(sawStreaming).toBe(true);
    const prose = lastTurn(c).segments.filter((s) => s.kind === 'prose');
    expect(prose.every((p) => !(p as { streaming: boolean }).streaming)).toBe(true);
    expect((prose[0] as { text: string }).text).toContain('1.');
  });
  it('empty thinking renders no segment; the flag is set only while in progress', () => {
    const recs = replay('Run it', fixture('permission-allow'));
    let c = emptyConversation();
    let sawThinking = false;
    for (const r of recs) {
      c = applyRecord(c, r);
      if (lastTurn(c)?.thinking) sawThinking = true;
    }
    expect(sawThinking).toBe(true);
    expect(lastTurn(c).thinking).toBe(false);
    expect(lastTurn(c).segments.some((s) => s.kind === 'thinking')).toBe(false);
  });
  it('a recorded interrupt makes the turn stopped, not error', () => {
    const c = reduceRecords(replay('List', fixture('interrupt-text')));
    expect(lastTurn(c).end).toEqual({ state: 'stopped' });
    expect(c.phase).toBe('idle');
  });
  it('an interrupt with a pending card removes the card and marks the tool interrupted', () => {
    const c = reduceRecords(replay('Run it', fixture('interrupt-pending')));
    const t = lastTurn(c);
    expect(t.end).toEqual({ state: 'stopped' });
    expect(t.segments.some((s) => s.kind === 'pending')).toBe(false);
    const tool = t.segments.find((s) => s.kind === 'tool') as ToolSegment;
    expect(tool.state).toBe('error');
  });
  it('a queued message survives an interrupt and gets its own turn', () => {
    const lines = fixture('queued-interrupt');
    const recs = replay(
      'Without tools, write a numbered list of 80 vegetables, one per line, each with a one-sentence description.',
      lines,
      {
        'Without tools reply with exactly: QUEUED-SURVIVED': 'u-q',
      }
    );
    const qi = recs.findIndex((r) => r.type === 'user_message' && r.id === 'u-q');
    const [q] = recs.splice(qi, 1);
    const ii = recs.findIndex((r) => r.type === 'control');
    recs.splice(ii, 0, q);
    const c = reduceRecords(recs);
    const turns = c.items.filter((i) => i.kind === 'assistant') as AssistantTurn[];
    expect(turns.map((t) => t.end)).toEqual([{ state: 'stopped' }, { state: 'done' }]);
    expect(c.phase).toBe('idle');
  });
  it('a result with is_error and no interrupt is an error with the subtype as text', () => {
    let c = applyRecord(emptyConversation(), user('x'));
    c = applyRecord(
      c,
      harness({ type: 'result', subtype: 'error_max_turns', is_error: true, result: null })
    );
    expect(lastTurn(c).end).toEqual({ state: 'error', text: 'error_max_turns' });
  });
  it('unknown records render nothing', () => {
    let c = applyRecord(emptyConversation(), user('x'));
    const before = JSON.stringify(c);
    for (const line of [
      { type: 'system', subtype: 'init' },
      { type: 'rate_limit_event' },
      { type: 'wat' },
    ]) {
      c = applyRecord(c, harness(line as HarnessLine));
    }
    expect(JSON.stringify(c)).toBe(before);
  });
  it('an interrupt control record outside a turn does nothing', () => {
    const c = applyRecord(
      emptyConversation(),
      control({ type: 'control_request', request_id: 'int-1', request: { subtype: 'interrupt' } })
    );
    expect(c).toEqual(emptyConversation());
  });
});

describe('reducer: session ended', () => {
  it('closes an open turn as stopped, clears queued flags, and idles', () => {
    let c = applyRecord(emptyConversation(), user('first', 'u1'));
    c = applyRecord(
      c,
      harness({
        type: 'stream_event',
        event: {
          type: 'content_block_start',
          index: 0,
          content_block: { type: 'text', text: '' },
        },
      })
    );
    c = applyRecord(c, user('second', 'u2'));
    expect((c.items[2] as UserMessage).queued).toBe(true);
    c = applyRecord(c, { ts: 't', type: 'session', event: 'ended' });
    expect(lastTurn(c).end).toEqual({ state: 'stopped' });
    expect((c.items[2] as UserMessage).queued).toBe(false);
    expect(c.phase).toBe('idle');
    // A message after the end opens a fresh turn after itself, not in the old one.
    c = applyRecord(c, user('third', 'u3'));
    expect(c.items[c.items.length - 1]).toMatchObject({ kind: 'assistant', end: null });
    expect((c.items[c.items.length - 2] as UserMessage).text).toBe('third');
  });
  it('outside a turn it only clears queued flags', () => {
    const c = applyRecord(emptyConversation(), { ts: 't', type: 'session', event: 'ended' });
    // Session-ended marks the live tracking scope as ended without changing
    // items or phase; the activity field keeps its current shape with
    // live=false.
    expect(c).toEqual({
      ...emptyConversation(),
      activity: { ...emptyConversation().activity, live: false },
    });
  });
});

describe('reducer: subagents', () => {
  it("folds the subagent's tool calls under the Agent row and never into the transcript", () => {
    const c = reduceRecords(replay('Use the Agent tool', fixture('subagent')));
    const t = lastTurn(c);
    const tools = t.segments.filter((s) => s.kind === 'tool') as ToolSegment[];
    expect(tools.map((x) => x.name)).toEqual(['Agent']);
    expect(tools[0].subtools).toHaveLength(1);
    expect(tools[0].subtools[0]).toMatchObject({
      name: 'Bash',
      state: 'done',
      result: 'sub-hello',
    });
    expect(tools[0].state).toBe('done');
    expect(t.end).toEqual({ state: 'done' });
    const prose = t.segments.filter((s) => s.kind === 'prose') as { text: string }[];
    expect(prose.map((p) => p.text).join('\n')).toContain('PARENT-DONE');
    expect(prose.map((p) => p.text).join('\n')).not.toContain(
      'Run the shell command echo sub-hello'
    );
  });
  it('a subagent record whose parent is unknown is dropped', () => {
    let c = applyRecord(emptyConversation(), user('x'));
    const before = JSON.stringify(c);
    c = applyRecord(
      c,
      harness({
        type: 'assistant',
        parent_tool_use_id: 'nope',
        message: { content: [{ type: 'tool_use', id: 't1', name: 'Bash', input: {} }] },
      })
    );
    expect(JSON.stringify(c)).toBe(before);
  });
});

describe('claudeResolvedRequestId', () => {
  it('returns the request id for a control_response record', () => {
    const r: ConversationRecord = {
      ts: 't',
      type: 'control',
      line: { type: 'control_response', response: { request_id: 'req-1', subtype: 'success' } },
    };
    expect(claudeResolvedRequestId(r)).toBe('req-1');
  });

  it('returns the request id for a control_cancel_request record', () => {
    const r: ConversationRecord = {
      ts: 't',
      type: 'harness',
      line: { type: 'control_cancel_request', request_id: 'req-2' },
    };
    expect(claudeResolvedRequestId(r)).toBe('req-2');
  });

  it('returns null for unrelated records', () => {
    const r: ConversationRecord = {
      ts: 't',
      type: 'user_message',
      id: 'u1',
      text: 'hi',
    };
    expect(claudeResolvedRequestId(r)).toBeNull();
  });
});

describe('reducer: session activity is preserved through every path', () => {
  it('replacing an open turn keeps the activity state', () => {
    const c0 = applyRecord(emptyConversation(), user('hi'));
    const op = {
      namespace: 'claude-task',
      id: 't-1',
      kind: 'claude-task' as const,
      title: 'Run check gate',
      ownerTurnId: null,
      parentId: null,
      lifecycle: 'running' as const,
      rawStatus: null,
      firstObservedAt: 't',
      startTime: null,
      endTime: null,
      lastUpdateAt: 't',
      durationMs: null,
      latestActivity: null,
      usage: null,
      toolId: null,
      assignmentId: 0,
      outputFile: null,
      terminalAt: null,
    };
    const seeded: Conversation = {
      ...c0,
      activity: {
        ...c0.activity,
        operations: { 'claude-task:t-1': op },
        order: ['claude-task:t-1'],
      },
    };
    // Run a no-op stream event through; it should still go through
    // replaceOpenTurn without dropping the activity.
    const r: HarnessLine = { type: 'stream_event', event: { type: 'ping' } };
    const c1 = applyRecord(seeded, harness(r));
    expect(c1.activity.operations['claude-task:t-1']).toEqual(op);
  });

  it('live capture: TaskCreate and TaskUpdate produce checklist entries', () => {
    const lines = fixture('activity-checklist');
    const recs: ConversationRecord[] = [user('use TaskCreate')];
    for (const line of lines) {
      if (line.type === 'system' && line.subtype === 'init') continue;
      recs.push(harness(line));
    }
    const c = reduceRecords(recs);
    // TaskCreate returned tool_use_result.task.id = "1" and "2".
    expect(c.activity.checklist['1']?.subject).toBe('step 1');
    expect(c.activity.checklist['2']?.subject).toBe('step 2');
    // TaskUpdate marked task 1 completed.
    expect(c.activity.checklist['1']?.status).toBe('completed');
    // Task 2 is still pending.
    expect(c.activity.checklist['2']?.status).toBe('pending');
  });

  it('live capture: background task survives turn result and reaches finished', () => {
    const lines = fixture('activity-background');
    const recs: ConversationRecord[] = [user('start a bg job')];
    for (const line of lines) {
      if (line.type === 'system' && line.subtype === 'init') continue;
      recs.push(harness(line));
    }
    const c = reduceRecords(recs);
    // After the parent's result, the background task should still be
    // tracked, and the task_notification arrived with status "completed".
    const keys = Object.keys(c.activity.operations).filter((k) => k.startsWith('claude-task:'));
    expect(keys.length).toBeGreaterThan(0);
    const bgKey = keys[0];
    const op = c.activity.operations[bgKey];
    expect(op.rawStatus).toBe('completed');
  });

  it('live capture: async Agent launch creates a claude-agent operation', () => {
    const lines = fixture('activity-agent');
    const recs: ConversationRecord[] = [user('launch subagent')];
    for (const line of lines) {
      if (line.type === 'system' && line.subtype === 'init') continue;
      recs.push(harness(line));
    }
    const c = reduceRecords(recs);
    // The agentId from tool_use_result was used as the operation id.
    const agentKeys = Object.keys(c.activity.operations).filter((k) =>
      k.startsWith('claude-agent:')
    );
    expect(agentKeys.length).toBeGreaterThan(0);
    const op = c.activity.operations[agentKeys[0]];
    // The task_notification arrived after the result and resolved the op to
    // its terminal state. The agent's launch tool was async, so the launch
    // itself is finished and the agent's final status is "completed".
    expect(op.rawStatus).toBe('completed');
    expect(op.lifecycle).toBe('finished');
    expect(op.terminalAt).not.toBeNull();
  });

  it('an unresolved foreground tool on a normal result becomes "Status unavailable", not "interrupted"', () => {
    // Build a turn with one tool still in 'running' state and close it with
    // a normal done result (not an interrupt).
    const recs: ConversationRecord[] = [
      user('hi'),
      harness({
        type: 'assistant',
        message: {
          content: [{ type: 'tool_use', id: 'tu-1', name: 'Bash', input: {} }],
        },
      } as HarnessLine),
      // No tool_result for tu-1: tool stays 'running'.
      harness({ type: 'result' } as HarnessLine),
    ];
    const c = reduceRecords(recs);
    const t = lastTurn(c);
    const tool = (t.segments as Array<{ kind: string; state: string; result: string }>).find(
      (s) => s.kind === 'tool'
    );
    expect(tool?.state).toBe('error');
    expect(tool?.result).toBe('Status unavailable');
  });

  it('session: ended preserves activity but marks it not-live', () => {
    let c = applyRecord(emptyConversation(), user('hi'));
    c = {
      ...c,
      activity: {
        ...c.activity,
        operations: {
          'claude-task:t-1': {
            namespace: 'claude-task',
            id: 't-1',
            kind: 'claude-task',
            title: 'test',
            ownerTurnId: null,
            parentId: null,
            lifecycle: 'running',
            rawStatus: null,
            firstObservedAt: 't',
            startTime: null,
            endTime: null,
            lastUpdateAt: 't',
            durationMs: null,
            latestActivity: null,
            usage: null,
            toolId: null,
            assignmentId: 0,
            outputFile: null,
            terminalAt: null,
          },
        },
        order: ['claude-task:t-1'],
      },
    };
    const ended = applyRecord(c, { ts: 't', type: 'session', event: 'ended' });
    expect(ended.activity.live).toBe(false);
    // The operation stays in the table so the view can render it as
    // "Session ended before completion was reported".
    expect(ended.activity.operations['claude-task:t-1']).toBeDefined();
  });
});

describe('reducer: structured tool_use_result must not skip transcript processing', () => {
  // Replaying the live checklist fixture must produce successful transcript
  // results for TaskCreate, TaskUpdate, and Agent launches, not error rows.
  function build(records: ConversationRecord[]): Conversation {
    return reduceRecords(records);
  }

  it('successful TaskCreate tool result is recorded as a successful transcript tool', () => {
    const lines = fixture('activity-checklist');
    const ts = '2026-01-01T00:00:00.000Z';
    const recs: ConversationRecord[] = [
      { ts, type: 'user_message', id: 'u-1', text: 'use TaskCreate' },
    ];
    for (const line of lines) {
      if (line.type === 'system' && line.subtype === 'init') continue;
      recs.push({ ts, type: 'harness', line });
    }
    const c = build(recs);
    const last = c.items[c.items.length - 1] as AssistantTurn;
    const tools = last.segments.filter((s) => s.kind === 'tool') as ToolSegment[];
    const taskCreate = tools.find(
      (t) => t.name === 'TaskCreate' && t.id === 'call_01a0852d019471718db28666'
    );
    expect(taskCreate).toBeDefined();
    expect(taskCreate?.state).toBe('done');
    expect(taskCreate?.result).toContain('Task #1 created successfully');
  });

  it('successful TaskUpdate tool result is recorded as a successful transcript tool', () => {
    const lines = fixture('activity-checklist');
    const ts = '2026-01-01T00:00:00.000Z';
    const recs: ConversationRecord[] = [
      { ts, type: 'user_message', id: 'u-1', text: 'use TaskUpdate' },
    ];
    for (const line of lines) {
      if (line.type === 'system' && line.subtype === 'init') continue;
      recs.push({ ts, type: 'harness', line });
    }
    const c = build(recs);
    const last = c.items[c.items.length - 1] as AssistantTurn;
    const tools = last.segments.filter((s) => s.kind === 'tool') as ToolSegment[];
    const update = tools.find((t) => t.name === 'TaskUpdate');
    expect(update).toBeDefined();
    expect(update?.state).toBe('done');
    expect(update?.result).toContain('Updated task #1 status');
  });

  it('successful async Agent launch result is recorded as a successful transcript tool', () => {
    const lines = fixture('activity-agent');
    const ts = '2026-01-01T00:00:00.000Z';
    const recs: ConversationRecord[] = [
      { ts, type: 'user_message', id: 'u-1', text: 'launch subagent' },
    ];
    for (const line of lines) {
      if (line.type === 'system' && line.subtype === 'init') continue;
      recs.push({ ts, type: 'harness', line });
    }
    const c = build(recs);
    const last = c.items[c.items.length - 1] as AssistantTurn;
    const tools = last.segments.filter((s) => s.kind === 'tool') as ToolSegment[];
    const agent = tools.find((t) => t.name === 'Agent');
    expect(agent).toBeDefined();
    expect(agent?.state).toBe('done');
    // The result content includes the agentId; the reducer should not
    // discard it as an error.
    expect(agent?.result).toContain('Async agent launched');
  });
});

describe('reducer: record ts is threaded into activity state (finding 2)', () => {
  it('claude-task terminalAt equals the record ts when the harness line omits it', () => {
    const ts = '2026-09-09T00:00:00.000Z';
    const recs: ConversationRecord[] = [
      { ts, type: 'user_message', id: 'u-1', text: 'go' },
      {
        ts,
        type: 'harness',
        line: { type: 'system', subtype: 'task_notification' } as HarnessLine,
      },
    ];
    // The harness line itself has no timestamp; only the record-level ts is
    // available. The reducer must thread that through, otherwise expiry
    // and age labels can never be computed from the captured wire events.
    const c = reduceRecords(recs);
    expect(Object.values(c.activity.operations)).toHaveLength(0); // no task_id so it's filtered
  });

  it('claude-task terminalAt is the record ts when the event has a real id but no line timestamp', () => {
    const ts = '2026-09-09T00:00:00.000Z';
    const recs: ConversationRecord[] = [
      { ts, type: 'user_message', id: 'u-1', text: 'go' },
      {
        ts,
        type: 'harness',
        line: {
          type: 'system',
          subtype: 'task_started',
          task_id: 't-1',
          tool_use_id: 'call-1',
          is_backgrounded: true,
        } as unknown as HarnessLine,
      },
      {
        ts: '2026-09-09T00:00:05.000Z',
        type: 'harness',
        line: {
          type: 'system',
          subtype: 'task_notification',
          task_id: 't-1',
          tool_use_id: 'call-1',
          status: 'completed',
        } as unknown as HarnessLine,
      },
    ];
    const c = reduceRecords(recs);
    const op = c.activity.operations['claude-task:t-1'];
    expect(op).toBeDefined();
    // The terminal time is the record ts of the notification, not the inner
    // line's (missing) timestamp.
    expect(op.terminalAt).toBe('2026-09-09T00:00:05.000Z');
    expect(op.lastUpdateAt).toBe('2026-09-09T00:00:05.000Z');
    // The terminal time parses cleanly: expiry/age can be computed.
    expect(Number.isFinite(Date.parse(op.terminalAt!))).toBe(true);
  });
});

describe('reducer: Restart must clear stale activity (finding 4)', () => {
  it('a background task from the previous session does not appear after Restart', () => {
    // Replay order: user message → background task starts → session ends →
    // new user message (in the new session, after Restart).
    const recs: ConversationRecord[] = [
      { ts: 't1', type: 'user_message', id: 'u-1', text: 'go' },
      {
        ts: 't1',
        type: 'harness',
        line: {
          type: 'system',
          subtype: 'task_started',
          task_id: 't-old',
          tool_use_id: 'call-1',
          is_backgrounded: true,
        } as unknown as HarnessLine,
      },
      { ts: 't2', type: 'session', event: 'ended' },
      { ts: 't3', type: 'user_message', id: 'u-2', text: 'restart' },
    ];
    const c = reduceRecords(recs);
    // After the new user message, the previous session's background task
    // must not be present. The activity state is fresh.
    expect(c.activity.operations['claude-task:t-old']).toBeUndefined();
    expect(c.activity.live).toBe(true);
  });

  it('session: ended alone does not destroy operations so they can render as ended', () => {
    // Just the session: ended record: the previous operation stays so the
    // view can show "Session ended before completion was reported".
    const recs: ConversationRecord[] = [
      { ts: 't1', type: 'user_message', id: 'u-1', text: 'go' },
      {
        ts: 't1',
        type: 'harness',
        line: {
          type: 'system',
          subtype: 'task_started',
          task_id: 't-old',
          tool_use_id: 'call-1',
          is_backgrounded: true,
        } as unknown as HarnessLine,
      },
      { ts: 't2', type: 'session', event: 'ended' },
    ];
    const c = reduceRecords(recs);
    expect(c.activity.operations['claude-task:t-old']).toBeDefined();
    expect(c.activity.live).toBe(false);
  });
});

describe('reducer: tool_progress heartbeats (finding 5)', () => {
  it('heartbeat with tool_use_id creates a running claude-tool row', () => {
    const recs: ConversationRecord[] = [
      { ts: 't1', type: 'user_message', id: 'u-1', text: 'go' },
      {
        ts: 't1',
        type: 'harness',
        line: {
          type: 'system',
          subtype: 'tool_progress',
          tool_use_id: 'call-bash-1',
          elapsed_time_ms: 30000,
        } as unknown as HarnessLine,
      },
    ];
    const c = reduceRecords(recs);
    const op = c.activity.operations['claude-tool:call-bash-1'];
    expect(op).toBeDefined();
    expect(op.lifecycle).toBe('running');
    expect(op.durationMs).toBe(30000);
  });

  it('heartbeat with parent_tool_use_id creates a running claude-tool row', () => {
    const recs: ConversationRecord[] = [
      { ts: 't1', type: 'user_message', id: 'u-1', text: 'go' },
      {
        ts: 't1',
        type: 'harness',
        line: {
          type: 'system',
          subtype: 'tool_progress',
          parent_tool_use_id: 'call-agent-1',
          elapsed_time_ms: 60000,
        } as unknown as HarnessLine,
      },
    ];
    const c = reduceRecords(recs);
    const op = c.activity.operations['claude-tool:call-agent-1'];
    expect(op).toBeDefined();
    expect(op.lifecycle).toBe('running');
    expect(op.durationMs).toBe(60000);
  });

  it('repeated heartbeat updates the same op, not a new one per heartbeat (finding 5)', () => {
    const recs: ConversationRecord[] = [
      { ts: 't1', type: 'user_message', id: 'u-1', text: 'go' },
      {
        ts: 't1',
        type: 'harness',
        line: {
          type: 'system',
          subtype: 'tool_progress',
          tool_use_id: 'call-bash-1',
          elapsed_time_ms: 30000,
        } as unknown as HarnessLine,
      },
      {
        ts: 't2',
        type: 'harness',
        line: {
          type: 'system',
          subtype: 'tool_progress',
          tool_use_id: 'call-bash-1',
          elapsed_time_ms: 60000,
        } as unknown as HarnessLine,
      },
    ];
    const c = reduceRecords(recs);
    const ops = Object.keys(c.activity.operations).filter((k) => k === 'claude-tool:call-bash-1');
    expect(ops).toHaveLength(1);
    expect(c.activity.operations['claude-tool:call-bash-1'].durationMs).toBe(60000);
  });
});

describe('reducer: child-input status for subagent questions', () => {
  it('subagent control_request marks the parent Agent op as pending-input', () => {
    // Sequence: user → agent launches → subagent asks a question
    // (control_request with parent_tool_use_id pointing at the
    // subagent's child tool_use_id).
    const recs: ConversationRecord[] = [
      { ts: 't1', type: 'user_message', id: 'u-1', text: 'launch' },
      {
        ts: 't1',
        type: 'harness',
        line: {
          type: 'assistant',
          message: {
            content: [
              {
                type: 'tool_use',
                id: 'agent-1',
                name: 'Agent',
                input: { description: 'sub', prompt: 'go' },
              },
            ],
          },
        } as HarnessLine,
      },
      // Subagent child tool (a question tool)
      {
        ts: 't1',
        type: 'harness',
        line: {
          type: 'assistant',
          parent_tool_use_id: 'agent-1',
          message: {
            content: [
              {
                type: 'tool_use',
                id: 'child-q-1',
                name: 'AskUserQuestion',
                input: { questions: [{ question: 'Pick?' }] },
              },
            ],
          },
        } as HarnessLine,
      },
      // control_request for the child's question
      {
        ts: 't1',
        type: 'harness',
        line: {
          type: 'control_request',
          request_id: 'req-1',
          request: {
            subtype: 'can_use_tool',
            tool_name: 'AskUserQuestion',
            tool_use_id: 'child-q-1',
            input: { questions: [{ question: 'Pick?' }] },
            requires_user_interaction: true,
          },
        } as unknown as HarnessLine,
      },
    ];
    const c = reduceRecords(recs);
    // The Agent op must be marked as pending-input.
    const agentKey = Object.keys(c.activity.operations).find((k) => k.startsWith('claude-agent:'));
    expect(agentKey).toBeDefined();
    expect(c.activity.operations[agentKey!].lifecycle).toBe('pending-input');
  });

  it('control_cancel_request clears pending-input on the owning Agent op', () => {
    const recs: ConversationRecord[] = [
      { ts: 't1', type: 'user_message', id: 'u-1', text: 'launch' },
      {
        ts: 't1',
        type: 'harness',
        line: {
          type: 'assistant',
          message: {
            content: [
              {
                type: 'tool_use',
                id: 'agent-1',
                name: 'Agent',
                input: { description: 'sub', prompt: 'go' },
              },
            ],
          },
        } as HarnessLine,
      },
      {
        ts: 't1',
        type: 'harness',
        line: {
          type: 'assistant',
          parent_tool_use_id: 'agent-1',
          message: {
            content: [
              {
                type: 'tool_use',
                id: 'child-q-1',
                name: 'AskUserQuestion',
                input: { questions: [{ question: 'Pick?' }] },
              },
            ],
          },
        } as HarnessLine,
      },
      {
        ts: 't1',
        type: 'harness',
        line: {
          type: 'control_request',
          request_id: 'req-1',
          request: {
            subtype: 'can_use_tool',
            tool_name: 'AskUserQuestion',
            tool_use_id: 'child-q-1',
            input: { questions: [{ question: 'Pick?' }] },
            requires_user_interaction: true,
          },
        } as unknown as HarnessLine,
      },
      {
        ts: 't2',
        type: 'harness',
        line: {
          type: 'control_cancel_request',
          request_id: 'req-1',
        } as unknown as HarnessLine,
      },
    ];
    const c = reduceRecords(recs);
    const agentKey = Object.keys(c.activity.operations).find((k) => k.startsWith('claude-agent:'));
    expect(agentKey).toBeDefined();
    expect(c.activity.operations[agentKey!].lifecycle).not.toBe('pending-input');
  });

  it('control_response (answered request) clears pending-input on the owning Agent op', () => {
    const recs: ConversationRecord[] = [
      { ts: 't1', type: 'user_message', id: 'u-1', text: 'launch' },
      {
        ts: 't1',
        type: 'harness',
        line: {
          type: 'assistant',
          message: {
            content: [
              {
                type: 'tool_use',
                id: 'agent-1',
                name: 'Agent',
                input: { description: 'sub', prompt: 'go' },
              },
            ],
          },
        } as HarnessLine,
      },
      {
        ts: 't1',
        type: 'harness',
        line: {
          type: 'assistant',
          parent_tool_use_id: 'agent-1',
          message: {
            content: [
              {
                type: 'tool_use',
                id: 'child-q-1',
                name: 'AskUserQuestion',
                input: { questions: [{ question: 'Pick?' }] },
              },
            ],
          },
        } as HarnessLine,
      },
      {
        ts: 't1',
        type: 'harness',
        line: {
          type: 'control_request',
          request_id: 'req-1',
          request: {
            subtype: 'can_use_tool',
            tool_name: 'AskUserQuestion',
            tool_use_id: 'child-q-1',
            input: { questions: [{ question: 'Pick?' }] },
            requires_user_interaction: true,
          },
        } as unknown as HarnessLine,
      },
      {
        ts: 't2',
        type: 'control',
        line: {
          type: 'control_response',
          response: { subtype: 'success', request_id: 'req-1', response: { behavior: 'allow' } },
        } as unknown as HarnessLine,
      },
    ];
    const c = reduceRecords(recs);
    const agentKey = Object.keys(c.activity.operations).find((k) => k.startsWith('claude-agent:'));
    expect(agentKey).toBeDefined();
    expect(c.activity.operations[agentKey!].lifecycle).not.toBe('pending-input');
  });
});

describe('reducer: claude api_retry clears on result (finding 7)', () => {
  it('api_retry op is removed when result is received', () => {
    const recs: ConversationRecord[] = [
      { ts: 't1', type: 'user_message', id: 'u-1', text: 'go' },
      {
        ts: 't1',
        type: 'harness',
        line: {
          type: 'system',
          subtype: 'api_retry',
          attempt: 2,
          maxAttempts: 5,
          retryDelayMs: 1000,
        } as unknown as HarnessLine,
      },
      { ts: 't2', type: 'harness', line: { type: 'result' } as HarnessLine },
    ];
    const c = reduceRecords(recs);
    expect(c.activity.operations['claude-retry:attempt-2']).toBeUndefined();
  });
});
