import React, { useState, useRef, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate } from 'react-router-dom';
import { spawnSessions } from '../lib/api.js';
import { useToast } from './ToastProvider.jsx';

export default function SpawnDropdown({ workspace, commands, disabled }) {
  const { success, error: toastError } = useToast();
  const navigate = useNavigate();
  const [isOpen, setIsOpen] = useState(false);
  const [spawning, setSpawning] = useState(false);
  const [menuPosition, setMenuPosition] = useState({ top: 0, left: 0 });
  const toggleRef = useRef(null);

  // Calculate menu position when dropdown opens
  useEffect(() => {
    if (isOpen && toggleRef.current) {
      const rect = toggleRef.current.getBoundingClientRect();
      setMenuPosition({
        top: rect.bottom + 4,
        left: rect.right,
      });
    }
  }, [isOpen]);

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (event) => {
      // Check if click is outside the toggle button
      if (toggleRef.current && !toggleRef.current.contains(event.target)) {
        setIsOpen(false);
      }
    };

    if (isOpen) {
      document.addEventListener('click', handleClickOutside);
    }

    return () => {
      document.removeEventListener('click', handleClickOutside);
    };
  }, [isOpen]);

  const handleCustomSpawn = (event) => {
    event.stopPropagation();
    setIsOpen(false);
    navigate(`/spawn?workspace_id=${workspace.id}`);
  };

  const handleCommandSpawn = async (command, event) => {
    event.stopPropagation();
    setIsOpen(false);
    setSpawning(true);

    try {
      const response = await spawnSessions({
        repo: workspace.repo,
        branch: workspace.branch,
        prompt: '',
        nickname: '',
        agents: { [command.name]: 1 },
        workspace_id: workspace.id,
      });

      const result = response[0];
      if (result.error) {
        toastError(`Failed to spawn ${command.name}: ${result.error}`);
      } else {
        success(`Spawned ${command.name} session`);
        navigate(`/sessions/${result.session_id}`);
      }
    } catch (err) {
      toastError(`Failed to spawn: ${err.message}`);
    } finally {
      setSpawning(false);
    }
  };

  const hasCommands = commands && commands.length > 0;

  const menu = isOpen && !spawning && (
    <div
      className="spawn-dropdown__menu spawn-dropdown__menu--portal"
      role="menu"
      style={{
        position: 'fixed',
        top: `${menuPosition.top}px`,
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

      {hasCommands && (
        <>
          <div className="spawn-dropdown__separator" role="separator"></div>
          {commands.map((command) => (
            <button
              key={command.name}
              className="spawn-dropdown__item"
              onClick={(e) => handleCommandSpawn(command, e)}
              role="menuitem"
            >
              <span className="spawn-dropdown__item-label">{command.name}</span>
              <span className="spawn-dropdown__item-hint mono">{command.command}</span>
            </button>
          ))}
        </>
      )}

      {!hasCommands && (
        <div className="spawn-dropdown__empty">
          No commands configured
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
