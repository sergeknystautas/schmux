import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import AnsweredQuestion from './AnsweredQuestion';
import type { AnsweredSegment } from '../../lib/chat/types';

const segment: AnsweredSegment = {
  kind: 'answered',
  requestId: 'r1',
  toolUseId: 't1',
  toolName: 'AskUserQuestion',
  input: {},
  questions: [
    {
      id: 'Pick one?',
      question: 'Pick one?',
      options: [
        { label: 'A', description: 'first choice' },
        { label: 'B', description: 'second choice' },
      ],
      multiSelect: false,
    },
  ],
  answers: { 'Pick one?': 'B' },
};

describe('AnsweredQuestion', () => {
  it('disables options, highlights the pick, and shows the answer as a user bubble', () => {
    render(<AnsweredQuestion segment={segment} />);
    const a = screen.getByRole('button', { name: /^A/ });
    const b = screen.getByRole('button', { name: /^B/ });
    expect(a).toBeDisabled();
    expect(b).toBeDisabled();
    expect(a).toHaveClass('btn--secondary');
    expect(b).toHaveClass('btn--primary');
    expect(screen.getByTestId('chat-question-answer')).toHaveTextContent('B');
  });

  it('renders no bubble when resolved without an answer', () => {
    render(<AnsweredQuestion segment={{ ...segment, answers: {} }} />);
    expect(screen.queryByTestId('chat-question-answer')).not.toBeInTheDocument();
  });

  it('shows Other-text answers in the bubble without highlighting options', () => {
    render(
      <AnsweredQuestion segment={{ ...segment, answers: { 'Pick one?': 'none of these' } }} />
    );
    expect(screen.getByTestId('chat-question-answer')).toHaveTextContent('none of these');
    expect(screen.getByRole('button', { name: /^A/ })).toHaveClass('btn--secondary');
    expect(screen.getByRole('button', { name: /^B/ })).toHaveClass('btn--secondary');
  });
});
