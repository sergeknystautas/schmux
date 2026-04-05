import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import AdvancedTab from './AdvancedTab';
import type { ConfigFormAction } from './useConfigForm';
import type { Model } from '../../lib/types';

const dispatch = vi.fn<(action: ConfigFormAction) => void>();

const models: Model[] = [
  {
    id: 'claude-sonnet-4-6',
    display_name: 'Claude Sonnet 4.6',
    provider: 'anthropic',
    configured: true,
    runners: ['claude'],
  },
];
const defaultProps = {
  loreEnabled: true,
  loreLLMTarget: '',
  loreCurateOnDispose: 'session',
  loreAutoPR: false,
  lorePublicRuleMode: 'direct_push',
  subredditTarget: '',
  subredditHours: 24,
  nudgenikTarget: '',
  viewedBuffer: 5000,
  nudgenikSeenInterval: 2000,
  desyncEnabled: false,
  desyncTarget: '',
  ioWorkspaceTelemetryEnabled: false,
  ioWorkspaceTelemetryTarget: '',
  branchSuggestTarget: '',
  conflictResolveTarget: '',
  soundDisabled: false,
  confirmBeforeClose: false,
  suggestDisposeAfterPush: true,
  dashboardPollInterval: 5000,
  gitStatusPollInterval: 10000,
  gitCloneTimeout: 300000,
  gitStatusTimeout: 30000,
  xtermQueryTimeout: 5000,
  xtermOperationTimeout: 10000,
  xtermUseWebGL: true,
  localEchoRemote: false,
  nudgenikTargetMissing: false,
  branchSuggestTargetMissing: false,
  conflictResolveTargetMissing: false,
  stepErrors: { 1: null, 2: null, 3: null, 4: null, 5: null, 6: null } as Record<
    number,
    string | null
  >,
  hasSaplingRepos: false,
  saplingCmdCreateWorkspace: '',
  saplingCmdRemoveWorkspace: '',
  saplingCmdCheckRepoBase: '',
  saplingCmdCreateRepoBase: '',
  tmuxBinary: '',
  timelapseEnabled: true,
  timelapseRetentionDays: 7,
  timelapseMaxFileSizeMB: 50,
  timelapseMaxTotalStorageMB: 500,
  models,
  dispatch,
};

describe('AdvancedTab', () => {
  it('renders all sections', () => {
    render(<AdvancedTab {...defaultProps} />);
    expect(screen.getByText('Lore')).toBeInTheDocument();
    expect(screen.getByText('NudgeNik')).toBeInTheDocument();
    expect(screen.getByText('Terminal Desync Diagnostics')).toBeInTheDocument();
    expect(screen.getByText('Branch Suggestion')).toBeInTheDocument();
    expect(screen.getByText('Conflict Resolution')).toBeInTheDocument();
    expect(screen.getByText('Notifications')).toBeInTheDocument();
    expect(screen.getByText('Sessions')).toBeInTheDocument();
    expect(screen.getByText('Xterm')).toBeInTheDocument();
  });

  it('renders lore checkbox checked', () => {
    render(<AdvancedTab {...defaultProps} loreEnabled={true} />);
    const checkbox = screen.getByLabelText('Enable lore system');
    expect(checkbox).toBeChecked();
  });

  it('dispatches loreEnabled toggle', async () => {
    dispatch.mockClear();
    render(<AdvancedTab {...defaultProps} loreEnabled={true} />);
    await userEvent.click(screen.getByLabelText('Enable lore system'));
    expect(dispatch).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'SET_FIELD', field: 'loreEnabled', value: false })
    );
  });

  it('renders sound notification checkbox', () => {
    render(<AdvancedTab {...defaultProps} soundDisabled={false} />);
    expect(screen.getByLabelText('Play sound when agents need attention')).toBeChecked();
  });

  it('renders confirm before close checkbox', () => {
    render(<AdvancedTab {...defaultProps} confirmBeforeClose={true} />);
    expect(screen.getByLabelText('Confirm before closing tab')).toBeChecked();
  });

  it('shows missing target warnings', () => {
    render(
      <AdvancedTab
        {...defaultProps}
        nudgenikTarget="missing"
        nudgenikTargetMissing={true}
        branchSuggestTarget="missing"
        branchSuggestTargetMissing={true}
        conflictResolveTarget="missing"
        conflictResolveTargetMissing={true}
      />
    );
    const errors = screen.getAllByText('Selected target is not available.');
    expect(errors).toHaveLength(3);
  });

  it('shows step 5 error when present', () => {
    render(
      <AdvancedTab
        {...defaultProps}
        stepErrors={{ ...defaultProps.stepErrors, 5: 'xterm error' }}
      />
    );
    expect(screen.getByText('xterm error')).toBeInTheDocument();
  });

  it('renders desync checkbox unchecked by default', () => {
    render(<AdvancedTab {...defaultProps} />);
    expect(screen.getByLabelText('Enable terminal desync diagnostics')).not.toBeChecked();
  });

  it('dispatches desyncEnabled toggle', async () => {
    dispatch.mockClear();
    render(<AdvancedTab {...defaultProps} desyncEnabled={false} />);
    await userEvent.click(screen.getByLabelText('Enable terminal desync diagnostics'));
    expect(dispatch).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'SET_FIELD', field: 'desyncEnabled', value: true })
    );
  });

  it('dispatches loreCurateOnDispose change', async () => {
    dispatch.mockClear();
    render(<AdvancedTab {...defaultProps} loreCurateOnDispose="session" />);
    await userEvent.selectOptions(screen.getByDisplayValue('Every session'), 'never');
    expect(dispatch).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'SET_FIELD', field: 'loreCurateOnDispose', value: 'never' })
    );
  });

  it('dispatches dashboardPollInterval on typing', async () => {
    dispatch.mockClear();
    render(<AdvancedTab {...defaultProps} dashboardPollInterval={5000} />);
    // Multiple inputs share value "5000" — use the label to find the right one
    const label = screen.getByText('Dashboard Poll Interval (ms)');
    const input = label.closest('.form-group')!.querySelector('input')!;
    await userEvent.clear(input);
    await userEvent.type(input, '3000');
    expect(dispatch).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'SET_FIELD', field: 'dashboardPollInterval' })
    );
  });
});
