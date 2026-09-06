import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import AssistantTurnView from './AssistantTurnView';
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
});
