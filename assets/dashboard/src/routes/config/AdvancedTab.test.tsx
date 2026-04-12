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
  desyncEnabled: false,
  desyncTarget: '',
  ioWorkspaceTelemetryEnabled: false,
  ioWorkspaceTelemetryTarget: '',
  dashboardPollInterval: 5000,
  gitStatusPollInterval: 10000,
  gitCloneTimeout: 300000,
  gitStatusTimeout: 30000,
  xtermQueryTimeout: 5000,
  xtermOperationTimeout: 10000,
  xtermUseWebGL: true,
  localEchoRemote: false,
  debugUI: false,
  hasSaplingRepos: false,
  saplingCmdCreateWorkspace: '',
  saplingCmdRemoveWorkspace: '',
  saplingCmdCheckRepoBase: '',
  saplingCmdCreateRepoBase: '',
  tmuxBinary: '',
  tmuxSocketName: '',
  externalDiffCommands: [] as { name: string; command: string }[],
  externalDiffCleanupMinutes: 60,
  newDiffName: '',
  newDiffCommand: '',
  onAddDiffCommand: vi.fn(),
  models,
  dispatch,
};

describe('AdvancedTab', () => {
  it('renders core sections', () => {
    render(<AdvancedTab {...defaultProps} />);
    expect(screen.getByText('Sessions')).toBeInTheDocument();
    expect(screen.getByText('Xterm')).toBeInTheDocument();
    expect(screen.getByText('Custom Diff Tools')).toBeInTheDocument();
    expect(screen.getByText('Temp Cleanup')).toBeInTheDocument();
    // Dev-only sections hidden when debugUI is false
    expect(screen.queryByText('Terminal Desync Diagnostics')).not.toBeInTheDocument();
    expect(screen.queryByText('IO Workspace Telemetry')).not.toBeInTheDocument();
  });

  it('renders dev-only sections when debugUI is true', () => {
    render(<AdvancedTab {...defaultProps} debugUI={true} />);
    expect(screen.getByText('Terminal Desync Diagnostics')).toBeInTheDocument();
    expect(screen.getByText('IO Workspace Telemetry')).toBeInTheDocument();
  });

  it('does not render Branch Suggestion or Conflict Resolution', () => {
    render(<AdvancedTab {...defaultProps} />);
    expect(screen.queryByText('Branch Suggestion')).not.toBeInTheDocument();
    expect(screen.queryByText('Conflict Resolution')).not.toBeInTheDocument();
  });

  it('renders desync checkbox unchecked by default', () => {
    render(<AdvancedTab {...defaultProps} debugUI={true} />);
    expect(screen.getByLabelText('Enable terminal desync diagnostics')).not.toBeChecked();
  });

  it('dispatches desyncEnabled toggle', async () => {
    dispatch.mockClear();
    render(<AdvancedTab {...defaultProps} debugUI={true} desyncEnabled={false} />);
    await userEvent.click(screen.getByLabelText('Enable terminal desync diagnostics'));
    expect(dispatch).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'SET_FIELD', field: 'desyncEnabled', value: true })
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

  it('renders diff tools when provided', () => {
    render(
      <AdvancedTab
        {...defaultProps}
        externalDiffCommands={[{ name: 'Kaleidoscope', command: 'ksdiff' }]}
      />
    );
    expect(screen.getByText('Kaleidoscope')).toBeInTheDocument();
    expect(screen.getByText('ksdiff')).toBeInTheDocument();
  });
});
