import React, { useState, useRef, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate } from 'react-router-dom';
import { diffExternal, getErrorMessage } from '../lib/api'
import { useToast } from './ToastProvider'
import type { WorkspaceResponse } from '../lib/types';

type ExternalDiffCommand = {
  name: string;
  command: string;
};

type DiffDropdownProps = {
  workspace: WorkspaceResponse;
  externalDiffCommands: ExternalDiffCommand[];
};

// Built-in diff commands (always available, not editable)
const BUILTIN_DIFF_COMMANDS: ExternalDiffCommand[] = [
  { name: 'VS Code', command: 'code --diff "$LOCAL" "$REMOTE"' }
];

export default function DiffDropdown({ workspace, externalDiffCommands }: DiffDropdownProps) {
  const { success, error: toastError } = useToast();
  const navigate = useNavigate();
  const [isOpen, setIsOpen] = useState(false);
  const [executing, setExecuting] = useState<string | null>(null);
  const [result, setResult] = useState<{ success: boolean; message: string } | null>(null);
  const [menuPosition, setMenuPosition] = useState({ top: 0, left: 0 });
  const [placementAbove, setPlacementAbove] = useState(false);
  const toggleRef = useRef<HTMLButtonElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);

  // Calculate menu position when dropdown opens
  useEffect(() => {
    if (isOpen && toggleRef.current) {
      const rect = toggleRef.current.getBoundingClientRect();
      const gap = 4;
      // Estimate menu height: 1 for browser view + built-in + user commands
      const estimatedMenuHeight = menuRef.current?.offsetHeight ||
        Math.min(300, 40 + (1 + BUILTIN_DIFF_COMMANDS.length + (externalDiffCommands?.length || 0)) * 36);

      const spaceBelow = window.innerHeight - rect.bottom - gap;
      const spaceAbove = rect.top - gap;

      // Flip above if not enough space below and more space above
      const shouldPlaceAbove = spaceBelow < estimatedMenuHeight && spaceAbove > spaceBelow;
      setPlacementAbove(shouldPlaceAbove);

      if (shouldPlaceAbove) {
        setMenuPosition({
          top: rect.top - gap,
          left: rect.right,
        });
      } else {
        setMenuPosition({
          top: rect.bottom + gap,
          left: rect.right,
        });
      }
    }
  }, [isOpen, externalDiffCommands?.length]);

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node | null;
      if (!target) return;
      if (toggleRef.current?.contains(target)) return;
      if (menuRef.current?.contains(target)) return;
      setIsOpen(false);
    };

    if (isOpen) {
      document.addEventListener('click', handleClickOutside, true);
    }

    return () => {
      document.removeEventListener('click', handleClickOutside, true);
    };
  }, [isOpen]);

  const handleViewInBrowser = (event: React.MouseEvent<HTMLButtonElement>) => {
    event.stopPropagation();
    setIsOpen(false);
    navigate(`/diff/${workspace.id}`);
  };

  const handleExternalDiff = async (cmd: ExternalDiffCommand, event: React.MouseEvent<HTMLButtonElement>) => {
    event.stopPropagation();
    setIsOpen(false);
    setExecuting(cmd.name);

    try {
      const response = await diffExternal(workspace.id, cmd.command);
      setResult({ success: response.success, message: response.message });
    } catch (err) {
      setResult({ success: false, message: getErrorMessage(err, 'Failed to open diff tool') });
    } finally {
      setExecuting(null);
    }
  };

  const hasUserCommands = externalDiffCommands && externalDiffCommands.length > 0;

  const menu = isOpen && !executing && (
    <div
      ref={menuRef}
      className={`spawn-dropdown__menu spawn-dropdown__menu--portal${placementAbove ? ' spawn-dropdown__menu--above' : ''}`}
      role="menu"
      style={{
        position: 'fixed',
        top: placementAbove ? 'auto' : `${menuPosition.top}px`,
        bottom: placementAbove ? `${window.innerHeight - menuPosition.top}px` : 'auto',
        right: `${window.innerWidth - menuPosition.left}px`,
      }}
    >
      <button
        className="spawn-dropdown__item"
        onClick={handleViewInBrowser}
        role="menuitem"
      >
        <span className="spawn-dropdown__item-label">View in browser</span>
      </button>

      <div className="spawn-dropdown__separator" role="separator"></div>

      {BUILTIN_DIFF_COMMANDS.map((cmd) => (
        <button
          key={`builtin-${cmd.name}`}
          className="spawn-dropdown__item"
          onClick={(e) => handleExternalDiff(cmd, e)}
          role="menuitem"
        >
          <span className="spawn-dropdown__item-label">{cmd.name}</span>
        </button>
      ))}

      {hasUserCommands && (
        <>
          <div className="spawn-dropdown__separator" role="separator"></div>
          {externalDiffCommands.map((cmd) => (
            <button
              key={cmd.name}
              className="spawn-dropdown__item"
              onClick={(e) => handleExternalDiff(cmd, e)}
              role="menuitem"
            >
              <span className="spawn-dropdown__item-label">{cmd.name}</span>
            </button>
          ))}
        </>
      )}
    </div>
  );

  const closeModal = () => setResult(null);

  return (
    <>
      <button
        ref={toggleRef}
        className="btn btn--sm btn--ghost btn--bordered"
        disabled={executing !== null}
        onClick={(e) => {
          e.stopPropagation();
          setIsOpen(!isOpen);
        }}
        aria-expanded={isOpen}
        aria-haspopup="menu"
        aria-label="View diff options"
      >
        {executing ? (
          <div className="spinner--small"></div>
        ) : (
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"></path>
            <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"></path>
          </svg>
        )}
      </button>
      {menu && createPortal(menu, document.body)}

      {result && (
        <div className="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="diff-modal-title">
          <div className="modal">
            <div className="modal__header">
              <h2 className="modal__title" id="diff-modal-title">
                {result.success ? 'Diff tool opened' : 'Failed to open diff tool'}
              </h2>
            </div>
            <div className="modal__body">
              <p>{result.message}</p>
            </div>
            <div className="modal__footer">
              <button className="btn btn--primary" onClick={closeModal}>
                OK
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
