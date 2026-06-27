import React from 'react';
import type { ConfigPanelProps } from './ConfigPanelProps';
import type { ConfigFormState } from './useConfigForm';

function repoSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');
}

export default function RepofeedConfig({ state, dispatch }: ConfigPanelProps) {
  const setField = (field: keyof ConfigFormState, value: unknown) =>
    dispatch({ type: 'SET_FIELD', field, value });

  const toggleRepo = (slug: string) => {
    const newRepos = { ...state.repofeedRepos };
    newRepos[slug] = !newRepos[slug];
    setField('repofeedRepos', newRepos);
  };

  return (
    <>
      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Timing</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="form-group__label">Publish interval</label>
            <div className="input-with-unit">
              <input
                type="number"
                className="input input--compact"
                value={state.repofeedPublishInterval}
                min={5}
                onChange={(e) =>
                  setField('repofeedPublishInterval', parseInt(e.target.value) || 30)
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
                value={state.repofeedFetchInterval}
                min={10}
                onChange={(e) => setField('repofeedFetchInterval', parseInt(e.target.value) || 60)}
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
                value={state.repofeedCompletedRetention}
                min={1}
                onChange={(e) =>
                  setField('repofeedCompletedRetention', parseInt(e.target.value) || 48)
                }
              />
              <span className="input-with-unit__unit">hours</span>
            </div>
          </div>
        </div>
      </div>

      {state.repos.length > 0 && (
        <div className="settings-section">
          <div className="settings-section__header">
            <h3 className="settings-section__title">Repos</h3>
            <p className="form-group__hint">Choose which repos to include in the repofeed.</p>
          </div>
          <div className="settings-section__body">
            <div className="checkbox-list">
              {state.repos.map((repo) => {
                const slug = repoSlug(repo.name);
                const enabled = state.repofeedRepos[slug] === true;
                return (
                  <label key={slug} className="checkbox-list__item">
                    <input type="checkbox" checked={enabled} onChange={() => toggleRepo(slug)} />
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
