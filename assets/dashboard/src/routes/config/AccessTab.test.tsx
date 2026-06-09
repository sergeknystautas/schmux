import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import AccessTab from './AccessTab';
import type { ConfigFormAction } from './useConfigForm';

const mockFeatures: Record<string, boolean> = {
  tunnel: true,
  github: true,
  telemetry: true,
  update: true,
  dashboardsx: true,
  model_registry: true,
  repofeed: true,
  subreddit: true,
  personas: true,
  comm_styles: true,
  autolearn: true,
  floor_manager: true,
  timelapse: true,
  vendor_locked: false,
};

vi.mock('../../contexts/FeaturesContext', () => ({
  useFeatures: () => ({ features: mockFeatures, loading: false }),
}));

const dispatch = vi.fn<(action: ConfigFormAction) => void>();
const noop = () => {};

const baseProps = {
  networkAccess: true,
  remoteAccessEnabled: false,
  remoteAccessPasswordHashSet: false,
  passwordInput: '',
  passwordConfirm: '',
  passwordSaving: false,
  passwordError: '',
  passwordSuccess: '',
  remoteAccessTimeoutMinutes: 0,
  remoteAccessNtfyTopic: '',
  remoteAccessNotifyCommand: '',
  authEnabled: false,
  authPublicBaseURL: '',
  authTlsCertPath: '',
  authTlsKeyPath: '',
  authSessionTTLMinutes: 1440,
  authClientIdSet: false,
  authClientSecretSet: false,
  combinedAuthWarnings: [],
  httpsEnabled: false,
  tlsHostname: '',
  tlsExpires: '',
  tlsModalCertPath: '',
  tlsModalKeyPath: '',
  tlsModalValidating: false,
  tlsModalHostname: '',
  tlsModalExpires: '',
  tlsModalError: '',
  dispatch,
  onSetPassword: noop,
  onOpenAuthSecretsModal: noop,
  onOpenTlsModal: noop,
  onDisableAuth: noop,
  success: noop,
  toastError: noop,
};

describe('AccessTab feature gating', () => {
  beforeEach(() => {
    mockFeatures.github = true;
    mockFeatures.tunnel = true;
    mockFeatures.vendor_locked = false;
    dispatch.mockClear();
  });

  it('renders GitHub Authentication and Remote Access when both features are available', () => {
    render(<AccessTab {...baseProps} />);
    expect(screen.getByTestId('config-section-github-auth')).toBeInTheDocument();
    expect(screen.getByTestId('config-section-remote-access')).toBeInTheDocument();
  });

  it('hides GitHub Authentication when github feature is compiled out', () => {
    mockFeatures.github = false;
    render(<AccessTab {...baseProps} />);
    expect(screen.queryByTestId('config-section-github-auth')).not.toBeInTheDocument();
    // Remote Access still present
    expect(screen.getByTestId('config-section-remote-access')).toBeInTheDocument();
  });

  it('hides Remote Access when tunnel feature is compiled out', () => {
    mockFeatures.tunnel = false;
    render(<AccessTab {...baseProps} />);
    expect(screen.queryByTestId('config-section-remote-access')).not.toBeInTheDocument();
    // GitHub Auth still present
    expect(screen.getByTestId('config-section-github-auth')).toBeInTheDocument();
  });

  it('hides both sections when both features are compiled out', () => {
    mockFeatures.github = false;
    mockFeatures.tunnel = false;
    render(<AccessTab {...baseProps} />);
    expect(screen.queryByTestId('config-section-github-auth')).not.toBeInTheDocument();
    expect(screen.queryByTestId('config-section-remote-access')).not.toBeInTheDocument();
    // Network and HTTPS sections remain (the tab still has content)
    expect(screen.getByText('Network')).toBeInTheDocument();
    expect(screen.getByText('HTTPS')).toBeInTheDocument();
  });

  it('omits the GitHub authentication mention from the HTTPS hint when github is compiled out', () => {
    mockFeatures.github = false;
    render(<AccessTab {...baseProps} networkAccess={true} />);
    expect(screen.queryByText(/Required for GitHub authentication/i)).not.toBeInTheDocument();
  });
});

describe('AccessTab inline warnings', () => {
  beforeEach(() => {
    dispatch.mockClear();
  });

  it('shows TLS warning when network access enabled without TLS', () => {
    render(<AccessTab {...baseProps} networkAccess={true} httpsEnabled={false} />);
    expect(screen.getByText(/without TLS exposes traffic/)).toBeInTheDocument();
  });

  it('shows auth warning when network access enabled without auth', () => {
    render(<AccessTab {...baseProps} networkAccess={true} authEnabled={false} />);
    expect(screen.getByText(/without authentication allows/)).toBeInTheDocument();
  });

  it('does not show warnings when local access only', () => {
    render(<AccessTab {...baseProps} networkAccess={false} />);
    expect(screen.queryByText(/without TLS exposes traffic/)).not.toBeInTheDocument();
    expect(screen.queryByText(/without authentication allows/)).not.toBeInTheDocument();
  });

  it('does not show TLS warning when HTTPS is enabled', () => {
    render(<AccessTab {...baseProps} networkAccess={true} httpsEnabled={true} />);
    expect(screen.queryByText(/without TLS exposes traffic/)).not.toBeInTheDocument();
  });

  it('does not show auth warning when auth is enabled', () => {
    render(<AccessTab {...baseProps} networkAccess={true} authEnabled={true} />);
    expect(screen.queryByText(/without authentication allows/)).not.toBeInTheDocument();
  });
});

// AccessTab vendor-locked behavior is enforced at the wizard level
// (ConfigPage drops the Access tab from navigation when features.vendor_locked
// is true, see ConfigPage.test.tsx). AccessTab itself stays vendor-agnostic.

describe('AccessTab auth enable/disable actions', () => {
  beforeEach(() => {
    mockFeatures.github = true;
    mockFeatures.tunnel = true;
    mockFeatures.vendor_locked = false;
    dispatch.mockClear();
  });

  it('shows "Set up & enable…" and no checkbox when auth is off', () => {
    render(<AccessTab {...baseProps} authEnabled={false} httpsEnabled={true} />);
    expect(screen.getByRole('button', { name: /set up & enable/i })).toBeInTheDocument();
    expect(screen.queryByLabelText(/enable github authentication/i)).not.toBeInTheDocument();
  });

  it('shows Disable + Update credentials when auth is on', () => {
    render(
      <AccessTab
        {...baseProps}
        authEnabled={true}
        httpsEnabled={true}
        authClientIdSet={true}
        authClientSecretSet={true}
      />
    );
    expect(screen.getByRole('button', { name: /disable/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /update credentials/i })).toBeInTheDocument();
  });

  it('calls onDisableAuth when Disable is clicked', () => {
    const onDisableAuth = vi.fn();
    render(
      <AccessTab
        {...baseProps}
        authEnabled={true}
        httpsEnabled={true}
        onDisableAuth={onDisableAuth}
      />
    );
    fireEvent.click(screen.getByRole('button', { name: /disable/i }));
    expect(onDisableAuth).toHaveBeenCalledTimes(1);
  });
});
