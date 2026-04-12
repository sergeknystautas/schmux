import React, { useMemo, useState, useEffect } from 'react';
import type { ConfigPanelProps } from './ConfigPanelProps';
import { getStyles } from '../../lib/api';
import type { Style } from '../../lib/types.generated';

export default function CommStylesConfig({ state, dispatch }: ConfigPanelProps) {
  const [styles, setStyles] = useState<Style[]>([]);

  useEffect(() => {
    let active = true;
    getStyles()
      .then((data) => {
        if (active) setStyles(data.styles || []);
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, []);

  // Unique base tool names (runners) from enabled models.
  // The config stores comm_styles keyed by tool name ("claude"), not model ID.
  const uniqueTools = useMemo(() => {
    const seen = new Set<string>();
    for (const runner of Object.values(state.enabledModels)) {
      if (runner) seen.add(runner);
    }
    return Array.from(seen).sort();
  }, [state.enabledModels]);

  if (styles.length === 0 || uniqueTools.length === 0) {
    return (
      <p className="form-group__hint">
        No styles or enabled models found. Enable models in the Agents tab and create styles to
        configure defaults.
      </p>
    );
  }

  return (
    <>
      <p className="form-group__hint mb-md">
        Set a default communication style for each agent tool. When spawning, agents will use the
        style assigned here unless overridden.
      </p>
      <div className="item-list">
        {uniqueTools.map((toolName) => (
          <div className="item-list__item" key={toolName} data-testid={`comm-style-${toolName}`}>
            <span className="item-list__item-name" style={{ minWidth: '120px' }}>
              {toolName}
            </span>
            <select
              className="select"
              value={state.commStyles[toolName] || ''}
              onChange={(e) => {
                const val = e.target.value;
                const next = { ...state.commStyles };
                if (val) {
                  next[toolName] = val;
                } else {
                  delete next[toolName];
                }
                dispatch({ type: 'SET_FIELD', field: 'commStyles', value: next });
              }}
              style={{ flex: 1, maxWidth: '300px' }}
            >
              <option value="">None</option>
              {styles.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.icon} {s.name}
                </option>
              ))}
            </select>
          </div>
        ))}
      </div>
    </>
  );
}
