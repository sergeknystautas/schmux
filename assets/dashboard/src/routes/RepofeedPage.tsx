import React, { useState, useEffect, useCallback } from 'react';
import { useSessions } from '../contexts/SessionsContext';
import {
  getRepofeedOutgoing,
  getRepofeedIncoming,
  setIntentShared,
  dismissRepofeedIntent,
} from '../lib/api';
import type { RepofeedOutgoingEntry, RepofeedIncomingEntry } from '../lib/api';
import type { WorkspaceResponse } from '../lib/types';
import { workspaceDisplayLabel } from '../lib/workspace-display';
import styles from '../styles/repofeed.module.css';

type FilterKind = 'all' | 'active' | 'completed';

function statusPillVariant(status: string): string {
  switch (status) {
    case 'active':
      return 'status-pill--running';
    case 'inactive':
      return 'status-pill--stopped';
    case 'completed':
      return 'status-pill--stopped';
    default:
      return 'status-pill--stopped';
  }
}

function statusLabel(status: string): string {
  switch (status) {
    case 'active':
      return 'active';
    case 'inactive':
      return 'idle';
    case 'completed':
      return 'finished';
    default:
      return status;
  }
}

function timeAgo(started: string): string {
  if (!started) return '';
  try {
    const diff = Date.now() - new Date(started).getTime();
    const hours = Math.floor(diff / (1000 * 60 * 60));
    if (hours < 1) {
      const mins = Math.floor(diff / (1000 * 60));
      return `${mins}m ago`;
    }
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    return `${days}d ago`;
  } catch {
    return '';
  }
}

function OutgoingCard({
  ws,
  summary,
  onToggle,
}: {
  ws: WorkspaceResponse;
  summary?: string;
  onToggle: () => void;
}) {
  const isShared = ws.intent_shared;
  const sessionCount = ws.sessions?.length ?? 0;
  const statusText = ws.backburner ? 'idle' : sessionCount > 0 ? 'active' : 'idle';

  return (
    <div className={`${styles.intent} ${!isShared ? styles.intentMuted : ''}`}>
      {isShared ? (
        <span
          className={`status-pill ${statusPillVariant(sessionCount > 0 && !ws.backburner ? 'active' : 'inactive')}`}
        >
          <span className="status-pill__dot" />
          {statusText}
        </span>
      ) : (
        <span className={styles.intentLock}>🔒 Private</span>
      )}
      <div className={styles.intentBody}>
        <div className={styles.intentDeveloper}>{ws.id}</div>
        <div className={styles.intentText}>
          {isShared && summary ? summary : workspaceDisplayLabel(ws)}
        </div>
      </div>
      <button
        className="btn btn--sm btn--primary"
        onClick={onToggle}
        title={isShared ? 'Stop sharing activity' : 'Share activity with team'}
      >
        {isShared ? 'Stop sharing activity' : 'Share activity'}
      </button>
    </div>
  );
}

function IncomingCard({
  intent,
  onDismiss,
}: {
  intent: RepofeedIncomingEntry;
  onDismiss?: () => void;
}) {
  return (
    <div className={styles.intent}>
      <span className={`status-pill ${statusPillVariant(intent.status)}`}>
        <span className="status-pill__dot" />
        {statusLabel(intent.status)}
      </span>
      <div className={styles.intentBody}>
        <div className={styles.intentText}>{intent.intent}</div>
        {intent.started && (
          <div className={styles.intentMeta}>
            <span>{timeAgo(intent.started)}</span>
          </div>
        )}
      </div>
      {intent.status === 'completed' && onDismiss && (
        <button className="btn btn--sm btn--ghost" onClick={onDismiss} title="Dismiss">
          Dismiss
        </button>
      )}
    </div>
  );
}

export default function RepofeedPage() {
  const { workspaces, repofeedUpdateCount } = useSessions();
  const [outgoingSummaries, setOutgoingSummaries] = useState<Record<string, string>>({});
  const [incomingIntents, setIncomingIntents] = useState<RepofeedIncomingEntry[]>([]);
  const [filter, setFilter] = useState<FilterKind>('all');
  const [loading, setLoading] = useState(true);

  const fetchData = useCallback(() => {
    Promise.all([getRepofeedOutgoing(), getRepofeedIncoming()])
      .then(([outgoing, incoming]) => {
        const summaryMap: Record<string, string> = {};
        for (const e of outgoing.entries) {
          if (e.summary) summaryMap[e.workspace_id] = e.summary;
        }
        setOutgoingSummaries(summaryMap);
        setIncomingIntents(incoming.entries);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    fetchData();
  }, [fetchData, repofeedUpdateCount]);

  const handleToggleShare = async (wsId: string, currentlyShared: boolean) => {
    try {
      await setIntentShared(wsId, !currentlyShared);
    } catch {
      // error already handled by parseErrorResponse
    }
  };

  const handleDismiss = async (developer: string, workspaceId: string) => {
    try {
      await dismissRepofeedIntent(developer, workspaceId);
      fetchData();
    } catch {
      // error already handled
    }
  };

  const filteredIntents = incomingIntents.filter((intent) => {
    if (filter === 'all') return true;
    if (filter === 'active') return intent.status === 'active' || intent.status === 'inactive';
    if (filter === 'completed') return intent.status === 'completed';
    return true;
  });

  // Group incoming by developer
  const byDeveloper = new Map<string, RepofeedIncomingEntry[]>();
  for (const intent of filteredIntents) {
    const key = intent.display_name || intent.developer;
    if (!byDeveloper.has(key)) byDeveloper.set(key, []);
    byDeveloper.get(key)!.push(intent);
  }

  if (loading) {
    return (
      <div className="loading-state">
        <div className="spinner"></div>
        <span>Loading...</span>
      </div>
    );
  }

  return (
    <>
      <div className="app-header">
        <div className="app-header__info">
          <h1 className="app-header__meta">Repofeed</h1>
        </div>
      </div>

      <div className={styles.page}>
        {/* Outgoing section */}
        <div className="settings-section">
          <div className="settings-section__header">
            <h3 className="settings-section__title">Outgoing</h3>
          </div>
          <div className="settings-section__body">
            {workspaces.length === 0 ? (
              <div className="empty-state">
                <p className="empty-state__description">No workspaces.</p>
              </div>
            ) : (
              <div className={styles.list}>
                {[...workspaces]
                  .sort((a, b) => a.id.localeCompare(b.id))
                  .map((ws) => (
                    <OutgoingCard
                      key={ws.id}
                      ws={ws}
                      summary={outgoingSummaries[ws.id]}
                      onToggle={() => handleToggleShare(ws.id, !!ws.intent_shared)}
                    />
                  ))}
              </div>
            )}
          </div>
        </div>

        {/* Incoming section */}
        <div className="settings-section">
          <div className="settings-section__header">
            <h3 className="settings-section__title">Incoming</h3>
          </div>
          <div className="settings-section__body">
            {/* Filter chips */}
            <div className={styles.filters}>
              {(['all', 'active', 'completed'] as FilterKind[]).map((kind) => (
                <button
                  key={kind}
                  className={`btn btn--sm ${filter === kind ? 'btn--primary' : 'btn--secondary'}`}
                  onClick={() => setFilter(kind)}
                >
                  {kind === 'all' ? 'All' : kind === 'active' ? 'In Progress' : 'Finished'}
                </button>
              ))}
            </div>

            {/* Intent list grouped by developer */}
            {filteredIntents.length === 0 ? (
              <div className="empty-state">
                <p className="empty-state__description">No incoming activity yet.</p>
              </div>
            ) : (
              <div className={styles.list}>
                {Array.from(byDeveloper.entries()).map(([developer, intents]) => (
                  <div key={developer}>
                    <div className={styles.developerHeader}>{developer}</div>
                    {intents.map((intent) => (
                      <IncomingCard
                        key={`${intent.developer}-${intent.intent}-${intent.workspace_id || ''}`}
                        intent={intent}
                        onDismiss={
                          intent.status === 'completed'
                            ? () => handleDismiss(intent.developer, intent.workspace_id || '')
                            : undefined
                        }
                      />
                    ))}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </>
  );
}
