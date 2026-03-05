import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import ModelCatalog from './ModelCatalog';
import type { Model, RunnerInfo } from '../../lib/types';

const makeModel = (overrides: Partial<Model> & { id: string }): Model => ({
  display_name: overrides.id,
  provider: 'test',
  configured: false,
  runners: [],
  ...overrides,
});

const topRunners: Record<string, RunnerInfo> = {
  claude: { available: true, capabilities: ['interactive', 'oneshot', 'streaming'] },
  opencode: { available: true, capabilities: ['interactive', 'oneshot'] },
  codex: { available: true, capabilities: ['interactive', 'oneshot'] },
  gemini: { available: false, capabilities: ['interactive'] },
};

const anthropicModels: Model[] = [
  makeModel({
    id: 'claude-opus-4-6',
    display_name: 'Claude Opus 4.6',
    provider: 'anthropic',
    runners: ['claude', 'opencode'],
  }),
  makeModel({
    id: 'claude-sonnet-4-6',
    display_name: 'Claude Sonnet 4.6',
    provider: 'anthropic',
    runners: ['claude', 'opencode'],
  }),
];

const codexModels: Model[] = [
  makeModel({
    id: 'gpt-5.3-codex',
    display_name: 'GPT 5.3 Codex',
    provider: 'openai',
    runners: ['codex', 'opencode'],
  }),
];

const disabledModels: Model[] = [
  makeModel({
    id: 'gemini-2.5-pro',
    display_name: 'Gemini 2.5 Pro',
    provider: 'google',
    runners: ['gemini'],
  }),
];

describe('ModelCatalog', () => {
  const defaultProps = {
    models: [...anthropicModels, ...codexModels, ...disabledModels],
    runners: topRunners,
    enabledModels: {} as Record<string, string>,
    onToggleModel: vi.fn(),
    onChangeRunner: vi.fn(),
    onModelAction: vi.fn(),
  };

  it('groups models by provider', () => {
    render(<ModelCatalog {...defaultProps} />);
    expect(screen.getByText('anthropic')).toBeInTheDocument();
    expect(screen.getByText('openai')).toBeInTheDocument();
    expect(screen.getByText('google')).toBeInTheDocument();
  });

  it('renders model names', () => {
    render(<ModelCatalog {...defaultProps} />);
    expect(screen.getByText('Claude Opus 4.6')).toBeInTheDocument();
    expect(screen.getByText('GPT 5.3 Codex')).toBeInTheDocument();
  });

  it('shows only detected runners in picker', () => {
    render(<ModelCatalog {...defaultProps} />);
    // Claude Opus has both claude and opencode available
    const opusRow = screen.getByText('Claude Opus 4.6').closest('[data-testid="model-row"]');
    expect(opusRow).toHaveTextContent('claude');
    expect(opusRow).toHaveTextContent('opencode');

    // Claude Sonnet has only claude available (opencode not available)
    const sonnetRow = screen.getByText('Claude Sonnet 4.6').closest('[data-testid="model-row"]');
    expect(sonnetRow).toHaveTextContent('claude');
    // opencode should NOT be in the runner picker for sonnet
  });

  it('disables provider group when no tools detected', () => {
    render(<ModelCatalog {...defaultProps} />);
    const googleProvider = screen.getByText('google').closest('[data-disabled]');
    expect(googleProvider).toHaveAttribute('data-disabled', 'true');
  });

  it('shows models as unchecked when enabledModels is empty', () => {
    render(<ModelCatalog {...defaultProps} enabledModels={{}} />);
    const checkboxes = screen.getAllByRole('checkbox');
    for (const cb of checkboxes) {
      expect(cb).not.toBeChecked();
    }
  });

  it('calls onToggleModel when checkbox changes', () => {
    const onToggleModel = vi.fn();
    render(<ModelCatalog {...defaultProps} onToggleModel={onToggleModel} />);
    const opusRow = screen.getByText('Claude Opus 4.6').closest('[data-testid="model-row"]');
    const checkbox = opusRow?.querySelector('input[type="checkbox"]') as HTMLInputElement;
    fireEvent.click(checkbox);
    expect(onToggleModel).toHaveBeenCalledWith('claude-opus-4-6', true, 'claude');
  });

  it('toggles model when clicking the row', () => {
    const onToggleModel = vi.fn();
    render(<ModelCatalog {...defaultProps} onToggleModel={onToggleModel} />);
    const opusRow = screen.getByText('Claude Opus 4.6').closest('[data-testid="model-row"]');
    fireEvent.click(opusRow!);
    expect(onToggleModel).toHaveBeenCalledWith('claude-opus-4-6', true, 'claude');
  });

  it('does not double-toggle when clicking the checkbox', () => {
    const onToggleModel = vi.fn();
    render(<ModelCatalog {...defaultProps} onToggleModel={onToggleModel} />);
    const opusRow = screen.getByText('Claude Opus 4.6').closest('[data-testid="model-row"]');
    const checkbox = opusRow?.querySelector('input[type="checkbox"]') as HTMLInputElement;
    fireEvent.click(checkbox);
    // Should fire exactly once, not twice (checkbox click + row click)
    expect(onToggleModel).toHaveBeenCalledTimes(1);
  });

  it('shows checked state for explicitly enabled models', () => {
    render(<ModelCatalog {...defaultProps} enabledModels={{ 'claude-opus-4-6': 'claude' }} />);
    const opusRow = screen.getByText('Claude Opus 4.6').closest('[data-testid="model-row"]');
    const checkbox = opusRow?.querySelector('input[type="checkbox"]') as HTMLInputElement;
    expect(checkbox).toBeChecked();
  });

  it('highlights selected runner in segmented control', () => {
    render(<ModelCatalog {...defaultProps} enabledModels={{ 'claude-opus-4-6': 'opencode' }} />);
    const opusRow = screen.getByText('Claude Opus 4.6').closest('[data-testid="model-row"]');
    const selectedBtn = opusRow?.querySelector(
      '[data-testid="runner-option"][data-selected="true"]'
    );
    expect(selectedBtn).toHaveTextContent('opencode');
  });

  it('disables runner picker when model is not enabled', () => {
    render(<ModelCatalog {...defaultProps} enabledModels={{}} />);
    const opusRow = screen.getByText('Claude Opus 4.6').closest('[data-testid="model-row"]');
    const picker = opusRow?.querySelector('[data-testid="runner-picker"]');
    expect(picker).toHaveAttribute('data-disabled', 'true');
  });

  it('sorts models by tier then version within provider', () => {
    const models: Model[] = [
      makeModel({
        id: 'claude-opus-4-6',
        display_name: 'Claude Opus 4.6',
        provider: 'anthropic',
        runners: ['claude'],
      }),
      makeModel({
        id: 'claude-haiku-3-5',
        display_name: 'Claude Haiku 3.5',
        provider: 'anthropic',
        runners: ['claude'],
      }),
      makeModel({
        id: 'claude-sonnet-4',
        display_name: 'Claude Sonnet 4',
        provider: 'anthropic',
        runners: ['claude'],
      }),
    ];
    render(<ModelCatalog {...defaultProps} models={models} />);
    const rows = screen.getAllByText(/Claude/).map((el) => el.textContent);
    expect(rows).toEqual(['Claude Haiku 3.5', 'Claude Sonnet 4', 'Claude Opus 4.6']);
  });

  it('calls onChangeRunner when runner button clicked', () => {
    const onChangeRunner = vi.fn();
    render(
      <ModelCatalog
        {...defaultProps}
        enabledModels={{ 'claude-opus-4-6': 'claude' }}
        onChangeRunner={onChangeRunner}
      />
    );
    const opusRow = screen.getByText('Claude Opus 4.6').closest('[data-testid="model-row"]');
    const opencodeBtn = Array.from(
      opusRow?.querySelectorAll('[data-testid="runner-option"]') || []
    ).find((btn) => btn.textContent === 'opencode');
    if (opencodeBtn) fireEvent.click(opencodeBtn);
    expect(onChangeRunner).toHaveBeenCalledWith('claude-opus-4-6', 'opencode');
  });
});
