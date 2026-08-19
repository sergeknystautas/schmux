import { useEffect, useState } from 'react';
import { getServerLoad, getServerLoadVersion } from '../lib/serverLoad';

export default function ServerLoad() {
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem('server-load-collapsed') === '1'
  );
  const [version, setVersion] = useState(getServerLoadVersion());

  // Poll the store version, same pattern as TmuxDiagnostic: re-render
  // only when a new server_load message has actually arrived.
  useEffect(() => {
    const id = setInterval(() => {
      const v = getServerLoadVersion();
      if (v !== version) setVersion(v);
    }, 500);
    return () => clearInterval(id);
  }, [version]);

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      const next = !prev;
      localStorage.setItem('server-load-collapsed', next ? '1' : '0');
      return next;
    });
  };

  const load = getServerLoad();

  return (
    <div className="typing-perf">
      <div className="typing-perf__header">
        <button className="diag-pane__toggle" onClick={toggleCollapsed}>
          <span className={`diag-pane__chevron${collapsed ? '' : ' diag-pane__chevron--open'}`}>
            ▶
          </span>
          <span className="nav-section-title">Server Load</span>
        </button>
      </div>
      {!collapsed &&
        (load ? (
          <div className="server-load__values" data-testid="server-load-values">
            Load: {load.one.toFixed(2)} {load.five.toFixed(2)} {load.fifteen.toFixed(2)}
          </div>
        ) : (
          <div className="typing-perf__empty">Waiting for first sample…</div>
        ))}
    </div>
  );
}
