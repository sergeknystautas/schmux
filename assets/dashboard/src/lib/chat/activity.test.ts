// operationForTool resolves the operation linked to a tool id. It is called
// per tool segment per record during history load and per segment per render
// in the transcript memo comparator, so it must stay O(1): the state carries
// a toolId → key index the upserters maintain. These tests pin the
// invariants that index has to uphold.
import { describe, expect, it } from 'vitest';
import {
  clearRetries,
  emptyActivity,
  operationForTool,
  updateOperation,
  upsertOperation,
  type Operation,
} from './activity';
import { reduceRecords } from './reducer';
import type { ConversationRecord, HarnessLine } from './types';

const op = (over: Partial<Operation> & Pick<Operation, 'namespace' | 'id'>): Operation => ({
  kind: 'claude-tool',
  title: 'op',
  ownerTurnId: null,
  parentId: null,
  lifecycle: 'running',
  rawStatus: null,
  firstObservedAt: 't0',
  startTime: null,
  endTime: null,
  lastUpdateAt: 't0',
  durationMs: null,
  latestActivity: null,
  usage: null,
  toolId: null,
  assignmentId: 0,
  outputFile: null,
  terminalAt: null,
  ...over,
});

const harness = (line: HarnessLine): ConversationRecord => ({ ts: 't1', type: 'harness', line });

describe('operationForTool index', () => {
  it('resolves an upserted operation by tool id and misses unknown ids', () => {
    const activity = upsertOperation(
      emptyActivity(),
      op({ namespace: 'claude-tool', id: 'toolu_1', toolId: 'toolu_1' })
    );
    expect(operationForTool(activity, 'toolu_1')?.id).toBe('toolu_1');
    expect(operationForTool(activity, 'other')).toBeUndefined();
    expect(operationForTool(undefined, 'toolu_1')).toBeUndefined();
  });

  it('follows a tool id change through updateOperation', () => {
    let activity = upsertOperation(
      emptyActivity(),
      op({ namespace: 'claude-tool', id: 'a', toolId: 'old' })
    );
    activity = updateOperation(activity, 'claude-tool', 'a', { toolId: 'new' });
    expect(operationForTool(activity, 'old')).toBeUndefined();
    expect(operationForTool(activity, 'new')?.id).toBe('a');
  });

  it('keeps non-retry operations resolvable after retries are cleared', () => {
    let activity = upsertOperation(
      emptyActivity(),
      op({ namespace: 'claude-tool', id: 'a', toolId: 't-a' })
    );
    activity = upsertOperation(
      activity,
      op({ namespace: 'claude-retry', id: 'attempt-1', kind: 'claude-retry' })
    );
    activity = clearRetries(activity);
    expect(operationForTool(activity, 't-a')?.id).toBe('a');
    expect(activity.operations['claude-retry:attempt-1']).toBeUndefined();
  });

  it('keeps the tool link when a launch tool merges into its task operation', () => {
    // The claude-tool op created by the tool_use alias-merges into the
    // claude-agent op from task_started; the tool id must resolve to the
    // merged operation, not a deleted alias.
    const c = reduceRecords('claude-stream-json', [
      { ts: 't0', type: 'user_message', id: 'u1', text: 'go' },
      harness({
        type: 'stream_event',
        event: {
          type: 'content_block_start',
          index: 0,
          content_block: { type: 'tool_use', id: 'toolu_9', name: 'Agent', input: {} },
        },
      }),
      harness({
        type: 'system',
        subtype: 'task_started',
        task_id: 'ag1',
        task_type: 'local_agent',
        description: 'Worker',
        tool_use_id: 'toolu_9',
      }),
    ]);
    const resolved = operationForTool(c.activity, 'toolu_9');
    expect(resolved?.namespace).toBe('claude-agent');
    expect(resolved?.id).toBe('ag1');
    expect(c.activity.operations['claude-tool:toolu_9']).toBeUndefined();
  });
});
