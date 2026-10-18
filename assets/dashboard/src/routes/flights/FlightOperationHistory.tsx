import type { FlightOperation } from '../../lib/types.generated';
import { formatTimestamp } from '../../lib/utils';
import layout from './FlightLayout.module.css';

function pillClass(status: string): string {
  switch (status) {
    case 'succeeded':
      return 'status-pill--running';
    case 'failed':
      return 'status-pill--error';
    default:
      return 'status-pill--stopped';
  }
}

/** Every recorded transition and terminate attempt, most recent first. */
export default function FlightOperationHistory({ operations }: { operations: FlightOperation[] }) {
  if (operations.length === 0) {
    return <p className="text-muted p-lg m-0">No operations yet.</p>;
  }
  const ordered = [...operations].reverse();
  return (
    <div className={layout.tableScroll}>
      <table
        className={`session-table ${layout.table}`}
        data-testid="flight-operation-history"
        aria-label="Flight operation history"
      >
        <thead>
          <tr>
            <th scope="col">Action</th>
            <th scope="col">Result</th>
            <th scope="col">Time</th>
          </tr>
        </thead>
        <tbody>
          {ordered.map((op) => (
            <tr key={op.id} data-testid={`flight-operation-${op.id}`}>
              <td>
                {op.action}
                {op.resources && op.resources.length > 0 && (
                  <details className="mt-sm text-muted">
                    <summary className="cursor-pointer">
                      {op.resources.length} resource{op.resources.length === 1 ? '' : 's'}
                    </summary>
                    <ul className={layout.resourceList}>
                      {op.resources.map((r, i) => (
                        <li key={`${r.kind}-${r.id ?? ''}-${i}`}>
                          <span className={r.status === 'failed' ? 'text-error' : 'text-muted'}>
                            {r.kind}
                          </span>{' '}
                          {r.id && <span className="mono">{r.id}</span>} {r.status}
                          {r.error && <span className="text-error"> — {r.error}</span>}
                        </li>
                      ))}
                    </ul>
                  </details>
                )}
              </td>
              <td>
                <span className={`status-pill status-pill--sm ${pillClass(op.status)}`}>
                  <span className="status-pill__dot" />
                  {op.status}
                </span>
                {op.error && (
                  <div
                    className="form-group__error w-full"
                    data-testid={`flight-operation-error-${op.id}`}
                  >
                    {op.error}
                  </div>
                )}
              </td>
              <td className="text-muted">{formatTimestamp(op.created_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
