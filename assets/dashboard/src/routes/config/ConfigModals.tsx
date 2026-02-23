import React from 'react';
import type {
  AuthSecretsModalState,
  ConfigFormAction,
  QuickLaunchEditModalState,
  RunTargetEditModalState,
} from './useConfigForm';

type ConfigModalsProps = {
  authSecretsModal: AuthSecretsModalState;
  runTargetEditModal: RunTargetEditModalState;
  quickLaunchEditModal: QuickLaunchEditModalState;
  dispatch: React.Dispatch<ConfigFormAction>;
  onSaveAuthSecrets: () => void;
  onSaveRunTargetEdit: () => void;
  onSaveQuickLaunchEdit: () => void;
};

export default function ConfigModals({
  authSecretsModal,
  runTargetEditModal,
  quickLaunchEditModal,
  dispatch,
  onSaveAuthSecrets,
  onSaveRunTargetEdit,
  onSaveQuickLaunchEdit,
}: ConfigModalsProps) {
  const closeAuthSecretsModal = () => dispatch({ type: 'SET_AUTH_SECRETS_MODAL', modal: null });
  const closeRunTargetEditModal = () =>
    dispatch({ type: 'SET_RUN_TARGET_EDIT_MODAL', modal: null });
  const closeQuickLaunchEditModal = () =>
    dispatch({ type: 'SET_QUICK_LAUNCH_EDIT_MODAL', modal: null });

  return (
    <>
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
                  placeholder="Enter client secret"
                  value={authSecretsModal.clientSecret}
                  onChange={(e) =>
                    dispatch({
                      type: 'SET_AUTH_SECRETS_MODAL',
                      modal: { ...authSecretsModal, clientSecret: e.target.value },
                    })
                  }
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') onSaveAuthSecrets();
                  }}
                />
              </div>
              {authSecretsModal.error && (
                <p className="form-group__error" style={{ marginTop: 'var(--spacing-sm)' }}>
                  {authSecretsModal.error}
                </p>
              )}
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
                <p className="form-group__hint">
                  {runTargetEditModal.target.type === 'promptable'
                    ? 'Prompt is appended as last arg'
                    : 'Shell command to run'}
                </p>
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

      {quickLaunchEditModal && (
        <div
          className="modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-labelledby="quicklaunch-edit-modal-title"
          onKeyDown={(e) => {
            if (e.key === 'Escape') closeQuickLaunchEditModal();
          }}
        >
          <div className="modal">
            <div className="modal__header">
              <h2 className="modal__title" id="quicklaunch-edit-modal-title">
                Edit {quickLaunchEditModal.item.name}
              </h2>
            </div>
            <div className="modal__body">
              {quickLaunchEditModal.isCommandTarget ? (
                <div className="form-group">
                  <label className="form-group__label">Command</label>
                  <textarea
                    className="input"
                    value={quickLaunchEditModal.prompt}
                    onChange={(e) =>
                      dispatch({
                        type: 'SET_QUICK_LAUNCH_EDIT_MODAL',
                        modal: { ...quickLaunchEditModal, prompt: e.target.value, error: '' },
                      })
                    }
                    placeholder="Shell command to run"
                    rows={6}
                    autoFocus
                  />
                  <p className="form-group__hint" style={{ color: 'var(--color-warning-text)' }}>
                    This will update the underlying command target used by this quick launch item.
                  </p>
                </div>
              ) : (
                <div className="form-group">
                  <label className="form-group__label">Prompt</label>
                  <textarea
                    className="input quick-launch-editor__prompt-input"
                    value={quickLaunchEditModal.prompt}
                    onChange={(e) =>
                      dispatch({
                        type: 'SET_QUICK_LAUNCH_EDIT_MODAL',
                        modal: { ...quickLaunchEditModal, prompt: e.target.value, error: '' },
                      })
                    }
                    placeholder="Prompt to send to the agent"
                    rows={10}
                    autoFocus
                  />
                </div>
              )}
              {quickLaunchEditModal.error && (
                <p className="form-group__error">{quickLaunchEditModal.error}</p>
              )}
            </div>
            <div className="modal__footer">
              <button className="btn" onClick={closeQuickLaunchEditModal}>
                Cancel
              </button>
              <button className="btn btn--primary" onClick={onSaveQuickLaunchEdit}>
                Save
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
