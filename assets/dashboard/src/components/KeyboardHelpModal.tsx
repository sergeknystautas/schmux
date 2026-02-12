import React, { createContext, useContext, useEffect, useMemo, useState } from 'react';

type HelpModalState = {
  isOpen: boolean;
};

type HelpModalContextValue = {
  show: () => void;
};

const HelpModalContext = createContext<HelpModalContextValue | null>(null);

export function useHelpModal() {
  const ctx = useContext(HelpModalContext);
  if (!ctx) throw new Error('useHelpModal must be used within HelpModalProvider');
  return ctx;
}

const shortcuts = [
  { key: 'N', description: 'Spawn new session (context-aware)' },
  { key: 'Shift+N', description: 'Spawn new session (always general)' },
  { key: 'W', description: 'Dispose session' },
  { key: 'Shift+W', description: 'Dispose workspace' },
  { key: 'V', description: 'Open workspace in VS Code' },
  { key: 'D', description: 'Go to diff page' },
  { key: 'G', description: 'Go to git graph' },
  { key: 'H', description: 'Go to home' },
  { key: '?', description: 'Show this help' },
];

// Detect if user is on Mac (use Cmd) or Windows/Linux (use Ctrl)
const isMac = typeof navigator !== 'undefined' && /Mac|iPod|iPhone|iPad/.test(navigator.platform);
const modKey = isMac ? 'Cmd' : 'Ctrl';

const directShortcuts = [
  { key: `${modKey}+K`, description: 'Toggle keyboard mode' },
  { key: `${modKey}+←`, description: 'Previous tab in workspace' },
  { key: `${modKey}+→`, description: 'Next tab in workspace' },
  { key: `${modKey}+↑`, description: 'Previous workspace' },
  { key: `${modKey}+↓`, description: 'Next workspace' },
  { key: `${modKey}+Enter`, description: 'Submit spawn form' },
];

export default function HelpModalProvider({ children }: { children: React.ReactNode }) {
  const [isOpen, setIsOpen] = useState(false);

  const show = () => setIsOpen(true);
  const close = () => setIsOpen(false);

  useEffect(() => {
    if (!isOpen) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' || e.key === 'Enter') {
        e.preventDefault();
        e.stopPropagation();
        close();
      }
    };

    // Use capture phase to intercept events before they reach the terminal
    window.addEventListener('keydown', handleKeyDown, true);
    return () => window.removeEventListener('keydown', handleKeyDown, true);
  }, [isOpen]);

  const value = useMemo(() => ({ show }), []);
  const splitIndex = Math.ceil(shortcuts.length / 2);
  const leftShortcuts = shortcuts.slice(0, splitIndex);
  const rightShortcuts = shortcuts.slice(splitIndex);
  const maxRows = Math.max(leftShortcuts.length, rightShortcuts.length);

  return (
    <HelpModalContext.Provider value={value}>
      {children}
      {isOpen && (
        <div
          className="modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-labelledby="help-modal-title"
        >
          <div className="modal modal--wide">
            <div className="modal__header">
              <h2 className="modal__title" id="help-modal-title">
                Keyboard Shortcuts
              </h2>
            </div>
            <div className="modal__body">
              <p style={{ marginBottom: 'var(--spacing-md)' }}>
                Press <kbd>{modKey}</kbd> + <kbd>K</kbd> to enter keyboard mode, then press a key to
                execute an action.
              </p>
              <table className="keyboard-shortcuts-table keyboard-shortcuts-table--two-col">
                <thead>
                  <tr>
                    <th>Key</th>
                    <th>Action</th>
                    <th>Key</th>
                    <th>Action</th>
                  </tr>
                </thead>
                <tbody>
                  {Array.from({ length: maxRows }).map((_, index) => {
                    const left = leftShortcuts[index];
                    const right = rightShortcuts[index];
                    return (
                      <tr
                        key={`${left?.key || 'empty-left'}-${right?.key || 'empty-right'}-${index}`}
                      >
                        <td>{left ? <kbd>{left.key}</kbd> : null}</td>
                        <td>{left?.description || ''}</td>
                        <td>{right ? <kbd>{right.key}</kbd> : null}</td>
                        <td>{right?.description || ''}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
              <p style={{ marginTop: 'var(--spacing-lg)', marginBottom: 'var(--spacing-md)' }}>
                These shortcuts work directly without entering keyboard mode:
              </p>
              <table className="keyboard-shortcuts-table keyboard-shortcuts-table--two-col">
                <thead>
                  <tr>
                    <th>Key</th>
                    <th>Action</th>
                    <th>Key</th>
                    <th>Action</th>
                  </tr>
                </thead>
                <tbody>
                  {Array.from({ length: Math.ceil(directShortcuts.length / 2) }).map((_, index) => {
                    const left = directShortcuts[index * 2];
                    const right = directShortcuts[index * 2 + 1];
                    return (
                      <tr
                        key={`direct-${left?.key || 'empty-left'}-${right?.key || 'empty-right'}-${index}`}
                      >
                        <td>{left ? <kbd>{left.key}</kbd> : null}</td>
                        <td>{left?.description || ''}</td>
                        <td>{right ? <kbd>{right.key}</kbd> : null}</td>
                        <td>{right?.description || ''}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            <div className="modal__footer">
              <button className="btn btn--primary" onClick={close}>
                Close
              </button>
            </div>
          </div>
        </div>
      )}
    </HelpModalContext.Provider>
  );
}
