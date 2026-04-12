import React from 'react';
import TargetSelect from './TargetSelect';
import type { ConfigPanelProps } from './ConfigPanelProps';
import type { ConfigFormState } from './useConfigForm';

// Create URL-safe slug from repo name
function repoSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');
}

export default function SubredditConfig({ state, dispatch, models }: ConfigPanelProps) {
  const setField = (field: keyof ConfigFormState, value: unknown) =>
    dispatch({ type: 'SET_FIELD', field, value });

  const toggleRepo = (slug: string) => {
    const newRepos = { ...state.subredditRepos };
    newRepos[slug] = !newRepos[slug];
    setField('subredditRepos', newRepos);
  };

  return (
    <>
      <div className="form-group">
        <label className="form-group__label">LLM Target</label>
        <TargetSelect
          value={state.subredditTarget}
          onChange={(v) => setField('subredditTarget', v)}
          models={models}
          includeDisabledOption={true}
        />
        <p className="form-group__hint">
          Select an LLM target to enable subreddit generation. When disabled, the digest card is
          hidden from the home page.
        </p>
      </div>

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
                  value={state.subredditInterval || 30}
                  onChange={(e) =>
                    setField(
                      'subredditInterval',
                      e.target.value === '' ? 30 : parseInt(e.target.value) || 30
                    )
                  }
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
                  value={state.subredditCheckingRange || 48}
                  onChange={(e) =>
                    setField(
                      'subredditCheckingRange',
                      e.target.value === '' ? 48 : parseInt(e.target.value) || 48
                    )
                  }
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
                  value={state.subredditMaxPosts || 30}
                  onChange={(e) =>
                    setField(
                      'subredditMaxPosts',
                      e.target.value === '' ? 30 : parseInt(e.target.value) || 30
                    )
                  }
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
                  value={state.subredditMaxAge || 14}
                  onChange={(e) =>
                    setField(
                      'subredditMaxAge',
                      e.target.value === '' ? 14 : parseInt(e.target.value) || 14
                    )
                  }
                  data-testid="subreddit-max-age"
                />
                <span className="input-unit">days</span>
              </div>
              <p className="form-group__hint">Posts older than this are removed</p>
            </div>
          </div>
        </div>
      </div>

      {state.repos.length > 0 && (
        <div className="settings-section">
          <div className="settings-section__header">
            <h3 className="settings-section__title">Repositories</h3>
          </div>
          <div className="settings-section__body">
            <p className="form-group__hint mb-md">
              Select which repositories should generate posts.
            </p>
            <div className="repo-list">
              {state.repos.map((repo) => {
                const slug = repoSlug(repo.name);
                const isEnabled = state.subredditRepos[slug] !== false;
                return (
                  <label key={slug} className="flex-row gap-xs cursor-pointer">
                    <input type="checkbox" checked={isEnabled} onChange={() => toggleRepo(slug)} />
                    <span>{repo.name}</span>
                  </label>
                );
              })}
            </div>
          </div>
        </div>
      )}
    </>
  );
}
