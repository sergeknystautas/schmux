import React, { createContext, useContext, useMemo, useState } from 'react';

const ModalContext = createContext(null);

export function useModal() {
  const ctx = useContext(ModalContext);
  if (!ctx) throw new Error('useModal must be used within ModalProvider');
  return ctx;
}

export default function ModalProvider({ children }) {
  const [modal, setModal] = useState(null);

  const show = (title, message, options = {}) => new Promise((resolve) => {
    setModal({
      title,
      message,
      confirmText: options.confirmText || 'Confirm',
      cancelText: options.cancelText || 'Cancel',
      danger: options.danger || false,
      detailedMessage: options.detailedMessage || '',
      resolve
    });
  });

  const alert = (title, message) => show(title, message, { confirmText: 'OK', cancelText: null });

  const confirm = (message, options = {}) => show('Confirm Action', message, options);

  const prompt = (title, options = {}) => new Promise((resolve) => {
    setModal({
      title,
      isPrompt: true,
      defaultValue: options.defaultValue || '',
      placeholder: options.placeholder || '',
      confirmText: options.confirmText || 'Save',
      cancelText: options.cancelText || 'Cancel',
      errorMessage: options.errorMessage || '',
      resolve
    });
  });

  const api = useMemo(() => ({ show, alert, confirm, prompt }), []);

  const close = (result) => {
    if (!modal) return;
    if (modal.isPrompt) {
      modal.resolve(result); // result is the input value or null
    } else {
      modal.resolve(result);
    }
    setModal(null);
  };

  const handlePromptConfirm = () => {
    const input = document.getElementById('modal-prompt-input');
    const value = input?.value || '';
    close(value);
  };

  return (
    <ModalContext.Provider value={api}>
      {children}
      {modal && (
        <div className="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="modal-title">
          <div className="modal">
            <div className="modal__header">
              <h2 className="modal__title" id="modal-title">{modal.title}</h2>
            </div>
            <div className="modal__body">
              {modal.isPrompt ? (
                <>
                  <input
                    id="modal-prompt-input"
                    type="text"
                    className="input"
                    defaultValue={modal.defaultValue}
                    placeholder={modal.placeholder}
                    autoFocus
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') handlePromptConfirm();
                      if (e.key === 'Escape') close(null);
                    }}
                  />
                  {modal.errorMessage && (
                    <p className="form-group__error" style={{ marginTop: 'var(--spacing-sm)', color: 'var(--color-error)' }}>
                      {modal.errorMessage}
                    </p>
                  )}
                </>
              ) : (
                <>
                  <p>{modal.message}</p>
                  {modal.detailedMessage ? <p className="text-muted">{modal.detailedMessage}</p> : null}
                </>
              )}
            </div>
            <div className="modal__footer">
              {modal.cancelText ? (
                <button className="btn" onClick={() => close(null)}>{modal.cancelText}</button>
              ) : null}
              <button
                className={`btn ${modal.danger ? 'btn--danger' : 'btn--primary'}`}
                onClick={() => modal.isPrompt ? handlePromptConfirm() : close(true)}
              >
                {modal.confirmText}
              </button>
            </div>
          </div>
        </div>
      )}
    </ModalContext.Provider>
  );
}
