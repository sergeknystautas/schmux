import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { spawnSessions, getErrorMessage } from '../lib/api';
import { getSpawnEntries, recordSpawnEntryUse } from '../lib/spawn-api';
import { useToast } from './ToastProvider';
import { useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import { getQuickLaunchItems } from '../lib/quicklaunch';
import type { SpawnEntry } from '../lib/types.generated';
import type { WorkspaceResponse } from '../lib/types';
import styles from './ActionDropdown.module.css';

type ActionDropdownProps = {
  workspace: WorkspaceResponse;
  onClose: () => void;
  style?: React.CSSProperties;
  placementAbove?: boolean;
};

function ProvenanceMarker({ source }: { source: string }) {
  switch (source) {
    case 'built-in':
      return (
        <span className={styles.marker} title="Built-in">
          &#9632;
        </span>
      );
    case 'emerged':
      return (
        <span className={styles.marker} title="Emerged">
          &#9673;
        </span>
      );
    case 'manual':
      return (
        <span className={styles.marker} title="Manual">
          &#9675;
        </span>
      );
    default:
      return null;
  }
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
  const [entries, setEntries] = useState<SpawnEntry[]>([]);

  useEffect(() => {
    getSpawnEntries(repoName)
      .then(setEntries)
      .catch(() => {});
  }, [repoName]);

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

  const handleEntrySpawn = async (entry: SpawnEntry, e: React.MouseEvent) => {
    e.stopPropagation();
    onClose();

    try {
      const response = await spawnSessions({
        repo: workspace.repo,
        branch: workspace.branch,
        prompt: entry.type === 'skill' || entry.type === 'agent' ? entry.prompt || '' : '',
        nickname: entry.name,
        workspace_id: workspace.id,
        command: entry.type === 'command' || entry.type === 'shell' ? entry.command : undefined,
        targets: entry.target ? { [entry.target]: 1 } : undefined,
      });

      const result = response[0];
      if (result.error) {
        toastError(`Failed to spawn ${entry.name}: ${result.error}`);
      } else {
        success(`Spawned ${entry.name} session`);
        recordSpawnEntryUse(repoName, entry.id).catch(() => {});
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
    navigate('/config?tab=sessions');
  };

  const handleManageEmerged = (e: React.MouseEvent) => {
    e.stopPropagation();
    onClose();
    navigate(`/autolearn?repo=${encodeURIComponent(repoName)}&tab=actions`);
  };

  const handleCreateAction = (e: React.MouseEvent) => {
    e.stopPropagation();
    onClose();
    navigate(`/autolearn?repo=${encodeURIComponent(repoName)}&tab=actions&create=1`);
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
      {entries.length > 0 ? (
        entries.map((entry) => (
          <button
            key={entry.id}
            className={styles.item}
            onClick={(e) => handleEntrySpawn(entry, e)}
            role="menuitem"
          >
            <span className={styles.itemLabel}>{entry.name}</span>
            <ProvenanceMarker source={entry.source} />
          </button>
        ))
      ) : (
        <div className={styles.emptyState}>No emerged actions yet</div>
      )}

      {/* Create action */}
      <div className={styles.separator} role="separator" />
      <button
        className={`${styles.item} ${styles.addAction}`}
        onClick={handleCreateAction}
        role="menuitem"
      >
        <span className={styles.itemLabel}>+ Create action</span>
      </button>
    </div>
  );
}
