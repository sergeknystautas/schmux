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
  { key: '0-9', description: 'Jump to session by index' },
  { key: 'W', description: 'Dispose session (session detail only)' },
  { key: 'D', description: 'Go to diff page (workspace only)' },
  { key: 'G', description: 'Go to git graph (workspace only)' },
  { key: 'H', description: 'Go to home' },
  { key: '?', description: 'Show this help modal' },
  { key: 'Esc', description: 'Cancel keyboard mode' },
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

  return (
    <HelpModalContext.Provider value={value}>
      {children}
      {isOpen && (
        <div className="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="help-modal-title">
          <div className="modal">
            <div className="modal__header">
              <h2 className="modal__title" id="help-modal-title">Keyboard Shortcuts</h2>
            </div>
            <div className="modal__body">
              <p style={{ marginBottom: 'var(--spacing-md)' }}>
                Press <kbd>Cmd</kbd> + <kbd>K</kbd> to enter keyboard mode, then press a key to execute an action.
              </p>
              <table className="keyboard-shortcuts-table">
                <thead>
                  <tr>
                    <th>Key</th>
                    <th>Action</th>
                  </tr>
                </thead>
                <tbody>
                  {shortcuts.map((shortcut) => (
                    <tr key={shortcut.key}>
                      <td><kbd>{shortcut.key}</kbd></td>
                      <td>{shortcut.description}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="modal__footer">
              <button className="btn btn--primary" onClick={close}>Close</button>
            </div>
          </div>
        </div>
      )}
    </HelpModalContext.Provider>
  );
}
