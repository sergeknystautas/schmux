import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import SessionsTab from './SessionsTab';
import type { ConfigFormAction } from './useConfigForm';
import type { Model, RunTargetResponse } from '../../lib/types';

const dispatch = vi.fn<(action: ConfigFormAction) => void>();

const detectedTargets: RunTargetResponse[] = [
  { name: 'claude', command: 'claude', type: 'promptable', source: 'detected' },
];

const models: Model[] = [
  {
    id: 'gpt-4',
    display_name: 'GPT-4',
    base_tool: 'openai',
    provider: 'openai',
    category: 'external',
    required_secrets: ['OPENAI_API_KEY'],
    configured: false,
    default_value: 'gpt-4',
  },
];

const defaultProps = {
  detectedTargets,
  models,
  promptableTargets: [] as RunTargetResponse[],
  commandTargets: [] as RunTargetResponse[],
  newPromptableName: '',
  newPromptableCommand: '',
  newCommandName: '',
  newCommandCommand: '',
  dispatch,
  onAddPromptableTarget: vi.fn(),
  onRemovePromptableTarget: vi.fn(),
  onAddCommand: vi.fn(),
  onRemoveCommand: vi.fn(),
  onModelAction: vi.fn(),
  onOpenRunTargetEditModal: vi.fn(),
};

describe('SessionsTab', () => {
  it('renders detected targets', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.getAllByText('claude').length).toBeGreaterThanOrEqual(1);
  });

  it('renders models with Add Secrets button when not configured', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.getByText('GPT-4')).toBeInTheDocument();
    expect(screen.getByText('Add Secrets')).toBeInTheDocument();
  });

  it('renders models with Update/Remove when configured', () => {
    const configuredModels: Model[] = [{ ...models[0], configured: true }];
    render(<SessionsTab {...defaultProps} models={configuredModels} />);
    expect(screen.getByText('Update')).toBeInTheDocument();
    expect(screen.getByText('Remove')).toBeInTheDocument();
  });

  it('renders promptable targets with Edit and Remove buttons', () => {
    const promptableTargets: RunTargetResponse[] = [
      { name: 'custom-agent', command: 'my-agent', type: 'promptable', source: 'user' },
    ];
    render(<SessionsTab {...defaultProps} promptableTargets={promptableTargets} />);
    expect(screen.getByText('custom-agent')).toBeInTheDocument();
    expect(screen.getByText('Edit')).toBeInTheDocument();
  });

  it('renders command targets', () => {
    const commandTargets: RunTargetResponse[] = [
      { name: 'build', command: 'make build', type: 'command', source: 'user' },
    ];
    render(<SessionsTab {...defaultProps} commandTargets={commandTargets} />);
    expect(screen.getByText('build')).toBeInTheDocument();
    expect(screen.getByText('make build')).toBeInTheDocument();
  });

  it('calls onAddPromptableTarget when Add button is clicked', async () => {
    const onAddPromptableTarget = vi.fn();
    render(<SessionsTab {...defaultProps} onAddPromptableTarget={onAddPromptableTarget} />);
    await userEvent.click(screen.getByTestId('add-target'));
    expect(onAddPromptableTarget).toHaveBeenCalled();
  });

  it('calls onModelAction when Add Secrets is clicked', async () => {
    const onModelAction = vi.fn();
    render(<SessionsTab {...defaultProps} onModelAction={onModelAction} />);
    await userEvent.click(screen.getByText('Add Secrets'));
    expect(onModelAction).toHaveBeenCalledWith(models[0], 'add');
  });

  it('shows empty state when no detected targets', () => {
    render(<SessionsTab {...defaultProps} detectedTargets={[]} />);
    expect(screen.getByText(/No detected run targets/)).toBeInTheDocument();
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
