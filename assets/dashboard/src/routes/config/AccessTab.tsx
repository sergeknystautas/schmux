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
  dispatch: React.Dispatch<ConfigFormAction>;
  onSetPassword: () => void;
  onOpenAuthSecretsModal: () => void;
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
  dispatch,
  onSetPassword,
  onOpenAuthSecretsModal,
  success,
  toastError,
}: AccessTabProps) {
  return (
    <div className="wizard-step-content" data-step="5">
      <h2 className="wizard-step-content__title">Access</h2>
      <p className="wizard-step-content__description">
        Control how the dashboard is accessed — local network, remote tunneling, and authentication.
      </p>

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

          {remoteAccessEnabled && (
            <div className="remote-access-grid">
              <div className="remote-access-grid__fields">
                <div className="form-group">
                  <label className="form-group__label">Access Password</label>
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
                    {passwordError && <p className="form-group__error">{passwordError}</p>}
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

                <div className="form-group">
                  <label className="form-group__label">Timeout (minutes)</label>
                  <input
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

                <div className="form-group">
                  <label className="form-group__label">ntfy Topic</label>
                  <input
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

                <div className="form-group">
                  <label className="form-group__label">Notify Command</label>
                  <input
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

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Authentication</h3>
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
                checked={authEnabled}
                onChange={(e) =>
                  dispatch({ type: 'SET_FIELD', field: 'authEnabled', value: e.target.checked })
                }
              />
              <span>Enable GitHub authentication</span>
            </label>
            <p className="form-group__hint">
              Require GitHub login to access the dashboard. Requires HTTPS.
            </p>
          </div>

          {authEnabled && (
            <>
              <div className="form-group">
                <label className="form-group__label">Dashboard URL</label>
                <input
                  type="text"
                  className="input"
                  placeholder="https://schmux.local:7337"
                  value={authPublicBaseURL}
                  onChange={(e) =>
                    dispatch({
                      type: 'SET_FIELD',
                      field: 'authPublicBaseURL',
                      value: e.target.value,
                    })
                  }
                />
                <p className="form-group__hint">
                  The URL users will type to access schmux. Must be https.
                </p>
              </div>

              <div className="form-row">
                <div className="form-group">
                  <label className="form-group__label">TLS Cert Path</label>
                  <input
                    type="text"
                    className="input"
                    placeholder="~/.schmux/tls/schmux.local.pem"
                    value={authTlsCertPath}
                    onChange={(e) =>
                      dispatch({
                        type: 'SET_FIELD',
                        field: 'authTlsCertPath',
                        value: e.target.value,
                      })
                    }
                  />
                </div>
                <div className="form-group">
                  <label className="form-group__label">TLS Key Path</label>
                  <input
                    type="text"
                    className="input"
                    placeholder="~/.schmux/tls/schmux.local-key.pem"
                    value={authTlsKeyPath}
                    onChange={(e) =>
                      dispatch({
                        type: 'SET_FIELD',
                        field: 'authTlsKeyPath',
                        value: e.target.value,
                      })
                    }
                  />
                </div>
              </div>
              <p className="form-group__hint" style={{ marginTop: 'calc(-1 * var(--spacing-sm))' }}>
                Use <code>mkcert</code> to generate local certificates, or run{' '}
                <code>schmux auth github</code> for guided setup.
              </p>

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
                />
                <p className="form-group__hint">How long before requiring re-authentication.</p>
              </div>

              <div className="form-group">
                <label className="form-group__label">GitHub OAuth Credentials</label>
                <div className="item-list" style={{ marginTop: 'var(--spacing-xs)' }}>
                  <div className="item-list__item">
                    <div className="item-list__item-primary">
                      <span className="item-list__item-name">
                        {authClientIdSet && authClientSecretSet ? (
                          <span style={{ color: 'var(--color-success)' }}>Configured</span>
                        ) : (
                          <span style={{ color: 'var(--color-warning)' }}>Not configured</span>
                        )}
                      </span>
                      <span className="item-list__item-detail">
                        Create at github.com/settings/developers
                      </span>
                    </div>
                    {authClientIdSet && authClientSecretSet ? (
                      <button
                        type="button"
                        className="btn btn--primary"
                        onClick={onOpenAuthSecretsModal}
                      >
                        Update
                      </button>
                    ) : (
                      <button
                        type="button"
                        className="btn btn--primary"
                        onClick={onOpenAuthSecretsModal}
                      >
                        Add
                      </button>
                    )}
                  </div>
                </div>
                <div className="form-group__hint" style={{ marginTop: 'var(--spacing-sm)' }}>
                  <p
                    className="form-group__hint"
                    style={{ marginTop: 'calc(-1 * var(--spacing-sm))' }}
                  >
                    To create or check on your GitHub OAuth credentials, follow these steps:
                  </p>
                  <ol style={{ margin: 0, paddingLeft: 'var(--spacing-lg)' }}>
                    <li>
                      Go to{' '}
                      <a
                        href="https://github.com/settings/developers"
                        target="_blank"
                        rel="noreferrer"
                      >
                        github.com/settings/developers
                      </a>
                    </li>
                    <li>Click "New OAuth App" (or edit existing)</li>
                    <li>
                      Use these values:
                      <ul style={{ marginTop: 'var(--spacing-xs)' }}>
                        <li>
                          Application name: <code>schmux</code>
                        </li>
                        <li>
                          Homepage URL:{' '}
                          <code>{authPublicBaseURL || 'https://schmux.local:7337'}</code>
                        </li>
                        <li>
                          Callback URL:{' '}
                          <code>
                            {authPublicBaseURL
                              ? `${authPublicBaseURL.replace(/\/+$/, '')}/auth/callback`
                              : 'https://schmux.local:7337/auth/callback'}
                          </code>
                        </li>
                      </ul>
                    </li>
                    <li>Copy the Client ID and Client Secret</li>
                  </ol>
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
            </>
          )}
        </div>
      </div>
    </div>
  );
}
