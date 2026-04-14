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
import styles from '../styles/repofeed.module.css';

type FilterKind = 'all' | 'active' | 'completed';

function statusDotClass(status: string): string {
  switch (status) {
    case 'active':
      return `${styles['repofeed-intent__dot']} ${styles['repofeed-intent__dot--active']}`;
    case 'inactive':
      return `${styles['repofeed-intent__dot']} ${styles['repofeed-intent__dot--inactive']}`;
    case 'completed':
      return `${styles['repofeed-intent__dot']} ${styles['repofeed-intent__dot--completed']}`;
    default:
      return styles['repofeed-intent__dot'];
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
    <div
      className={`${styles['repofeed-intent']} ${!isShared ? styles['repofeed-intent--muted'] : ''}`}
    >
      <div className={styles['repofeed-intent__status']}>
        {isShared ? (
          <span
            className={statusDotClass(sessionCount > 0 && !ws.backburner ? 'active' : 'inactive')}
          />
        ) : (
          <span className={styles['repofeed-intent__lock']}>🔒</span>
        )}
      </div>
      <div className={styles['repofeed-intent__body']}>
        <div className={styles['repofeed-intent__developer']}>
          {ws.id}
          {isShared && ` · ${statusText}`}
        </div>
        <div className={styles['repofeed-intent__text']}>
          {isShared && summary ? summary : ws.branch}
        </div>
      </div>
      <button
        className={styles['repofeed-intent__toggle']}
        onClick={onToggle}
        title={isShared ? 'Stop sharing' : 'Share with team'}
      >
        {isShared ? 'Unshare' : 'Share'}
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
    <div className={styles['repofeed-intent']}>
      <div className={styles['repofeed-intent__status']}>
        <span className={statusDotClass(intent.status)} />
      </div>
      <div className={styles['repofeed-intent__body']}>
        <div className={styles['repofeed-intent__developer']}>
          {intent.display_name || intent.developer}
          {` · ${statusLabel(intent.status)}`}
        </div>
        <div className={styles['repofeed-intent__text']}>{intent.intent}</div>
        <div className={styles['repofeed-intent__meta']}>
          {intent.started && <span>{timeAgo(intent.started)}</span>}
        </div>
      </div>
      {intent.status === 'completed' && onDismiss && (
        <button className={styles['repofeed-intent__toggle']} onClick={onDismiss} title="Dismiss">
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
      <div className={styles['repofeed-page']}>
        <div className={styles['repofeed-page__empty']}>Loading...</div>
      </div>
    );
  }

  return (
    <div className={styles['repofeed-page']}>
      <div className={styles['repofeed-page__header']}>
        <h2 className={styles['repofeed-page__title']}>Repofeed</h2>
      </div>

      {/* Outgoing section */}
      <div className={styles['repofeed-page__section']}>
        <h3 className={styles['repofeed-page__section-title']}>Outgoing</h3>
        {workspaces.length === 0 ? (
          <div className={styles['repofeed-page__empty']}>No workspaces.</div>
        ) : (
          <div className={styles['repofeed-page__list']}>
            {workspaces.map((ws) => (
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

      {/* Incoming section */}
      <div className={styles['repofeed-page__section']}>
        <h3 className={styles['repofeed-page__section-title']}>Incoming</h3>

        {/* Filter chips */}
        <div className={styles['repofeed-page__filters']}>
          {(['all', 'active', 'completed'] as FilterKind[]).map((kind) => (
            <button
              key={kind}
              className={`${styles['repofeed-page__chip']} ${filter === kind ? styles['repofeed-page__chip--active'] : ''}`}
              onClick={() => setFilter(kind)}
            >
              {kind === 'all' ? 'All' : kind === 'active' ? 'In Progress' : 'Finished'}
            </button>
          ))}
        </div>

        {/* Intent list grouped by developer */}
        {filteredIntents.length === 0 ? (
          <div className={styles['repofeed-page__empty']}>No incoming intents yet.</div>
        ) : (
          <div className={styles['repofeed-page__list']}>
            {Array.from(byDeveloper.entries()).map(([developer, intents]) =>
              intents.map((intent) => (
                <IncomingCard
                  key={`${intent.developer}-${intent.intent}-${intent.workspace_id || ''}`}
                  intent={intent}
                  onDismiss={
                    intent.status === 'completed'
                      ? () => handleDismiss(intent.developer, intent.workspace_id || '')
                      : undefined
                  }
                />
              ))
            )}
          </div>
        )}
      </div>
    </div>
  );
}
