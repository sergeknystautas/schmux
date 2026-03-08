import React, { useState, useEffect, useCallback } from 'react';
import { useSessions } from '../contexts/SessionsContext';
import { getRepofeedList, getRepofeedRepo } from '../lib/api';
import type { RepofeedListResponse, RepofeedRepoResponse, RepofeedIntentEntry } from '../lib/types';
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

function IntentCard({ intent }: { intent: RepofeedIntentEntry }) {
  return (
    <div className={styles['repofeed-intent']}>
      <div className={styles['repofeed-intent__status']}>
        <span className={statusDotClass(intent.status)} />
      </div>
      <div className={styles['repofeed-intent__body']}>
        <div className={styles['repofeed-intent__developer']}>
          {intent.display_name || intent.developer}
          {intent.session_count > 0 &&
            ` · ${intent.session_count} session${intent.session_count !== 1 ? 's' : ''}`}
        </div>
        <div className={styles['repofeed-intent__text']}>{intent.intent}</div>
        <div className={styles['repofeed-intent__meta']}>
          {intent.branches?.map((b) => (
            <span key={b} className={styles['repofeed-intent__branch']}>
              {b}
            </span>
          ))}
          {intent.agents?.length > 0 && <span>{intent.agents.join(', ')}</span>}
          {intent.started && <span>{timeAgo(intent.started)}</span>}
        </div>
      </div>
    </div>
  );
}

export default function RepofeedPage() {
  const { repofeedUpdateCount } = useSessions();
  const [repoList, setRepoList] = useState<RepofeedListResponse | null>(null);
  const [selectedSlug, setSelectedSlug] = useState<string | null>(null);
  const [repoDetail, setRepoDetail] = useState<RepofeedRepoResponse | null>(null);
  const [filter, setFilter] = useState<FilterKind>('all');
  const [loading, setLoading] = useState(true);

  // Fetch repo list
  const fetchList = useCallback(() => {
    getRepofeedList()
      .then((data) => {
        setRepoList(data);
        if (!selectedSlug && data.repos.length > 0) {
          setSelectedSlug(data.repos[0].slug);
        }
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [selectedSlug]);

  useEffect(() => {
    fetchList();
  }, [fetchList, repofeedUpdateCount]);

  // Fetch repo detail when slug changes
  useEffect(() => {
    if (!selectedSlug) return;
    getRepofeedRepo(selectedSlug)
      .then(setRepoDetail)
      .catch(() => setRepoDetail(null));
  }, [selectedSlug, repofeedUpdateCount]);

  const filteredIntents = (repoDetail?.intents || []).filter((intent) => {
    if (filter === 'all') return true;
    if (filter === 'active') return intent.status === 'active' || intent.status === 'inactive';
    if (filter === 'completed') return intent.status === 'completed';
    return true;
  });

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

      {/* Repo tabs */}
      {repoList && repoList.repos.length > 0 && (
        <div className={styles['repofeed-page__tabs']}>
          {repoList.repos.map((repo) => (
            <button
              key={repo.slug}
              className={`${styles['repofeed-page__tab']} ${selectedSlug === repo.slug ? styles['repofeed-page__tab--active'] : ''}`}
              onClick={() => setSelectedSlug(repo.slug)}
            >
              {repo.name}
              {repo.active_intents > 0 && (
                <span className={styles['repofeed-page__badge']}>{repo.active_intents}</span>
              )}
            </button>
          ))}
        </div>
      )}

      {/* Filter chips */}
      <div className={styles['repofeed-page__filters']}>
        {(['all', 'active', 'completed'] as FilterKind[]).map((kind) => (
          <button
            key={kind}
            className={`${styles['repofeed-page__chip']} ${filter === kind ? styles['repofeed-page__chip--active'] : ''}`}
            onClick={() => setFilter(kind)}
          >
            {kind === 'all' ? 'All' : kind === 'active' ? 'In Progress' : 'Landed'}
          </button>
        ))}
      </div>

      {/* Intent list */}
      {filteredIntents.length === 0 ? (
        <div className={styles['repofeed-page__empty']}>
          {repoList?.repos.length === 0
            ? 'No repofeed data yet. Enable repofeed in settings to start publishing.'
            : 'No matching activities.'}
        </div>
      ) : (
        <div className={styles['repofeed-page__list']}>
          {filteredIntents.map((intent) => (
            <IntentCard key={`${intent.developer}-${intent.intent}`} intent={intent} />
          ))}
        </div>
      )}
    </div>
  );
}
