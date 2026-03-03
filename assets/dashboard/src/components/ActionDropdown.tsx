import React, { useEffect, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { spawnSessions, getErrorMessage } from '../lib/api';
import { useToast } from './ToastProvider';
import { useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import { useActions } from '../hooks/useActions';
import { getQuickLaunchItems } from '../lib/quicklaunch';
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
  const { config } = useConfig();
  const { waitForSession } = useSessions();
  const repoName = workspace.repo_name || workspace.repo;
  const { actions, refetch } = useActions(repoName);

  useEffect(() => {
    refetch();
  }, [refetch]);

  const quickLaunch = useMemo(() => {
    const globalNames = (config?.quick_launch || []).map((item) => item.name);
    return getQuickLaunchItems(globalNames, workspace.quick_launch || []);
  }, [config?.quick_launch, workspace.quick_launch]);

  const handleCustomSpawn = (e: React.MouseEvent) => {
    e.stopPropagation();
    onClose();
    navigate(`/spawn?workspace_id=${workspace.id}`);
  };

  const handleQuickLaunchSpawn = async (name: string, e: React.MouseEvent) => {
    e.stopPropagation();
    onClose();

    try {
      const response = await spawnSessions({
        repo: workspace.repo,
        branch: workspace.branch,
        prompt: '',
        nickname: name,
        workspace_id: workspace.id,
        quick_launch_name: name,
      });

      const result = response[0];
      if (result.error) {
        toastError(`Failed to spawn ${name}: ${result.error}`);
      } else {
        success(`Spawned ${name} session`);
        await waitForSession(result.session_id!);
        navigate(`/sessions/${result.session_id!}`);
      }
    } catch (err) {
      toastError(`Failed to spawn: ${getErrorMessage(err, 'Unknown error')}`);
    }
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

  const handleManageQuickLaunch = (e: React.MouseEvent) => {
    e.stopPropagation();
    onClose();
    navigate('/config?tab=quicklaunch');
  };

  const handleManageEmerged = (e: React.MouseEvent) => {
    e.stopPropagation();
    onClose();
    navigate(`/lore?repo=${encodeURIComponent(repoName)}&tab=actions`);
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

      {/* Quick Launch section */}
      <div className={styles.separator} role="separator" />
      <div className={styles.sectionHeader}>
        <span className={styles.sectionLabel}>Quick Launch</span>
        <button className={styles.manageLink} onClick={handleManageQuickLaunch}>
          manage
        </button>
      </div>
      {quickLaunch.length > 0 ? (
        quickLaunch.map((item) => (
          <button
            key={item.name}
            className={styles.item}
            onClick={(e) => handleQuickLaunchSpawn(item.name, e)}
            role="menuitem"
          >
            <span className={styles.itemLabel}>{item.name}</span>
          </button>
        ))
      ) : (
        <div className={styles.emptyState}>No presets configured</div>
      )}

      {/* Emerged actions section */}
      <div className={styles.separator} role="separator" />
      <div className={styles.sectionHeader}>
        <span className={styles.sectionLabel}>Emerged</span>
        <button className={styles.manageLink} onClick={handleManageEmerged}>
          manage
        </button>
      </div>
      {actions.length > 0 ? (
        actions.map((action) => (
          <button
            key={action.id}
            className={styles.item}
            onClick={(e) => handleActionSpawn(action, e)}
            role="menuitem"
          >
            <span className={styles.itemLabel}>{action.name}</span>
            <ConfidenceDots confidence={action.confidence} type={action.type} />
          </button>
        ))
      ) : (
        <div className={styles.emptyState}>No emerged actions yet</div>
      )}
    </div>
  );
}
