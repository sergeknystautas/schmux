import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
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

// AccessTab vendor-locked behavior is enforced at the wizard level
// (ConfigPage drops the Access tab from navigation when features.vendor_locked
// is true, see ConfigPage.test.tsx). AccessTab itself stays vendor-agnostic.
