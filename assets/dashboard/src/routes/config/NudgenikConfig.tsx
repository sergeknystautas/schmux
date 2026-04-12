import React from 'react';
import TargetSelect from './TargetSelect';
import type { ConfigPanelProps } from './ConfigPanelProps';
import type { ConfigFormState } from './useConfigForm';

export default function NudgenikConfig({ state, models, dispatch }: ConfigPanelProps) {
  const setField = (field: string, value: unknown) =>
    dispatch({
      type: 'SET_FIELD',
      field: field as keyof ConfigFormState,
      value,
    });

  const nudgenikTargetMissing =
    !!state.nudgenikTarget &&
    state.nudgenikTarget !== '__disabled__' &&
    !models.some((m) => m.id === state.nudgenikTarget);

  return (
    <div className="settings-section">
      <div className="settings-section__header">
        <h3 className="settings-section__title">NudgeNik</h3>
      </div>
      <div className="settings-section__body">
        <div className="form-group">
          <label className="form-group__label">Target</label>
          <TargetSelect
            value={state.nudgenikTarget}
            onChange={(v) => setField('nudgenikTarget', v)}
            models={models}
          />
          <p className="form-group__hint">
            Select a model for NudgeNik session feedback, or leave disabled.
          </p>
          {nudgenikTargetMissing && (
            <p className="form-group__error">Selected target is not available.</p>
          )}
        </div>

        <div className="form-row">
          <div className="form-group">
            <label className="form-group__label">Viewed Buffer (ms)</label>
            <input
              type="number"
              className="input input--compact"
              min="100"
              value={state.viewedBuffer === 0 ? '' : state.viewedBuffer}
              onChange={(e) =>
                setField(
                  'viewedBuffer',
                  e.target.value === '' ? 0 : parseInt(e.target.value) || 5000
                )
              }
            />
            <p className="form-group__hint">
              Time to keep session marked as "viewed" after last check
            </p>
          </div>

          <div className="form-group">
            <label className="form-group__label">Seen Interval (ms)</label>
            <input
              type="number"
              className="input input--compact"
              min="100"
              value={state.nudgenikSeenInterval === 0 ? '' : state.nudgenikSeenInterval}
              onChange={(e) =>
                setField(
                  'nudgenikSeenInterval',
                  e.target.value === '' ? 0 : parseInt(e.target.value) || 2000
                )
              }
            />
            <p className="form-group__hint">How often to check for session activity</p>
          </div>
        </div>
      </div>
    </div>
  );
}
