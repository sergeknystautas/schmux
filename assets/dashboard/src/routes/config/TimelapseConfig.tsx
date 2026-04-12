import React from 'react';
import type { ConfigPanelProps } from './ConfigPanelProps';
import type { ConfigFormState } from './useConfigForm';

export default function TimelapseConfig({ state, dispatch }: ConfigPanelProps) {
  const setField = (field: string, value: unknown) =>
    dispatch({
      type: 'SET_FIELD',
      field: field as keyof ConfigFormState,
      value,
    });

  return (
    <div className="form-row">
      <div className="form-group">
        <label className="form-group__label">Retention (days)</label>
        <input
          type="number"
          className="input input--compact"
          value={state.timelapseRetentionDays}
          onChange={(e) => setField('timelapseRetentionDays', parseInt(e.target.value, 10) || 7)}
          min={1}
          max={365}
        />
      </div>
      <div className="form-group">
        <label className="form-group__label">Max file size (MB)</label>
        <input
          type="number"
          className="input input--compact"
          value={state.timelapseMaxFileSizeMB}
          onChange={(e) => setField('timelapseMaxFileSizeMB', parseInt(e.target.value, 10) || 50)}
          min={1}
          max={1000}
        />
      </div>
      <div className="form-group">
        <label className="form-group__label">Max total storage (MB)</label>
        <input
          type="number"
          className="input input--compact"
          value={state.timelapseMaxTotalStorageMB}
          onChange={(e) =>
            setField('timelapseMaxTotalStorageMB', parseInt(e.target.value, 10) || 500)
          }
          min={10}
          max={10000}
        />
      </div>
    </div>
  );
}
