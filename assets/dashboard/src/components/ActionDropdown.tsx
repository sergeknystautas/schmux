import React, { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { spawnSessions, getErrorMessage } from '../lib/api';
import { useToast } from './ToastProvider';
import { useSessions } from '../contexts/SessionsContext';
import { useActions } from '../hooks/useActions';
import type { Action } from '../lib/types.generated';
import type { WorkspaceResponse } from '../lib/types';
import styles from './ActionDropdown.module.css';

type ActionDropdownProps = {
  workspace: WorkspaceResponse;
  onClose: () => void;
  style?: React.CSSProperties;
  placementAbove?: boolean;
};

function ConfidenceDots({ confidence, type }: { confidence: number; type: string }) {
  if (type !== 'agent') {
    return <span className={styles.marker}>&#9642;</span>;
  }
  const filled = Math.round(confidence * 4);
  return (
    <span className={styles.confidenceDots}>
      {[0, 1, 2, 3].map((i) => (
        <span key={i} className={`${styles.dot}${i < filled ? ` ${styles.dotFilled}` : ''}`} />
      ))}
    </span>
  );
}

export default function ActionDropdown({
  workspace,
  onClose,
  style,
  placementAbove,
}: ActionDropdownProps) {
  const navigate = useNavigate();
  const { success, error: toastError } = useToast();
  const { waitForSession } = useSessions();
  const { actions, refetch } = useActions(workspace.repo);

  useEffect(() => {
    refetch();
  }, [refetch]);

  const handleCustomSpawn = (e: React.MouseEvent) => {
    e.stopPropagation();
    onClose();
    navigate(`/spawn?workspace_id=${workspace.id}`);
  };

  const handleActionSpawn = async (action: Action, e: React.MouseEvent) => {
    e.stopPropagation();
    onClose();

    // For agent actions with unfilled parameters, navigate to spawn page with template.
    if (action.type === 'agent' && action.template) {
      const hasUnfilledParams = action.parameters && action.parameters.some((p) => !p.default);
      if (hasUnfilledParams) {
        const params = new URLSearchParams({
          workspace_id: workspace.id,
          prompt: action.template,
        });
        if (action.target) params.set('target', action.target);
        if (action.persona) params.set('persona', action.persona);
        navigate(`/spawn?${params.toString()}`);
        return;
      }
    }

    try {
      // Fill template with defaults.
      let prompt = action.template || '';
      if (action.parameters) {
        for (const p of action.parameters) {
          if (p.default) {
            prompt = prompt.replace(`{{${p.name}}}`, p.default);
          }
        }
      }

      const targets: Record<string, number> = {};
      const target = action.learned_target?.value || action.target;
      if (target) {
        targets[target] = 1;
      }

      const response = await spawnSessions({
        repo: workspace.repo,
        branch: workspace.branch,
        prompt: action.type === 'agent' ? prompt : '',
        nickname: action.name,
        workspace_id: workspace.id,
        targets: action.type === 'agent' && Object.keys(targets).length > 0 ? targets : undefined,
        command: action.type === 'command' || action.type === 'shell' ? action.command : undefined,
        action_id: action.id,
        persona_id: action.learned_persona?.value || action.persona || undefined,
      });

      const result = response[0];
      if (result.error) {
        toastError(`Failed to spawn ${action.name}: ${result.error}`);
      } else {
        success(`Spawned ${action.name} session`);
        await waitForSession(result.session_id!);
        navigate(`/sessions/${result.session_id!}`);
      }
    } catch (err) {
      toastError(`Failed to spawn: ${getErrorMessage(err, 'Unknown error')}`);
    }
  };

  const handleAddAction = (e: React.MouseEvent) => {
    e.stopPropagation();
    onClose();
    navigate('/config');
  };

  return (
    <div
      className={`${styles.menu}${placementAbove ? ` ${styles.menuAbove}` : ''}`}
      role="menu"
      style={style}
    >
      <button
        className={`${styles.item} ${styles.itemBold}`}
        onClick={handleCustomSpawn}
        role="menuitem"
      >
        <span className={styles.itemLabel}>Spawn a session...</span>
        <span className={styles.itemHint}>wizard</span>
      </button>

      {actions.length > 0 && (
        <>
          <div className={styles.separator} role="separator" />
          {actions.map((action) => (
            <button
              key={action.id}
              className={styles.item}
              onClick={(e) => handleActionSpawn(action, e)}
              role="menuitem"
            >
              <span className={styles.itemLabel}>{action.name}</span>
              <ConfidenceDots confidence={action.confidence} type={action.type} />
            </button>
          ))}
        </>
      )}

      <div className={styles.separator} role="separator" />
      <button
        className={`${styles.item} ${styles.addAction}`}
        onClick={handleAddAction}
        role="menuitem"
      >
        + Add action
      </button>
    </div>
  );
}
