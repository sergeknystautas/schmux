import React from 'react';
import TargetSelect from './TargetSelect';
import type { ConfigFormAction, ConfigFormState } from './useConfigForm';
import type { Model, RepoResponse } from '../../lib/types';

type SubredditTabProps = {
  subredditTarget: string;
  subredditInterval: number;
  subredditCheckingRange: number;
  subredditMaxPosts: number;
  subredditMaxAge: number;
  subredditRepos: Record<string, boolean>;
  repos: RepoResponse[];
  models: Model[];
  dispatch: React.Dispatch<ConfigFormAction>;
};

const setField = (
  dispatch: React.Dispatch<ConfigFormAction>,
  field: keyof ConfigFormState,
  value: unknown
) => {
  dispatch({ type: 'SET_FIELD', field, value });
};

// Create URL-safe slug from repo name
function repoSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');
}

export default function SubredditTab({
  subredditTarget,
  subredditInterval,
  subredditCheckingRange,
  subredditMaxPosts,
  subredditMaxAge,
  subredditRepos,
  repos,
  models,
  dispatch,
}: SubredditTabProps) {
  const enabled = !!subredditTarget;

  const toggleRepo = (slug: string) => {
    const newRepos = { ...subredditRepos };
    newRepos[slug] = !newRepos[slug];
    setField(dispatch, 'subredditRepos', newRepos);
  };

  return (
    <div className="wizard-step-content" data-step="7">
      <h2 className="wizard-step-content__title">Subreddit</h2>
      <p className="wizard-step-content__description">
        Generate Reddit-style posts about recent commits for each repository. Posts are created and
        updated automatically based on commit activity.
      </p>

      {/* Section 1: Target */}
      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Target</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="form-group__label">LLM Target</label>
            <TargetSelect
              value={subredditTarget}
              onChange={(v) => setField(dispatch, 'subredditTarget', v)}
              models={models}
              includeDisabledOption={true}
            />
            <p className="form-group__hint">
              Select an LLM target to enable subreddit generation. When disabled, the digest card is
              hidden from the home page.
            </p>
          </div>
        </div>
      </div>

      {/* Section 2: Timing - 2x2 grid */}
      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Timing</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-row">
            <div className="form-group">
              <label className="form-group__label">Polling Interval</label>
              <div className="input-with-unit">
                <input
                  type="number"
                  className="input input--compact"
                  min="5"
                  max="1440"
                  value={subredditInterval || 30}
                  onChange={(e) =>
                    setField(
                      dispatch,
                      'subredditInterval',
                      e.target.value === '' ? 30 : parseInt(e.target.value) || 30
                    )
                  }
                  disabled={!enabled}
                  data-testid="subreddit-interval"
                />
                <span className="input-unit">min</span>
              </div>
              <p className="form-group__hint">How often to check for new commits</p>
            </div>
            <div className="form-group">
              <label className="form-group__label">Update Window</label>
              <div className="input-with-unit">
                <input
                  type="number"
                  className="input input--compact"
                  min="1"
                  max="168"
                  value={subredditCheckingRange || 48}
                  onChange={(e) =>
                    setField(
                      dispatch,
                      'subredditCheckingRange',
                      e.target.value === '' ? 48 : parseInt(e.target.value) || 48
                    )
                  }
                  disabled={!enabled}
                  data-testid="subreddit-checking-range"
                />
                <span className="input-unit">hrs</span>
              </div>
              <p className="form-group__hint">Posts within this window can be updated</p>
            </div>
          </div>
          <div className="form-row">
            <div className="form-group">
              <label className="form-group__label">Max Posts</label>
              <div className="input-with-unit">
                <input
                  type="number"
                  className="input input--compact"
                  min="1"
                  max="100"
                  value={subredditMaxPosts || 30}
                  onChange={(e) =>
                    setField(
                      dispatch,
                      'subredditMaxPosts',
                      e.target.value === '' ? 30 : parseInt(e.target.value) || 30
                    )
                  }
                  disabled={!enabled}
                  data-testid="subreddit-max-posts"
                />
                <span className="input-unit">/repo</span>
              </div>
              <p className="form-group__hint">Maximum posts to keep per repo</p>
            </div>
            <div className="form-group">
              <label className="form-group__label">Max Age</label>
              <div className="input-with-unit">
                <input
                  type="number"
                  className="input input--compact"
                  min="1"
                  max="365"
                  value={subredditMaxAge || 14}
                  onChange={(e) =>
                    setField(
                      dispatch,
                      'subredditMaxAge',
                      e.target.value === '' ? 14 : parseInt(e.target.value) || 14
                    )
                  }
                  disabled={!enabled}
                  data-testid="subreddit-max-age"
                />
                <span className="input-unit">days</span>
              </div>
              <p className="form-group__hint">Posts older than this are removed</p>
            </div>
          </div>
        </div>
      </div>

      {/* Section 3: Repos */}
      {repos.length > 0 && (
        <div className="settings-section">
          <div className="settings-section__header">
            <h3 className="settings-section__title">Repositories</h3>
          </div>
          <div className="settings-section__body">
            <p className="form-group__hint" style={{ marginBottom: '0.75rem' }}>
              Select which repositories should generate posts.
            </p>
            <div className="repo-list">
              {repos.map((repo) => {
                const slug = repoSlug(repo.name);
                const isEnabled = subredditRepos[slug] !== false;
                return (
                  <label
                    key={slug}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 'var(--spacing-xs)',
                      cursor: enabled ? 'pointer' : 'not-allowed',
                      opacity: enabled ? 1 : 0.6,
                    }}
                  >
                    <input
                      type="checkbox"
                      checked={isEnabled}
                      onChange={() => toggleRepo(slug)}
                      disabled={!enabled}
                    />
                    <span>{repo.name}</span>
                  </label>
                );
              })}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
