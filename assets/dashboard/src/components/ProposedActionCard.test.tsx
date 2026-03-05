import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ProposedActionCard, PinnedActionRow } from './ProposedActionCard';
import type { Action } from '../lib/types.generated';

function makeAction(overrides: Partial<Action> = {}): Action {
  return {
    id: 'act-test-1',
    name: 'Fix lint errors',
    type: 'agent',
    scope: 'repo',
    source: 'emerged',
    confidence: 0.75,
    state: 'proposed',
    first_seen: '2025-01-01T00:00:00Z',
    template: 'Fix lint errors in {{path}}',
    evidence_count: 5,
    ...overrides,
  };
}

describe('ProposedActionCard', () => {
  const defaultHandlers = {
    onPin: vi.fn(),
    onDismiss: vi.fn(),
  };

  it('renders action name, template, and evidence count', () => {
    render(<ProposedActionCard action={makeAction()} {...defaultHandlers} />);

    expect(screen.getByText('Fix lint errors')).toBeInTheDocument();
    expect(screen.getByText('Fix lint errors in {{path}}')).toBeInTheDocument();
    expect(screen.getByText('Based on 5 similar prompts')).toBeInTheDocument();
  });

  it('renders singular "prompt" for evidence_count=1', () => {
    render(<ProposedActionCard action={makeAction({ evidence_count: 1 })} {...defaultHandlers} />);

    expect(screen.getByText('Based on 1 similar prompt')).toBeInTheDocument();
  });

  it('hides evidence section when evidence_count is 0', () => {
    render(<ProposedActionCard action={makeAction({ evidence_count: 0 })} {...defaultHandlers} />);

    expect(screen.queryByText(/Based on/)).not.toBeInTheDocument();
  });

  it('calls onPin when Pin button clicked', async () => {
    const user = userEvent.setup();
    const onPin = vi.fn();
    const action = makeAction();
    render(<ProposedActionCard action={action} onPin={onPin} onDismiss={vi.fn()} />);

    await user.click(screen.getByText('Pin'));
    expect(onPin).toHaveBeenCalledTimes(1);
    expect(onPin).toHaveBeenCalledWith(action);
  });

  it('calls onDismiss when Dismiss button clicked', async () => {
    const user = userEvent.setup();
    const onDismiss = vi.fn();
    const action = makeAction();
    render(<ProposedActionCard action={action} onPin={vi.fn()} onDismiss={onDismiss} />);

    await user.click(screen.getByText('Dismiss'));
    expect(onDismiss).toHaveBeenCalledTimes(1);
    expect(onDismiss).toHaveBeenCalledWith(action);
  });

  it('renders state badge', () => {
    render(<ProposedActionCard action={makeAction()} {...defaultHandlers} />);

    expect(screen.getByText('proposed')).toBeInTheDocument();
  });

  it('renders learned defaults when present', () => {
    const action = makeAction({
      learned_target: { value: 'sonnet', confidence: 0.9 },
      learned_persona: { value: 'backend', confidence: 0.8 },
    });
    render(<ProposedActionCard action={action} {...defaultHandlers} />);

    expect(screen.getByText('sonnet')).toBeInTheDocument();
    expect(screen.getByText('backend')).toBeInTheDocument();
  });
});

describe('PinnedActionRow', () => {
  it('renders name and template', () => {
    render(<PinnedActionRow action={makeAction({ state: 'pinned', template: 'Build project' })} />);

    expect(screen.getByText('Fix lint errors')).toBeInTheDocument();
    expect(screen.getByText('Build project')).toBeInTheDocument();
  });

  it('renders use count', () => {
    render(<PinnedActionRow action={makeAction({ state: 'pinned', use_count: 7 })} />);

    expect(screen.getByText('7 uses')).toBeInTheDocument();
  });

  it('renders singular use for count of 1', () => {
    render(<PinnedActionRow action={makeAction({ state: 'pinned', use_count: 1 })} />);

    expect(screen.getByText('1 use')).toBeInTheDocument();
  });

  it('renders empty string when use_count is 0', () => {
    render(<PinnedActionRow action={makeAction({ state: 'pinned', use_count: 0 })} />);

    expect(screen.queryByText(/use/)).not.toBeInTheDocument();
  });

  it('uses command as fallback when template is empty', () => {
    render(
      <PinnedActionRow
        action={makeAction({ state: 'pinned', template: '', command: 'go build ./...' })}
      />
    );

    expect(screen.getByText('go build ./...')).toBeInTheDocument();
  });
});

describe('ConfidenceDots', () => {
  it('fills correct number of dots for confidence=0.75', () => {
    const { container } = render(
      <ProposedActionCard
        action={makeAction({ confidence: 0.75 })}
        onPin={vi.fn()}
        onDismiss={vi.fn()}
      />
    );

    // ConfidenceDots renders 4 dots; at 0.75, Math.round(0.75*4)=3 filled
    const dotsContainer = container.querySelector('[data-testid="confidence-dots"]');
    expect(dotsContainer).toBeInTheDocument();
    const dotSpans = dotsContainer!.querySelectorAll('[data-filled]');
    expect(dotSpans.length).toBe(4);
    const filled = Array.from(dotSpans).filter((el) => el.getAttribute('data-filled') === 'true');
    expect(filled.length).toBe(3);
  });

  it('fills all dots for confidence=1.0', () => {
    const { container } = render(
      <ProposedActionCard
        action={makeAction({ confidence: 1.0 })}
        onPin={vi.fn()}
        onDismiss={vi.fn()}
      />
    );

    const dotsContainer = container.querySelector('[data-testid="confidence-dots"]');
    const dotSpans = dotsContainer!.querySelectorAll('[data-filled]');
    const filled = Array.from(dotSpans).filter((el) => el.getAttribute('data-filled') === 'true');
    expect(filled.length).toBe(4);
  });

  it('fills no dots for confidence=0', () => {
    const { container } = render(
      <ProposedActionCard
        action={makeAction({ confidence: 0 })}
        onPin={vi.fn()}
        onDismiss={vi.fn()}
      />
    );

    const dotsContainer = container.querySelector('[data-testid="confidence-dots"]');
    const dotSpans = dotsContainer!.querySelectorAll('[data-filled]');
    const filled = Array.from(dotSpans).filter((el) => el.getAttribute('data-filled') === 'true');
    expect(filled.length).toBe(0);
  });
});
