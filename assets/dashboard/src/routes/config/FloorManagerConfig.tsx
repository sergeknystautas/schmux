import React from 'react';
import TargetSelect from './TargetSelect';
import type { ConfigPanelProps } from './ConfigPanelProps';
import type { ConfigFormState } from './useConfigForm';

export default function FloorManagerConfig({ state, dispatch, models }: ConfigPanelProps) {
  const setField = (field: keyof ConfigFormState, value: unknown) =>
    dispatch({ type: 'SET_FIELD', field, value });

  return (
    <>
      <div className="form-group">
        <label className="form-group__label">Target</label>
        <TargetSelect
          value={state.fmTarget}
          onChange={(v) => setField('fmTarget', v)}
          models={models}
          includeDisabledOption={false}
          includeNoneOption="Select a target..."
        />
        <p className="form-group__hint">
          The run target (agent) to use for the floor manager session.
        </p>
      </div>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Signal Injection</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-row">
            <div className="form-group">
              <label className="form-group__label">Rotation Threshold</label>
              <input
                type="number"
                className="input input--compact"
                value={state.fmRotationThreshold}
                onChange={(e) => setField('fmRotationThreshold', parseInt(e.target.value, 10) || 0)}
                min={0}
                max={10000}
                data-testid="fm-rotation-threshold"
              />
              <p className="form-group__hint">
                Maximum signal injections before forced context rotation. Set to 0 to disable.
              </p>
            </div>
            <div className="form-group">
              <label className="form-group__label">Debounce (ms)</label>
              <input
                type="number"
                className="input input--compact"
                value={state.fmDebounceMs}
                onChange={(e) => setField('fmDebounceMs', parseInt(e.target.value, 10) || 0)}
                min={0}
                max={30000}
                step={100}
                data-testid="fm-debounce-ms"
              />
              <p className="form-group__hint">
                Minimum time between signal injections. Groups rapid state changes into fewer
                messages.
              </p>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}
