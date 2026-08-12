import { useCallback, useEffect, useRef, useState } from 'react';
import { getGitHubConnectStatus, runGitHubConnect, getErrorMessage } from '../lib/api';
import type { GitHubConnectStatus, GitHubConnectResult } from '../lib/types.generated';
import useFocusTrap from '../hooks/useFocusTrap';
import styles from './GitHubConnectBanner.module.css';

const STEP_LABELS: Record<string, string> = {
  set_origin: 'Set git origin',
  create_repo: 'Create GitHub repository',
  update_config: 'Register repository in schmux config',
  link_workspaces: 'Link workspace to repository',
  initial_push: 'Push history to default branch',
};

/** Status-pill variant + label per executed step status. Status is never
 *  colour alone — every pill carries its own text. */
const RESULT_PILLS: Record<string, { variant: string; label: string }> = {
  done: { variant: 'status-pill--running', label: 'Done' },
  skipped: { variant: 'status-pill--stopped', label: 'Skipped' },
  failed: { variant: 'status-pill--error', label: 'Failed' },
  not_run: { variant: 'status-pill--stopped', label: 'Not run' },
};

function StatusPill({ variant, label }: { variant: string; label: string }) {
  return (
    <span className={`status-pill ${variant}`}>
      <span className="status-pill__dot" />
      {label}
    </span>
  );
}

interface GitHubConnectBannerProps {
  workspaceId: string;
}

export default function GitHubConnectBanner({ workspaceId }: GitHubConnectBannerProps) {
  const [open, setOpen] = useState(false);
  const [status, setStatus] = useState<GitHubConnectStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<GitHubConnectResult | null>(null);
  const [owner, setOwner] = useState('');
  const [name, setName] = useState('');
  const [visibility, setVisibility] = useState<'private' | 'public'>('private');
  const [defaultBranch, setDefaultBranch] = useState('main');
  const modalRef = useRef<HTMLDivElement>(null);

  useFocusTrap(modalRef, open);

  const fetchStatus = useCallback(
    async (surfaceError: boolean) => {
      try {
        const s = await getGitHubConnectStatus(workspaceId);
        setStatus(s);
        setName(s.name || '');
        setDefaultBranch(s.default_branch || 'main');
        if (s.owners && s.owners.length > 0) setOwner(s.owners[0]);
      } catch (err) {
        if (surfaceError) setError(getErrorMessage(err, 'Failed to load connect status'));
      }
    },
    [workspaceId]
  );

  // Prefetch on mount: the probes behind the GET (ls-remote, gh) take seconds
  // cold, so warm them while the user is still looking at the banner. The POST
  // re-detects server-side, so acting on a slightly stale plan is safe.
  useEffect(() => {
    void fetchStatus(false);
  }, [fetchStatus]);

  const close = useCallback(() => setOpen(false), []);

  // Escape closes the dialog, except mid-run where cancelling would strand the
  // pipeline's progress off-screen.
  useEffect(() => {
    if (!open) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !running) {
        e.preventDefault();
        close();
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [open, running, close]);

  const openDialog = useCallback(() => {
    setOpen(true);
    setError(null);
    setResult(null);
    if (!status) void fetchStatus(true);
  }, [status, fetchStatus]);

  const needs = (step: string) => status?.plan?.find((p) => p.step === step)?.needed ?? false;
  const needsCreate = needs('create_repo');
  const needsPush = needs('initial_push');
  const ghBlocked = needsCreate && !(status?.gh?.available ?? false);

  const submit = async () => {
    setRunning(true);
    setError(null);
    try {
      const r = await runGitHubConnect(workspaceId, {
        owner,
        name,
        visibility,
        default_branch: defaultBranch,
      });
      setResult(r);
      if (!r.success) {
        const failed = r.steps.find((s) => s.status === 'failed');
        setError(
          failed ? `${STEP_LABELS[failed.step] ?? failed.step}: ${failed.detail}` : 'Connect failed'
        );
      }
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to connect repository'));
    } finally {
      setRunning(false);
    }
  };

  return (
    <>
      <div className={`banner banner--info ${styles.banner}`}>
        <span className={styles.bannerText}>
          <span className={styles.bannerTitle}>This repository exists only in this workspace</span>
          <span className={styles.bannerHint}>
            Connect it to GitHub to push commits and open pull requests.
          </span>
        </span>
        <button type="button" className="btn btn--primary" onClick={openDialog}>
          Connect to GitHub
        </button>
      </div>

      {open && (
        <div
          className="modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-labelledby="github-connect-title"
        >
          <div ref={modalRef} className="modal modal--wide">
            <div className="modal__header">
              <h2 className="modal__title" id="github-connect-title">
                Connect to GitHub
              </h2>
            </div>

            <div className="modal__body">
              {!status && !error && (
                <p className={styles.loading}>
                  <span className="spinner spinner--small" />
                  Checking GitHub and the workspace&rsquo;s remote&hellip;
                </p>
              )}

              {status && !result && (
                <>
                  <ul className={styles.steps}>
                    {status.plan.map((p) => (
                      <li key={p.step} className={styles.step}>
                        <StatusPill
                          variant={p.needed ? 'status-pill--stopped' : 'status-pill--running'}
                          label={p.needed ? 'Pending' : 'Done'}
                        />
                        <span className={styles.stepBody}>
                          <span className={p.needed ? styles.stepLabel : styles.stepLabelMuted}>
                            {STEP_LABELS[p.step] ?? p.step}
                          </span>
                          <span className={styles.stepReason}>{p.reason}</span>
                        </span>
                      </li>
                    ))}
                  </ul>

                  {ghBlocked && (
                    <div className={styles.section}>
                      <p className="form-group__error">
                        Creating the repository requires the GitHub CLI to be installed and
                        authenticated. Run <code>gh auth login</code>, then reopen this dialog.
                      </p>
                    </div>
                  )}

                  {needsCreate && !ghBlocked && (
                    <div className={styles.section}>
                      <h3 className={styles.sectionTitle}>New repository</h3>
                      <div className={styles.formRow}>
                        <div className="form-group">
                          <label className="form-group__label" htmlFor="ghc-owner">
                            Owner
                          </label>
                          <select
                            id="ghc-owner"
                            className="select"
                            value={owner}
                            onChange={(e) => setOwner(e.target.value)}
                          >
                            {(status.owners ?? []).map((o) => (
                              <option key={o} value={o}>
                                {o}
                              </option>
                            ))}
                          </select>
                        </div>
                        <div className="form-group">
                          <label className="form-group__label" htmlFor="ghc-name">
                            Repository name
                          </label>
                          <input
                            id="ghc-name"
                            className="input"
                            value={name}
                            onChange={(e) => setName(e.target.value)}
                          />
                        </div>
                      </div>
                      <div className="form-group">
                        <label className="form-group__label" htmlFor="ghc-visibility">
                          Visibility
                        </label>
                        <select
                          id="ghc-visibility"
                          className="select"
                          value={visibility}
                          onChange={(e) => setVisibility(e.target.value as 'private' | 'public')}
                        >
                          <option value="private">Private</option>
                          <option value="public">Public</option>
                        </select>
                        <p className="form-group__hint">
                          {owner && name.trim()
                            ? `Creates github.com/${owner}/${name.trim()}`
                            : 'Choose an owner and name for the new repository.'}
                        </p>
                      </div>
                    </div>
                  )}

                  {needsPush && !ghBlocked && (
                    <div className={styles.section}>
                      <h3 className={styles.sectionTitle}>Initial push</h3>
                      <div className="form-group">
                        <label className="form-group__label" htmlFor="ghc-branch">
                          Default branch
                        </label>
                        <input
                          id="ghc-branch"
                          className="input"
                          value={defaultBranch}
                          onChange={(e) => setDefaultBranch(e.target.value)}
                        />
                        <p className="form-group__hint">
                          This workspace&rsquo;s history is pushed under this name, which becomes
                          the repository&rsquo;s default branch.
                        </p>
                      </div>
                    </div>
                  )}
                </>
              )}

              {result && (
                <ul className={styles.steps}>
                  {result.steps.map((s) => {
                    const pill = RESULT_PILLS[s.status] ?? RESULT_PILLS.not_run;
                    return (
                      <li key={s.step} className={styles.step}>
                        <StatusPill variant={pill.variant} label={pill.label} />
                        <span className={styles.stepBody}>
                          <span className={styles.stepLabel}>{STEP_LABELS[s.step] ?? s.step}</span>
                          {s.detail && <span className={styles.stepDetail}>{s.detail}</span>}
                        </span>
                      </li>
                    );
                  })}
                </ul>
              )}

              {result?.success && (
                <div className={styles.section}>
                  <p>
                    Connected to <span className={styles.repoUrl}>{result.repo_url}</span>
                  </p>
                </div>
              )}

              {error && (
                <div className={styles.section}>
                  <p className="form-group__error">{error}</p>
                </div>
              )}
            </div>

            <div className="modal__footer">
              <button type="button" className="btn" onClick={close} disabled={running}>
                {result?.success ? 'Close' : 'Cancel'}
              </button>
              {!result?.success && (
                <button
                  type="button"
                  className="btn btn--primary"
                  onClick={submit}
                  disabled={
                    running || !status || ghBlocked || (needsCreate && (!owner || !name.trim()))
                  }
                >
                  {running ? (
                    <>
                      <span className="spinner spinner--small" />
                      Connecting
                    </>
                  ) : (
                    'Connect'
                  )}
                </button>
              )}
            </div>
          </div>
        </div>
      )}
    </>
  );
}
