import React, { createContext, useContext, useEffect, useMemo, useState } from 'react';

type ModalBase = {
  title: string;
  confirmText: string;
  cancelText: string | null;
  danger: boolean;
  detailedMessage: string;
  wide: boolean;
  resolve: (value: boolean | string | null) => void;
};

type AlertModal = ModalBase & {
  isPrompt?: false;
  message: string;
};

type PromptModal = ModalBase & {
  isPrompt: true;
  defaultValue: string;
  placeholder: string;
  errorMessage: string;
  password: boolean;
};

type ModalState = AlertModal | PromptModal;

type ModalOptions = {
  confirmText?: string;
  cancelText?: string | null;
  danger?: boolean;
  detailedMessage?: string;
  defaultValue?: string;
  placeholder?: string;
  errorMessage?: string;
  password?: boolean;
  wide?: boolean;
};

type ModalOptionsInput = ModalOptions | string;

type ModalContextValue = {
  show: (title: string, message: string, options?: ModalOptionsInput) => Promise<boolean | null>;
  alert: (title: string, message: string) => Promise<boolean | null>;
  confirm: (message: string, options?: ModalOptionsInput) => Promise<boolean | null>;
  prompt: (title: string, options?: ModalOptionsInput) => Promise<string | null>;
};

const ModalContext = createContext<ModalContextValue | null>(null);

export function useModal() {
  const ctx = useContext(ModalContext);
  if (!ctx) throw new Error('useModal must be used within ModalProvider');
  return ctx;
}

export default function ModalProvider({ children }: { children: React.ReactNode }) {
  const [modal, setModal] = useState<ModalState | null>(null);

  const show = (title: string, message: string, options: ModalOptionsInput = {}) =>
    new Promise<boolean | null>((resolve) => {
      const normalizedOptions = typeof options === 'string' ? {} : options;
      const resolveModal = resolve as (value: boolean | string | null) => void;
      setModal({
        title,
        message,
        confirmText: normalizedOptions.confirmText || 'Confirm',
        cancelText:
          normalizedOptions.cancelText !== undefined ? normalizedOptions.cancelText : 'Cancel',
        danger: normalizedOptions.danger || false,
        detailedMessage: normalizedOptions.detailedMessage || '',
        wide: normalizedOptions.wide || false,
        resolve: resolveModal,
      });
    });

  const alert = (title: string, message: string) =>
    show(title, message, { confirmText: 'OK', cancelText: null });

  const confirm = (message: string, options: ModalOptionsInput = {}) =>
    show('Confirm Action', message, options);

  const prompt = (title: string, options: ModalOptionsInput = {}) =>
    new Promise<string | null>((resolve) => {
      const normalizedOptions = typeof options === 'string' ? {} : options;
      const resolveModal = resolve as (value: boolean | string | null) => void;
      setModal({
        title,
        isPrompt: true,
        defaultValue: normalizedOptions.defaultValue || '',
        placeholder: normalizedOptions.placeholder || '',
        confirmText: normalizedOptions.confirmText || 'Save',
        cancelText:
          normalizedOptions.cancelText !== undefined ? normalizedOptions.cancelText : 'Cancel',
        errorMessage: normalizedOptions.errorMessage || '',
        danger: normalizedOptions.danger || false,
        detailedMessage: normalizedOptions.detailedMessage || '',
        password: normalizedOptions.password || false,
        wide: normalizedOptions.wide || false,
        resolve: resolveModal,
      });
    });

  const api = useMemo(() => ({ show, alert, confirm, prompt }), []);

  const close = (result: boolean | string | null) => {
    if (!modal) return;
    if (modal.isPrompt) {
      modal.resolve(result); // result is the input value or null
    } else {
      modal.resolve(result);
    }
    setModal(null);
  };

  const handlePromptConfirm = () => {
    const input = document.getElementById('modal-prompt-input') as HTMLInputElement | null;
    const value = input?.value || '';
    close(value);
  };

  // Keyboard handling for non-prompt modals
  useEffect(() => {
    if (!modal || modal.isPrompt) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        close(true);
      } else if (e.key === 'Escape') {
        e.preventDefault();
        // If no cancel button, Escape confirms; otherwise Escape cancels
        if (modal.cancelText === null) {
          close(true);
        } else {
          close(null);
        }
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [modal]);

  return (
    <ModalContext.Provider value={api}>
      {children}
      {modal && (
        <div
          className="modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-labelledby="modal-title"
        >
          <div className={`modal${modal.wide ? ' modal--wide' : ''}`}>
            <div className="modal__header">
              <h2 className="modal__title" id="modal-title">
                {modal.title}
              </h2>
            </div>
            <div className="modal__body">
              {modal.isPrompt ? (
                <>
                  <input
                    id="modal-prompt-input"
                    type={modal.password ? 'password' : 'text'}
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
                    <p
                      className="form-group__error"
                      style={{ marginTop: 'var(--spacing-sm)', color: 'var(--color-error)' }}
                    >
                      {modal.errorMessage}
                    </p>
                  )}
                </>
              ) : (
                <>
                  {'message' in modal ? <p>{modal.message}</p> : null}
                  {modal.detailedMessage ? (
                    <p className="text-muted">{modal.detailedMessage}</p>
                  ) : null}
                </>
              )}
            </div>
            <div className="modal__footer">
              {modal.cancelText ? (
                <button className="btn" onClick={() => close(null)}>
                  {modal.cancelText}
                </button>
              ) : null}
              <button
                className={`btn ${modal.danger ? 'btn--danger' : 'btn--primary'}`}
                onClick={() => (modal.isPrompt ? handlePromptConfirm() : close(true))}
                autoFocus={!modal.isPrompt}
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
