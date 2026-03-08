import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import PromptAutocomplete, { matchItems, type PromptHistoryEntry } from './PromptAutocomplete';
import type { SpawnEntry } from '../lib/types.generated';

// --- matchItems pure-function tests ---

function makeEntry(overrides: Partial<SpawnEntry> = {}): SpawnEntry {
  return {
    id: 'se-1',
    name: 'Fix lint',
    type: 'agent',
    source: 'manual',
    state: 'pinned',
    use_count: 0,
    prompt: 'Fix lint errors',
    ...overrides,
  };
}

function makeHistory(text: string, count = 1): PromptHistoryEntry {
  return { text, last_seen: '2025-01-01T00:00:00Z', count };
}

describe('matchItems', () => {
  it('returns empty for no matches', () => {
    const entries = [makeEntry({ prompt: 'Build project' })];
    const history = [makeHistory('Deploy server')];
    expect(matchItems('zzz', entries, history)).toEqual([]);
  });

  it('prefix matches sort before substring matches', () => {
    const entries = [
      makeEntry({ id: 'a1', name: 'Contains fix here', prompt: 'Contains fix here' }),
      makeEntry({ id: 'a2', name: 'Fix lint', prompt: 'Fix lint errors' }),
    ];
    const items = matchItems('fix', entries, []);
    expect(items.length).toBe(2);
    expect(items[0].text).toBe('Fix lint errors'); // prefix match
    expect(items[1].text).toBe('Contains fix here'); // substring match
  });

  it('entries appear before history', () => {
    const entries = [makeEntry({ prompt: 'Run tests' })];
    const history = [makeHistory('Run all tests')];
    const items = matchItems('run', entries, history);
    expect(items.length).toBe(2);
    expect(items[0].source).toBe('spawn-entry');
    expect(items[1].source).toBe('history');
  });

  it('deduplicates history entry matching entry prompt', () => {
    const entries = [makeEntry({ prompt: 'Fix lint errors' })];
    const history = [makeHistory('Fix lint errors')];
    const items = matchItems('fix', entries, history);
    expect(items.length).toBe(1);
    expect(items[0].source).toBe('spawn-entry');
  });

  it('caps at 8 results', () => {
    const history = Array.from({ length: 15 }, (_, i) => makeHistory(`task number ${i}`));
    const items = matchItems('task', [], history);
    expect(items.length).toBe(8);
  });

  it('only includes pinned entries (skips proposed/dismissed)', () => {
    const entries = [
      makeEntry({ id: 'a1', state: 'pinned', prompt: 'Fix lint' }),
      makeEntry({ id: 'a2', state: 'proposed', prompt: 'Fix tests' }),
      makeEntry({ id: 'a3', state: 'dismissed', prompt: 'Fix docs' }),
    ];
    const items = matchItems('fix', entries, []);
    expect(items.length).toBe(1);
    expect(items[0].text).toBe('Fix lint');
  });

  it('uses entry name as fallback when prompt is empty', () => {
    const entries = [makeEntry({ name: 'Build all', prompt: '' })];
    const items = matchItems('build', entries, []);
    expect(items.length).toBe(1);
    expect(items[0].text).toBe('Build all');
  });

  it('shows use count as meta', () => {
    const entries = [makeEntry({ prompt: 'Fix lint', use_count: 5 })];
    const items = matchItems('fix', entries, []);
    expect(items[0].meta).toBe('5x');
  });
});

// --- Component render tests ---

describe('PromptAutocomplete', () => {
  const defaultProps = {
    selectedIndex: 0,
    onSelect: vi.fn(),
    onHover: vi.fn(),
  };

  it('renders nothing when no items match', () => {
    const { container } = render(
      <PromptAutocomplete {...defaultProps} query="zzz" entries={[makeEntry()]} history={[]} />
    );
    expect(container.innerHTML).toBe('');
  });

  it('renders items with correct source badges', () => {
    const entries = [makeEntry({ prompt: 'Fix lint errors' })];
    const history = [makeHistory('Fix all the things')];
    render(
      <PromptAutocomplete {...defaultProps} query="fix" entries={entries} history={history} />
    );

    expect(screen.getByText('action')).toBeInTheDocument();
    expect(screen.getByText('history')).toBeInTheDocument();
    expect(screen.getByText('Fix lint errors')).toBeInTheDocument();
    expect(screen.getByText('Fix all the things')).toBeInTheDocument();
  });

  it('calls onSelect when item clicked', async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(
      <PromptAutocomplete
        {...defaultProps}
        query="fix"
        entries={[makeEntry({ prompt: 'Fix lint errors' })]}
        history={[]}
        onSelect={onSelect}
      />
    );

    await user.click(screen.getByText('Fix lint errors'));
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith(
      expect.objectContaining({ text: 'Fix lint errors', source: 'spawn-entry' })
    );
  });

  it('calls onHover on mouse enter', async () => {
    const user = userEvent.setup();
    const onHover = vi.fn();
    const entries = [
      makeEntry({ id: 'a1', prompt: 'Fix lint' }),
      makeEntry({ id: 'a2', prompt: 'Fix tests' }),
    ];
    render(
      <PromptAutocomplete
        {...defaultProps}
        query="fix"
        entries={entries}
        history={[]}
        onHover={onHover}
      />
    );

    await user.hover(screen.getByText('Fix tests'));
    expect(onHover).toHaveBeenCalledWith(1);
  });

  it('highlights selectedIndex item', () => {
    const entries = [
      makeEntry({ id: 'a1', prompt: 'Fix lint' }),
      makeEntry({ id: 'a2', prompt: 'Fix tests' }),
    ];
    render(
      <PromptAutocomplete
        {...defaultProps}
        query="fix"
        entries={entries}
        history={[]}
        selectedIndex={1}
      />
    );

    const buttons = screen.getAllByRole('button');
    // Second button should have aria-selected="true"
    expect(buttons[1].getAttribute('aria-selected')).toBe('true');
    expect(buttons[0].getAttribute('aria-selected')).toBe('false');
  });

  it('accepts pre-computed items prop and skips internal matching', () => {
    const items = [
      { text: 'Pre-computed item', source: 'spawn-entry' as const },
      { text: 'Another item', source: 'history' as const },
    ];
    render(
      <PromptAutocomplete {...defaultProps} query="" entries={[]} history={[]} items={items} />
    );

    expect(screen.getByText('Pre-computed item')).toBeInTheDocument();
    expect(screen.getByText('Another item')).toBeInTheDocument();
  });
});
