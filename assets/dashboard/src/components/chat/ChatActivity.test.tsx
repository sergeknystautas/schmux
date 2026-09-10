import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import ChatActivity from './ChatActivity';
import { reduceRecords, emptyConversation } from '../../lib/chat/reducer';
import activityChecklistRaw from '../../lib/chat/__fixtures__/claude/activity-checklist.jsonl?raw';
import activityBackgroundRaw from '../../lib/chat/__fixtures__/claude/activity-background.jsonl?raw';
import activityAgentRaw from '../../lib/chat/__fixtures__/claude/activity-agent.jsonl?raw';
import type { Conversation, ConversationRecord, HarnessLine } from '../../lib/chat/types';

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

describe('ChatActivity', () => {
  let now = 0;
  beforeEach(() => {
    now = Date.parse('2026-01-01T00:00:00.000Z');
    vi.spyOn(Date, 'now').mockImplementation(() => now);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders nothing while history has not loaded', () => {
    const { container } = render(
      <ChatActivity
        conversation={emptyConversation()}
        status="connecting"
        historyLoaded={false}
        ended={false}
      />
    );
    expect(container.querySelector('[data-testid="chat-activity"]')).toBeNull();
  });

  it('renders nothing when there is nothing to show', () => {
    const { container } = render(
      <ChatActivity
        conversation={emptyConversation()}
        status="connected"
        historyLoaded={true}
        ended={false}
      />
    );
    expect(container.querySelector('[data-testid="chat-activity"]')).toBeNull();
  });

  it('hides completed and pending checklist entries', () => {
    const lines = fixture('checklist');
    const recs: ConversationRecord[] = [user('use TaskCreate')];
    for (const line of lines) {
      if (line.type === 'system' && line.subtype === 'init') continue;
      recs.push(harness(line));
    }
    const conversation = reduceRecords('claude-stream-json', recs);
    now = Date.parse('2026-01-01T00:00:30.000Z');
    render(
      <ChatActivity
        conversation={conversation}
        status="connected"
        historyLoaded={true}
        ended={false}
      />
    );
    const plan = screen.queryByTestId('chat-activity-plan');
    expect(plan).toBeNull();
  });

  it('renders "Session ended" when the process is gone', () => {
    const recs: ConversationRecord[] = [
      user('hi'),
      harness({ type: 'session', event: 'ended' } as HarnessLine),
    ];
    const conversation = reduceRecords('claude-stream-json', recs);
    render(
      <ChatActivity conversation={conversation} status="gone" historyLoaded={true} ended={true} />
    );
    expect(screen.getByTestId('chat-activity-headline').textContent).toContain('Session ended');
  });

  it('keeps the panel visible during a reconnect (finding 9)', () => {
    // Build a conversation that has activity rows so the panel would be
    // visible. After the socket disconnects (historyLoaded=false), the
    // panel must still render the existing rows with the
    // "Reconnecting · activity may be out of date" headline.
    const lines = fixture('agent');
    const ts = '2026-01-01T00:00:00.000Z';
    const recs: ConversationRecord[] = [
      { ts, type: 'user_message', id: 'u-1', text: 'launch subagent' },
    ];
    for (const line of lines) {
      if (line.type === 'system' && line.subtype === 'init') continue;
      recs.push({ ts, type: 'harness', line });
    }
    const conversation = reduceRecords('claude-stream-json', recs);
    const { rerender, container } = render(
      <ChatActivity
        conversation={conversation}
        status="connected"
        historyLoaded={true}
        ended={false}
      />
    );
    // Reconnect: historyLoaded=false, but priorConnected is true (set after
    // the initial connect). The panel must remain visible.
    rerender(
      <ChatActivity
        conversation={conversation}
        status="disconnected"
        historyLoaded={false}
        ended={false}
      />
    );
    expect(container.querySelector('[data-testid="chat-activity"]')).not.toBeNull();
  });

  it('hides the panel during the initial connect before any history is loaded', () => {
    const { container } = render(
      <ChatActivity
        conversation={emptyConversation()}
        status="connecting"
        historyLoaded={false}
        ended={false}
      />
    );
    expect(container.querySelector('[data-testid="chat-activity"]')).toBeNull();
  });

  it('hides a completed background task fixture', () => {
    const lines = fixture('background');
    const recs: ConversationRecord[] = [user('start a bg job')];
    for (const line of lines) {
      if (line.type === 'system' && line.subtype === 'init') continue;
      recs.push(harness(line));
    }
    const conversation = reduceRecords('claude-stream-json', recs);
    const { container } = render(
      <ChatActivity
        conversation={conversation}
        status="connected"
        historyLoaded={true}
        ended={false}
      />
    );
    expect(container.querySelector('[data-testid="chat-activity-row"]')).toBeNull();
  });
});

describe('ChatActivity: show-all expansion (finding 3)', () => {
  it('reveals the 4th and 5th rows when the user expands the panel', () => {
    // Build a conversation with five running rows.
    const base = reduceRecords('claude-stream-json', [
      { ts: 't', type: 'user_message', id: 'u-1', text: 'go' },
    ]);
    let conversation = base;
    for (let i = 0; i < 5; i++) {
      conversation = {
        ...conversation,
        activity: {
          ...conversation.activity,
          operations: {
            ...conversation.activity.operations,
            [`codex-agent:op-${i}`]: {
              namespace: 'codex-agent',
              id: `op-${i}`,
              kind: 'codex-agent',
              title: `op ${i}`,
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
          order: [...conversation.activity.order, `codex-agent:op-${i}`],
        },
      };
    }
    const { container } = render(
      <ChatActivity
        conversation={conversation}
        status="connected"
        historyLoaded={true}
        ended={false}
      />
    );
    // Default state: only the first 3 rows render.
    let rowItems = container.querySelectorAll('[data-testid="chat-activity-row"]');
    expect(rowItems).toHaveLength(3);
    // Click the Show-all button and confirm 4th and 5th rows appear.
    const showAll = container.querySelector(
      '[data-testid="chat-activity-show-all"]'
    ) as HTMLButtonElement;
    expect(showAll).not.toBeNull();
    fireEvent.click(showAll);
    rowItems = container.querySelectorAll('[data-testid="chat-activity-row"]');
    expect(rowItems).toHaveLength(5);
    // Toggle back to fewer rows.
    fireEvent.click(showAll);
    rowItems = container.querySelectorAll('[data-testid="chat-activity-row"]');
    expect(rowItems).toHaveLength(3);
  });
});

describe('ChatActivity: row expand affordance', () => {
  it('shows reported runtime in the row and expands usage details', () => {
    const conversation = reduceRecords('claude-stream-json', [
      { ts: '2026-01-01T00:00:00.000Z', type: 'user_message', id: 'u-1', text: 'go' },
    ]);
    const seeded: Conversation = {
      ...conversation,
      activity: {
        ...conversation.activity,
        operations: {
          ...conversation.activity.operations,
          'codex-agent:op-1': {
            namespace: 'codex-agent',
            id: 'op-1',
            kind: 'codex-agent',
            title: 'Run editor tests',
            ownerTurnId: null,
            parentId: null,
            lifecycle: 'running',
            rawStatus: null,
            firstObservedAt: '2026-01-01T00:00:00.000Z',
            startTime: null,
            endTime: null,
            lastUpdateAt: '2026-01-01T00:00:00.000Z',
            durationMs: 30000,
            latestActivity: 'sleep 60',
            usage: { toolUses: 3, totalTokens: 120 },
            toolId: null,
            assignmentId: 0,
            outputFile: null,
            terminalAt: null,
          },
        },
        order: ['codex-agent:op-1'],
      },
    };
    const { container } = render(
      <ChatActivity conversation={seeded} status="connected" historyLoaded={true} ended={false} />
    );
    const expandBtn = container.querySelector(
      '[data-testid="chat-activity-row-expand"]'
    ) as HTMLButtonElement;
    expect(expandBtn).not.toBeNull();
    expect(expandBtn.textContent).toBe('Details');
    // Details not yet visible.
    expect(container.querySelector('[data-testid="chat-activity-row-details"]')).toBeNull();
    expect(container.textContent).toContain('Last reported runtime: 30s');
    // Click to expand.
    fireEvent.click(expandBtn);
    const details = container.querySelector('[data-testid="chat-activity-row-details"]');
    expect(details).not.toBeNull();
    expect(details?.textContent).toContain('Latest: sleep 60');
    expect(container.textContent).toContain('Last reported runtime: 30s');
    expect(details?.textContent).toContain('Tool calls: 3');
    expect(details?.textContent).toContain('Tokens: 120');
    // Click to collapse.
    fireEvent.click(expandBtn);
    expect(container.querySelector('[data-testid="chat-activity-row-details"]')).toBeNull();
  });

  it('does not advertise an anonymous unlinked command', () => {
    const conversation = reduceRecords('claude-stream-json', [
      { ts: 't', type: 'user_message', id: 'u-1', text: 'go' },
    ]);
    const seeded: Conversation = {
      ...conversation,
      activity: {
        ...conversation.activity,
        operations: {
          ...conversation.activity.operations,
          'codex-tool:plain': {
            namespace: 'codex-tool',
            id: 'plain',
            kind: 'codex-tool',
            title: 'Bash',
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
        order: ['codex-tool:plain'],
      },
    };
    const { container } = render(
      <ChatActivity conversation={seeded} status="connected" historyLoaded={true} ended={false} />
    );
    expect(container.querySelector('[data-testid="chat-activity-row"]')).toBeNull();
  });
});
