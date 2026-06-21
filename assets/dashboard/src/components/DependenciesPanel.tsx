import { Fragment, useState, useEffect } from 'react';
import { getDependencies } from '../lib/api';
import type { DependenciesResponse, Dependency } from '../lib/types.generated';

function InstallCell({ dep }: { dep: Dependency }) {
  if (dep.detected) {
    return <span className="env-badge env-badge--in-sync">installed</span>;
  }
  const m = dep.install?.[0];
  if (!m) return <span className="text-muted">—</span>;
  if (m.command) {
    return (
      <span>
        <code>{m.command}</code>
        {m.requires && <span className="text-muted"> (requires {m.requires})</span>}
      </span>
    );
  }
  if (m.url) {
    return (
      <a href={m.url} target="_blank" rel="noreferrer">
        {m.url}
      </a>
    );
  }
  return <span className="text-muted">—</span>;
}

export function DependenciesPanel() {
  const [data, setData] = useState<DependenciesResponse | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    getDependencies()
      .then((d) => {
        if (!cancelled) setData(d);
      })
      .catch(() => {
        if (!cancelled) setError('Failed to load dependencies');
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (error) return <p className="empty-state__description">{error}</p>;
  if (!data) {
    return (
      <div className="loading-state">
        <span>Loading dependencies...</span>
      </div>
    );
  }

  return (
    <table className="session-table deps-table" data-testid="dependencies-panel">
      <thead>
        <tr>
          <th>Tool</th>
          <th>Install</th>
        </tr>
      </thead>
      <tbody>
        {data.groups.map((g) => (
          <Fragment key={g.id}>
            <tr className="deps-group-row" data-testid={`dep-group-${g.id}`}>
              <th colSpan={2}>{g.display_name}</th>
            </tr>
            {g.dependencies.map((dep) => (
              <tr key={dep.id} data-testid={`dep-row-${dep.id}`}>
                <td>{dep.display_name}</td>
                <td>
                  <InstallCell dep={dep} />
                </td>
              </tr>
            ))}
          </Fragment>
        ))}
      </tbody>
    </table>
  );
}
