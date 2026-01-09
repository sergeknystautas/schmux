import React from 'react';
import { extractRepoName } from '../lib/utils.js';

function getWorkspaceChanges(oldWs, newWs) {
  const changes = [];
  if (oldWs.branch !== newWs.branch) {
    changes.push(`branch: ${oldWs.branch} → ${newWs.branch}`);
  }
  if (oldWs.repo !== newWs.repo) {
    changes.push(`repo: ${extractRepoName(oldWs.repo)} → ${extractRepoName(newWs.repo)}`);
  }
  return changes;
}

export default function ScanResultsModal({ result, onClose }) {
  const hasChanges = result.added.length > 0 || result.updated.length > 0 || result.removed.length > 0;

  return (
    <div className="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="scan-modal-title">
      <div className="modal modal--medium">
        <div className="modal__header">
          <h2 className="modal__title" id="scan-modal-title">Scan Results</h2>
        </div>
        <div className="modal__body">
          {!hasChanges ? (
            <p>No changes detected. All workspaces are up to date.</p>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              {result.added.length > 0 && (
                <div>
                  <h3 className="modal__section-title" style={{ color: 'var(--color-success)', fontSize: '0.875rem', fontWeight: 600, marginBottom: '0.5rem' }}>
                    Added {result.added.length} workspace{result.added.length !== 1 ? 's' : ''}
                  </h3>
                  <ul className="modal__list" style={{ margin: 0, paddingLeft: '1.25rem' }}>
                    {result.added.map((ws) => (
                      <li key={ws.id} style={{ marginBottom: '0.25rem' }}>
                        <strong>{ws.id}</strong>
                        <span style={{ color: 'var(--color-text-subtle)', marginLeft: '0.5rem' }}>
                          {extractRepoName(ws.repo)} · {ws.branch}
                        </span>
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {result.updated.length > 0 && (
                <div>
                  <h3 className="modal__section-title" style={{ color: 'var(--color-warning)', fontSize: '0.875rem', fontWeight: 600, marginBottom: '0.5rem' }}>
                    Updated {result.updated.length} workspace{result.updated.length !== 1 ? 's' : ''}
                  </h3>
                  <ul className="modal__list" style={{ margin: 0, paddingLeft: '1.25rem' }}>
                    {result.updated.map((change) => {
                      const changes = getWorkspaceChanges(change.old, change.new);
                      return (
                        <li key={change.new.id} style={{ marginBottom: '0.25rem' }}>
                          <strong>{change.new.id}</strong>
                          <span style={{ color: 'var(--color-text-subtle)', marginLeft: '0.5rem', fontSize: '0.875rem' }}>
                            {changes.join(', ')}
                          </span>
                        </li>
                      );
                    })}
                  </ul>
                </div>
              )}

              {result.removed.length > 0 && (
                <div>
                  <h3 className="modal__section-title" style={{ color: 'var(--color-error)', fontSize: '0.875rem', fontWeight: 600, marginBottom: '0.5rem' }}>
                    Removed {result.removed.length} workspace{result.removed.length !== 1 ? 's' : ''}
                  </h3>
                  <ul className="modal__list" style={{ margin: 0, paddingLeft: '1.25rem' }}>
                    {result.removed.map((ws) => (
                      <li key={ws.id} style={{ marginBottom: '0.25rem' }}>
                        <strong>{ws.id}</strong>
                        <span style={{ color: 'var(--color-text-subtle)', marginLeft: '0.5rem' }}>
                          {extractRepoName(ws.repo)} · {ws.branch}
                        </span>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}
        </div>
        <div className="modal__footer">
          <button className="btn btn--primary" onClick={onClose}>
            {hasChanges ? 'Done' : 'Close'}
          </button>
        </div>
      </div>
    </div>
  );
}
