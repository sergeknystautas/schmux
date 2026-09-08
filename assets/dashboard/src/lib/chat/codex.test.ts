import { describe, it, expect } from 'vitest';
import { summarizeTool } from '../../components/chat/ToolCallRow';
import { applyRecord as applyAny, reduceRecords as reduceAny, emptyConversation } from './reducer';
import { codexResolvedRequestId } from './codex';
import type {
  AssistantTurn,
  Conversation,
  ConversationRecord,
  HarnessLine,
  PendingSegment,
  ToolSegment,
} from './types';
import streamOut from './__fixtures__/codex/stream.out.jsonl?raw';
import streamIn from './__fixtures__/codex/stream.in.jsonl?raw';
import approvalOut from './__fixtures__/codex/approval.out.jsonl?raw';
import approvalIn from './__fixtures__/codex/approval.in.jsonl?raw';
import userinputOut from './__fixtures__/codex/userinput.out.jsonl?raw';
import userinputIn from './__fixtures__/codex/userinput.in.jsonl?raw';
import interruptOut from './__fixtures__/codex/interrupt.out.jsonl?raw';
import interruptIn from './__fixtures__/codex/interrupt.in.jsonl?raw';
import steerOut from './__fixtures__/codex/steer.out.jsonl?raw';
import steerIn from './__fixtures__/codex/steer.in.jsonl?raw';
import imageOut from './__fixtures__/codex/image.out.jsonl?raw';
import imageIn from './__fixtures__/codex/image.in.jsonl?raw';
import loggedoutOut from './__fixtures__/codex/loggedout.out.jsonl?raw';
import loggedoutIn from './__fixtures__/codex/loggedout.in.jsonl?raw';
import actionsOut from './__fixtures__/codex/actions.out.jsonl?raw';
import editsOut from './__fixtures__/codex/edits.out.jsonl?raw';

const P = 'codex-app-server' as const;
const applyRecord = (c: Conversation, r: ConversationRecord) => applyAny(P, c, r);
const reduceRecords = (records: ConversationRecord[]) => reduceAny(P, records);

const lines = (raw: string): HarnessLine[] =>
  raw
    .split('\n')
    .filter(Boolean)
    .map((l) => JSON.parse(l) as unknown as HarnessLine);

const user = (text: string, id: string): ConversationRecord => ({
  ts: 't',
  type: 'user_message',
  id,
  text,
});
const harness = (line: HarnessLine): ConversationRecord => ({ ts: 't', type: 'harness', line });
const control = (line: HarnessLine): ConversationRecord => ({ ts: 't', type: 'control', line });

function replay(inRaw: string, outRaw: string): ConversationRecord[] {
  const ins = lines(inRaw);
  const outs = lines(outRaw);
  const turnStarts = new Map<number, HarnessLine>();
  const interrupts = new Map<number, HarnessLine>();
  const answers = new Map<number, HarnessLine>();
  for (const l of ins) {
    const id = l.id as number | undefined;
    if (l.method === 'turn/start' && id !== undefined) turnStarts.set(id, l);
    else if (l.method === 'turn/steer' && id !== undefined) turnStarts.set(id, l);
    else if (l.method === 'turn/interrupt' && id !== undefined) interrupts.set(id, l);
    else if (l.method === undefined && id !== undefined && 'result' in l) answers.set(id, l);
  }
  const out: ConversationRecord[] = [];
  let n = 0;
  for (const l of outs) {
    const id = l.id as number | undefined;
    if (l.method === undefined && id !== undefined) {
      const ts = turnStarts.get(id);
      if (ts) {
        const input = (ts.params as { input: { type: string; text?: string }[] }).input;
        out.push(user(input.find((i) => i.type === 'text')?.text ?? '', `u-${++n}`));
      }
      const int = interrupts.get(id);
      if (int) out.push(control(int));
    }
    out.push(harness(l));
    if (l.method !== undefined && id !== undefined && answers.has(id))
      out.push(control(answers.get(id)!));
  }
  return out;
}

const lastTurn = (c: Conversation): AssistantTurn =>
  [...c.items].reverse().find((i) => i.kind === 'assistant') as AssistantTurn;
const tools = (t: AssistantTurn): ToolSegment[] =>
  t.segments.filter((s) => s.kind === 'tool') as ToolSegment[];

describe('codex reducer: turns and prose', () => {
  it('streams agentMessage deltas into one prose segment and finalizes from item/completed', () => {
    const recs = replay(streamIn, streamOut);
    let c = emptyConversation();
    let sawStreaming = false;
    for (const r of recs) {
      c = applyRecord(c, r);
      const t = lastTurn(c);
      if (t?.segments.some((s) => s.kind === 'prose' && s.streaming && s.text.length > 0))
        sawStreaming = true;
    }
    expect(sawStreaming).toBe(true);
    const t = lastTurn(c);
    const prose = t.segments.filter((s) => s.kind === 'prose');
    expect(prose).toHaveLength(1);
    expect(prose[0]).toMatchObject({ streaming: false });
    expect((prose[0] as { text: string }).text.length).toBeGreaterThan(200);
    expect(t.end).toEqual({ state: 'done' });
    expect(c.phase).toBe('idle');
    expect(c.items.filter((i) => i.kind === 'user')).toHaveLength(1);
  });
  it('empty reasoning renders nothing', () => {
    const c = reduceRecords(replay(imageIn, imageOut));
    expect(lastTurn(c).segments.some((s) => s.kind === 'thinking')).toBe(false);
    expect(lastTurn(c).thinking).toBe(false);
  });
  it('turn/started, hooks, status, token usage, and user item echoes add nothing', () => {
    const c = reduceRecords(replay(imageIn, imageOut));
    expect(lastTurn(c).segments.map((s) => s.kind)).toEqual(['prose']);
  });
});

describe('codex reducer: tools and approvals', () => {
  it('commandExecution becomes a tool row with command, output, and exit state', () => {
    const c = reduceRecords(replay(approvalIn, approvalOut));
    const ts = tools(lastTurn(c));
    expect(ts).toHaveLength(2);
    expect(ts[0]).toMatchObject({ name: 'Bash', state: 'done', result: '42\n' });
    expect((ts[0].input as { command: string }).command).toContain('print(41+1)');
    expect(ts[1]).toMatchObject({ name: 'Bash', state: 'error' });
  });
  it('requestApproval inserts a pending card after its tool row; our answer removes it', () => {
    const recs = replay(approvalIn, approvalOut);
    let c = emptyConversation();
    let sawPending = 0;
    for (const r of recs) {
      c = applyRecord(c, r);
      const t = lastTurn(c);
      if (!t) continue;
      const pi = t.segments.findIndex((s) => s.kind === 'pending');
      if (pi >= 0) {
        sawPending++;
        expect(t.segments[pi - 1]?.kind).toBe('tool');
        expect((t.segments[pi] as PendingSegment).toolName).toBe('Bash');
        expect((t.segments[pi] as PendingSegment).requestId).toMatch(/^\d+$/);
      }
    }
    expect(sawPending).toBeGreaterThan(0);
    expect(lastTurn(c).segments.some((s) => s.kind === 'pending')).toBe(false);
  });
  it('serverRequest/resolved alone removes a pending card', () => {
    let c = applyRecord(emptyConversation(), user('go', 'u1'));
    c = applyRecord(
      c,
      harness({
        method: 'item/started',
        params: { item: { type: 'commandExecution', id: 'e1', command: 'ls', cwd: '/w' } },
      } as unknown as HarnessLine)
    );
    c = applyRecord(
      c,
      harness({
        method: 'item/commandExecution/requestApproval',
        id: 0,
        params: { itemId: 'e1', command: 'ls', cwd: '/w' },
      } as unknown as HarnessLine)
    );
    expect(lastTurn(c).segments.some((s) => s.kind === 'pending')).toBe(true);
    c = applyRecord(
      c,
      harness({
        method: 'serverRequest/resolved',
        params: { requestId: 0 },
      } as unknown as HarnessLine)
    );
    expect(lastTurn(c).segments.some((s) => s.kind === 'pending')).toBe(false);
  });
  it('fileChange and mcpToolCall become tool rows (schema-specified, unverified live)', () => {
    let c = applyRecord(emptyConversation(), user('edit', 'u1'));
    c = applyRecord(
      c,
      harness({
        method: 'item/started',
        params: {
          item: {
            type: 'fileChange',
            id: 'f1',
            status: 'inProgress',
            changes: [{ path: 'a.go', kind: 'update' }],
          },
        },
      } as unknown as HarnessLine)
    );
    c = applyRecord(
      c,
      harness({
        method: 'item/fileChange/requestApproval',
        id: 1,
        params: { itemId: 'f1', reason: 'outside root' },
      } as unknown as HarnessLine)
    );
    let p = lastTurn(c).segments.find((s) => s.kind === 'pending') as PendingSegment;
    expect(p.toolName).toBe('Edit');
    expect((p.input as { file_path: string }).file_path).toBe('a.go');
    c = applyRecord(
      c,
      control({ id: 1, result: { decision: 'accept' } } as unknown as HarnessLine)
    );
    c = applyRecord(
      c,
      harness({
        method: 'item/completed',
        params: {
          item: {
            type: 'fileChange',
            id: 'f1',
            status: 'completed',
            changes: [{ path: 'a.go', kind: 'update' }],
          },
        },
      } as unknown as HarnessLine)
    );
    expect(tools(lastTurn(c))[0]).toMatchObject({ name: 'Edit', state: 'done' });
    c = applyRecord(
      c,
      harness({
        method: 'item/started',
        params: {
          item: {
            type: 'mcpToolCall',
            id: 'm1',
            server: 'srv',
            tool: 'search',
            arguments: { q: 'x' },
            status: 'inProgress',
          },
        },
      } as unknown as HarnessLine)
    );
    c = applyRecord(
      c,
      harness({
        method: 'item/completed',
        params: {
          item: {
            type: 'mcpToolCall',
            id: 'm1',
            server: 'srv',
            tool: 'search',
            arguments: { q: 'x' },
            status: 'failed',
            result: { content: [{ type: 'text', text: 'boom' }] },
          },
        },
      } as unknown as HarnessLine)
    );
    expect(tools(lastTurn(c))[1]).toMatchObject({
      name: 'srv/search',
      state: 'error',
      result: 'boom',
    });
    p = lastTurn(c).segments.find((s) => s.kind === 'pending') as PendingSegment;
    expect(p).toBeUndefined();
  });
});

describe('codex reducer: questions', () => {
  it('requestUserInput becomes a question card keyed by harness id', () => {
    const recs = replay(userinputIn, userinputOut);
    let c = emptyConversation();
    let q: PendingSegment | undefined;
    for (const r of recs) {
      c = applyRecord(c, r);
      const found = lastTurn(c)?.segments.find((s) => s.kind === 'pending') as
        PendingSegment | undefined;
      if (found?.questions) q = found;
    }
    expect(q?.questions?.[0]).toMatchObject({
      id: 'fruit',
      header: 'Fruit',
      question: 'Which fruit?',
      multiSelect: false,
    });
    expect(q?.questions?.[0].options.map((o) => o.label)).toEqual(['Apple', 'Banana']);
    expect(lastTurn(c).segments.some((s) => s.kind === 'pending')).toBe(false);
    expect(lastTurn(c).end).toEqual({ state: 'done' });
  });
});

describe('codex reducer: parity rows', () => {
  const rowsFrom = (out: string) => {
    let c = applyRecord(emptyConversation(), user('go', 'u1'));
    for (const line of lines(out)) c = applyRecord(c, harness(line));
    return tools(lastTurn(c));
  };

  it('names command rows from commandActions', () => {
    const rows = rowsFrom(actionsOut);
    expect(rows).toHaveLength(9);
    expect(rows.map((row) => row.name)).not.toContain('command');
    expect(new Set(rows.map((row) => row.name))).toEqual(new Set(['Bash', 'Explore']));
    for (const row of rows.filter((item) => item.name === 'Bash')) {
      expect((row.input as { command: string }).command.startsWith('/bin/zsh')).toBe(false);
    }
  });

  it('renders single command actions with Claude-shaped names and summaries', () => {
    let c = applyRecord(emptyConversation(), user('go', 'u1'));
    const started = (actions: unknown[], id: string) =>
      harness({
        method: 'item/started',
        params: {
          item: {
            type: 'commandExecution',
            id,
            command: '/bin/zsh -lc "x"',
            cwd: '/w',
            status: 'inProgress',
            commandActions: actions,
          },
        },
      } as unknown as HarnessLine);
    c = applyRecord(
      c,
      started([{ type: 'read', command: 'cat a.go', name: 'a.go', path: '/w/a.go' }], 'e1')
    );
    c = applyRecord(
      c,
      started([{ type: 'search', command: 'rg foo', query: 'foo', path: 'internal' }], 'e2')
    );
    c = applyRecord(c, started([{ type: 'listFiles', command: 'ls', path: null }], 'e3'));
    const rows = tools(lastTurn(c));
    expect(rows[0]).toMatchObject({ name: 'Read', input: { file_path: '/w/a.go' } });
    expect(rows[1]).toMatchObject({ name: 'Search', input: { pattern: 'foo', path: 'internal' } });
    expect(rows[2]).toMatchObject({ name: 'List', input: { file_path: '.' } });
    expect(summarizeTool(rows[1])).toBe('foo');
  });

  it('renders reasoning summaries and file changes', () => {
    let c = applyRecord(emptyConversation(), user('go', 'u1'));
    let sawThinking = false;
    for (const line of lines(editsOut)) {
      c = applyRecord(c, harness(line));
      if (lastTurn(c).thinking && lastTurn(c).segments.some((s) => s.kind === 'thinking' && s.text))
        sawThinking = true;
    }
    expect(sawThinking).toBe(true);
    expect(
      (lastTurn(c).segments.find((s) => s.kind === 'thinking') as { text: string }).text
    ).toContain('**Clarifying absence of planning tool**\n\n**Preparing to search README file**');
    const rows = tools(lastTurn(c)).filter((row) => row.name === 'Write' || row.name === 'Edit');
    expect(rows.map((row) => row.name)).toEqual(['Write', 'Edit']);
    expect(rows[0].result).toContain('hello');
    expect(rows[1].result).toContain('+hello world');
    expect(lastTurn(c).thinking).toBe(false);
  });

  it('shows unknown items and requests, and removes an aborted request', () => {
    let c = applyRecord(emptyConversation(), user('go', 'u1'));
    c = applyRecord(
      c,
      harness({
        method: 'item/started',
        params: {
          item: { type: 'webSearch', id: 'w1', status: 'inProgress', query: 'codex hooks' },
        },
      } as unknown as HarnessLine)
    );
    expect(tools(lastTurn(c))[0]).toMatchObject({
      name: 'webSearch',
      input: { query: 'codex hooks' },
    });
    c = applyRecord(
      c,
      harness({
        method: 'item/completed',
        params: {
          item: {
            type: 'webSearch',
            id: 'w1',
            status: 'completed',
            query: 'codex hooks',
            output: 'three results',
          },
        },
      } as unknown as HarnessLine)
    );
    expect(tools(lastTurn(c))[0]).toMatchObject({ result: 'three results', state: 'done' });
    c = applyRecord(
      c,
      harness({
        method: 'item/permissions/requestApproval',
        id: 4,
        params: { itemId: 'p1', permissions: { network: true } },
      } as unknown as HarnessLine)
    );
    const pending = lastTurn(c).segments.find(
      (segment) => segment.kind === 'pending'
    ) as PendingSegment;
    expect(pending).toMatchObject({
      requestId: '4',
      toolName: 'item/permissions/requestApproval',
      abortOnly: true,
    });
    c = applyRecord(
      c,
      control({
        id: 4,
        error: { code: -32601, message: 'schmux: unsupported server request' },
      } as unknown as HarnessLine)
    );
    expect(lastTurn(c).segments.some((segment) => segment.kind === 'pending')).toBe(false);
  });
});

describe('codex reducer: interrupt, steer, errors', () => {
  it('our turn/interrupt control marks the turn; turn/completed interrupted closes it as stopped', () => {
    const c = reduceRecords(replay(interruptIn, interruptOut));
    const turns = c.items.filter((i) => i.kind === 'assistant') as AssistantTurn[];
    expect(turns).toHaveLength(2);
    expect(turns[0].end).toEqual({ state: 'stopped' });
    expect(turns[0].interrupted).toBe(true);
    expect(turns[1].end).toEqual({ state: 'done' });
  });
  it('a message sent mid-turn is a user segment inside the open turn, not a queued item', () => {
    const c = reduceRecords(replay(steerIn, steerOut));
    const turns = c.items.filter((i) => i.kind === 'assistant') as AssistantTurn[];
    expect(turns).toHaveLength(1);
    expect(c.items.filter((i) => i.kind === 'user')).toHaveLength(1);
    const segs = turns[0].segments;
    const ui = segs.findIndex((s) => s.kind === 'user');
    expect(ui).toBeGreaterThan(0);
    expect((segs[ui] as { text: string }).text).toContain('STEERED-OK');
    expect(segs.slice(ui + 1).some((s) => s.kind === 'prose')).toBe(true);
    expect(turns[0].end).toEqual({ state: 'done' });
  });
  it('logged out closes the turn with an error naming codex login', () => {
    const c = reduceRecords([user('hi', 'u1'), ...lines(loggedoutOut).map(harness)]);
    expect(lastTurn(c).end).toMatchObject({ state: 'error' });
    expect((lastTurn(c).end as { text: string }).text).toContain('codex login');
    void loggedoutIn;
  });
  it('logged out before any message still shows the error, as a closed turn of its own', () => {
    const accountLine = lines(loggedoutOut).find((l) => l.id === 2 && l.method === undefined);
    expect(accountLine).toBeDefined();
    let c = applyRecord(emptyConversation(), harness(accountLine!));
    expect(c.items).toHaveLength(1);
    expect(lastTurn(c).end).toMatchObject({ state: 'error' });
    expect((lastTurn(c).end as { text: string }).text).toContain('codex login');
    expect(c.phase).toBe('idle');
    // A message sent afterwards opens a normal turn below the error.
    c = applyRecord(c, user('hi', 'u1'));
    expect(c.items.map((i) => i.kind)).toEqual(['assistant', 'user', 'assistant']);
    expect(lastTurn(c).end).toBeNull();
  });
  it('turn/completed failed and non-retrying error notifications end the turn with error', () => {
    let c = applyRecord(emptyConversation(), user('x', 'u1'));
    c = applyRecord(
      c,
      harness({
        method: 'turn/completed',
        params: { turn: { id: 't', status: 'failed', error: { message: 'quota' } } },
      } as unknown as HarnessLine)
    );
    expect(lastTurn(c).end).toEqual({ state: 'error', text: 'quota' });
    c = applyRecord(c, user('y', 'u2'));
    c = applyRecord(
      c,
      harness({
        method: 'error',
        params: { error: { message: 'Reconnecting... 2/5' }, willRetry: true },
      } as unknown as HarnessLine)
    );
    expect(lastTurn(c).end).toBeNull();
    c = applyRecord(
      c,
      harness({
        method: 'error',
        params: { error: { message: 'gone' }, willRetry: false },
      } as unknown as HarnessLine)
    );
    expect(lastTurn(c).end).toEqual({ state: 'error', text: 'gone' });
  });
  it('a delta for an unknown item (reconnect mid-item) opens a streaming prose segment', () => {
    let c = applyRecord(emptyConversation(), user('x', 'u1'));
    c = applyRecord(
      c,
      harness({
        method: 'item/agentMessage/delta',
        params: { itemId: 'm9', delta: 'tail' },
      } as unknown as HarnessLine)
    );
    expect(lastTurn(c).segments[0]).toMatchObject({ kind: 'prose', text: 'tail', streaming: true });
  });
});

describe('codexResolvedRequestId', () => {
  it('returns the request id for a JSON-RPC response record', () => {
    const r: ConversationRecord = {
      ts: 't',
      type: 'control',
      line: { type: 'x', id: 42, result: {} },
    };
    expect(codexResolvedRequestId(r)).toBe('42');
  });

  it('returns the request id for a serverRequest/resolved record', () => {
    const r: ConversationRecord = {
      ts: 't',
      type: 'harness',
      line: { type: 'x', method: 'serverRequest/resolved', params: { requestId: 7 } },
    };
    expect(codexResolvedRequestId(r)).toBe('7');
  });

  it('returns null for unrelated records', () => {
    const r: ConversationRecord = {
      ts: 't',
      type: 'user_message',
      id: 'u1',
      text: 'hi',
    };
    expect(codexResolvedRequestId(r)).toBeNull();
  });
});
