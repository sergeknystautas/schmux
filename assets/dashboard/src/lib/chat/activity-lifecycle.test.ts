import { describe, expect, it } from 'vitest';
import { applyRecord, emptyConversation, reduceRecords } from './reducer';
import { selectActivity } from './activity-selector';
import type { Conversation, ConversationRecord, HarnessLine } from './types';

const start = Date.parse('2026-01-01T00:00:00Z');
const ts = (seconds: number) => new Date(start + seconds * 1000).toISOString();
const user = (seconds = 0): ConversationRecord => ({
  type: 'user_message',
  ts: ts(seconds),
  id: `u-${seconds}`,
  text: 'go',
});
const harness = (line: HarnessLine, seconds = 1): ConversationRecord => ({
  type: 'harness',
  ts: ts(seconds),
  line,
});
const view = (c: Conversation, seconds = 2) =>
  selectActivity(c, { kind: 'connected' }, { now: start + seconds * 1000 });

describe('activity lifecycle boundaries', () => {
  it.each([0, 1])(
    'shows a long Codex command and removes its terminal outcome (exit %s)',
    (exitCode) => {
      const item = { type: 'commandExecution', id: 'exec', command: 'just test-editor' };
      let c = reduceRecords('codex-app-server', [
        user(),
        harness({ method: 'item/started', params: { item } } as unknown as HarnessLine),
      ]);
      expect(view(c, 10).rows).toEqual([]);
      expect(view(c, 11).rows).toMatchObject([
        { title: 'just test-editor', toolId: 'exec', status: 'running' },
      ]);
      c = applyRecord(
        'codex-app-server',
        c,
        harness(
          {
            method: 'item/completed',
            params: {
              item: {
                ...item,
                status: exitCode ? 'failed' : 'completed',
                exitCode,
                aggregatedOutput: 'Test report',
              },
            },
          } as unknown as HarnessLine,
          20
        )
      );
      expect(view(c, 20).rows).toEqual([]);
      expect(c.activity.operations['codex-tool:exec'].lifecycle).toBe(
        exitCode ? 'failed' : 'finished'
      );
    }
  );

  it('links an early heartbeat when its durable launch arrives without duplicating it', () => {
    let c = reduceRecords('claude-stream-json', [
      user(),
      harness({
        type: 'tool_progress',
        tool_use_id: 'early',
        tool_name: 'Bash',
        elapsed_time_seconds: 1,
      }),
    ]);
    expect(view(c).rows).toEqual([]);
    c = applyRecord(
      'claude-stream-json',
      c,
      harness({ type: 'tool_progress', tool_use_id: 'early', elapsed_time_seconds: 2 }, 2)
    );
    c = applyRecord(
      'claude-stream-json',
      c,
      harness(
        {
          type: 'assistant',
          message: {
            content: [
              { type: 'tool_use', id: 'early', name: 'Bash', input: { command: 'sleep 60' } },
            ],
          },
        },
        3
      )
    );
    expect(view(c, 13).rows).toMatchObject([{ toolId: 'early', durationMs: 2000 }]);
    expect(view(c, 13).rows).toHaveLength(1);
  });

  it('promotes a long command after 10 seconds and removes it on completion', () => {
    let c = reduceRecords('claude-stream-json', [
      user(),
      harness({
        type: 'assistant',
        message: {
          content: [{ type: 'tool_use', id: 'bash', name: 'Bash', input: { command: 'sleep 60' } }],
        },
      }),
    ]);
    expect(view(c).rows).toEqual([]);
    expect(view(c, 10).rows).toEqual([]);
    expect(view(c, 11).rows).toMatchObject([
      { title: 'sleep 60', status: 'running', toolId: 'bash' },
    ]);
    c = applyRecord(
      'claude-stream-json',
      c,
      harness(
        { type: 'tool_progress', tool_use_id: 'bash', tool_name: 'Bash', elapsed_time_seconds: 60 },
        61
      )
    );
    expect(view(c, 61).rows).toMatchObject([{ durationMs: 60000 }]);
    c = applyRecord(
      'claude-stream-json',
      c,
      harness(
        {
          type: 'user',
          message: { content: [{ type: 'tool_result', tool_use_id: 'bash', content: 'done' }] },
        },
        62
      )
    );
    expect(view(c, 62).rows).toEqual([]);
    expect(c.activity.operations['claude-tool:bash'].lifecycle).toBe('finished');
    expect(view(c, 66).rows).toEqual([]);
  });

  it('leaves terminal request errors in the transcript', () => {
    const c = reduceRecords('claude-stream-json', [
      user(),
      harness({ type: 'result', is_error: true, result: 'Request failed' }),
    ]);
    expect(view(c).hidden).toBe(true);
    expect(c.items[c.items.length - 1]).toMatchObject({
      end: { state: 'error', text: 'Request failed' },
    });
    expect(view(applyRecord('claude-stream-json', c, user(3))).headline).not.toBe('Request failed');
  });

  it('clears retry on observed recovery and uses explicit waiting/thinking signals', () => {
    let c = reduceRecords('claude-stream-json', [
      user(),
      harness({ type: 'system', subtype: 'status', status: 'requesting' }),
    ]);
    expect(view(c).headline).toBe('Waiting for response…');
    c = applyRecord(
      'claude-stream-json',
      c,
      harness({ type: 'system', subtype: 'thinking_tokens', thinking_tokens: 12 })
    );
    expect(view(c).headline).toBe('Thinking…');
    c = applyRecord(
      'claude-stream-json',
      c,
      harness({ type: 'system', subtype: 'api_retry', attempt: 1 })
    );
    expect(view(c).headline).toBe('Retrying request');
    c = applyRecord(
      'claude-stream-json',
      c,
      harness({ type: 'assistant', message: { content: [{ type: 'text', text: 'Recovered' }] } })
    );
    expect(view(c).headline).not.toBe('Retrying request');
  });

  it('does not report old workers as running after process end or in replacement history', () => {
    let c = reduceRecords('claude-stream-json', [
      user(),
      harness({ type: 'system', subtype: 'task_started', task_id: 'old', is_backgrounded: true }),
    ]);
    c = applyRecord('claude-stream-json', c, { type: 'session', ts: ts(2), event: 'ended' });
    const ended = selectActivity(c, { kind: 'gone' }, { now: start + 2000 });
    expect(ended.rows).toEqual([]);
    expect(ended.headline).toBe('Session ended');
    expect(view(c).hidden).toBe(true);
    c = applyRecord('claude-stream-json', c, user(3));
    expect(view(c).rows).toEqual([]);
    expect(c.activity.live).toBe(true);
  });

  it('acknowledges an old failure on the next accepted message without deleting its transcript outcome', () => {
    let c = reduceRecords('claude-stream-json', [
      user(),
      harness({
        type: 'system',
        subtype: 'task_notification',
        task_id: 'failed',
        status: 'failed',
        summary: 'exit 1',
      }),
    ]);
    expect(view(c).rows).toEqual([]);
    c = applyRecord('claude-stream-json', c, user(3));
    expect(view(c, 13).rows).toEqual([]);
    expect(c.activity.operations['claude-task:failed'].latestActivity).toBe('exit 1');
  });

  it('shows slow running hooks and removes them on failure', () => {
    const c = reduceRecords('claude-stream-json', [
      user(),
      harness({
        type: 'system',
        subtype: 'hook_started',
        hook_id: 'hook',
        hook_event: 'PreToolUse',
      }),
    ]);
    expect(view(c, 1.5).rows).toEqual([]);
    expect(view(c, 2).rows).toHaveLength(1);
    const failed = applyRecord(
      'claude-stream-json',
      c,
      harness(
        {
          type: 'system',
          subtype: 'hook_response',
          hook_id: 'hook',
          outcome: 'failed',
          exit_code: 1,
        },
        1.7
      )
    );
    expect(view(failed, 1.8).rows).toEqual([]);
  });

  it('keeps an aliased child needing input until all its questions are resolved', () => {
    const child = (id: string) =>
      harness({
        type: 'assistant',
        parent_tool_use_id: 'launch',
        message: { content: [{ type: 'tool_use', id, name: 'AskUserQuestion', input: {} }] },
      });
    const question = (id: string) =>
      harness({
        type: 'control_request',
        request_id: id,
        request: {
          subtype: 'can_use_tool',
          tool_name: 'AskUserQuestion',
          tool_use_id: id,
          requires_user_interaction: true,
          input: { questions: [{ question: 'Pick?', options: [] }] },
        },
      });
    let c = reduceRecords('claude-stream-json', [
      user(),
      harness({
        type: 'assistant',
        message: {
          content: [
            { type: 'tool_use', id: 'launch', name: 'Agent', input: { description: 'Review' } },
          ],
        },
      }),
      harness({
        type: 'system',
        subtype: 'task_started',
        task_id: 'agent',
        tool_use_id: 'launch',
        task_type: 'local_agent',
        is_backgrounded: true,
      }),
      child('q1'),
      child('q2'),
      question('q1'),
      question('q2'),
    ]);
    expect(view(c).headline).toBe('Needs your answer (2)');
    const answer = (id: string): ConversationRecord => ({
      type: 'control',
      ts: ts(3),
      line: {
        type: 'control_response',
        response: { subtype: 'success', request_id: id, response: { behavior: 'allow' } },
      },
    });
    c = applyRecord('claude-stream-json', c, answer('q1'));
    expect(view(c).rows).toMatchObject([{ status: 'pending-input' }]);
    c = applyRecord('claude-stream-json', c, answer('q2'));
    expect(view(c).rows).toMatchObject([{ status: 'running-background' }]);
  });
});
