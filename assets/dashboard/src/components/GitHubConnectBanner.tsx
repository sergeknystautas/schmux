import { useCallback, useEffect, useState } from 'react';
import { getGitHubConnectStatus, runGitHubConnect, getErrorMessage } from '../lib/api';
import type { GitHubConnectStatus, GitHubConnectResult } from '../lib/types.generated';

const STEP_LABELS: Record<string, string> = {
  set_origin: 'Set git origin',
  create_repo: 'Create GitHub repository',
  update_config: 'Register repository in schmux config',
  link_workspaces: 'Link workspace to repository',
  initial_push: 'Push history to default branch',
};

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
      <div className="banner github-connect-banner">
        <span>This repo only exists in this workspace — connect it to GitHub.</span>
        <button type="button" className="btn btn--primary" onClick={openDialog}>
          Connect to GitHub
        </button>
      </div>
      {open && (
        <div className="modal-overlay" onClick={() => !running && setOpen(false)}>
          <div
            className="modal"
            role="dialog"
            aria-labelledby="github-connect-title"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="modal__header">
              <h2 className="modal__title" id="github-connect-title">
                Connect to GitHub
              </h2>
            </div>
            <div className="modal__body">
              {!status && !error && (
                <p className="text-muted">
                  <span className="spinner" /> Checking GitHub and the workspace's remote…
                </p>
              )}
              {status && !result && (
                <>
                  <ul className="github-connect-steps">
                    {status.plan.map((p) => (
                      <li
                        key={p.step}
                        className={
                          p.needed
                            ? 'github-connect-steps__item github-connect-steps__item--needed'
                            : 'github-connect-steps__item'
                        }
                      >
                        {p.needed ? '○' : '✓'} {STEP_LABELS[p.step] ?? p.step}
                        <span className="text-muted"> — {p.reason}</span>
                      </li>
                    ))}
                  </ul>
                  {ghBlocked && (
                    <p className="text-muted">
                      Creating the repository requires the gh CLI to be installed and authenticated
                      (<code>gh auth login</code>).
                    </p>
                  )}
                  {needsCreate && !ghBlocked && (
                    <>
                      <div className="form-group">
                        <label className="form-group__label" htmlFor="ghc-owner">
                          Owner
                        </label>
                        <select
                          id="ghc-owner"
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
                          value={name}
                          onChange={(e) => setName(e.target.value)}
                        />
                      </div>
                      <div className="form-group">
                        <label className="form-group__label" htmlFor="ghc-visibility">
                          Visibility
                        </label>
                        <select
                          id="ghc-visibility"
                          value={visibility}
                          onChange={(e) => setVisibility(e.target.value as 'private' | 'public')}
                        >
                          <option value="private">Private</option>
                          <option value="public">Public</option>
                        </select>
                      </div>
                    </>
                  )}
                  {needsPush && (
                    <div className="form-group">
                      <label className="form-group__label" htmlFor="ghc-branch">
                        Default branch
                      </label>
                      <input
                        id="ghc-branch"
                        value={defaultBranch}
                        onChange={(e) => setDefaultBranch(e.target.value)}
                      />
                    </div>
                  )}
                </>
              )}
              {result && (
                <ul className="github-connect-steps">
                  {result.steps.map((s) => (
                    <li key={s.step} className="github-connect-steps__item">
                      {s.status === 'done' ? '✓' : s.status === 'skipped' ? '–' : '✗'}{' '}
                      {STEP_LABELS[s.step] ?? s.step}
                      {s.detail && <span className="text-muted"> — {s.detail}</span>}
                    </li>
                  ))}
                </ul>
              )}
              {result?.success && (
                <p>Connected. This workspace is now linked to {result.repo_url}.</p>
              )}
              {error && <div className="banner banner--error">{error}</div>}
            </div>
            <div className="modal__footer">
              <button
                type="button"
                className="btn"
                onClick={() => setOpen(false)}
                disabled={running}
              >
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
                      <span className="spinner" /> Connecting
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
