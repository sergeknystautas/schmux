import React, { useState, useRef, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate } from 'react-router-dom';
import { spawnSessions, getErrorMessage } from '../lib/api'
import { useToast } from './ToastProvider'
import { useSessions } from '../contexts/SessionsContext'
import type { QuickLaunchPreset, WorkspaceResponse } from '../lib/types';

type SpawnDropdownProps = {
  workspace: WorkspaceResponse;
  quickLaunch: QuickLaunchPreset[];
  disabled?: boolean;
};

export default function SpawnDropdown({ workspace, quickLaunch, disabled }: SpawnDropdownProps) {
  const { success, error: toastError } = useToast();
  const { refresh, waitForSession } = useSessions();
  const navigate = useNavigate();
  const [isOpen, setIsOpen] = useState(false);
  const [spawning, setSpawning] = useState(false);
  const [menuPosition, setMenuPosition] = useState({ top: 0, left: 0 });
  const [placementAbove, setPlacementAbove] = useState(false);
  const toggleRef = useRef<HTMLButtonElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);

  // Calculate menu position when dropdown opens
  useEffect(() => {
    if (isOpen && toggleRef.current) {
      const rect = toggleRef.current.getBoundingClientRect();
      const gap = 4;
      // Estimate menu height based on items, or use actual measurement if available
      const estimatedMenuHeight = menuRef.current?.offsetHeight ||
        Math.min(300, 60 + (quickLaunch?.length || 0) * 52 + 40);

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
  }, [isOpen, quickLaunch?.length]);

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node | null;
      if (!target) return;
      // Keep open if click is inside toggle or menu
      if (toggleRef.current?.contains(target)) return;
      if (menuRef.current?.contains(target)) return;
      setIsOpen(false);
    };

    if (isOpen) {
      // Use capture phase so we receive the event before stopPropagation() on other buttons
      document.addEventListener('click', handleClickOutside, true);
    }

    return () => {
      document.removeEventListener('click', handleClickOutside, true);
    };
  }, [isOpen]);

  const handleCustomSpawn = (event: React.MouseEvent<HTMLButtonElement>) => {
    event.stopPropagation();
    setIsOpen(false);
    navigate(`/spawn?workspace_id=${workspace.id}`);
  };

  const handleQuickLaunchSpawn = async (preset: QuickLaunchPreset, event: React.MouseEvent<HTMLButtonElement>) => {
    event.stopPropagation();
    setIsOpen(false);
    setSpawning(true);

    try {
      const response = await spawnSessions({
        repo: workspace.repo,
        branch: workspace.branch,
        prompt: preset.prompt || '',
        nickname: preset.name,
        targets: { [preset.target]: 1 },
        workspace_id: workspace.id,
      });

      const result = response[0];
      if (result.error) {
        toastError(`Failed to spawn ${preset.name}: ${result.error}`);
      } else {
        success(`Spawned ${preset.name} session`);
        refresh();
        await waitForSession(result.session_id);
        navigate(`/sessions/${result.session_id}`);
      }
    } catch (err) {
      toastError(`Failed to spawn: ${getErrorMessage(err, 'Unknown error')}`);
    } finally {
      setSpawning(false);
    }
  };

  const hasQuickLaunch = quickLaunch && quickLaunch.length > 0;

  const menu = isOpen && !spawning && (
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
        onClick={handleCustomSpawn}
        role="menuitem"
      >
        <span className="spawn-dropdown__item-label">Custom…</span>
        <span className="spawn-dropdown__item-hint">Open spawn wizard</span>
      </button>

      {hasQuickLaunch && (
        <>
          <div className="spawn-dropdown__separator" role="separator"></div>
          {quickLaunch.map((preset) => (
            <button
              key={preset.name}
              className="spawn-dropdown__item"
              onClick={(e) => handleQuickLaunchSpawn(preset, e)}
              role="menuitem"
            >
              <span className="spawn-dropdown__item-label">{preset.name}</span>
              <span className="spawn-dropdown__item-hint mono">{preset.target}</span>
            </button>
          ))}
        </>
      )}

      {!hasQuickLaunch && (
        <div className="spawn-dropdown__empty">
          No quick launch presets
        </div>
      )}
    </div>
  );

  return (
    <>
      <button
        ref={toggleRef}
        className="btn btn--sm btn--primary spawn-dropdown__toggle"
        onClick={(e) => {
          e.stopPropagation();
          setIsOpen(!isOpen);
        }}
        disabled={disabled || spawning}
        aria-expanded={isOpen}
        aria-haspopup="menu"
      >
        {spawning ? (
          <>
            <span className="spinner spinner--small"></span>
            Spawning...
          </>
        ) : (
          <>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <circle cx="12" cy="12" r="10"></circle>
              <line x1="12" y1="8" x2="12" y2="16"></line>
              <line x1="8" y1="12" x2="16" y2="12"></line>
            </svg>
            Spawn
            <svg
              width="12"
              height="12"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              className={`spawn-dropdown__arrow${isOpen ? ' spawn-dropdown__arrow--open' : ''}`}
            >
              <polyline points="6 9 12 15 18 9"></polyline>
            </svg>
          </>
        )}
      </button>
      {menu && createPortal(menu, document.body)}
    </>
  );
}
