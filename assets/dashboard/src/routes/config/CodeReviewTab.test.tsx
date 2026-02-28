import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import CodeReviewTab from './CodeReviewTab';
import type { ConfigFormAction } from './useConfigForm';
import type { Model, RunTargetResponse } from '../../lib/types';

const dispatch = vi.fn<(action: ConfigFormAction) => void>();

const detectedTargets: RunTargetResponse[] = [
  { name: 'claude', command: 'claude', type: 'promptable', source: 'detected' },
];
const models: Model[] = [];
const defaultProps = {
  commitMessageTarget: '',
  prReviewTarget: '',
  externalDiffCommands: [] as { name: string; command: string }[],
  externalDiffCleanupMinutes: 60,
  newDiffName: '',
  newDiffCommand: '',
  commitMessageTargetMissing: false,
  prReviewTargetMissing: false,
  detectedTargets,
  models,
  dispatch,
  onAddDiffCommand: vi.fn(),
};

describe('CodeReviewTab', () => {
  it('renders commit message and PR review sections', () => {
    render(<CodeReviewTab {...defaultProps} />);
    expect(screen.getByText('Commit Message')).toBeInTheDocument();
    expect(screen.getByText('PR Review')).toBeInTheDocument();
  });

  it('renders built-in diff options', () => {
    render(<CodeReviewTab {...defaultProps} />);
    expect(screen.getByText('VS Code')).toBeInTheDocument();
    expect(screen.getByText('Web view')).toBeInTheDocument();
  });

  it('renders custom diff tools', () => {
    render(
      <CodeReviewTab
        {...defaultProps}
        externalDiffCommands={[{ name: 'Kaleidoscope', command: 'ksdiff' }]}
      />
    );
    expect(screen.getByText('Kaleidoscope')).toBeInTheDocument();
    expect(screen.getByText('ksdiff')).toBeInTheDocument();
  });

  it('shows empty state for custom diff tools', () => {
    render(<CodeReviewTab {...defaultProps} />);
    expect(screen.getByText('No custom diff tools configured.')).toBeInTheDocument();
  });

  it('shows missing target warnings', () => {
    render(
      <CodeReviewTab
        {...defaultProps}
        commitMessageTarget="missing"
        commitMessageTargetMissing={true}
        prReviewTarget="missing"
        prReviewTargetMissing={true}
      />
    );
    const errors = screen.getAllByText('Selected target is not available or not promptable.');
    expect(errors).toHaveLength(2);
  });

  it('calls onAddDiffCommand when Add Diff Tool is clicked', async () => {
    const onAddDiffCommand = vi.fn();
    render(
      <CodeReviewTab
        {...defaultProps}
        newDiffName="MyTool"
        newDiffCommand="mytool"
        onAddDiffCommand={onAddDiffCommand}
      />
    );
    await userEvent.click(screen.getByText('Add Diff Tool'));
    expect(onAddDiffCommand).toHaveBeenCalled();
  });

  it('removes a diff command via dispatch', async () => {
    dispatch.mockClear();
    render(
      <CodeReviewTab
        {...defaultProps}
        externalDiffCommands={[{ name: 'ksdiff', command: 'ksdiff' }]}
      />
    );
    await userEvent.click(screen.getByText('Remove'));
    expect(dispatch).toHaveBeenCalledWith({ type: 'REMOVE_DIFF_COMMAND', name: 'ksdiff' });
  });

  it('dispatches SET_FIELD when cleanup minutes changes', async () => {
    dispatch.mockClear();
    render(<CodeReviewTab {...defaultProps} />);
    const input = screen.getByDisplayValue('60');
    await userEvent.clear(input);
    await userEvent.type(input, '30');
    expect(dispatch).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'SET_FIELD', field: 'externalDiffCleanupMinutes' })
    );
  });

  it('dispatches SET_FIELD for newDiffName on typing', async () => {
    dispatch.mockClear();
    render(<CodeReviewTab {...defaultProps} />);
    const nameInput = screen.getByPlaceholderText('e.g., Kaleidoscope');
    await userEvent.type(nameInput, 'K');
    expect(dispatch).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'SET_FIELD', field: 'newDiffName' })
    );
  });

  it('disables Add Diff Tool button when inputs are empty', () => {
    render(<CodeReviewTab {...defaultProps} newDiffName="" newDiffCommand="" />);
    expect(screen.getByText('Add Diff Tool')).toBeDisabled();
  });
});
