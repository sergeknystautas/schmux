import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import CreateActionForm from './CreateActionForm';

// Mock emergence API
const mockCreateSpawnEntry = vi.fn().mockResolvedValue(undefined);
vi.mock('../lib/emergence-api', () => ({
  createSpawnEntry: (...args: unknown[]) => mockCreateSpawnEntry(...args),
}));

// Mock toast
const mockSuccess = vi.fn();
vi.mock('./ToastProvider', () => ({
  useToast: () => ({ success: mockSuccess }),
}));

// Mock modal
const mockAlert = vi.fn();
vi.mock('./ModalProvider', () => ({
  useModal: () => ({ alert: mockAlert }),
}));

describe('CreateActionForm', () => {
  const defaultProps = {
    repo: 'my-repo',
    onCreated: vi.fn(),
    onCancel: vi.fn(),
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders name input and type radios', () => {
    render(<CreateActionForm {...defaultProps} />);

    expect(screen.getByLabelText('Name')).toBeInTheDocument();
    expect(screen.getByLabelText('Shell command')).toBeChecked();
    expect(screen.getByLabelText('Agent session')).not.toBeChecked();
  });

  it('shows command input when Shell command is selected', () => {
    render(<CreateActionForm {...defaultProps} />);

    expect(screen.getByLabelText('Command')).toBeInTheDocument();
    expect(screen.queryByLabelText('Prompt')).not.toBeInTheDocument();
  });

  it('shows prompt and target inputs when Agent session is selected', async () => {
    const user = userEvent.setup();
    render(<CreateActionForm {...defaultProps} />);

    await user.click(screen.getByLabelText('Agent session'));

    expect(screen.getByLabelText('Prompt')).toBeInTheDocument();
    expect(screen.getByLabelText('Target (optional)')).toBeInTheDocument();
    expect(screen.queryByLabelText('Command')).not.toBeInTheDocument();
  });

  it('disables Save when name is empty', () => {
    render(<CreateActionForm {...defaultProps} />);

    expect(screen.getByText('Save')).toBeDisabled();
  });

  it('enables Save when name is provided', async () => {
    const user = userEvent.setup();
    render(<CreateActionForm {...defaultProps} />);

    await user.type(screen.getByLabelText('Name'), 'Run tests');

    expect(screen.getByText('Save')).toBeEnabled();
  });

  it('calls createSpawnEntry with command type on submit', async () => {
    const user = userEvent.setup();
    const onCreated = vi.fn();
    render(<CreateActionForm {...defaultProps} onCreated={onCreated} />);

    await user.type(screen.getByLabelText('Name'), 'Build project');
    await user.type(screen.getByLabelText('Command'), 'go build ./...');
    await user.click(screen.getByText('Save'));

    expect(mockCreateSpawnEntry).toHaveBeenCalledWith('my-repo', {
      name: 'Build project',
      type: 'command',
      command: 'go build ./...',
      prompt: undefined,
      target: undefined,
    });
    expect(mockSuccess).toHaveBeenCalledWith('Created "Build project"');
    expect(onCreated).toHaveBeenCalled();
  });

  it('calls createSpawnEntry with agent type on submit', async () => {
    const user = userEvent.setup();
    const onCreated = vi.fn();
    render(<CreateActionForm {...defaultProps} onCreated={onCreated} />);

    await user.type(screen.getByLabelText('Name'), 'Fix lint');
    await user.click(screen.getByLabelText('Agent session'));
    await user.type(screen.getByLabelText('Prompt'), 'Fix all lint errors');
    await user.type(screen.getByLabelText('Target (optional)'), 'claude-code');
    await user.click(screen.getByText('Save'));

    expect(mockCreateSpawnEntry).toHaveBeenCalledWith('my-repo', {
      name: 'Fix lint',
      type: 'agent',
      command: undefined,
      prompt: 'Fix all lint errors',
      target: 'claude-code',
    });
    expect(onCreated).toHaveBeenCalled();
  });

  it('calls onCancel when Cancel is clicked', async () => {
    const user = userEvent.setup();
    const onCancel = vi.fn();
    render(<CreateActionForm {...defaultProps} onCancel={onCancel} />);

    await user.click(screen.getByText('Cancel'));

    expect(onCancel).toHaveBeenCalled();
  });

  it('shows error dialog on API failure', async () => {
    mockCreateSpawnEntry.mockRejectedValueOnce(new Error('Network error'));
    const user = userEvent.setup();
    render(<CreateActionForm {...defaultProps} />);

    await user.type(screen.getByLabelText('Name'), 'Failing action');
    await user.click(screen.getByText('Save'));

    expect(mockAlert).toHaveBeenCalledWith('Save Failed', 'Network error');
    expect(defaultProps.onCreated).not.toHaveBeenCalled();
  });
});
