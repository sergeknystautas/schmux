import { describe, it, expect } from 'vitest';
import {
  reduce,
  initialState,
  TERMINAL_REASONS,
  type DocState,
  type Effect,
} from './markdownDocument';

let counter = 0;
const newId = () => `id-${++counter}`;

function run(state: DocState, ...events: Parameters<typeof reduce>[1][]): [DocState, Effect[]] {
  let s = state;
  const all: Effect[] = [];
  for (const e of events) {
    const [next, effects] = reduce(s, e, newId);
    s = next;
    all.push(...effects);
  }
  return [s, all];
}

const opened = (content: string): DocState =>
  run(initialState, { type: 'open' }, { type: 'document', content })[0];

describe('markdownDocument reducer', () => {
  it('adopts the first document as clean', () => {
    const [s, effects] = run(initialState, { type: 'open' }, { type: 'document', content: '# a' });
    expect(s).toMatchObject({
      base: '# a',
      draft: '# a',
      inFlight: null,
      status: 'saved',
      awaitingFirst: false,
    });
    expect(effects).toEqual([]);
  });

  it('an edit arms the timer only when dirty', () => {
    const s0 = opened('a');
    const [, e1] = run(s0, { type: 'edit', text: 'a' });
    expect(e1).toEqual([]);
    const [s2, e2] = run(s0, { type: 'edit', text: 'ab' });
    expect(s2.draft).toBe('ab');
    expect(e2).toEqual([{ type: 'armTimer' }]);
  });

  it('timer sends base and draft with a fresh id and records inFlight', () => {
    const [s, effects] = run(opened('a'), { type: 'edit', text: 'ab' }, { type: 'timer' });
    expect(effects[1]).toMatchObject({ type: 'send', base: 'a', draft: 'ab' });
    expect(s.inFlight).toMatchObject({ base: 'a', draft: 'ab' });
    expect(s.status).toBe('saving');
  });

  it('reply with no typing since send adopts the merged content', () => {
    const s0 = run(opened('a'), { type: 'edit', text: 'ab' }, { type: 'timer' })[0];
    const id = s0.inFlight!.id;
    const [s, effects] = run(s0, { type: 'document', content: 'ab+agent', reply: id });
    expect(s).toMatchObject({
      base: 'ab+agent',
      draft: 'ab+agent',
      inFlight: null,
      status: 'saved',
    });
    expect(effects).toEqual([]);
  });

  it('reply after more typing rebases onto the sent draft and saves again', () => {
    const s0 = run(
      opened('a'),
      { type: 'edit', text: 'ab' },
      { type: 'timer' },
      { type: 'edit', text: 'abc' }
    )[0];
    const id = s0.inFlight!.id;
    const [s, effects] = run(s0, { type: 'document', content: 'ab+agent', reply: id });
    expect(s.draft).toBe('abc');
    expect(s.base).toBe('ab');
    expect(effects).toEqual([{ type: 'send', id: s.inFlight!.id, base: 'ab', draft: 'abc' }]);
    expect(s.inFlight!.id).not.toBe(id);
  });

  it('a reply for an unknown id is treated as an external document', () => {
    const s0 = run(opened('a'), { type: 'edit', text: 'ab' }, { type: 'timer' })[0];
    const [s, effects] = run(s0, { type: 'document', content: 'zzz', reply: 'stale' });
    expect(s.inFlight).not.toBeNull();
    expect(s.draft).toBe('ab');
    expect(effects).toEqual([]);
  });

  it('external document while clean is adopted', () => {
    const [s, effects] = run(opened('a'), { type: 'document', content: 'agent' });
    expect(s).toMatchObject({ base: 'agent', draft: 'agent' });
    expect(effects).toEqual([]);
  });

  it('external document while dirty cancels the timer and saves now', () => {
    const [s, effects] = run(
      opened('a'),
      { type: 'edit', text: 'ab' },
      { type: 'document', content: 'agent' }
    );
    expect(s.base).toBe('a');
    expect(effects).toEqual([
      { type: 'armTimer' },
      { type: 'cancelTimer' },
      { type: 'send', id: s.inFlight!.id, base: 'a', draft: 'ab' },
    ]);
  });

  it('external document while a save is in flight is ignored', () => {
    const s0 = run(opened('a'), { type: 'edit', text: 'ab' }, { type: 'timer' })[0];
    const [s, effects] = run(s0, { type: 'document', content: 'agent' });
    expect(s).toEqual(s0);
    expect(effects).toEqual([]);
  });

  it('reconnect with a save in flight resends the same id', () => {
    const s0 = run(opened('a'), { type: 'edit', text: 'ab' }, { type: 'timer' })[0];
    const id = s0.inFlight!.id;
    const [s, effects] = run(
      s0,
      { type: 'close', reason: null },
      { type: 'open' },
      { type: 'document', content: 'whatever' }
    );
    expect(effects).toEqual([
      { type: 'cancelTimer' },
      { type: 'send', id, base: 'a', draft: 'ab' },
    ]);
    expect(s.status).toBe('saving');
  });

  it('reconnect while dirty without inFlight sends a fresh save', () => {
    const s0 = run(opened('a'), { type: 'edit', text: 'ab' })[0];
    const [, effects] = run(
      s0,
      { type: 'close', reason: null },
      { type: 'open' },
      { type: 'document', content: 'agent' }
    );
    expect(effects[effects.length - 1]).toMatchObject({ type: 'send', base: 'a', draft: 'ab' });
  });

  it('reconnect while clean adopts the new document', () => {
    const [s] = run(
      opened('a'),
      { type: 'close', reason: 'deleted' },
      { type: 'open' },
      { type: 'document', content: 'back' }
    );
    expect(s).toMatchObject({ base: 'back', draft: 'back', status: 'saved', reason: null });
  });

  it('close keeps the draft, records the reason, and cancels the timer', () => {
    const [s, effects] = run(
      opened('a'),
      { type: 'edit', text: 'ab' },
      { type: 'close', reason: 'write_failed' }
    );
    expect(s).toMatchObject({ draft: 'ab', base: 'a', status: 'error', reason: 'write_failed' });
    expect(effects[effects.length - 1]).toEqual({ type: 'cancelTimer' });
  });

  it('names the terminal reasons', () => {
    expect([...TERMINAL_REASONS].sort()).toEqual([
      'bad_request',
      'invalid_path',
      'not_utf8',
      'too_large',
    ]);
  });
});
