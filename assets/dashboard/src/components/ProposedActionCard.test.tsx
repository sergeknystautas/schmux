import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ProposedActionCard, PinnedActionRow } from './ProposedActionCard';
import type { SpawnEntry } from '../lib/types.generated';

function makeEntry(overrides: Partial<SpawnEntry> = {}): SpawnEntry {
  return {
    id: 'se-test-1',
    name: 'Fix lint errors',
    type: 'skill',
    source: 'emerged',
    state: 'proposed',
    use_count: 0,
    prompt: 'Fix lint errors in the project',
    ...overrides,
  };
}

describe('ProposedActionCard', () => {
  const defaultHandlers = {
    onPin: vi.fn(),
    onDismiss: vi.fn(),
  };

  it('renders entry name and prompt', () => {
    render(<ProposedActionCard entry={makeEntry()} {...defaultHandlers} />);

    expect(screen.getByText('Fix lint errors')).toBeInTheDocument();
    expect(screen.getByText('Fix lint errors in the project')).toBeInTheDocument();
  });

  it('renders command when present', () => {
    render(
      <ProposedActionCard
        entry={makeEntry({ prompt: undefined, command: 'go build ./...' })}
        {...defaultHandlers}
      />
    );

    expect(screen.getByText('go build ./...')).toBeInTheDocument();
  });

  it('calls onPin when Pin button clicked', async () => {
    const user = userEvent.setup();
    const onPin = vi.fn();
    const entry = makeEntry();
    render(<ProposedActionCard entry={entry} onPin={onPin} onDismiss={vi.fn()} />);

    await user.click(screen.getByText('Pin'));
    expect(onPin).toHaveBeenCalledTimes(1);
    expect(onPin).toHaveBeenCalledWith(entry);
  });

  it('calls onDismiss when Dismiss button clicked', async () => {
    const user = userEvent.setup();
    const onDismiss = vi.fn();
    const entry = makeEntry();
    render(<ProposedActionCard entry={entry} onPin={vi.fn()} onDismiss={onDismiss} />);

    await user.click(screen.getByText('Dismiss'));
    expect(onDismiss).toHaveBeenCalledTimes(1);
    expect(onDismiss).toHaveBeenCalledWith(entry);
  });

  it('renders state badge', () => {
    render(<ProposedActionCard entry={makeEntry()} {...defaultHandlers} />);

    expect(screen.getByText('proposed')).toBeInTheDocument();
  });

  it('renders skill_ref when present', () => {
    render(
      <ProposedActionCard entry={makeEntry({ skill_ref: 'code-review' })} {...defaultHandlers} />
    );

    expect(screen.getByText('code-review')).toBeInTheDocument();
  });
});

describe('PinnedActionRow', () => {
  it('renders name and prompt', () => {
    render(<PinnedActionRow entry={makeEntry({ state: 'pinned', prompt: 'Build project' })} />);

    expect(screen.getByText('Fix lint errors')).toBeInTheDocument();
    expect(screen.getByText('Build project')).toBeInTheDocument();
  });

  it('renders use count', () => {
    render(<PinnedActionRow entry={makeEntry({ state: 'pinned', use_count: 7 })} />);

    expect(screen.getByText('7 uses')).toBeInTheDocument();
  });

  it('renders singular use for count of 1', () => {
    render(<PinnedActionRow entry={makeEntry({ state: 'pinned', use_count: 1 })} />);

    expect(screen.getByText('1 use')).toBeInTheDocument();
  });

  it('renders empty string when use_count is 0', () => {
    render(<PinnedActionRow entry={makeEntry({ state: 'pinned', use_count: 0 })} />);

    expect(screen.queryByText(/use/)).not.toBeInTheDocument();
  });

  it('uses command as fallback when prompt is empty', () => {
    render(
      <PinnedActionRow
        entry={makeEntry({ state: 'pinned', prompt: undefined, command: 'go build ./...' })}
      />
    );

    expect(screen.getByText('go build ./...')).toBeInTheDocument();
  });
});
