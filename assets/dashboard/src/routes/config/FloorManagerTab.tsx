import React from 'react';
import TargetSelect from './TargetSelect';
import type { ConfigFormAction, ConfigFormState } from './useConfigForm';
import type { Model } from '../../lib/types';

type FloorManagerTabProps = {
  fmEnabled: boolean;
  fmTarget: string;
  fmRotationThreshold: number;
  fmDebounceMs: number;
  models: Model[];
  dispatch: React.Dispatch<ConfigFormAction>;
};

export default function FloorManagerTab({
  fmEnabled,
  fmTarget,
  fmRotationThreshold,
  fmDebounceMs,
  models,
  dispatch,
}: FloorManagerTabProps) {
  const setField = (field: string, value: unknown) =>
    dispatch({
      type: 'SET_FIELD',
      field: field as keyof ConfigFormState,
      value,
    });

  return (
    <div className="wizard-step-content" data-step="5">
      <h2 className="wizard-step-content__title">Floor Manager</h2>
      <p className="wizard-step-content__description">
        A singleton agent that monitors all sessions and provides conversational orchestration. When
        enabled, a dedicated tmux session runs the floor manager agent, which receives status
        signals from all active sessions and can coordinate work across agents.
      </p>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">General</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="flex-row gap-xs cursor-pointer">
              <input
                type="checkbox"
                checked={fmEnabled}
                onChange={(e) => setField('fmEnabled', e.target.checked)}
                data-testid="fm-enabled"
              />
              <span>Enable floor manager</span>
            </label>
            <p className="form-group__hint">
              Start a dedicated floor manager session that receives status signals from all active
              sessions.
            </p>
          </div>

          <div className="form-group">
            <label className="form-group__label">Target</label>
            <TargetSelect
              value={fmTarget}
              onChange={(v) => setField('fmTarget', v)}
              models={models}
              includeDisabledOption={false}
              includeNoneOption="Select a target..."
            />
            <p className="form-group__hint">
              The run target (agent) to use for the floor manager session.
            </p>
          </div>
        </div>
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
                value={fmRotationThreshold}
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
                value={fmDebounceMs}
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
    </div>
  );
}
