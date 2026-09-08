import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import QuestionCard from './QuestionCard';
import type { PendingSegment } from '../../lib/chat/types';

const pending: PendingSegment = {
  kind: 'pending',
  requestId: 'r1',
  toolUseId: 't1',
  toolName: 'AskUserQuestion',
  input: {},
  questions: [
    {
      id: 'Pick one?',
      question: 'Pick one?',
      options: [{ label: 'A' }, { label: 'B' }],
      multiSelect: false,
    },
  ],
};

function renderCard(overrides: Partial<Parameters<typeof QuestionCard>[0]> = {}) {
  const props = { pending, onAnswer: vi.fn(), ...overrides };
  render(<QuestionCard {...props} />);
  return props;
}

describe('QuestionCard persistence', () => {
  beforeEach(() => {
    sessionStorage.clear();
  });

  it('initializes selections and Other text from initialAnswers', () => {
    renderCard({
      initialAnswers: { 'Pick one?': { selected: ['B'], other: 'typed note' } },
    });
    expect(screen.getByRole('radio', { name: 'B' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByLabelText('Other ( Pick one? )')).toHaveValue('typed note');
  });

  it('reports answer changes per question', () => {
    const onAnswerChange = vi.fn();
    renderCard({ onAnswerChange });
    fireEvent.click(screen.getByRole('radio', { name: 'A' }));
    expect(onAnswerChange).toHaveBeenLastCalledWith('Pick one?', { selected: ['A'], other: '' });
    fireEvent.change(screen.getByLabelText('Other ( Pick one? )'), { target: { value: 'x' } });
    expect(onAnswerChange).toHaveBeenLastCalledWith('Pick one?', { selected: ['A'], other: 'x' });
  });

  it('reports focus targets with request id and caret', () => {
    const onFocusChange = vi.fn();
    renderCard({ onFocusChange });
    const input = screen.getByLabelText('Other ( Pick one? )') as HTMLInputElement;
    fireEvent.focus(input);
    expect(onFocusChange).toHaveBeenLastCalledWith({
      target: 'other-input',
      requestId: 'r1',
      questionId: 'Pick one?',
      position: 0,
    });
    fireEvent.focus(screen.getByRole('radio', { name: 'A' }));
    expect(onFocusChange).toHaveBeenLastCalledWith({
      target: 'option',
      requestId: 'r1',
      questionId: 'Pick one?',
      label: 'A',
    });
  });

  it('tags inputs and option buttons for transcript-level focus restore', () => {
    renderCard();
    const input = screen.getByLabelText('Other ( Pick one? )');
    expect(input).toHaveAttribute('data-chat-question-target');
    expect(input).toHaveAttribute('data-request-id', 'r1');
    expect(input).toHaveAttribute('data-question-id', 'Pick one?');
    const button = screen.getByRole('radio', { name: 'A' });
    expect(button).toHaveAttribute('data-chat-question-target');
    expect(button).toHaveAttribute('data-option-label', 'A');
  });
});
