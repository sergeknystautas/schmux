import React from 'react';
import type {
  AuthSecretsModalState,
  ConfigFormAction,
  PastebinEditModalState,
  QuickLaunchDialogModalState,
  RunTargetEditModalState,
  TlsModalState,
} from './useConfigForm';
import type { Model, Persona } from '../../lib/types.generated';

type ConfigModalsProps = {
  authSecretsModal: AuthSecretsModalState;
  runTargetEditModal: RunTargetEditModalState;
  quickLaunchDialogModal: QuickLaunchDialogModalState;
  pastebinEditModal: PastebinEditModalState;
  tlsModal: TlsModalState;
  dispatch: React.Dispatch<ConfigFormAction>;
  onSaveAuthSecrets: () => void;
  onSaveRunTargetEdit: () => void;
  onSaveQuickLaunchDialog: () => void;
  onSavePastebinEdit: () => void;
  onSaveTls: () => void;
  onValidateTls: () => void;
  authPublicBaseURL: string;
  models: Model[];
  personas: Persona[];
};

export default function ConfigModals({
  authSecretsModal,
  runTargetEditModal,
  quickLaunchDialogModal,
  pastebinEditModal,
  tlsModal,
  dispatch,
  onSaveAuthSecrets,
  onSaveRunTargetEdit,
  onSaveQuickLaunchDialog,
  onSavePastebinEdit,
  onSaveTls,
  onValidateTls,
  authPublicBaseURL,
  models,
  personas,
}: ConfigModalsProps) {
  const closeAuthSecretsModal = () => dispatch({ type: 'SET_AUTH_SECRETS_MODAL', modal: null });
  const closeRunTargetEditModal = () =>
    dispatch({ type: 'SET_RUN_TARGET_EDIT_MODAL', modal: null });
  const closeQuickLaunchDialogModal = () =>
    dispatch({ type: 'SET_QUICK_LAUNCH_DIALOG_MODAL', modal: null });
  const closePastebinEditModal = () => dispatch({ type: 'SET_PASTEBIN_EDIT_MODAL', modal: null });
  const closeTlsModal = () => dispatch({ type: 'SET_TLS_MODAL', modal: null });

  return (
    <>
      {tlsModal && (
        <div
          className="modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-labelledby="tls-modal-title"
          onKeyDown={(e) => {
            if (e.key === 'Escape') closeTlsModal();
          }}
        >
          <div className="modal">
            <div className="modal__header">
              <h2 className="modal__title" id="tls-modal-title">
                TLS Certificate
              </h2>
            </div>
            <div className="modal__body">
              <div className="form-group">
                <label className="form-group__label">Certificate Path</label>
                <input
                  type="text"
                  className="input"
                  autoFocus
                  placeholder="~/.schmux/tls/schmux.local.pem"
                  value={tlsModal.certPath}
                  onChange={(e) =>
                    dispatch({
                      type: 'SET_TLS_MODAL',
                      modal: {
                        ...tlsModal,
                        certPath: e.target.value,
                        hostname: '',
                        expires: '',
                        error: '',
                      },
                    })
                  }
                />
              </div>
              <div className="form-group">
                <label className="form-group__label">Key Path</label>
                <input
                  type="text"
                  className="input"
                  placeholder="~/.schmux/tls/schmux.local-key.pem"
                  value={tlsModal.keyPath}
                  onChange={(e) =>
                    dispatch({
                      type: 'SET_TLS_MODAL',
                      modal: {
                        ...tlsModal,
                        keyPath: e.target.value,
                        hostname: '',
                        expires: '',
                        error: '',
                      },
                    })
                  }
                />
              </div>
              <p className="form-group__hint">
                Use <code>mkcert</code> to generate local certificates, or run{' '}
                <code>schmux auth github</code> for guided setup.
              </p>

              {tlsModal.hostname && (
                <div className="banner banner--success mt-md">
                  <p className="m-0">
                    <strong>Valid certificate</strong> for <code>{tlsModal.hostname}</code>
                    {tlsModal.expires && <span> (expires {tlsModal.expires})</span>}
                  </p>
                </div>
              )}

              {tlsModal.error && <p className="form-group__error mt-sm">{tlsModal.error}</p>}
            </div>
            <div className="modal__footer">
              <button className="btn" onClick={closeTlsModal}>
                Cancel
              </button>
              <button
                className="btn btn--secondary"
                onClick={onValidateTls}
                disabled={tlsModal.validating}
              >
                {tlsModal.validating ? 'Validating...' : 'Validate'}
              </button>
              <button
                className="btn btn--primary"
                onClick={onSaveTls}
                disabled={!tlsModal.hostname}
              >
                Save
              </button>
            </div>
          </div>
        </div>
      )}

      {authSecretsModal && (
        <div
          className="modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-labelledby="auth-secrets-modal-title"
          onKeyDown={(e) => {
            if (e.key === 'Escape') closeAuthSecretsModal();
          }}
        >
          <div className="modal">
            <div className="modal__header">
              <h2 className="modal__title" id="auth-secrets-modal-title">
                GitHub OAuth Credentials
              </h2>
            </div>
            <div className="modal__body">
              <div className="form-group">
                <label className="form-group__label">Client ID</label>
                <input
                  type="text"
                  className="input"
                  autoFocus
                  placeholder="Ov23li..."
                  value={authSecretsModal.clientId}
                  onChange={(e) =>
                    dispatch({
                      type: 'SET_AUTH_SECRETS_MODAL',
                      modal: { ...authSecretsModal, clientId: e.target.value },
                    })
                  }
                />
              </div>
              <div className="form-group">
                <label className="form-group__label">Client Secret</label>
                <input
                  type="password"
                  className="input"
                  placeholder={
                    authSecretsModal.clientSecretWasSet
                      ? 'Enter new secret (leave blank to keep existing)'
                      : 'Enter client secret'
                  }
                  value={authSecretsModal.clientSecret}
                  onChange={(e) =>
                    dispatch({
                      type: 'SET_AUTH_SECRETS_MODAL',
                      modal: { ...authSecretsModal, clientSecret: e.target.value },
                    })
                  }
                  onFocus={(e) => {
                    // Clear the mask when user focuses to enter new value
                    if (e.target.value === '••••••••') {
                      dispatch({
                        type: 'SET_AUTH_SECRETS_MODAL',
                        modal: { ...authSecretsModal, clientSecret: '' },
                      });
                    }
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') onSaveAuthSecrets();
                  }}
                />
              </div>
              {authSecretsModal.error && (
                <p className="form-group__error mt-sm">{authSecretsModal.error}</p>
              )}

              <div
                className="form-group__hint"
                style={{
                  marginTop: 'var(--spacing-md)',
                  padding: 'var(--spacing-sm)',
                  background: 'var(--color-bg-secondary)',
                  borderRadius: 'var(--radius-sm)',
                }}
              >
                <p style={{ margin: '0 0 var(--spacing-sm) 0', fontWeight: 500 }}>
                  To create or check on your GitHub OAuth credentials:
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
            <div className="modal__footer">
              <button className="btn" onClick={closeAuthSecretsModal}>
                Cancel
              </button>
              <button className="btn btn--primary" onClick={onSaveAuthSecrets}>
                Save
              </button>
            </div>
          </div>
        </div>
      )}

      {runTargetEditModal && (
        <div
          className="modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-labelledby="runtarget-edit-modal-title"
          onKeyDown={(e) => {
            if (e.key === 'Escape') closeRunTargetEditModal();
          }}
        >
          <div className="modal">
            <div className="modal__header">
              <h2 className="modal__title" id="runtarget-edit-modal-title">
                Edit {runTargetEditModal.target.name}
              </h2>
            </div>
            <div className="modal__body">
              <div className="form-group">
                <label className="form-group__label">Command</label>
                <textarea
                  className="input"
                  value={runTargetEditModal.command}
                  onChange={(e) =>
                    dispatch({
                      type: 'SET_RUN_TARGET_EDIT_MODAL',
                      modal: { ...runTargetEditModal, command: e.target.value, error: '' },
                    })
                  }
                  rows={6}
                  autoFocus
                />
                <p className="form-group__hint">Shell command to run</p>
              </div>
              {runTargetEditModal.error && (
                <p className="form-group__error">{runTargetEditModal.error}</p>
              )}
            </div>
            <div className="modal__footer">
              <button className="btn" onClick={closeRunTargetEditModal}>
                Cancel
              </button>
              <button className="btn btn--primary" onClick={onSaveRunTargetEdit}>
                Save
              </button>
            </div>
          </div>
        </div>
      )}

      {quickLaunchDialogModal && (
        <div
          className="modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-labelledby="quicklaunch-dialog-modal-title"
          onKeyDown={(e) => {
            if (e.key === 'Escape') closeQuickLaunchDialogModal();
          }}
        >
          <div className="modal modal--wide">
            <div className="modal__header">
              <h2 className="modal__title" id="quicklaunch-dialog-modal-title">
                {quickLaunchDialogModal.mode === 'edit'
                  ? `Edit ${quickLaunchDialogModal.name}`
                  : 'Add Quick Launch'}
              </h2>
            </div>
            <div className="modal__body">
              {quickLaunchDialogModal.kind === 'command' ? (
                <>
                  <div className="form-group">
                    <label className="form-group__label">Name</label>
                    <input
                      className="input"
                      value={quickLaunchDialogModal.name}
                      onChange={(e) =>
                        dispatch({
                          type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
                          modal: { ...quickLaunchDialogModal, name: e.target.value, error: '' },
                        })
                      }
                      placeholder="e.g. build"
                      readOnly={quickLaunchDialogModal.mode === 'edit'}
                      autoFocus
                    />
                  </div>
                  <div className="form-group">
                    <label className="form-group__label">Command</label>
                    <textarea
                      className="input"
                      value={quickLaunchDialogModal.command || ''}
                      onChange={(e) =>
                        dispatch({
                          type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
                          modal: { ...quickLaunchDialogModal, command: e.target.value, error: '' },
                        })
                      }
                      placeholder="Shell command (e.g. make build)"
                      rows={10}
                    />
                  </div>
                </>
              ) : (
                <>
                  <div className="form-group">
                    <label className="form-group__label">Name</label>
                    <input
                      className="input"
                      value={quickLaunchDialogModal.name}
                      onChange={(e) =>
                        dispatch({
                          type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
                          modal: { ...quickLaunchDialogModal, name: e.target.value, error: '' },
                        })
                      }
                      placeholder="e.g. code-review"
                      readOnly={quickLaunchDialogModal.mode === 'edit'}
                      autoFocus
                    />
                  </div>
                  <div className="form-group">
                    <label className="form-group__label">Model</label>
                    <select
                      className="input"
                      value={quickLaunchDialogModal.target || ''}
                      onChange={(e) =>
                        dispatch({
                          type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
                          modal: { ...quickLaunchDialogModal, target: e.target.value, error: '' },
                        })
                      }
                    >
                      <option value="">Select agent...</option>
                      {quickLaunchDialogModal.mode === 'edit' &&
                        quickLaunchDialogModal.target &&
                        !models.some(
                          (m) => m.id === quickLaunchDialogModal.target && m.configured
                        ) && (
                          <option value={quickLaunchDialogModal.target} disabled>
                            {quickLaunchDialogModal.target} (unavailable)
                          </option>
                        )}
                      <optgroup label="Agents">
                        {models
                          .filter((model) => model.configured)
                          .map((model) => (
                            <option key={model.id} value={model.id}>
                              {model.display_name}
                            </option>
                          ))}
                      </optgroup>
                    </select>
                  </div>
                  {personas.length > 0 && (
                    <div className="form-group">
                      <label className="form-group__label">Persona</label>
                      <select
                        className="input"
                        value={quickLaunchDialogModal.personaId || ''}
                        onChange={(e) =>
                          dispatch({
                            type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
                            modal: {
                              ...quickLaunchDialogModal,
                              personaId: e.target.value,
                              error: '',
                            },
                          })
                        }
                      >
                        <option value="">No persona</option>
                        {personas.map((p) => (
                          <option key={p.id} value={p.id}>
                            {p.icon} {p.name}
                          </option>
                        ))}
                      </select>
                    </div>
                  )}
                  <div className="form-group">
                    <label className="form-group__label">Prompt</label>
                    <textarea
                      className="input"
                      value={quickLaunchDialogModal.prompt || ''}
                      onChange={(e) =>
                        dispatch({
                          type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
                          modal: { ...quickLaunchDialogModal, prompt: e.target.value, error: '' },
                        })
                      }
                      placeholder="Prompt to send to the agent"
                      rows={10}
                    />
                  </div>
                </>
              )}
              {quickLaunchDialogModal.error && (
                <p className="form-group__error">{quickLaunchDialogModal.error}</p>
              )}
            </div>
            <div className="modal__footer">
              <button className="btn" onClick={closeQuickLaunchDialogModal}>
                Cancel
              </button>
              <button className="btn btn--primary" onClick={onSaveQuickLaunchDialog}>
                Save
              </button>
            </div>
          </div>
        </div>
      )}

      {pastebinEditModal && (
        <div
          className="modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-labelledby="pastebin-edit-modal-title"
          onKeyDown={(e) => {
            if (e.key === 'Escape') closePastebinEditModal();
          }}
        >
          <div className="modal modal--wide">
            <div className="modal__header">
              <h2 className="modal__title" id="pastebin-edit-modal-title">
                {pastebinEditModal.index !== undefined ? 'Edit clip' : 'Add clip'}
              </h2>
            </div>
            <div className="modal__body">
              <div className="form-group">
                <label className="form-group__label">Content</label>
                <textarea
                  className="textarea"
                  value={pastebinEditModal.content}
                  onChange={(e) =>
                    dispatch({
                      type: 'SET_PASTEBIN_EDIT_MODAL',
                      modal: { ...pastebinEditModal, content: e.target.value, error: '' },
                    })
                  }
                  rows={10}
                  autoFocus
                />
              </div>
              {pastebinEditModal.error && (
                <p className="form-group__error">{pastebinEditModal.error}</p>
              )}
            </div>
            <div className="modal__footer">
              <button className="btn" onClick={closePastebinEditModal}>
                Cancel
              </button>
              <button className="btn btn--primary" onClick={onSavePastebinEdit}>
                Save
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
