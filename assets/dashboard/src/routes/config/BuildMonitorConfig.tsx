import React, { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import type { ConfigPanelProps } from './ConfigPanelProps';
import type { BuildMonitorRepoConfig } from '../../lib/types.generated';

function isGithubUrl(url: string): boolean {
  return /^https?:\/\/github\.com\//i.test(url) || /^git@github\.com:/i.test(url);
}

function repoSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');
}

export default function BuildMonitorConfig({ state, dispatch }: ConfigPanelProps) {
  const [identities, setIdentities] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let active = true;
    fetch('/api/build-monitor/identities')
      .then((r) => r.json())
      .then((data) => {
        if (active) {
          setIdentities(data.logins || []);
          setLoading(false);
        }
      })
      .catch(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  const githubRepos = state.repos.filter((r) => isGithubUrl(r.url));

  // Heal entries saved without an identity when only one identity exists —
  // there is no choice to make, so make it.
  useEffect(() => {
    if (identities.length !== 1) return;
    const missing = Object.entries(state.buildMonitorRepos).filter(([, rc]) => !rc.github_login);
    if (missing.length === 0) return;
    const next = { ...state.buildMonitorRepos };
    for (const [name, rc] of missing) {
      next[name] = { ...rc, github_login: identities[0] };
    }
    dispatch({ type: 'SET_FIELD', field: 'buildMonitorRepos', value: next });
  }, [identities, state.buildMonitorRepos, dispatch]);

  const updateRepo = (name: string, patch: Partial<BuildMonitorRepoConfig>) => {
    const next = { ...state.buildMonitorRepos };
    const existing = next[name] || { enabled: false, github_login: '' };
    const merged = { ...existing, ...patch };
    // With a single authorized identity there is no choice to make — bind it.
    if (!merged.github_login && identities.length === 1) {
      merged.github_login = identities[0];
    }
    next[name] = merged;
    dispatch({ type: 'SET_FIELD', field: 'buildMonitorRepos', value: next });
  };

  const signInReady = state.authEnabled && state.authClientIdSet && state.authClientSecretSet;
  const hasIdentities = identities.length > 0;
  const anyRepoEnabled = Object.values(state.buildMonitorRepos).some((rc) => rc.enabled);

  if (loading) {
    return <p className="form-group__hint">Loading…</p>;
  }

  const authorize = () => {
    window.location.href = '/api/build-monitor/connect';
  };

  return (
    <>
      <div className="settings-section" data-testid="build-monitor-section-access">
        <div className="settings-section__header">
          <h3 className="settings-section__title">GitHub Access</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            {hasIdentities ? (
              <>
                <span className="text-success">Authorized</span>
                <span
                  style={{
                    fontSize: '0.9rem',
                    color: 'var(--color-text-secondary)',
                    marginLeft: 'var(--spacing-sm)',
                  }}
                >
                  — {identities.join(', ')}
                </span>
                <div className="flex-row gap-md" style={{ marginTop: 'var(--spacing-xs)' }}>
                  <button type="button" className="btn btn--secondary btn--sm" onClick={authorize}>
                    Authorize another identity
                  </button>
                </div>
              </>
            ) : (
              <button
                type="button"
                className="btn btn--primary btn--sm"
                disabled={!signInReady}
                onClick={authorize}
              >
                Authorize GitHub…
              </button>
            )}
            <p className="form-group__hint">
              {signInReady
                ? 'Build monitoring reads GitHub Actions runs using an authorized identity (repo scope).'
                : 'Requires GitHub sign-in to be set up in the Access tab first.'}
            </p>
          </div>
        </div>
      </div>

      <div className="settings-section" data-testid="build-monitor-section-checking">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Checking</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="form-group__label" htmlFor="bm-interval">
              Check interval (minutes)
            </label>
            <input
              id="bm-interval"
              type="number"
              className="input"
              style={{ maxWidth: '120px' }}
              min={1}
              value={state.buildMonitorInterval}
              onChange={(e) =>
                dispatch({
                  type: 'SET_FIELD',
                  field: 'buildMonitorInterval',
                  value: Math.max(1, parseInt(e.target.value, 10) || 1),
                })
              }
              data-testid="build-monitor-interval"
            />
            <p className="form-group__hint">
              How often the daemon checks GitHub Actions for the enabled repos.
            </p>
          </div>
        </div>
      </div>

      <div
        className="settings-section"
        data-testid="build-monitor-section-repos"
        style={{
          opacity: hasIdentities ? 1 : 0.5,
          pointerEvents: hasIdentities ? 'auto' : 'none',
        }}
      >
        <div className="settings-section__header">
          <h3 className="settings-section__title">Repositories</h3>
          <p className="form-group__hint">
            Each enabled repo watches every active GitHub Actions workflow on its default branch.
          </p>
        </div>
        <div className="settings-section__body">
          {githubRepos.length === 0 ? (
            <p className="form-group__hint">No GitHub repositories configured.</p>
          ) : (
            <div className="form-group">
              <div className="flex-col gap-xs">
                {githubRepos.map((repo) => {
                  const slug = repoSlug(repo.name);
                  const rc = state.buildMonitorRepos[repo.name] || {
                    enabled: false,
                    github_login: identities.length === 1 ? identities[0] : '',
                  };
                  const needsIdentity = !!rc.enabled && !rc.github_login;

                  return (
                    <React.Fragment key={slug}>
                      <label className="flex-row gap-xs cursor-pointer">
                        <input
                          type="checkbox"
                          checked={!!rc.enabled}
                          onChange={(e) => updateRepo(repo.name, { enabled: e.target.checked })}
                          data-testid={`build-monitor-enable-${slug}`}
                        />
                        <span>{repo.name}</span>
                      </label>
                      {rc.enabled && identities.length > 1 && (
                        <div className="flex-row gap-xs">
                          <label className="form-group__label" htmlFor={`bm-identity-${slug}`}>
                            GitHub identity
                          </label>
                          <select
                            id={`bm-identity-${slug}`}
                            className="select"
                            style={{ maxWidth: '300px' }}
                            value={rc.github_login}
                            onChange={(e) =>
                              updateRepo(repo.name, { github_login: e.target.value })
                            }
                            data-testid={`build-monitor-identity-${slug}`}
                          >
                            <option value="">Select identity</option>
                            {identities.map((login) => (
                              <option key={login} value={login}>
                                {login}
                              </option>
                            ))}
                          </select>
                        </div>
                      )}
                      {needsIdentity && (
                        <p className="form-group__error">
                          Select an identity to start monitoring this repo.
                        </p>
                      )}
                    </React.Fragment>
                  );
                })}
              </div>
            </div>
          )}
          {anyRepoEnabled && (
            <p className="form-group__hint">
              Build status appears on the <Link to="/build-monitor">Build Monitor</Link> page.
            </p>
          )}
        </div>
      </div>
    </>
  );
}
