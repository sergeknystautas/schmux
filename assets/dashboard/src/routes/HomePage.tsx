import React, { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useSessions } from '../contexts/SessionsContext';
import { useConfig, useRequireConfig } from '../contexts/ConfigContext';
import { useFeatures } from '../contexts/FeaturesContext';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import Tooltip from '../components/Tooltip';
import {
  scanWorkspaces,
  getRecentBranches,
  refreshRecentBranches,
  prepareBranchSpawn,
  getPRs,
  refreshPRs,
  checkoutPR,
  getOverlays,
  dismissOverlayNudge,
  dismissRemoteHost,
  getErrorMessage,
  linearSyncFromMain,
  getCommitGraph,
  getSubreddit,
  getRepofeedList,
} from '../lib/api';
import { navigateToWorkspace, usePendingNavigation } from '../lib/navigation';
import { useFloorManager } from '../hooks/useFloorManager';
import { useTerminalStream } from '../hooks/useTerminalStream';
import type {
  WorkspaceResponse,
  RecentBranch,
  PullRequest,
  OverlayInfo,
  SubredditResponse,
  RepofeedListResponse,
} from '../lib/types';
import { ArrowDownIcon, ArrowUpIcon } from '../components/Icons';
import RecyclableIndicator from '../components/RecyclableIndicator';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import styles from '../styles/home.module.css';

// Helper to format relative date from ISO string
function formatRelativeDate(isoDate: string): string {
  const date = new Date(isoDate);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  const diffHours = Math.floor(diffMs / 3600000);
  const diffDays = Math.floor(diffMs / 86400000);

  if (diffMins < 1) return 'just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 7) return `${diffDays}d ago`;
  if (diffDays < 30) return `${Math.floor(diffDays / 7)}w ago`;
  return date.toLocaleDateString();
}

// Helper to format absolute time (e.g., "2:45 PM")
function formatAbsoluteTime(isoDate: string): string {
  const date = new Date(isoDate);
  return date.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
}

// SVG Icons
const GitBranchIcon = () => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth="1.5"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <circle cx="4" cy="4" r="2" />
    <circle cx="4" cy="12" r="2" />
    <circle cx="12" cy="4" r="2" />
    <path d="M4 6v4M12 6c0 3-2 4-6 4" />
  </svg>
);

const PlusIcon = () => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
  >
    <path d="M8 3v10M3 8h10" />
  </svg>
);

const RocketIcon = () => (
  <svg
    width="18"
    height="18"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <path d="M4.5 16.5c-1.5 1.26-2 5-2 5s3.74-.5 5-2c.71-.84.7-2.13-.09-2.91a2.18 2.18 0 0 0-2.91-.09z" />
    <path d="m12 15-3-3a22 22 0 0 1 2-3.95A12.88 12.88 0 0 1 22 2c0 2.72-.78 7.5-6 11a22.35 22.35 0 0 1-4 2z" />
    <path d="M9 12H4s.55-3.03 2-4c1.62-1.08 5 0 5 0" />
    <path d="M12 15v5s3.03-.55 4-2c1.08-1.62 0-5 0-5" />
  </svg>
);

const FolderIcon = () => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth="1.5"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <path d="M2 4a1 1 0 0 1 1-1h3l2 2h5a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V4z" />
  </svg>
);

const ScanIcon = () => (
  <svg
    width="14"
    height="14"
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth="1.5"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <circle cx="8" cy="8" r="6" />
    <path d="M8 2v6l4 2" />
  </svg>
);

const ChevronRightIcon = () => (
  <svg
    width="14"
    height="14"
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <path d="M6 4l4 4-4 4" />
  </svg>
);

const TerminalIcon = () => (
  <svg
    width="18"
    height="18"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <polyline points="4 17 10 11 4 5" />
    <line x1="12" y1="19" x2="20" y2="19" />
  </svg>
);

const CloseIcon = () => (
  <svg
    width="14"
    height="14"
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <path d="M4 4l8 8M12 4l-8 8" />
  </svg>
);

const GitPullRequestIcon = () => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth="1.5"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <circle cx="4" cy="4" r="2" />
    <circle cx="4" cy="12" r="2" />
    <circle cx="12" cy="12" r="2" />
    <path d="M4 6v4M12 6v4" />
    <path d="M12 4V4a2 2 0 0 0-2-2H8" />
  </svg>
);

const RefreshIcon = () => (
  <svg
    width="14"
    height="14"
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth="1.5"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <path d="M2 8a6 6 0 0 1 10.3-4.2L14 2v4h-4l1.7-1.7A4.5 4.5 0 0 0 3.5 8" />
    <path d="M14 8a6 6 0 0 1-10.3 4.2L2 14v-4h4l-1.7 1.7A4.5 4.5 0 0 0 12.5 8" />
  </svg>
);

const arrowDown = (
  <span
    style={{
      position: 'relative',
      top: -3,
      left: -2,
      width: 7,
      height: 7,
      display: 'inline-block',
    }}
  >
    {ArrowDownIcon}
  </span>
);
const arrowUp = (
  <span
    style={{
      position: 'relative',
      top: -3,
      left: -2,
      width: 7,
      height: 7,
      display: 'inline-block',
    }}
  >
    {ArrowUpIcon}
  </span>
);

export default function HomePage() {
  useRequireConfig();
  const {
    workspaces,
    loading: sessionsLoading,
    connected,
    subredditUpdateCount,
    repofeedUpdateCount,
  } = useSessions();
  const { config, loading: configLoading, getRepoName } = useConfig();
  const { features } = useFeatures();

  // dashboard.sx alerts
  const dxStatus = config.dashboard_sx_status;
  const dxAlerts: { key: string; text: string }[] = [];
  if (dxStatus) {
    if (dxStatus.last_heartbeat_status && dxStatus.last_heartbeat_status !== 200) {
      const time = dxStatus.last_heartbeat_time
        ? new Date(dxStatus.last_heartbeat_time).toLocaleString()
        : 'unknown';
      dxAlerts.push({
        key: 'heartbeat',
        text: `heartbeat: ${time} ${dxStatus.last_heartbeat_status} ${dxStatus.last_heartbeat_error || ''}`.trim(),
      });
    }
    if (dxStatus.cert_expires_at) {
      const daysLeft = Math.ceil(
        (new Date(dxStatus.cert_expires_at).getTime() - Date.now()) / (1000 * 60 * 60 * 24)
      );
      if (daysLeft <= 30) {
        dxAlerts.push({
          key: 'cert',
          text: `certificate: ${dxStatus.cert_domain || 'unknown'} expires in ${daysLeft} days`,
        });
      }
    }
  }
  const { success, error: toastError } = useToast();
  const { alert, confirm } = useModal();
  const { setPendingNavigation } = usePendingNavigation();
  const navigate = useNavigate();

  const [scanning, setScanning] = useState(false);
  const [recentBranches, setRecentBranches] = useState<RecentBranch[]>([]);
  const [branchesLoading, setBranchesLoading] = useState(true);
  const [branchesRefreshing, setBranchesRefreshing] = useState(false);
  const [preparingBranch, setPreparingBranch] = useState<string | null>(null);
  const [pullRequests, setPullRequests] = useState<PullRequest[]>([]);
  const [prsLoading, setPrsLoading] = useState(true);
  const [prsRefreshing, setPrsRefreshing] = useState(false);
  const [checkingOutPR, setCheckingOutPR] = useState<string | null>(null);
  const [heroDismissed, setHeroDismissed] = useState(() => {
    return localStorage.getItem('home-hero-dismissed') === 'true';
  });
  const [overlays, setOverlays] = useState<OverlayInfo[]>([]);
  const [dismissedNudges, setDismissedNudges] = useState<Set<string>>(new Set());
  const [subreddit, setSubreddit] = useState<SubredditResponse | null>(null);
  const [repofeedList, setRepofeedList] = useState<RepofeedListResponse | null>(null);
  const [activeRepoTab, setActiveRepoTab] = useState<string>('');
  const activeRepo =
    subreddit?.repos?.find((r) => r.slug === activeRepoTab) ?? subreddit?.repos?.[0] ?? null;

  // Floor manager
  const fm = useFloorManager();
  const { containerRef: fmTerminalRef } = useTerminalStream({
    sessionId: fm.enabled && fm.running ? fm.tmuxSession : null,
    useWebGL: config.xterm?.use_webgl !== false,
  });

  const handleDismissHero = () => {
    setHeroDismissed(true);
    localStorage.setItem('home-hero-dismissed', 'true');
  };

  // Fetch overlay info for nudge banners
  useEffect(() => {
    (async () => {
      try {
        const data = await getOverlays();
        setOverlays(data.overlays || []);
      } catch (err) {
        // Non-critical nudge — log for debugging
        console.debug('Failed to fetch overlays:', err);
      }
    })();
  }, []);

  // Fetch subreddit digest (on mount + when WebSocket signals an update)
  useEffect(() => {
    (async () => {
      try {
        const data = await getSubreddit();
        setSubreddit(data);
        // Set active tab to first repo if not already set
        if (data.repos && data.repos.length > 0) {
          setActiveRepoTab((prev) => prev || data.repos![0].slug);
        }
      } catch (err) {
        // Non-critical — log for debugging
        console.debug('Failed to fetch subreddit:', err);
      }
    })();
  }, [subredditUpdateCount]);

  // Fetch repofeed list (on mount + when WebSocket signals an update)
  useEffect(() => {
    getRepofeedList()
      .then(setRepofeedList)
      .catch(() => {});
  }, [repofeedUpdateCount]);

  const handleDismissNudge = async (repoName: string) => {
    setDismissedNudges((prev) => new Set(prev).add(repoName));
    try {
      await dismissOverlayNudge(repoName);
    } catch (err) {
      // Banner is already hidden locally — log for debugging
      console.debug('Failed to dismiss overlay nudge:', err);
    }
  };

  // Fetch recent branches on mount
  const fetchBranches = useCallback(async () => {
    setBranchesLoading(true);
    try {
      const branches = await getRecentBranches(10);
      setRecentBranches(branches || []);
    } catch (err) {
      console.error('Failed to fetch recent branches:', err);
    } finally {
      setBranchesLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchBranches();
  }, [fetchBranches]);

  // Fetch PRs on mount
  useEffect(() => {
    (async () => {
      setPrsLoading(true);
      try {
        const result = await getPRs();
        setPullRequests(result.prs || []);
      } catch (err) {
        console.error('Failed to fetch PRs:', err);
      } finally {
        setPrsLoading(false);
      }
    })();
  }, []);

  const handleRefreshPRs = async () => {
    setPrsRefreshing(true);
    try {
      const result = await refreshPRs();
      setPullRequests(result.prs || []);
      if (result.error) {
        alert('PR Refresh Failed', result.error);
      } else {
        success(
          `Found ${result.fetched_count} pull request${result.fetched_count !== 1 ? 's' : ''}`
        );
      }
    } catch (err) {
      alert('PR Refresh Failed', getErrorMessage(err, 'Failed to refresh PRs'));
    } finally {
      setPrsRefreshing(false);
    }
  };

  const handleRefreshBranches = async () => {
    setBranchesRefreshing(true);
    try {
      const result = await refreshRecentBranches();
      setRecentBranches(result.branches || []);
      success(
        `Found ${result.fetched_count} recent branch${result.fetched_count !== 1 ? 'es' : ''}`
      );
    } catch (err) {
      alert('Branch Refresh Failed', getErrorMessage(err, 'Failed to refresh branches'));
    } finally {
      setBranchesRefreshing(false);
    }
  };

  const hasPrReviewTarget = () => {
    if (!config) return false;
    return (config.pr_review?.target?.trim() ?? '') !== '';
  };

  const handlePRClick = async (pr: PullRequest) => {
    if (!hasPrReviewTarget()) {
      toastError('No PR review target configured. Set pr_review.target in config.');
      return;
    }
    const checkoutKey = `${pr.repo_url}#${pr.number}`;
    setCheckingOutPR(checkoutKey);
    try {
      const result = await checkoutPR(pr.repo_url, pr.number);
      setPendingNavigation({ type: 'session', id: result.session_id });
      setCheckingOutPR(null);
    } catch (err) {
      alert('PR Checkout Failed', getErrorMessage(err, 'Failed to checkout PR'));
      setCheckingOutPR(null);
    }
  };

  // Handle scan workspaces
  const handleScan = async () => {
    setScanning(true);
    try {
      const result = await scanWorkspaces();
      const changes =
        (result.added?.length || 0) + (result.updated?.length || 0) + (result.removed?.length || 0);
      if (changes > 0) {
        success(`Scan complete: ${changes} change${changes !== 1 ? 's' : ''} found`);
      } else {
        success('Scan complete: no changes');
      }
    } catch (err) {
      alert('Workspace Scan Failed', getErrorMessage(err, 'Failed to scan workspaces'));
    } finally {
      setScanning(false);
    }
  };

  // Handle pull all workspaces that are behind
  const [pulling, setPulling] = useState(false);
  const handlePull = async () => {
    const behindWorkspaces = workspaces.filter((ws) => ws.behind > 0);
    if (behindWorkspaces.length === 0) {
      success('No workspaces behind');
      return;
    }
    setPulling(true);
    let pulled = 0;
    let failed = 0;
    for (const ws of behindWorkspaces) {
      try {
        const graph = await getCommitGraph(ws.id, { maxTotal: 1, mainContext: 1 });
        const hash = graph.main_ahead_next_hash;
        if (!hash) {
          failed++;
          continue;
        }
        await linearSyncFromMain(ws.id, hash);
        pulled++;
      } catch {
        failed++;
      }
    }
    setPulling(false);
    if (failed > 0) {
      alert(
        'Pull Failed',
        `Pulled ${pulled} workspace${pulled !== 1 ? 's' : ''}, ${failed} failed`
      );
    } else {
      success(`Pulled ${pulled} workspace${pulled !== 1 ? 's' : ''}`);
    }
  };

  const handleBranchClick = async (repoName: string, branchName: string) => {
    const key = `${repoName}:${branchName}`;
    setPreparingBranch(key);
    try {
      const result = await prepareBranchSpawn(repoName, branchName);
      navigate('/spawn', { state: result });
    } catch (err) {
      alert('Branch Spawn Failed', getErrorMessage(err, 'Failed to prepare branch spawn'));
      setPreparingBranch(null);
    }
  };

  const handleWorkspaceClick = (workspaceId: string) => {
    navigateToWorkspace(navigate, workspaces, workspaceId);
  };

  const handleDismissRemoteWorkspace = async (e: React.MouseEvent, ws: WorkspaceResponse) => {
    e.stopPropagation(); // Don't navigate to workspace
    if (!ws.remote_host_id) return;
    const accepted = await confirm('This will remove the workspace and all its sessions.', {
      danger: true,
      confirmText: 'Dismiss',
    });
    if (!accepted) return;
    try {
      await dismissRemoteHost(ws.remote_host_id);
      success('Remote workspace dismissed');
    } catch (err) {
      await alert('Dismiss Failed', getErrorMessage(err, 'Failed to dismiss remote workspace'));
    }
  };

  const loading = sessionsLoading || configLoading;

  // Shared sidebar content: workspaces, connection, tips
  const sidebarContent = (
    <>
      <div className={styles.sectionCard}>
        <div className={styles.sectionHeader}>
          <h2 className={styles.sectionTitle}>
            <FolderIcon />
            Active Workspaces ({workspaces.length})
          </h2>
          <div className={styles.headerActions}>
            <Tooltip content="Sync workspaces that are behind main">
              <span>
                <button
                  className={styles.scanButton}
                  onClick={handlePull}
                  disabled={pulling || workspaces.filter((ws) => ws.behind > 0).length === 0}
                  data-testid="pull-workspaces"
                >
                  {pulling ? 'Pulling...' : 'Pull'}
                </button>
              </span>
            </Tooltip>
            <Tooltip content="Scan for workspace changes">
              <span>
                <button
                  className={styles.scanButton}
                  onClick={handleScan}
                  disabled={scanning}
                  data-testid="scan-workspaces"
                >
                  <ScanIcon />
                  {scanning ? 'Scanning...' : 'Scan'}
                </button>
              </span>
            </Tooltip>
          </div>
        </div>

        <div className={styles.sectionContent}>
          {loading ? (
            <div className={styles.loadingState}>
              <div className="spinner spinner--small" />
              <span>Loading workspaces...</span>
            </div>
          ) : workspaces.length === 0 ? (
            <div className={styles.emptyState}>
              <p className={styles.emptyStateText}>No active workspaces</p>
              <p className={styles.emptyStateHint}>
                Spawn a session to create your first workspace
              </p>
            </div>
          ) : (
            <div className={styles.workspaceTable} data-testid="workspace-list">
              <div className={styles.tableBody}>
                {workspaces.map((ws) => {
                  const runningCount = ws.sessions.filter((s) => s.running).length;
                  const isRemoteDead =
                    ws.remote_host_id &&
                    (ws.remote_host_status === 'expired' ||
                      ws.remote_host_status === 'disconnected');
                  return (
                    <button
                      key={ws.id}
                      className={styles.workspaceRow}
                      onClick={() => handleWorkspaceClick(ws.id)}
                      type="button"
                      data-testid={`workspace-${ws.id}`}
                    >
                      <div className={styles.workspaceInfo}>
                        <span className={styles.workspaceBranch}>{ws.branch}</span>
                        <span className={styles.workspaceRepo}>{getRepoName(ws.repo)}</span>
                      </div>
                      <div className={styles.workspaceStats}>
                        {isRemoteDead && (
                          <span
                            className={styles.scanButton}
                            role="button"
                            tabIndex={0}
                            onClick={(e) => handleDismissRemoteWorkspace(e, ws)}
                            onKeyDown={(e) => {
                              if (e.key === 'Enter' || e.key === ' ') {
                                e.preventDefault();
                                handleDismissRemoteWorkspace(e as unknown as React.MouseEvent, ws);
                              }
                            }}
                            style={{ fontSize: '0.7rem', color: 'var(--color-error)' }}
                            data-testid={`dismiss-workspace-${ws.id}`}
                          >
                            Dismiss
                          </span>
                        )}
                        <span className={styles.gitStats} data-testid="git-stats">
                          <span className="inline-flex" style={{ gap: 1 }}>
                            {ws.behind}
                            {arrowDown}
                          </span>{' '}
                          <span className="inline-flex" style={{ gap: 1 }}>
                            {ws.ahead}
                            {arrowUp}
                          </span>
                        </span>
                        {runningCount > 0 && (
                          <span className={styles.runningBadge}>{runningCount}</span>
                        )}
                      </div>
                    </button>
                  );
                })}
              </div>
            </div>
          )}
        </div>
      </div>

      <RecyclableIndicator />

      {/* Connection Status */}
      {!loading && (
        <div className={styles.connectionStatus}>
          <span
            className={`${styles.connectionDot} ${connected ? styles.connectionDotConnected : styles.connectionDotDisconnected}`}
          />
          <span className={styles.connectionText}>
            {connected ? 'Live updates' : 'Reconnecting...'}
          </span>
        </div>
      )}

      {/* Tips */}
      <div className={styles.tipsCard}>
        <div className={styles.tipItem}>
          <span className={styles.tipKey}>Tip:</span>
          <span className={styles.tipText}>
            Use <code>tmux -L schmux attach -t SESSION_NAME</code> to connect directly from terminal
          </span>
        </div>
      </div>
    </>
  );

  // FM-enabled layout: terminal takes center stage
  if (fm.enabled) {
    return (
      <div className={`${styles.homePage} ${styles.homePageFM}`}>
        {/* FM Terminal */}
        <div className={styles.fmTerminalColumn}>
          <div className={`${styles.sectionCard} flex-1 flex-col`}>
            <div className={styles.sectionHeader}>
              <h2 className={styles.sectionTitle}>
                <TerminalIcon />
                Floor Manager
              </h2>
              {fm.running && (
                <span className={styles.runningBadge}>{fm.injectionCount} signals</span>
              )}
            </div>
            <div
              className="log-viewer__output terminal-xterm flex-1"
              ref={fmTerminalRef}
              data-testid="fm-terminal"
              style={{ minHeight: 400 }}
            />
            {!fm.running && (
              <div className={styles.fmLoading}>
                <div className="spinner spinner--small" />
                <span>Starting floor manager...</span>
              </div>
            )}
          </div>
        </div>

        {/* Sidebar */}
        <div className={styles.fmSideColumn}>
          {dxAlerts.length > 0 && (
            <div
              className="banner banner--warning mb-md"
              data-testid="dashboardsx-alerts"
              style={{ flexDirection: 'column', alignItems: 'flex-start' }}
            >
              <strong>dashboard.sx alerts</strong>
              {dxAlerts.map((a) => (
                <div key={a.key}>{a.text}</div>
              ))}
            </div>
          )}
          {!heroDismissed && (
            <div className={styles.heroSection}>
              <button
                className={styles.heroDismiss}
                onClick={handleDismissHero}
                title="Dismiss"
                aria-label="Dismiss hero banner"
              >
                <CloseIcon />
              </button>
              <div className={styles.heroContent}>
                <h1 className={styles.heroTitle}>
                  <span className={styles.heroIcon}>
                    <TerminalIcon />
                  </span>
                  schmux
                </h1>
                <p className={styles.heroSubtitle}>
                  Multi-agent orchestration for AI coding assistants
                </p>
              </div>
            </div>
          )}
          {sidebarContent}

          {/* Repofeed Summary */}
          {features.repofeed && repofeedList && repofeedList.repos.length > 0 && (
            <div className={styles.sectionCard}>
              <div className={styles.sectionHeader}>
                <h2 className={styles.sectionTitle}>
                  <span style={{ fontSize: '1.1em' }}>📡</span>
                  Also Active
                </h2>
                <Link to="/repofeed" className={styles.sectionLink}>
                  View full repofeed →
                </Link>
              </div>
              <div className={styles.sectionContent}>
                {repofeedList.repos.map((repo) => (
                  <div
                    key={repo.slug}
                    style={{
                      fontSize: '0.875rem',
                      color: 'var(--text-secondary)',
                      padding: '0.25rem 0',
                    }}
                  >
                    <strong>{repo.name}</strong>: {repo.active_intents} developer
                    {repo.active_intents !== 1 ? 's' : ''} working
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Subreddit Digest */}
          {features.subreddit && subreddit?.enabled && (
            <div className={styles.sectionCard}>
              <div className={styles.sectionHeader}>
                <h2 className={styles.sectionTitle}>
                  <span style={{ fontSize: '1.1em' }}>📢</span>
                  r/schmux
                </h2>
              </div>
              {subreddit.repos && subreddit.repos.length > 1 && (
                <div className={styles.subredditTabs}>
                  {subreddit.repos.map((repo) => (
                    <button
                      key={repo.slug}
                      className={`${styles.subredditTab} ${activeRepo?.slug === repo.slug ? styles.subredditTabActive : ''}`}
                      onClick={() => setActiveRepoTab(repo.slug)}
                    >
                      {repo.name}
                    </button>
                  ))}
                </div>
              )}
              <div className={`${styles.sectionContent} ${styles.subredditScroll}`}>
                {subreddit.repos && subreddit.repos.length > 0 ? (
                  <div className={styles.subredditPosts}>
                    {activeRepo &&
                      activeRepo.posts.map((post, index) => (
                        <div
                          key={`${activeRepo.slug}-${post.id}-${index}`}
                          className={styles.subredditPost}
                        >
                          <div className={styles.postHeader}>
                            <h4 className={styles.postTitle}>{post.title}</h4>
                            <span className={styles.postTime}>
                              {formatRelativeDate(post.created_at)}
                            </span>
                          </div>
                          <div className={styles.postBody}>
                            <ReactMarkdown remarkPlugins={[remarkGfm]}>
                              {post.content}
                            </ReactMarkdown>
                          </div>
                          <div className={styles.postFooter}>
                            <span className={styles.postVotes}>{'★'.repeat(post.upvotes)}</span>
                            {post.revision > 1 && (
                              <span className={styles.postUpdated}>
                                · Updated {post.revision - 1}x
                              </span>
                            )}
                          </div>
                        </div>
                      ))}
                  </div>
                ) : (
                  <div
                    className={styles.subredditContent}
                    style={{ opacity: 0.6, fontStyle: 'italic' }}
                  >
                    {subreddit.next_generation_at
                      ? `Generating digest... (scheduled ${formatAbsoluteTime(subreddit.next_generation_at)})`
                      : 'Generating digest...'}
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      </div>
    );
  }

  // Standard layout (FM disabled)

  return (
    <div className={styles.homePage}>
      {/* Left Column - Quick Actions */}
      <div className={styles.leftColumn}>
        {dxAlerts.length > 0 && (
          <div
            className="banner banner--warning mb-md"
            data-testid="dashboardsx-alerts"
            style={{ flexDirection: 'column', alignItems: 'flex-start' }}
          >
            <strong>dashboard.sx alerts</strong>
            {dxAlerts.map((a) => (
              <div key={a.key}>{a.text}</div>
            ))}
          </div>
        )}
        {/* Hero Section - dismissable */}
        {!heroDismissed && (
          <div className={styles.heroSection}>
            <button
              className={styles.heroDismiss}
              onClick={handleDismissHero}
              title="Dismiss"
              aria-label="Dismiss hero banner"
            >
              <CloseIcon />
            </button>
            <div className={styles.heroContent}>
              <h1 className={styles.heroTitle}>
                <span className={styles.heroIcon}>
                  <TerminalIcon />
                </span>
                schmux
              </h1>
              <p className={styles.heroSubtitle}>
                Multi-agent orchestration for AI coding assistants
              </p>
            </div>
          </div>
        )}

        {/* Primary Action - Spawn New Session (only when no workspaces) */}
        {workspaces.length === 0 && (
          <Link to="/spawn" className={styles.primaryAction} data-testid="spawn-new-session">
            <span className={styles.primaryActionIcon}>
              <RocketIcon />
            </span>
            <span className={styles.primaryActionText}>
              <span className={styles.primaryActionTitle}>Spawn New Session</span>
              <span className={styles.primaryActionHint}>Start your first AI coding session</span>
            </span>
            <span className={styles.primaryActionArrow}>
              <ChevronRightIcon />
            </span>
          </Link>
        )}

        {/* Recent Branches Section */}
        <div className={styles.sectionCard} data-testid="recent-branches">
          <div className={styles.sectionHeader}>
            <h2 className={styles.sectionTitle}>
              <GitBranchIcon />
              Recent Branches
            </h2>
            <button
              className={styles.scanButton}
              onClick={handleRefreshBranches}
              disabled={branchesRefreshing}
              title="Refresh branches from remote"
            >
              <RefreshIcon />
              {branchesRefreshing ? 'Refreshing...' : 'Refresh'}
            </button>
          </div>
          <div className={styles.sectionContent}>
            {branchesLoading ? (
              <div className={styles.loadingState}>
                <div className="spinner spinner--small" />
                <span>Loading branches...</span>
              </div>
            ) : recentBranches.length === 0 ? (
              <div className={styles.placeholderState}>
                <p className={styles.placeholderText}>No branches found yet.</p>
                <p className={styles.placeholderHint}>
                  Branches will appear after the first fetch completes.
                </p>
              </div>
            ) : (
              <div className={styles.branchList}>
                {recentBranches.slice(0, 5).map((branch, idx) => {
                  const key = `${branch.repo_name}:${branch.branch}`;
                  const isPreparing = preparingBranch === key;
                  return (
                    <button
                      key={`${branch.repo_name}-${branch.branch}-${idx}`}
                      className={styles.branchItem}
                      data-testid="branch-item"
                      onClick={() => handleBranchClick(branch.repo_name, branch.branch)}
                      title={`Spawn session on ${branch.branch}`}
                      disabled={!!preparingBranch}
                    >
                      <div className={styles.branchRow1}>
                        <span className={styles.branchName}>
                          {branch.branch}
                          {isPreparing && (
                            <span className={styles.branchSpinner}>
                              <div className="spinner spinner--small" />
                            </span>
                          )}
                        </span>
                        <span className={styles.branchRepo}>{branch.repo_name}</span>
                        <span className={styles.branchDate}>
                          {formatRelativeDate(branch.commit_date)}
                        </span>
                      </div>
                      <div className={styles.branchRow2}>
                        <span className={styles.branchSubject}>{branch.subject}</span>
                      </div>
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        </div>

        {/* Pull Requests Section */}
        {features.github && (
          <div className={styles.sectionCard}>
            <div className={styles.sectionHeader}>
              <h2 className={styles.sectionTitle}>
                <GitPullRequestIcon />
                Pull Requests
              </h2>
              <button
                className={styles.scanButton}
                onClick={handleRefreshPRs}
                disabled={prsRefreshing}
                title="Refresh pull requests from GitHub"
              >
                <RefreshIcon />
                {prsRefreshing ? 'Refreshing...' : 'Refresh'}
              </button>
            </div>
            <div className={styles.sectionContent}>
              {prsLoading ? (
                <div className={styles.loadingState}>
                  <div className="spinner spinner--small" />
                  <span>Loading pull requests...</span>
                </div>
              ) : pullRequests.length === 0 ? (
                <div className={styles.placeholderState}>
                  <p className={styles.placeholderText}>No open pull requests found.</p>
                  <p className={styles.placeholderHint}>
                    PRs from public GitHub repos will appear here.
                  </p>
                </div>
              ) : (
                <div className={styles.branchList}>
                  {pullRequests.map((pr) => {
                    const checkoutKey = `${pr.repo_url}#${pr.number}`;
                    const isCheckingOut = checkingOutPR === checkoutKey;
                    const isBusy = checkingOutPR !== null;
                    const canCheckout = hasPrReviewTarget();
                    return (
                      <div
                        key={checkoutKey}
                        className={styles.branchItem}
                        onClick={() => {
                          if (isBusy) return;
                          if (!canCheckout) {
                            toastError(
                              'No PR review target configured. Set pr_review.target in config.'
                            );
                            return;
                          }
                          handlePRClick(pr);
                        }}
                        onKeyDown={(event) => {
                          if (isBusy) return;
                          if (event.key === 'Enter' || event.key === ' ') {
                            event.preventDefault();
                            if (!canCheckout) {
                              toastError(
                                'No PR review target configured. Set pr_review.target in config.'
                              );
                              return;
                            }
                            handlePRClick(pr);
                          }
                        }}
                        role="button"
                        tabIndex={0}
                        aria-disabled={isBusy || !canCheckout}
                        data-disabled={!canCheckout}
                        data-busy={isBusy}
                        title={`Review PR #${pr.number}: ${pr.title}`}
                      >
                        <div className={styles.branchRow1}>
                          <span className={styles.branchName}>
                            <a
                              href={pr.html_url}
                              target="_blank"
                              rel="noopener noreferrer"
                              onClick={(e) => e.stopPropagation()}
                              style={{ color: 'inherit', textDecoration: 'none' }}
                            >
                              #{pr.number}
                            </a>{' '}
                            {pr.title}
                            {isCheckingOut && (
                              <span className={styles.branchSpinner}>
                                <div className="spinner spinner--small" />
                              </span>
                            )}
                          </span>
                          <span className={styles.branchRepo}>{pr.repo_name}</span>
                          <span className={styles.branchDate}>
                            {formatRelativeDate(pr.created_at)}
                          </span>
                        </div>
                        <div className={styles.branchRow2}>
                          <span className={styles.branchSubject}>
                            {pr.source_branch} &rarr; {pr.target_branch} &middot; @{pr.author}
                          </span>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          </div>
        )}

        {/* Subreddit Digest Section */}
        {features.subreddit && subreddit?.enabled && (
          <div className={styles.sectionCard}>
            <div className={styles.sectionHeader}>
              <h2 className={styles.sectionTitle}>
                <span style={{ fontSize: '1.1em' }}>📢</span>
                r/schmux
              </h2>
              {subreddit.next_generation_at && (
                <span className={styles.subredditMeta}>
                  Next: {formatRelativeDate(subreddit.next_generation_at)}
                </span>
              )}
            </div>
            <div className={`${styles.sectionContent} ${styles.subredditScroll}`}>
              {subreddit.repos && subreddit.repos.length > 0 ? (
                <>
                  {subreddit.repos.length > 1 && (
                    <div className={styles.subredditTabs}>
                      {subreddit.repos.map((repo) => (
                        <button
                          key={repo.slug}
                          className={`${styles.subredditTab} ${activeRepo?.slug === repo.slug ? styles.subredditTabActive : ''}`}
                          onClick={() => setActiveRepoTab(repo.slug)}
                        >
                          {repo.name}
                        </button>
                      ))}
                    </div>
                  )}
                  <div className={styles.subredditPosts}>
                    {activeRepo &&
                      activeRepo.posts.map((post, index) => (
                        <div
                          key={`${activeRepo.slug}-${post.id}-${index}`}
                          className={styles.subredditPost}
                        >
                          <div className={styles.postHeader}>
                            <h4 className={styles.postTitle}>{post.title}</h4>
                            <span className={styles.postTime}>
                              {formatRelativeDate(post.created_at)}
                            </span>
                          </div>
                          <div className={styles.postBody}>
                            <ReactMarkdown remarkPlugins={[remarkGfm]}>
                              {post.content}
                            </ReactMarkdown>
                          </div>
                          <div className={styles.postFooter}>
                            <span className={styles.postVotes}>{'★'.repeat(post.upvotes)}</span>
                            {post.revision > 1 && (
                              <span className={styles.postUpdated}>
                                · Updated {post.revision - 1}x
                              </span>
                            )}
                          </div>
                        </div>
                      ))}
                  </div>
                </>
              ) : (
                <div
                  className={styles.subredditContent}
                  style={{ opacity: 0.6, fontStyle: 'italic' }}
                >
                  {subreddit.next_generation_at
                    ? `Generating digest... (scheduled ${formatAbsoluteTime(subreddit.next_generation_at)})`
                    : 'Generating digest...'}
                </div>
              )}
            </div>
          </div>
        )}
      </div>

      {/* Right Column - Workspaces */}
      <div className={styles.rightColumn}>
        <div className={styles.sectionCard}>
          <div className={styles.sectionHeader}>
            <h2 className={styles.sectionTitle}>
              <FolderIcon />
              Active Workspaces ({workspaces.length})
            </h2>
            <div className={styles.headerActions}>
              <Tooltip content="Sync workspaces that are behind main">
                <span>
                  <button
                    className={styles.scanButton}
                    onClick={handlePull}
                    disabled={pulling || workspaces.filter((ws) => ws.behind > 0).length === 0}
                    data-testid="pull-workspaces"
                  >
                    {pulling ? 'Pulling...' : 'Pull'}
                  </button>
                </span>
              </Tooltip>
              <Tooltip content="Scan for workspace changes">
                <span>
                  <button
                    className={styles.scanButton}
                    onClick={handleScan}
                    disabled={scanning}
                    data-testid="scan-workspaces"
                  >
                    <ScanIcon />
                    {scanning ? 'Scanning...' : 'Scan'}
                  </button>
                </span>
              </Tooltip>
            </div>
          </div>

          <div className={styles.sectionContent}>
            {loading ? (
              <div className={styles.loadingState}>
                <div className="spinner spinner--small" />
                <span>Loading workspaces...</span>
              </div>
            ) : workspaces.length === 0 ? (
              <div className={styles.emptyState}>
                <p className={styles.emptyStateText}>No active workspaces</p>
                <p className={styles.emptyStateHint}>
                  Spawn a session to create your first workspace
                </p>
              </div>
            ) : (
              <div className={styles.workspaceTable} data-testid="workspace-list">
                <div className={styles.tableBody}>
                  {workspaces.map((ws) => {
                    const runningCount = ws.sessions.filter((s) => s.running).length;
                    const isRemoteDead =
                      ws.remote_host_id &&
                      (ws.remote_host_status === 'expired' ||
                        ws.remote_host_status === 'disconnected');
                    return (
                      <button
                        key={ws.id}
                        className={styles.workspaceRow}
                        onClick={() => handleWorkspaceClick(ws.id)}
                        type="button"
                        data-testid={`workspace-${ws.id}`}
                      >
                        <div className={styles.workspaceInfo}>
                          <span className={styles.workspaceBranch}>{ws.branch}</span>
                          <span className={styles.workspaceRepo}>{getRepoName(ws.repo)}</span>
                        </div>
                        <div className={styles.workspaceStats}>
                          {isRemoteDead && (
                            <span
                              className={styles.scanButton}
                              role="button"
                              tabIndex={0}
                              onClick={(e) => handleDismissRemoteWorkspace(e, ws)}
                              onKeyDown={(e) => {
                                if (e.key === 'Enter' || e.key === ' ') {
                                  e.preventDefault();
                                  handleDismissRemoteWorkspace(
                                    e as unknown as React.MouseEvent,
                                    ws
                                  );
                                }
                              }}
                              style={{ fontSize: '0.7rem', color: 'var(--color-error)' }}
                              data-testid={`dismiss-workspace-${ws.id}`}
                            >
                              Dismiss
                            </span>
                          )}
                          <span className={styles.gitStats} data-testid="git-stats">
                            <span className="inline-flex" style={{ gap: 1 }}>
                              {ws.behind}
                              {arrowDown}
                            </span>{' '}
                            <span className="inline-flex" style={{ gap: 1 }}>
                              {ws.ahead}
                              {arrowUp}
                            </span>
                          </span>
                          {runningCount > 0 && (
                            <span className={styles.runningBadge}>{runningCount}</span>
                          )}
                        </div>
                      </button>
                    );
                  })}
                </div>
              </div>
            )}
          </div>
        </div>

        <RecyclableIndicator />

        {/* Overlay Nudge Banners */}
        {overlays
          .filter((o) => {
            if (o.nudge_dismissed || dismissedNudges.has(o.repo_name)) return false;
            // Show only when the repo has no repo-specific overlay files (only builtins)
            const hasRepoSpecific = o.declared_paths.some((p) => p.source !== 'builtin');
            return !hasRepoSpecific;
          })
          .map((o) => (
            <div
              key={o.repo_name}
              style={{
                display: 'flex',
                alignItems: 'flex-start',
                gap: 'var(--spacing-sm)',
                padding: 'var(--spacing-sm) var(--spacing-md)',
                background: 'var(--color-surface)',
                border: '1px solid var(--color-border)',
                borderRadius: 'var(--radius-md)',
                fontSize: '0.8rem',
                lineHeight: 1.5,
                color: 'var(--color-text-muted)',
              }}
            >
              <span className="flex-1">
                Overlay is active for <strong>{o.repo_name}</strong>. Agent config files
                (.claude/settings.local.json) are automatically synced across workspaces.{' '}
                <Link
                  to="/overlays"
                  style={{ color: 'var(--color-accent)', textDecoration: 'none' }}
                >
                  Manage overlays &rarr;
                </Link>
              </span>
              <button
                onClick={() => handleDismissNudge(o.repo_name)}
                title="Dismiss"
                aria-label={`Dismiss overlay nudge for ${o.repo_name}`}
                style={{
                  flexShrink: 0,
                  background: 'transparent',
                  border: 'none',
                  cursor: 'pointer',
                  color: 'var(--color-text-faint)',
                  padding: '2px',
                  lineHeight: 1,
                }}
              >
                <CloseIcon />
              </button>
            </div>
          ))}

        {/* Connection Status */}
        {!loading && (
          <div className={styles.connectionStatus}>
            <span
              className={`${styles.connectionDot} ${connected ? styles.connectionDotConnected : styles.connectionDotDisconnected}`}
            />
            <span className={styles.connectionText}>
              {connected ? 'Live updates' : 'Reconnecting...'}
            </span>
          </div>
        )}

        {/* Tips */}
        <div className={styles.tipsCard}>
          <div className={styles.tipItem}>
            <span className={styles.tipKey}>Tip:</span>
            <span className={styles.tipText}>
              Use <code>tmux -L schmux attach -t SESSION_NAME</code> to connect directly from terminal
            </span>
          </div>
        </div>
      </div>
    </div>
  );
}
