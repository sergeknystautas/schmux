import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import PromptAutocomplete, { matchItems } from './PromptAutocomplete';
import type { Action } from '../lib/types.generated';
import type { PromptHistoryEntry } from '../lib/types.generated';

// --- matchItems pure-function tests ---

function makeAction(overrides: Partial<Action> = {}): Action {
  return {
    id: 'act-1',
    name: 'Fix lint',
    type: 'agent',
    scope: 'repo',
    source: 'manual',
    confidence: 1.0,
    state: 'pinned',
    first_seen: '2025-01-01T00:00:00Z',
    template: 'Fix lint errors',
    ...overrides,
  };
}

function makeHistory(text: string, count = 1): PromptHistoryEntry {
  return { text, last_seen: '2025-01-01T00:00:00Z', count };
}

describe('matchItems', () => {
  it('returns empty for no matches', () => {
    const actions = [makeAction({ template: 'Build project' })];
    const history = [makeHistory('Deploy server')];
    expect(matchItems('zzz', actions, history)).toEqual([]);
  });

  it('prefix matches sort before substring matches', () => {
    const actions = [
      makeAction({ id: 'a1', name: 'Contains fix here', template: 'Contains fix here' }),
      makeAction({ id: 'a2', name: 'Fix lint', template: 'Fix lint errors' }),
    ];
    const items = matchItems('fix', actions, []);
    expect(items.length).toBe(2);
    expect(items[0].text).toBe('Fix lint errors'); // prefix match
    expect(items[1].text).toBe('Contains fix here'); // substring match
  });

  it('actions appear before history', () => {
    const actions = [makeAction({ template: 'Run tests' })];
    const history = [makeHistory('Run all tests')];
    const items = matchItems('run', actions, history);
    expect(items.length).toBe(2);
    expect(items[0].source).toBe('action');
    expect(items[1].source).toBe('history');
  });

  it('deduplicates history entry matching action template', () => {
    const actions = [makeAction({ template: 'Fix lint errors' })];
    const history = [makeHistory('Fix lint errors')];
    const items = matchItems('fix', actions, history);
    expect(items.length).toBe(1);
    expect(items[0].source).toBe('action');
  });

  it('caps at 8 results', () => {
    const history = Array.from({ length: 15 }, (_, i) => makeHistory(`task number ${i}`));
    const items = matchItems('task', [], history);
    expect(items.length).toBe(8);
  });

  it('only includes pinned actions (skips proposed/dismissed)', () => {
    const actions = [
      makeAction({ id: 'a1', state: 'pinned', template: 'Fix lint' }),
      makeAction({ id: 'a2', state: 'proposed', template: 'Fix tests' }),
      makeAction({ id: 'a3', state: 'dismissed', template: 'Fix docs' }),
    ];
    const items = matchItems('fix', actions, []);
    expect(items.length).toBe(1);
    expect(items[0].text).toBe('Fix lint');
  });

  it('uses action name as fallback when template is empty', () => {
    const actions = [makeAction({ name: 'Build all', template: '' })];
    const items = matchItems('build', actions, []);
    expect(items.length).toBe(1);
    expect(items[0].text).toBe('Build all');
  });

  it('shows use count as meta', () => {
    const actions = [makeAction({ template: 'Fix lint', use_count: 5 })];
    const items = matchItems('fix', actions, []);
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
      <PromptAutocomplete {...defaultProps} query="zzz" actions={[makeAction()]} history={[]} />
    );
    expect(container.innerHTML).toBe('');
  });

  it('renders items with correct source badges', () => {
    const actions = [makeAction({ template: 'Fix lint errors' })];
    const history = [makeHistory('Fix all the things')];
    render(
      <PromptAutocomplete {...defaultProps} query="fix" actions={actions} history={history} />
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
        actions={[makeAction({ template: 'Fix lint errors' })]}
        history={[]}
        onSelect={onSelect}
      />
    );

    await user.click(screen.getByText('Fix lint errors'));
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith(
      expect.objectContaining({ text: 'Fix lint errors', source: 'action' })
    );
  });

  it('calls onHover on mouse enter', async () => {
    const user = userEvent.setup();
    const onHover = vi.fn();
    const actions = [
      makeAction({ id: 'a1', template: 'Fix lint' }),
      makeAction({ id: 'a2', template: 'Fix tests' }),
    ];
    render(
      <PromptAutocomplete
        {...defaultProps}
        query="fix"
        actions={actions}
        history={[]}
        onHover={onHover}
      />
    );

    await user.hover(screen.getByText('Fix tests'));
    expect(onHover).toHaveBeenCalledWith(1);
  });

  it('highlights selectedIndex item', () => {
    const actions = [
      makeAction({ id: 'a1', template: 'Fix lint' }),
      makeAction({ id: 'a2', template: 'Fix tests' }),
    ];
    render(
      <PromptAutocomplete
        {...defaultProps}
        query="fix"
        actions={actions}
        history={[]}
        selectedIndex={1}
      />
    );

    const buttons = screen.getAllByRole('button');
    // Second button should have the selected class
    expect(buttons[1].className).toContain('Selected');
    expect(buttons[0].className).not.toContain('Selected');
  });

  it('accepts pre-computed items prop and skips internal matching', () => {
    const items = [
      { text: 'Pre-computed item', source: 'action' as const },
      { text: 'Another item', source: 'history' as const },
    ];
    render(
      <PromptAutocomplete {...defaultProps} query="" actions={[]} history={[]} items={items} />
    );

    expect(screen.getByText('Pre-computed item')).toBeInTheDocument();
    expect(screen.getByText('Another item')).toBeInTheDocument();
  });
});
