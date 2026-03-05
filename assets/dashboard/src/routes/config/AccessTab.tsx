import React from 'react';
import { NtfyTopicGenerateButton, NtfyTopicQRDisplay } from '../../components/NtfyTopicGenerator';
import { passwordStrength } from '../../lib/passwordStrength';
import type { ConfigFormAction } from './useConfigForm';
import { getErrorMessage, testRemoteAccessNotification } from '../../lib/api';

type AccessTabProps = {
  networkAccess: boolean;
  remoteAccessEnabled: boolean;
  remoteAccessPasswordHashSet: boolean;
  passwordInput: string;
  passwordConfirm: string;
  passwordSaving: boolean;
  passwordError: string;
  passwordSuccess: string;
  remoteAccessTimeoutMinutes: number;
  remoteAccessNtfyTopic: string;
  remoteAccessNotifyCommand: string;
  authEnabled: boolean;
  authPublicBaseURL: string;
  authTlsCertPath: string;
  authTlsKeyPath: string;
  authSessionTTLMinutes: number;
  authClientIdSet: boolean;
  authClientSecretSet: boolean;
  combinedAuthWarnings: string[];
  // HTTPS state
  httpsEnabled: boolean;
  tlsHostname: string;
  tlsExpires: string;
  tlsModalCertPath: string;
  tlsModalKeyPath: string;
  tlsModalValidating: boolean;
  tlsModalHostname: string;
  tlsModalExpires: string;
  tlsModalError: string;
  dispatch: React.Dispatch<ConfigFormAction>;
  onSetPassword: () => void;
  onOpenAuthSecretsModal: () => void;
  onOpenTlsModal: () => void;
  success: (msg: string) => void;
  toastError: (msg: string) => void;
};

export default function AccessTab({
  networkAccess,
  remoteAccessEnabled,
  remoteAccessPasswordHashSet,
  passwordInput,
  passwordConfirm,
  passwordSaving,
  passwordError,
  passwordSuccess,
  remoteAccessTimeoutMinutes,
  remoteAccessNtfyTopic,
  remoteAccessNotifyCommand,
  authEnabled,
  authPublicBaseURL,
  authTlsCertPath,
  authTlsKeyPath,
  authSessionTTLMinutes,
  authClientIdSet,
  authClientSecretSet,
  combinedAuthWarnings,
  httpsEnabled,
  tlsHostname,
  tlsExpires,
  onOpenTlsModal,
  success,
  toastError,
  dispatch,
  onSetPassword,
  onOpenAuthSecretsModal,
}: AccessTabProps) {
  return (
    <div className="wizard-step-content" data-step="5" data-testid="config-tab-content-access">
      <h2 className="wizard-step-content__title">Access</h2>
      <p className="wizard-step-content__description">
        Control how the dashboard is accessed — local network, HTTPS, and authentication.
      </p>

      {/* Network Section */}
      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Network</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="form-group__label">Dashboard Access</label>
            <div
              style={{
                display: 'flex',
                gap: 'var(--spacing-md)',
                alignItems: 'center',
                fontSize: '0.9rem',
              }}
            >
              <label
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 'var(--spacing-xs)',
                  cursor: 'pointer',
                  fontSize: 'inherit',
                }}
              >
                <input
                  type="radio"
                  name="networkAccess"
                  checked={!networkAccess}
                  onChange={() =>
                    dispatch({ type: 'SET_FIELD', field: 'networkAccess', value: false })
                  }
                />
                <span>Local access only</span>
              </label>
              <label
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 'var(--spacing-xs)',
                  cursor: 'pointer',
                  fontSize: 'inherit',
                }}
              >
                <input
                  type="radio"
                  name="networkAccess"
                  checked={networkAccess}
                  onChange={() =>
                    dispatch({ type: 'SET_FIELD', field: 'networkAccess', value: true })
                  }
                />
                <span>Local network access</span>
              </label>
            </div>
            <p className="form-group__hint">
              {!networkAccess
                ? 'Dashboard accessible only from this computer (localhost).'
                : 'Dashboard accessible from other devices on your local network.'}
            </p>
          </div>
        </div>
      </div>

      {/* HTTPS Section - Always visible, greyed when disabled */}
      <div
        className="settings-section"
        style={{
          opacity: networkAccess ? 1 : 0.5,
          pointerEvents: networkAccess ? 'auto' : 'none',
        }}
      >
        <div className="settings-section__header">
          <h3 className="settings-section__title">HTTPS</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--spacing-xs)',
                cursor: networkAccess ? 'pointer' : 'not-allowed',
              }}
            >
              <input
                type="checkbox"
                checked={httpsEnabled}
                onChange={(e) => {
                  if (e.target.checked) {
                    onOpenTlsModal();
                  } else {
                    dispatch({ type: 'SET_FIELD', field: 'authTlsCertPath', value: '' });
                    dispatch({ type: 'SET_FIELD', field: 'authTlsKeyPath', value: '' });
                    dispatch({ type: 'SET_FIELD', field: 'authPublicBaseURL', value: '' });
                  }
                }}
                disabled={!networkAccess}
              />
              <span>Enable HTTPS</span>
            </label>
            <p className="form-group__hint">
              {networkAccess
                ? 'Secure connections with TLS certificates. Required for GitHub authentication.'
                : 'Requires local network access to be enabled first.'}
            </p>
          </div>

          {/* Certificate status and configure button - always visible */}
          <div className="form-group">
            <label className="form-group__label">TLS Certificate</label>
            <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-md)' }}>
              <span style={{ fontSize: '0.9rem', color: 'var(--color-text-secondary)' }}>
                {httpsEnabled ? (
                  tlsHostname ? (
                    <>
                      <span style={{ color: 'var(--color-success)' }}>Valid</span> for{' '}
                      <code>{tlsHostname}</code>
                      {tlsExpires && <span> (expires {tlsExpires})</span>}
                    </>
                  ) : (
                    <span style={{ color: 'var(--color-success)' }}>Configured</span>
                  )
                ) : (
                  <span style={{ color: 'var(--color-text-tertiary)' }}>Not configured</span>
                )}
              </span>
              <button
                type="button"
                className="btn btn--secondary btn--sm"
                onClick={onOpenTlsModal}
                disabled={!networkAccess}
              >
                {httpsEnabled ? 'Update' : 'Configure'}
              </button>
            </div>
          </div>

          {/* Dashboard URL - only show when HTTPS is enabled */}
          {httpsEnabled && authPublicBaseURL && (
            <div className="form-group">
              <label className="form-group__label">Dashboard URL</label>
              <code style={{ fontSize: '0.9rem' }}>{authPublicBaseURL}</code>
              <p className="form-group__hint">
                Share this URL to access schmux from other devices on your network.
              </p>
            </div>
          )}
        </div>
      </div>

      {/* GitHub Auth Section - Always visible, greyed when HTTPS not enabled */}
      <div
        className="settings-section"
        style={{
          opacity: httpsEnabled ? 1 : 0.5,
          pointerEvents: httpsEnabled ? 'auto' : 'none',
        }}
      >
        <div className="settings-section__header">
          <h3 className="settings-section__title">GitHub Authentication</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--spacing-xs)',
                cursor: httpsEnabled ? 'pointer' : 'not-allowed',
              }}
            >
              <input
                type="checkbox"
                checked={authEnabled}
                onChange={(e) =>
                  dispatch({ type: 'SET_FIELD', field: 'authEnabled', value: e.target.checked })
                }
                disabled={!httpsEnabled}
              />
              <span>Enable GitHub authentication</span>
            </label>
            <p className="form-group__hint">
              {httpsEnabled
                ? 'Require GitHub login to access the dashboard.'
                : 'Requires HTTPS to be enabled first.'}
            </p>
          </div>

          {/* Auth fields - always visible when auth is checked */}
          <div
            style={{
              opacity: authEnabled ? 1 : 0.5,
              pointerEvents: authEnabled ? 'auto' : 'none',
            }}
          >
            <div className="form-group">
              <label className="form-group__label">Session TTL (minutes)</label>
              <input
                type="number"
                className="input input--compact"
                style={{ maxWidth: '120px' }}
                min="1"
                value={authSessionTTLMinutes}
                onChange={(e) =>
                  dispatch({
                    type: 'SET_FIELD',
                    field: 'authSessionTTLMinutes',
                    value: parseInt(e.target.value) || 1440,
                  })
                }
                disabled={!authEnabled}
              />
              <p className="form-group__hint">How long before requiring re-authentication.</p>
            </div>

            <div className="form-group">
              <label className="form-group__label">GitHub OAuth Credentials</label>
              <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-md)' }}>
                <span style={{ fontSize: '0.9rem', color: 'var(--color-text-secondary)' }}>
                  {authClientIdSet && authClientSecretSet ? (
                    <span style={{ color: 'var(--color-success)' }}>Configured</span>
                  ) : (
                    <span style={{ color: 'var(--color-warning)' }}>Not configured</span>
                  )}
                </span>
                <button
                  type="button"
                  className="btn btn--secondary btn--sm"
                  onClick={onOpenAuthSecretsModal}
                  disabled={!authEnabled}
                >
                  {authClientIdSet && authClientSecretSet ? 'Update' : 'Configure'}
                </button>
              </div>
            </div>

            {combinedAuthWarnings.length > 0 && (
              <div className="form-group">
                <p className="form-group__error">Configuration issues:</p>
                <ul className="form-group__hint" style={{ color: 'var(--color-error)' }}>
                  {combinedAuthWarnings.map((item) => (
                    <li key={item}>{item}</li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Separator */}
      <hr
        style={{
          border: 'none',
          borderTop: '1px solid var(--color-border)',
          margin: 'var(--spacing-lg) 0',
        }}
      />

      {/* Remote Access Section - Independent, always at bottom */}
      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Remote Access</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--spacing-xs)',
                cursor: 'pointer',
              }}
            >
              <input
                type="checkbox"
                checked={remoteAccessEnabled}
                onChange={(e) =>
                  dispatch({
                    type: 'SET_FIELD',
                    field: 'remoteAccessEnabled',
                    value: e.target.checked,
                  })
                }
              />
              <span>Enable remote access</span>
            </label>
            <p className="form-group__hint">
              Allow accessing the dashboard remotely via a Cloudflare tunnel.
            </p>
          </div>

          <div
            style={{
              opacity: remoteAccessEnabled ? 1 : 0.5,
              pointerEvents: remoteAccessEnabled ? 'auto' : 'none',
            }}
          >
            {remoteAccessEnabled && (
              <div className="remote-access-grid">
                <div className="remote-access-grid__fields">
                  <div className="form-group" data-testid="form-group-access-password">
                    <label className="form-group__label" htmlFor="access-password">
                      Access Password
                    </label>
                    <div
                      style={{
                        display: 'flex',
                        flexDirection: 'column',
                        gap: 'var(--spacing-xs)',
                      }}
                    >
                      {remoteAccessPasswordHashSet && (
                        <p className="form-group__hint" style={{ color: 'var(--color-success)' }}>
                          Password is configured
                        </p>
                      )}
                      <input
                        type="password"
                        className="input"
                        placeholder={
                          remoteAccessPasswordHashSet
                            ? 'New password (leave blank to keep)'
                            : 'Enter password'
                        }
                        value={passwordInput}
                        onChange={(e) =>
                          dispatch({
                            type: 'SET_FIELD',
                            field: 'passwordInput',
                            value: e.target.value,
                          })
                        }
                      />
                      {passwordInput && passwordInput.length >= 6 && (
                        <span
                          className={`password-strength password-strength--${passwordStrength(passwordInput)}`}
                          data-testid="password-strength"
                        >
                          {passwordStrength(passwordInput) === 'weak'
                            ? 'Weak password'
                            : passwordStrength(passwordInput) === 'ok'
                              ? 'OK'
                              : 'Strong'}
                        </span>
                      )}
                      {passwordInput && (
                        <input
                          type="password"
                          className="input"
                          placeholder="Confirm password"
                          value={passwordConfirm}
                          onChange={(e) =>
                            dispatch({
                              type: 'SET_FIELD',
                              field: 'passwordConfirm',
                              value: e.target.value,
                            })
                          }
                        />
                      )}
                      {passwordInput && (
                        <button
                          type="button"
                          className="btn btn--primary"
                          style={{ alignSelf: 'flex-start' }}
                          onClick={onSetPassword}
                          disabled={passwordSaving}
                        >
                          {passwordSaving
                            ? 'Saving...'
                            : remoteAccessPasswordHashSet
                              ? 'Update Password'
                              : 'Set Password'}
                        </button>
                      )}
                      {passwordError && (
                        <p className="form-group__error" data-testid="password-error">
                          {passwordError}
                        </p>
                      )}
                      {passwordSuccess && (
                        <p className="form-group__hint" style={{ color: 'var(--color-success)' }}>
                          {passwordSuccess}
                        </p>
                      )}
                    </div>
                    <p className="form-group__hint">
                      Required to start a remote tunnel. You&apos;ll enter this password when
                      connecting from another device.
                    </p>
                  </div>

                  <div className="form-group" data-testid="form-group-timeout">
                    <label className="form-group__label" htmlFor="remote-timeout">
                      Timeout (minutes)
                    </label>
                    <input
                      id="remote-timeout"
                      type="number"
                      className="input input--compact"
                      style={{ maxWidth: '120px' }}
                      min="0"
                      value={remoteAccessTimeoutMinutes}
                      onChange={(e) =>
                        dispatch({
                          type: 'SET_FIELD',
                          field: 'remoteAccessTimeoutMinutes',
                          value: parseInt(e.target.value) || 0,
                        })
                      }
                    />
                    <p className="form-group__hint">
                      Auto-stop the tunnel after this many minutes. 0 means no timeout.
                    </p>
                  </div>

                  <div className="form-group" data-testid="form-group-ntfy-topic">
                    <label className="form-group__label" htmlFor="ntfy-topic">
                      ntfy Topic
                    </label>
                    <input
                      id="ntfy-topic"
                      type="text"
                      className="input"
                      placeholder="my-schmux-notifications"
                      value={remoteAccessNtfyTopic}
                      onChange={(e) =>
                        dispatch({
                          type: 'SET_FIELD',
                          field: 'remoteAccessNtfyTopic',
                          value: e.target.value,
                        })
                      }
                    />
                    <div
                      style={{
                        display: 'flex',
                        gap: 'var(--spacing-sm)',
                        marginTop: 'var(--spacing-xs)',
                      }}
                    >
                      <NtfyTopicGenerateButton
                        onChange={(v: string) =>
                          dispatch({ type: 'SET_FIELD', field: 'remoteAccessNtfyTopic', value: v })
                        }
                      />
                      <button
                        type="button"
                        className="btn btn--secondary btn--sm"
                        disabled={!remoteAccessNtfyTopic}
                        onClick={async () => {
                          try {
                            await testRemoteAccessNotification();
                            success('Test notification sent!');
                          } catch (err) {
                            toastError(getErrorMessage(err, 'Failed to send test notification'));
                          }
                        }}
                      >
                        Send test notification
                      </button>
                    </div>
                    <p className="form-group__hint">
                      This topic receives your auth URL. <strong>Treat it as a secret</strong> —
                      anyone who knows it can see your auth links. Use a randomly generated value.
                    </p>
                  </div>

                  <div className="form-group" data-testid="form-group-notify-command">
                    <label className="form-group__label" htmlFor="notify-command">
                      Notify Command
                    </label>
                    <input
                      id="notify-command"
                      type="text"
                      className="input"
                      placeholder="echo $SCHMUX_REMOTE_URL | pbcopy"
                      value={remoteAccessNotifyCommand}
                      onChange={(e) =>
                        dispatch({
                          type: 'SET_FIELD',
                          field: 'remoteAccessNotifyCommand',
                          value: e.target.value,
                        })
                      }
                    />
                    <p className="form-group__hint">
                      Shell command to run when the tunnel connects. The URL is available as{' '}
                      <code>$SCHMUX_REMOTE_URL</code>.
                    </p>
                  </div>
                </div>

                <div className="remote-access-grid__qr">
                  <NtfyTopicQRDisplay topic={remoteAccessNtfyTopic} />
                </div>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
