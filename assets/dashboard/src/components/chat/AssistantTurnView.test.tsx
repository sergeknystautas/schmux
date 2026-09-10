import { describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import AssistantTurnView from './AssistantTurnView';
import { capturedActivity } from '../../lib/chat/__fixtures__/activity';
import { applyRecord, reduceRecords } from '../../lib/chat/reducer';
import type { AssistantTurn } from '../../lib/chat/types';

const noop = {
  onPermission: vi.fn(),
  onAnswer: vi.fn(),
  onAbort: vi.fn(),
};

function turn(overrides: Partial<AssistantTurn> = {}): AssistantTurn {
  return {
    kind: 'assistant',
    end: null,
    interrupted: false,
    thinking: false,
    segments: [],
    ...overrides,
  };
}

describe('AssistantTurnView', () => {
  it('renders thinking line only while thinking is in progress', () => {
    const { rerender } = render(<AssistantTurnView turn={turn({ thinking: true })} {...noop} />);
    expect(screen.getByTestId('chat-thinking')).toHaveTextContent('Thinking…');
    rerender(<AssistantTurnView turn={turn({ thinking: false })} {...noop} />);
    expect(screen.queryByTestId('chat-thinking')).not.toBeInTheDocument();
  });

  it('renders a thinking disclosure only for non-empty thinking text', async () => {
    const { rerender } = render(
      <AssistantTurnView
        turn={turn({ segments: [{ kind: 'thinking', text: 'let me consider' }] })}
        {...noop}
      />
    );
    await userEvent.click(screen.getByText('Thinking'));
    expect(screen.getByText('let me consider')).toBeInTheDocument();
    rerender(
      <AssistantTurnView turn={turn({ segments: [{ kind: 'thinking', text: '' }] })} {...noop} />
    );
    expect(screen.queryByText('Thinking')).not.toBeInTheDocument();
  });

  it('renders turn end lines: nothing for done, muted Stopped, danger error', () => {
    const { rerender, container } = render(
      <AssistantTurnView turn={turn({ end: { state: 'done' } })} {...noop} />
    );
    expect(container.querySelector('[data-testid="chat-turn-end"]')).toBeNull();
    rerender(<AssistantTurnView turn={turn({ end: { state: 'stopped' } })} {...noop} />);
    expect(screen.getByTestId('chat-turn-end')).toHaveTextContent('Stopped');
    rerender(
      <AssistantTurnView turn={turn({ end: { state: 'error', text: 'boom' } })} {...noop} />
    );
    const end = screen.getByTestId('chat-turn-end');
    expect(end).toHaveTextContent('boom');
    expect(end.dataset.endState).toBe('error');
  });

  it('renders a user segment between prose segments in document order', () => {
    render(
      <AssistantTurnView
        turn={turn({
          segments: [
            { kind: 'prose', text: 'before', streaming: false },
            { kind: 'user', id: 'u2', text: 'steer me', images: [] },
            { kind: 'prose', text: 'after', streaming: false },
          ],
        })}
        {...noop}
      />
    );
    const bubble = screen.getByText('steer me').closest('[data-testid="chat-user-message"]');
    expect(bubble).not.toBeNull();
    const proset = Array.from(screen.getAllByTestId('chat-prose'));
    expect(proset.length).toBe(2);
    const before = proset[0];
    const after = proset[1];
    const orderBefore = bubble!.compareDocumentPosition(before);
    const orderAfter = before.compareDocumentPosition(after);
    expect(orderBefore & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy();
    expect(orderBefore & Node.DOCUMENT_POSITION_FOLLOWING).toBeFalsy();
    expect(orderAfter & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('rewrites absolute workspace file links to content-agnostic jump URLs', () => {
    render(
      <AssistantTurnView
        turn={turn({
          segments: [
            {
              kind: 'prose',
              text: '[Steam notes](/Users/dev/bach-godot-003/docs/liveops/steam-internal.md)',
              streaming: false,
            },
          ],
        })}
        workspaceId="bach-godot-003"
        workspacePath="/Users/dev/bach-godot-003"
        {...noop}
      />
    );

    expect(screen.getByRole('link', { name: 'Steam notes' })).toHaveAttribute(
      'href',
      '/jump/bach-godot-003/docs%2Fliveops%2Fsteam-internal.md'
    );
  });
});

describe('captured late outcomes in the transcript', () => {
  it.each(['background', 'agent'] as const)(
    'updates the closed %s launch row after its terminal notification',
    async (name) => {
      const records = capturedActivity(name);
      const split = records.findIndex(
        (r) => r.type === 'harness' && r.line.subtype === 'task_notification'
      );
      const record = records[split];
      if (record.type !== 'harness') throw new Error('missing notification');
      const toolId = String(record.line.tool_use_id);
      const taskId = String(record.line.task_id);
      expect(taskId).not.toBe(toolId);
      // The capture completes before result; explicitly delay this notification
      // until after a confirmed parent close to exercise the late-delivery case.
      const before = reduceRecords('claude-stream-json', [
        ...records.slice(0, split),
        { type: 'harness', ts: record.ts, line: { type: 'result', subtype: 'success' } },
      ]);
      const closed = before.items.find(
        (i): i is AssistantTurn =>
          i.kind === 'assistant' && i.segments.some((s) => s.kind === 'tool' && s.id === toolId)
      )!;
      expect(closed.end).not.toBeNull();
      const tool = closed.segments.find((s) => s.kind === 'tool' && s.id === toolId)!;
      if (tool.kind !== 'tool') throw new Error('missing launch tool');
      expect(tool.result).not.toBe('');
      const { rerender, container } = render(
        <AssistantTurnView turn={closed} activity={before.activity} {...noop} />
      );
      const after = applyRecord('claude-stream-json', before, record);
      expect(after.items.find((i) => i === closed)).toBe(closed);
      rerender(<AssistantTurnView turn={closed} activity={after.activity} {...noop} />);
      const row = container.querySelector<HTMLElement>(`[data-tool-id="${toolId}"]`)!;
      expect(within(row).getByTestId('chat-tool-dot')).toHaveAttribute('data-state', 'done');
      expect(within(row).getByTestId('chat-tool-result')).toHaveTextContent(
        String(record.line.summary)
      );
      await userEvent.click(within(row).getByTestId('chat-tool-row'));
      // The terminal outcome supplements rather than destroys the launch response.
      expect(
        Array.from(within(row).getByTestId('chat-tool-details').querySelectorAll('pre')).map(
          (pre) => pre.textContent
        )
      ).toContain(tool.result);
      expect(within(row).getByTestId('chat-tool-details')).toHaveTextContent(
        String(record.line.summary)
      );

      const ended = applyRecord('claude-stream-json', after, {
        type: 'session',
        event: 'ended',
        ts: record.ts,
      });
      const restarted = applyRecord('claude-stream-json', ended, {
        type: 'user_message',
        id: 'replacement-user',
        text: 'Next lifetime',
        ts: record.ts,
      });
      expect(restarted.activity.operations).toEqual({});
      const archived = restarted.items.find(
        (i): i is AssistantTurn =>
          i.kind === 'assistant' && i.segments.some((s) => s.kind === 'tool' && s.id === toolId)
      )!;
      rerender(<AssistantTurnView turn={archived} activity={restarted.activity} {...noop} />);
      expect(within(row).getByTestId('chat-tool-result')).toHaveTextContent(
        String(record.line.summary)
      );
      expect(within(row).getByTestId('chat-tool-dot')).toHaveAttribute('data-state', 'done');
    }
  );
});
