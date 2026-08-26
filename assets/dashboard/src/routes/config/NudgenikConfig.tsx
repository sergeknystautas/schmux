import React from 'react';
import TargetChain from './TargetChain';
import type { ConfigPanelProps } from './ConfigPanelProps';
import type { ConfigFormState } from './useConfigForm';

export default function NudgenikConfig({ state, models, dispatch }: ConfigPanelProps) {
  const setField = (field: string, value: unknown) =>
    dispatch({
      type: 'SET_FIELD',
      field: field as keyof ConfigFormState,
      value,
    });

  return (
    <div className="settings-section">
      <div className="settings-section__header">
        <h3 className="settings-section__title">NudgeNik</h3>
      </div>
      <div className="settings-section__body">
        <div className="form-group">
          <label className="form-group__label">Target</label>
          <TargetChain
            idPrefix="nudgenik"
            value={state.nudgenikTargets}
            onChange={(v) => setField('nudgenikTargets', v)}
            options={models}
          />
          <p className="form-group__hint">
            Ordered targets for NudgeNik session feedback. Falls back to the next target when one is
            rate-limited (429). Leave empty to disable.
          </p>
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
