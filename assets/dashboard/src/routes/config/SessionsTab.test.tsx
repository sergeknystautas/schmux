import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import SessionsTab from './SessionsTab';
import type { Model, RunTargetResponse } from '../../lib/types';

const models: Model[] = [
  {
    id: 'claude-opus-4-6',
    display_name: 'Claude Opus 4.6',
    provider: 'anthropic',
    category: 'native',
    configured: true,
    runners: {
      claude: { available: true, configured: true },
      opencode: { available: true, configured: true },
    },
  },
  {
    id: 'gpt-5.3-codex',
    display_name: 'GPT 5.3 Codex',
    provider: 'openai',
    category: 'native',
    configured: true,
    runners: {
      codex: { available: true, configured: true },
      opencode: { available: true, configured: true },
    },
  },
];

const commandTargets: RunTargetResponse[] = [
  { name: 'build', command: 'go build ./...', source: 'user', type: 'command' },
];

const dispatch = vi.fn();

const defaultProps = {
  models,
  enabledModels: {} as Record<string, string>,
  commandTargets,
  newCommandName: '',
  newCommandCommand: '',
  dispatch,
  onAddCommand: vi.fn(),
  onRemoveCommand: vi.fn(),
  onModelAction: vi.fn(),
  onOpenRunTargetEditModal: vi.fn(),
};

describe('SessionsTab', () => {
  it('renders model catalog with provider groups', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.getByText('anthropic')).toBeInTheDocument();
    expect(screen.getByText('openai')).toBeInTheDocument();
  });

  it('renders command targets section', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.getByText('Command Targets')).toBeInTheDocument();
    expect(screen.getByText('build')).toBeInTheDocument();
  });

  it('dispatches TOGGLE_MODEL when checking a model', () => {
    render(<SessionsTab {...defaultProps} />);
    const opusRow = screen.getByText('Claude Opus 4.6').closest('.model-catalog__model-row');
    const checkbox = opusRow?.querySelector('input[type="checkbox"]') as HTMLInputElement;
    fireEvent.click(checkbox);
    expect(dispatch).toHaveBeenCalledWith({
      type: 'TOGGLE_MODEL',
      modelId: 'claude-opus-4-6',
      enabled: true,
      defaultRunner: 'claude',
    });
  });

  it('dispatches TOGGLE_MODEL when unchecking a model', () => {
    render(
      <SessionsTab
        {...defaultProps}
        enabledModels={{ 'claude-opus-4-6': 'claude', 'gpt-5.3-codex': 'codex' }}
      />
    );
    const opusRow = screen.getByText('Claude Opus 4.6').closest('.model-catalog__model-row');
    const checkbox = opusRow?.querySelector('input[type="checkbox"]') as HTMLInputElement;
    fireEvent.click(checkbox);
    expect(dispatch).toHaveBeenCalledWith({
      type: 'TOGGLE_MODEL',
      modelId: 'claude-opus-4-6',
      enabled: false,
      defaultRunner: 'claude',
    });
  });

  it('dispatches CHANGE_RUNNER when runner button clicked', () => {
    render(<SessionsTab {...defaultProps} enabledModels={{ 'claude-opus-4-6': 'claude' }} />);
    const opusRow = screen.getByText('Claude Opus 4.6').closest('.model-catalog__model-row');
    const opencodeBtn = Array.from(opusRow?.querySelectorAll('.runner-picker__option') || []).find(
      (btn) => btn.textContent === 'opencode'
    );
    if (opencodeBtn) fireEvent.click(opencodeBtn);
    expect(dispatch).toHaveBeenCalledWith({
      type: 'CHANGE_RUNNER',
      modelId: 'claude-opus-4-6',
      runner: 'opencode',
    });
  });

  it('renders add command form', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.getByPlaceholderText('Name')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Command (e.g., go build ./...)')).toBeInTheDocument();
  });

  it('does not render detected targets or promptable targets sections', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.queryByText('Detected Run Targets')).not.toBeInTheDocument();
    expect(screen.queryByText('Promptable Targets')).not.toBeInTheDocument();
  });

  it('dispatches SET_FIELD for newPromptableName on typing', async () => {
    dispatch.mockClear();
    render(<SessionsTab {...defaultProps} />);
    // First "Name" placeholder belongs to the promptable targets form
    const nameInput = screen.getAllByPlaceholderText('Name')[0];
    await userEvent.type(nameInput, 'x');
    expect(dispatch).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'SET_FIELD', field: 'newPromptableName' })
    );
  });

  it('calls onRemovePromptableTarget with correct name', async () => {
    const onRemovePromptableTarget = vi.fn();
    const promptableTargets: RunTargetResponse[] = [
      { name: 'my-agent', command: 'my-agent', type: 'promptable', source: 'user' },
    ];
    render(
      <SessionsTab
        {...defaultProps}
        promptableTargets={promptableTargets}
        onRemovePromptableTarget={onRemovePromptableTarget}
      />
    );
    await userEvent.click(screen.getByText('Remove'));
    expect(onRemovePromptableTarget).toHaveBeenCalledWith('my-agent');
  });

  it('calls onOpenRunTargetEditModal when Edit is clicked', async () => {
    const onOpenRunTargetEditModal = vi.fn();
    const commandTargets: RunTargetResponse[] = [
      { name: 'build', command: 'make build', type: 'command', source: 'user' },
    ];
    render(
      <SessionsTab
        {...defaultProps}
        commandTargets={commandTargets}
        onOpenRunTargetEditModal={onOpenRunTargetEditModal}
      />
    );
    await userEvent.click(screen.getByText('Edit'));
    expect(onOpenRunTargetEditModal).toHaveBeenCalledWith(commandTargets[0]);
  });
});
