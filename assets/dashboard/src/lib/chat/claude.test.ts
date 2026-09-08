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
  it('a user_message is the only source of user items and opens a turn', () => {
    const c = applyRecord(emptyConversation(), user('hi'));
    expect(c.items[0]).toMatchObject({ kind: 'user', text: 'hi', queued: false });
    expect(c.items[1]).toMatchObject({ kind: 'assistant', end: null });
    expect(c.phase).toBe('running');
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
    expect(c).toEqual(emptyConversation());
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
