import React from 'react';
import TargetSelect from './TargetSelect';
import type { ConfigPanelProps } from './ConfigPanelProps';
import type { ConfigFormState } from './useConfigForm';

export default function AutolearnConfig({ state, dispatch, models }: ConfigPanelProps) {
  const setField = (field: string, value: unknown) =>
    dispatch({
      type: 'SET_FIELD',
      field: field as keyof ConfigFormState,
      value,
    });

  return (
    <>
      <div className="form-group">
        <label className="form-group__label" htmlFor="lore-llm-target">
          LLM Target
        </label>
        <TargetSelect
          id="lore-llm-target"
          value={state.loreLLMTarget}
          onChange={(v) => setField('loreLLMTarget', v)}
          includeDisabledOption={false}
          includeNoneOption="None (curator disabled)"
          models={models}
        />
        <p className="form-group__hint">
          Promptable target for curating autolearn entries into documentation proposals.
        </p>
      </div>

      <div className="form-group">
        <label className="form-group__label" htmlFor="lore-curate-on-dispose">
          Curate On Dispose
        </label>
        <select
          id="lore-curate-on-dispose"
          className="input"
          value={state.loreCurateOnDispose}
          onChange={(e) => setField('loreCurateOnDispose', e.target.value)}
        >
          <option value="session">Every session</option>
          <option value="workspace">Last session per workspace</option>
          <option value="never">Never (manual only)</option>
        </select>
        <p className="form-group__hint">
          When to automatically trigger autolearn curation after disposing a session.
        </p>
      </div>

      <div className="form-group">
        <label className="flex-row gap-xs cursor-pointer">
          <input
            type="checkbox"
            checked={state.loreAutoPR}
            onChange={(e) => setField('loreAutoPR', e.target.checked)}
          />
          <span>Auto-create PR after applying proposals</span>
        </label>
        <p className="form-group__hint">
          Automatically open a pull request when an autolearn proposal is applied.
        </p>
      </div>

      <div className="form-group">
        <label className="form-group__label" htmlFor="lore-public-rule-mode">
          Public Rule Mode
        </label>
        <select
          id="lore-public-rule-mode"
          className="input"
          value={state.lorePublicRuleMode || 'direct_push'}
          onChange={(e) => setField('lorePublicRuleMode', e.target.value)}
        >
          <option value="direct_push">Direct push to main</option>
          <option value="create_pr">Create pull request</option>
        </select>
        <p className="form-group__hint">
          How public autolearn rules are committed to the repository.
        </p>
      </div>
    </>
  );
}
