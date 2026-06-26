import React from 'react';
import { EXPERIMENTAL_FEATURES } from './experimentalRegistry';
import { useFeatures } from '../../contexts/FeaturesContext';
import type { ConfigFormState, ConfigFormAction } from './useConfigForm';
import type { TargetOption } from './TargetSelect';
import type { Features } from '../../lib/types.generated';

type ExperimentalTabProps = {
  state: ConfigFormState;
  dispatch: React.Dispatch<ConfigFormAction>;
  models: TargetOption[];
};

export default function ExperimentalTab({ state, dispatch, models }: ExperimentalTabProps) {
  const { features } = useFeatures();

  const visibleFeatures = EXPERIMENTAL_FEATURES.filter((f) => {
    if (f.buildFeatureKey) {
      return features[f.buildFeatureKey as keyof Features] === true;
    }
    return true;
  });

  const setField = (field: keyof ConfigFormState, value: unknown) =>
    dispatch({ type: 'SET_FIELD', field, value });

  return (
    <div className="wizard-step-content" data-testid="config-tab-content-experimental">
      <h2 className="wizard-step-content__title">Experimental Features</h2>
      <p className="wizard-step-content__description">
        Opt-in features that are still evolving. Enable them individually and configure their
        settings.
      </p>

      {visibleFeatures.map((feature) => {
        const enabled = !!state[feature.enabledKey];
        const Panel = feature.configPanel;

        return (
          <div className="settings-section" key={feature.id}>
            <div className="settings-section__header">
              <h3 className="settings-section__title">{feature.name}</h3>
            </div>
            <div className="settings-section__body">
              <div className="form-group">
                <label className="flex-row gap-xs cursor-pointer">
                  <input
                    type="checkbox"
                    checked={enabled}
                    onChange={(e) => setField(feature.enabledKey, e.target.checked)}
                    data-testid={`experimental-toggle-${feature.id}`}
                  />
                  <span>{feature.description}</span>
                </label>
              </div>
              {enabled && Panel && <Panel state={state} dispatch={dispatch} models={models} />}
            </div>
          </div>
        );
      })}

      {visibleFeatures.length === 0 && (
        <p className="wizard-step-content__description">
          No experimental features are available in this build.
        </p>
      )}

      <div className="settings-section" data-testid="experimental-fence">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Fence</h3>
        </div>
        <div className="settings-section__body">
          <p className="wizard-step-content__description">
            Run spawned sessions inside the fence OS sandbox.
          </p>
          {!state.fenceAvailable && (
            <p className="wizard-step-content__description">
              <span>Install fence to use sandboxed sessions.</span>{' '}
              <a
                href="https://fencesandbox.com/docs/guides/agents"
                target="_blank"
                rel="noreferrer"
              >
                fence docs
              </a>
            </p>
          )}
          <div role="radiogroup" style={state.fenceAvailable ? undefined : { opacity: 0.5 }}>
            {[
              { value: 'disabled', label: 'Disabled' },
              { value: 'optional_off', label: 'Optional, default off' },
              { value: 'optional_on', label: 'Optional, default on' },
            ].map((m) => (
              <label key={m.value} className="flex-row gap-xs cursor-pointer">
                <input
                  type="radio"
                  name="fence-mode"
                  value={m.value}
                  checked={(state.fenceMode || 'optional_off') === m.value}
                  disabled={!state.fenceAvailable}
                  onChange={() => setField('fenceMode', m.value)}
                  data-testid={`fence-mode-${m.value}`}
                />
                <span>{m.label}</span>
              </label>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
