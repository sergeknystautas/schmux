import React from 'react';
import type { ConfigFormAction, ConfigFormState } from './useConfigForm';
import type { RepoResponse } from '../../lib/types';

type RepofeedTabProps = {
  repofeedEnabled: boolean;
  repofeedPublishInterval: number;
  repofeedFetchInterval: number;
  repofeedCompletedRetention: number;
  repofeedRepos: Record<string, boolean>;
  repos: RepoResponse[];
  dispatch: React.Dispatch<ConfigFormAction>;
};

const setField = (
  dispatch: React.Dispatch<ConfigFormAction>,
  field: keyof ConfigFormState,
  value: unknown
) => {
  dispatch({ type: 'SET_FIELD', field, value });
};

function repoSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');
}

export default function RepofeedTab({
  repofeedEnabled,
  repofeedPublishInterval,
  repofeedFetchInterval,
  repofeedCompletedRetention,
  repofeedRepos,
  repos,
  dispatch,
}: RepofeedTabProps) {
  const toggleRepo = (slug: string) => {
    const newRepos = { ...repofeedRepos };
    newRepos[slug] = !newRepos[slug];
    setField(dispatch, 'repofeedRepos', newRepos);
  };

  return (
    <div className="settings-tab">
      <div className="settings-section">
        <div className="settings-section__header">
          <h3>Repofeed</h3>
          <p className="form-group__hint">
            Cross-developer intent federation — publish what you're working on and see what others
            are doing.
          </p>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="form-group__label">
              <input
                type="checkbox"
                checked={repofeedEnabled}
                onChange={(e) => setField(dispatch, 'repofeedEnabled', e.target.checked)}
              />{' '}
              Enable repofeed
            </label>
          </div>
        </div>
      </div>

      {repofeedEnabled && (
        <>
          <div className="settings-section">
            <div className="settings-section__header">
              <h3>Timing</h3>
            </div>
            <div className="settings-section__body">
              <div className="form-group">
                <label className="form-group__label">Publish interval</label>
                <div className="input-with-unit">
                  <input
                    type="number"
                    className="input input--compact"
                    value={repofeedPublishInterval}
                    min={5}
                    onChange={(e) =>
                      setField(dispatch, 'repofeedPublishInterval', parseInt(e.target.value) || 30)
                    }
                  />
                  <span className="input-with-unit__unit">seconds</span>
                </div>
              </div>
              <div className="form-group">
                <label className="form-group__label">Fetch interval</label>
                <div className="input-with-unit">
                  <input
                    type="number"
                    className="input input--compact"
                    value={repofeedFetchInterval}
                    min={10}
                    onChange={(e) =>
                      setField(dispatch, 'repofeedFetchInterval', parseInt(e.target.value) || 60)
                    }
                  />
                  <span className="input-with-unit__unit">seconds</span>
                </div>
              </div>
              <div className="form-group">
                <label className="form-group__label">Completed retention</label>
                <div className="input-with-unit">
                  <input
                    type="number"
                    className="input input--compact"
                    value={repofeedCompletedRetention}
                    min={1}
                    onChange={(e) =>
                      setField(
                        dispatch,
                        'repofeedCompletedRetention',
                        parseInt(e.target.value) || 48
                      )
                    }
                  />
                  <span className="input-with-unit__unit">hours</span>
                </div>
              </div>
            </div>
          </div>

          {repos.length > 0 && (
            <div className="settings-section">
              <div className="settings-section__header">
                <h3>Repos</h3>
                <p className="form-group__hint">Choose which repos to include in the repofeed.</p>
              </div>
              <div className="settings-section__body">
                {repos.map((repo) => {
                  const slug = repoSlug(repo.name);
                  const enabled = repofeedRepos[slug] !== false;
                  return (
                    <div key={slug} className="form-group">
                      <label className="form-group__label">
                        <input
                          type="checkbox"
                          checked={enabled}
                          onChange={() => toggleRepo(slug)}
                        />{' '}
                        {repo.name}
                      </label>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}
