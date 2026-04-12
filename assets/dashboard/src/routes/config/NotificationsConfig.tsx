import React from 'react';
import type { ConfigPanelProps } from './ConfigPanelProps';
import type { ConfigFormState } from './useConfigForm';

export default function NotificationsConfig({ state, dispatch }: ConfigPanelProps) {
  const setField = (field: string, value: unknown) =>
    dispatch({
      type: 'SET_FIELD',
      field: field as keyof ConfigFormState,
      value,
    });

  return (
    <div className="settings-section">
      <div className="settings-section__header">
        <h3 className="settings-section__title">Notifications</h3>
      </div>
      <div className="settings-section__body">
        <div className="form-group">
          <label className="flex-row gap-xs cursor-pointer">
            <input
              type="checkbox"
              checked={!state.soundDisabled}
              onChange={(e) => setField('soundDisabled', !e.target.checked)}
            />
            <span>Play sound when agents need attention</span>
          </label>
          <p className="form-group__hint">
            Plays an audio notification when an agent signals it needs input or encounters an error.
          </p>
        </div>
        <div className="form-group">
          <label className="flex-row gap-xs cursor-pointer">
            <input
              type="checkbox"
              checked={state.confirmBeforeClose}
              onChange={(e) => setField('confirmBeforeClose', e.target.checked)}
            />
            <span>Confirm before closing tab</span>
          </label>
          <p className="form-group__hint">
            Shows a browser &ldquo;Leave site?&rdquo; dialog when closing or reloading the dashboard
            tab.
          </p>
        </div>
        <div className="form-group">
          <label className="flex-row gap-xs cursor-pointer">
            <input
              type="checkbox"
              checked={state.suggestDisposeAfterPush}
              onChange={(e) => setField('suggestDisposeAfterPush', e.target.checked)}
            />
            <span>Suggest disposing workspace after push to main</span>
          </label>
          <p className="form-group__hint">
            After pushing all commits to main, prompts to dispose the workspace and its sessions.
          </p>
        </div>
      </div>
    </div>
  );
}
