import React from 'react';
import { Link } from 'react-router-dom';
import type { ConfigFormAction, ConfigFormState } from './useConfigForm';
import type { OverlayInfo, RepoResponse } from '../../lib/types';

type WorkspacesTabProps = {
  workspacePath: string;
  repos: RepoResponse[];
  overlays: OverlayInfo[];
  newRepoName: string;
  newRepoUrl: string;
  newRepoVcs: string;
  stepErrors: Record<number, string | null>;
  dispatch: React.Dispatch<ConfigFormAction>;
  onEditWorkspacePath: () => void;
  onRemoveRepo: (name: string) => void;
  onAddRepo: () => void;
};

export default function WorkspacesTab({
  workspacePath,
  repos,
  overlays,
  newRepoName,
  newRepoUrl,
  newRepoVcs,
  stepErrors,
  dispatch,
  onEditWorkspacePath,
  onRemoveRepo,
  onAddRepo,
}: WorkspacesTabProps) {
  return (
    <div className="wizard-step-content" data-step="1">
      <h2 className="wizard-step-content__title">Workspace Directory</h2>
      <p className="wizard-step-content__description">
        This is where schmux will store cloned repositories. Each session gets its own workspace
        directory here. Only affects new sessions - existing workspaces keep their current location.
      </p>

      <div className="form-group">
        <label className="form-group__label">Workspace Path</label>
        <div style={{ display: 'flex', gap: 'var(--spacing-sm)', alignItems: 'stretch' }}>
          <input
            type="text"
            className="input"
            value={workspacePath}
            readOnly
            style={{ background: 'var(--color-surface-alt)', flex: 1 }}
          />
          <button type="button" className="btn" onClick={onEditWorkspacePath}>
            Edit
          </button>
        </div>
        <p className="form-group__hint">
          Directory where cloned repositories will be stored. Can use ~ for home directory.
        </p>
      </div>

      <h3>Repositories</h3>
      <p className="wizard-step-content__description">
        Add the Git repositories that run targets will work on.
      </p>

      {repos.length === 0 ? (
        <div className="empty-state-hint">
          No repositories configured. Add at least one to continue.
        </div>
      ) : (
        <div className="item-list">
          {repos.map((repo) => {
            const overlay = overlays.find((o) => o.repo_name === repo.name);
            const overlayPath = overlay?.path || `~/.schmux/overlays/${repo.name}`;
            const fileCount = overlay?.exists ? overlay.file_count : 0;

            return (
              <div className="item-list__item" key={repo.name}>
                <div className="item-list__item-primary">
                  <span className="item-list__item-name">
                    {repo.name}
                    {(repo.vcs === 'sapling' || repo.vcs === 'git-clone') && (
                      <span
                        style={{ marginLeft: 'var(--spacing-xs)', fontSize: '0.8em', opacity: 0.7 }}
                      >
                        [{repo.vcs === 'sapling' ? 'sapling' : 'git clone'}]
                      </span>
                    )}
                  </span>
                  <span className="item-list__item-detail">{repo.url}</span>
                  <Link
                    to="/overlays"
                    className="item-list__item-detail"
                    style={{
                      fontSize: '0.85em',
                      opacity: 0.8,
                      textDecoration: 'none',
                      color: 'inherit',
                    }}
                    title="Open Overlay manager"
                  >
                    Overlay: {overlayPath}{' '}
                    {overlay?.exists ? (
                      <span style={{ color: 'var(--color-success)' }}>({fileCount} files)</span>
                    ) : (
                      <span style={{ color: 'var(--color-text-muted)' }}>(empty)</span>
                    )}{' '}
                    <span style={{ color: 'var(--color-text-muted)', fontSize: '0.9em' }}>
                      → Manage
                    </span>
                  </Link>
                </div>
                <button className="btn btn--sm btn--danger" onClick={() => onRemoveRepo(repo.name)}>
                  Remove
                </button>
              </div>
            );
          })}
        </div>
      )}

      <div className="add-item-form">
        <div className="add-item-form__inputs">
          <input
            type="text"
            className="input"
            placeholder="Name"
            value={newRepoName}
            onChange={(e) =>
              dispatch({ type: 'SET_FIELD', field: 'newRepoName', value: e.target.value })
            }
            onKeyDown={(e) => e.key === 'Enter' && onAddRepo()}
          />
          <input
            type="text"
            className="input"
            placeholder={
              newRepoVcs === 'sapling' ? 'Repo Identifier' : 'git@github.com:user/repo.git'
            }
            value={newRepoUrl}
            onChange={(e) =>
              dispatch({ type: 'SET_FIELD', field: 'newRepoUrl', value: e.target.value })
            }
            onKeyDown={(e) => e.key === 'Enter' && onAddRepo()}
          />
          <select
            className="select"
            value={newRepoVcs}
            onChange={(e) =>
              dispatch({ type: 'SET_FIELD', field: 'newRepoVcs', value: e.target.value })
            }
            style={{ width: 'auto', minWidth: '130px' }}
          >
            <option value="">git worktree</option>
            <option value="git-clone">git clone</option>
            <option value="sapling">sapling</option>
          </select>
        </div>
        <button
          type="button"
          className="btn btn--sm btn--primary"
          onClick={onAddRepo}
          data-testid="add-repo"
        >
          Add
        </button>
      </div>

      {stepErrors[1] && (
        <p className="form-group__error" style={{ marginTop: 'var(--spacing-md)' }}>
          {stepErrors[1]}
        </p>
      )}
    </div>
  );
}
