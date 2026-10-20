import type { Dispatch } from 'react';
import type { ConfigFormAction, ConfigFormState } from './useConfigForm';
import TargetSelect, { type TargetOption } from './TargetSelect';

type FlightsTabProps = {
  state: Pick<ConfigFormState, 'flightNameTarget' | 'flightPilotTarget' | 'typesafeAPIKeySet'>;
  dispatch: Dispatch<ConfigFormAction>;
  models: TargetOption[];
  pilotModels: TargetOption[];
  flightNameTargetMissing: boolean;
  onTypesafeKeyAction: (mode: 'set' | 'update' | 'remove') => void;
};

export default function FlightsTab({
  state,
  dispatch,
  models,
  pilotModels,
  flightNameTargetMissing,
  onTypesafeKeyAction,
}: FlightsTabProps) {
  const pilotTargetMissing =
    state.flightPilotTarget !== '' && !pilotModels.some((m) => m.id === state.flightPilotTarget);
  return (
    <div className="wizard-step-content" data-testid="config-tab-content-flights">
      <h2 className="wizard-step-content__title">Flights</h2>
      <p className="wizard-step-content__description">
        Configure flight naming and pilot supervision.
      </p>
      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Naming</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="form-group__label" htmlFor="flight-name-target">
              Flight Naming
            </label>
            <TargetSelect
              id="flight-name-target"
              className="select"
              value={state.flightNameTarget}
              options={models}
              onChange={(value) =>
                dispatch({ type: 'SET_FIELD', field: 'flightNameTarget', value })
              }
            />
            <p className="form-group__hint">
              Model that names flights from their prompt. Leave disabled to label flights by ID.
            </p>
            {flightNameTargetMissing && (
              <p className="form-group__error">Selected target is not available.</p>
            )}
          </div>
        </div>
      </div>
      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Pilot Supervision</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="form-group__label" htmlFor="flight-pilot-target">
              Pilot Model
            </label>
            <TargetSelect
              id="flight-pilot-target"
              className="select"
              value={state.flightPilotTarget}
              options={pilotModels}
              includeDisabledOption={false}
              includeNoneOption="Select model"
              onChange={(value) =>
                dispatch({ type: 'SET_FIELD', field: 'flightPilotTarget', value })
              }
            />
            <p className="form-group__hint">
              Chat model for every flight's pilot session. Flights cannot take off until one is
              selected.
            </p>
            {pilotTargetMissing && (
              <p className="form-group__error">Selected model is not available for chat.</p>
            )}
          </div>
          <div className="form-group">
            <span className="form-group__label">TypeSafe API Key</span>
            <p className="form-group__hint">
              Used to classify planner responses and notify the pilot.
            </p>
            <div className="flex-row gap-sm">
              <span className="form-group__hint">
                {state.typesafeAPIKeySet ? 'Key is stored.' : 'No key stored.'}
              </span>
              <button
                className="btn btn--sm btn--primary"
                onClick={() => onTypesafeKeyAction(state.typesafeAPIKeySet ? 'update' : 'set')}
              >
                {state.typesafeAPIKeySet ? 'Update TypeSafe Key' : 'Set TypeSafe Key'}
              </button>
              {state.typesafeAPIKeySet && (
                <button
                  className="btn btn--sm btn--danger"
                  onClick={() => onTypesafeKeyAction('remove')}
                >
                  Remove TypeSafe Key
                </button>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
