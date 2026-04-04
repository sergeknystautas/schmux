import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { LoreCard } from './LoreCard';
import type { LoreRule, LoreLayer } from '../lib/types';
import type { SpawnEntry } from '../lib/types.generated';

function makeRule(overrides: Partial<LoreRule> = {}): LoreRule {
  return {
    id: 'r1',
    text: 'Always run tests before committing',
    category: 'workflow',
    suggested_layer: 'repo_private' as LoreLayer,
    status: 'pending',
    source_entries: [
      {
        type: 'failure',
        input_summary: 'git commit',
        error_summary: 'tests not run',
        tool: 'bash',
      },
      {
        type: 'reflection',
        text: 'Running tests catches regressions early',
      },
    ],
    ...overrides,
  };
}

function makeAction(overrides: Partial<SpawnEntry> = {}): SpawnEntry {
  return {
    id: 'a1',
    name: 'Fix lint errors',
    type: 'skill',
    prompt: './format.sh && ./test.sh --quick',
    state: 'proposed',
    source: 'emerged',
    use_count: 0,
    description: 'Auto-format and run quick tests',
    metadata: {
      skill_name: 'fix-lint',
      confidence: 0.8,
      evidence: ['ran format 4 times'],
      evidence_count: 4,
      emerged_at: '2026-04-01T00:00:00Z',
      last_curated: '2026-04-01T00:00:00Z',
    },
    ...overrides,
  };
}

const instructionDefaults = {
  type: 'instruction' as const,
  repoName: 'schmux',
  proposalId: 'p1',
  onApprove: vi.fn(),
  onDismiss: vi.fn(),
  onEdit: vi.fn(),
  onLayerChange: vi.fn(),
};

const actionDefaults = {
  type: 'action' as const,
  repoName: 'schmux',
  onApprove: vi.fn(),
  onDismiss: vi.fn(),
  onEdit: vi.fn(),
};

describe('LoreCard instruction variant', () => {
  it('renders rule text and source signals', () => {
    const rule = makeRule();
    render(<LoreCard {...instructionDefaults} rule={rule} />);

    expect(screen.getByText('Always run tests before committing')).toBeInTheDocument();
    // Failure signal
    expect(screen.getByText('git commit \u2192 "tests not run"')).toBeInTheDocument();
    // Reflection signal
    expect(screen.getByText('Running tests catches regressions early')).toBeInTheDocument();
  });

  it('shows category tag and repo name', () => {
    render(<LoreCard {...instructionDefaults} rule={makeRule()} />);

    expect(screen.getByText('workflow')).toBeInTheDocument();
    expect(screen.getByText('schmux')).toBeInTheDocument();
  });

  it('defaults to private with "Commit to repo" unchecked', () => {
    render(<LoreCard {...instructionDefaults} rule={makeRule()} />);

    const checkbox = screen.getByLabelText('Commit to repo (visible to collaborators)');
    expect(checkbox).not.toBeChecked();
  });

  it('calls onApprove with rule ID when Approve clicked', () => {
    const onApprove = vi.fn();
    render(<LoreCard {...instructionDefaults} rule={makeRule()} onApprove={onApprove} />);

    fireEvent.click(screen.getByText('Approve'));
    expect(onApprove).toHaveBeenCalledTimes(1);
    expect(onApprove).toHaveBeenCalledWith('r1');
  });

  it('calls onDismiss with rule ID when Dismiss clicked', () => {
    vi.useFakeTimers();
    const onDismiss = vi.fn();
    render(<LoreCard {...instructionDefaults} rule={makeRule()} onDismiss={onDismiss} />);

    fireEvent.click(screen.getByText('Dismiss'));
    // Should not fire immediately
    expect(onDismiss).not.toHaveBeenCalled();
    // Fire after animation delay
    vi.advanceTimersByTime(200);
    expect(onDismiss).toHaveBeenCalledTimes(1);
    expect(onDismiss).toHaveBeenCalledWith('r1');
    vi.useRealTimers();
  });

  it('shows textarea when Edit clicked, calls onEdit with new text on Save', () => {
    const onEdit = vi.fn();
    render(<LoreCard {...instructionDefaults} rule={makeRule()} onEdit={onEdit} />);

    fireEvent.click(screen.getByText('Edit'));

    const textarea = screen.getByRole('textbox');
    expect(textarea).toBeInTheDocument();
    expect(textarea).toHaveValue('Always run tests before committing');

    fireEvent.change(textarea, { target: { value: 'Updated rule text' } });
    fireEvent.click(screen.getByText('Save'));

    expect(onEdit).toHaveBeenCalledTimes(1);
    expect(onEdit).toHaveBeenCalledWith('r1', 'Updated rule text');
  });

  it('restores original text on Cancel', () => {
    render(<LoreCard {...instructionDefaults} rule={makeRule()} />);

    fireEvent.click(screen.getByText('Edit'));

    const textarea = screen.getByRole('textbox');
    fireEvent.change(textarea, { target: { value: 'Changed text' } });

    fireEvent.click(screen.getByText('Cancel'));

    // Should be back to showing the original rule text, not a textarea
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    expect(screen.getByText('Always run tests before committing')).toBeInTheDocument();
  });

  it('shows collapsed variant when status is approved', () => {
    const rule = makeRule({ status: 'approved', chosen_layer: 'repo_public' });
    render(<LoreCard {...instructionDefaults} rule={rule} />);

    // Collapsed state shows check mark and truncated text
    expect(screen.getByText('\u2713')).toBeInTheDocument();
    expect(screen.getByText('Always run tests before committing')).toBeInTheDocument();
    expect(screen.getByText('Public')).toBeInTheDocument();

    // Should NOT show action buttons
    expect(screen.queryByText('Approve')).not.toBeInTheDocument();
    expect(screen.queryByText('Dismiss')).not.toBeInTheDocument();
    expect(screen.queryByText('Edit')).not.toBeInTheDocument();
  });

  it('calls onLayerChange when privacy checkboxes change', () => {
    const onLayerChange = vi.fn();
    render(<LoreCard {...instructionDefaults} rule={makeRule()} onLayerChange={onLayerChange} />);

    const commitCheckbox = screen.getByLabelText('Commit to repo (visible to collaborators)');
    fireEvent.click(commitCheckbox);

    expect(onLayerChange).toHaveBeenCalledWith('r1', 'repo_public');

    // "Apply to all" should now be visible
    const applyAllCheckbox = screen.getByLabelText('Apply to all my repos');
    fireEvent.click(applyAllCheckbox);

    expect(onLayerChange).toHaveBeenCalledWith('r1', 'cross_repo_private');
  });
});

describe('LoreCard action variant', () => {
  it('renders action name and prompt', () => {
    render(<LoreCard {...actionDefaults} action={makeAction()} />);

    expect(screen.getByText('Fix lint errors')).toBeInTheDocument();
    expect(screen.getByText('./format.sh && ./test.sh --quick')).toBeInTheDocument();
  });

  it('does not show privacy controls', () => {
    render(<LoreCard {...actionDefaults} action={makeAction()} />);

    expect(
      screen.queryByLabelText('Commit to repo (visible to collaborators)')
    ).not.toBeInTheDocument();
  });

  it('shows evidence as source signals', () => {
    render(<LoreCard {...actionDefaults} action={makeAction()} />);

    expect(screen.getByText('ran format 4 times')).toBeInTheDocument();
  });

  it('calls onApprove with action ID when Approve clicked', () => {
    const onApprove = vi.fn();
    render(<LoreCard {...actionDefaults} action={makeAction()} onApprove={onApprove} />);

    fireEvent.click(screen.getByText('Approve'));
    expect(onApprove).toHaveBeenCalledWith('a1');
  });
});
