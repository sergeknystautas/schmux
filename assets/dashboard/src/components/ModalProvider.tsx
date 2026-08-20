import React, { createContext, useContext, useEffect, useMemo, useRef, useState } from 'react';
import useFocusTrap from '../hooks/useFocusTrap';

type ModalBase = {
  title: string;
  confirmText: string;
  cancelText: string | null;
  danger: boolean;
  detailedMessage: string;
  wide: boolean;
  checkbox?: { label: string; code?: string; note?: string; defaultChecked: boolean };
  resolve: (value: boolean | string | null | { confirmed: boolean; checked: boolean }) => void;
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
  checkbox?: { label: string; code?: string; note?: string; defaultChecked: boolean };
};

type ModalOptionsInput = ModalOptions | string;

type ModalContextValue = {
  show: (title: string, message: string, options?: ModalOptionsInput) => Promise<boolean | null>;
  alert: (title: string, message: string) => Promise<boolean | null>;
  confirm: (message: string, options?: ModalOptionsInput) => Promise<boolean | null>;
  prompt: (title: string, options?: ModalOptionsInput) => Promise<string | null>;
  confirmWithCheckbox: (
    message: string,
    options: ModalOptions & {
      checkbox: { label: string; code?: string; note?: string; defaultChecked: boolean };
    }
  ) => Promise<{ confirmed: boolean; checked: boolean } | null>;
};

const ModalContext = createContext<ModalContextValue | null>(null);

export function useModal() {
  const ctx = useContext(ModalContext);
  if (!ctx) throw new Error('useModal must be used within ModalProvider');
  return ctx;
}

export default function ModalProvider({ children }: { children: React.ReactNode }) {
  const [modal, setModal] = useState<ModalState | null>(null);
  const [checked, setChecked] = useState(false);
  const modalRef = useRef<HTMLDivElement>(null);

  useFocusTrap(modalRef, !!modal);

  const show = (title: string, message: string, options: ModalOptionsInput = {}) =>
    new Promise<boolean | null>((resolve) => {
      const normalizedOptions = typeof options === 'string' ? {} : options;
      const resolveModal = resolve as (
        value: boolean | string | null | { confirmed: boolean; checked: boolean }
      ) => void;
      setModal({
        title,
        message,
        confirmText: normalizedOptions.confirmText || 'Confirm',
        cancelText:
          normalizedOptions.cancelText !== undefined ? normalizedOptions.cancelText : 'Cancel',
        danger: normalizedOptions.danger || false,
        detailedMessage: normalizedOptions.detailedMessage || '',
        wide: normalizedOptions.wide || false,
        checkbox: normalizedOptions.checkbox,
        resolve: resolveModal,
      });
    });

  const alert = (title: string, message: string) =>
    show(title, message, { confirmText: 'OK', cancelText: null });

  const confirm = (message: string, options: ModalOptionsInput = {}) =>
    show('Confirm Action', message, options);

  const confirmWithCheckbox = (
    message: string,
    options: ModalOptions & {
      checkbox: { label: string; code?: string; note?: string; defaultChecked: boolean };
    }
  ) =>
    new Promise<{ confirmed: boolean; checked: boolean } | null>((resolve) => {
      setChecked(options.checkbox.defaultChecked);
      const resolveModal = resolve as (
        value: boolean | string | null | { confirmed: boolean; checked: boolean }
      ) => void;
      setModal({
        title: 'Confirm Action',
        message,
        confirmText: options.confirmText || 'Confirm',
        cancelText: options.cancelText !== undefined ? options.cancelText : 'Cancel',
        danger: options.danger || false,
        detailedMessage: options.detailedMessage || '',
        wide: options.wide || false,
        checkbox: options.checkbox,
        resolve: resolveModal,
      });
    });

  const prompt = (title: string, options: ModalOptionsInput = {}) =>
    new Promise<string | null>((resolve) => {
      const normalizedOptions = typeof options === 'string' ? {} : options;
      const resolveModal = resolve as (
        value: boolean | string | null | { confirmed: boolean; checked: boolean }
      ) => void;
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

  const api = useMemo(() => ({ show, alert, confirm, confirmWithCheckbox, prompt }), []);

  const close = (result: boolean | string | null) => {
    if (!modal) return;
    if (modal.isPrompt) {
      modal.resolve(result); // result is the input value or null
    } else if (modal.checkbox && typeof result === 'boolean') {
      modal.resolve({ confirmed: result, checked });
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
    // `checked` is a dependency because close() reads it: without it the
    // listener keeps the value the checkbox had when the modal opened, and
    // confirming with Enter reports a stale box.
  }, [modal, checked]);

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
          <div ref={modalRef} className={`modal${modal.wide ? ' modal--wide' : ''}`}>
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
                    <p className="form-group__error mt-sm text-error">{modal.errorMessage}</p>
                  )}
                </>
              ) : (
                <>
                  {'message' in modal ? <p>{modal.message}</p> : null}
                  {modal.detailedMessage ? (
                    <p className="text-muted">{modal.detailedMessage}</p>
                  ) : null}
                  {modal.checkbox ? (
                    <div className="checkbox-list mt-md">
                      <label className="checkbox-list__item">
                        <input
                          type="checkbox"
                          data-testid="modal-checkbox"
                          checked={checked}
                          onChange={(e) => setChecked(e.target.checked)}
                        />
                        <span>
                          {modal.checkbox.label}
                          {modal.checkbox.code ? (
                            <span className="checkbox-list__ref">
                              <span className="mono">{modal.checkbox.code}</span>
                              {modal.checkbox.note ? (
                                <span className="text-muted"> {modal.checkbox.note}</span>
                              ) : null}
                            </span>
                          ) : null}
                        </span>
                      </label>
                    </div>
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
                data-variant={modal.danger ? 'danger' : undefined}
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
